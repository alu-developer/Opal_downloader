// Package gui serves a small local web UI for opal-downloader, bound
// strictly to 127.0.0.1. It is a separate front-end over the same
// internal/config, internal/syncer, and internal/scraper packages the
// CLI subcommands already use.
package gui

import (
	"context"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/updater"
)

// Options configures the GUI server.
type Options struct {
	// Port to bind on 127.0.0.1. Zero selects an available port automatically.
	Port int
	// ConfigPath is the config.yaml path the settings page reads/writes, and
	// that the login page uses to resolve credentials (OPAL URL, session
	// state file, browser settings). Defaults to "config.yaml" in the
	// current working directory when empty.
	ConfigPath string
	// Version is the running binary's build version (cmd/opal-downloader's
	// buildVersion, e.g. "dev" or "0.2.0"), passed in explicitly to avoid an
	// import cycle between internal/gui and cmd/opal-downloader. Used only
	// for the update-checker banner. Defaults to "dev" when empty, matching
	// cmd/opal-downloader's own default.
	Version string
}

// server holds the shared state needed by the GUI's HTTP handlers.
type server struct {
	configPath   string
	buildVersion string

	// loginMu serializes login attempts and guards loginActive. Login runs
	// synchronously from the caller's perspective (v1 scope): a request to
	// /login/start blocks until the interactive Playwright login completes
	// or fails.
	loginMu     sync.Mutex
	loginActive bool

	// updateMu guards the cached result of the one-time startup update
	// check (see checkForUpdateOnce). This is a short-lived local tool, not
	// a daemon, so the check runs once per process start rather than on a
	// recurring ticker - updateChecked flips true once the single check
	// completes (success or failure) and updateResult/updateErr hold its
	// outcome for the rest of the process's life.
	updateMu       sync.Mutex
	updateChecked  bool
	updateResult   updater.Release
	updateErr      error
	updateDevBuild bool

	// updaterClient overrides the updater package's default GitHub API
	// client. nil (the zero value used in production) means "use the real
	// github.com API via the package-level updater.CheckLatest/Download
	// functions"; tests set this to a *updater.Client pointed at an
	// httptest.Server to fake both the release-check and asset/checksum
	// downloads without any network dependency.
	updaterClient *updater.Client

	// launchInstaller and exitProcess together perform the installer
	// hand-off (see handleUpdateStart): launchInstaller starts the
	// downloaded setup.exe as a detached process and reports whether that
	// succeeded (see defaultLaunchInstaller), and exitProcess - only called
	// once launchInstaller has confirmed success - terminates this GUI
	// process (see defaultExitProcess) so Inno Setup's upgrade-in-place
	// isn't fighting a running instance of the app it's replacing. Split
	// into two fields (rather than one "launch and exit" func) so a launch
	// failure can be reported back to the HTTP response instead of exiting
	// a process that never actually handed off to anything. Both
	// overridable in tests so `go test` doesn't spawn a real process or
	// call os.Exit.
	launchInstaller func(installerPath string) error
	exitProcess     func()
}

