package scraper

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alu-developer/opal-downloader/internal/logging"
	"github.com/mxschmitt/playwright-go"
)

// EnsurePlaywrightBrowsersPath defaults PLAYWRIGHT_BROWSERS_PATH to a
// directory under the user's home (~/.opal-downloader/ms-playwright)
// instead of leaving playwright-go to fall back to its own default
// (%LOCALAPPDATA%/ms-playwright on Windows). Found 2026-07-13: on at least
// one machine, %LOCALAPPDATA%/ms-playwright had silently become an NTFS
// junction into an unrelated packaged app's private storage (created by
// that app's own sandboxing, not by opal-downloader or the user), and
// launching chrome.exe through that junction failed with "the application
// has failed to start because its side-by-side configuration is
// incorrect" - an identical copy of the same files launched fine from a
// plain directory. Installing/launching against a path under the user's
// home directory instead avoids depending on %LOCALAPPDATA% ever staying a
// normal (non-redirected) directory. Only sets the var if the user hasn't
// already set one, so an explicit PLAYWRIGHT_BROWSERS_PATH always wins.
func EnsurePlaywrightBrowsersPath() {
	if os.Getenv("PLAYWRIGHT_BROWSERS_PATH") != "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	_ = os.Setenv("PLAYWRIGHT_BROWSERS_PATH", filepath.Join(home, ".opal-downloader", "ms-playwright"))
}

func (s *OpalScraper) launchBrowser(headless, useSavedState bool) error {
	if s.getPw() == nil {
		EnsurePlaywrightBrowsersPath()
		pw, err := playwright.Run()
		if err != nil {
			return err
		}
		s.setPw(pw)
	}

	if !headless && !useSavedState {
		// Interactive login (or an expired-saved-session fallback that
		// lands here) always launches Playwright's bundled Chromium as a
		// persistent context against the single, hardcoded dedicated login
		// profile (~/.opal-downloader/login-profile, see LoginProfileDir),
		// with extensions enabled so TU-Fast can be installed once and
		// auto-complete every login after that.
		//
		// This used to be conditional on a configurable browser_user_data_dir
		// pointing at the user's real installed Brave/Chrome profile, with a
		// bundled-Chromium fallback when that wasn't set. That "point
		// opal-downloader at your real browser" option has been removed
		// entirely (queue task chromium-only-login-remove-real-browser, per
		// the maintainer's explicit decision) - Chromium (Playwright's
		// bundled build) is now the only browser opal-downloader ever
		// launches, always against this one dedicated profile, never a real
		// installed browser executable.
		profileDir, err := LoginProfileDir()
		if err != nil {
			return err
		}

		// isUserDataDirLocked still matters here even though the profile is
		// no longer a real everyday browser: it protects against two
		// opal-downloader processes (e.g. a manual `login` run and a GUI
		// job) both trying to launch a persistent context against the same
		// dedicated profile directory at once, which Chromium's own
		// ProcessSingleton would otherwise reject in a much less legible way.
		locked, err := isUserDataDirLocked(profileDir)
		if err != nil {
			return err
		}
		if locked {
			return fmt.Errorf("%w: %s appears to be in use by another opal-downloader process right now - close it (or wait for it to finish) before running login/sync here", ErrProfileLocked, profileDir)
		}

		opts := playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless: playwright.Bool(headless),
			IgnoreDefaultArgs: []string{
				"--disable-extensions",
				"--disable-component-extensions-with-background-pages",
			},
		}
		opts.Args = append(opts.Args, "--enable-extensions")
		ctx, err := s.getPw().Chromium.LaunchPersistentContext(profileDir, opts)
		if err != nil {
			return fmt.Errorf("launching browser with profile %s: %w", profileDir, err)
		}
		logging.Detail("Launching persistent browser profile: userDataDir=%s", profileDir)
		s.setContext(ctx)
		s.blockInlineFilePreviews(ctx)
		s.trackActivePage(ctx)
		pages := ctx.Pages()
		var page playwright.Page
		if len(pages) > 0 {
			page = pages[0]
		} else {
			p, pErr := ctx.NewPage()
			if pErr != nil {
				return pErr
			}
			page = p
		}
		page.SetDefaultTimeout(15000)
		page.SetDefaultNavigationTimeout(20000)
		s.setPage(page)
		return nil
	}

	// headless+useSavedState (session-reuse for sync/list once a saved
	// session exists) deliberately does NOT touch the persistent login
	// profile above at all: it launches a fresh, anonymous Chromium instance
	// (Playwright's bundled build - there is no browserExecutable concept
	// anymore, ExecutablePath is never set anywhere) and, when useSavedState
	// is true, loads cookies from the saved storage-state JSON file instead
	// of any profile directory.
	//
	// This is intentional, not a leftover from the pre-Chromium-only design:
	// queue task chromium-only-login-remove-real-browser explicitly
	// investigated whether this branch could also collapse into the
	// persistent-context branch above now that there's only one profile
	// concept, and concluded no. Two (or more) concurrent headless sync/list
	// runs - a real scenario: the GUI's own background job plus a
	// manually-run CLI `list`/`sync`, or two scripted cron invocations
	// overlapping - each launching a persistent context against the SAME
	// login-profile directory would collide on isUserDataDirLocked/
	// Chromium's own ProcessSingleton exactly the way two interactive logins
	// would, which this anonymous-context path simply never risks today
	// (every headless launch gets its own throwaway browser instance with no
	// shared user-data-dir). Collapsing this branch too would trade away a
	// currently-safe "concurrent headless runs just work" property for no
	// actual benefit: extensions/TU-Fast are never needed on this path
	// anyway, since a still-valid saved session's cookies already carry the
	// authenticated state.
	launchOpts := playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(headless)}
	browser, err := s.getPw().Chromium.Launch(launchOpts)
	if err != nil {
		return err
	}
	s.setBrowser(browser)

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
	s.setContext(ctx)
	s.blockInlineFilePreviews(ctx)
	s.trackActivePage(ctx)
	page, err := ctx.NewPage()
	if err != nil {
		return err
	}
	page.SetDefaultTimeout(15000)
	page.SetDefaultNavigationTimeout(20000)
	s.setPage(page)
	return nil
}

