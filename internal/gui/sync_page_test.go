package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The /sync page is one link away from the landing page, which carefully
// disables its "Sync now" button and explains why whenever a click would
// fail (no config, or configured but not logged in). The /sync page's own
// "Sync" button must be gated the same way instead of staying live.

func writeSyncPageConfig(t *testing.T, courses string) (configPath, stateFile string) {
	t.Helper()
	dir := t.TempDir()
	configPath = filepath.Join(dir, "config.yaml")
	stateFile = filepath.Join(dir, "state.json")
	yaml := `download_path: ./downloads
courses:` + courses + `
sync: true
opal_url: https://bildungsportal.sachsen.de/opal/
session_state_file: ` + filepath.ToSlash(stateFile) + `
`
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, stateFile
}

// The second button was called "List courses" until 2026-07-26. The name was
// wrong twice over, and the maintainer said so directly: it does not really
// list courses (it reports how many files each one holds), and it gets there
// by crawling every section of every course - 210s and 482s in two measured
// runs. Sitting next to "Sync" and reading like a cheap lookup, it invited a
// click that then took minutes with no warning.
func TestSyncPagePreviewButtonIsHonestAboutWhatItCosts(t *testing.T) {
	configPath, stateFile := writeSyncPageConfig(t, "\n  - Analysis")
	if err := os.WriteFile(stateFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}
	srv := &server{configPath: configPath}
	sp := newSyncPage(configPath, srv)

	rec := httptest.NewRecorder()
	sp.handlePage(rec, httptest.NewRequest(http.MethodGet, "/sync", nil))
	body := rec.Body.String()

	if strings.Contains(body, "List courses") {
		t.Fatal(`the button is called "List courses" again; it neither lists courses nor is quick`)
	}
	if !strings.Contains(body, "Preview sync (no download)") {
		t.Fatalf("expected the preview button, body was:\n%s", body)
	}
	// Naming it honestly is only half the fix - the cost has to be stated
	// before the click, not discovered after it.
	if !strings.Contains(body, "about as long as a sync") {
		t.Fatalf("expected the page to warn that a preview costs a full crawl, body was:\n%s", body)
	}
	if !strings.Contains(body, "without downloading anything") {
		t.Fatalf("expected the page to say a preview downloads nothing, body was:\n%s", body)
	}
}

func TestSyncPageDisablesButtonWhenConfigMissing(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	srv := &server{configPath: configPath}
	sp := newSyncPage(configPath, srv)

	rec := httptest.NewRecorder()
	sp.handlePage(rec, httptest.NewRequest(http.MethodGet, "/sync", nil))
	body := rec.Body.String()

	if !strings.Contains(body, `id="btn-sync" disabled`) {
		t.Fatalf("expected the Sync button disabled with no config, body was:\n%s", body)
	}
	if !strings.Contains(body, `<a href="/settings">Set up opal-downloader</a>`) {
		t.Fatalf("expected a setup link when no config exists, body was:\n%s", body)
	}
}

func TestSyncPageDisablesButtonWhenNotLoggedIn(t *testing.T) {
	configPath, _ := writeSyncPageConfig(t, "\n  - Analysis")
	srv := &server{configPath: configPath}
	sp := newSyncPage(configPath, srv)

	rec := httptest.NewRecorder()
	sp.handlePage(rec, httptest.NewRequest(http.MethodGet, "/sync", nil))
	body := rec.Body.String()

	if !strings.Contains(body, `id="btn-sync" disabled`) {
		t.Fatalf("expected the Sync button disabled when not logged in, body was:\n%s", body)
	}
	if !strings.Contains(body, "Not logged in") {
		t.Fatalf("expected the login-specific reason on the page, body was:\n%s", body)
	}
}

func TestSyncPageEnablesButtonWhenReady(t *testing.T) {
	configPath, stateFile := writeSyncPageConfig(t, "\n  - Analysis")
	if err := os.WriteFile(stateFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}
	srv := &server{configPath: configPath}
	sp := newSyncPage(configPath, srv)

	rec := httptest.NewRecorder()
	sp.handlePage(rec, httptest.NewRequest(http.MethodGet, "/sync", nil))
	body := rec.Body.String()

	if strings.Contains(body, `id="btn-sync" disabled`) {
		t.Fatalf("expected the Sync button enabled once configured and logged in, body was:\n%s", body)
	}
	if strings.Contains(body, `class="status warn"`) {
		t.Fatalf("expected no blocked-state banner once ready, body was:\n%s", body)
	}
}
