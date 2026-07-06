package scraper

import (
	"errors"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

func (s *OpalScraper) extractSectionContentCandidatesV2() ([]map[string]string, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	value, err := s.page.Evaluate(`() => {
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
			const out = [];
			const seen = new Set();
			for (const root of roots) {
				for (const el of root.querySelectorAll('a[href], [onclick], [data-href], [data-url]')) {
					const item = {
						href: (el.getAttribute('href') || '').trim(),
						onclick: (el.getAttribute('onclick') || '').trim(),
						dataHref: (el.getAttribute('data-href') || '').trim(),
						dataUrl: (el.getAttribute('data-url') || '').trim(),
						text: (el.textContent || '').trim(),
						title: (el.getAttribute('title') || '').trim(),
						rootText: (root.textContent || '').trim(),
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

func appendSectionFilesV2(existing []FileRefV2, fileSeen map[string]struct{}, candidates []map[string]string, course CourseRefV2, section SectionRefV2, sourceURL, opalURL string, downloadCandidates map[string]downloadCandidate) []FileRefV2 {
	files := append([]FileRefV2(nil), existing...)
	for _, candidate := range candidates {
		linkTarget := extractLinkTarget(candidate["href"], candidate["onclick"], candidate["dataHref"], candidate["dataUrl"])
		if linkTarget == "" {
			continue
		}
		name := deriveFileNameV2(candidate["title"], candidate["text"], linkTarget)
		if !looksLikeFileLinkV2(linkTarget, name) {
			continue
		}

		absURL := resolveURL(opalURL, linkTarget)
		if !isFileURLAllowedForCourseV2(absURL, course.RepoID) {
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
			downloadCandidates[absURL] = downloadCandidate{SourceURL: sourceURL, LinkText: strings.TrimSpace(defaultString(candidate["title"], candidate["text"])), LinkTarget: linkTarget}
		}
		files = append(files, FileRefV2{
			CourseRepoID: course.RepoID,
			CourseTitle:  safeCourse,
			SectionTitle: sanitizeFilename(section.Title),
			Name:         safeName,
			URL:          absURL,
			Path:         filepath.ToSlash(filepath.Join(safeCourse, safeName)),
		})
	}
	return files
}

var fileNameWhitespaceRe = regexp.MustCompile(`\s+`)

func deriveFileNameV2(title, text, linkTarget string) string {
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

func looksLikeFileLinkV2(href, name string) bool {
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

func looksLikeSectionFolderLinkV2(href, title string) bool {
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

// showAllControlTextNeedlesV2 lists case-insensitive substrings that identify an
// OPAL/ILIAS "expand the paginated list" affordance by its visible link/button text.
// This is a best-effort guess based on common OPAL/ILIAS UI copy (German and English
// variants); it has not been verified against a live OPAL instance, since this
// environment has no OPAL login available. A human should confirm the exact wording
// against a real course with a paginated (>20 item) file list and extend this list if
// needed.
var showAllControlTextNeedlesV2 = []string{
	"alle anzeigen",
	"alle einträge anzeigen",
	"alle elemente anzeigen",
	"komplette liste anzeigen",
	"show all",
	"view all",
	"display all",
}

// showAllControlHrefNeedlesV2 lists substrings in an href/onclick/data-* target that
// indicate a direct link to a "show everything, no pagination" URL variant, as seen in
// ILIAS/DataTables-style table pagination (e.g. a DataTables length selector wired to
// length=-1, or a bespoke showAll flag). Also a best-effort guess pending live
// verification.
var showAllControlHrefNeedlesV2 = []string{
	"length=-1",
	"showall=true",
	"showall=1",
	"show_all=true",
	"show_all=1",
	"pagesize=-1",
}

// looksLikeShowAllControlV2 decides whether a candidate anchor/button found on a
// section or folder page is OPAL's "Alle anzeigen" ("show all") pagination control,
// which expands a truncated (commonly capped at ~20 items) file listing to show every
// entry. The exact OPAL markup could not be verified live in this environment (no OPAL
// login available here), so this matches on common OPAL/ILIAS UI text and known
// "no pagination" URL parameter shapes; a human should verify against a real course
// known to have more than 20 files in one section.
func looksLikeShowAllControlV2(linkTarget, text, title string) bool {
	textL := strings.ToLower(strings.TrimSpace(defaultString(text, title)))
	hrefL := strings.ToLower(strings.TrimSpace(linkTarget))
	if containsAny(textL, showAllControlTextNeedlesV2) {
		return true
	}
	if hrefL != "" && containsAny(hrefL, showAllControlHrefNeedlesV2) {
		return true
	}
	return false
}

// findShowAllTargetV2 scans extracted candidates (as produced by
// extractSectionContentCandidatesV2) for a "show all" pagination control and returns
// its raw (possibly relative) link target. It reports found=false when none matches,
// which is the common case for sections that already show every file.
func findShowAllTargetV2(candidates []map[string]string) (linkTarget string, found bool) {
	for _, candidate := range candidates {
		target := extractLinkTarget(candidate["href"], candidate["onclick"], candidate["dataHref"], candidate["dataUrl"])
		if !looksLikeShowAllControlV2(target, candidate["text"], candidate["title"]) {
			continue
		}
		return target, true
	}
	return "", false
}

func isFileURLAllowedForCourseV2(absURL, repoID string) bool {
	if strings.TrimSpace(absURL) == "" || strings.TrimSpace(repoID) == "" {
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
