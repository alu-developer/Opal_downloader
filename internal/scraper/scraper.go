package scraper

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/playwright-community/playwright-go"
)

type RemoteFile struct {
	Name     string
	URL      string
	Course   string
	Path     string
	Size     *int64
	Modified *string
}

type downloadCandidate struct {
	SourceURL  string
	LinkText   string
	LinkTarget string
}

type OpalScraper struct {
	opalURL            string
	stateFile          string
	browserExecutable  string
	browserUserDataDir string

	pw      *playwright.Playwright
	browser playwright.Browser
	context playwright.BrowserContext
	page    playwright.Page

	downloadCandidates map[string]downloadCandidate
}

func New(opalURL, stateFile, browserExecutable, browserUserDataDir string) *OpalScraper {
	if opalURL == "" {
		opalURL = config.DefaultOPALURL
	}
	opalURL = strings.TrimRight(opalURL, "/") + "/"
	if stateFile == "" {
		stateFile = config.DefaultStateFile
	}

	return &OpalScraper{
		opalURL:            opalURL,
		stateFile:          stateFile,
		browserExecutable:  browserExecutable,
		browserUserDataDir: browserUserDataDir,
		downloadCandidates: map[string]downloadCandidate{},
	}
}

func (s *OpalScraper) LoginWithBrowser() error {
	return s.ensureSession(true)
}

func (s *OpalScraper) ScrapeWithSavedSession(courseFilter []string) ([]RemoteFile, error) {
	if len(courseFilter) == 0 {
		courseFilter = []string{"*"}
	}
	if err := s.ensureSession(false); err != nil {
		return nil, err
	}
	return s.scrapeCoursesBrowser(courseFilter)
}

func (s *OpalScraper) Close() error {
	_ = s.closeBrowser()
	if s.pw != nil {
		if err := s.pw.Stop(); err != nil {
			return err
		}
		s.pw = nil
	}
	return nil
}

func (s *OpalScraper) DownloadFile(fileURL, localPath string) error {
	if s.context == nil {
		return errors.New("no authenticated browser context available")
	}

	response, err := s.context.Request().Get(fileURL)
	if err == nil && response != nil && response.Status() == 200 {
		headers := response.Headers()
		contentType := strings.ToLower(headers["content-type"])
		body, bodyErr := response.Body()
		if bodyErr == nil && !strings.Contains(contentType, "text/html") {
			if strings.HasSuffix(strings.ToLower(localPath), ".pdf") && !strings.HasPrefix(string(body), "%PDF") {
				return fmt.Errorf("downloaded payload is not a valid PDF")
			}
			return os.WriteFile(localPath, body, 0o644)
		}
	}

	return s.downloadFileViaBrowser(fileURL, localPath)
}

func (s *OpalScraper) launchBrowser(headless, useSavedState bool) error {
	if s.pw == nil {
		pw, err := playwright.Run()
		if err != nil {
			return err
		}
		s.pw = pw
	}

	if s.browserUserDataDir != "" && !headless && !useSavedState {
		opts := playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless: playwright.Bool(headless),
		}
		if s.browserExecutable != "" {
			opts.ExecutablePath = playwright.String(s.browserExecutable)
		}
		ctx, err := s.pw.Chromium.LaunchPersistentContext(s.browserUserDataDir, opts)
		if err != nil {
			return err
		}
		s.context = ctx
		pages := ctx.Pages()
		if len(pages) > 0 {
			s.page = pages[0]
		} else {
			p, pErr := ctx.NewPage()
			if pErr != nil {
				return pErr
			}
			s.page = p
		}
		return nil
	}

	launchOpts := playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(headless)}
	if s.browserExecutable != "" {
		launchOpts.ExecutablePath = playwright.String(s.browserExecutable)
	}
	browser, err := s.pw.Chromium.Launch(launchOpts)
	if err != nil {
		return err
	}
	s.browser = browser

	ctxOpts := playwright.BrowserNewContextOptions{}
	if useSavedState {
		if _, err := os.Stat(s.stateFile); err == nil {
			ctxOpts.StorageStatePath = playwright.String(s.stateFile)
		}
	}
	ctx, err := browser.NewContext(ctxOpts)
	if err != nil {
		return err
	}
	s.context = ctx
	page, err := ctx.NewPage()
	if err != nil {
		return err
	}
	s.page = page
	s.page.SetDefaultTimeout(15000)
	s.page.SetDefaultNavigationTimeout(20000)
	return nil
}