// trackActivePage keeps s.page pointed at whatever page is currently open in
// ctx. During interactive login, TU-Fast (or the Shibboleth IdP redirect it
// drives) sometimes opens a new tab/window for the auth flow and closes the
// original one; without this, s.page keeps referencing the closed page and
// any later call on it (Goto/WaitForSelector) fails with "target closed"
// even though the login flow is proceeding fine in the new tab.
//
// ctx.OnPage fires for every page opened in the context, not just login-flow
// tabs - including the one-page-per-course tabs that
// collectCourseFilesConcurrently/newCourseFileCollector (orchestrator.go)
// open and close during concurrent course crawling. Retargeting s.page at
// those would leave it pointing at an already-closed crawl page once
// discovery finishes, breaking every subsequent browser-fallback download
// with "target closed" errors. So this hook is a no-op while
// s.pageTrackingSuspended is set (see suspendPageTracking/
// resumePageTracking in scraper.go, toggled around the concurrent crawl in
// orchestrator.go's scrapeCoursesBrowser).
func (s *OpalScraper) trackActivePage(ctx playwright.BrowserContext) {
	ctx.OnPage(s.handleContextPageOpened)
}

// handleContextPageOpened is trackActivePage's ctx.OnPage callback, split out
// as its own method so it can be exercised directly in tests without needing
// a real playwright.BrowserContext to fire the event (see session_test.go).
func (s *OpalScraper) handleContextPageOpened(p playwright.Page) {
	if s.pageTrackingSuspended.Load() {
		return
	}
	p.SetDefaultTimeout(15000)
	p.SetDefaultNavigationTimeout(20000)
	s.setPage(p)
}

func (s *OpalScraper) closeOpalPages() {
	ctx := s.getContext()
	if ctx == nil {
		return
	}
	for _, p := range ctx.Pages() {
		pageURL := strings.ToLower(p.URL())
		if strings.Contains(pageURL, "bildungsportal.sachsen.de/opal") || pageURL == "about:blank" {
			_ = p.Close()
		}
	}
}

