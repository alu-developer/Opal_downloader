package opaldownloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/gui"
	"github.com/alu-developer/opal-downloader/internal/notify"
	"github.com/alu-developer/opal-downloader/internal/procguard"
	"github.com/alu-developer/opal-downloader/internal/report"
	"github.com/alu-developer/opal-downloader/internal/scheduler"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/smokecheck"
	"github.com/alu-developer/opal-downloader/internal/statuslog"
	"github.com/alu-developer/opal-downloader/internal/syncer"
	"github.com/alu-developer/opal-downloader/internal/synclock"
	"github.com/alu-developer/opal-downloader/internal/timing"
	"github.com/alu-developer/opal-downloader/internal/updater"
	"github.com/alu-developer/opal-downloader/internal/visitlog"
	"github.com/mxschmitt/playwright-go"
)

// Exit codes for distinguishable scheduled-sync failure modes (see
// EnsureTUFastPresent/synclock's ErrHeld) - added for `sync --scheduled`'s
// benefit: a Task Scheduler-triggered run has no human reading stdout, so
// the follow-up failure-log task (add-scheduled-sync-failure-log-and-gui-
// banner) needs a way to tell these apart from a generic failure without
// parsing free-text error messages. 0 (success) and 1 (any other error)
// keep their pre-existing meaning for every command, scheduled or not.
const (
	exitCodeGenericError       = 1
	exitCodeTUFastNotReady     = 3
	exitCodeSyncAlreadyRunning = 4
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
	case "schedule":
		err = runSchedule(args)
	case "smoke-check":
		err = runSmokeCheck(args)
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
		os.Exit(exitCodeForError(err))
	}
}

