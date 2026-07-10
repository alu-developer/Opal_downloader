package scraper

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

type RemoteFile struct {
	Name         string
	URL          string
	Course       string
	SectionTitle string
	Path         string
	Size         *int64
	Modified     *string
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

	// debugClicks enables the click/wait audit log (see audit.go). Same
	// set-once-before-scrape/read-only-afterward lifecycle as
	// developerMode, so it needs no locking of its own.
	debugClicks bool

	// courseConcurrency is the number of courses crawled concurrently during
	// discovery, each on its own browser tab/page (see
	// collectCourseFilesConcurrently in orchestrator.go). Like
	// developerMode, it is set once via SetCourseConcurrency before a scrape
	// begins and only read afterward, so it needs no locking of its own.
	// <= 0 means "use config.DefaultCourseConcurrency".
	courseConcurrency int

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

	// pageTrackingSuspended, when set, tells trackActivePage's ctx.OnPage
	// hook (session.go) to ignore newly opened pages instead of retargeting
	// s.page at them. It is set for the duration of concurrent course-file
	// collection (see suspendPageTracking/resumePageTracking, called from
	// orchestrator.go's scrapeCoursesBrowser): newCourseFileCollector opens
	// one throwaway page per course via ctx.NewPage() and Close()s it once
	// that course's crawl finishes, and ctx.OnPage fires for every page
	// opened in the context - not just login-flow tabs. Without suspending
	// tracking during that phase, s.page ends up pointing at whichever
	// course's crawl page was opened last, which is already closed by the
	// time downloads start, breaking every browser-fallback download with
	// "target closed" errors. It is an atomic.Bool (not fieldMu-guarded like
	// page/context/browser above) because it is independent of those fields
	// and is only ever toggled by the single goroutine driving a scrape, not
	// raced against Close().
	pageTrackingSuspended atomic.Bool
}

// suspendPageTracking stops trackActivePage's ctx.OnPage hook from
// retargeting s.page, for the duration of concurrent course-file collection.
// See pageTrackingSuspended's doc comment for why this is necessary.
func (s *OpalScraper) suspendPageTracking() {
	s.pageTrackingSuspended.Store(true)
}

// resumePageTracking re-enables trackActivePage's ctx.OnPage hook after
// concurrent course-file collection finishes, so it keeps working for any
// later interactive-login tab switching.
func (s *OpalScraper) resumePageTracking() {
	s.pageTrackingSuspended.Store(false)
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

// SetCourseConcurrency sets the number of courses crawled concurrently
// during discovery. A value <= 0 falls back to
// config.DefaultCourseConcurrency at scrape time.
func (s *OpalScraper) SetCourseConcurrency(concurrency int) {
	s.courseConcurrency = concurrency
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
