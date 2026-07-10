package gui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/updater"
)

// fakeGitHub serves a minimal GitHub "get latest release" response plus the
// two release assets (installer + checksum sidecar) at whatever paths the
// caller wires up, so tests never touch the real network. It mirrors the
// shape internal/updater's own tests already use (see updater_test.go).
func fakeGitHub(t *testing.T, tagName string, assetContent string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	var srv *httptest.Server

	mux.HandleFunc("/repos/alu-developer/Opal_downloader/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"tag_name": tagName,
			"html_url": "https://github.example/releases/tag/" + tagName,
			"assets": []map[string]interface{}{
				{
					"name":                 updater.AssetName,
					"browser_download_url": srv.URL + "/asset",
					"size":                 int64(len(assetContent)),
				},
				{
					"name":                 updater.ChecksumAssetName,
					"browser_download_url": srv.URL + "/asset.sha256",
					"size":                 int64(64),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(assetContent))
	})
	mux.HandleFunc("/asset.sha256", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sha256HexOf(assetContent) + "  " + updater.AssetName))
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func sha256HexOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestCheckForUpdateOnce_CachesResult(t *testing.T) {
	srv := fakeGitHub(t, "v9.9.9", "fake installer bytes")

	s := &server{
		buildVersion:  "1.0.0",
		updaterClient: &updater.Client{BaseURL: srv.URL},
	}

	if checked, _, _ := s.debugUpdateState(); checked {
		t.Fatal("expected updateChecked to be false before checkForUpdateOnce runs")
	}

	s.checkForUpdateOnce(context.Background())

	checked, available, latest := s.debugUpdateState()
	if !checked {
		t.Fatal("expected updateChecked to be true after checkForUpdateOnce")
	}
	if !available {
		t.Error("expected an update to be reported available (v9.9.9 > 1.0.0)")
	}
	if latest != "9.9.9" {
		t.Errorf("latest version = %q, want 9.9.9", latest)
	}
}

func TestCheckForUpdateOnce_NoUpdateAvailable(t *testing.T) {
	srv := fakeGitHub(t, "v1.0.0", "fake installer bytes")

	s := &server{
		buildVersion:  "1.0.0",
		updaterClient: &updater.Client{BaseURL: srv.URL},
	}
	s.checkForUpdateOnce(context.Background())

	checked, available, _ := s.debugUpdateState()
	if !checked {
		t.Fatal("expected updateChecked to be true")
	}
	if available {
		t.Error("expected no update to be reported (same version)")
	}
}

// debugUpdateState is a small test helper exposing the mutex-guarded update
// fields without duplicating locking logic in every test.
func (s *server) debugUpdateState() (checked, available bool, latest string) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	return s.updateChecked, s.updateChecked && s.updateErr == nil && s.updateResult.IsNewer, s.updateResult.Version
}

// TestCheckForUpdateOnce_DevBuildSkipsNetworkCall reproduces a bug found
// during manual live verification: a "dev" buildVersion (the default for
// anything not built with the release -ldflags) can't be compared against a
// real release tag, and calling updater.CheckLatest with it surfaced a raw
// "not a parseable numeric version" error - shown verbatim on /update while
// the landing page simultaneously claimed "Running the latest version",
// which is misleading since the check never actually succeeded. The fix
// short-circuits before any network call and records a distinct
// updateDevBuild state instead.
func TestCheckForUpdateOnce_DevBuildSkipsNetworkCall(t *testing.T) {
	// No updaterClient set: if checkForUpdateOnce didn't short-circuit for
	// "dev", this would panic dialing the real github.com API in a unit
	// test, catching a regression immediately rather than just visually.
	s := &server{buildVersion: "dev"}
	s.checkForUpdateOnce(context.Background())

	s.updateMu.Lock()
	checked, devBuild, err := s.updateChecked, s.updateDevBuild, s.updateErr
	s.updateMu.Unlock()

	if !checked {
		t.Fatal("expected updateChecked to be true after checkForUpdateOnce")
	}
	if !devBuild {
		t.Error("expected updateDevBuild to be true for buildVersion \"dev\"")
	}
	if err != nil {
		t.Errorf("expected no error recorded for the dev-build case, got %v", err)
	}
}