// exitCodeForError maps a returned command error to a process exit code.
// Every command keeps its existing exitCodeGenericError (1) behavior for
// any error that isn't one of the two scheduled-sync-specific sentinels
// below - see those consts' doc comment for why they exist.
func exitCodeForError(err error) int {
	switch {
	case errors.Is(err, scraper.ErrTUFastNotReady):
		return exitCodeTUFastNotReady
	case errors.Is(err, synclock.ErrHeld):
		return exitCodeSyncAlreadyRunning
	default:
		return exitCodeGenericError
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
	fmt.Println("  2. Optional but recommended: install TU-Fast for automatic 2FA on every")
	fmt.Println("     login (one-time, one click) - open the GUI's Settings -> \"Set up TU-Fast\"")
	fmt.Println("     (/tufast-setup) page, or see docs/browser-profile-strategy.md. Skipping")
	fmt.Println("     this is fine too - login just needs manual 2FA each time instead.")
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
	scraper.EnsurePlaywrightBrowsersPath()
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
	fmt.Println("  2. Optional but recommended: install TU-Fast for automatic 2FA on every")
	fmt.Println("     login (one-time, one click) - open the GUI's Settings -> \"Set up TU-Fast\"")
	fmt.Println("     (/tufast-setup) page, or see docs/browser-profile-strategy.md. Skipping")
	fmt.Println("     this is fine too - login just needs manual 2FA each time instead.")
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

	checkLoginProfileHealth()

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

// checkLoginProfileHealth performs the filesystem-only pre-flight checks
// described in docs/browser-profile-strategy.md's "Health-check design"
// section, now unconditionally against the single hardcoded dedicated login
// profile (scraper.LoginProfileDir, ~/.opal-downloader/login-profile) -
// there is no longer a configurable browser_user_data_dir to gate this on,
// since Chromium (Playwright's bundled build) launched against this one
// profile is the only login path opal-downloader has (queue task
// chromium-only-login-remove-real-browser). No browser is launched; this
// only stats a few paths, keeping `status` fast and offline.
func checkLoginProfileHealth() {
	profileDir, err := scraper.LoginProfileDir()
	if err != nil {
		fmt.Println()
		fmt.Printf("Could not determine the login profile directory: %v\n", err)
		return
	}

	if _, err := os.Stat(profileDir); err != nil {
		// Not an error: a brand-new install hasn't run `login` yet, so the
		// profile hasn't been created on disk at all - it's created lazily
		// on first login/tufast-setup, not by `init`/`setup`.
		return
	}

	preferencesPath := filepath.Join(profileDir, "Default", "Preferences")
	if _, err := os.Stat(preferencesPath); err != nil {
		fmt.Println()
		fmt.Printf("%s exists but %s wasn't found, so this doesn't look like a real browser profile yet. Run `opal-downloader login` (or use the GUI's /tufast-setup page) to initialize it.\n", profileDir, preferencesPath)
		return
	}

	if !scraper.HasTUFastExtension(profileDir) {
		fmt.Println()
		fmt.Println("Note: TU-Fast extension not detected in the login profile. Logins will need manual 2FA each time. See the GUI's /tufast-setup page (or docs/browser-profile-strategy.md) to install it once.")
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

	sc := scraper.New(credentials.URL, credentials.StateFile)
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
	sectionConcurrency := 0
	visitReport := false
	noSkipEnrollmentSections := false
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
		case "--section-concurrency":
			i++
			if i >= len(args) {
				return fmt.Errorf("--section-concurrency requires a value")
			}
			parsedSection, parseSectionErr := strconv.Atoi(args[i])
			if parseSectionErr != nil || parsedSection <= 0 {
				return fmt.Errorf("--section-concurrency requires a positive integer, got %q", args[i])
			}
			sectionConcurrency = parsedSection
		case "--visit-report":
			visitReport = true
		case "--no-skip-enrollment-sections":
			noSkipEnrollmentSections = true
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
	if sectionConcurrency > 0 {
		loaded.App.SectionConcurrency = sectionConcurrency
	}
	if noSkipEnrollmentSections {
		loaded.App.SkipEnrollmentSections = false
	}

	// --visit-report only reads the persistent cross-run visit-effectiveness
	// log built up by prior list/sync runs (see internal/visitlog) - it does
	// not open a browser or touch OPAL at all, so it works offline and
	// without a login/session.
	if visitReport {
		logPath := filepath.Join(loaded.App.DownloadPath, visitlog.FileName)
		records, loadErr := visitlog.Load(logPath)
		if loadErr != nil {
			return loadErr
		}
		fmt.Printf("Section visit-effectiveness report (%s)\n\n", logPath)
		fmt.Print(visitlog.FormatReport(visitlog.Aggregate(records)))
		return nil
	}

	sc := scraper.New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	sc.SetDeveloperMode(devMode)
	sc.SetDebugClicks(debugClicks)
	if debugClicks {
		if logPath, logErr := sc.EnableDebugLogFile(""); logErr != nil {
			fmt.Fprintln(os.Stderr, "Warning: could not open persistent debug log file:", logErr)
		} else {
			fmt.Printf("Debug audit log: %s\n", logPath)
		}
	}
	sc.SetCourseConcurrency(loaded.App.CourseConcurrency)
	sc.SetSectionConcurrency(loaded.App.SectionConcurrency)
	sc.SetSkipEnrollmentSections(loaded.App.SkipEnrollmentSections)
	defer sc.Close()
	defer closeBrowserOnInterrupt(sc)()

	totalTimer := timing.StartTimer()
	err = syncer.ListAvailableCourses(context.Background(), sc)
	if visitLogErr := persistVisitLog(sc, loaded.App.DownloadPath); visitLogErr != nil {
		fmt.Fprintln(os.Stderr, "Warning: failed to update section visit log:", visitLogErr)
	}
	fmt.Println()
	timing.PrintTotalSummary(totalTimer.Elapsed())
	if err == nil {
		printUpdateFooter()
	}
	return err
}

// persistVisitLog appends whatever section-visit records sc accumulated
// during its most recent scrape (see OpalScraper.VisitRecords) into the
// persistent cross-run visit-effectiveness log at
// "<download_path>/.opal-visit-log.json", next to the sync manifest. A no-op
// when sc recorded nothing (e.g. the scrape failed before visiting any
// section) - see visitlog.Append.
func persistVisitLog(sc *scraper.OpalScraper, downloadPath string) error {
	records := sc.VisitRecords()
	if len(records) == 0 {
		return nil
	}
	logPath := filepath.Join(downloadPath, visitlog.FileName)
	return visitlog.Append(logPath, records)
}

func runSync(args []string) (err error) {
	configPath := filepath.Join(projectDir(), "config.yaml")
	force := false
	devMode := false
	profile := false
	debugClicks := false
	concurrency := 0
	courseConcurrency := 0
	sectionConcurrency := 0
	noSkipEnrollmentSections := false
	scheduled := false

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
		case "--scheduled":
			scheduled = true
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
		case "--section-concurrency":
			i++
			if i >= len(args) {
				return fmt.Errorf("--section-concurrency requires a value")
			}
			parsedSection, parseSectionErr := strconv.Atoi(args[i])
			if parseSectionErr != nil || parsedSection <= 0 {
				return fmt.Errorf("--section-concurrency requires a positive integer, got %q", args[i])
			}
			sectionConcurrency = parsedSection
		case "--no-skip-enrollment-sections":
			noSkipEnrollmentSections = true
		default:
			return fmt.Errorf("unknown option for sync: %s", args[i])
		}
	}
	timing.Profile = profile

	// --scheduled runs write a status file after every run (success or
	// failure), so a human has something to read the next time they open
	// the GUI - see docs/scheduled-sync-plan.md section 3 and
	// internal/statuslog's package doc. sc/stats/attemptedLogin/loaded are
	// captured by the deferred write below via closure; sc stays nil,
	// stats stays its zero value, and loaded stays its zero value for any
	// early return before they're set (e.g. EnsureTUFastPresent or
	// config.Load failing), which buildScheduledRunStatus already accounts
	// for - and a zero-value loaded.App.NotifyOnScheduledFailure is false,
	// so the notify step below is simply skipped in that case (opt-in
	// preference can't be known if config never loaded).
	//
	// loaded is declared here (rather than at its original assignment
	// point further down) specifically so this closure can reference it -
	// Go requires a captured variable to already be in lexical scope at the
	// point a closure literal is written, even though the closure itself
	// only runs later.
	var sc *scraper.OpalScraper
	var stats syncer.Stats
	var loaded config.Loaded
	attemptedLogin := false
	runStart := time.Now()

	if scheduled {
		defer func() {
			usedInteractiveLogin := false
			if sc != nil {
				usedInteractiveLogin = sc.UsedInteractiveLogin()
			}
			status := buildScheduledRunStatus(runStart, err, stats, attemptedLogin, usedInteractiveLogin)
			if writeErr := statuslog.WriteDefault(status); writeErr != nil {
				fmt.Fprintln(os.Stderr, "Warning: failed to write scheduled-run status file:", writeErr)
			}

			// Toast notification: opt-in (config.yaml's
			// notify_on_scheduled_failure / the GUI Settings "Notify me if
			// a scheduled sync fails" checkbox), fires only on an actual
			// failure outcome - never partial or success, so a routine
			// file-error hiccup doesn't create notification fatigue. See
			// internal/notify's package doc for the investigation behind
			// this approach (no BurntToast/module dependency needed).
			if status.Outcome == statuslog.OutcomeFailure && loaded.App.NotifyOnScheduledFailure {
				if notifyErr := notify.ScheduledSyncFailure(status.Message); notifyErr != nil && !errors.Is(notifyErr, notify.ErrUnsupported) {
					fmt.Fprintln(os.Stderr, "Warning: failed to show scheduled-sync-failure toast notification:", notifyErr)
				}
			}
		}()
	}

	// --scheduled is what Task Scheduler's registered task runs
	// unattended (see internal/scheduler and `schedule enable`). Fail fast,
	// before touching config or opening a browser, if the dedicated login
	// profile isn't ready for a human-free login: otherwise a session that
	// needs relogin would fall through to ensureSession's interactive
	// branch and block for waitForLoggedInCourseLink's full 300s timeout
	// waiting for a 2FA click that will never come on an unattended
	// machine. See docs/scheduled-sync-plan.md section 2 and
	// scraper.EnsureTUFastPresent's doc comment.
	if scheduled {
		if err = scraper.EnsureTUFastPresent(); err != nil {
			return err
		}
	}

	loaded, err = config.Load(configPath)
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
	if sectionConcurrency > 0 {
		loaded.App.SectionConcurrency = sectionConcurrency
	}
	if noSkipEnrollmentSections {
		loaded.App.SkipEnrollmentSections = false
	}

	sc = scraper.New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	sc.SetDeveloperMode(devMode)
	sc.SetDebugClicks(debugClicks)
	if debugClicks {
		if logPath, logErr := sc.EnableDebugLogFile(""); logErr != nil {
			fmt.Fprintln(os.Stderr, "Warning: could not open persistent debug log file:", logErr)
		} else {
			fmt.Printf("Debug audit log: %s\n", logPath)
		}
	}
	sc.SetCourseConcurrency(loaded.App.CourseConcurrency)
	sc.SetSectionConcurrency(loaded.App.SectionConcurrency)
	sc.SetSkipEnrollmentSections(loaded.App.SkipEnrollmentSections)
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
	attemptedLogin = true
	stats, err = syncer.SyncCourses(context.Background(), sc, loaded.App, force)
	// Persist whatever section visits were recorded regardless of whether
	// the sync itself succeeded - even a failed/partial sync's visit data is
	// useful cross-run history (see internal/visitlog), and this must not
	// clobber the more important sync error return below.
	if visitLogErr := persistVisitLog(sc, loaded.App.DownloadPath); visitLogErr != nil {
		fmt.Fprintln(os.Stderr, "Warning: failed to update section visit log:", visitLogErr)
	}
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

// buildScheduledRunStatus composes the statuslog.Status a `sync --scheduled`
// run should report, given how it went. It is a pure function (no I/O) so
// it can be unit tested directly - see root_test.go - without needing a
// live scraper/browser.
//
// attemptedLogin/usedInteractiveLogin describe what happened, if anything,
// on the way to runErr/stats: attemptedLogin is true once runSync actually
// called syncer.SyncCourses (which is what triggers ensureSession
// internally), and usedInteractiveLogin is *sc.UsedInteractiveLogin() at
// that point. The two known-early-failure sentinels
// (scraper.ErrTUFastNotReady, synclock.ErrHeld) are special-cased to
// LoginPathUnknown regardless of those two flags, since both can fail
// before (TUFast pre-flight) or without necessarily going through (sync
// lock contention) ensureSession's own branch logic.
func buildScheduledRunStatus(now time.Time, runErr error, stats syncer.Stats, attemptedLogin, usedInteractiveLogin bool) statuslog.Status {
	status := statuslog.Status{
		Timestamp:       now,
		FilesDownloaded: stats.Downloaded,
		FilesSkipped:    stats.Skipped,
		FilesErrored:    stats.Errors,
	}

	switch {
	case errors.Is(runErr, scraper.ErrTUFastNotReady), errors.Is(runErr, synclock.ErrHeld):
		status.LoginPath = statuslog.LoginPathUnknown
	case attemptedLogin && usedInteractiveLogin:
		status.LoginPath = statuslog.LoginPathInteractiveRelogin
	case attemptedLogin:
		status.LoginPath = statuslog.LoginPathHeadlessOnly
	default:
		status.LoginPath = statuslog.LoginPathUnknown
	}

	switch {
	case errors.Is(runErr, synclock.ErrHeld):
		// Lock contention, not a failure: another opal-downloader process
		// (most likely the GUI running its own "Sync now" job) already had
		// the sync in hand when this run tried to start. Reported separately
		// from OutcomeFailure so it doesn't trigger the failure toast or the
		// GUI banner - see statuslog.OutcomeSkipped's doc comment.
		status.Outcome = statuslog.OutcomeSkipped
		status.Message = "Skipped: " + statuslog.SanitizeMessage(runErr.Error()) + " - another opal-downloader process was already syncing."
	case runErr != nil:
		status.Outcome = statuslog.OutcomeFailure
		status.Message = statuslog.SanitizeMessage(runErr.Error())
	case stats.Errors > 0:
		status.Outcome = statuslog.OutcomePartial
		status.Message = fmt.Sprintf("Synced with %d file error(s) (%d downloaded, %d skipped).", stats.Errors, stats.Downloaded, stats.Skipped)
	default:
		status.Outcome = statuslog.OutcomeSuccess
		status.Message = fmt.Sprintf("Synced successfully: %d downloaded, %d skipped.", stats.Downloaded, stats.Skipped)
	}

	return status
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

	sc := scraper.New(credentials.URL, credentials.StateFile)
	sc.SetDeveloperMode(devMode)
	defer sc.Close()
	defer closeBrowserOnInterrupt(sc)()

	if err := sc.DumpPageLinks(targetURL, outputPath); err != nil {
		return err
	}

	fmt.Printf("Link dump written to: %s\n", outputPath)
	return nil
}

// runSmokeCheck implements `opal-downloader smoke-check` - queue task
// add-scraper-smoke-check's local, on-demand answer to "does the crawl/sync
// still actually work against real OPAL", meant to be run by a
// maintainer or queue-run right after touching internal/scraper or
// internal/syncer, reusing the existing saved session_state_file exactly
// like `list`/`sync` already do.
//
// By default this only runs a single read-only discovery pass (see
// internal/smokecheck's package doc comment for the two signals checked:
// login/session validity and discovery-count sanity against a
// locally-persisted baseline) - deliberately the same "list is read-only,
// safe to run often" property docs/manual-setup-checklist.md already
// documents for `list` itself. --full-sync opts into an additional real
// download pass against a disposable scratch directory (never the user's
// real download_path/manifest) to also exercise file-download reachability,
// at the cost of being slower and touching the network's file bytes, not
// just page/DOM content.
//
// Like `sync --scheduled`, this calls scraper.EnsureTUFastPresent before
// ever touching a saved session: smoke-check is meant to be run
// unattended (by queue-run, or by a maintainer who has walked away), so a
// session that would need a *human* 2FA click must fail fast here instead
// of ensureSession silently blocking for up to 300s waiting for a click
// that will never come. This does mean smoke-check requires TU-Fast to be
// set up in the dedicated login profile, same as scheduled sync - a
// deliberate scope match with the only other "runs with nobody watching"
// path this codebase already has, not a new concept.
func runSmokeCheck(args []string) error {
	configPath := filepath.Join(projectDir(), "config.yaml")
	threshold := smokecheck.DefaultDropThresholdPercent
	resetBaseline := false
	fullSync := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i >= len(args) {
				return fmt.Errorf("--config requires a path")
			}
			configPath = args[i]
		case "--threshold":
			i++
			if i >= len(args) {
				return fmt.Errorf("--threshold requires a value")
			}
			parsed, parseErr := strconv.ParseFloat(args[i], 64)
			if parseErr != nil || parsed <= 0 {
				return fmt.Errorf("--threshold requires a positive number (percent), got %q", args[i])
			}
			threshold = parsed
		case "--reset-baseline":
			resetBaseline = true
		case "--full-sync":
			fullSync = true
		default:
			return fmt.Errorf("unknown option for smoke-check: %s", args[i])
		}
	}

	// Fail fast (no network call, filesystem-only - same property
	// EnsureTUFastPresent already documents) rather than risk hanging for a
	// human who isn't there. See this function's doc comment.
	if err := scraper.EnsureTUFastPresent(); err != nil {
		return err
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		return err
	}
	printConfigWarnings(loaded.App)

	sc := scraper.New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()
	defer closeBrowserOnInterrupt(sc)()

	fmt.Println("Running discovery (read-only, same as `list`)...")
	result, err := smokecheck.Run(context.Background(), sc, smokecheck.Options{
		ThresholdPercent: threshold,
		ResetBaseline:    resetBaseline,
	})
	if err != nil {
		// err may wrap a raw Playwright/network error (e.g. unreachable
		// opal_url) - sanitize before printing, same bar as any other
		// message this command prints (see statuslog.SanitizeMessage's doc
		// comment on why this project always routes error text through it).
		return fmt.Errorf("smoke-check: discovery failed: %s", statuslog.SanitizeMessage(err.Error()))
	}

	fmt.Println()
	fmt.Println("Login/session:", result.LoginMessage)
	fmt.Println("Discovery:", result.DiscoveryMessage)
	if result.BaselineWriteWarning != "" {
		fmt.Fprintln(os.Stderr, "Warning: could not save smoke-check baseline:", result.BaselineWriteWarning)
	}
	courseNames := make([]string, 0, len(result.Courses))
	for name := range result.Courses {
		courseNames = append(courseNames, name)
	}
	sort.Strings(courseNames)
	for _, name := range courseNames {
		fmt.Printf("  [%s] (%d files)\n", name, result.Courses[name])
	}

	if fullSync {
		fmt.Println()
		if err := runSmokeCheckFullSync(context.Background(), sc, loaded.App); err != nil {
			return fmt.Errorf("smoke-check: --full-sync failed: %s", statuslog.SanitizeMessage(err.Error()))
		}
	}

	fmt.Println()
	if !result.Pass {
		return fmt.Errorf("smoke-check FAILED: %s", result.DiscoveryMessage)
	}
	fmt.Println("smoke-check PASSED")
	return nil
}