// Run starts the local web UI server and blocks until it is stopped via
// SIGINT (Ctrl-C) or the server fails to serve.
func Run(opts Options) error {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
	if err != nil {
		return fmt.Errorf("starting GUI server: %w", err)
	}

	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = "config.yaml"
	}

	version := opts.Version
	if version == "" {
		version = "dev"
	}

	srv := &server{configPath: configPath, buildVersion: version, launchInstaller: defaultLaunchInstaller, exitProcess: defaultExitProcess}

	// Check for an update once per process start (not a recurring ticker -
	// this is a short-lived local tool, not a daemon). Launched right after
	// the listener starts so the check runs concurrently with GUI startup
	// instead of delaying it; handleLanding/handleUpdatePage read whatever
	// updateChecked/updateResult look like at request time, so a request
	// that races the check simply sees "not checked yet" until it finishes.
	go srv.checkForUpdateOnce(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleLanding)
	mux.HandleFunc("/settings", handleSettings(configPath))
	mux.HandleFunc("/settings/browse-folder", handleBrowseFolder)
	mux.HandleFunc("/login", srv.handleLoginPage)
	mux.HandleFunc("/login/start", srv.handleLoginStart)
	mux.HandleFunc("/update", srv.handleUpdatePage)
	mux.HandleFunc("/update/start", srv.handleUpdateStart)
	registerSyncRoutes(mux, configPath)

	httpServer := &http.Server{Handler: mux}

	url := fmt.Sprintf("http://%s", listener.Addr().String())

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(listener)
	}()

	// On Windows, hasNativeWindow is true and this opens a native WebView2
	// window showing the GUI, blocking until the user closes it. On other
	// platforms (no native window implementation - see window_other.go)
	// this falls through to the old print-the-URL-and-wait-for-Ctrl-C
	// behavior.
	if hasNativeWindow {
		fmt.Printf("Opal Downloader GUI opening in a native window (%s)...\n", url)
		windowErr := openNativeWindow(url)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr := httpServer.Shutdown(ctx)

		if windowErr != nil {
			return windowErr
		}
		return shutdownErr
	}

	fmt.Printf("Opal Downloader GUI running at %s\n", url)
	fmt.Println("Open that address in your browser. Press Ctrl-C to stop.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
		fmt.Println("\nShutting down GUI server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(ctx)
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

// disclaimerHTML is shown on the landing and login pages, where it's
// load-bearing: it tells the user a *second*, separate browser window will
// open for OPAL login/sync automation, so closing one window doesn't affect
// the other.
const disclaimerHTML = `<div class="disclaimer">
		A separate browser window opens for OPAL login/sync. Closing this
		window does not stop it, and closing it does not close this window.
	</div>`

// pageStyle is the shared look for every GUI page (landing, login, update,
// settings, sync): same fonts/spacing/colors for headings, status boxes,
// buttons, hints, and the back link, so no page looks structurally
// different from another. Page-specific templates append their own extra
// rules (forms, tables, log view, etc.) after this.
const pageStyle = `
	body { font-family: system-ui, sans-serif; max-width: 44rem; margin: 3rem auto; padding: 0 1rem; color: #1a1a1a; }
	h1 { margin-bottom: 0.25rem; }
	h2 { margin-top: 2.5rem; border-bottom: 1px solid #ddd; padding-bottom: 0.25rem; }
	.disclaimer { background: #fff8e1; border: 1px solid #e0c46c; border-radius: 6px; padding: 0.75rem 1rem; font-size: 0.9rem; margin: 1.5rem 0; }
	.hint { color: #666; font-size: 0.85rem; margin: 0.15rem 0 0.6rem; }
	nav ul { list-style: none; padding: 0; }
	nav li { padding: 0.5rem 0; border-bottom: 1px solid #eee; }
	.soon { color: #888; font-size: 0.85rem; }
	.status { border-radius: 6px; padding: 0.75rem 1rem; margin: 1rem 0; font-size: 0.9rem; }
	.status.ok { background: #e6f4ea; border: 1px solid #8cc98f; }
	.status.warn { background: #fdecea; border: 1px solid #e0a6a6; }
	.error { background: #fdecea; border: 1px solid #d93025; color: #7a1712; border-radius: 6px; padding: 0.75rem 1rem; margin: 1.25rem 0; }
	.success { background: #e6f4ea; border: 1px solid #34a853; color: #1e4620; border-radius: 6px; padding: 0.75rem 1rem; margin: 1.25rem 0; }
	.warning { background: #fff8e1; border: 1px solid #e0c46c; color: #6b5300; border-radius: 6px; padding: 0.6rem 1rem; margin: 0.75rem 0; font-size: 0.9rem; }
	.warning ul { margin: 0.25rem 0 0; padding-left: 1.2rem; }
	button { font: inherit; padding: 0.6rem 1.2rem; border-radius: 6px; border: 1px solid #2a6fb0; background: #2a6fb0; color: #fff; cursor: pointer; }
	button:hover { background: #1f588f; }
	button:disabled { opacity: 0.5; cursor: not-allowed; }
	code { background: #f0f0f0; padding: 0.1rem 0.3rem; border-radius: 3px; }
	.back { margin-top: 1.5rem; font-size: 0.9rem; }
	.back a { color: #2a6fb0; }
`

var landingTemplate = template.Must(template.New("landing").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Opal Downloader</title>
<style>` + pageStyle + `</style>
</head>
<body>
	<h1>Opal Downloader</h1>

	` + disclaimerHTML + `

	<div class="status {{if .LoggedIn}}ok{{else}}warn{{end}}">
		{{if .LoggedIn}}
			Logged in (session saved {{if .StateModified}}{{.StateModified}}{{else}}earlier{{end}}). May still need a fresh login if it expired.
		{{else}}
			Not logged in yet.
		{{end}}
	</div>

	{{if .UpdateChecked}}
	<div class="status {{if .UpdateAvailable}}warn{{else}}ok{{end}}">
		{{if .UpdateAvailable}}
			An update is available: <code>{{.CurrentVersion}}</code> &rarr; <code>{{.LatestVersion}}</code>.
			<form method="post" action="/update/start" style="margin-top: 0.5rem;">
				<button type="submit">Download &amp; install</button>
			</form>
			{{if .ChangelogURL}}<p style="margin: 0.5rem 0 0;"><a href="{{.ChangelogURL}}" target="_blank" rel="noopener">Release notes</a></p>{{end}}
		{{else if .UpdateDevBuild}}
			Update checks are unavailable for development builds (<code>{{.CurrentVersion}}</code>).
		{{else}}
			Running the latest version (<code>{{.CurrentVersion}}</code>).
		{{end}}
	</div>
	{{end}}

	<nav>
		<ul>
			<li><a href="/settings">Settings</a></li>
			<li><a href="/login">Login</a></li>
			<li><a href="/sync">Sync / List / Dump links</a></li>
			<li><a href="/update">Check for updates</a></li>
		</ul>
	</nav>
</body>
</html>
`))

type landingData struct {
	LoggedIn      bool
	StateFile     string
	StateModified string

	UpdateChecked   bool
	UpdateAvailable bool
	UpdateDevBuild  bool
	CurrentVersion  string
	LatestVersion   string
	ChangelogURL    string
}

func (s *server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := s.sessionStatus()
	s.applyUpdateStatus(&data)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = landingTemplate.Execute(w, data)
}

// sessionStatus reports whether a session state file exists for the
// configured credentials, and its modification time when available. This is
// a cheap filesystem check only - it does not launch a browser or validate
// the session against OPAL.
func (s *server) sessionStatus() landingData {
	credentials, err := config.LoadCredentials(s.configPath)
	stateFile := config.DefaultStateFile
	if err == nil {
		stateFile = credentials.StateFile
	}

	data := landingData{StateFile: stateFile}
	info, statErr := os.Stat(stateFile)
	if statErr == nil && !info.IsDir() {
		data.LoggedIn = true
		data.StateModified = info.ModTime().Format("2006-01-02 15:04:05")
	}
	return data
}

var loginPageTemplate = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Opal Downloader - Login</title>
<style>` + pageStyle + `</style>
</head>
<body>
	<h1>Log in to OPAL</h1>

	` + disclaimerHTML + `

	<div class="status {{if .LoggedIn}}ok{{else}}warn{{end}}">
		{{if .LoggedIn}}
			Logged in (session saved{{if .StateModified}} {{.StateModified}}{{end}}, <code>{{.StateFile}}</code>).
		{{else}}
			Not logged in yet (<code>{{.StateFile}}</code> not found).
		{{end}}
	</div>

	{{if .Result}}
	<div class="status {{if .ResultOK}}ok{{else}}warn{{end}}">{{.Result}}</div>
	{{end}}

	<p class="hint">
		A separate browser window opens to complete the OPAL login (including
		TU-Fast/2FA if needed). This page waits and updates once it's done.
	</p>

	<form method="post" action="/login/start">
		<button type="submit">Log in</button>
	</form>

	<p class="back"><a href="/">&larr; Back</a></p>
</body>
</html>
`))

type loginPageData struct {
	landingData
	Result   string
	ResultOK bool
}

func (s *server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	data := loginPageData{landingData: s.sessionStatus()}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = loginPageTemplate.Execute(w, data)
}

// handleLoginStart runs the interactive Playwright login flow synchronously
// (the same logic as `opal-downloader login`) and re-renders the login page
// with the outcome. This blocks the HTTP request for as long as the user
// takes to authenticate in the automation browser window - acceptable for
// v1 per the task scope (no general async task streaming here).
func (s *server) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	if !s.tryBeginLogin() {
		data := loginPageData{
			landingData: s.sessionStatus(),
			Result:      "A login is already in progress. Please wait for it to finish.",
			ResultOK:    false,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = loginPageTemplate.Execute(w, data)
		return
	}
	defer s.endLogin()

	err := s.runLogin()

	data := loginPageData{landingData: s.sessionStatus()}
	if err != nil {
		data.Result = fmt.Sprintf("Login failed: %s", err)
		data.ResultOK = false
	} else {
		data.Result = "Login successful. Session state saved."
		data.ResultOK = true
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = loginPageTemplate.Execute(w, data)
}

func (s *server) tryBeginLogin() bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if s.loginActive {
		return false
	}
	s.loginActive = true
	return true
}

func (s *server) endLogin() {
	s.loginMu.Lock()
	s.loginActive = false
	s.loginMu.Unlock()
}

// runLogin invokes the same login logic as the `login` CLI subcommand
// (cmd/opal-downloader/root.go's runLogin): load credentials, spawn a
// visible Playwright-controlled Chromium window, wait for the user to
// authenticate, and persist the session state file.
func (s *server) runLogin() error {
	credentials, err := config.LoadCredentials(s.configPath)
	if err != nil {
		return err
	}

	sc := scraper.New(credentials.URL, credentials.StateFile, credentials.BrowserExecutable, credentials.BrowserUserDataDir, credentials.BrowserProfileDir)
	defer sc.Close()

	if err := sc.LoginWithBrowser(); err != nil {
		return err
	}
	return nil
}

// checkForUpdateOnce hits internal/updater's release-check endpoint exactly
// once (bounded by a timeout so a slow/unreachable GitHub API can't hang
// startup indefinitely) and caches the outcome on the server struct. It is
// launched as a background goroutine right after Run()'s listener starts;
// handleLanding/handleUpdatePage just read whatever's cached at request
// time via applyUpdateStatus, so a request racing the still-in-flight check
// simply sees "not checked yet" (UpdateChecked=false) until this finishes.
func (s *server) checkForUpdateOnce(ctx context.Context) {
	// A "dev" buildVersion (the default for anything not built with the
	// release -ldflags, e.g. `go run .` during development) has no real
	// version to compare against - skip the network call entirely rather
	// than let it surface as a confusing "not a parseable numeric version"
	// error, and don't claim "running the latest version" either, since
	// that's not actually known. See applyUpdateStatus/handleUpdatePage for
	// how this is surfaced distinctly from a real check error.
	if isDevBuildVersion(s.buildVersion) {
		s.updateMu.Lock()
		s.updateChecked = true
		s.updateDevBuild = true
		s.updateMu.Unlock()
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	rel, err := s.checkLatest(checkCtx)

	s.updateMu.Lock()
	s.updateChecked = true
	s.updateResult = rel
	s.updateErr = err
	s.updateMu.Unlock()
}

// isDevBuildVersion reports whether v is the placeholder version used for
// unreleased/local builds ("dev", cmd/opal-downloader's default when built
// without -ldflags), which has no real release tag to compare against.
func isDevBuildVersion(v string) bool {
	return v == "" || v == "dev"
}

// checkLatest calls updater.CheckLatest (or, in tests, the injected
// updaterClient pointed at an httptest.Server) with this server's build
// version.
func (s *server) checkLatest(ctx context.Context) (updater.Release, error) {
	if s.updaterClient != nil {
		return s.updaterClient.CheckLatest(ctx, s.buildVersion)
	}
	return updater.CheckLatest(ctx, s.buildVersion)
}

// downloadAsset fetches url (an updater.Release's AssetURL or ChecksumURL)
// to destPath via updater.Download, or the injected updaterClient in tests.
func (s *server) downloadAsset(ctx context.Context, url, destPath string) error {
	if s.updaterClient != nil {
		return s.updaterClient.Download(ctx, url, destPath)
	}
	return updater.Download(ctx, url, destPath)
}

// applyUpdateStatus copies the cached update-check result into data's
// update fields. Safe to call before the background check has finished
// (UpdateChecked stays false until it has).
func (s *server) applyUpdateStatus(data *landingData) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()

	data.UpdateChecked = s.updateChecked
	data.UpdateAvailable = s.updateChecked && s.updateErr == nil && s.updateResult.IsNewer
	data.UpdateDevBuild = s.updateDevBuild
	data.CurrentVersion = s.buildVersion
	data.LatestVersion = s.updateResult.Version
	data.ChangelogURL = s.updateResult.HTMLURL
}

// updateSnapshot is a point-in-time, lock-free copy of the cached update
// check result, used by handleUpdateStart so it doesn't hold updateMu while
// downloading/verifying/launching the installer.
type updateSnapshot struct {
	checked  bool
	err      error
	release  updater.Release
	devBuild bool
}

func (s *server) snapshotUpdate() updateSnapshot {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	return updateSnapshot{checked: s.updateChecked, err: s.updateErr, release: s.updateResult, devBuild: s.updateDevBuild}
}

var updatePageTemplate = template.Must(template.New("update").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Opal Downloader - Update</title>
<style>` + pageStyle + `</style>
</head>
<body>
	<h1>Check for updates</h1>

	{{if not .Checked}}
	<div class="status warn">Still checking for updates - refresh this page in a moment.</div>
	{{else if .DevBuild}}
	<div class="status warn">Update checks are unavailable for development builds (<code>{{.CurrentVersion}}</code>). Build with <code>-ldflags</code> to embed a real version, or check <a href="https://github.com/alu-developer/Opal_downloader/releases" target="_blank" rel="noopener">GitHub Releases</a> manually.</div>
	{{else if .Result}}
	<div class="status warn">Could not check for updates: {{.Result}}</div>
	{{else if .Available}}
	<div class="status warn">
		An update is available: <code>{{.CurrentVersion}}</code> &rarr; <code>{{.LatestVersion}}</code>.
		{{if .ChangelogURL}}<p><a href="{{.ChangelogURL}}" target="_blank" rel="noopener">Release notes</a></p>{{end}}
	</div>
	<form method="post" action="/update/start">
		<button type="submit">Download &amp; install</button>
	</form>
	<p class="soon">This downloads the installer, verifies its checksum, launches it, and then closes this app so the installer can upgrade it in place.</p>
	{{else}}
	<div class="status ok">Running the latest version (<code>{{.CurrentVersion}}</code>).</div>
	{{end}}

	<p class="back"><a href="/">&larr; Back</a></p>
</body>
</html>
`))

type updatePageData struct {
	Checked        bool
	Available      bool
	DevBuild       bool
	CurrentVersion string
	LatestVersion  string
	ChangelogURL   string
	Result         string
}

func (s *server) handleUpdatePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	snap := s.snapshotUpdate()
	data := updatePageData{
		Checked:        snap.checked,
		DevBuild:       snap.devBuild,
		CurrentVersion: s.buildVersion,
		LatestVersion:  snap.release.Version,
		ChangelogURL:   snap.release.HTMLURL,
	}
	if snap.checked && !snap.devBuild {
		if snap.err != nil {
			data.Result = snap.err.Error()
		} else {
			data.Available = snap.release.IsNewer
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = updatePageTemplate.Execute(w, data)
}

var updateStartingTemplate = template.Must(template.New("update-starting").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Opal Downloader - Installing update</title>
<style>` + pageStyle + `</style>
</head>
<body>
	<h1>Starting installer</h1>
	<div class="status ok">
		The installer has been downloaded and verified. It is starting now,
		and <strong>this app will close</strong> so the installer can
		upgrade it in place.
	</div>
	<p>Once the installer finishes, you can start Opal Downloader again as usual.</p>
</body>
</html>
`))

