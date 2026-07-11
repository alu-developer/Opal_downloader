package opaldownloader

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/gui"
	"github.com/alu-developer/opal-downloader/internal/procguard"
	"github.com/alu-developer/opal-downloader/internal/report"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/syncer"
	"github.com/alu-developer/opal-downloader/internal/timing"
	"github.com/alu-developer/opal-downloader/internal/updater"
	"github.com/mxschmitt/playwright-go"
)

// updateCheckTimeout bounds how long the best-effort "is a newer version
// available" check (see printUpdateFooter) is allowed to block a
// login/list/sync run before giving up silently. Kept short so an
// offline/unreachable/rate-limited GitHub API never noticeably delays
// command completion.
const updateCheckTimeout = 3 * time.Second

// updaterCheckLatest is a package-level indirection over
// updater.CheckLatest so tests can substitute a fake implementation (e.g.
// backed by an httptest.Server) without printUpdateFooter making a real
// network call.
var updaterCheckLatest = updater.CheckLatest

// buildVersion holds the released version string. It defaults to "dev" for
// plain `go build`/`go run` and is overridden at release-build time via:
//
//	go build -ldflags "-X github.com/alu-developer/opal-downloader/cmd/opal-downloader.buildVersion=vX.Y.Z"
//
// This is a prerequisite for the planned in-app update checker (see
// docs/update-mechanism-plan.md Section 2.1), which needs a reliable "what
// version am I running" value. Wiring the release pipeline to actually pass
// -ldflags is separate follow-up work.
var buildVersion = "dev"

// Execute is the CLI entry point (called from main()). It recovers from any
// panic that escapes a subcommand rather than letting Go print a bare
// panic+stack dump and exit: best-effort crash capture per this task's
// scope (see CLAUDE.md "Reliability over features" - a crash should be a
// reported incident, not a silent/confusing exit). The recovered report is
// printed to stderr only; unlike the GUI's Feedback page (internal/gui's
// feedback.go), the CLI never opens a browser on the user's behalf - the
// user pastes the printed text into a new GitHub issue themselves if they
// choose to.
func Execute() {
	defer func() {
		if rec := recover(); rec != nil {
			printCrashReport(rec, debug.Stack())
			os.Exit(1)
		}
	}()

	// Make sure any Chromium/Brave process Playwright launches (login/sync/
	// list/dump-links/gui all go through internal/scraper) dies with this
	// process, even if this process is killed abruptly (crash, `taskkill
	// /F`, a queue-run force-stop) rather than exiting normally - see
	// internal/procguard's doc comment for the full story. Must run before
	// any subcommand has a chance to launch a browser.
	procguard.EnsureChildProcessesDieWithParent()

	// Running the binary with no subcommand at all launches the GUI - the
	// web UI is the primary/default way most users interact with
	// opal-downloader (see docs/gui-concept.md Section 5). All CLI
	// subcommands below remain fully functional for scripting/automation;
	// this only changes what happens when none of them is given.
	if len(os.Args) < 2 {
		if err := runGUI(nil); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		return
	}

	command := os.Args[1]
	args := os.Args[2:]

	var err error
	switch command {
	case "init":
		err = runInit(args)
	case "setup":
		err = runSetup(args)
	case "status":
		err = runStatus(args)
	case "login":
		err = runLogin(args)
	case "list":
		err = runList(args)
	case "sync":
		err = runSync(args)
	case "dump-links":
		err = runDumpLinks(args)
	case "gui":
		err = runGUI(args)
	case "__panic-test":
		// Undocumented, not listed in printHelp: exists solely so the panic
		// recovery wired up in Execute (see its doc comment) can be
		// live-verified end-to-end - `opal-downloader __panic-test` - without
		// needing a real bug to trigger a crash on demand.
		panic("intentional panic for __panic-test")
	case "--help", "-h", "help":
		printHelp()
		return
	case "--version", "-v", "version":
		fmt.Println("opal-downloader " + buildVersion)
		return
	default:
		err = fmt.Errorf("unknown command: %s", command)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func runInit(args []string) error {
	configPath := filepath.Join(projectDir(), "config.yaml")
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i >= len(args) {
				return fmt.Errorf("--config requires a path")
			}
			configPath = args[i]
		default:
			return fmt.Errorf("unknown option for init: %s", args[i])
		}
	}

	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("skip (exists): %s\n", configPath)
	} else {
		source := filepath.Join(projectDir(), "config.example.yaml")
		if err := copyFile(source, configPath); err != nil {
			return err
		}
		fmt.Printf("created: %s\n", configPath)
	}
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Edit config.yaml with your download path and course patterns")
	fmt.Println("  2. Set up browser login: for TU-Fast auto-login without ever locking your")
	fmt.Println("     everyday browser, see docs/browser-profile-strategy.md (recommended);")
	fmt.Println("     or leave browser_user_data_dir empty / point it at your everyday")
	fmt.Println("     profile if you'd rather skip that setup and log in manually or reuse")
	fmt.Println("     an existing TU-Fast install")
	fmt.Println("  3. Run: opal-downloader login")
	fmt.Println("  4. Run: opal-downloader sync")
	return nil
}

