package gui

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// tuFastWebStoreURL is TU-Fast's Chrome Web Store listing, verified live
// 2026-07-12 (resolves to "TUfast TU Dresden" by Oliver Hausdoerfer) - see
// the task that added this page. Used only to open a browser tab for the
// user to click "Add to Chrome"/"Add to Brave" themselves; nothing here
// automates the install itself (a consent action - see
// docs/browser-profile-strategy.md).
const tuFastWebStoreURL = "https://chromewebstore.google.com/detail/" + scraper.TUFastExtensionID

// defaultDedicatedProfileDir returns the recommended one-time dedicated
// browser profile location (~/.opal-downloader/login-profile, see
// docs/browser-profile-strategy.md), or "" if the home directory can't be
// resolved.
func defaultDedicatedProfileDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".opal-downloader", "login-profile")
}

// detectBrowserExecutable looks for a Brave or Chrome install in common
// per-OS locations. Returns "" if nothing is found - callers must fall back
// to asking the user to fill in "Browser executable" in Settings first.
// This is best-effort convenience only (cuts a click for the common case);
// it is never required for opal-downloader's core login/sync path, which
// can always fall back to Playwright's bundled browser.
func detectBrowserExecutable() string {
	switch runtime.GOOS {
	case "windows":
		var candidates []string
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")} {
			if base == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(base, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
				filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"),
			)
		}
		for _, c := range candidates {
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				return c
			}
		}
		return ""
	case "darwin":
		for _, c := range []string{
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		} {
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				return c
			}
		}
		return ""
	default:
		for _, name := range []string{"brave-browser", "brave", "google-chrome", "chromium-browser", "chromium"} {
			if path, err := exec.LookPath(name); err == nil {
				return path
			}
		}
		return ""
	}
}

type tuFastSetupPageData struct {
	TargetUserDataDir string
	TargetProfileDir  string
	SuggestedDir      string
	DetectedBrowser   string

	OpenResult   string
	OpenResultOK bool

	CopySourceUserDataDir string
	CopySourceProfileDir  string
	CopyResult            string
	CopyResultOK          bool
}

func (s *server) loadTUFastSetupPageData() tuFastSetupPageData {
	credentials, _ := config.LoadCredentials(s.configPath)
	detect := s.detectBrowser
	if detect == nil {
		detect = detectBrowserExecutable
	}
	return tuFastSetupPageData{
		TargetUserDataDir: credentials.BrowserUserDataDir,
		TargetProfileDir:  credentials.BrowserProfileDir,
		SuggestedDir:      defaultDedicatedProfileDir(),
		DetectedBrowser:   detect(),
	}
}

func (s *server) handleTUFastSetupPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	data := s.loadTUFastSetupPageData()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tuFastSetupTemplate.Execute(w, data)
}

