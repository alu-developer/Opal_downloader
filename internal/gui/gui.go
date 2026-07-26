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
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/report"
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

	// syncJob is the same *job the /sync page drives, handed over by
	// registerSyncRoutes so the landing page's "Sync now" button can report
	// whether a run is already in flight. Without it the button would happily
	// start a second sync that the overlap guard then rejects with a raw
	// "already running" error - a self-inflicted failure for the one action
	// the landing page exists to make easy. nil until registerSyncRoutes has
	// run (and in tests that build a bare server), so every read must be
	// nil-checked.
	syncJob *job

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

	// feedback holds the most recent panic recovered from a request handler
	// (see withRecover/renderPanicPage), so the Feedback page can offer to
	// include it. See feedback.go.
	feedback *feedbackState

	// openBrowser overrides openInDefaultBrowser (used by handleFeedbackOpen
	// to open a prefilled GitHub issue link). nil in production means "use
	// openInDefaultBrowser"; tests set this to a fake so `go test` never
	// actually launches a browser.
	openBrowser func(url string) error

	// launchBrowserAt overrides defaultLaunchBrowserAt (used by
	// handleTUFastSetupOpen to open a visible Playwright Chromium window
	// against the dedicated login profile at TU-Fast's Web Store listing).
	// nil in production means "use defaultLaunchBrowserAt"; tests set this
	// to a fake so `go test` never actually launches a browser.
	launchBrowserAt func(url string) error

	// loginProfileDir overrides scraper.LoginProfileDir (used by
	// loadTUFastSetupPageData to resolve the dedicated login profile
	// directory shown on the /tufast-setup page and used as the transplant
	// target). nil in production means "use scraper.LoginProfileDir"; tests
	// set this to a fake so they can point at a temp directory instead of
	// the real ~/.opal-downloader/login-profile.
	loginProfileDir func() (string, error)

	// detectBrowserUserDataDir overrides the package-level
	// detectBrowserUserDataDir func (used by loadTUFastSetupPageData to
	// find/prefill a candidate transplant-source browser profile root).
	// nil in production means "use the real, env/filesystem-based
	// detectBrowserUserDataDir"; tests set this to a fake so `go test`
	// never depends on what's actually installed on the machine running it.
	detectBrowserUserDataDir func() string

	// tuFastConsentMu guards tuFastConsent, the in-memory (process-lifetime,
	// not persisted to disk) record of where the user is in the
	// /tufast-setup consent gate - see tuFastConsent* constants in
	// tufast_setup.go. Reset to "" (unset) on every GUI process start,
	// which is what "for that session" means in that flow's design: this
	// is a local single-user tool with no other notion of a session.
	tuFastConsentMu sync.Mutex
	tuFastConsent   string
}

// Run starts the local web UI server and blocks until it is stopped via
// SIGINT (Ctrl-C) or the server fails to serve.
// newMux builds the GUI's complete route table.
//
// Split out of Run so a test can serve the real thing. Wiring handlers up
// individually in a test is not the same check: it silently passes when a
// route is registered at a different path, or not at all, which is precisely
// the kind of breakage an end-to-end walk exists to catch.
func newMux(srv *server, configPath string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.withRecover(srv.handleLanding))
	mux.HandleFunc("/settings", srv.withRecover(handleSettings(configPath)))
	mux.HandleFunc("/settings/browse-folder", srv.withRecover(handleBrowseFolder))
	mux.HandleFunc("/settings/discover-courses", srv.withRecover(handleDiscoverCourses(configPath)))
	mux.HandleFunc("/settings/suggest-folders", srv.withRecover(handleSuggestFolders(configPath)))
	mux.HandleFunc("/schedule", srv.withRecover(handleSchedulePage(configPath)))
	mux.HandleFunc("/tufast-setup", srv.withRecover(srv.handleTUFastSetupPage))
	mux.HandleFunc("/tufast-setup/consent", srv.withRecover(srv.handleTUFastSetupConsent))
	mux.HandleFunc("/tufast-setup/open", srv.withRecover(srv.handleTUFastSetupOpen))
	mux.HandleFunc("/tufast-setup/copy", srv.withRecover(srv.handleTUFastSetupCopy))
	mux.HandleFunc("/update", srv.withRecover(srv.handleUpdatePage))
	mux.HandleFunc("/update/start", srv.withRecover(srv.handleUpdateStart))
	mux.HandleFunc("/feedback", srv.withRecover(srv.handleFeedbackPage))
	mux.HandleFunc("/feedback/open", srv.withRecover(srv.handleFeedbackOpen))
	mux.HandleFunc("/scheduled-status", srv.withRecover(srv.handleScheduledStatus))
	mux.HandleFunc("/logo.svg", srv.withRecover(handleLogo))
	registerSyncRoutes(mux, srv, configPath)
	return mux
}

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

	srv := &server{
		configPath:      configPath,
		buildVersion:    version,
		launchInstaller: defaultLaunchInstaller,
		exitProcess:     defaultExitProcess,
		feedback:        &feedbackState{},
	}

	// Check for an update once per process start (not a recurring ticker -
	// this is a short-lived local tool, not a daemon). Launched right after
	// the listener starts so the check runs concurrently with GUI startup
	// instead of delaying it; handleLanding/handleUpdatePage read whatever
	// updateChecked/updateResult look like at request time, so a request
	// that races the check simply sees "not checked yet" until it finishes.
	go srv.checkForUpdateOnce(context.Background())

	httpServer := &http.Server{Handler: newMux(srv, configPath)}

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

