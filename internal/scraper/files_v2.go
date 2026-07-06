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