func (s *OpalScraper) closeOpalPages() {
	if s.context == nil {
		return
	}
	for _, p := range s.context.Pages() {
		pageURL := strings.ToLower(p.URL())
		if strings.Contains(pageURL, "bildungsportal.sachsen.de/opal") || pageURL == "about:blank" {
			_ = p.Close()
		}
	}
}

func (s *OpalScraper) closeBrowser() error {
	s.closeOpalPages()
	s.page = nil
	if s.context != nil {
		_ = s.context.Close()
		s.context = nil
	}
	if s.browser != nil {
		_ = s.browser.Close()
		s.browser = nil
	}
	return nil
}

func (s *OpalScraper) saveState() error {
	if s.context == nil {
		return errors.New("no browser context available")
	}
	if err := os.MkdirAll(filepath.Dir(s.stateFile), 0o755); err != nil {
		return err
	}
	_, err := s.context.StorageState(s.stateFile)
	if err != nil {
		return err
	}
	fmt.Printf("Saved session state to: %s\n", s.stateFile)
	return nil
}

func (s *OpalScraper) isAuthenticated() (bool, error) {
	if s.page == nil {
		return false, nil
	}
	_, err := s.page.Goto(s.opalURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
	if err != nil {
		return false, err
	}
	pageURL := strings.ToLower(s.page.URL())
	if strings.Contains(pageURL, "login") || strings.Contains(pageURL, "shib") || strings.Contains(pageURL, "idp") {
		return false, nil
	}
	passwordCount, err := s.page.Locator("input[type='password']").Count()
	if err != nil {
		return false, err
	}
	if passwordCount > 0 {
		return false, nil
	}
	courseCandidates, err := s.page.Locator("a[href*='crs_'], a[href*='course'], a[href*='RepositoryEntry']").Count()
	if err != nil {
		return false, err
	}
	return courseCandidates > 0, nil
}

func (s *OpalScraper) ensureSession(forceInteractive bool) error {
	_ = s.closeBrowser()

	if !forceInteractive {
		if _, err := os.Stat(s.stateFile); err == nil {
			if err := s.launchBrowser(true, true); err == nil {
				auth, authErr := s.isAuthenticated()
				if authErr == nil && auth {
					fmt.Println("Using saved OPAL session state")
					return nil
				}
			}
			fmt.Println("Saved session state expired. Interactive login required.")
			_ = s.closeBrowser()
		}
	}

	if err := s.launchBrowser(false, false); err != nil {
		return err
	}
	if s.page == nil {
		return errors.New("failed to initialize browser page")
	}

	fmt.Printf("Opening OPAL at %s\n", s.opalURL)
	fmt.Println("Please complete login in the opened browser window (TU-Fast/2FA supported).")
	_, err := s.page.Goto(s.opalURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
	if err != nil {
		return err
	}
	_, err = s.page.WaitForSelector("a[href*='crs_'], a[href*='course'], a[href*='RepositoryEntry']", playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(300000)})
	if err != nil {
		return err
	}
	return s.saveState()
}

func (s *OpalScraper) scrapeCoursesBrowser(courseFilter []string) ([]RemoteFile, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	courses, err := s.discoverCourseLinks(courseFilter, 12)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Found %d course links\n", len(courses))

	files := make([]RemoteFile, 0)
	for _, course := range courses {
		courseName := course[0]
		courseURL := course[1]
		if !config.CourseMatches(courseName, courseFilter) {
			fmt.Printf("  Skipped (filter): %s\n", courseName)
			continue
		}

		fmt.Printf("  Processing: %s\n", courseName)
		courseFiles, crawlErr := s.crawlCourseFiles(courseName, courseURL)
		if crawlErr != nil {
			fmt.Printf("  Error processing course: %v\n", crawlErr)
			continue
		}
		files = append(files, courseFiles...)
	}

	return files, nil
}

func (s *OpalScraper) discoverCourseLinks(courseFilter []string, maxPages int) ([][2]string, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	discovered := map[string][2]string{}
	sourcePages := []string{
		resolveURL(s.opalURL, "auth/home"),
		resolveURL(s.opalURL, "auth/RepositoryEntry/resources"),
		resolveURL(s.opalURL, "auth/resource/resources"),
	}

	for _, sourceURL := range sourcePages {
		_, err := s.page.Goto(sourceURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)})
		if err != nil {
			continue
		}
		s.waitForCourseEntries(sourceURL)

		candidates, err := s.extractCourseCandidatesFromCurrentPage()
		if err != nil {
			continue
		}

		for _, candidate := range candidates {
			linkTarget := extractLinkTarget(candidate["href"], candidate["onclick"], candidate["dataHref"], candidate["dataUrl"])
			if linkTarget == "" {
				continue
			}
			absURL := resolveURL(s.opalURL, linkTarget)
			repoID := extractRepositoryEntryID(absURL)
			if repoID == "" {
				continue
			}
			if !looksLikeCourseCandidate(candidate) {
				continue
			}

			label := deriveCourseLabel(candidate["title"], candidate["text"], candidate["cardText"], repoID)
			canonicalURL := resolveURL(s.opalURL, "auth/RepositoryEntry/"+repoID)
			previous, ok := discovered[repoID]
			if !ok || strings.HasPrefix(previous[0], "RepositoryEntry") || len(label) > len(previous[0]) {
				discovered[repoID] = [2]string{label, canonicalURL}
			}
		}
	}

	missingPatterns := getUnmatchedPatterns(courseFilter, mapValues(discovered))
	if len(missingPatterns) > 0 {
		fmt.Printf("Course discovery fallback: searching catalog for %d unmatched pattern(s)\n", len(missingPatterns))
		extra, _ := s.discoverFromCatalogForPatterns(missingPatterns, maxPages)
		for repoID, pair := range extra {
			if previous, ok := discovered[repoID]; !ok || strings.HasPrefix(previous[0], "RepositoryEntry") {
				discovered[repoID] = pair
			}
		}
	}

	items := mapValues(discovered)
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i][0]) < strings.ToLower(items[j][0]) })
	return items, nil
}