// withRecover wraps an HTTP handler so a panic inside it doesn't crash the
// whole GUI process (see CLAUDE.md's "Reliability over features" principle)
// - it recovers, records the panic+stack as this server's latest crash
// report (see feedback.go's feedbackState), and renders an error page
// linking to the Feedback page with that report pre-attached. The mux
// itself, and any other in-flight/future requests, are unaffected: Go's
// net/http already runs each request in its own goroutine, so a recovered
// panic here only ever aborts the one request that triggered it.
func (s *server) withRecover(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				s.feedback.setCrash(report.CrashReport(s.buildVersion, rec, stack))
				renderPanicPage(w, rec)
			}
		}()
		next(w, r)
	}
}

var panicPageTemplate = template.Must(template.New("panic").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
` + faviconLink + `
<title>Opal Downloader - Something went wrong</title>
<style>` + pageStyle + `</style>
</head>
<body>
	` + bannerChrome + `
	<h1>Something went wrong</h1>
	<div class="error">
		This page hit an unexpected error: <code>{{.}}</code>
	</div>
	<p>The rest of the app is still running - you can go back and try again.</p>
	<p><a href="/feedback?crash=1">Report this crash</a> (opens a prefilled GitHub issue with the error details - nothing is sent automatically).</p>
	<p class="back"><a href="/">&larr; Back to start</a></p>
</body>
</html>
`))

// renderPanicPage writes the recovered-panic error page. It always responds
// with 500 and never itself panics (html/template.Execute on this fixed
// template with a plain fmt.Stringer-able value cannot fail in a way worth
// handling here).
func renderPanicPage(w http.ResponseWriter, rec any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_ = panicPageTemplate.Execute(w, fmt.Sprintf("%v", rec))
}

// disclaimerHTML is shown on the landing page, where it's load-bearing: it
// tells the user a *second*, separate browser window will open for OPAL
// login/sync automation, so closing one window doesn't affect the other.
const disclaimerHTML = `<div class="disclaimer">
		A separate browser window opens for OPAL login/sync. Closing this
		window does not stop it, and closing it does not close this window.
	</div>`

var landingTemplate = template.Must(template.New("landing").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
` + faviconLink + `
<title>Opal Downloader</title>
<style>` + pageStyle + `</style>
</head>
<body>
	` + bannerChrome + `
	<h1>` + logoMark + `Opal Downloader</h1>

	{{if .SetupNeeded}}
	<div class="intro">
		<h2>What this does</h2>
		<p>It signs in to OPAL for you, finds the files in your courses, and
		copies them into a folder on this computer. Run it again later and it
		only fetches what changed &ndash; nothing is uploaded, and nothing is
		deleted from OPAL.</p>

		<h2>What you'll do once</h2>
		<ol>
			<li><strong>Choose a folder</strong> for your course files, on the
			next page.</li>
			<li><strong>Pick your courses</strong> &ndash; or leave "Sync all
			courses" ticked and get everything you're enrolled in. You can
			change this at any time.</li>
			<li><strong>Log in to OPAL once.</strong> Your login stays on this
			computer. If you install the TU-Fast browser extension, later logins
			happen without you.</li>
		</ol>
		<p class="hint">Everything runs on your own machine. There is no server
		belonging to this tool, and your password never goes anywhere else.</p>
	</div>
	{{end}}

	` + disclaimerHTML + `

	{{if not .SetupNeeded}}
	<div class="status {{if .LoggedIn}}ok{{else}}warn{{end}}">
		{{if .LoggedIn}}
			Logged in (session saved {{if .StateModified}}{{.StateModified}}{{else}}earlier{{end}}). May still need a fresh login if it expired.
		{{else}}
			Not logged in yet.
		{{end}}
	</div>
	{{end}}

	{{if .UpdateChecked}}
	<div class="status {{if .UpdateAvailable}}warn{{else if .UpdateDevBuild}}neutral{{else}}ok{{end}}">
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

	<div class="primary-action">
		{{if .SyncRunning}}
			<a class="cta" href="/sync">Sync running &ndash; show progress</a>
			<p class="cta-note">A sync is already in flight. Following it here won't start a second one.</p>
		{{else if .SyncReady}}
			<a class="cta" href="/sync?autostart=1">Sync now</a>
			<p class="cta-note">Downloads new and updated files for {{if .SyncAllCourses}}all your courses{{else}}your {{.CourseCount}} selected course{{if ne .CourseCount 1}}s{{end}}{{end}}.</p>
		{{else if .SetupNeeded}}
			<a class="cta" href="/settings">Set up opal-downloader</a>
			<p class="cta-note">{{.SyncBlockedReason}}</p>
		{{else}}
			<span class="cta disabled" aria-disabled="true">Sync now</span>
			<p class="cta-note">{{.SyncBlockedReason}}</p>
		{{end}}
	</div>

	<nav>
		<ul>
			<li><a href="/settings">Settings &ndash; courses and folders</a></li>
			<li><a href="/schedule">Automatic sync &ndash; run it daily on its own</a></li>
			<li><a href="/tufast-setup">TU-Fast setup</a></li>
			<li><a href="/sync">Sync options &amp; developer tools</a></li>
			<li><a href="/update">Check for updates</a></li>
			<li><a href="/feedback">Feedback / Problem melden</a></li>
		</ul>
	</nav>
</body>
</html>
`))