// closeBrowser tears down the page/context/browser. It is safe to call
// concurrently with itself or with any in-flight scrape/login/download call:
// each field is atomically read-and-cleared via the locked setters below
// before the captured value is closed, so a concurrent caller either sees
// the field already nil or gets back the (still valid to close once) old
// value - never a torn/racing read of the field itself. See fieldMu's doc
// comment on OpalScraper.
func (s *OpalScraper) closeBrowser() error {
	s.closeOpalPages()
	s.setPage(nil)

	if ctx := s.getContext(); ctx != nil {
		s.setContext(nil)
		_ = ctx.Close()
	}
	if browser := s.getBrowser(); browser != nil {
		s.setBrowser(nil)
		_ = browser.Close()
	}
	return nil
}

func (s *OpalScraper) saveState() error {
	ctx := s.getContext()
	if ctx == nil {
		return errors.New("no browser context available")
	}
	if err := os.MkdirAll(filepath.Dir(s.stateFile), 0o755); err != nil {
		return err
	}
	_, err := ctx.StorageState(playwright.BrowserContextStorageStateOptions{Path: playwright.String(s.stateFile)})
	if err != nil {
		return err
	}
	logging.Detail("Saved session state to: %s", s.stateFile)
	return nil
}

func (s *OpalScraper) isAuthenticated() (bool, error) {
	page := s.getPage()
	if page == nil {
		return false, nil
	}
	_, err := s.gotoPolitely(page, s.opalURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
	if err != nil {
		return false, fmt.Errorf("could not reach OPAL at %s - check your internet connection and opal_url in config.yaml: %w", s.opalURL, err)
	}
	pageURL := strings.ToLower(page.URL())
	if strings.Contains(pageURL, "login") || strings.Contains(pageURL, "shib") || strings.Contains(pageURL, "idp") {
		return false, nil
	}
	passwordCount, err := page.Locator("input[type='password']").Count()
	if err != nil {
		return false, err
	}
	if passwordCount > 0 {
		return false, nil
	}
	courseCandidates, err := page.Locator(courseLinkSelector).Count()
	if err != nil {
		return false, err
	}
	return courseCandidates > 0, nil
}

// sessionLockAcquireTimeout bounds how long ensureSession will wait for
// another opal-downloader process to finish its own session-establishment
// phase (see acquireSessionLock) before giving up. Interactive login's own
// wait (waitForLoggedInCourseLink) allows up to 300s for a human/TU-Fast to
// complete 2FA, so this must comfortably exceed that or a second process
// would time out waiting on a first process's legitimate, still-in-progress
// login; it does not need to be much larger than that, since anything still
// holding the lock past a full login-wait plus margin is unusual enough that
// surfacing an error (rather than blocking indefinitely) is more useful.
const sessionLockAcquireTimeout = 6 * time.Minute

func (s *OpalScraper) ensureSession(forceInteractive bool) error {
	profileDir, err := LoginProfileDir()
	if err != nil {
		return err
	}
	// Serializes this whole function - not just the interactive-login branch
	// - across every opal-downloader process targeting the same real
	// profileDir+stateFile pair. See acquireSessionLock's doc comment
	// (session_lock_windows.go) for the two concrete races this closes.
	release, err := acquireSessionLock(profileDir, s.stateFile, sessionLockAcquireTimeout)
	if err != nil {
		return err
	}
	defer release()

	_ = s.closeBrowser()

	// Reset before deciding which branch this call takes below - see
	// usedInteractiveLogin's doc comment. Defaults to false (headless-only)
	// and is only flipped to true right before falling through to the
	// interactive-login branch a few lines down.
	s.usedInteractiveLogin = false

	if !forceInteractive {
		_, statErr := os.Stat(s.stateFile)
		stateFileExists := statErr == nil
		if stateFileExists {
			headless := !s.developerMode
			if s.developerMode {
				logging.User("Developer mode enabled: launching visible browser for session reuse and crawl tracing.")
			}
			launchErr := s.launchBrowser(headless, true)
			var authenticated bool
			var authErr error
			if launchErr == nil {
				authenticated, authErr = s.isAuthenticated()
			}
			// This re-check runs after acquireSessionLock (above) has
			// already been acquired - critically, it is a *re-check*, not a
			// decision made before contending for the lock. That distinction
			// is the fix for queue task
			// fix-redundant-interactive-login-on-lock-contention: if another
			// process was holding the lock because it was doing its own
			// interactive login, that process's saveState() has already run
			// and released the lock by the time this process gets here, so
			// os.Stat/isAuthenticated above observe that process's *freshly
			// saved* session - not whatever was on disk (or missing) at
			// whatever earlier moment this process first decided it needed
			// to contend for the lock. See shouldReuseSavedSession's doc
			// comment and PR #80's own live-verification "Real gap found"
			// note for the original report. Live-verified 2026-07-17: two
			// concurrent `list` processes sharing the same real session
			// file, both starting with it missing - the second process
			// blocked on the lock the whole time the first interactively
			// logged in and saved a fresh session, then reused that fresh
			// session headlessly here without ever opening its own
			// interactive-login window.
			if shouldReuseSavedSession(stateFileExists, launchErr, authenticated, authErr) {
				logging.User("Using saved OPAL session state")
				return nil
			}
			logging.User("Saved session state expired. Interactive login required.")
			_ = s.closeBrowser()
		}
	}

	// Falling through to here means the saved session was missing/expired
	// (or forceInteractive was requested) - this call is taking the
	// interactive-login branch. Record that now, before launching the
	// visible browser, so UsedInteractiveLogin() reflects it even if this
	// branch later fails partway through (e.g. login/2FA times out).
	s.usedInteractiveLogin = true

	if err := s.launchBrowser(false, false); err != nil {
		return err
	}
	page := s.getPage()
	if page == nil {
		return errors.New("failed to initialize browser page")
	}

	logging.User("Opening OPAL at %s", s.opalURL)
	logging.User("Please complete login in the opened browser window (TU-Fast/2FA supported).")
	_, err = s.gotoPolitely(page, s.opalURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
	if err != nil {
		return fmt.Errorf("could not reach OPAL at %s - check your internet connection and opal_url in config.yaml: %w", s.opalURL, err)
	}
	if err := s.waitForLoggedInCourseLink(); err != nil {
		return err
	}
	if err := s.saveState(); err != nil {
		return err
	}

	if !shouldRelaunchHeadlessAfterInteractiveLogin(forceInteractive, s.developerMode) {
		// forceInteractive is only set by the standalone `login` command
		// (LoginWithBrowser), which has nothing left to do after saving
		// state - no crawl follows in the same process, so there's no
		// benefit to relaunching headless here, only extra latency.
		// developerMode explicitly asked for a visible browser for tracing,
		// so honor that too.
		return nil
	}

	// This is the ensureSession(false) path used by sync/list
	// (ScrapeWithSavedSession): the saved session state we just checked was
	// either missing or expired, so login fell through to the interactive,
	// visible persistent-context browser above (the only way to drive
	// TU-Fast - see launchBrowser's HMAC/real-profile comment). Without this,
	// the crawl that ScrapeWithSavedSession runs right after ensureSession
	// returns would continue in that same visible window, turning a one-time
	// interactive login step into an entire visible sync/list run. Now that
	// saveState() has persisted the freshly-authenticated session, close the
	// visible browser and relaunch headlessly against that saved state - via
	// the normal anonymous-context + StorageStatePath path (same as the
	// saved-session branch above), never the persistent-context path - so
	// the crawl proceeds headless. See queue task
	// investigate-sync-list-not-headless.
	_ = s.closeBrowser()
	if err := s.launchBrowser(true, true); err != nil {
		return fmt.Errorf("interactive login succeeded and session was saved, but relaunching headless afterward failed: %w", err)
	}
	auth, authErr := s.isAuthenticated()
	if authErr != nil {
		return fmt.Errorf("interactive login succeeded and session was saved, but verifying the headless relaunch failed: %w", authErr)
	}
	if !auth {
		return errors.New("interactive login succeeded and session was saved, but the headless relaunch did not appear authenticated")
	}
	return nil
}

// shouldRelaunchHeadlessAfterInteractiveLogin reports whether ensureSession
// should close the visible interactive-login browser (launched via
// launchBrowser(false, false) against the dedicated login profile, see
// LoginProfileDir - the only way to drive TU-Fast) and relaunch headlessly
// against the just-saved session state, once that interactive login has
// succeeded.
//
// forceInteractive is true only for the standalone `login` command
// (LoginWithBrowser) - it has nothing left to do after saving state, so
// there is no crawl to protect from running in a visible window and no
// reason to pay for an extra headless relaunch. developerMode is an
// explicit --dev request for a visible, traceable browser and must be
// honored the same way. Everywhere else - the ensureSession(false) path
// used by sync/list (ScrapeWithSavedSession) - a saved-but-expired or
// missing session falls through to this same interactive login, and
// without relaunching headless afterward the crawl that follows would run
// in that same visible window, which is the bug this function's caller
// fixes. See queue task investigate-sync-list-not-headless.
func shouldRelaunchHeadlessAfterInteractiveLogin(forceInteractive, developerMode bool) bool {
	return !forceInteractive && !developerMode
}

// shouldReuseSavedSession reports whether ensureSession's post-lock
// re-check (see the call site above) should reuse the saved session state
// headlessly instead of falling through to the interactive-login branch.
//
// This is the decision that closes the gap described in queue task
// fix-redundant-interactive-login-on-lock-contention (a follow-up filed
// from PR #80's own live-verification "Real gap found" note): once this
// process has acquired the cross-process session-establishment mutex
// (acquireSessionLock, session_lock_windows.go), it must re-validate the
// state file at *this* point, not trust whatever was true when it first
// decided to contend for the lock - another process may have been holding
// the lock precisely because it was doing its own interactive login, and
// by the time this process gets the lock, that session is the freshest
// one on disk.
//
// stateFileExists is os.Stat(s.stateFile)'s result at the point this
// process is inside the lock. launchErr is any error from launching the
// headless, anonymous-context browser against that saved state
// (launchBrowser(headless, true) - never the persistent-context/real-profile
// path, per the HMAC/real-profile constraint). authenticated/authErr are
// isAuthenticated()'s result once that browser launched successfully (both
// are the zero value when launchErr != nil, since isAuthenticated is never
// called in that case). Reuse only happens when the file existed, the
// headless browser launched, and isAuthenticated cleanly reports true -
// any error anywhere in that chain is treated the same as "not valid",
// which is the safe default (falls through to interactive login rather
// than risking a crawl against a session that might not actually be
// authenticated).
func shouldReuseSavedSession(stateFileExists bool, launchErr error, authenticated bool, authErr error) bool {
	return stateFileExists && launchErr == nil && authErr == nil && authenticated
}

// courseLinkSelector matches links present once the OPAL dashboard/course
// list has loaded after a successful login.
const courseLinkSelector = "a[href*='crs_'], a[href*='course'], a[href*='RepositoryEntry']"

// waitForLoggedInCourseLink waits for the post-login course list to appear.
// If the tab it's waiting on gets closed mid-wait - which happens when
// TU-Fast/the Shibboleth IdP opens a new tab for the auth flow and closes the
// original one - trackActivePage will already have retargeted s.page at the
// new tab, so this retries the wait there instead of failing outright.
// loginPollMs is how long each probe for the post-login course list runs
// before the login page is looked at again. Short, because its only job is to
// set how often the stall check gets to run.
const loginPollMs = 1000

// loginQuietMs is how long the login page must show no sign of life at all
// before it is treated as stalled. TU-Fast fills the credential fields within
// a second or two of a login page settling, so several seconds of a completely
// unchanged page is strong evidence it is not going to.
//
// This replaced a flat 45-second wait. The maintainer's report was that the
// reload "braucht viel zu lange" (2026-07-26) - and they were describing a
// design, not a tuning mistake: the old code waited out a fixed timer whether
// or not anything was happening, so a stalled login always cost 45 seconds of
// staring at a page that was never going to move.
const loginQuietMs = 8000

// loginTotalBudgetMs is the overall time allowed for a login to complete,
// unchanged from when this was a single flat wait. Human attention span is
// what sets it - a person completing 2FA by hand needs the room.
const loginTotalBudgetMs = 300000

// loginStallReloadAttempts bounds how many times a stalled login page is
// reloaded. Deliberately small: the reload is a workaround for someone
// else's bug, not a retry loop.
const loginStallReloadAttempts = 2

// loginSignals is what the login page looks like at one moment: enough to
// tell "nothing is happening" from "something is happening", and nothing more.
//
// CredentialsEntered is a count of non-empty fields, never their contents.
// That distinction is the whole reason this reads the DOM at all rather than
// being waved off as too close to the user's password: knowing that a box has
// something in it is what decides whether to reload, and the something itself
// is never read, logged, or compared.
type loginSignals struct {
	URL string

	// FieldCount is how many text/password/email inputs the page offers, which
	// changes when the flow moves from a password screen to a 2FA prompt even
	// if the URL does not.
	FieldCount int

	// FilledFields is how many of them are non-empty.
	FilledFields int

	// Unknown means the page could not be read - it was closed, navigating, or
	// the evaluation failed. Treated as "do not reload": acting on a reading
	// that could not be taken is how a working login gets interrupted.
	Unknown bool
}

// changedFrom reports whether anything observable moved between two readings.
// An unreadable page counts as movement, because a page mid-navigation is
// exactly what a working login looks like.
func (a loginSignals) changedFrom(b loginSignals) bool {
	if a.Unknown || b.Unknown {
		return true
	}
	return a.URL != b.URL || a.FieldCount != b.FieldCount || a.FilledFields != b.FilledFields
}

// stalled reports whether this reading is the specific shape that a TU-Fast
// that never fired leaves behind: still sitting in the login flow, showing
// fields to fill, with nothing filled in.
//
// The "nothing filled in" part is what makes this safe in a way the old timer
// was not. A human typing their password by hand keeps the page on a login
// URL, so the previous code would reload and wipe what they had typed once its
// 45 seconds were up. A filled field now means somebody or something is
// working, and the page is left alone.
func (a loginSignals) stalled() bool {
	if a.Unknown {
		return false
	}
	return looksLikeLoginPageURL(a.URL) && a.FieldCount > 0 && a.FilledFields == 0
}

// readLoginSignals takes one reading of the login page. Any failure is
// reported as Unknown rather than guessed at.
func (s *OpalScraper) readLoginSignals(page playwright.Page) loginSignals {
	if page == nil {
		return loginSignals{Unknown: true}
	}
	raw, err := page.Evaluate(`() => {
		const kinds = ['text', 'password', 'email', 'tel', 'number'];
		const fields = Array.from(document.querySelectorAll('input'))
			.filter(el => kinds.includes((el.type || 'text').toLowerCase()));
		return {
			fields: fields.length,
			// Length only - the value itself never leaves the page.
			filled: fields.filter(el => (el.value || '').length > 0).length,
		};
	}`)
	if err != nil {
		return loginSignals{Unknown: true}
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return loginSignals{Unknown: true}
	}
	toInt := func(v any) int {
		if f, ok := v.(float64); ok {
			return int(f)
		}
		if i, ok := v.(int); ok {
			return i
		}
		return 0
	}
	return loginSignals{
		URL:          page.URL(),
		FieldCount:   toInt(m["fields"]),
		FilledFields: toInt(m["filled"]),
	}
}

// looksLikeLoginPageURL reports whether pageURL is still somewhere in the
// login/identity-provider flow rather than on OPAL proper. Same heuristic
// isAuthenticated uses (see above); shared so "are we still stuck at login"
// means one thing in this package.
func looksLikeLoginPageURL(pageURL string) bool {
	lowered := strings.ToLower(pageURL)
	return strings.Contains(lowered, "login") || strings.Contains(lowered, "shib") || strings.Contains(lowered, "idp")
}

// reloadStalledLoginPage nudges a login page that TU-Fast has not acted on.
//
// The maintainer reports that TU-Fast occasionally just sits on the OPAL /
// Shibboleth login screen without ever firing, and that reloading the page
// has fixed it every single time. They also note this predates
// opal-downloader - they hit it browsing OPAL by hand - so the root cause is
// almost certainly in TU-Fast itself and not something this project can fix.
// It is cheap to work around here anyway.
//
// Only reloads while the page still looks like part of the login flow.
// Reloading after login has progressed could discard a half-finished 2FA
// exchange, which would turn a rare annoyance into a real failure.
func (s *OpalScraper) reloadStalledLoginPage(page playwright.Page, attempt int) bool {
	if page == nil {
		return false
	}
	currentURL := page.URL()
	if !looksLikeLoginPageURL(currentURL) {
		s.auditLog("login-stall-reload-skipped", page, currentURL,
			"login page did not resolve yet, but the page has already moved past the login flow - not reloading, that could discard an in-progress 2FA exchange")
		return false
	}

	logging.User("Login page has not progressed yet (TU-Fast may not have fired) - reloading it (attempt %d of %d)...", attempt, loginStallReloadAttempts)
	s.auditLog("login-stall-reload", page, currentURL, fmt.Sprintf("reloading stalled login page, attempt %d of %d", attempt, loginStallReloadAttempts))
	if _, err := page.Reload(playwright.PageReloadOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		s.auditLog("login-stall-reload-failed", page, currentURL, fmt.Sprintf("reload failed: %v", err))
		return false
	}
	return true
}

func (s *OpalScraper) waitForLoggedInCourseLink() error {
	// Watch the page rather than a clock. The budget is spent as short probes
	// for the post-login course list, and between probes the login page is
	// read for any sign that something is happening at all. A page that has
	// not moved in loginQuietMs, still shows empty credential fields, and is
	// still on a login URL is one TU-Fast never acted on - and is worth
	// reloading immediately instead of after a fixed wait.
	//
	// The retargeting the old code did by hand falls out of this for free:
	// s.getPage() is re-read every pass, so a flow that opens a new tab and
	// closes the old one is simply followed.
	deadline := time.Now().Add(loginTotalBudgetMs * time.Millisecond)
	reloads := 0

	previous := s.readLoginSignals(s.getPage())
	lastChange := time.Now()

	for {
		page := s.getPage()
		if page == nil {
			return fmt.Errorf("login page closed before the course list appeared")
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			s.auditLog("wait-selector-timeout", page, courseLinkSelector,
				fmt.Sprintf("post-login course link did not resolve within the %dms budget", loginTotalBudgetMs))
			return fmt.Errorf("timed out after %dms waiting for the OPAL course list after login", loginTotalBudgetMs)
		}

		probe := math.Min(float64(loginPollMs), float64(remaining.Milliseconds()))
		start := time.Now()
		if _, err := page.WaitForSelector(courseLinkSelector, playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(probe),
		}); err == nil {
			s.auditLog("wait-selector-resolved", page, courseLinkSelector,
				fmt.Sprintf("post-login course link resolved after %s", time.Since(start)))
			return nil
		}

		current := s.readLoginSignals(s.getPage())
		if current.changedFrom(previous) {
			previous = current
			lastChange = time.Now()
			continue
		}

		quiet := time.Since(lastChange)
		if reloads >= loginStallReloadAttempts || quiet < loginQuietMs*time.Millisecond || !current.stalled() {
			continue
		}

		s.auditLog("login-stall-detected", page, current.URL,
			fmt.Sprintf("no change for %s with %d empty credential field(s) - treating as a TU-Fast that did not fire", quiet, current.FieldCount))
		if !s.reloadStalledLoginPage(page, reloads+1) {
			// The page moved past login, or the reload itself failed. Either
			// way stop nudging and spend what is left of the budget waiting.
			reloads = loginStallReloadAttempts
			continue
		}
		reloads++
		previous = s.readLoginSignals(s.getPage())
		lastChange = time.Now()
	}
}