func runSetup(args []string) error {
	configPath := filepath.Join(projectDir(), "config.yaml")
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i >= len(args) {
				return fmt.Errorf("--config requires a path")
			}
			configPath = args[i]
		default:
			return fmt.Errorf("unknown option for setup: %s", args[i])
		}
	}

	fmt.Println("Installing Playwright browser binaries...")
	// Use playwright-go's own Install() API instead of shelling out to
	// `go run github.com/mxschmitt/playwright-go/cmd/playwright ... install`.
	// The shell-out required a Go toolchain on PATH, which a plain compiled
	// opal-downloader.exe (no Go, no git) does not have - it broke the
	// installer's post-install [Run] fallback and any `setup` rerun on a
	// machine without Go. See docs/installer-plan.md Section 9.
	if err := playwright.Install(&playwright.RunOptions{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}); err != nil {
		return fmt.Errorf("playwright install failed: %w", err)
	}
	fmt.Println("Playwright browsers ready.")
	fmt.Println()

	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config already exists: %s\n", configPath)
	} else {
		source := filepath.Join(projectDir(), "config.example.yaml")
		if err := copyFile(source, configPath); err != nil {
			return err
		}
		fmt.Printf("Created config: %s\n", configPath)
	}

	fmt.Println()
	fmt.Println("Setup cannot rebuild this binary itself - make sure you've already run:")
	fmt.Println("  go build -o opal-downloader.exe .   (Windows)")
	fmt.Println("  go build -o opal-downloader .        (Linux/macOS)")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit config.yaml with your download path and course patterns")
	fmt.Println("  2. Set up browser login: for TU-Fast auto-login without ever locking your")
	fmt.Println("     everyday browser, see docs/browser-profile-strategy.md (recommended);")
	fmt.Println("     or leave browser_user_data_dir empty / point it at your everyday")
	fmt.Println("     profile if you'd rather skip that setup and log in manually or reuse")
	fmt.Println("     an existing TU-Fast install")
	fmt.Println("  3. Run: opal-downloader login")
	return nil
}

func runStatus(args []string) error {
	configPath := filepath.Join(projectDir(), "config.yaml")
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i >= len(args) {
				return fmt.Errorf("--config requires a path")
			}
			configPath = args[i]
		default:
			return fmt.Errorf("unknown option for status: %s", args[i])
		}
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		return err
	}
	printConfigWarnings(loaded.App)

	fmt.Printf("Config: %s (OK)\n", configPath)
	fmt.Printf("OPAL URL: %s\n", loaded.Credentials.URL)
	fmt.Printf("Download path: %s\n", loaded.App.DownloadPath)

	if loaded.Credentials.BrowserUserDataDir != "" {
		checkBrowserProfileHealth(loaded.Credentials.BrowserUserDataDir, loaded.Credentials.BrowserProfileDir)
	}

	info, statErr := os.Stat(loaded.Credentials.StateFile)
	if statErr != nil || info.Size() == 0 {
		fmt.Println()
		fmt.Println("Not logged in yet. Run: opal-downloader login")
		return nil
	}

	fmt.Println()
	fmt.Printf("Logged in: session state file present (%s)\n", loaded.Credentials.StateFile)
	return nil
}

