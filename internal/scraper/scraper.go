package scraper

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/visitlog"
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
	// section listing *by navigating to a distinct URL*. downloadFileViaBrowser
	// falls back to navigating here when the link isn't present on SourceURL,
	// since files beyond the default ~20-item page cap only render on the
	// expanded page. Empty when the candidate was found on a section's normal
	// (non-expanded) first page, OR when the expansion happened via ShowAllViaClick
	// instead (see its doc comment for why those two cases aren't the same).
	ShowAllURL string
	// ShowAllViaClick marks a candidate that was only revealed by clicking a
	// "show all"/"Alle anzeigen" pagination control that expands the section
	// *in place* (no distinct URL to navigate back to - expandShowAllInSection's
	// `navigated` stays false, so ShowAllURL above is left empty for these).
	// Found live (queue task fix-html-response-download-fallback-failures,
	// 2026-07-17): a real course's "Vorlesung" section used exactly this
	// click-only expansion shape, and every one of its beyond-the-first-page
	// files (Kapitel1.pdf..Kapitel8.pdf) permanently failed both the fast-path
	// counter-refresh (a plain HTTP re-fetch of SourceURL can't run the click)
	// and the browser-fallback click (SourceURL alone, freshly loaded, never
	// re-triggers the expansion) - the exact "response is HTML, browser
	// fallback click did not find downloadable link" symptom, since there was
	// no ShowAllURL to retry and nothing re-clicked the control before
	// searching. clickCandidateLinkOnPage uses this flag to re-run
	// attemptShowAllExpandClick on SourceURL before giving up, mirroring what
	// expandShowAllInSection already does during discovery.
	ShowAllViaClick bool
	// ExpandedPageURL is the URL the browser was actually sitting on right
	// after a click-driven "show all" expansion rendered during discovery -
	// i.e. SourceURL plus OPAL/Wicket's page-instance counter, e.g.
	// ".../CourseNode/<id>/Vorlesung?1032" (observed live 2026-07-20). Wicket
	// keeps each rendered page instance addressable by that counter for the
	// life of the session, so re-requesting this URL can serve the
	// already-expanded listing, whereas re-requesting SourceURL always serves
	// the collapsed first page.
	//
	// Why it exists (queue task investigate-per-file-html-fallback-failures,
	// 2026-07-20): files past the first pagination page of a click-expanded
	// section are structurally invisible to download_refresh.go's counter-
	// refresh, because that refresh is a plain HTTP re-fetch of SourceURL and
	// a plain HTTP fetch cannot run the show-all click. Those files therefore
	// *always* fell through to the slow, serialized browser-click fallback -
	// live-confirmed for 6 of 36 files in one real course - where a single
	// flake produced the permanent "response is HTML, browser fallback click
	// did not find downloadable link" failure this task was filed for.
	// Recording the expanded page instance's URL gives both the counter-
	// refresh and the browser fallback a page that actually contains those
	// files' anchors.
	//
	// Strictly an *additional* retry target, never a replacement for
	// SourceURL: a page instance can be evicted from the session, in which
	// case this URL simply re-renders collapsed and the attempt falls through
	// to the pre-existing click-based path unchanged. Empty for candidates
	// whose section was not expanded, or was expanded by navigating to a
	// distinct ShowAllURL instead of by clicking.
	ExpandedPageURL string
}

