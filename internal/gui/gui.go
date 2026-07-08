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
	"os/signal"
	"sync"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
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
}

// server holds the shared state needed by the GUI's HTTP handlers.
type server struct {
	configPath string

	// loginMu serializes login attempts and guards loginActive. Login runs
	// synchronously from the caller's perspective (v1 scope): a request to
	// /login/start blocks until the interactive Playwright login completes
	// or fails.
	loginMu     sync.Mutex
	loginActive bool
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

	srv := &server{configPath: configPath}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleLanding)
	mux.HandleFunc("/settings", handleSettings(configPath))
	mux.HandleFunc("/login", srv.handleLoginPage)
	mux.HandleFunc("/login/start", srv.handleLoginStart)
	registerSyncRoutes(mux, configPath)

	httpServer := &http.Server{Handler: mux}

	fmt.Printf("Opal Downloader GUI running at http://%s\n", listener.Addr().String())
	fmt.Println("Press Ctrl-C to stop.")

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(listener)
	}()

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

const disclaimerHTML = `<div class="disclaimer">
		<strong>Note:</strong> this browser tab is just this app's local UI.
		It is separate from the Playwright-controlled browser window that
		opens for OPAL login/sync automation. Closing this tab does not
		affect an in-progress sync's automation browser, and closing the
		automation browser does not affect this tab.
	</div>`

const pageStyle = `
	body { font-family: system-ui, sans-serif; max-width: 40rem; margin: 3rem auto; padding: 0 1rem; color: #1a1a1a; }
	h1 { margin-bottom: 0.25rem; }
	.disclaimer { background: #fff8e1; border: 1px solid #e0c46c; border-radius: 6px; padding: 0.75rem 1rem; font-size: 0.9rem; margin: 1.5rem 0; }
	nav ul { list-style: none; padding: 0; }
	nav li { padding: 0.5rem 0; border-bottom: 1px solid #eee; }
	.soon { color: #888; font-size: 0.85rem; }
	.status { border-radius: 6px; padding: 0.75rem 1rem; margin: 1rem 0; font-size: 0.9rem; }
	.status.ok { background: #e6f4ea; border: 1px solid #8cc98f; }
	.status.warn { background: #fdecea; border: 1px solid #e0a6a6; }
	button { font: inherit; padding: 0.6rem 1.2rem; border-radius: 6px; border: 1px solid #2a6fb0; background: #2a6fb0; color: #fff; cursor: pointer; }
	button:hover { background: #1f588f; }
	a.back { display: inline-block; margin-top: 1.5rem; color: #2a6fb0; }
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
	<p>Local web UI, served only on 127.0.0.1.</p>

	` + disclaimerHTML + `

	<div class="status {{if .LoggedIn}}ok{{else}}warn{{end}}">
		{{if .LoggedIn}}
			Session state found: <code>{{.StateFile}}</code>{{if .StateModified}} (last updated {{.StateModified}}){{end}}.
			This looks like a saved OPAL login, but it can still be expired -
			the real check happens the next time <code>sync</code>/<code>list</code> runs.
		{{else}}
			Not logged in yet. No session state file found at <code>{{.StateFile}}</code>.
		{{end}}
	</div>

	<nav>
		<ul>
			<li><a href="/settings">Settings</a></li>
			<li><a href="/login">Login</a></li>
			<li><a href="/sync">Sync / List / Dump links</a></li>
		</ul>
	</nav>
</body>
</html>
`))

type landingData struct {
	LoggedIn      bool
	StateFile     string
	StateModified string
}

func (s *server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := s.sessionStatus()
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
			Session state found: <code>{{.StateFile}}</code>{{if .StateModified}} (last updated {{.StateModified}}){{end}}.
		{{else}}
			Not logged in yet. No session state file found at <code>{{.StateFile}}</code>.
		{{end}}
	</div>

	{{if .Result}}
	<div class="status {{if .ResultOK}}ok{{else}}warn{{end}}">{{.Result}}</div>
	{{end}}

	<p>
		Clicking "Log in" opens a <strong>separate, visible browser window</strong>
		controlled by this app (not this tab) where you complete the OPAL login,
		including TU-Fast/2FA if required. This tab will wait and update once
		that window reports login succeeded or failed.
	</p>

	<form method="post" action="/login/start">
		<button type="submit">Log in</button>
	</form>

	<a class="back" href="/">&larr; Back</a>
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
