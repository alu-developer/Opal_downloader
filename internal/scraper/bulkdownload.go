package scraper

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/alu-developer/opal-downloader/internal/logging"
	"github.com/mxschmitt/playwright-go"
)

// DownloadFilesBulk fetches every file in the section at sectionURL as one
// ZIP - OPAL's read-only "Gewählte Dateien herunterladen." folder control,
// live-verified in docs/sync-speed-model.md Question 43 to need only read
// access and to preserve real per-file modification timestamps - and writes
// each entry whose base filename matches a key in wantLocalPaths to that
// path, creating parent directories as needed.
//
// It selects every row in the section rather than only the wanted
// filenames. The only row-selection mechanism this project has live-proven
// to actually enable the download button is clicking each checkbox
// individually (the header "select all shortcut" visually checks every row
// but does not fire the per-row AJAX callback the button's enabled state
// depends on - diagnosed live, same cycle); matching wantLocalPaths against
// a section's real, unpredictable row-label text would be a second,
// unproven mechanism on top of that. Selecting everything and filtering the
// zip afterward costs the same one navigation + one download either way, at
// the price of a few extra kilobytes for files nobody asked for.
//
// Returns the modification time OPAL's own folder VFS attached to the zip
// entry, keyed by filename, for every requested file that was found. A
// filename in wantLocalPaths absent from the result was not found in this
// section's zip (wrong section, a permissions gate, or a name mismatch) -
// the caller must fall back to the normal single-file path for it. A
// partial result is not an error for the files that did succeed.
//
// Callers MUST NOT call this concurrently with DownloadFile or another
// DownloadFilesBulk call on the same *OpalScraper: both drive the one
// shared browser page and both serialize on s.browserDownloadMu for exactly
// that reason (see DownloadFile's own doc comment).
func (s *OpalScraper) DownloadFilesBulk(sectionURL string, wantLocalPaths map[string]string) (map[string]time.Time, error) {
	if len(wantLocalPaths) == 0 {
		return nil, nil
	}

	s.browserDownloadMu.Lock()
	defer s.browserDownloadMu.Unlock()

	page := s.getPage()
	if page == nil {
		return nil, errors.New("no browser page available for bulk download")
	}

	// This navigates the shared page away, so whatever the memo recorded
	// about it is no longer true - same reason downloadFileViaBrowser
	// invalidates it before its own navigation.
	s.fallbackPage.invalidate()

	if _, err := s.gotoPolitely(page, sectionURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(30000),
	}); err != nil {
		return nil, fmt.Errorf("navigate to section %s: %w", sectionURL, err)
	}
	_, calm := s.waitForInteractiveLinks(page, contentFallbackWaitMs)
	if _, cerr := s.waitForStableSectionContent(page, calm); cerr != nil {
		logging.Detail("bulk download: waitForStableSectionContent on %s: %v (continuing)", sectionURL, cerr)
	}

	selected, err := page.Evaluate(`() => {
		const boxes = Array.from(document.querySelectorAll('tbody td:first-child input[type=checkbox]'));
		for (const b of boxes) { b.click(); }
		return boxes.length;
	}`)
	if err != nil {
		return nil, fmt.Errorf("select rows on %s: %w", sectionURL, err)
	}
	if evalInt(selected) == 0 {
		return nil, fmt.Errorf("no selectable rows found on %s - not a bulk-download-capable folder page", sectionURL)
	}

	zipFile, err := os.CreateTemp("", "opal-bulkzip-*.zip")
	if err != nil {
		return nil, fmt.Errorf("create scratch zip file: %w", err)
	}
	zipPath := zipFile.Name()
	_ = zipFile.Close()
	defer func() { _ = os.Remove(zipPath) }()

	download, derr := page.ExpectDownload(func() error {
		_, evalErr := page.Evaluate(`() => {
			const nodes = Array.from(document.querySelectorAll('a, button'));
			const scored = nodes.map(el => {
				const text = (el.textContent || '').trim();
				const title = el.getAttribute('title') || '';
				const cls = el.getAttribute('class') || '';
				const hay = (text + ' ' + title + ' ' + cls).toLowerCase();
				let score = -1;
				if (hay.includes('download') || hay.includes('herunterladen')) score = 2;
				else if (hay.includes('zip')) score = 1;
				return {el, score};
			}).filter(s => s.score >= 0).sort((a,b) => b.score - a.score);
			if (scored.length === 0) return false;
			scored[0].el.click();
			return true;
		}`)
		return evalErr
	}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(30000)})
	if derr != nil {
		return nil, fmt.Errorf("trigger bulk download on %s: %w", sectionURL, derr)
	}
	if err := download.SaveAs(zipPath); err != nil {
		return nil, fmt.Errorf("save bulk zip from %s: %w", sectionURL, err)
	}

	r, zerr := zip.OpenReader(zipPath)
	if zerr != nil {
		return nil, fmt.Errorf("open bulk zip from %s: %w", sectionURL, zerr)
	}
	defer func() { _ = r.Close() }()

	results := make(map[string]time.Time, len(wantLocalPaths))
	for _, f := range r.File {
		name := filepath.Base(f.Name)
		target, wanted := wantLocalPaths[name]
		if !wanted {
			continue
		}
		if err := extractZipEntry(f, target); err != nil {
			logging.Warn("bulk download: extracting %s from %s: %v", name, sectionURL, err)
			continue
		}
		results[name] = f.Modified
	}
	return results, nil
}

// extractZipEntry writes f's contents to target, creating target's parent
// directory as needed.
func extractZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, rc)
	return err
}

// evalInt coerces a page.Evaluate JS-number result to int. playwright-go
// returns a JS integer/counter as Go int, not float64 (proven live,
// docs/sync-speed-model.md's 2026-09-02 cycle - three sites in this
// project's own bulk-ZIP probe asserted the wrong type and silently read
// zero every time); this defensively accepts either representation so a
// future playwright-go upgrade that changes that behavior fails loudly
// (returns 0, a real "no rows" error) rather than panicking.
func evalInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