// handleTUFastSetupOpen auto-creates the configured (or suggested) dedicated
// profile directory and launches a browser against it directly at TU-Fast's
// Chrome Web Store listing - cutting the manual-setup-checklist.md "Step 0"
// flow down from "create a folder yourself, launch Brave with a
// --user-data-dir flag yourself, search the Web Store yourself" to one
// click. Installing the extension and completing the OPAL/Shibboleth login
// itself stay genuinely manual (consent/identity actions) - this only opens
// the window and gets it to the right page.
func (s *server) handleTUFastSetupOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()

	data := s.loadTUFastSetupPageData()

	targetDir := strings.TrimSpace(r.FormValue("target_user_data_dir"))
	if targetDir == "" {
		targetDir = data.TargetUserDataDir
	}
	if targetDir == "" {
		targetDir = data.SuggestedDir
	}
	if targetDir == "" {
		data.OpenResult = "Could not determine a profile directory to use (and couldn't resolve your home directory to suggest one). Enter a path manually."
		data.OpenResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}

	browserExe := strings.TrimSpace(r.FormValue("browser_executable"))
	if browserExe == "" {
		detect := s.detectBrowser
		if detect == nil {
			detect = detectBrowserExecutable
		}
		browserExe = detect()
	}
	if browserExe == "" {
		data.OpenResult = "Could not find a Brave or Chrome install automatically. Set \"Browser executable\" in Settings to your browser's .exe path, then try again."
		data.OpenResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		data.OpenResult = fmt.Sprintf("Could not create %s: %s", targetDir, err)
		data.OpenResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}

	launch := s.launchBrowserAt
	if launch == nil {
		launch = defaultLaunchBrowserAt
	}
	if err := launch(browserExe, targetDir, "Default", tuFastWebStoreURL); err != nil {
		data.OpenResult = fmt.Sprintf("Created %s, but could not launch the browser: %s. Launch it yourself: %s --user-data-dir=\"%s\" --profile-directory=Default %s", targetDir, err, browserExe, targetDir, tuFastWebStoreURL)
		data.OpenResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}

	data.OpenResult = fmt.Sprintf(
		"Opened a new browser window against %s, at TU-Fast's Chrome Web Store listing. In that window: click \"Add to Chrome\"/\"Add to Brave\", then log into OPAL/Shibboleth once to complete 2FA/device registration. When done, come back to Settings and set \"Browser user data directory\" to %s (\"Browser profile directory\": Default) and save.",
		targetDir, targetDir)
	data.OpenResultOK = true
	s.renderTUFastSetup(w, data)
}

// handleTUFastSetupCopy runs the (optional, explicitly user-triggered) TU-Fast
// login-data transplant: see scraper.TransplantTUFastLoginData's doc comment
// for exactly what is and isn't copied, and
// docs/browser-profile-strategy.md's "Transplanting TU-Fast login data"
// section for the investigation this is based on.
func (s *server) handleTUFastSetupCopy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()

	data := s.loadTUFastSetupPageData()
	data.CopySourceUserDataDir = strings.TrimSpace(r.FormValue("source_user_data_dir"))
	data.CopySourceProfileDir = strings.TrimSpace(r.FormValue("source_profile_directory"))

	if data.TargetUserDataDir == "" {
		data.CopyResult = "Set \"Browser user data directory\" in Settings first (this copies into the profile configured there)."
		data.CopyResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}

	result, err := scraper.TransplantTUFastLoginData(data.CopySourceUserDataDir, data.CopySourceProfileDir, data.TargetUserDataDir, data.TargetProfileDir)
	if err != nil {
		data.CopyResult = err.Error()
		data.CopyResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}

	msg := fmt.Sprintf("Copied: %s.", strings.Join(result.CopiedDirs, ", "))
	if len(result.BackedUpDirs) > 0 {
		msg += fmt.Sprintf(" Previous data was kept, not deleted, at: %s.", strings.Join(result.BackedUpDirs, ", "))
	}
	msg += " Restart the browser for the target profile (if it's open) to pick this up."
	data.CopyResult = msg
	data.CopyResultOK = true
	s.renderTUFastSetup(w, data)
}

func (s *server) renderTUFastSetup(w http.ResponseWriter, data tuFastSetupPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tuFastSetupTemplate.Execute(w, data)
}

// defaultLaunchBrowserAt is the production implementation of
// server.launchBrowserAt: starts the browser as a detached process (never
// Wait()'d - the user drives that window themselves from here), matching
// the same "start and don't wait" pattern as defaultLaunchInstaller in
// gui.go.
func defaultLaunchBrowserAt(browserExecutable, userDataDir, profileDirectory, url string) error {
	args := []string{"--user-data-dir=" + userDataDir}
	if profileDirectory != "" {
		args = append(args, "--profile-directory="+profileDirectory)
	}
	args = append(args, url)
	cmd := exec.Command(browserExecutable, args...)
	return cmd.Start()
}

