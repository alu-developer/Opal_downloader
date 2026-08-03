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
	"github.com/alu-developer/opal-downloader/internal/logging"
	"github.com/alu-developer/opal-downloader/internal/polite"
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

	// limiter is the ceiling on how fast this process is allowed to ask OPAL
	// for pages - see internal/polite and docs/server-load.md. Shared across
	// every tab, because the thing being bounded is load on someone else's
	// server, and OPAL cannot tell which of this program's browser tabs a
	// request came from.
	limiter *polite.Limiter

	// previewsBlocked/previewBytesSaved count what the inline-preview route
	// discarded (previews.go). Accessed with sync/atomic because the route
	// handler runs on Playwright's dispatch goroutine, not the crawl's.
	previewsBlocked   int64
	previewBytesSaved int64

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

	// sectionTiming records where each section's wall time goes, so the
	// sync-speed question can be answered with numbers instead of constants
	// read off the source. See sectiontiming.go.
	sectionTiming *sectionTiming

	// sectionProbe is a test-only hook for per-section (settle+stable,
	// candidate count) correlation; nil in production (Question 10, sync-speed-model.md).
	sectionProbe func(settle, stable time.Duration, candidates int)

	// showAllProbe is a test-only hook reporting what a "show all" expansion
	// actually did to a section's candidate list: the hrefs before the
	// expansion and the hrefs after it, not just the counts. Nil in
	// production (Question 18, sync-speed-model.md).
	//
	// Counts alone are what left Question 18 undiagnosed for two days.
	// warnShowAllTruncated has been reporting "17 rows before, 14 after" on
	// one node in every archived run since 2026-08-01, and a count going
	// *down* has at least two very different explanations - the expansion
	// dropped rows that were really there, or the collapsed list contained
	// non-file rows (the show-all control itself is one of them) that stop
	// matching once expanded. Those are indistinguishable without the hrefs,
	// and they need opposite fixes.
	showAllProbe func(sectionURL string, before, after []map[string]string)

	// stall records when the crawl last made progress and what it was doing,
	// so a run that wedges leaves behind the one thing that was missing the
	// time this actually happened: which section it was on. See stallwatch.go.
	stall *stallWatch

	// courseConcurrency is the number of courses crawled concurrently during
	// discovery, each on its own browser tab/page (see
	// collectCourseFilesConcurrently in orchestrator.go). Like
	// developerMode, it is set once via SetCourseConcurrency before a scrape
	// begins and only read afterward, so it needs no locking of its own.
	// <= 0 means "use config.DefaultCourseConcurrency".
	courseConcurrency int

	// sectionConcurrency is how many sections of a *single* course are visited
	// at once inside that course's BFS crawl, each on its own tab (see
	// sectionTabPool in section_pool.go). Set once via SetSectionConcurrency
	// before a scrape begins and only read afterward, like courseConcurrency.
	// <= 0 means "use config.DefaultSectionConcurrency"; 1 disables it.
	sectionConcurrency int

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

// SetSectionConcurrency sets how many sections of a single course are visited
// at once within that course's crawl. A value <= 0 falls back to
// config.DefaultSectionConcurrency; 1 disables section concurrency entirely
// and restores the original one-tab serial crawl.
func (s *OpalScraper) SetSectionConcurrency(concurrency int) {
	s.sectionConcurrency = concurrency
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
		limiter:            polite.New(polite.DefaultMinInterval),
		stall:              &stallWatch{},
		sectionTiming:      &sectionTiming{},
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
	_, err := s.gotoPolitely(page, url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
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
	// Serial-hybrid discovery (option A, docs/RESUME.md): the browser walks the
	// course-content tree to enumerate section URLs (the part OPAL renders
	// client-side), then HTTP bulk-fetches each section's leaf file table -
	// where the ~0.67s/section settle wait actually goes - skipping that wait
	// because there is no render to wait for over HTTP. HTTP and browser never
	// run concurrently (measured: that corrupts the shared Wicket session).
	//
	// OPAL_HTTP_DISCOVERY=verify: run BOTH the browser crawl and the HTTP
	// fetch, serially, and log a per-course diff. Returns the browser result
	// (the trusted source) so verification can't silently lose files.
	// OPAL_HTTP_DISCOVERY=1: return the HTTP result (verified diff=0 against
	// the browser on 2026-07-31 across all courses), with a guard that falls
	// back to the browser result if HTTP ever finds fewer files than the
	// browser in any course. Unset (the default): plain browser crawl.
	httpDiscoveryMode := os.Getenv("OPAL_HTTP_DISCOVERY")
	if httpDiscoveryMode == "verify" || httpDiscoveryMode == "1" {
		return s.scrapeCoursesHybrid(ctx, courseFilter, httpDiscoveryMode)
	}
	return s.scrapeCoursesBrowser(ctx, courseFilter)
}

// DiscoverCourseNames logs in via the saved session and returns the titles
// of every course on the user's OPAL dashboard, in dashboard order and
// de-duplicated.
//
// It deliberately stops after reading the dashboard rather than reusing
// ScrapeWithSavedSession: that walks every section of every course to build
// the full file list, which on a real account takes minutes. Course *names*
// are already fully known from the dashboard cards alone
// (discoverCourseLinks), so a picker that only needs names should not pay
// for the crawl. This is what makes "let me pick my courses from a list"
// viable as an interactive setup step instead of a coffee break.
func (s *OpalScraper) DiscoverCourseNames(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ensureSession(false); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	courses, err := s.discoverCourseLinks([]string{"*"})
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(courses))
	seen := map[string]struct{}{}
	for _, course := range courses {
		title := strings.TrimSpace(course.Title)
		if title == "" {
			continue
		}
		if _, dup := seen[title]; dup {
			continue
		}
		seen[title] = struct{}{}
		names = append(names, title)
	}
	return names, nil
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
	// Logged here rather than at the end of discovery, where it was first put.
	// The downloads run *after* discovery returns, so a line emitted there
	// reported the crawl's waits and none of the download workers' - which is
	// precisely backwards, since the downloads are the concurrent, byte-moving
	// requests the ceiling exists for. Close is the one point that sees a
	// whole run.
	s.LogRateLimitStats()
	s.reportBlockedPreviews()
	s.sectionTiming.log()

	_ = s.closeBrowser()
	_ = s.CloseDebugLogFile()

	pw := s.getPw()
	if pw == nil {
		return nil
	}
	s.setPw(nil)
	return pw.Stop()
}