func TestHandleLanding_DevBuildShowsNeitherErrorNorFalseUpToDateClaim(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	s := &server{configPath: configPath, buildVersion: "dev"}
	s.checkForUpdateOnce(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	s.handleLanding(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "Update checks are unavailable for development builds") {
		t.Errorf("expected the dev-build explanation in landing page body, got:\n%s", body)
	}
	if strings.Contains(body, "Running the latest version") {
		t.Errorf("dev build must not falsely claim to be up to date, got:\n%s", body)
	}
	if strings.Contains(body, "not a parseable") {
		t.Errorf("dev build must not surface the raw parse error, got:\n%s", body)
	}
}

func TestHandleUpdatePage_DevBuildShowsNeitherErrorNorFalseUpToDateClaim(t *testing.T) {
	s := &server{buildVersion: "dev"}
	s.checkForUpdateOnce(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/update", nil)
	rr := httptest.NewRecorder()
	s.handleUpdatePage(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "Update checks are unavailable for development builds") {
		t.Errorf("expected the dev-build explanation on /update, got:\n%s", body)
	}
	if strings.Contains(body, "Running the latest version") {
		t.Errorf("dev build must not falsely claim to be up to date on /update, got:\n%s", body)
	}
	if strings.Contains(body, "not a parseable") {
		t.Errorf("dev build must not surface the raw parse error on /update, got:\n%s", body)
	}
}

func TestHandleLanding_ShowsUpdateBanner(t *testing.T) {
	srv := fakeGitHub(t, "v9.9.9", "fake installer bytes")

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	s := &server{
		configPath:    configPath,
		buildVersion:  "1.0.0",
		updaterClient: &updater.Client{BaseURL: srv.URL},
	}
	s.checkForUpdateOnce(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	s.handleLanding(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "An update is available") {
		t.Errorf("expected update banner in landing page body, got:\n%s", body)
	}
	if !strings.Contains(body, "9.9.9") {
		t.Errorf("expected latest version 9.9.9 mentioned in body")
	}
}

func TestHandleLanding_NoBannerBeforeCheckCompletes(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	s := &server{configPath: configPath, buildVersion: "1.0.0"}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	s.handleLanding(rr, req)

	body := rr.Body.String()
	if strings.Contains(body, "An update is available") {
		t.Errorf("expected no update banner before any check has completed, got:\n%s", body)
	}
	if strings.Contains(body, "Running the latest version") {
		t.Errorf("expected no 'up to date' status before any check has completed either, got:\n%s", body)
	}
}

func TestHandleUpdateStart_DownloadsVerifiesAndLaunches(t *testing.T) {
	const assetContent = "fake installer bytes for launch test"
	srv := fakeGitHub(t, "v9.9.9", assetContent)

	s := &server{
		buildVersion:  "1.0.0",
		updaterClient: &updater.Client{BaseURL: srv.URL},
	}
	s.checkForUpdateOnce(context.Background())

	var mu sync.Mutex
	var launchedPath string
	launched := make(chan struct{})
	exited := make(chan struct{})
	s.launchInstaller = func(path string) error {
		mu.Lock()
		launchedPath = path
		mu.Unlock()
		close(launched)
		return nil
	}
	s.exitProcess = func() { close(exited) }

	req := httptest.NewRequest(http.MethodPost, "/update/start", nil)
	rr := httptest.NewRecorder()
	s.handleUpdateStart(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUpdateStart status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "this app will close") {
		t.Errorf("expected 'starting installer' page, got:\n%s", rr.Body.String())
	}

	select {
	case <-launched:
	case <-time.After(2 * time.Second):
		t.Fatal("launchInstaller was never called")
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("exitProcess was never called after a successful launch")
	}

	mu.Lock()
	path := launchedPath
	mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading downloaded installer at %s: %v", path, err)
	}
	if string(data) != assetContent {
		t.Errorf("downloaded installer content = %q, want %q", data, assetContent)
	}
	_ = os.Remove(path)
}

func TestHandleUpdateStart_LaunchFailureReportsErrorAndDoesNotExit(t *testing.T) {
	// Regression test for a real gap found live during this task's exec+exit
	// spike: launching the installer can fail (e.g. Windows requiring
	// elevation the calling process can't silently grant - confirmed live
	// with an unsigned stand-in installer during this task). The handler
	// must report that failure instead of rendering "this app will close
	// now" and exiting a process that never actually handed off to
	// anything.
	const assetContent = "fake installer bytes for launch-failure test"
	srv := fakeGitHub(t, "v9.9.9", assetContent)

	s := &server{
		buildVersion:  "1.0.0",
		updaterClient: &updater.Client{BaseURL: srv.URL},
	}
	s.checkForUpdateOnce(context.Background())

	s.launchInstaller = func(path string) error {
		return fmt.Errorf("The requested operation requires elevation.")
	}
	exitCalled := false
	s.exitProcess = func() { exitCalled = true }

	req := httptest.NewRequest(http.MethodPost, "/update/start", nil)
	rr := httptest.NewRecorder()
	s.handleUpdateStart(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rr.Body.String(), "this app will close") {
		t.Errorf("must not claim the app is closing when the installer failed to launch, got:\n%s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "requires elevation") {
		t.Errorf("expected the launch error surfaced in the response body, got:\n%s", rr.Body.String())
	}
	if exitCalled {
		t.Error("exitProcess must not be called when launchInstaller failed")
	}
}

func TestHandleUpdateStart_NoUpdateAvailableRejected(t *testing.T) {
	srv := fakeGitHub(t, "v1.0.0", "fake installer bytes")

	s := &server{
		buildVersion:  "1.0.0",
		updaterClient: &updater.Client{BaseURL: srv.URL},
	}
	s.checkForUpdateOnce(context.Background())

	launchCalled := false
	s.launchInstaller = func(string) error { launchCalled = true; return nil }

	req := httptest.NewRequest(http.MethodPost, "/update/start", nil)
	rr := httptest.NewRecorder()
	s.handleUpdateStart(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if launchCalled {
		t.Error("launchInstaller should not be called when no update is available")
	}
}

func TestHandleUpdateStart_MethodNotAllowed(t *testing.T) {
	s := &server{buildVersion: "1.0.0"}
	req := httptest.NewRequest(http.MethodGet, "/update/start", nil)
	rr := httptest.NewRecorder()
	s.handleUpdateStart(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleUpdatePage(t *testing.T) {
	srv := fakeGitHub(t, "v9.9.9", "fake installer bytes")
	s := &server{
		buildVersion:  "1.0.0",
		updaterClient: &updater.Client{BaseURL: srv.URL},
	}
	s.checkForUpdateOnce(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/update", nil)
	rr := httptest.NewRecorder()
	s.handleUpdatePage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "9.9.9") {
		t.Errorf("expected latest version in /update page body, got:\n%s", body)
	}
	if !strings.Contains(body, url.PathEscape("/update/start")) && !strings.Contains(body, "/update/start") {
		t.Errorf("expected a form pointing at /update/start, got:\n%s", body)
	}
}