var tuFastSetupTemplate = template.Must(template.New("tufast-setup").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Opal Downloader - TU-Fast browser profile setup</title>
<style>` + pageStyle + `
	input[type=text] { width: 100%; box-sizing: border-box; padding: 0.4rem 0.5rem; border: 1px solid #ccc; border-radius: 4px; font: inherit; margin-bottom: 0.5rem; }
	.field { margin-bottom: 1rem; }
	label { display: block; font-weight: 600; margin-bottom: 0.25rem; }
</style>
</head>
<body>
	<h1>TU-Fast browser profile setup</h1>
	<p class="hint">
		A dedicated, never-used-for-anything-else browser profile keeps
		TU-Fast auto-login working without ever locking your everyday
		browser. See docs/browser-profile-strategy.md for the full
		background.
	</p>

	<h2>1. Open a fresh profile at TU-Fast's install page</h2>
	<p class="hint">
		Creates the folder below if it doesn't exist yet, and opens a new
		browser window there, straight at TU-Fast's Chrome Web Store
		listing. Installing the extension and completing the OPAL/Shibboleth
		2FA/device-registration login are consent/identity actions - do
		those two steps yourself in the window that opens; nothing here
		clicks through them for you.
	</p>

	{{if .OpenResult}}<div class="status {{if .OpenResultOK}}ok{{else}}warn{{end}}">{{.OpenResult}}</div>{{end}}

	<form method="post" action="/tufast-setup/open">
		<div class="field">
			<label for="target_user_data_dir">Profile directory</label>
			<input type="text" id="target_user_data_dir" name="target_user_data_dir" value="{{if .TargetUserDataDir}}{{.TargetUserDataDir}}{{else}}{{.SuggestedDir}}{{end}}">
			<p class="hint">Recommended default: <code>{{.SuggestedDir}}</code>. Change this only if you want the dedicated profile somewhere else.</p>
		</div>
		<div class="field">
			<label for="open_browser_executable">Browser executable</label>
			<input type="text" id="open_browser_executable" name="browser_executable" value="{{.DetectedBrowser}}">
			<p class="hint">{{if .DetectedBrowser}}Detected automatically - change if wrong.{{else}}Could not detect Brave/Chrome automatically - enter the path to brave.exe/chrome.exe.{{end}}</p>
		</div>
		<button type="submit">Create folder &amp; open TU-Fast in the Chrome Web Store</button>
	</form>

	<h2>2. Optional: copy an existing TU-Fast login instead of logging in again</h2>
	<p class="hint">
		If TU-Fast is already installed and logged in in another browser
		profile on <strong>this same computer</strong> (e.g. your everyday
		Brave/Chrome), this can copy just its stored login/2FA data into the
		profile configured in Settings (<code>{{if .TargetUserDataDir}}{{.TargetUserDataDir}}{{else}}(not set - configure it in Settings first){{end}}</code>) -
		skipping a second manual login. TU-Fast must already be installed
		(via step 1 above) in the target profile first; this only copies its
		stored data, never the extension install itself.
	</p>
	<p class="hint">
		This is a 100% local, offline file copy - nothing is uploaded or
		sent anywhere. It <strong>overwrites</strong> the target profile's
		current TU-Fast login data (any existing data is renamed aside, not
		deleted, so it can be restored by hand if needed). Only works
		reliably between profiles on the same physical machine - see
		docs/browser-profile-strategy.md for why.
	</p>

	{{if .CopyResult}}<div class="status {{if .CopyResultOK}}ok{{else}}warn{{end}}">{{.CopyResult}}</div>{{end}}

	<form method="post" action="/tufast-setup/copy">
		<div class="field">
			<label for="source_user_data_dir">Source browser's user data directory</label>
			<input type="text" id="source_user_data_dir" name="source_user_data_dir" value="{{.CopySourceUserDataDir}}" placeholder="e.g. your everyday Brave/Chrome profile root">
		</div>
		<div class="field">
			<label for="source_profile_directory">Source profile directory (optional)</label>
			<input type="text" id="source_profile_directory" name="source_profile_directory" value="{{.CopySourceProfileDir}}" placeholder="Default">
		</div>
		<button type="submit">Copy TU-Fast login data</button>
	</form>

	<p class="back"><a href="/settings">&larr; Back to Settings</a> &middot; <a href="/">Home</a></p>
</body>
</html>
`))
