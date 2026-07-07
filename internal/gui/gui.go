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
	"time"
)

// Options configures the GUI server.
type Options struct {
	// Port to bind on 127.0.0.1. Zero selects an available port automatically.
	Port int
	// ConfigPath is the config.yaml path the settings page reads/writes.
	// Defaults to "config.yaml" in the current working directory if empty.
	ConfigPath string
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

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleLanding)
	mux.HandleFunc("/settings", handleSettings(configPath))

	server := &http.Server{Handler: mux}

	fmt.Printf("Opal Downloader GUI running at http://%s\n", listener.Addr().String())
	fmt.Println("Press Ctrl-C to stop.")

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
		fmt.Println("\nShutting down GUI server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

var landingTemplate = template.Must(template.New("landing").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Opal Downloader</title>
<style>
	body { font-family: system-ui, sans-serif; max-width: 40rem; margin: 3rem auto; padding: 0 1rem; color: #1a1a1a; }
	h1 { margin-bottom: 0.25rem; }
	.disclaimer { background: #fff8e1; border: 1px solid #e0c46c; border-radius: 6px; padding: 0.75rem 1rem; font-size: 0.9rem; margin: 1.5rem 0; }
	nav ul { list-style: none; padding: 0; }
	nav li { padding: 0.5rem 0; border-bottom: 1px solid #eee; }
	.soon { color: #888; font-size: 0.85rem; }
</style>
</head>
<body>
	<h1>Opal Downloader</h1>
	<p>Local web UI, served only on 127.0.0.1.</p>

	<div class="disclaimer">
		<strong>Note:</strong> this browser tab is just this app's local UI.
		It is separate from the Playwright-controlled browser window that
		opens for OPAL login/sync automation. Closing this tab does not
		affect an in-progress sync's automation browser, and closing the
		automation browser does not affect this tab.
	</div>

	<nav>
		<ul>
			<li><a href="/settings">Settings</a></li>
			<li>Login <span class="soon">(coming soon)</span></li>
			<li>Sync <span class="soon">(coming soon)</span></li>
		</ul>
	</nav>
</body>
</html>
`))

func handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = landingTemplate.Execute(w, nil)
}
