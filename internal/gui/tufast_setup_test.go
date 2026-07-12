package gui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// writeTestConfig writes a minimal config.yaml with the given
// browser_user_data_dir/browser_profile_directory so handleTUFastSetupCopy
// has a target to resolve.
func writeTestConfig(t *testing.T, configPath, browserUserDataDir, browserProfileDir string) {
	t.Helper()
	loaded := config.Loaded{
		App: config.App{
			DownloadPath: "./downloads",
			Courses:      []string{"*"},
			Sync:         true,
		},
		Credentials: config.Credentials{
			URL:                config.DefaultOPALURL,
			StateFile:          config.DefaultStateFile,
			BrowserUserDataDir: browserUserDataDir,
			BrowserProfileDir:  browserProfileDir,
		},
	}
	if err := config.Save(configPath, loaded); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
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
	configPath := filepath.Join(dir, "config.yaml")
	writeTestConfig(t, configPath, "", "")

	targetDir := filepath.Join(dir, "login-profile")

	var launchedExe, launchedUserDataDir, launchedProfile, launchedURL string
	s := &server{
		configPath: configPath,
		launchBrowserAt: func(browserExecutable, userDataDir, profileDirectory, u string) error {
			launchedExe = browserExecutable
			launchedUserDataDir = userDataDir
			launchedProfile = profileDirectory
			launchedURL = u
			return nil
		},
	}

	form := url.Values{}
	form.Set("target_user_data_dir", targetDir)
	form.Set("browser_executable", "fake-browser.exe")
	req := httptest.NewRequest(http.MethodPost, "/tufast-setup/open", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	s.handleTUFastSetupOpen(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(targetDir); err != nil {
		t.Fatalf("expected %s to be created: %v", targetDir, err)
	}
	if launchedExe != "fake-browser.exe" {
		t.Fatalf("launched exe = %q", launchedExe)
	}
	if launchedUserDataDir != targetDir {
		t.Fatalf("launched user data dir = %q, want %q", launchedUserDataDir, targetDir)
	}
	if launchedProfile != "Default" {
		t.Fatalf("launched profile = %q, want Default", launchedProfile)
	}
	if !strings.Contains(launchedURL, scraper.TUFastExtensionID) {
		t.Fatalf("launched URL = %q, want it to reference the TU-Fast extension ID", launchedURL)
	}
	if !strings.Contains(rec.Body.String(), "Opened a new browser window") {
		t.Fatalf("body missing success message: %s", rec.Body.String())
	}
}

func TestHandleTUFastSetupOpen_NoBrowserFound(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	writeTestConfig(t, configPath, "", "")

	launchCalled := false
	s := &server{
		configPath:    configPath,
		detectBrowser: func() string { return "" }, // simulate no Brave/Chrome found, regardless of what's actually installed on the machine running this test
		launchBrowserAt: func(browserExecutable, userDataDir, profileDirectory, u string) error {
			launchCalled = true
			return nil
		},
	}

	form := url.Values{}
	form.Set("target_user_data_dir", filepath.Join(dir, "login-profile"))
	form.Set("browser_executable", "") // force auto-detection (stubbed above to fail)
	req := httptest.NewRequest(http.MethodPost, "/tufast-setup/open", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	s.handleTUFastSetupOpen(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Could not find a Brave or Chrome install") {
		t.Fatalf("body missing expected error: %s", rec.Body.String())
	}
	if launchCalled {
		t.Fatal("launchBrowserAt should not have been called when no browser was found")
	}
}

func TestHandleTUFastSetupCopy_NoTargetConfigured(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	writeTestConfig(t, configPath, "", "")

	s := &server{configPath: configPath}

	form := url.Values{}
	form.Set("source_user_data_dir", filepath.Join(dir, "source"))
	req := httptest.NewRequest(http.MethodPost, "/tufast-setup/copy", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	s.handleTUFastSetupCopy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Settings first") {
		t.Fatalf("body missing expected guidance: %s", rec.Body.String())
	}
}

// The full success path (source has data, target has TU-Fast installed,
// copy actually happens) is deliberately NOT re-tested at this layer with a
// real scraper.TransplantTUFastLoginData call - internal/scraper's own
// TestTransplantTUFastLoginData_Success and
// TestTransplantTUFastLoginData_BacksUpExistingTargetData already cover
// that exact logic directly and reliably. Doing it again here would also
// exercise TransplantTUFastLoginData's real isUserDataDirLocked call, which
// (found live while writing this test, 2026-07-12) can hang for minutes on
// a real interactive desktop: isWindowsSingletonWindowPresent's
// GetWindowTextW has no timeout, so a single unresponsive window anywhere
// on the desktop blocks the whole enumeration - a pre-existing issue in
// profile_lock_windows.go unrelated to this feature (it affects the
// existing login/sync lock check too), flagged separately rather than
// fixed here. TestHandleTUFastSetupCopy_TransplantError below stays safe
// because its source directory doesn't exist, so TransplantTUFastLoginData
// returns before ever reaching the lock check.

func TestHandleTUFastSetupCopy_TransplantError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	targetRoot := filepath.Join(dir, "target")
	makeFakeChromiumProfile(t, targetRoot, "Default", true, "") // TU-Fast installed, no source given

	writeTestConfig(t, configPath, targetRoot, "Default")

	s := &server{configPath: configPath}

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
	configPath := filepath.Join(dir, "config.yaml")
	writeTestConfig(t, configPath, "", "")

	s := &server{configPath: configPath}

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