// tuFastExtensionID is TU-Fast's Chrome Web Store extension ID
// (confirmed live in the task that first wired up TU-Fast auto-login -
// see docs/browser-profile-strategy.md's Health-check design section).
// This is a heuristic tied to the current TU-Fast build, not a stable
// protocol guarantee: if TU-Fast's extension ID ever changes, or a
// different Bildungsportal-Sachsen-instance-specific extension is used
// instead, this check degrades to "soft warning always fires" - never a
// false hard failure.
const tuFastExtensionID = "aheogihliekaafikeepfjngfegbnimbk"

// checkBrowserProfileHealth performs the filesystem-only pre-flight checks
// described in docs/browser-profile-strategy.md's "Health-check design"
// section for whichever browser_user_data_dir is configured. It applies
// identically to both supported strategies (a dedicated second profile or
// pointing directly at a real Brave/Chrome profile) - see that doc for why
// this isn't strategy-specific. No browser is launched; this only stats a
// few paths, keeping `status` fast and offline.
func checkBrowserProfileHealth(userDataDir, profileDir string) {
	if _, err := os.Stat(userDataDir); err != nil {
		fmt.Println()
		fmt.Printf("browser_user_data_dir is set to %s but that directory doesn't exist. If you were following the dedicated browser-profile setup, re-run the one-time setup steps in docs/browser-profile-strategy.md.\n", userDataDir)
		return
	}

	profile := profileDir
	if profile == "" {
		profile = "Default"
	}

	preferencesPath := filepath.Join(userDataDir, profile, "Preferences")
	if _, err := os.Stat(preferencesPath); err != nil {
		fmt.Println()
		fmt.Printf("browser_user_data_dir is set to %s but %s wasn't found, so this doesn't look like a real browser profile. If you were following the dedicated browser-profile setup, re-run the one-time setup steps in docs/browser-profile-strategy.md.\n", userDataDir, preferencesPath)
		return
	}

	extensionPath := filepath.Join(userDataDir, profile, "Extensions", tuFastExtensionID)
	if _, err := os.Stat(extensionPath); err != nil {
		fmt.Println()
		fmt.Println("Note: TU-Fast extension not detected in this browser profile. Logins will need manual 2FA each time. If you expected TU-Fast to be set up here, see docs/browser-profile-strategy.md.")
	}
}

func runLogin(args []string) error {
	configPath := filepath.Join(projectDir(), "config.yaml")
	devMode := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i >= len(args) {
				return fmt.Errorf("--config requires a path")
			}
			configPath = args[i]
		case "--dev":
			devMode = true
		default:
			return fmt.Errorf("unknown option for login: %s", args[i])
		}
	}

	credentials, err := config.LoadCredentials(configPath)
	if err != nil {
		return err
	}

	sc := scraper.New(credentials.URL, credentials.StateFile, credentials.BrowserExecutable, credentials.BrowserUserDataDir, credentials.BrowserProfileDir)
	sc.SetDeveloperMode(devMode)
	defer sc.Close()
	defer closeBrowserOnInterrupt(sc)()

	fmt.Println("Opening OPAL for login...")
	fmt.Println("Please log in using TU-Fast (2FA is supported).")
	fmt.Println()

	if err := sc.LoginWithBrowser(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Login successful! Session state saved.")
	fmt.Printf("Session state file: %s\n", credentials.StateFile)
	fmt.Println()
	fmt.Println("You can now run: opal-downloader sync")
	printUpdateFooter()
	return nil
}