func (s *OpalScraper) discoverFromCatalogForPatterns(missingPatterns []string, maxPages int) (map[string][2]string, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	discovered := map[string][2]string{}
	visited := map[string]struct{}{}
	queue := []string{
		resolveURL(s.opalURL, "auth/repository/catalog"),
		resolveURL(s.opalURL, "auth/repository/catalog/3571713"),
	}
	queued := map[string]struct{}{}
	for _, item := range queue {
		queued[normalizeURLForCrawl(item)] = struct{}{}
	}

	for len(queue) > 0 && len(visited) < maxPages {
		currentURL := queue[0]
		queue = queue[1:]
		key := normalizeURLForCrawl(currentURL)
		delete(queued, key)
		if _, ok := visited[key]; ok {
			continue
		}
		visited[key] = struct{}{}

		_, err := s.page.Goto(currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)})
		if err != nil {
			continue
		}
		s.waitForCourseEntries(currentURL)

		candidates, err := s.extractCourseCandidatesFromCurrentPage()
		if err == nil {
			for _, candidate := range candidates {
				linkTarget := extractLinkTarget(candidate["href"], candidate["onclick"], candidate["dataHref"], candidate["dataUrl"])
				if linkTarget == "" {
					continue
				}
				absURL := resolveURL(s.opalURL, linkTarget)
				repoID := extractRepositoryEntryID(absURL)
				if repoID == "" || !looksLikeCourseCandidate(candidate) {
					continue
				}
				label := deriveCourseLabel(candidate["title"], candidate["text"], candidate["cardText"], repoID)
				if !config.CourseMatches(label, missingPatterns) {
					continue
				}
				canonicalURL := resolveURL(s.opalURL, "auth/RepositoryEntry/"+repoID)
				discovered[repoID] = [2]string{label, canonicalURL}
			}
		}

		value, evalErr := s.page.Evaluate(`() => Array.from(document.querySelectorAll('a[href]'))
                .map(a => (a.getAttribute('href') || '').trim())
                .filter(href => href.includes('/auth/repository/catalog/'))`)
		if evalErr == nil {
			for _, href := range toStringSlice(value) {
				absURL := resolveURL(s.opalURL, href)
				absKey := normalizeURLForCrawl(absURL)
				if _, seen := visited[absKey]; seen {
					continue
				}
				if _, pending := queued[absKey]; pending {
					continue
				}
				queue = append(queue, absURL)
				queued[absKey] = struct{}{}
			}
		}

		if len(getUnmatchedPatterns(missingPatterns, mapValues(discovered))) == 0 {
			break
		}
	}

	return discovered, nil
}