type OpalScraper struct {
	opalURL       string
	stateFile     string
	developerMode bool

	// debugClicks enables the click/wait audit log (see audit.go). Same
	// set-once-before-scrape/read-only-afterward lifecycle as
	// developerMode, so it needs no locking of its own.
	debugClicks bool

	// debugLogMu guards debugLogFile - auditLog (audit.go) can be called
	// concurrently from several courses' crawl goroutines at once during a
	// course_concurrency>1 run (see collectCourseFilesConcurrently,
	// orchestrator.go), and concurrent unsynchronized writes to the same
	// *os.File would interleave/corrupt lines. See EnableDebugLogFile's doc
	// comment (audit.go) for why this file exists at all.
	debugLogMu   sync.Mutex
	debugLogFile *os.File

	// progressMu guards progressFn and progressStarted. Discovery is the long
	// phase of a sync (it walks every section of every course) and used to
	// report nothing at all until it had completely finished, so a caller had
	// no way to tell a healthy long crawl from a hang. progressFn lets a
	// caller observe it as it happens; see SetDiscoveryProgress.
	//
	// The mutex is not optional: with course_concurrency > 1 several course
	// crawl goroutines call publishProgress concurrently
	// (collectCourseFilesConcurrently, orchestrator.go), and the callback
	// ultimately drives a GUI event log. Serializing here means callers get
	// the same "events never interleave" guarantee the syncer's own progress
	// callback already provides, instead of every caller needing its own lock.
	progressMu      sync.Mutex
	progressFn      func(DiscoveryProgress)
	progressStarted int

	// courseConcurrency is the number of courses crawled concurrently during
	// discovery, each on its own browser tab/page (see
	// collectCourseFilesConcurrently in orchestrator.go). Like
	// developerMode, it is set once via SetCourseConcurrency before a scrape
	// begins and only read afterward, so it needs no locking of its own.
	// <= 0 means "use config.DefaultCourseConcurrency".
	courseConcurrency int

	// skipEnrollmentSections gates the structural (not title-text, not
	// visit-history) "Einschreibung" course-node skip in
	// appendSectionFolderTargets (crawl.go) - see isNonFileSectionType and
	// config.DefaultSkipEnrollmentSections's doc comments for the live
	// investigation behind it. Like courseConcurrency, it is set once via
	// SetSkipEnrollmentSections before a scrape begins and only read
	// afterward, so it needs no locking of its own. The zero value (false,
	// i.e. skipping disabled) is deliberately the safe default: a caller
	// that never calls the setter gets the pre-existing "visit every
	// discovered section" behavior rather than silently opting into the new
	// skip.
	skipEnrollmentSections bool

	// usedInteractiveLogin records whether the most recent ensureSession
	// call (session.go) had to fall through to the interactive-login
	// branch (saved session state missing or expired) rather than reusing
	// a still-valid saved session headlessly. Written exactly once per
	// ensureSession call, before any concurrent crawl work begins, and only
	// read afterward via UsedInteractiveLogin - same
	// set-once-before-scrape/read-only-afterward lifecycle as
	// developerMode/courseConcurrency above, so it needs no locking of its
	// own. Added for docs/scheduled-sync-plan.md section 3's "which login
	// path did this run take" instrumentation: cmd/opal-downloader's
	// runSync reads this after a `sync --scheduled` run to record it in the
	// scheduled-run status file (internal/statuslog).
	usedInteractiveLogin bool

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

	// fallbackPage remembers what the shared page currently shows, so
	// consecutive browser-fallback downloads from the same section skip the
	// navigation and the show-all re-expansion. Only touched while
	// browserDownloadMu is held. See fallback_page_memo.go.
	fallbackPage fallbackPageMemo

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

	// visitLogMu guards visitRecords. collectCourseFiles (crawl.go) records
	// one entry per successfully-visited section, and runs concurrently
	// across courses during discovery (see collectCourseFilesConcurrently in
	// orchestrator.go, each course on its own goroutine) - so appends here
	// need their own lock, independent of fieldMu above.
	visitLogMu sync.Mutex

	// visitRecords accumulates every section-visit observation made across
	// all scrapes run on this OpalScraper so far. It is purely in-memory
	// during a scrape; callers (cmd/opal-downloader/root.go's runList/
	// runSync) retrieve it via VisitRecords() once a scrape completes and
	// persist it into the cross-run visitlog.Log file that sits next to the
	// sync manifest. See internal/visitlog's package doc for why this is a
	// separate, cross-run concern from --debug-clicks (audit.go).
	visitRecords []visitlog.Record

	// newPageMu serializes ctx.NewPage() calls made by
	// newCourseFileCollector (orchestrator.go) during concurrent course
	// crawling - see newCourseFileCollector's doc comment for why this
	// exists: live A/B testing (queue task
	// fix-concurrent-crawl-ajax-race-and-raise-concurrency) found that a
	// worker finishing one course and immediately opening a fresh tab for
	// its next one, at the same moment one or more *other* workers are
	// doing the same (a common pattern once several short courses finish
	// around the same time under a fixed-size worker pool), produces a
	// short burst of simultaneous Chromium tab/renderer-process creation
	// that measurably worsens the AJAX-render race candidateStabilityPoll
	// (navigation.go) otherwise handles - a fix verified sufficient with an
	// isolated 3-course pool was live-reproduced to still lose files in the
	// real 8-course pool specifically around this kind of worker-handoff
	// churn. Serializing just the NewPage() call (not the crawl that
	// follows it) keeps tab creation itself from piling up, while still
	// letting already-open tabs render/crawl fully concurrently.
	newPageMu sync.Mutex
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

// recordSectionVisit appends one section-visit observation to the
// in-memory list accumulated during this scrape. Called from
// collectCourseFiles (crawl.go) once a section's Goto+extraction has
// actually succeeded (so this reflects a real visit, not a failed
// navigation attempt) - see VisitRecords for how a caller retrieves these.
func (s *OpalScraper) recordSectionVisit(course, sectionTitle, sectionURL string, filesFound int) {
	s.visitLogMu.Lock()
	defer s.visitLogMu.Unlock()
	s.visitRecords = append(s.visitRecords, visitlog.Record{
		Course:       course,
		SectionTitle: sectionTitle,
		SectionURL:   sectionURL,
		FilesFound:   filesFound,
		Timestamp:    time.Now(),
	})
}

// VisitRecords returns every section-visit record accumulated across all
// scrapes run on this OpalScraper so far (a copy, safe for the caller to
// keep/mutate independently of further scrapes). Callers persist these into
// the persistent cross-run visitlog.Log file via visitlog.Append - see
// cmd/opal-downloader/root.go's persistVisitLog.
func (s *OpalScraper) VisitRecords() []visitlog.Record {
	s.visitLogMu.Lock()
	defer s.visitLogMu.Unlock()
	out := make([]visitlog.Record, len(s.visitRecords))
	copy(out, s.visitRecords)
	return out
}

// SetCourseConcurrency sets the number of courses crawled concurrently
// during discovery. A value <= 0 falls back to
// config.DefaultCourseConcurrency at scrape time.
func (s *OpalScraper) SetCourseConcurrency(concurrency int) {
	s.courseConcurrency = concurrency
}

// SetSkipEnrollmentSections enables or disables the structural
// "Einschreibung" course-node skip (see skipEnrollmentSections's doc
// comment). Callers should set this from config.App.SkipEnrollmentSections
// (default true - see config.DefaultSkipEnrollmentSections) before
// scraping; not calling this at all leaves skipping disabled.
func (s *OpalScraper) SetSkipEnrollmentSections(enabled bool) {
	s.skipEnrollmentSections = enabled
}

// UsedInteractiveLogin reports whether the most recent ensureSession call
// (triggered by ScrapeWithSavedSession/LoginWithBrowser) had to fall
// through to the interactive-login branch instead of reusing a still-valid
// saved session headlessly. See usedInteractiveLogin's doc comment. Only
// meaningful after a scrape/login call has actually run; before that it
// reports the zero value (false).
func (s *OpalScraper) UsedInteractiveLogin() bool {
	return s.usedInteractiveLogin
}

func New(opalURL, stateFile string) *OpalScraper {
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
		downloadCandidates: map[string]downloadCandidate{},
	}
}

