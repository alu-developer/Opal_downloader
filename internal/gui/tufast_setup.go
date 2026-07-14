package gui

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// tuFastWebStoreURL is TU-Fast's Chrome Web Store listing, verified live
// 2026-07-12 (resolves to "TUfast TU Dresden" by Oliver Hausdoerfer) - see
// the task that added this page. Used only to open a browser tab for the
// user to click "Add to Chrome" themselves; nothing here automates the
// install itself (a consent action - see docs/browser-profile-strategy.md).
const tuFastWebStoreURL = "https://chromewebstore.google.com/detail/" + scraper.TUFastExtensionID

type tuFastSetupPageData struct {
	// ProfileDir is the single, hardcoded dedicated login profile directory
	// (~/.opal-downloader/login-profile, see scraper.LoginProfileDir) -
	// there is nothing left to configure here since Chromium (Playwright's
	// bundled build) launched against this one profile is the only login
	// path opal-downloader has.
	ProfileDir    string
	ProfileDirErr string

	OpenResult   string
	OpenResultOK bool

	CopySourceUserDataDir string
	CopySourceProfileDir  string
	CopyResult            string
	CopyResultOK          bool
}

func (s *server) loadTUFastSetupPageData() tuFastSetupPageData {
	resolve := s.loginProfileDir
	if resolve == nil {
		resolve = scraper.LoginProfileDir
	}
	dir, err := resolve()
	data := tuFastSetupPageData{ProfileDir: dir}
	if err != nil {
		data.ProfileDirErr = err.Error()
	}
	return data
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

// handleTUFastSetupOpen auto-creates the dedicated login profile directory
// (if it doesn't exist yet) and launches Playwright's bundled Chromium
// directly at TU-Fast's Chrome Web Store listing - cutting the
// manual-setup-checklist.md "Step 0" flow down to one click. Installing the
// extension and completing the OPAL/Shibboleth login itself stay genuinely
// manual (consent/identity actions) - this only opens the window and gets
// it to the right page.
func (s *server) handleTUFastSetupOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()

	data := s.loadTUFastSetupPageData()
	if data.ProfileDir == "" {
		data.OpenResult = fmt.Sprintf("Could not determine the login profile directory: %s", data.ProfileDirErr)
		data.OpenResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}

	if err := os.MkdirAll(data.ProfileDir, 0o755); err != nil {
		data.OpenResult = fmt.Sprintf("Could not create %s: %s", data.ProfileDir, err)
		data.OpenResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}

	launch := s.launchBrowserAt
	if launch == nil {
		launch = defaultLaunchBrowserAt
	}
	if err := launch(tuFastWebStoreURL); err != nil {
		data.OpenResult = fmt.Sprintf("Created %s, but could not launch the browser: %s", data.ProfileDir, err)
		data.OpenResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}

	data.OpenResult = fmt.Sprintf(
		"Opened Chromium against the dedicated login profile at %s, at TU-Fast's Chrome Web Store listing. In that window: click \"Add to Chrome\", then log into OPAL/Shibboleth once to complete 2FA/device registration. After that, every future login/sync auto-completes - nothing else to configure.",
		data.ProfileDir)
	data.OpenResultOK = true
	s.renderTUFastSetup(w, data)
}

// handleTUFastSetupCopy runs the (optional, explicitly user-triggered) TU-Fast
// login-data transplant: see scraper.TransplantTUFastLoginData's doc comment
// for exactly what is and isn't copied, and
// docs/browser-profile-strategy.md's "Transplanting TU-Fast login data"
// section for the investigation this is based on. The source is still a
// real, separately-installed browser profile (e.g. the user's everyday
// Brave/Chrome, if TU-Fast is already logged in there) - only the *target*
// is now always the single hardcoded dedicated login profile, never
// user-configurable.
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

	if data.ProfileDir == "" {
		data.CopyResult = fmt.Sprintf("Could not determine the login profile directory: %s", data.ProfileDirErr)
		data.CopyResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}

	result, err := scraper.TransplantTUFastLoginData(data.CopySourceUserDataDir, data.CopySourceProfileDir, data.ProfileDir, "Default")
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
// server.launchBrowserAt: launches a visible Playwright Chromium window
// against the dedicated login profile (via scraper.OpalScraper.
// OpenInteractiveBrowserAt, the same launchBrowser path login/sync use, with
// extensions enabled) and navigates it to url. Unlike the pre-Chromium-only
// design, this never shells out to a real installed Brave/Chrome executable
// - Chromium (Playwright's bundled build) is the only browser
// opal-downloader ever launches.
//
// Deliberately does not close the returned scraper/browser: the window has
// to stay open for the user to install TU-Fast and log in by hand. It keeps
// running as a child process of this GUI process (guarded by
// internal/procguard, so it still dies if opal-downloader itself is killed)
// until the user closes the window or the whole process exits - matching
// the existing "a separate browser window opens and outlives this page"
// disclaimer already shown on the landing/login pages.
func defaultLaunchBrowserAt(url string) error {
	sc := scraper.New(config.DefaultOPALURL, config.DefaultStateFile)
	if err := sc.OpenInteractiveBrowserAt(url); err != nil {
		_ = sc.Close()
		return err
	}
	return nil
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
		opal-downloader always logs in and syncs using Playwright's bundled
		Chromium against one dedicated profile at
		<code>{{.ProfileDir}}</code> - never your everyday browser. See
		docs/browser-profile-strategy.md for the full background.
	</p>

	<h2>1. Open Chromium at TU-Fast's install page</h2>
	<p class="hint">
		Creates the profile directory above if it doesn't exist yet, and
		opens a Chromium window there, straight at TU-Fast's Chrome Web
		Store listing. Installing the extension and completing the
		OPAL/Shibboleth 2FA/device-registration login are consent/identity
		actions - do those two steps yourself in the window that opens;
		nothing here clicks through them for you.
	</p>

	{{if .OpenResult}}<div class="status {{if .OpenResultOK}}ok{{else}}warn{{end}}">{{.OpenResult}}</div>{{end}}

	<form method="post" action="/tufast-setup/open">
		<button type="submit">Open Chromium in the Chrome Web Store</button>
	</form>

	<h2>2. Optional: copy an existing TU-Fast login instead of logging in again</h2>
	<p class="hint">
		If TU-Fast is already installed and logged in in another browser
		profile on <strong>this same computer</strong> (e.g. your everyday
		Brave/Chrome), this can copy just its stored login/2FA data into the
		dedicated login profile above - skipping a second manual login.
		TU-Fast must already be installed (via step 1 above) in the
		dedicated profile first; this only copies its stored data, never the
		extension install itself.
	</p>
	<p class="hint">
		This is a 100% local, offline file copy - nothing is uploaded or
		sent anywhere. It <strong>overwrites</strong> the dedicated profile's
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