func runList(args []string) error {
	configPath := filepath.Join(projectDir(), "config.yaml")
	devMode := false
	profile := false
	debugClicks := false
	courseConcurrency := 0
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i >= len(args) {
				return fmt.Errorf("--config requires a path")
			}
			configPath = args[i]
		case "--dev":
			devMode = true
		case "--profile":
			profile = true
		case "--debug-clicks":
			debugClicks = true
		case "--course-concurrency":
			i++
			if i >= len(args) {
				return fmt.Errorf("--course-concurrency requires a value")
			}
			parsed, parseErr := strconv.Atoi(args[i])
			if parseErr != nil || parsed <= 0 {
				return fmt.Errorf("--course-concurrency requires a positive integer, got %q", args[i])
			}
			courseConcurrency = parsed
		default:
			return fmt.Errorf("unknown option for list: %s", args[i])
		}
	}
	timing.Profile = profile

	loaded, err := config.Load(configPath)
	if err != nil {
		return err
	}
	printConfigWarnings(loaded.App)
	if courseConcurrency > 0 {
		loaded.App.CourseConcurrency = courseConcurrency
	}

	sc := scraper.New(loaded.Credentials.URL, loaded.Credentials.StateFile, loaded.Credentials.BrowserExecutable, loaded.Credentials.BrowserUserDataDir, loaded.Credentials.BrowserProfileDir)
	sc.SetDeveloperMode(devMode)
	sc.SetDebugClicks(debugClicks)
	sc.SetCourseConcurrency(loaded.App.CourseConcurrency)
	defer sc.Close()
	defer closeBrowserOnInterrupt(sc)()

	totalTimer := timing.StartTimer()
	err = syncer.ListAvailableCourses(sc)
	fmt.Println()
	timing.PrintTotalSummary(totalTimer.Elapsed())
	if err == nil {
		printUpdateFooter()
	}
	return err
}

func runSync(args []string) error {
	configPath := filepath.Join(projectDir(), "config.yaml")
	force := false
	devMode := false
	profile := false
	debugClicks := false
	concurrency := 0
	courseConcurrency := 0

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i >= len(args) {
				return fmt.Errorf("--config requires a path")
			}
			configPath = args[i]
		case "--force":
			force = true
		case "--dev":
			devMode = true
		case "--profile":
			profile = true
		case "--debug-clicks":
			debugClicks = true
		case "--concurrency":
			i++
			if i >= len(args) {
				return fmt.Errorf("--concurrency requires a value")
			}
			parsed, parseErr := strconv.Atoi(args[i])
			if parseErr != nil || parsed <= 0 {
				return fmt.Errorf("--concurrency requires a positive integer, got %q", args[i])
			}
			concurrency = parsed
		case "--course-concurrency":
			i++
			if i >= len(args) {
				return fmt.Errorf("--course-concurrency requires a value")
			}
			parsed, parseErr := strconv.Atoi(args[i])
			if parseErr != nil || parsed <= 0 {
				return fmt.Errorf("--course-concurrency requires a positive integer, got %q", args[i])
			}
			courseConcurrency = parsed
		default:
			return fmt.Errorf("unknown option for sync: %s", args[i])
		}
	}
	timing.Profile = profile

	loaded, err := config.Load(configPath)
	if err != nil {
		return err
	}
	printConfigWarnings(loaded.App)
	if concurrency > 0 {
		loaded.App.DownloadConcurrency = concurrency
	}
	if courseConcurrency > 0 {
		loaded.App.CourseConcurrency = courseConcurrency
	}

	sc := scraper.New(loaded.Credentials.URL, loaded.Credentials.StateFile, loaded.Credentials.BrowserExecutable, loaded.Credentials.BrowserUserDataDir, loaded.Credentials.BrowserProfileDir)
	sc.SetDeveloperMode(devMode)
	sc.SetDebugClicks(debugClicks)
	sc.SetCourseConcurrency(loaded.App.CourseConcurrency)
	defer sc.Close()
	defer closeBrowserOnInterrupt(sc)()

	fmt.Printf("Download path: %s\n", loaded.App.DownloadPath)
	fmt.Printf("Course patterns: %s\n", strings.Join(loaded.App.Courses, ", "))
	if loaded.App.DefaultCourseFolder != "" {
		fmt.Printf("Default course folder: %s\n", loaded.App.DefaultCourseFolder)
	}
	if len(loaded.App.CourseFolders) > 0 {
		fmt.Println("Course folder rules:")
		for pattern, folder := range loaded.App.CourseFolders {
			fmt.Printf("  %s -> %s\n", pattern, folder)
		}
	}
	fmt.Println()

	totalTimer := timing.StartTimer()
	stats, err := syncer.SyncCourses(sc, loaded.App, force)
	if err != nil {
		return err
	}
	fmt.Printf("\nDone. downloaded=%d skipped=%d errors=%d\n", stats.Downloaded, stats.Skipped, stats.Errors)

	fmt.Println()
	timing.PrintDownloadSummary(stats.DownloadDuration, stats.Downloads)
	timing.PrintTotalSummary(totalTimer.Elapsed())
	printUpdateFooter()
	return nil
}

