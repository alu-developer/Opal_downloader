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

// grantedServer sets s's consent gate to tuFastConsentGranted and returns
// it - the state most of the install/copy-handler tests below want to
// start from, since those handlers now refuse to act before consent is
// granted. Takes and returns a pointer (rather than a value) because
// server embeds sync.Mutex fields, which must never be copied.
func grantedServer(s *server) *server {
	s.tuFastConsent = tuFastConsentGranted
	return s
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
	s := grantedServer(&server{
		loginProfileDir:          fakeLoginProfileDir(targetDir),
		detectBrowserUserDataDir: func() string { return "" }, // pin: don't depend on what's actually installed on the machine running `go test`
		launchBrowserAt: func(u string) error {
			launchedURL = u
			return nil
		},
	})

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

	s := grantedServer(&server{
		loginProfileDir:          fakeLoginProfileDir(targetDir),
		detectBrowserUserDataDir: func() string { return "" }, // pin: don't depend on what's actually installed on the machine running `go test`
		launchBrowserAt: func(u string) error {
			return errors.New("simulated launch failure")
		},
	})

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

func TestHandleTUFastSetupOpen_RequiresConsent(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "login-profile")

	launched := false
	s := &server{
		loginProfileDir: fakeLoginProfileDir(targetDir),
		launchBrowserAt: func(u string) error {
			launched = true
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/tufast-setup/open", nil)
	rec := httptest.NewRecorder()

	s.handleTUFastSetupOpen(rec, req)

	if launched {
		t.Fatalf("expected launch to be refused without consent")
	}
	if !strings.Contains(rec.Body.String(), "Consent is required") {
		t.Fatalf("body missing consent-required message: %s", rec.Body.String())
	}
	if _, err := os.Stat(targetDir); err == nil {
		t.Fatalf("expected %s NOT to be created without consent", targetDir)
	}
}

func TestHandleTUFastSetupCopy_TransplantError(t *testing.T) {
	dir := t.TempDir()

	targetRoot := filepath.Join(dir, "target")
	makeFakeChromiumProfile(t, targetRoot, "Default", true, "") // TU-Fast installed, no source given

	// detectBrowserUserDataDir must be pinned to "" here, not left to fall
	// back to the real, machine-dependent function: this test's whole
	// point is the "already installed, nothing else detected" case (the
	// production bug this guards against only reproduced when
	// DetectedSourceDir was empty - it passed locally on a dev machine
	// with a real Brave install, which made DetectedSourceDir non-empty
	// and accidentally exercised a different code path, but failed in CI
	// where no such browser exists). Pinning it makes the test's actual
	// scenario explicit and independent of what's installed wherever it
	// runs.
	s := grantedServer(&server{
		loginProfileDir:          fakeLoginProfileDir(targetRoot),
		detectBrowserUserDataDir: func() string { return "" },
	})

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

func TestHandleTUFastSetupCopy_RequiresConsent(t *testing.T) {
	dir := t.TempDir()
	targetRoot := filepath.Join(dir, "target")
	makeFakeChromiumProfile(t, targetRoot, "Default", true, "")

	s := &server{loginProfileDir: fakeLoginProfileDir(targetRoot)}

	form := url.Values{}
	form.Set("source_user_data_dir", filepath.Join(dir, "source"))
	req := httptest.NewRequest(http.MethodPost, "/tufast-setup/copy", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	s.handleTUFastSetupCopy(rec, req)

	if !strings.Contains(rec.Body.String(), "Consent is required") {
		t.Fatalf("body missing consent-required message: %s", rec.Body.String())
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
	if !strings.Contains(rec.Body.String(), "TU-Fast setup") {
		t.Fatalf("body missing page title: %s", rec.Body.String())
	}
}

func TestHandleTUFastSetupPage_ConsentGateDefaultsToUnset(t *testing.T) {
	dir := t.TempDir()
	s := &server{loginProfileDir: fakeLoginProfileDir(filepath.Join(dir, "login-profile"))}

	req := httptest.NewRequest(http.MethodGet, "/tufast-setup", nil)
	rec := httptest.NewRecorder()
	s.handleTUFastSetupPage(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Set up automatic login?") {
		t.Fatalf("expected the unset-state consent question, got: %s", body)
	}
	// No install/copy action should be reachable before consent.
	if strings.Contains(body, `action="/tufast-setup/open"`) {
		t.Fatalf("install form should not be reachable before consent: %s", body)
	}
	if strings.Contains(body, `action="/tufast-setup/copy"`) {
		t.Fatalf("copy form should not be reachable before consent: %s", body)
	}
}

func postConsent(t *testing.T, s *server, choice string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("choice", choice)
	req := httptest.NewRequest(http.MethodPost, "/tufast-setup/consent", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleTUFastSetupConsent(rec, req)
	return rec
}

func TestHandleTUFastSetupConsent_YesRevealsExplicitInstallWarning(t *testing.T) {
	dir := t.TempDir()
	s := &server{loginProfileDir: fakeLoginProfileDir(filepath.Join(dir, "login-profile"))}

	rec := postConsent(t, s, "yes")

	if got := s.getTUFastConsent(); got != tuFastConsentPendingConfirm {
		t.Fatalf("consent state = %q, want %q", got, tuFastConsentPendingConfirm)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "third-party") || !strings.Contains(body, "extension") {
		t.Fatalf("expected explicit third-party-extension warning, got: %s", body)
	}
	// Still no install/copy action reachable until "confirm".
	if strings.Contains(body, `action="/tufast-setup/open"`) {
		t.Fatalf("install form should not be reachable before explicit confirm: %s", body)
	}
}

func TestHandleTUFastSetupConsent_NoExitsFlowCleanly(t *testing.T) {
	dir := t.TempDir()
	s := &server{loginProfileDir: fakeLoginProfileDir(filepath.Join(dir, "login-profile"))}

	rec := postConsent(t, s, "no")

	if got := s.getTUFastConsent(); got != tuFastConsentDeclined {
		t.Fatalf("consent state = %q, want %q", got, tuFastConsentDeclined)
	}
	body := rec.Body.String()
	if strings.Contains(body, `action="/tufast-setup/open"`) || strings.Contains(body, `action="/tufast-setup/copy"`) {
		t.Fatalf("no TU-Fast install/copy UI should show after declining: %s", body)
	}
}

func TestHandleTUFastSetupConsent_ConfirmGrantsAndCancelDeclines(t *testing.T) {
	dir := t.TempDir()
	s := &server{loginProfileDir: fakeLoginProfileDir(filepath.Join(dir, "login-profile"))}

	postConsent(t, s, "yes")
	rec := postConsent(t, s, "confirm")
	if got := s.getTUFastConsent(); got != tuFastConsentGranted {
		t.Fatalf("consent state = %q, want %q", got, tuFastConsentGranted)
	}
	if !strings.Contains(rec.Body.String(), `action="/tufast-setup/open"`) {
		t.Fatalf("install form should be reachable once granted: %s", rec.Body.String())
	}

	s2 := &server{loginProfileDir: fakeLoginProfileDir(filepath.Join(dir, "login-profile-2"))}
	postConsent(t, s2, "yes")
	postConsent(t, s2, "cancel")
	if got := s2.getTUFastConsent(); got != tuFastConsentDeclined {
		t.Fatalf("consent state after cancel = %q, want %q", got, tuFastConsentDeclined)
	}
}

func TestHandleTUFastSetupConsent_ResetReturnsToUnset(t *testing.T) {
	dir := t.TempDir()
	s := &server{loginProfileDir: fakeLoginProfileDir(filepath.Join(dir, "login-profile"))}

	postConsent(t, s, "no")
	rec := postConsent(t, s, "reset")

	if got := s.getTUFastConsent(); got != tuFastConsentUnset {
		t.Fatalf("consent state = %q, want %q", got, tuFastConsentUnset)
	}
	if !strings.Contains(rec.Body.String(), "Set up automatic login?") {
		t.Fatalf("expected to be back at the consent question: %s", rec.Body.String())
	}
}

func TestLoadTUFastSetupPageData_AlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	targetRoot := filepath.Join(dir, "target")
	makeFakeChromiumProfile(t, targetRoot, "Default", true, "")

	s := grantedServer(&server{
		loginProfileDir:          fakeLoginProfileDir(targetRoot),
		detectBrowserUserDataDir: func() string { return "" },
	})

	data := s.loadTUFastSetupPageData()
	if !data.AlreadyInstalled {
		t.Fatalf("expected AlreadyInstalled = true, got data = %+v", data)
	}
	if data.DetectedSourceDir != "" {
		t.Fatalf("expected no detected source dir, got %q", data.DetectedSourceDir)
	}
}

func TestLoadTUFastSetupPageData_TransplantAvailable(t *testing.T) {
	dir := t.TempDir()
	targetRoot := filepath.Join(dir, "target")
	makeFakeChromiumProfile(t, targetRoot, "Default", false, "") // no TU-Fast yet

	sourceRoot := filepath.Join(dir, "source")
	makeFakeChromiumProfile(t, sourceRoot, "Default", true, "fake-data")

	s := grantedServer(&server{
		loginProfileDir:          fakeLoginProfileDir(targetRoot),
		detectBrowserUserDataDir: func() string { return sourceRoot },
	})

	data := s.loadTUFastSetupPageData()
	if data.AlreadyInstalled {
		t.Fatalf("expected AlreadyInstalled = false, got data = %+v", data)
	}
	if data.DetectedSourceDir != sourceRoot {
		t.Fatalf("DetectedSourceDir = %q, want %q", data.DetectedSourceDir, sourceRoot)
	}
	if data.CopySourceUserDataDir != sourceRoot {
		t.Fatalf("CopySourceUserDataDir = %q, want prefilled to %q", data.CopySourceUserDataDir, sourceRoot)
	}
}

func TestLoadTUFastSetupPageData_ManualPathWhenNothingDetected(t *testing.T) {
	dir := t.TempDir()
	targetRoot := filepath.Join(dir, "target")
	makeFakeChromiumProfile(t, targetRoot, "Default", false, "")

	s := grantedServer(&server{
		loginProfileDir:          fakeLoginProfileDir(targetRoot),
		detectBrowserUserDataDir: func() string { return "" },
	})

	data := s.loadTUFastSetupPageData()
	if data.AlreadyInstalled || data.DetectedSourceDir != "" {
		t.Fatalf("expected neither already-installed nor a detected source, got data = %+v", data)
	}
}

func TestLoadTUFastSetupPageData_AlreadyInstalledComputedBeforeConsent(t *testing.T) {
	dir := t.TempDir()
	targetRoot := filepath.Join(dir, "target")
	makeFakeChromiumProfile(t, targetRoot, "Default", true, "")

	// ConsentState is unset (the default, and what every fresh GUI process
	// restart resets to) - AlreadyInstalled must still be detected: it's a
	// real filesystem fact, not something that should wait behind the
	// in-memory consent gate. Regression test for the bug where a GUI
	// restart made the "already installed, nothing more to do" message
	// unreachable until the user re-clicked through consent. The
	// transplant-source probe (detectBrowserUserDataDir), however, is part
	// of the still consent-gated install flow and must NOT run yet.
	s := &server{
		loginProfileDir:          fakeLoginProfileDir(targetRoot),
		detectBrowserUserDataDir: func() string { t.Fatal("detectBrowserUserDataDir should not be called before consent"); return "" },
	}

	data := s.loadTUFastSetupPageData()
	if !data.AlreadyInstalled {
		t.Fatalf("expected AlreadyInstalled = true even before consent, got data = %+v", data)
	}
	if data.ConsentState != tuFastConsentUnset {
		t.Fatalf("expected ConsentState = %q (untouched), got %q", tuFastConsentUnset, data.ConsentState)
	}
}

// TestHandleTUFastSetupPage_AlreadyInstalledSkipsConsentGate is the
// page-render-level companion to
// TestLoadTUFastSetupPageData_AlreadyInstalledComputedBeforeConsent: with a
// fresh/unset consent state (as after a GUI process restart) and TU-Fast
// already installed in the dedicated profile, the page must show the
// "already installed" message immediately, not the "Set up automatic
// login?" consent gate.
func TestHandleTUFastSetupPage_AlreadyInstalledSkipsConsentGate(t *testing.T) {
	dir := t.TempDir()
	targetRoot := filepath.Join(dir, "target")
	makeFakeChromiumProfile(t, targetRoot, "Default", true, "")

	s := &server{
		loginProfileDir:          fakeLoginProfileDir(targetRoot),
		detectBrowserUserDataDir: func() string { t.Fatal("detectBrowserUserDataDir should not be called before consent"); return "" },
	}

	req := httptest.NewRequest(http.MethodGet, "/tufast-setup", nil)
	rec := httptest.NewRecorder()
	s.handleTUFastSetupPage(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "TU-Fast is already installed") {
		t.Fatalf("expected the already-installed message with unset consent, got: %s", body)
	}
	if strings.Contains(body, "Set up automatic login?") {
		t.Fatalf("consent gate should be skipped once already installed: %s", body)
	}
}

func TestDetectBrowserUserDataDir_FirstMatchWins(t *testing.T) {
	// This exercises the real, unmocked detectBrowserUserDataDir against
	// whatever is actually installed on the machine running the test - it
	// can't assert a specific path, but it can assert the "" or a real,
	// existing directory contract holds.
	got := detectBrowserUserDataDir()
	if got == "" {
		return
	}
	info, err := os.Stat(got)
	if err != nil || !info.IsDir() {
		t.Fatalf("detectBrowserUserDataDir() = %q, want either empty or an existing directory (stat err: %v)", got, err)
	}
}

func TestHasTUFastExtension(t *testing.T) {
	dir := t.TempDir()

	withExt := filepath.Join(dir, "with-ext")
	makeFakeChromiumProfile(t, withExt, "Default", true, "")
	if !hasTUFastExtension(withExt) {
		t.Fatalf("expected hasTUFastExtension(%q) = true", withExt)
	}

	withoutExt := filepath.Join(dir, "without-ext")
	makeFakeChromiumProfile(t, withoutExt, "Default", false, "")
	if hasTUFastExtension(withoutExt) {
		t.Fatalf("expected hasTUFastExtension(%q) = false", withoutExt)
	}

	if hasTUFastExtension(filepath.Join(dir, "does-not-exist")) {
		t.Fatalf("expected hasTUFastExtension on a nonexistent dir = false")
	}
	if hasTUFastExtension("") {
		t.Fatalf("expected hasTUFastExtension(\"\") = false")
	}
}
