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
		launchUserDataDir, err := s.prepareBrowserProfile()
		if err != nil {
			return err
		}

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
	page, err := ctx.NewPage()
	if err != nil {
		return err
	}
	s.page = page
	s.page.SetDefaultTimeout(15000)
	s.page.SetDefaultNavigationTimeout(20000)
	return nil
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
		return false, fmt.Errorf("could not reach OPAL at %s - check your internet connection and opal_url in config.yaml: %w", s.opalURL, err)
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
	courseCandidates, err := s.page.Locator("a[href*='crs_'], a[href*='course'], a[href*='RepositoryEntry']").Count()
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
		return fmt.Errorf("could not reach OPAL at %s - check your internet connection and opal_url in config.yaml: %w", s.opalURL, err)
	}
	_, err = s.page.WaitForSelector("a[href*='crs_'], a[href*='course'], a[href*='RepositoryEntry']", playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(300000)})
	if err != nil {
		return err
	}
	return s.saveState()
}