func runDumpLinks(args []string) error {
	configPath := filepath.Join(projectDir(), "config.yaml")
	devMode := false
	targetURL := ""
	outputPath := filepath.Join(projectDir(), "tmp", "opal-links.json")

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i >= len(args) {
				return fmt.Errorf("--config requires a path")
			}
			configPath = args[i]
		case "--url":
			i++
			if i >= len(args) {
				return fmt.Errorf("--url requires a value")
			}
			targetURL = args[i]
		case "--out":
			i++
			if i >= len(args) {
				return fmt.Errorf("--out requires a path")
			}
			outputPath = args[i]
		case "--dev":
			devMode = true
		default:
			return fmt.Errorf("unknown option for dump-links: %s", args[i])
		}
	}

	if strings.TrimSpace(targetURL) == "" {
		return fmt.Errorf("dump-links requires --url")
	}

	credentials, err := config.LoadCredentials(configPath)
	if err != nil {
		return err
	}

	sc := scraper.New(credentials.URL, credentials.StateFile, credentials.BrowserExecutable, credentials.BrowserUserDataDir, credentials.BrowserProfileDir)
	sc.SetDeveloperMode(devMode)
	defer sc.Close()
	defer closeBrowserOnInterrupt(sc)()

	if err := sc.DumpPageLinks(targetURL, outputPath); err != nil {
		return err
	}

	fmt.Printf("Link dump written to: %s\n", outputPath)
	return nil
}

func runGUI(args []string) error {
	port := 0
	configPath := filepath.Join(projectDir(), "config.yaml")
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			i++
			if i >= len(args) {
				return fmt.Errorf("--port requires a value")
			}
			p, convErr := strconv.Atoi(args[i])
			if convErr != nil {
				return fmt.Errorf("invalid --port value: %s", args[i])
			}
			port = p
		case "--config":
			i++
			if i >= len(args) {
				return fmt.Errorf("--config requires a path")
			}
			configPath = args[i]
		default:
			return fmt.Errorf("unknown option for gui: %s", args[i])
		}
	}

	return gui.Run(gui.Options{Port: port, ConfigPath: configPath, Version: buildVersion})
}

// closeBrowserOnInterrupt is a belt-and-suspenders companion to
// procguard.EnsureChildProcessesDieWithParent (see its doc comment): that
// guarantees a Chromium/Brave process launched by sc never outlives this
// process even on a hard kill, but a graceful Ctrl-C should still close the
// browser window immediately and let the interrupted command return a clean
// error, rather than leaving the visible window open until the job-object
// kill fires. It mirrors the same os.Interrupt-triggered sc.Close() pattern
// internal/gui/gui.go's Run and window_windows.go's openNativeWindow already
// use for the GUI's own window. Closing sc.Close() causes whatever
// Playwright call is currently blocked (page.Goto/WaitForSelector/etc.) to
// return an error promptly, which propagates up through the normal
// error-returning path - no separate os.Exit needed here.
//
// Returns a stop func that must be deferred by the caller to release the
// signal channel once the command finishes normally.
func closeBrowserOnInterrupt(sc *scraper.OpalScraper) (stop func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	done := make(chan struct{})
	go func() {
		select {
		case <-sigCh:
			_ = sc.Close()
		case <-done:
		}
	}()
	return func() {
		close(done)
		signal.Stop(sigCh)
	}
}

