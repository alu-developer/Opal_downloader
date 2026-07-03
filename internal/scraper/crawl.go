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

type courseSection struct {
	Label string
	URL   string
}

func (s *OpalScraper) crawlCourseFiles(courseName, startURL string) ([]RemoteFile, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	sections, err := s.discoverCourseSections(startURL)
	if err != nil {
		return nil, err
	}

	files := make([]RemoteFile, 0)
	fileSeen := map[string]struct{}{}
	visitedPages := map[string]struct{}{}
	startRepoID := extractRepositoryEntryID(defaultString(startURL, sections[0].URL))

	for _, section := range sections {
		fmt.Printf("    Section: %s\n", section.Label)
		sectionFiles, crawlErr := s.crawlSectionFiles(courseName, section.URL, startRepoID, visitedPages, fileSeen)
		if crawlErr != nil {
			fmt.Printf("    Section error: %v\n", crawlErr)
			continue
		}
		files = append(files, sectionFiles...)
	}

	fmt.Printf("    Crawled %d pages, found %d files\n", len(visitedPages), len(files))
	return files, nil
}

func (s *OpalScraper) discoverCourseSections(startURL string) ([]courseSection, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	if _, err := s.page.Goto(startURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)}); err != nil {
		return nil, err
	}
	if _, err := s.page.WaitForSelector("a[href], [onclick], [data-href], [data-url]", playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(4000)}); err != nil {
		s.page.WaitForTimeout(1200)
	}

	currentURL := defaultString(s.page.URL(), startURL)
	repoID := extractRepositoryEntryID(currentURL)
	if repoID == "" {
		repoID = extractRepositoryEntryID(startURL)
	}

	sections := []courseSection{{Label: "Kursstart", URL: currentURL}}
	if repoID == "" {
		return sections, nil
	}

	value, err := s.page.Evaluate(`() => {
			const selectors = [
				'aside',
				'nav',
				'[role="navigation"]',
				'#left_col',
				'#il_left_col',
				'#sidebar',
				'.sidebar',
				'.ilContainerSideBlock',
				'.o_tree',
				'.o_page_side',
				'.o_page_sidebar',
				'.il_VAccordionInnerContainer'
			];
			const roots = [];
			const seenRoots = new Set();
			for (const selector of selectors) {
				for (const root of document.querySelectorAll(selector)) {
					if (seenRoots.has(root)) {
						continue;
					}
					seenRoots.add(root);
					roots.push(root);
				}
			}
			const out = [];
			const seen = new Set();
			for (const root of roots) {
				for (const el of root.querySelectorAll('a[href], [onclick], [data-href], [data-url]')) {
					const item = {
						href: (el.getAttribute('href') || '').trim(),
						text: (el.textContent || '').trim(),
						title: (el.getAttribute('title') || '').trim(),
						onclick: (el.getAttribute('onclick') || '').trim(),
						dataHref: (el.getAttribute('data-href') || '').trim(),
						dataUrl: (el.getAttribute('data-url') || '').trim(),
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
		return sections, nil
	}

	return appendCourseSections(sections, toStringMapSlice(value), s.opalURL, repoID), nil
}

func appendCourseSections(existing []courseSection, candidates []map[string]string, opalURL, repoID string) []courseSection {
	sections := append([]courseSection(nil), existing...)
	seen := make(map[string]struct{}, len(sections))
	for _, section := range sections {
		seen[normalizeURLForCrawl(section.URL)] = struct{}{}
	}

	for _, item := range candidates {
		linkTarget := extractLinkTarget(strings.TrimSpace(item["href"]), strings.TrimSpace(item["onclick"]), strings.TrimSpace(item["dataHref"]), strings.TrimSpace(item["dataUrl"]))
		if linkTarget == "" {
			continue
		}
		text := pickLabel(strings.TrimSpace(item["title"]), strings.TrimSpace(item["text"]))
		if !looksLikeSectionLink(linkTarget, text) {
			continue
		}
		absURL := resolveURL(opalURL, linkTarget)
		candidateRepoID := extractRepositoryEntryID(absURL)
		if candidateRepoID != "" && candidateRepoID != repoID {
			continue
		}
		key := normalizeURLForCrawl(absURL)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sections = append(sections, courseSection{Label: defaultString(text, "Bereich"), URL: absURL})
	}

	return sections
}

func (s *OpalScraper) crawlSectionFiles(courseName, startURL, startRepoID string, visitedPages, fileSeen map[string]struct{}) ([]RemoteFile, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	files := make([]RemoteFile, 0)
	queue := []string{startURL}
	queued := map[string]struct{}{normalizeURLForCrawl(startURL): {}}
	maxPages := 12

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

			if looksLikeCourseBrowseLink(linkTarget, text) {
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

func looksLikeCourseBrowseLink(href, text string) bool {
	hrefL := strings.ToLower(href)
	textL := strings.ToLower(text)
	if containsAny(textL, []string{"neuigkeiten", "ankündigungen", "forum", "kalender", "gehe zu seite", "aktuelle seite", "vorherige seite", "nächste seite", "teilnehmer", "mitglieder"}) {
		return false
	}
	if containsAny(hrefL, []string{"/login", "shibboleth", "logout", "cmd=edit", "cmd=delete", "target=file_", "downloadtablecontainer", "&anticache=", "-pager-", "mycourses", "membership", "/auth/home", "resource/courses", "resource/resources", "baseclass=ildashboardgui", "baseclass=ilmembershipoverviewgui"}) {
		return false
	}
	return containsAny(hrefL, []string{"target=fold_", "target=grp_", "target=crs_", "goto.php?target=fold_", "goto.php?target=grp_", "goto.php?target=crs_", "/coursenode/", "/repositoryentry/"})
}

func looksLikeSectionLink(href, text string) bool {
	hrefL := strings.ToLower(href)
	textL := strings.ToLower(text)
	if containsAny(textL, []string{"forum", "kalender", "neuigkeiten", "ankündigungen", "mitglieder", "teilnehmer", "bewertung", "statistik", "übersicht"}) {
		return false
	}
	if containsAny(hrefL, []string{"/login", "shibboleth", "logout", "mycourses", "membership", "/auth/home", "resource/courses", "resource/resources", "cmd=edit", "cmd=delete", "baseclass=ildashboardgui", "baseclass=ilmembershipoverviewgui", "-pager-", "downloadtablecontainer"}) {
		return false
	}
	return containsAny(hrefL, []string{"target=fold_", "target=grp_", "target=crs_", "goto.php?target=fold_", "goto.php?target=grp_", "goto.php?target=crs_", "/coursenode/", "/repositoryentry/"})
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