// LogRateLimitStats records how much the politeness ceiling actually got in
// the way of a run, so the claim in docs/server-load.md - that it does not bind
// on today's work - is checkable against a real run rather than taken on trust.
//
// Called from Close, which is the only point that sees a whole run: discovery
// and then the downloads that follow it.
func (s *OpalScraper) LogRateLimitStats() {
	if s == nil || s.limiter == nil {
		return
	}
	waits, delayed, held := s.limiter.Stats()
	logging.Detail("rate ceiling: %d navigation(s), %d delayed, %s held in total", waits, delayed, held)
}

// getPolitely is the door every HTTP request that is not a page navigation
// goes through: the file downloads themselves, and the Wicket counter-refresh
// dance behind them.
//
// These were missed when the ceiling was first added, which had it bounding the
// wrong half of the load. Page navigations are cheap and serial; the downloads
// are the requests that run concurrently (download_concurrency, default 3) and
// the only ones that move real bytes. A ceiling that let those through was
// bounding the part nobody was worried about.
//
// One shared limiter with the navigations, not a second one of its own:
// "how hard does this tool hit OPAL" is one question, and OPAL cannot tell a
// page load from a file fetch.
func (s *OpalScraper) getPolitely(reqCtx playwright.APIRequestContext, url string, opts ...playwright.APIRequestContextGetOptions) (playwright.APIResponse, error) {
	if s != nil && s.limiter != nil {
		if err := s.limiter.Wait(context.Background()); err != nil {
			return nil, err
		}
	}

	resp, err := reqCtx.Get(url, opts...)

	if s != nil && s.limiter != nil {
		status := 0
		if resp != nil {
			status = resp.Status()
		}
		s.limiter.Observe(status)
		if level := s.limiter.BackoffLevel(); level > 0 && (status == 429 || status == 503) {
			logging.Warn("OPAL reported it is overloaded (HTTP %d) on a download - slowing down (backoff level %d of %d)", status, level, polite.MaxBackoffLevel)
		}
	}
	return resp, err
}

// gotoPolitely is the single door every navigation to OPAL goes through.
//
// It exists so that "how hard does this tool hit somebody else's server" has
// one answer in one place, rather than being an emergent property of fifteen
// call sites and whatever the concurrency settings happen to be. See
// internal/polite and docs/server-load.md.
//
// It also watches what comes back: a 429 or a 503 is OPAL saying it is
// struggling, and the limiter widens its spacing in response.
//
// A scraper built without a limiter (a zero-value struct in a test) simply
// navigates, so this is safe to call unconditionally.
func (s *OpalScraper) gotoPolitely(page playwright.Page, url string, opts playwright.PageGotoOptions) (playwright.Response, error) {
	if s != nil && s.limiter != nil {
		if err := s.limiter.Wait(context.Background()); err != nil {
			return nil, err
		}
	}

	resp, err := page.Goto(url, opts)

	if s != nil && s.limiter != nil {
		status := 0
		if resp != nil {
			status = resp.Status()
		}
		s.limiter.Observe(status)
		// Being throttled makes a sync look slow for a reason nobody can see
		// from the outside, so it goes on the record.
		if level := s.limiter.BackoffLevel(); level > 0 && (status == 429 || status == 503) {
			logging.Warn("OPAL reported it is overloaded (HTTP %d) - slowing down (backoff level %d of %d)", status, level, polite.MaxBackoffLevel)
		}
	}
	return resp, err
}