// printUpdateFooter does a best-effort, short-timeout check against
// internal/updater for a newer release and, if one exists, prints one
// informational line pointing at its GitHub release page. This is the CLI
// counterpart to the GUI's update banner (docs/update-mechanism-plan.md
// Section 2.4's "CLI footnote") - purely informational, no
// download/install prompt, since login/list/sync are often run directly
// without the GUI. Any failure (offline, rate-limited, unparseable
// buildVersion such as the "dev" default) is swallowed silently: the
// update check must never fail or delay the command it's attached to.
func printUpdateFooter() {
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()

	rel, err := updaterCheckLatest(ctx, buildVersion)
	if err != nil || !rel.IsNewer {
		return
	}

	fmt.Printf("\nA newer version (v%s) is available: %s\n", rel.Version, rel.HTMLURL)
}

// printCrashReport prints a report.CrashReport (version, OS/arch, Go
// runtime, panic value, stack trace) to stderr, followed by a one-line
// instruction to paste it into a new GitHub issue. See Execute's doc
// comment for why this only prints (no browser-opening) unlike the GUI's
// equivalent Feedback flow.
func printCrashReport(rec any, stack []byte) {
	fmt.Fprintln(os.Stderr, "opal-downloader crashed unexpectedly.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, report.CrashReport(buildVersion, rec, stack))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Please report this at %s (paste the text above into the issue).\n", report.IssuesNewURL)
}

// printConfigWarnings prints non-fatal config.Warnings for app to stderr,
// one line per warning, prefixed "Warning:" (matching the "Error:" prefix
// used for fatal errors in Execute). A no-op when there are no warnings.
func printConfigWarnings(app config.App) {
	for _, w := range config.Warnings(app) {
		fmt.Fprintln(os.Stderr, "Warning:", w)
	}
}

func projectDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func printHelp() {
	fmt.Println("opal-downloader (Go)")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  opal-downloader <command> [options]")
	fmt.Println()
	fmt.Println("Running opal-downloader with no command starts the GUI (same as")
	fmt.Println("'opal-downloader gui') - that's the primary way to use this tool.")
	fmt.Println("The commands below remain available for scripting/automation.")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  init    Create config.yaml from example")
	fmt.Println("  setup   Install Playwright browsers, create config.yaml if missing, print next steps")
	fmt.Println("  status  Offline check: config parses and whether a session state file exists (no browser opened)")
	fmt.Println("  gui     Start the local web UI (127.0.0.1)")
	fmt.Println("  login   Open browser, complete login, save session state")
	fmt.Println("  list    List detected courses and file counts")
	fmt.Println("  sync    Download new/changed files")
	fmt.Println("  dump-links  Open a page and write all detected link candidates to a JSON file")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  init --config <path>")
	fmt.Println("  setup --config <path>")
	fmt.Println("  status --config <path>")
	fmt.Println("  gui [--port <port>] [--config <path>]")
	fmt.Println("  login --config <path> [--dev]")
	fmt.Println("  list --config <path> [--dev] [--profile] [--debug-clicks] [--course-concurrency <n>]")
	fmt.Println("  sync --config <path> [--force] [--dev] [--profile] [--debug-clicks] [--concurrency <n>] [--course-concurrency <n>]")
	fmt.Println("  dump-links --url <url> [--out <path>] [--config <path>] [--dev]")
	fmt.Println()
	fmt.Println("  --profile               Print granular per-course/per-file timings in addition to the summary")
	fmt.Println("  --debug-clicks          Log every click and navigation/interactive-link wait with timestamp, page URL, selector, and reason (diagnostic tool)")
	fmt.Println("  --concurrency n         Max concurrent file downloads for sync (default 3, overrides config.yaml)")
	fmt.Println("  --course-concurrency n  Max concurrent courses crawled during discovery (default 3, overrides config.yaml)")
}

func copyFile(source, target string) error {
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("missing example file: %s", source)
	}

	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
