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
)

func Execute() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
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
		fmt.Println("opal-downloader 0.1.0")
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
			return fmt.Errorf("unknown option for list: %s", args[i])
		}
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		return err
	}

	sc := scraper.New(loaded.Credentials.URL, loaded.Credentials.StateFile, loaded.Credentials.BrowserExecutable, loaded.Credentials.BrowserUserDataDir, loaded.Credentials.BrowserProfileDir)
	sc.SetDeveloperMode(devMode)
	defer sc.Close()
	return syncer.ListAvailableCourses(sc)
}

func runSync(args []string) error {
	configPath := filepath.Join(projectDir(), "config.yaml")
	force := false
	devMode := false

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
		default:
			return fmt.Errorf("unknown option for sync: %s", args[i])
		}
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		return err
	}

	sc := scraper.New(loaded.Credentials.URL, loaded.Credentials.StateFile, loaded.Credentials.BrowserExecutable, loaded.Credentials.BrowserUserDataDir, loaded.Credentials.BrowserProfileDir)
	sc.SetDeveloperMode(devMode)
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

	stats, err := syncer.SyncCourses(sc, loaded.App, force)
	if err != nil {
		return err
	}
	fmt.Printf("\nDone. downloaded=%d skipped=%d errors=%d\n", stats.Downloaded, stats.Skipped, stats.Errors)
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
	fmt.Println("  list --config <path> [--dev]")
	fmt.Println("  sync --config <path> [--force] [--dev]")
	fmt.Println("  dump-links --url <url> [--out <path>] [--config <path>] [--dev]")
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
