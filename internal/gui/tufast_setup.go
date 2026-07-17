package gui

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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

// tuFastConsent* are the states of the /tufast-setup consent gate (see
// server.tuFastConsent). "unset" is the zero value/starting state.
const (
	tuFastConsentUnset          = "unset"
	tuFastConsentPendingConfirm = "pending_confirm"
	tuFastConsentDeclined       = "declined"
	tuFastConsentGranted        = "granted"
)

// tuFastDefaultProfileSubdir is the Chromium profile subfolder name inside
// the dedicated login user-data-dir (see scraper.LoginProfileDir) - Chromium
// always creates a "Default" profile subdirectory even for a single-profile
// launch, which is why scraper.TransplantTUFastLoginData's target profile
// argument is hardcoded to "Default" elsewhere in this file too.
const tuFastDefaultProfileSubdir = "Default"

type tuFastSetupPageData struct {
	// ProfileDir is the single, hardcoded dedicated login profile directory
	// (~/.opal-downloader/login-profile, see scraper.LoginProfileDir) -
	// there is nothing left to configure here since Chromium (Playwright's
	// bundled build) launched against this one profile is the only login
	// path opal-downloader has.
	ProfileDir    string
	ProfileDirErr string

	// ConsentState drives which section of the page is shown - see the
	// tuFastConsent* constants. No install/copy action is reachable in the
	// UI until this is tuFastConsentGranted.
	ConsentState string

	// AlreadyInstalled and DetectedSourceDir are only populated once
	// ConsentState is tuFastConsentGranted (see loadTUFastSetupPageData) -
	// real filesystem checks, never user-reported. AlreadyInstalled means
	// the dedicated profile already has TU-Fast installed (nothing left to
	// set up here). DetectedSourceDir, when non-empty, is another Chromium/
	// Brave profile root on this machine that already has TU-Fast
	// installed, and is the transplant source candidate.
	AlreadyInstalled  bool
	DetectedSourceDir string

	OpenResult   string
	OpenResultOK bool

	CopySourceUserDataDir string
	CopySourceProfileDir  string
	CopyResult            string
	CopyResultOK          bool
}

// detectBrowserUserDataDir looks for a Brave or Chrome install's user-data
// directory (the profile *root*, one level above a "Default"/"Profile 1"
// subfolder - not the browser executable) in common per-OS locations.
// Returns "" if nothing is found - callers fall back to asking the user to
// type a path in by hand. Chrome/Brave always use this fixed, well-known
// per-OS location unless launched with an explicit --user-data-dir
// override, so this is a reliable guess, not a scan of arbitrary disk
// locations. Mirrors the shape (and OS-detection approach) of the old
// detectBrowserExecutable, deleted when the "point opal-downloader at your
// real Brave/Chrome executable" login path was removed - see this func's
// task for that history.
//
// Windows paths verified live 2026-07-16 against a real installed Brave on
// the machine used to write this feature (%LocalAppData%\BraveSoftware\
// Brave-Browser\User Data existed and contained "Default"/"Profile 1"); the
// Chrome path and the macOS/Linux paths follow Chromium's documented
// per-OS user-data-dir convention but were not separately live-verified on
// those platforms.
func detectBrowserUserDataDir() string {
	switch runtime.GOOS {
	case "windows":
		var candidates []string
		if base := os.Getenv("LocalAppData"); base != "" {
			candidates = append(candidates,
				filepath.Join(base, "BraveSoftware", "Brave-Browser", "User Data"),
				filepath.Join(base, "Google", "Chrome", "User Data"),
			)
		}
		for _, c := range candidates {
			if info, err := os.Stat(c); err == nil && info.IsDir() {
				return c
			}
		}
		return ""
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		for _, c := range []string{
			filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser"),
			filepath.Join(home, "Library", "Application Support", "Google", "Chrome"),
		} {
			if info, err := os.Stat(c); err == nil && info.IsDir() {
				return c
			}
		}
		return ""
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		for _, c := range []string{
			filepath.Join(home, ".config", "BraveSoftware", "Brave-Browser"),
			filepath.Join(home, ".config", "google-chrome"),
			filepath.Join(home, ".config", "chromium"),
		} {
			if info, err := os.Stat(c); err == nil && info.IsDir() {
				return c
			}
		}
		return ""
	}
}

