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

	pw      *playwright.Playwright
	browser playwright.Browser
	context playwright.BrowserContext
	page    playwright.Page

	// downloadCandidates is populated once, synchronously, during discovery
	// (crawl.go/files.go, before any download worker goroutines start) and is
	// only read afterward, concurrently, from download.go. That ordering is
	// what makes concurrent reads safe without a lock today - do not make
	// discovery lazy/interleaved with downloads without adding a lock (or
	// switching to a concurrent-safe map) first.
	downloadCandidates map[string]downloadCandidate

	// browserDownloadMu serializes the single-page browser-fallback download
	// path (downloadFileViaBrowser). s.page is a single shared Playwright page
	// and is not safe for concurrent navigation, so even though DownloadFile's
	// fast HTTP path can run from many goroutines at once (the underlying
	// APIRequestContext/connection is safe for concurrent use - see
	// playwright-go's connection.go, which dispatches calls via atomic IDs and
	// a sync.Map of callbacks), any request that falls back to driving the
	// browser must be serialized behind this mutex.
	browserDownloadMu sync.Mutex
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
