package scraper

import (
	"fmt"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/logging"
	"github.com/mxschmitt/playwright-go"
)

// This file is the fetch layer of the serial-hybrid discovery path (option A,
// docs/RESUME.md). httpdiscovery.go parses a section's HTML into candidates;
// this file fetches that HTML over the authenticated session and turns it
// into FileRefs, including the pager-showAllLink follow-up that recovers
// files beyond a section's ~20-row first page.
//
// CRITICAL INVARIANT: fetchSectionFilesHTTP must never run while the browser
// is mid-crawl against the same OPAL session. Measured 2026-07-31, concurrent
// HTTP + browser corrupted the shared Wicket session and made the browser's
// show-all expansion drop 125/200 files. The caller (the hybrid discovery
// method) is responsible for finishing the browser tree-walk before invoking
// any HTTP fetch.

// httpFetcher is the narrow slice of playwright's APIRequestContext that
// fetchSectionFilesHTTP needs, factored out so the logic is unit-testable
// with a fake instead of a live browser. mirrors the field set the probe
// (httpdiscovery_probe_test.go) already drives against reqCtx.Get.
type httpFetcher interface {
	Get(url string, options ...playwright.APIRequestContextGetOptions) (playwright.APIResponse, error)
}

// responseBodyText is a tiny helper that mirrors the probe's resp.Text() use;
// kept inline because playwright.APIResponse.Text already returns (string, error).
func responseBodyText(resp playwright.APIResponse) (string, error) { return resp.Text() }

// fetchSectionFilesHTTP fetches one section's file table over HTTP and returns
// the FileRefs it contains, parsed by the same appendSectionFiles predicate
// the browser path uses (so HTTP and browser results share a Path|URL merge
// key and dedupe cleanly). When the section is paginated, it additionally
// fetches the pager-showAllLink AJAX URL to recover files beyond the first
// page. course/section identify the result; opalURL resolves relative links.
//
// Returns the files and the number of HTTP requests issued (1 for a normal
// section, 2 for a paginated one), so the caller can log server-load impact.
func fetchSectionFilesHTTP(fetch httpFetcher, course CourseRef, section SectionRef, opalURL string) ([]FileRef, int, error) {
	resp, err := fetch.Get(section.URL)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP GET section %s: %w", section.URL, err)
	}
	body, err := responseBodyText(resp)
	status := resp.Status()
	if err != nil {
		return nil, 1, fmt.Errorf("HTTP read section %s: %w", section.URL, err)
	}
	if status != 200 {
		return nil, 1, fmt.Errorf("HTTP GET section %s: status %d", section.URL, status)
	}

	requests := 1
	candidates := parseHTTPSectionCandidates(body)
	fileSeen := map[string]struct{}{}
	files := appendSectionFiles(nil, fileSeen, candidates, course, section, section.URL, "", "", false, opalURL, nil)

	// Paginated section: the first page caps at ~20 rows. The show-all AJAX
	// endpoint returns the full table over plain HTTP (measured: Part-3 went
	// 20 -> 57 data-file-name entries). Fetch it and merge, reusing the same
	// predicate so the merge is just more candidates through the same filter.
	if showAllRel := extractShowAllURLFromHTML(body); showAllRel != "" {
		showAllURL := resolveURL(opalURL, showAllRel)
		showResp, showErr := fetch.Get(showAllURL)
		requests = 2
		if showErr != nil {
			logging.Warn("HTTP show-all fetch failed for %s: %v (continuing with first-page files only)", section.URL, showErr)
			return files, requests, nil
		}
		showBody, showBodyErr := responseBodyText(showResp)
		if showBodyErr != nil || showResp.Status() != 200 {
			logging.Warn("HTTP show-all for %s: status %d, err %v (continuing with first-page files only)", section.URL, showResp.Status(), showBodyErr)
			return files, requests, nil
		}
		showCandidates := parseHTTPSectionCandidates(showBody)
		// The show-all URL is the section's "expanded" view; pass it as the
		// sourceURL so downloadCandidates (when used) point where the file
		// was actually seen. FileSeen dedupes against the first-page pass.
		files = appendSectionFiles(files, fileSeen, showCandidates, course, section, showAllURL, "", "", false, opalURL, nil)
	}

	if len(files) == 0 {
		// Not an error (a section can genuinely have no files), but log it at
		// detail level so an HTTP path that silently returns nothing for a
		// section the browser found files in is visible during verification.
		logging.Detail("HTTP section %s returned 0 files (body %d bytes)", section.URL, len(body))
	}
	return files, requests, nil
}

// fileNamesFromHTTPFiles is a test helper that collapses a FileRef slice to a
// sorted, deduped name list - the shape the 345-file-contract diff uses.
func fileNamesFromHTTPFiles(files []FileRef) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if !seen[f.Name] {
			seen[f.Name] = true
			out = append(out, f.Name)
		}
	}
	// stable order for diffs
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && strings.Compare(out[j], out[j-1]) < 0 {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}