// runSmokeCheckFullSync is --full-sync's opt-in deeper check: a real
// syncer.SyncCourses download pass, proving files are actually reachable and
// downloadable (not just discoverable), run against a disposable scratch
// directory under the OS temp dir rather than the user's real
// download_path/manifest.
//
// Deliberately NOT a copy of the user's full config.App: DefaultCourseFolder/
// CourseFolders/UseSectionSubfolders/SectionFolderNames/SubfolderDestinations
// are all left unset here, even though the real loaded config may set them,
// because subfolder_destinations in particular can redirect a course's files
// to an arbitrary absolute path anywhere on disk (see
// docs/OPERATIONS.md's per-subfolder-download-destinations section) - copying
// it verbatim would defeat the "this only ever touches a scratch directory"
// guarantee this flag exists to provide. Every file this pass downloads
// lands flatly at <scratchDir>/<course>/<file>.
func runSmokeCheckFullSync(ctx context.Context, sc *scraper.OpalScraper, appCfg config.App) error {
	scratchDir, err := os.MkdirTemp("", "opal-smoke-check-")
	if err != nil {
		return fmt.Errorf("creating scratch download directory: %w", err)
	}
	defer os.RemoveAll(scratchDir)

	fmt.Printf("Running a real sync into a scratch directory (%s) - --full-sync was requested...\n", scratchDir)

	scratchCfg := config.App{
		DownloadPath:           scratchDir,
		Courses:                appCfg.Courses,
		DownloadConcurrency:    appCfg.DownloadConcurrency,
		CourseConcurrency:      appCfg.CourseConcurrency,
		SkipEnrollmentSections: appCfg.SkipEnrollmentSections,
	}

	stats, err := syncer.SyncCourses(ctx, sc, scratchCfg, false)
	if err != nil {
		return err
	}
	fmt.Printf("--full-sync done. downloaded=%d skipped=%d errors=%d\n", stats.Downloaded, stats.Skipped, stats.Errors)
	if stats.Errors > 0 {
		return fmt.Errorf("%d file(s) failed to download", stats.Errors)
	}
	return nil
}