// hasTUFastExtension reports whether a Chromium user-data-dir (profile
// root) has TU-Fast installed in its "Default" profile. Thin wrapper over
// scraper.HasTUFastExtension, the canonical implementation shared with
// cmd/opal-downloader/root.go's checkLoginProfileHealth (`status`) and
// scraper.EnsureTUFastPresent (`sync --scheduled`'s pre-flight gate) - kept
// as a local name here only so this file's many existing call sites didn't
// need touching.
func hasTUFastExtension(userDataDir string) bool {
	return scraper.HasTUFastExtension(userDataDir)
}

// getTUFastConsent returns the current /tufast-setup consent-gate state,
// defaulting to tuFastConsentUnset.
func (s *server) getTUFastConsent() string {
	s.tuFastConsentMu.Lock()
	defer s.tuFastConsentMu.Unlock()
	if s.tuFastConsent == "" {
		return tuFastConsentUnset
	}
	return s.tuFastConsent
}

func (s *server) setTUFastConsent(state string) {
	s.tuFastConsentMu.Lock()
	s.tuFastConsent = state
	s.tuFastConsentMu.Unlock()
}

func (s *server) loadTUFastSetupPageData() tuFastSetupPageData {
	resolve := s.loginProfileDir
	if resolve == nil {
		resolve = scraper.LoginProfileDir
	}
	dir, err := resolve()
	data := tuFastSetupPageData{ProfileDir: dir, ConsentState: s.getTUFastConsent()}
	if err != nil {
		data.ProfileDirErr = err.Error()
	}

	// Only detect/show install state once the user has actually consented -
	// no install/open/copy action (and no reason to probe for one) is
	// reachable before that.
	if data.ConsentState != tuFastConsentGranted || dir == "" {
		return data
	}

	data.AlreadyInstalled = hasTUFastExtension(dir)

	detect := s.detectBrowserUserDataDir
	if detect == nil {
		detect = detectBrowserUserDataDir
	}
	if candidate := detect(); candidate != "" && hasTUFastExtension(candidate) {
		data.DetectedSourceDir = candidate
		data.CopySourceUserDataDir = candidate
	}
	return data
}

// handleTUFastSetupPage renders the current state - a plain GET, no
// side effects, so it also serves as the "Check again" action (re-running
// the exact same detection) after the user does something in the opened
// browser window.
func (s *server) handleTUFastSetupPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	s.renderTUFastSetup(w, s.loadTUFastSetupPageData())
}

// handleTUFastSetupConsent advances or resets the consent-gate state
// machine:
//
//	"yes"     unset          -> pending_confirm  (show the explicit "this
//	                             installs a third-party extension..." step)
//	"confirm" pending_confirm -> granted          (only state that unlocks
//	                             install/copy actions)
//	"no"      unset          -> declined
//	"cancel"  pending_confirm -> declined
//	"reset"   declined        -> unset            (user changed their mind)
//
// Any other/missing choice leaves the state untouched. This is a UI gate
// only - it does not change what TU-Fast itself can do once installed, just
// makes the existing consent moment explicit instead of implicit in an
// instructional sentence (see this page's task for the full rationale).
func (s *server) handleTUFastSetupConsent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()

	switch r.FormValue("choice") {
	case "yes":
		s.setTUFastConsent(tuFastConsentPendingConfirm)
	case "confirm":
		s.setTUFastConsent(tuFastConsentGranted)
	case "no", "cancel":
		s.setTUFastConsent(tuFastConsentDeclined)
	case "reset":
		s.setTUFastConsent(tuFastConsentUnset)
	}

	s.renderTUFastSetup(w, s.loadTUFastSetupPageData())
}

