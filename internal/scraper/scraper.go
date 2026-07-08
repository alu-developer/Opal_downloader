package scraper

import (
	"strings"

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
	// ShowAllURL is the "show all"/"Alle anzeigen"-expanded variant of SourceURL,
	// set only when this candidate was discovered after expanding a paginated
	// section listing. downloadFileViaBrowser falls back to navigating here when
	// the link isn't present on SourceURL, since files beyond the default ~20-item
	// page cap only render on the expanded page. Empty when the candidate was
	// found on a section's normal (non-expanded) first page.
	ShowAllURL string
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

	downloadCandidates map[string]downloadCandidate
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