func (s *OpalScraper) LoginWithBrowser() error {
	return s.ensureSession(true)
}

// OpenInteractiveBrowserAt launches a visible Playwright Chromium window
// against the dedicated login profile (see launchBrowser/LoginProfileDir)
// and navigates it to url, without waiting for login or closing anything
// afterward. Used by the GUI's /tufast-setup page to open TU-Fast's Chrome
// Web Store listing for the user to install by hand - see
// gui.defaultLaunchBrowserAt. The caller must not call s.Close() right
// after this returns: unlike LoginWithBrowser, there is nothing to wait for
// here, so the browser window has to stay open on its own for the user to
// interact with (install the extension, log in) - it keeps running as a
// child process of this Go process (guarded by internal/procguard so it
// still dies if opal-downloader itself is killed) until the user closes the
// window or the whole process exits.
func (s *OpalScraper) OpenInteractiveBrowserAt(url string) error {
	if err := s.launchBrowser(false, false); err != nil {
		return err
	}
	page := s.getPage()
	if page == nil {
		return errors.New("failed to initialize browser page")
	}
	_, err := page.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
	return err
}

// ScrapeWithSavedSession logs in (via the saved session state) and crawls
// every matching course for its files. ctx is checked for cancellation
// before login/discovery starts and threaded through the per-course crawl
// loop (scrapeCoursesBrowser) so a caller (the GUI's /sync/cancel handler)
// can stop it from iterating further courses once the browser has been
// closed out from under it - see cancelFn's doc comment in
// internal/gui/sync.go. A cancelled ctx surfaces here as ctx.Err() (wrapped
// or returned as-is by scrapeCoursesBrowser), which callers can distinguish
// from a genuine scrape failure via errors.Is(err, context.Canceled).
func (s *OpalScraper) ScrapeWithSavedSession(ctx context.Context, courseFilter []string) ([]RemoteFile, error) {
	if len(courseFilter) == 0 {
		courseFilter = []string{"*"}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ensureSession(false); err != nil {
		return nil, err
	}
	return s.scrapeCoursesBrowser(ctx, courseFilter)
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
	_ = s.CloseDebugLogFile()

	pw := s.getPw()
	if pw == nil {
		return nil
	}
	s.setPw(nil)
	return pw.Stop()
}
