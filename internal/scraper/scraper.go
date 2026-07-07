package scraper

import (
	"strings"
	"sync"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
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
	browserProfileDir  string
	developerMode      bool

	// fieldMu guards pw/browser/context/page. The GUI's /sync/cancel handler
	// calls Close() from the HTTP-handler goroutine while runJob's goroutine
	// may still be reading/writing these same fields mid-scrape (see PR #22
	// review) - a genuine data race, not just a logical one. Close() is the
	// only interruption mechanism available (Playwright-go has no
	// context-aware cancellation for an in-flight call), so it must be able
	// to run concurrently with - and promptly interrupt - any other method;
	// it cannot simply wait for a long read-lock to be released. Instead,
	// every access to these four fields goes through the page()/context()/
	// browser()/pw() getters and the setPage()/setContext()/setBrowser()/
	// setPw() setters below, which take fieldMu only for the instant needed
	// to read or swap a pointer. Close() takes fieldMu the same way when it
	// nils the fields out and closes the previous values - so the fields
	// themselves never see a concurrent unsynchronized read/write, even
	// though a scrape call using the (now-stale) local copy of page/context
	// it captured just before Close() ran will simply get an error back from
	// Playwright once the underlying browser process is gone, which is
	// exactly the desired "cancelled" behavior.
	fieldMu sync.Mutex

	pw      *playwright.Playwright
	browser playwright.Browser
	context playwright.BrowserContext
	page    playwright.Page

	downloadCandidates map[string]downloadCandidate
}

func (s *OpalScraper) getPage() playwright.Page {
	s.fieldMu.Lock()
	defer s.fieldMu.Unlock()
	return s.page
}

func (s *OpalScraper) setPage(p playwright.Page) {
	s.fieldMu.Lock()
	defer s.fieldMu.Unlock()
	s.page = p
}

func (s *OpalScraper) getContext() playwright.BrowserContext {
	s.fieldMu.Lock()
	defer s.fieldMu.Unlock()
	return s.context
}

func (s *OpalScraper) setContext(c playwright.BrowserContext) {
	s.fieldMu.Lock()
	defer s.fieldMu.Unlock()
	s.context = c
}

func (s *OpalScraper) getBrowser() playwright.Browser {
	s.fieldMu.Lock()
	defer s.fieldMu.Unlock()
	return s.browser
}

func (s *OpalScraper) setBrowser(b playwright.Browser) {
	s.fieldMu.Lock()
	defer s.fieldMu.Unlock()
	s.browser = b
}

func (s *OpalScraper) getPw() *playwright.Playwright {
	s.fieldMu.Lock()
	defer s.fieldMu.Unlock()
	return s.pw
}

func (s *OpalScraper) setPw(pw *playwright.Playwright) {
	s.fieldMu.Lock()
	defer s.fieldMu.Unlock()
	s.pw = pw
}

func (s *OpalScraper) SetDeveloperMode(enabled bool) {
	s.developerMode = enabled
}

func New(opalURL, stateFile, browserExecutable, browserUserDataDir, browserProfileDir string) *OpalScraper {
	if opalURL == "" {
		opalURL = config.DefaultOPALURL
	}
	opalURL = strings.TrimRight(opalURL, "/") + "/"
	if stateFile == "" {
		stateFile = config.DefaultStateFile
	}

	browserUserDataDir, browserProfileDir = normalizePersistentProfileSettings(browserUserDataDir, browserProfileDir)

	return &OpalScraper{
		opalURL:            opalURL,
		stateFile:          stateFile,
		browserExecutable:  browserExecutable,
		browserUserDataDir: browserUserDataDir,
		browserProfileDir:  browserProfileDir,
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

// Close tears down the browser/Playwright process. It is safe to call
// concurrently with any other OpalScraper method, and safe to call more than
// once (including concurrently with itself): every field it touches is
// swapped out under fieldMu first, and the actual teardown of the captured
// values happens afterwards without holding the lock, so a concurrent
// in-flight scrape/login/download simply gets an error from Playwright the
// next time it touches the (now-closed) page/context rather than racing on
// the struct fields themselves. See the fieldMu doc comment above.
func (s *OpalScraper) Close() error {
	_ = s.closeBrowser()

	pw := s.getPw()
	if pw == nil {
		return nil
	}
	s.setPw(nil)
	return pw.Stop()
}