type landingData struct {
	LoggedIn      bool
	StateFile     string
	StateModified string

	// CourseCount/SyncAllCourses/SyncReady/SyncBlockedReason drive the
	// landing page's primary "Sync now" action. SyncReady is false whenever
	// starting a sync would fail (config missing/unreadable, not logged in
	// yet), in which case SyncBlockedReason says which of those it is and the
	// button renders disabled - the point is to explain the situation on the
	// page the user is already looking at, instead of letting them click
	// through to a failure.
	//
	// SyncAllCourses reflects the wildcard course filter. config.Load turns
	// an empty courses list into []string{"*"} meaning "every course", so a
	// zero count never reaches here and a naive len() would render a wildcard
	// config as the plainly wrong "your 1 selected course".
	CourseCount       int
	SyncAllCourses    bool
	SyncReady         bool
	SyncBlockedReason string

	// SetupNeeded marks the one blocked case the user can act on right here:
	// there is no usable config yet. A first run then leads with a live
	// "Set up opal-downloader" button instead of a dead grey "Sync now" and a
	// sentence telling them to go find Settings among five equal-weight
	// links. The other blocked case (configured but not logged in) keeps the
	// disabled button, because logging in is not a page you fill in.
	SetupNeeded bool

	// SyncRunning reports that a sync/list job is already in flight, so the
	// button offers to follow along instead of starting a competing run.
	SyncRunning bool

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
	s.applySyncReadiness(&data)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = landingTemplate.Execute(w, data)
}

// applySyncReadiness fills in the landing page's "Sync now" state: how many
// courses are configured, whether a sync can usefully be started right now,
// and whether one is already running.
//
// The checks are deliberately ordered cheapest-and-most-fundamental first
// (can we even read a config? does it select any courses? are we logged
// in?), so the reason shown is the first thing the user actually has to fix
// rather than whichever check happened to run last. Like sessionStatus, this
// stays a config/filesystem-only inspection - it must never launch a browser
// or touch the network, because it runs on every load of the landing page.
func (s *server) applySyncReadiness(data *landingData) {
	if s.syncJob != nil {
		running, _ := s.syncJob.isRunning()
		data.SyncRunning = running
	}

	loaded, err := config.Load(s.configPath)
	if err != nil {
		data.SetupNeeded = true
		data.SyncBlockedReason = "First time here? This sets your download folder and picks the courses to sync."
		return
	}

	data.CourseCount = len(loaded.App.Courses)
	for _, course := range loaded.App.Courses {
		if strings.TrimSpace(course) == "*" {
			data.SyncAllCourses = true
			break
		}
	}

	if !data.LoggedIn {
		data.SyncBlockedReason = "Not logged in yet - run the login step first so a sync has a session to use."
		return
	}

	data.SyncReady = true
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
` + faviconLink + `
<title>Opal Downloader - Update</title>
<style>` + pageStyle + `</style>
</head>
<body>
	` + bannerChrome + `
	<h1>Check for updates</h1>

	{{if not .Checked}}
	<div class="status warn">Still checking for updates - refresh this page in a moment.</div>
	{{else if .DevBuild}}
	<div class="status neutral">Update checks are unavailable for development builds (<code>{{.CurrentVersion}}</code>). Build with <code>-ldflags</code> to embed a real version, or check <a href="https://github.com/alu-developer/Opal_downloader/releases" target="_blank" rel="noopener">GitHub Releases</a> manually.</div>
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

// updateStartingTemplate deliberately does not include bannerChrome: this
// page is shown for only the few hundred ms before handleUpdateStart exits
// the whole GUI process to hand off to the installer (see its doc
// comment), so a banner here would never have time to matter.
var updateStartingTemplate = template.Must(template.New("update-starting").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
` + faviconLink + `
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
` + faviconLink + `
<title>Opal Downloader - Update failed</title>
<style>` + pageStyle + `</style>
</head>
<body>
	` + bannerChrome + `
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
