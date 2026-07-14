package scraper

import (
	"errors"
	"fmt"
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

	if s.browserUserDataDir != "" && !headless && !useSavedState {
		// Launch directly against the real browser_user_data_dir rather than
		// a private copy. A copy-based approach was tried (see git history
		// around "fix-brave-profile-lock-conflict") to let Brave stay usable
		// while opal-downloader runs, but Chromium's "Secure Preferences"
		// file is integrity-protected (HMAC'd) specifically to detect
		// externally-modified extension state: copying it into a new
		// user-data-dir invalidates that protection, and Chromium resets
		// TU-Fast's permissions (or drops it entirely) the moment it loads
		// the copy - confirmed by direct inspection, not just a hunch. There
		// is no known way to relax that check without disabling Chromium's
		// tamper-protection outright, so launching against the real profile
		// (accepting the lock conflict below) is the only way to get a
		// working TU-Fast.
		locked, err := isUserDataDirLocked(s.browserUserDataDir)
		if err != nil {
			return err
		}
		if locked {
			return fmt.Errorf("%w: %s appears to be open in Brave (or another Chromium-based browser) right now — please fully close Brave before running opal-downloader login/sync, so it can use your real profile (with TU-Fast) directly", ErrProfileLocked, s.browserUserDataDir)
		}
		launchUserDataDir := s.browserUserDataDir

		opts := playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless: playwright.Bool(headless),
			IgnoreDefaultArgs: []string{
				"--disable-extensions",
				"--disable-component-extensions-with-background-pages",
			},
		}
		if s.browserProfileDir != "" {
			opts.Args = append(opts.Args, "--profile-directory="+s.browserProfileDir)
		}
		opts.Args = append(opts.Args, "--enable-extensions")
		if s.browserExecutable != "" {
			opts.ExecutablePath = playwright.String(s.browserExecutable)
		}
		ctx, err := s.getPw().Chromium.LaunchPersistentContext(launchUserDataDir, opts)
		if err != nil {
			return fmt.Errorf("launching browser with profile %s: %w", launchUserDataDir, err)
		}
		fmt.Printf("Launching persistent browser profile: userDataDir=%s profile=%s\n", launchUserDataDir, defaultString(s.browserProfileDir, "(default)"))
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

	launchOpts := playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(headless)}
	if s.browserExecutable != "" {
		launchOpts.ExecutablePath = playwright.String(s.browserExecutable)
	}
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

func (s *OpalScraper) ensureSession(forceInteractive bool) error {
	_ = s.closeBrowser()

	if !forceInteractive {
		if _, err := os.Stat(s.stateFile); err == nil {
			headless := !s.developerMode
			if s.developerMode {
				fmt.Println("Developer mode enabled: launching visible browser for session reuse and crawl tracing.")
			}
			if err := s.launchBrowser(headless, true); err == nil {
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
	page := s.getPage()
	if page == nil {
		return errors.New("failed to initialize browser page")
	}

	fmt.Printf("Opening OPAL at %s\n", s.opalURL)
	fmt.Println("Please complete login in the opened browser window (TU-Fast/2FA supported).")
	_, err := page.Goto(s.opalURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
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
// launchBrowser(false, false) against the real browser_user_data_dir, the
// only way to drive TU-Fast) and relaunch headlessly against the
// just-saved session state, once that interactive login has succeeded.
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

// courseLinkSelector matches links present once the OPAL dashboard/course
// list has loaded after a successful login.
const courseLinkSelector = "a[href*='crs_'], a[href*='course'], a[href*='RepositoryEntry']"

// waitForLoggedInCourseLink waits for the post-login course list to appear.
// If the tab it's waiting on gets closed mid-wait - which happens when
// TU-Fast/the Shibboleth IdP opens a new tab for the auth flow and closes the
// original one - trackActivePage will already have retargeted s.page at the
// new tab, so this retries the wait there instead of failing outright.
func (s *OpalScraper) waitForLoggedInCourseLink() error {
	for attempt := 0; attempt < 2; attempt++ {
		waitingOn := s.getPage()
		start := time.Now()
		_, err := waitingOn.WaitForSelector(courseLinkSelector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(300000)})
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
