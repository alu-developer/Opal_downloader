package opaldownloader

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/gui"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/syncer"
	"github.com/alu-developer/opal-downloader/internal/timing"
)

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

func Execute() {
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
	fmt.Println("  2. Run: opal-downloader login")
	fmt.Println("  3. Run: opal-downloader sync")
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
	cmd := exec.Command("go", "run", "github.com/mxschmitt/playwright-go/cmd/playwright@v0.6100.0", "install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
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
	fmt.Println("  2. Run: opal-downloader login")
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
	return nil
}

func runList(args []string) error {
	configPath := filepath.Join(projectDir(), "config.yaml")
	devMode := false
	profile := false
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
	sc.SetCourseConcurrency(loaded.App.CourseConcurrency)
	defer sc.Close()

	totalTimer := timing.StartTimer()
	err = syncer.ListAvailableCourses(sc)
	fmt.Println()
	timing.PrintTotalSummary(totalTimer.Elapsed())
	return err
}

func runSync(args []string) error {
	configPath := filepath.Join(projectDir(), "config.yaml")
	force := false
	devMode := false
	profile := false
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
	sc.SetCourseConcurrency(loaded.App.CourseConcurrency)
	defer sc.Close()

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

	return gui.Run(gui.Options{Port: port, ConfigPath: configPath})
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
	fmt.Println("  list --config <path> [--dev] [--profile] [--course-concurrency <n>]")
	fmt.Println("  sync --config <path> [--force] [--dev] [--profile] [--concurrency <n>] [--course-concurrency <n>]")
	fmt.Println("  dump-links --url <url> [--out <path>] [--config <path>] [--dev]")
	fmt.Println()
	fmt.Println("  --profile               Print granular per-course/per-file timings in addition to the summary")
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
