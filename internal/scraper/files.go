package scraper

import (
	"errors"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

// extractSectionContentCandidates extracts candidate link elements from
// page's current content area. page is passed explicitly so this can be run
// against any of the per-worker tabs opened for parallel course crawling,
// not just the single shared s.page.
func (s *OpalScraper) extractSectionContentCandidates(page playwright.Page) ([]map[string]string, error) {
	if page == nil {
		return nil, errors.New("no page available")
	}

	value, err := page.Evaluate(`() => {
			const rootSelectors = [
				'main',
				'[role="main"]',
				'#il_center_col',
				'#center_col',
				'#maincontent',
				'#content',
				'.ilContainerWidth',
				'.ilc_page_cont_page',
				'.o_main_content',
				'.o_page_body',
				'.o_content'
			];
			const roots = [];
			const seenRoots = new Set();
			for (const selector of rootSelectors) {
				for (const root of document.querySelectorAll(selector)) {
					if (seenRoots.has(root)) {
						continue;
					}
					seenRoots.add(root);
					roots.push(root);
				}
			}
			if (roots.length === 0) {
				roots.push(document.body);
			}
			const rowSelector = 'tr, li, .o_file, .ilContainerListItem, .o_table_row, [class*="row"], [class*="item"]';
			const out = [];
			const seen = new Set();
			for (const root of roots) {
				for (const el of root.querySelectorAll('a[href], [onclick], [data-href], [data-url]')) {
					const row = el.closest(rowSelector);
					const item = {
						href: (el.getAttribute('href') || '').trim(),
						onclick: (el.getAttribute('onclick') || '').trim(),
						dataHref: (el.getAttribute('data-href') || '').trim(),
						dataUrl: (el.getAttribute('data-url') || '').trim(),
						text: (el.textContent || '').trim(),
						title: (el.getAttribute('title') || '').trim(),
						rootText: (root.textContent || '').trim(),
						// rowText is the closest table-row/list-item ancestor's own
						// text, used to best-effort parse OPAL's "Größe"/"Zuletzt
						// geändert" file-list columns (see parseRowSizeBytes/
						// parseRowModified below) - unlike rootText (the whole
						// section), this is scoped to just this file's row so the
						// parsed size/date actually belongs to this link.
						rowText: row ? (row.textContent || '').trim() : '',
						// className backs looksLikeShowAllControl's primary check (see
						// its doc comment): OPAL's real "show all" pagination toggle is
						// reliably identified by the class "pager-showall", confirmed
						// live against this account's Analysis course - a far more
						// stable hook than matching the link's visible text, which
						// toggles between "Alle anzeigen" (collapsed) and "Seiten"
						// (expanded) depending on state.
						className: (el.getAttribute('class') || '').trim()
					};
					const key = JSON.stringify(item);
					if (seen.has(key)) {
						continue;
					}
					seen.add(key);
					out.push(item);
				}
			}
			return out;
		}`)
	if err != nil {
		return nil, err
	}

	return toStringMapSlice(value), nil
}

func appendSectionFiles(existing []FileRef, fileSeen map[string]struct{}, candidates []map[string]string, course CourseRef, section SectionRef, sourceURL, showAllURL, expandedPageURL string, showAllViaClick bool, opalURL string, downloadCandidates map[string]downloadCandidate) []FileRef {
	files := append([]FileRef(nil), existing...)
	for _, candidate := range candidates {
		linkTarget := extractLinkTarget(candidate["href"], candidate["onclick"], candidate["dataHref"], candidate["dataUrl"])
		if linkTarget == "" {
			continue
		}
		name := deriveFileName(candidate["title"], candidate["text"], linkTarget)
		if !looksLikeFileLink(linkTarget, name) {
			continue
		}

		absURL := resolveURL(opalURL, linkTarget)
		if !isFileURLAllowedForCourse(absURL, course.RepoID, opalURL) {
			continue
		}

		safeCourse := sanitizeFilename(course.Title)
		fileKey := safeCourse + "|" + absURL
		if _, seen := fileSeen[fileKey]; seen {
			continue
		}
		fileSeen[fileKey] = struct{}{}

		safeName := sanitizeFilename(name)
		if downloadCandidates != nil {
			downloadCandidates[absURL] = downloadCandidate{SourceURL: sourceURL, ShowAllURL: showAllURL, ExpandedPageURL: expandedPageURL, ShowAllViaClick: showAllViaClick, LinkText: strings.TrimSpace(defaultString(candidate["title"], candidate["text"])), LinkTarget: linkTarget}
		}
		rowText := candidate["rowText"]
		files = append(files, FileRef{
			CourseRepoID: course.RepoID,
			CourseTitle:  safeCourse,
			SectionTitle: sanitizeFilename(section.Title),
			Name:         safeName,
			URL:          absURL,
			Path:         filepath.ToSlash(filepath.Join(safeCourse, safeName)),
			Size:         parseRowSizeBytes(rowText),
			Modified:     parseRowModified(rowText),
		})
	}
	return files
}

// fileSizeRe matches OPAL's "Größe" file-list column, e.g. "245 KB", "1,2 MB",
// "3.4 GB", or a bare byte count like "512 Byte"/"512 Bytes". OPAL renders
// this as plain column text (comma as the German decimal separator) rather
// than a structured data-* attribute, so this is a best-effort regex match
// against the file link's row text rather than a precise DOM lookup.
//
// Confirmed live (2026-07, this account's "Materialien" course folder,
// captured DOM saved under tmp/opal-course-1-materialien-links.json - not
// committed, local investigation artifact): the file-list table header row
// reads "Dateityp Name Größe Zuletzt geändert Lizenz", i.e. OPAL does expose
// a "Größe" (size) column for entries in this table. That capture only
// reached a folder-only listing (three subfolders, no individual files were
// visited), so the exact rendering of a *file* row's Größe cell (units used,
// whether it's "245 KB" vs "245,0 KB" vs something else) was not directly
// observed - this regex is written to match OPAL's/OLAT's documented German
// UI convention for this column. A human should confirm against a real file
// row (not a folder row) and adjust the unit list/format here if it differs.
var fileSizeRe = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(GB|MB|KB|Bytes?)`)

// fileModifiedRe matches OPAL's "Zuletzt geändert" column / the "am
// DD.MM.YYYY[, HH:MM Uhr]" phrasing OPAL uses for both files and folders
// (e.g. a folder row's observed text "Probeklausur am 16.06.2026 um 14:11
// Uhr"). Confirmed live against this pattern for folder rows (see
// fileSizeRe's doc comment for the capture details); files in the same
// table are expected to use the same date rendering since they share the
// same "Zuletzt geändert" column, but this was not directly confirmed
// against an individual file row live.
var fileModifiedRe = regexp.MustCompile(`\d{1,2}\.\d{1,2}\.\d{4}(?:,?\s*(?:um\s*)?\d{1,2}:\d{2}(?:\s*Uhr)?)?`)

// parseRowSizeBytes best-effort extracts a file size in bytes from a file
// link's surrounding row text (see the rowText field populated in
// extractSectionContentCandidates above). Returns nil when no size-shaped
// substring is found, which is the common case for course sections where
// OPAL doesn't render this column (e.g. it's hidden, or the entry isn't a
// plain file) - callers must treat a nil Size as "unknown", not "zero
// bytes".
func parseRowSizeBytes(rowText string) *int64 {
	match := fileSizeRe.FindStringSubmatch(rowText)
	if match == nil {
		return nil
	}
	numeric := strings.ReplaceAll(match[1], ",", ".")
	value, err := strconv.ParseFloat(numeric, 64)
	if err != nil {
		return nil
	}
	var multiplier float64
	switch strings.ToUpper(match[2]) {
	case "GB":
		multiplier = 1024 * 1024 * 1024
	case "MB":
		multiplier = 1024 * 1024
	case "KB":
		multiplier = 1024
	default: // "Byte" / "Bytes"
		multiplier = 1
	}
	bytes := int64(value * multiplier)
	return &bytes
}

// parseRowModified best-effort extracts OPAL's "Zuletzt geändert" date text
// from a file link's surrounding row text. The raw matched substring (e.g.
// "01.07.2026, 09:06 Uhr") is kept as-is rather than parsed into a
// time.Time: syncer.fileChanged only ever compares this value for exact
// string equality against the previously recorded one, so any stable,
// OPAL-produced string works as a change signal regardless of its format.
// Returns nil when no date-shaped substring is found.
func parseRowModified(rowText string) *string {
	match := fileModifiedRe.FindString(rowText)
	if match == "" {
		return nil
	}
	return &match
}

var fileNameWhitespaceRe = regexp.MustCompile(`\s+`)

func deriveFileName(title, text, linkTarget string) string {
	for _, raw := range []string{title, text} {
		cleaned := strings.TrimSpace(raw)
		if cleaned == "" {
			continue
		}
		cleaned = fileNameWhitespaceRe.ReplaceAllString(cleaned, " ")
		cleaned = strings.Trim(cleaned, " -,")
		if cleaned != "" {
			return cleaned
		}
	}
	return path.Base(strings.TrimSpace(linkTarget))
}

func looksLikeFileLink(href, name string) bool {
	hrefL := strings.ToLower(strings.TrimSpace(href))
	nameL := strings.ToLower(strings.TrimSpace(name))
	if nameL == "" {
		return false
	}
	if strings.Contains(nameL, "tabelle herunterladen") {
		return false
	}
	if containsAny(hrefL, []string{"/login", "shibboleth", "logout", "cmd=edit", "cmd=delete", "target=fold_", "target=crs_", "target=grp_", "cmd=view", "cmdclass=ilrepositorygui"}) {
		return false
	}
	if containsAny(hrefL, []string{"sendfile", "cmd=sendfile", "target=file_", "file_", "cmd=download", "cmdclass=ilobjfilegui", "cmdclass=ilobjfoldergui", "cmd=export", "/download", "getfile"}) {
		return true
	}
	return fileExtensionRe.MatchString(nameL)
}

var fileExtensionRe = regexp.MustCompile(`\.(pdf|zip|doc|docx|ppt|pptx|xls|xlsx|txt|csv|ipynb|py|java|c|cpp|jpg|jpeg|png)$`)

func looksLikeSectionFolderLink(href, title string) bool {
	hrefL := strings.ToLower(strings.TrimSpace(href))
	titleL := strings.ToLower(strings.TrimSpace(title))
	if titleL == "" {
		return false
	}
	if containsAny(titleL, []string{"forum", "kalender", "neuigkeiten", "ankündigungen", "mitglieder", "teilnehmer", "bewertung", "statistik", "meine kurse", "katalog"}) {
		return false
	}
	if containsAny(hrefL, []string{"target=file_", "cmd=download", "cmd=sendfile", "sendfile", "/download", "mycourses", "membership", "/auth/home", "/auth/repository/catalog", "resource/courses", "resource/resources", "baseclass=ildashboardgui", "baseclass=ilmembershipoverviewgui"}) {
		return false
	}
	return containsAny(hrefL, []string{"target=fold_", "target=grp_", "target=crs_", "goto.php?target=fold_", "goto.php?target=grp_", "goto.php?target=crs_", "/coursenode/", "/repositoryentry/"})
}

// showAllControlClassNeedle is the confirmed, structural CSS class OPAL/OLAT
// (Apache Wicket-based) renders on its table-pagination "show all" toggle
// link. Confirmed live 2026-07-12 (queue task
// fix-show-all-pagination-unverified-guesswork) by inspecting the real DOM of
// this account's "Analysis" course "Übungsblätter" section, which has 28
// files (more than the ~20-item default page size): the toggle renders as
// <a class="pager-showall" href="javascript:;"><span>Alle anzeigen</span></a>
// before expansion. This is the primary, structural signal
// looksLikeShowAllControl checks - unlike the visible text (see
// showAllControlTextNeedles below), the class name does not change when the
// control's own label toggles between "Alle anzeigen" (collapsed/paginated)
// and "Seiten" (expanded/showing everything, click again to re-paginate), so
// it stays a reliable match regardless of which state a section happens to
// be in when this runs.
const showAllControlClassNeedle = "pager-showall"

// showAllControlTextNeedles lists case-insensitive substrings that identify an
// OPAL/ILIAS "expand the paginated list" affordance by its visible link/button text.
// The first entry, "alle anzeigen", is confirmed live (see
// showAllControlClassNeedle's doc comment for the exact capture) as the real
// collapsed-state label OPAL renders for this control. The rest of this list
// (other German phrasings, the English variants) remains an unverified
// best-effort guess for OPAL deployments/locales this account hasn't been
// able to exercise - kept as a secondary fallback behind the class-based
// check above, in case a future OPAL render changes the class name but not
// the wording.
var showAllControlTextNeedles = []string{
	"alle anzeigen",
	"alle einträge anzeigen",
	"alle elemente anzeigen",
	"komplette liste anzeigen",
	"show all",
	"view all",
	"display all",
}

// showAllControlHrefNeedles lists substrings in an href/onclick/data-* target that
// would indicate a direct link to a "show everything, no pagination" URL variant, as
// seen in ILIAS/DataTables-style table pagination (e.g. a DataTables length selector
// wired to length=-1, or a bespoke showAll flag). Confirmed live NOT to be how this
// account's real OPAL/OLAT instance renders the control - the actual href is the inert
// "javascript:;" (see showAllControlClassNeedle's doc comment), with expansion instead
// driven by a Wicket AJAX behavior attached to the element, not a navigable URL
// parameter. Kept as a low-cost fallback in case some other OPAL section/deployment
// does render a plain paginated-vs-all URL variant, but this is unverified and,
// based on what was actually found, unlikely to ever match in practice.
var showAllControlHrefNeedles = []string{
	"length=-1",
	"showall=true",
	"showall=1",
	"show_all=true",
	"show_all=1",
	"pagesize=-1",
}

// looksLikeShowAllControl decides whether a candidate anchor/button found on a
// section or folder page is OPAL's "Alle anzeigen" ("show all") pagination control,
// which expands a truncated (commonly capped at ~20 items) file listing to show every
// entry. Confirmed live 2026-07-12 against this account's "Analysis" course (see
// showAllControlClassNeedle's doc comment): the primary, most reliable signal is the
// "pager-showall" CSS class, checked first; the visible-text and href/onclick checks
// below it are kept as fallbacks for OPAL renders/locales this account hasn't
// exercised.
func looksLikeShowAllControl(linkTarget, text, title, className string) bool {
	classL := strings.ToLower(strings.TrimSpace(className))
	if classL != "" && containsAny(classL, []string{showAllControlClassNeedle}) {
		return true
	}
	textL := strings.ToLower(strings.TrimSpace(defaultString(text, title)))
	hrefL := strings.ToLower(strings.TrimSpace(linkTarget))
	if containsAny(textL, showAllControlTextNeedles) {
		return true
	}
	if hrefL != "" && containsAny(hrefL, showAllControlHrefNeedles) {
		return true
	}
	return false
}

// findShowAllTarget scans extracted candidates (as produced by
// extractSectionContentCandidates) for a "show all" pagination control and returns
// its raw (possibly relative) link target. It reports found=false when none matches,
// which is the common case for sections that already show every file.
func findShowAllTarget(candidates []map[string]string) (linkTarget string, found bool) {
	for _, candidate := range candidates {
		target := extractLinkTarget(candidate["href"], candidate["onclick"], candidate["dataHref"], candidate["dataUrl"])
		if !looksLikeShowAllControl(target, candidate["text"], candidate["title"], candidate["className"]) {
			continue
		}
		return target, true
	}
	return "", false
}

// isFileURLAllowedForCourse decides whether absURL - already confirmed to
// looksLikeFileLink-match - should actually be treated as a downloadable file for the
// given course. opalURL gates out links to a different host entirely: a course's
// "Linkliste" section commonly lists plain external resource links (e.g. a tool's
// homepage) alongside real file downloads, and looksLikeFileLink's substring checks
// can't tell those apart from an OPAL sendfile link by shape alone (confirmed live:
// "[Java-Tools] Eclipse" -> https://www.eclipse.org/downloads/, misclassified as a
// file because "/downloads/" contains the "/download" substring looksLikeFileLink
// checks for - see queue-task fix-download-fallback-html-errors-post-pr11). No
// genuine OPAL-hosted file link is ever served from a different host than opalURL, so
// this check is safe to apply unconditionally.
func isFileURLAllowedForCourse(absURL, repoID, opalURL string) bool {
	if strings.TrimSpace(absURL) == "" || strings.TrimSpace(repoID) == "" {
		return false
	}
	if !isSameHostAsOpal(absURL, opalURL) {
		return false
	}
	relatedRepoID := extractRepositoryEntryID(absURL)
	if relatedRepoID != "" && relatedRepoID != repoID {
		return false
	}
	urlLower := strings.ToLower(absURL)
	if containsAny(urlLower, []string{"/auth/home", "/auth/repository/catalog", "mycourses", "membership", "baseclass=ildashboardgui", "baseclass=ilmembershipoverviewgui", "resource/courses", "resource/resources"}) {
		return false
	}
	return true
}