var updateErrorTemplate = template.Must(template.New("update-error").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Opal Downloader - Update failed</title>
<style>` + pageStyle + `</style>
</head>
<body>
	<h1>Update failed</h1>
	<div class="status warn">{{.}}</div>
	<p class="back"><a href="/update">&larr; Back</a></p>
</body>
</html>
`))

// handleUpdateStart downloads the update asset, verifies its checksum, and
// hands off to the downloaded setup.exe: it renders a "starting installer,
// this app will close now" page, flushes it to the client, then - in a
// background goroutine so the flush above isn't racing process exit -
// launches the installer as a detached process and exits this GUI process.
// See launchAndExit/defaultLaunchAndExit for why the process must exit
// rather than keep running: Inno Setup's upgrade-in-place needs the app
// it's replacing to not be holding its own files open.
func (s *server) handleUpdateStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snap := s.snapshotUpdate()
	if !snap.checked || snap.err != nil || !snap.release.IsNewer {
		s.renderUpdateError(w, http.StatusBadRequest, fmt.Errorf("no update is available to install"))
		return
	}
	if snap.release.AssetURL == "" {
		s.renderUpdateError(w, http.StatusBadRequest, fmt.Errorf("release %s has no %s asset", snap.release.TagName, updater.AssetName))
		return
	}

	destPath := filepath.Join(os.TempDir(), updater.AssetName)
	if err := s.downloadAsset(r.Context(), snap.release.AssetURL, destPath); err != nil {
		s.renderUpdateError(w, http.StatusBadGateway, fmt.Errorf("downloading installer: %w", err))
		return
	}

	if snap.release.ChecksumURL != "" {
		checksumPath := destPath + ".sha256"
		if err := s.downloadAsset(r.Context(), snap.release.ChecksumURL, checksumPath); err != nil {
			s.renderUpdateError(w, http.StatusBadGateway, fmt.Errorf("downloading checksum: %w", err))
			return
		}
		sidecar, err := os.ReadFile(checksumPath)
		_ = os.Remove(checksumPath)
		if err != nil {
			s.renderUpdateError(w, http.StatusInternalServerError, fmt.Errorf("reading downloaded checksum: %w", err))
			return
		}
		if err := updater.VerifyChecksumSidecar(destPath, sidecar); err != nil {
			_ = os.Remove(destPath)
			s.renderUpdateError(w, http.StatusBadGateway, fmt.Errorf("checksum verification failed: %w", err))
			return
		}
	}

	// Start the installer *before* telling the user this app is closing:
	// cmd.Start() only blocks on the initial CreateProcess call (it does
	// not wait for the installer to finish), so this stays effectively
	// synchronous from the request's perspective, but it means a launch
	// failure - e.g. Windows requiring elevation the calling process can't
	// silently grant, confirmed live during this task's spike (see PR
	// description) - surfaces as a real error page instead of a "closing
	// now" page that lies about what happened.
	launchInstaller := s.launchInstaller
	if launchInstaller == nil {
		launchInstaller = defaultLaunchInstaller
	}
	if err := launchInstaller(destPath); err != nil {
		s.renderUpdateError(w, http.StatusInternalServerError, fmt.Errorf(
			"could not start the installer automatically (%w). The installer was downloaded and verified at %s - you can run it manually", err, destPath))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = updateStartingTemplate.Execute(w, nil)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	if s.exitProcess != nil {
		go s.exitProcess()
	}
}

func (s *server) renderUpdateError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = updateErrorTemplate.Execute(w, err.Error())
}

// defaultLaunchInstaller is the production implementation of
// server.launchInstaller: start installerPath as a detached child process
// (exec.Command(...).Start(), deliberately never Wait()'d - the point is
// that the installer must keep running after this process is gone). It
// returns whatever error CreateProcess reports rather than swallowing it,
// so handleUpdateStart can tell the user the installer didn't actually
// start instead of claiming this app is closing when it isn't.
func defaultLaunchInstaller(installerPath string) error {
	cmd := exec.Command(installerPath)
	return cmd.Start()
}

// defaultExitProcess is the production implementation of
// server.exitProcess, run in a background goroutine right after
// handleUpdateStart has confirmed the installer started and flushed the
// "this app will close now" response. It terminates this GUI process so
// Inno Setup's upgrade-in-place isn't fighting a running instance of the
// app it's replacing. Live-verified on Windows (see this task's PR
// description) that a process started via defaultLaunchInstaller survives
// this process's exit with no orphaned/zombie state, using a plain
// manifested Win32 executable as the stand-in installer.
//
// The short sleep before os.Exit gives the HTTP response written by
// handleUpdateStart a moment to actually reach the client (browser tab or
// the native webview window, see window_windows.go) before the process
// disappears out from under the connection.
func defaultExitProcess() {
	time.Sleep(500 * time.Millisecond)
	os.Exit(0)
}
