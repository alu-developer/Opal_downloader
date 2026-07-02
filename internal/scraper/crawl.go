package scraper

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

func (s *OpalScraper) crawlCourseFiles(courseName, startURL string) ([]RemoteFile, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	files := make([]RemoteFile, 0)
	fileSeen := map[string]struct{}{}
	visitedPages := map[string]struct{}{}
	queue := []string{startURL}
	queued := map[string]struct{}{normalizeURLForCrawl(startURL): {}}
	maxPages := 12
	startRepoID := extractRepositoryEntryID(startURL)

	for len(queue) > 0 && len(visitedPages) < maxPages {
		currentURL := queue[0]
		queue = queue[1:]
		currentKey := normalizeURLForCrawl(currentURL)
		delete(queued, currentKey)
		if _, ok := visitedPages[currentKey]; ok {
			continue
		}
		visitedPages[currentKey] = struct{}{}

		if len(visitedPages)%10 == 0 {
			fmt.Printf("    Crawl progress: visited=%d queued=%d files=%d\n", len(visitedPages), len(queue), len(files))
		}

		_, err := s.page.Goto(currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)})
		if err != nil {
			continue
		}
		if _, err := s.page.WaitForSelector("a[href], [onclick], [data-href], [data-url]", playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(4000)}); err != nil {
			s.page.WaitForTimeout(1200)
		}

		value, err := s.page.Evaluate(`() => {
                    const out = [];
                    for (const el of document.querySelectorAll('a[href], [onclick], [data-href], [data-url]')) {
                        out.push({
                            href: (el.getAttribute('href') || '').trim(),
                            text: (el.textContent || '').trim(),
                            title: (el.getAttribute('title') || '').trim(),
                            onclick: (el.getAttribute('onclick') || '').trim(),
                            dataHref: (el.getAttribute('data-href') || '').trim(),
                            dataUrl: (el.getAttribute('data-url') || '').trim(),
                        });
                    }
                    return out;
                }`)
		if err != nil {
			continue
		}
		links := toStringMapSlice(value)

		for _, item := range links {
			hrefRaw := strings.TrimSpace(item["href"])
			onclickRaw := strings.TrimSpace(item["onclick"])
			dataHrefRaw := strings.TrimSpace(item["dataHref"])
			dataURLRaw := strings.TrimSpace(item["dataUrl"])
			text := pickLabel(strings.TrimSpace(item["title"]), strings.TrimSpace(item["text"]))
			linkTarget := extractLinkTarget(hrefRaw, onclickRaw, dataHrefRaw, dataURLRaw)
			if linkTarget == "" {
				continue
			}

			absURL := resolveURL(s.opalURL, linkTarget)
			if !strings.Contains(strings.ToLower(absURL), "bildungsportal.sachsen.de/opal") {
				continue
			}

			repoID := extractRepositoryEntryID(absURL)
			if startRepoID != "" && repoID != "" && repoID != startRepoID {
				continue
			}

			if looksLikeDownloadLink(linkTarget, text) {
				cleanedName := sanitizeFilename(defaultString(text, path.Base(absURL), "download"))
				cleanedCourse := sanitizeFilename(courseName)
				fileKey := cleanedCourse + "|" + absURL
				if _, seen := fileSeen[fileKey]; seen {
					continue
				}
				fileSeen[fileKey] = struct{}{}
				s.downloadCandidates[absURL] = downloadCandidate{SourceURL: currentURL, LinkText: text, LinkTarget: linkTarget}
				files = append(files, RemoteFile{
					Name:   cleanedName,
					URL:    absURL,
					Course: cleanedCourse,
					Path:   filepath.ToSlash(filepath.Join(cleanedCourse, cleanedName)),
				})
				fmt.Printf("    Detected file: %s\n", cleanedName)
				continue
			}

			if looksLikeBrowseLink(linkTarget, text) {
				absKey := normalizeURLForCrawl(absURL)
				if _, seen := visitedPages[absKey]; seen {
					continue
				}
				if _, pending := queued[absKey]; pending {
					continue
				}
				queue = append(queue, absURL)
				queued[absKey] = struct{}{}
			}
		}
	}

	fmt.Printf("    Crawled %d pages, found %d files\n", len(visitedPages), len(files))
	return files, nil
}