func (s *OpalScraper) extractCourseCandidatesFromCurrentPage() ([]map[string]string, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}
	value, err := s.page.Evaluate(`() => {
                const out = [];
                for (const el of document.querySelectorAll('a[href], [onclick], [data-href], [data-url]')) {
                    const card = el.closest('li, article, .list-group-item, .dynamic-tab, .o_repository_entry, .o_repoentry, .o_infoPanel, .o_card') || el.parentElement;
                    out.push({
                        href: (el.getAttribute('href') || '').trim(),
                        onclick: (el.getAttribute('onclick') || '').trim(),
                        dataHref: (el.getAttribute('data-href') || '').trim(),
                        dataUrl: (el.getAttribute('data-url') || '').trim(),
                        text: (el.textContent || '').trim(),
                        title: (el.getAttribute('title') || '').trim(),
                        cardClass: ((card && card.getAttribute('class')) || '').trim(),
                        cardText: ((card && card.textContent) || '').trim(),
                    });
                }
                return out;
            }`)
	if err != nil {
		return nil, err
	}
	return toStringMapSlice(value), nil
}

func (s *OpalScraper) waitForCourseEntries(pageURL string) {
	if s.page == nil {
		return
	}
	selectors := []string{
		"a[href*='/RepositoryEntry/']",
		"a[href*='/CourseNode/']",
		".list-group-item a[href]",
		".dynamic-tab a[href]",
	}

	isResourcesLike := strings.Contains(strings.ToLower(pageURL), "repositoryentry/resources") || strings.Contains(strings.ToLower(pageURL), "/auth/home") || strings.Contains(strings.ToLower(pageURL), "resource/resources")
	timeoutMs := 5000.0
	if isResourcesLike {
		timeoutMs = 12000
	}

	for _, selector := range selectors {
		if _, err := s.page.WaitForSelector(selector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(timeoutMs)}); err == nil {
			s.page.WaitForTimeout(800)
			return
		}
	}
	s.page.WaitForTimeout(1500)
}

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

func (s *OpalScraper) downloadFileViaBrowser(fileURL, localPath string) error {
	if s.page == nil {
		return errors.New("no browser page available for download fallback")
	}

	download, err := s.page.ExpectDownload(func() error {
		_, navErr := s.page.Goto(fileURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)})
		return navErr
	}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
	if err == nil {
		return download.SaveAs(localPath)
	}

	candidate, ok := s.downloadCandidates[fileURL]
	if !ok {
		return errors.New("response is HTML, not a direct file download")
	}

	if _, err := s.page.Goto(candidate.SourceURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)}); err != nil {
		return err
	}

	targetFragment := strings.TrimSpace(candidate.LinkTarget)
	if strings.HasPrefix(targetFragment, "http") {
		parsed, parseErr := url.Parse(targetFragment)
		if parseErr == nil {
			targetFragment = parsed.Path
		}
	}

	if targetFragment != "" {
		selector := fmt.Sprintf("a[href*='%s']", strings.ReplaceAll(targetFragment, "'", "\\'"))
		download, clickErr := s.page.ExpectDownload(func() error {
			return s.page.Locator(selector).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)})
		}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
		if clickErr == nil {
			return download.SaveAs(localPath)
		}
	}

	if candidate.LinkText != "" {
		download, clickErr := s.page.ExpectDownload(func() error {
			return s.page.GetByText(candidate.LinkText, playwright.PageGetByTextOptions{Exact: playwright.Bool(false)}).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)})
		}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
		if clickErr == nil {
			return download.SaveAs(localPath)
		}
	}

	return errors.New("response is HTML, browser fallback click did not find downloadable link")
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

func deriveCourseLabel(title, text, cardText, repoID string) string {
	for _, raw := range []string{title, text, cardText} {
		cleaned := strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(raw, " "))
		if cleaned == "" {
			continue
		}
		cleaned = regexp.MustCompile(`(?i)^Als Favorit markieren\s+`).ReplaceAllString(cleaned, "")
		cleaned = regexp.MustCompile(`(?i)\bVerantwortliche\(r\):.*$`).ReplaceAllString(cleaned, "")
		cleaned = regexp.MustCompile(`(?i)\bZuletzt angesehen:.*$`).ReplaceAllString(cleaned, "")
		cleaned = regexp.MustCompile(`(?i)\bAufrufe:.*$`).ReplaceAllString(cleaned, "")
		cleaned = strings.Trim(cleaned, " -,")
		if isCourseLabel(cleaned) {
			return cleaned
		}
	}
	return "RepositoryEntry " + repoID
}

