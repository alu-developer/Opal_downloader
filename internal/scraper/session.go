package scraper

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		fmt.Printf("Launching persistent browser profile: userDataDir=%s\n", profileDir)
		s.setContext(ctx)
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
	fmt.Printf("Saved session state to: %s\n", s.stateFile)
	return nil
}

func (s *OpalScraper) isAuthenticated() (bool, error) {
	page := s.getPage()
	if page == nil {
		return false, nil
	}
	_, err := page.Goto(s.opalURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
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
				fmt.Println("Developer mode enabled: launching visible browser for session reuse and crawl tracing.")
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
				fmt.Println("Using saved OPAL session state")
				return nil
			}
			fmt.Println("Saved session state expired. Interactive login required.")
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

	fmt.Printf("Opening OPAL at %s\n", s.opalURL)
	fmt.Println("Please complete login in the opened browser window (TU-Fast/2FA supported).")
	_, err = page.Goto(s.opalURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
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
// loginStallProbeMs is how long to wait for the post-login course list
// before considering a reload. It is not a timeout: the full budget below
// is still spent before giving up, this is only the point at which a stalled
// TU-Fast becomes worth nudging.
const loginStallProbeMs = 45000

// loginTotalBudgetMs is the overall time allowed for a login to complete,
// unchanged from when this was a single flat wait. Human attention span is
// what sets it - a person completing 2FA by hand needs the room.
const loginTotalBudgetMs = 300000

// loginStallReloadAttempts bounds how many times a stalled login page is
// reloaded. Deliberately small: the reload is a workaround for someone
// else's bug, not a retry loop.
const loginStallReloadAttempts = 2

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

	fmt.Printf("Login page has not progressed yet (TU-Fast may not have fired) - reloading it (attempt %d of %d)...\n", attempt, loginStallReloadAttempts)
	s.auditLog("login-stall-reload", page, currentURL, fmt.Sprintf("reloading stalled login page, attempt %d of %d", attempt, loginStallReloadAttempts))
	if _, err := page.Reload(playwright.PageReloadOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		s.auditLog("login-stall-reload-failed", page, currentURL, fmt.Sprintf("reload failed: %v", err))
		return false
	}
	return true
}

func (s *OpalScraper) waitForLoggedInCourseLink() error {
	// Spend the budget as a few short probes followed by the remainder,
	// rather than one flat wait: a stalled TU-Fast used to burn the entire
	// 300s and then fail, when a reload after 45s would have fixed it. The
	// total time to a genuine failure is unchanged, so a human doing 2FA by
	// hand still gets the same room they always had.
	remaining := float64(loginTotalBudgetMs)
	for reloads := 0; reloads < loginStallReloadAttempts; reloads++ {
		waitingOn := s.getPage()
		if waitingOn == nil {
			break
		}
		probe := math.Min(float64(loginStallProbeMs), remaining)
		start := time.Now()
		_, err := waitingOn.WaitForSelector(courseLinkSelector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(probe)})
		remaining -= float64(time.Since(start).Milliseconds())
		if err == nil {
			s.auditLog("wait-selector-resolved", waitingOn, courseLinkSelector, fmt.Sprintf("post-login course link resolved after %s", time.Since(start)))
			return nil
		}
		if remaining <= 0 {
			s.auditLog("wait-selector-timeout", waitingOn, courseLinkSelector, fmt.Sprintf("post-login course link did not resolve within the %dms budget: %v", loginTotalBudgetMs, err))
			return err
		}
		if !s.reloadStalledLoginPage(s.getPage(), reloads+1) {
			// Either the page moved past login (so someone is mid-2FA and
			// just needs more time) or the reload failed. Either way, stop
			// nudging and spend what is left of the budget waiting.
			break
		}
	}

	for attempt := 0; attempt < 2; attempt++ {
		waitingOn := s.getPage()
		start := time.Now()
		_, err := waitingOn.WaitForSelector(courseLinkSelector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(math.Max(remaining, 1000))})
		if err == nil {
			// Logged for --debug-clicks completeness (queue task
			// click-audit-analysis-and-cleanup closed this gap - this wait was
			// previously not audited at all). This is a one-time,
			// once-per-login wait for interactive TU-Fast/2FA completion, not
			// a per-section crawl-loop cost, so unlike
			// navigation.go's/discovery.go's waits it was never a slowness
			// suspect and its long 300s timeout is intentional (human
			// attention span), not dead weight to trim.
			s.auditLog("wait-selector-resolved", waitingOn, courseLinkSelector, fmt.Sprintf("post-login course link resolved after %s", time.Since(start)))
			return nil
		}
		if attempt == 0 && s.getPage() != nil && s.getPage() != waitingOn {
			// The active page changed while we were waiting; retry on it.
			s.auditLog("wait-selector-retargeted", waitingOn, courseLinkSelector, fmt.Sprintf("active page changed after %s; retrying wait on new page", time.Since(start)))
			continue
		}
		s.auditLog("wait-selector-timeout", waitingOn, courseLinkSelector, fmt.Sprintf("post-login course link did not resolve within 300000ms (waited %s): %v", time.Since(start), err))
		return err
	}
	return nil
}