func looksLikeDownloadLink(href, text string) bool {
	hrefL := strings.ToLower(href)
	textL := strings.ToLower(text)
	if strings.Contains(textL, "tabelle herunterladen") {
		return false
	}
	if containsAny(hrefL, []string{"/login", "shibboleth", "logout", "cmd=edit", "cmd=delete"}) {
		return false
	}
	if containsAny(hrefL, []string{"target=fold_", "target=crs_", "target=grp_", "cmd=view", "cmdclass=ilrepositorygui"}) {
		return false
	}
	if containsAny(hrefL, []string{"sendfile", "cmd=sendfile", "target=file_", "file_", "cmd=download", "cmdclass=ilobjfilegui", "cmdclass=ilobjfoldergui", "cmd=export", "/download", "getfile"}) {
		return true
	}
	extRe := regexp.MustCompile(`\.(pdf|zip|doc|docx|ppt|pptx|xls|xlsx|txt|csv|ipynb|py|java|c|cpp|jpg|jpeg|png)$`)
	return extRe.MatchString(textL)
}

func looksLikeBrowseLink(href, text string) bool {
	hrefL := strings.ToLower(href)
	textL := strings.ToLower(text)
	if containsAny(textL, []string{"neuigkeiten", "ankündigungen", "forum", "kalender", "gehe zu seite", "aktuelle seite", "vorherige seite", "nächste seite"}) {
		return false
	}
	if containsAny(hrefL, []string{"/login", "shibboleth", "logout", "cmd=edit", "cmd=delete", "target=file_", "downloadtablecontainer", "&anticache=", "-pager-"}) {
		return false
	}
	return containsAny(hrefL, []string{"target=fold_", "target=grp_", "target=crs_", "goto.php?target=fold_", "goto.php?target=grp_", "goto.php?target=crs_", "/coursenode/", "/repositoryentry/", "baseclass=ilmembershipoverviewgui", "baseclass=ildashboardgui", "mycourses", "membership"})
}

func looksLikeCourseLink(href, text string) bool {
	hrefL := strings.ToLower(href)
	textL := strings.ToLower(text)
	if len(textL) < 3 {
		return false
	}
	if strings.HasPrefix(textL, "http://") || strings.HasPrefix(textL, "https://") {
		return false
	}
	if containsAny(textL, []string{"aktuelle seite", "vorherige seite", "nächste seite", "next page", "previous page"}) {
		return false
	}
	if containsAny(hrefL, []string{"target=file_", "cmd=download", "cmd=sendfile", "sendfile", "/download"}) {
		return false
	}
	if containsAny(textL, []string{"neuigkeiten", "ankündigungen", "materialien", "übungseinschreibung", "forum", "kalender"}) {
		return false
	}
	return containsAny(hrefL, []string{"target=crs_", "target=grp_", "goto.php?target=crs_", "goto.php?target=grp_", "/auth/repositoryentry/", "/auth/coursenode/"})
}

func isCourseLabel(text string) bool {
	textL := strings.ToLower(strings.TrimSpace(text))
	if len(textL) < 3 {
		return false
	}
	return !containsAny(textL, []string{"gehe zu", "aktuelle seite", "vorherige seite", "nächste seite", "tabelle herunterladen", "ankündigungen", "materialien", "übungseinschreibung", "vorlesung", "übungsblätter", "probeklausur"})
}

func looksLikeCourseCandidate(candidate map[string]string) bool {
	href := candidate["href"]
	text := candidate["text"]
	title := candidate["title"]
	cardText := candidate["cardText"]
	cardClass := strings.ToLower(candidate["cardClass"])
	combined := strings.ToLower(strings.Join([]string{text, title, cardText}, " "))

	if !looksLikeCourseLink(href, defaultString(title, text)) {
		return false
	}
	if containsAny(combined, []string{"gehe zu seite", "tabelle herunterladen", "forum", "kalender", "ankündigungen"}) {
		return false
	}
	if strings.Contains(cardClass, "dynamic-tab") {
		return true
	}
	if strings.Contains(cardClass, "list-group-item") && strings.Contains(combined, "verantwortliche") {
		return true
	}
	if strings.Contains(combined, "zuletzt angesehen") {
		return true
	}
	return isCourseLabel(defaultString(title, text))
}

func pickLabel(titleText, visibleText string) string {
	if len(titleText) > len(visibleText) {
		return titleText
	}
	return visibleText
}