// runSchedule implements `opal-downloader schedule enable|disable|status`,
// the CLI counterpart to the GUI Settings page's "Enable daily automatic
// sync" toggle (internal/gui/schedule.go) - both call the same
// internal/scheduler package, so either front-end sees the other's changes
// (there's exactly one underlying Windows Task Scheduler task).
func runSchedule(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("schedule requires a subcommand: enable, disable, or status")
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "enable":
		timeArg := scheduler.DefaultTime
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case "--time":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--time requires a value, e.g. --time 06:00")
				}
				timeArg = rest[i]
			default:
				return fmt.Errorf("unknown option for schedule enable: %s", rest[i])
			}
		}
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolving this executable's own path: %w", err)
		}
		// Refuse to register a path that will not survive (a `go run`
		// build-cache binary, a temp directory) - the resulting task fails
		// silently once that file is cleaned up, and the user has no reason
		// to suspect their scheduled sync stopped. See
		// scheduler.ErrEphemeralExecutable.
		if err := scheduler.CheckExecutableStable(exePath); err != nil {
			return err
		}
		if err := scheduler.Enable(exePath, timeArg); err != nil {
			return err
		}
		fmt.Printf("Scheduled daily sync enabled: Task Scheduler will run %q sync --scheduled every day at %s (catches up automatically if the machine was off/asleep).\n", exePath, timeArg)
		return nil

	case "disable":
		if len(rest) > 0 {
			return fmt.Errorf("unknown option for schedule disable: %s", rest[0])
		}
		if err := scheduler.Disable(); err != nil {
			return err
		}
		fmt.Println("Scheduled daily sync disabled (Task Scheduler entry removed).")
		return nil

	case "status":
		if len(rest) > 0 {
			return fmt.Errorf("unknown option for schedule status: %s", rest[0])
		}
		info, err := scheduler.Status()
		if err != nil {
			return err
		}
		if !info.Registered {
			fmt.Println("Scheduled daily sync: not enabled.")
			return nil
		}
		fmt.Printf("Scheduled daily sync: enabled, daily at %s.\n", info.Time)
		return nil

	default:
		return fmt.Errorf("unknown schedule subcommand: %s (expected enable, disable, or status)", sub)
	}
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

	return gui.Run(gui.Options{
		Port:       port,
		ConfigPath: configPath,
		Version:    buildVersion,
	})
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
	fmt.Println("  schedule    Enable/disable/check a daily automatic sync via Windows Task Scheduler")
	fmt.Println("  smoke-check Local, on-demand check that the crawl still actually works against real OPAL (read-only by default)")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  init --config <path>")
	fmt.Println("  setup --config <path>")
	fmt.Println("  status --config <path>")
	fmt.Println("  gui [--port <port>] [--config <path>]")
	fmt.Println("  login --config <path> [--dev]")
	fmt.Println("  list --config <path> [--dev] [--profile] [--debug-clicks] [--course-concurrency <n>] [--visit-report] [--no-skip-enrollment-sections]")
	fmt.Println("  sync --config <path> [--force] [--dev] [--profile] [--debug-clicks] [--concurrency <n>] [--course-concurrency <n>] [--no-skip-enrollment-sections] [--scheduled]")
	fmt.Println("  dump-links --url <url> [--out <path>] [--config <path>] [--dev]")
	fmt.Println("  schedule enable [--time HH:MM] | disable | status")
	fmt.Println("  smoke-check --config <path> [--threshold <percent>] [--reset-baseline] [--full-sync]")
	fmt.Println()
	fmt.Println("  --profile               Print granular per-course/per-file timings in addition to the summary")
	fmt.Println("  --debug-clicks          Log every click and navigation/interactive-link wait with timestamp, page URL, selector, and reason (diagnostic tool)")
	fmt.Println("  --concurrency n         Max concurrent file downloads for sync (default 3, overrides config.yaml)")
	fmt.Println("  --course-concurrency n  Max concurrent courses crawled during discovery (default 2, overrides config.yaml)")
	fmt.Println("  --visit-report          (list only) Print the cross-run section visit-effectiveness report from .opal-visit-log.json and exit - no browser/login needed")
	fmt.Println("  --no-skip-enrollment-sections  Visit every OPAL 'Einschreibung' (enrollment/sign-up) course-node section instead of skipping it (overrides config.yaml's skip_enrollment_sections: true default) - escape hatch if the structural skip is ever wrong for your OPAL instance")
	fmt.Println("  --scheduled             (sync only) Unattended mode for Task Scheduler: fails fast if TU-Fast isn't set up in the dedicated login profile instead of waiting for a 2FA click that will never come. Also enforced: a single-instance overlap-guard lock, checked on every sync (scheduled or manual/GUI), so two runs never race.")
	fmt.Println("  --threshold n           (smoke-check only) Percent the discovered file count may drop below the saved baseline before failing (default 20)")
	fmt.Println("  --reset-baseline        (smoke-check only) Discard the saved baseline and record the current file count as the new starting point")
	fmt.Println("  --full-sync             (smoke-check only) Also run a real sync into a disposable scratch directory, to test file-download reachability in addition to discovery")
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