func pickLabel(titleText, visibleText string) string {
	if len(titleText) > len(visibleText) {
		return titleText
	}
	return visibleText
}

func extractLinkTarget(href, onclick, dataHref, dataURL string) string {
	for _, candidate := range []string{href, dataHref, dataURL} {
		c := strings.TrimSpace(candidate)
		if c != "" && c != "#" && !strings.HasPrefix(strings.ToLower(c), "javascript:") {
			return c
		}
	}
	if onclick != "" {
		re1 := regexp.MustCompile(`(?:location\.href|window\.open)\s*\(\s*['\"]([^'\"]+)['\"]`)
		if match := re1.FindStringSubmatch(onclick); len(match) > 1 {
			return match[1]
		}
		re2 := regexp.MustCompile(`location\.href\s*=\s*['\"]([^'\"]+)['\"]`)
		if match := re2.FindStringSubmatch(onclick); len(match) > 1 {
			return match[1]
		}
		if strings.Contains(onclick, "goto.php") {
			re3 := regexp.MustCompile(`(/[^'\"]*goto\.php[^'\"]*)`)
			if match := re3.FindStringSubmatch(onclick); len(match) > 1 {
				return match[1]
			}
		}
	}
	return ""
}

func sanitizeFilename(name string) string {
	cleaned := strings.TrimSpace(name)
	invalid := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	cleaned = invalid.ReplaceAllString(cleaned, "_")
	cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimRight(cleaned, ". ")
	if cleaned == "" {
		cleaned = "unnamed"
	}
	upper := strings.ToUpper(cleaned)
	reserved := map[string]struct{}{"CON": {}, "PRN": {}, "AUX": {}, "NUL": {}, "COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {}, "COM6": {}, "COM7": {}, "COM8": {}, "COM9": {}, "LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {}, "LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {}}
	if _, ok := reserved[upper]; ok {
		cleaned = "_" + cleaned
	}
	return cleaned
}

func extractRepositoryEntryID(rawURL string) string {
	re := regexp.MustCompile(`(?i)/RepositoryEntry/(\d+)`)
	if match := re.FindStringSubmatch(rawURL); len(match) > 1 {
		return match[1]
	}
	return ""
}

func normalizeURLForCrawl(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := u.Query()
	keep := map[string]struct{}{"target": {}, "ref_id": {}, "cmd": {}, "cmdClass": {}, "baseClass": {}, "cmdNode": {}, "node_id": {}, "crs_next_sess": {}}
	filtered := url.Values{}
	for key, values := range query {
		if _, ok := keep[key]; !ok {
			continue
		}
		for _, value := range values {
			filtered.Add(key, value)
		}
	}
	u.RawQuery = filtered.Encode()
	u.Fragment = ""
	return u.String()
}

func getUnmatchedPatterns(patterns []string, courses [][2]string) []string {
	if len(patterns) == 0 || (len(patterns) == 1 && patterns[0] == "*") {
		return nil
	}
	courseNames := make([]string, 0, len(courses))
	for _, course := range courses {
		courseNames = append(courseNames, course[0])
	}
	unmatched := make([]string, 0)
	for _, pattern := range patterns {
		matched := false
		for _, name := range courseNames {
			if config.CourseMatches(name, []string{pattern}) {
				matched = true
				break
			}
		}
		if !matched {
			unmatched = append(unmatched, pattern)
		}
	}
	return unmatched
}

func resolveURL(baseURL, href string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return href
	}
	rel, err := url.Parse(href)
	if err != nil {
		return href
	}
	return base.ResolveReference(rel).String()
}

func toStringSlice(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func toStringMapSlice(value interface{}) []map[string]string {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]map[string]string, 0, len(items))
	for _, item := range items {
		mapped, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		entry := map[string]string{}
		for key, raw := range mapped {
			entry[key] = fmt.Sprintf("%v", raw)
			if entry[key] == "<nil>" {
				entry[key] = ""
			}
		}
		result = append(result, entry)
	}
	return result
}

func mapValues[K comparable, V any](m map[K]V) []V {
	result := make([]V, 0, len(m))
	for _, value := range m {
		result = append(result, value)
	}
	return result
}

func defaultString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
