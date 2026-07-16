package gui

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// fakeLoginProfileDir returns a server.loginProfileDir override that always
// resolves to dir, so tests don't touch the real
// ~/.opal-downloader/login-profile on the machine running `go test`.
func fakeLoginProfileDir(dir string) func() (string, error) {
	return func() (string, error) { return dir, nil }
}

func makeFakeChromiumProfile(t *testing.T, userDataDir, profile string, withExtension bool, dataContent string) {
	t.Helper()
	profileDir := filepath.Join(userDataDir, profile)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Preferences"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile Preferences: %v", err)
	}
	if withExtension {
		extDir := filepath.Join(profileDir, "Extensions", scraper.TUFastExtensionID, "8.3.0.0_0")
		if err := os.MkdirAll(extDir, 0o755); err != nil {
			t.Fatalf("MkdirAll extension dir: %v", err)
		}
	}
	if dataContent != "" {
		dataDir := filepath.Join(profileDir, "Local Extension Settings", scraper.TUFastExtensionID)
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			t.Fatalf("MkdirAll data dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, "000003.log"), []byte(dataContent), 0o644); err != nil {
			t.Fatalf("WriteFile fake data: %v", err)
		}
	}
}

func TestHandleTUFastSetupOpen_CreatesDirAndLaunchesBrowser(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "login-profile")

	var launchedURL string
	s := &server{
		loginProfileDir: fakeLoginProfileDir(targetDir),
		launchBrowserAt: func(u string) error {
			launchedURL = u
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/tufast-setup/open", nil)
	rec := httptest.NewRecorder()

	s.handleTUFastSetupOpen(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(targetDir); err != nil {
		t.Fatalf("expected %s to be created: %v", targetDir, err)
	}
	if !strings.Contains(launchedURL, scraper.TUFastExtensionID) {
		t.Fatalf("launched URL = %q, want it to reference the TU-Fast extension ID", launchedURL)
	}
	if !strings.Contains(rec.Body.String(), "Opened Chromium") {
		t.Fatalf("body missing success message: %s", rec.Body.String())
	}
}

func TestHandleTUFastSetupOpen_LaunchFails(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "login-profile")

	s := &server{
		loginProfileDir: fakeLoginProfileDir(targetDir),
		launchBrowserAt: func(u string) error {
			return errors.New("simulated launch failure")
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/tufast-setup/open", nil)
	rec := httptest.NewRecorder()

	s.handleTUFastSetupOpen(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "simulated launch failure") {
		t.Fatalf("body missing expected launch error: %s", rec.Body.String())
	}
	// The directory is still created before the launch attempt, even though
	// the launch itself failed.
	if _, err := os.Stat(targetDir); err != nil {
		t.Fatalf("expected %s to still be created: %v", targetDir, err)
	}
}

func TestHandleTUFastSetupCopy_TransplantError(t *testing.T) {
	dir := t.TempDir()

	targetRoot := filepath.Join(dir, "target")
	makeFakeChromiumProfile(t, targetRoot, "Default", true, "") // TU-Fast installed, no source given

	s := &server{loginProfileDir: fakeLoginProfileDir(targetRoot)}

	form := url.Values{}
	form.Set("source_user_data_dir", filepath.Join(dir, "does-not-exist"))
	req := httptest.NewRequest(http.MethodPost, "/tufast-setup/copy", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	s.handleTUFastSetupCopy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "doesn&#39;t exist") && !strings.Contains(rec.Body.String(), "doesn't exist") {
		t.Fatalf("body missing expected error about missing source: %s", rec.Body.String())
	}
}

func TestHandleTUFastSetupPage_GET(t *testing.T) {
	dir := t.TempDir()
	s := &server{loginProfileDir: fakeLoginProfileDir(filepath.Join(dir, "login-profile"))}

	req := httptest.NewRequest(http.MethodGet, "/tufast-setup", nil)
	rec := httptest.NewRecorder()
	s.handleTUFastSetupPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TU-Fast browser profile setup") {
		t.Fatalf("body missing page title: %s", rec.Body.String())
	}
}
