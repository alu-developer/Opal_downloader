package scraper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

func (s *OpalScraper) launchBrowser(headless, useSavedState bool) error {
	if s.pw == nil {
		pw, err := playwright.Run()
		if err != nil {
			return err
		}
		s.pw = pw
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
		ctx, err := s.pw.Chromium.LaunchPersistentContext(launchUserDataDir, opts)
		if err != nil {
			return fmt.Errorf("launching browser with profile %s: %w", launchUserDataDir, err)
		}
		fmt.Printf("Launching persistent browser profile: userDataDir=%s profile=%s\n", launchUserDataDir, defaultString(s.browserProfileDir, "(default)"))
		s.context = ctx
		s.trackActivePage(ctx)
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
		s.page.SetDefaultTimeout(15000)
		s.page.SetDefaultNavigationTimeout(20000)
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
	s.trackActivePage(ctx)
	page, err := ctx.NewPage()
	if err != nil {
		return err
	}
	s.page = page
	s.page.SetDefaultTimeout(15000)
	s.page.SetDefaultNavigationTimeout(20000)
	return nil
}

// trackActivePage keeps s.page pointed at whatever page is currently open in
// ctx. During interactive login, TU-Fast (or the Shibboleth IdP redirect it
// drives) sometimes opens a new tab/window for the auth flow and closes the
// original one; without this, s.page keeps referencing the closed page and
// any later call on it (Goto/WaitForSelector) fails with "target closed"
// even though the login flow is proceeding fine in the new tab.
func (s *OpalScraper) trackActivePage(ctx playwright.BrowserContext) {
	ctx.OnPage(func(p playwright.Page) {
		p.SetDefaultTimeout(15000)
		p.SetDefaultNavigationTimeout(20000)
		s.page = p
	})
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
	_, err := s.context.StorageState(playwright.BrowserContextStorageStateOptions{Path: playwright.String(s.stateFile)})
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
	courseCandidates, err := s.page.Locator(courseLinkSelector).Count()
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
	if s.page == nil {
		return errors.New("failed to initialize browser page")
	}

	fmt.Printf("Opening OPAL at %s\n", s.opalURL)
	fmt.Println("Please complete login in the opened browser window (TU-Fast/2FA supported).")
	_, err := s.page.Goto(s.opalURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
	if err != nil {
		return err
	}
	if err := s.waitForLoggedInCourseLink(); err != nil {
		return err
	}
	return s.saveState()
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
		waitingOn := s.page
		_, err := waitingOn.WaitForSelector(courseLinkSelector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(300000)})
		if err == nil {
			return nil
		}
		if attempt == 0 && s.page != nil && s.page != waitingOn {
			// The active page changed while we were waiting; retry on it.
			continue
		}
		return err
	}
	return nil
}