// handleTUFastSetupOpen auto-creates the dedicated login profile directory
// (if it doesn't exist yet) and launches Playwright's bundled Chromium
// directly at TU-Fast's Chrome Web Store listing - cutting the
// manual-setup-checklist.md "Step 0" flow down to one click. Installing the
// extension and completing the OPAL/Shibboleth login itself stay genuinely
// manual (consent/identity actions) - this only opens the window and gets
// it to the right page. Requires the consent gate to already be past
// tuFastConsentGranted (defense in depth - the template also never renders
// this action's form before that point).
func (s *server) handleTUFastSetupOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()

	data := s.loadTUFastSetupPageData()
	if data.ConsentState != tuFastConsentGranted {
		data.OpenResult = "Consent is required first - go back and confirm the consent step above before installing TU-Fast."
		data.OpenResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}
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
		"Opened Chromium against the dedicated login profile at %s, at TU-Fast's Chrome Web Store listing. In that window: click \"Add to Chrome\", then log into OPAL/Shibboleth once to complete 2FA/device registration. After that, every future login/sync auto-completes - nothing else to configure. Come back here and click \"Check again\" once you're done.",
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
// user-configurable. Requires the consent gate to already be past
// tuFastConsentGranted (defense in depth, see handleTUFastSetupOpen).
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

	if data.ConsentState != tuFastConsentGranted {
		data.CopyResult = "Consent is required first - go back and confirm the consent step above before copying TU-Fast login data."
		data.CopyResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}
	if data.ProfileDir == "" {
		data.CopyResult = fmt.Sprintf("Could not determine the login profile directory: %s", data.ProfileDirErr)
		data.CopyResultOK = false
		s.renderTUFastSetup(w, data)
		return
	}

	result, err := scraper.TransplantTUFastLoginData(data.CopySourceUserDataDir, data.CopySourceProfileDir, data.ProfileDir, tuFastDefaultProfileSubdir)
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
	.inline-form { display: inline-block; margin-right: 0.5rem; }
	.secondary { background: #888; border-color: #888; }
	.secondary:hover { background: #6f6f6f; }
</style>
</head>
<body>
	` + bannerChrome + `
	<h1>TU-Fast browser profile setup</h1>
	<p class="hint">
		opal-downloader always logs in and syncs using Playwright's bundled
		Chromium against one dedicated profile at
		<code>{{.ProfileDir}}</code> - never your everyday browser. See
		docs/browser-profile-strategy.md for the full background.
	</p>

	{{/* Always shown once, regardless of which section below is currently
	     active: handleTUFastSetupOpen/Copy can leave a result here either
	     from a real action (only reachable once granted) or a
	     defense-in-depth refusal (consent not granted yet) - and which
	     detected-state branch renders below (AlreadyInstalled/
	     DetectedSourceDir/manual) is independent of whether a result is
	     waiting to be shown, so this must not be nested inside any of
	     those branches (a prior version nested it under "AlreadyInstalled
	     && DetectedSourceDir" and silently dropped copy-result messages
	     whenever no transplant source was detected - see the task/PR this
	     fix landed in). */}}
	{{if .OpenResult}}<div class="status {{if .OpenResultOK}}ok{{else}}warn{{end}}">{{.OpenResult}}</div>{{end}}
	{{if .CopyResult}}<div class="status {{if .CopyResultOK}}ok{{else}}warn{{end}}">{{.CopyResult}}</div>{{end}}

	{{if eq .ConsentState "unset"}}
	<h2>Set up automatic login?</h2>
	<p>
		Do you want TU-Fast to auto-fill your OPAL login (username,
		password, and 2FA code) every time, so you never have to log in by
		hand again?
	</p>
	<form class="inline-form" method="post" action="/tufast-setup/consent">
		<input type="hidden" name="choice" value="yes">
		<button type="submit">Yes, set it up</button>
	</form>
	<form class="inline-form" method="post" action="/tufast-setup/consent">
		<input type="hidden" name="choice" value="no">
		<button type="submit" class="secondary">No, I'll log in manually</button>
	</form>

	{{else if eq .ConsentState "pending_confirm"}}
	<h2>Confirm before installing</h2>
	<div class="disclaimer">
		This installs <strong>TU-Fast</strong>, a third-party browser
		extension, into the dedicated profile above. It will be able to
		read and fill in your OPAL username, password, and 2FA code.
		opal-downloader itself never sees or stores this data - TU-Fast
		keeps it in that browser profile.
	</div>
	<form class="inline-form" method="post" action="/tufast-setup/consent">
		<input type="hidden" name="choice" value="confirm">
		<button type="submit">I understand, continue</button>
	</form>
	<form class="inline-form" method="post" action="/tufast-setup/consent">
		<input type="hidden" name="choice" value="cancel">
		<button type="submit" class="secondary">Cancel</button>
	</form>

	{{else if eq .ConsentState "declined"}}
	<div class="status warn">
		You chose to log in to OPAL manually. TU-Fast will not be installed
		or used. Manual <code>login</code> (typing your credentials/2FA
		yourself in the dedicated profile) still works as before.
	</div>
	<form method="post" action="/tufast-setup/consent">
		<input type="hidden" name="choice" value="reset">
		<button type="submit">Actually, set up automatic login</button>
	</form>

	{{else}}
	{{/* ConsentState == granted */}}

	{{if .AlreadyInstalled}}
	<div class="status ok">
		TU-Fast is already installed in the dedicated profile
		(<code>{{.ProfileDir}}</code>). Nothing more to do here - login/sync
		will use it automatically.
	</div>
	{{if .DetectedSourceDir}}
	<h2>Optional: skip a manual login</h2>
	<p class="hint">
		TU-Fast also looks logged in at <code>{{.DetectedSourceDir}}</code>
		on this machine. If the dedicated profile hasn't completed its own
		OPAL/Shibboleth login yet, you can copy that already-working login
		data over instead of doing it by hand.
	</p>
	<form method="post" action="/tufast-setup/copy">
		<div class="field">
			<label for="source_user_data_dir">Source browser's user data directory</label>
			<input type="text" id="source_user_data_dir" name="source_user_data_dir" value="{{.CopySourceUserDataDir}}">
		</div>
		<div class="field">
			<label for="source_profile_directory">Source profile directory (optional)</label>
			<input type="text" id="source_profile_directory" name="source_profile_directory" value="{{.CopySourceProfileDir}}" placeholder="Default">
		</div>
		<button type="submit">Copy TU-Fast login data</button>
	</form>
	{{end}}

	{{else if .DetectedSourceDir}}
	<h2>Step A: Install TU-Fast in the dedicated profile</h2>
	<p class="hint">
		Creates the profile directory above if it doesn't exist yet, and
		opens a Chromium window there, straight at TU-Fast's Chrome Web
		Store listing. Installing the extension and completing the
		OPAL/Shibboleth 2FA/device-registration login are consent/identity
		actions - do those two steps yourself in the window that opens;
		nothing here clicks through them for you.
	</p>
	<form method="post" action="/tufast-setup/open">
		<button type="submit">Open Chromium in the Chrome Web Store</button>
	</form>

	<h2>Step B: Copy your existing login data</h2>
	<p class="hint">
		Detected TU-Fast already installed and logged in at
		<code>{{.DetectedSourceDir}}</code> on this machine - this can copy
		just its stored login/2FA data into the dedicated profile above,
		skipping a second manual login. Enabled once step A is detected
		done; click "Check again" after installing.
	</p>
	<form method="post" action="/tufast-setup/copy">
		<div class="field">
			<label for="source_user_data_dir">Source browser's user data directory</label>
			<input type="text" id="source_user_data_dir" name="source_user_data_dir" value="{{.CopySourceUserDataDir}}">
		</div>
		<div class="field">
			<label for="source_profile_directory">Source profile directory (optional)</label>
			<input type="text" id="source_profile_directory" name="source_profile_directory" value="{{.CopySourceProfileDir}}" placeholder="Default">
		</div>
		<button type="submit" disabled title="Finish step A first, then click &quot;Check again&quot;">Copy TU-Fast login data</button>
	</form>
	<p class="back"><a href="/tufast-setup">&#8635; Check again</a></p>

	{{else}}
	<h2>Install TU-Fast in the dedicated profile</h2>
	<p class="hint">
		Creates the profile directory above if it doesn't exist yet, and
		opens a Chromium window there, straight at TU-Fast's Chrome Web
		Store listing. Installing the extension and completing the
		OPAL/Shibboleth 2FA/device-registration login are consent/identity
		actions - do those two steps yourself in the window that opens;
		nothing here clicks through them for you.
	</p>
	<form method="post" action="/tufast-setup/open">
		<button type="submit">Open Chromium in the Chrome Web Store</button>
	</form>
	<p class="back"><a href="/tufast-setup">&#8635; Check again</a></p>
	{{end}}
	{{end}}

	<p class="back"><a href="/settings">&larr; Back to Settings</a> &middot; <a href="/">Home</a></p>
</body>
</html>
`))
