package opaldownloader

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/updater"
	"github.com/alu-developer/opal-downloader/internal/visitlog"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading pipe: %v", err)
	}
	return string(out)
}

func TestPrintUpdateFooter_NewerVersionAvailable(t *testing.T) {
	origFn := updaterCheckLatest
	origVersion := buildVersion
	defer func() {
		updaterCheckLatest = origFn
		buildVersion = origVersion
	}()

	buildVersion = "0.1.0"
	updaterCheckLatest = func(ctx context.Context, currentVersion string) (updater.Release, error) {
		if currentVersion != "0.1.0" {
			t.Errorf("expected currentVersion %q, got %q", "0.1.0", currentVersion)
		}
		return updater.Release{
			TagName: "v0.2.0",
			Version: "0.2.0",
			HTMLURL: "https://github.example/releases/tag/v0.2.0",
			IsNewer: true,
		}, nil
	}

	out := captureStdout(t, printUpdateFooter)

	if !strings.Contains(out, "v0.2.0") {
		t.Errorf("expected output to mention v0.2.0, got %q", out)
	}
	if !strings.Contains(out, "https://github.example/releases/tag/v0.2.0") {
		t.Errorf("expected output to contain release URL, got %q", out)
	}
}

func TestPrintUpdateFooter_NoNewerVersion(t *testing.T) {
	origFn := updaterCheckLatest
	defer func() { updaterCheckLatest = origFn }()

	updaterCheckLatest = func(ctx context.Context, currentVersion string) (updater.Release, error) {
		return updater.Release{Version: "0.1.0", IsNewer: false}, nil
	}

	out := captureStdout(t, printUpdateFooter)

	if out != "" {
		t.Errorf("expected no output when no newer version is available, got %q", out)
	}
}

func TestPrintUpdateFooter_ErrorIsSilent(t *testing.T) {
	origFn := updaterCheckLatest
	defer func() { updaterCheckLatest = origFn }()

	updaterCheckLatest = func(ctx context.Context, currentVersion string) (updater.Release, error) {
		return updater.Release{}, errors.New("network unreachable")
	}

	out := captureStdout(t, printUpdateFooter)

	if out != "" {
		t.Errorf("expected no output on error, got %q", out)
	}
}

func TestPrintUpdateFooter_DoesNotHangOnSlowCheck(t *testing.T) {
	origFn := updaterCheckLatest
	defer func() { updaterCheckLatest = origFn }()

	// Simulate an unreachable/slow API: block until the context deadline
	// fires, then return its error - this exercises the same code path a
	// real offline/slow network hit would take, without an actual network
	// dependency in the test.
	updaterCheckLatest = func(ctx context.Context, currentVersion string) (updater.Release, error) {
		<-ctx.Done()
		return updater.Release{}, ctx.Err()
	}

	done := make(chan string, 1)
	go func() {
		done <- captureStdout(t, printUpdateFooter)
	}()

	select {
	case out := <-done:
		if out != "" {
			t.Errorf("expected no output when check times out, got %q", out)
		}
	case <-time.After(updateCheckTimeout + 2*time.Second):
		t.Fatal("printUpdateFooter did not return within the expected timeout window")
	}
}

// TestPersistVisitLogNoopWhenNoRecords exercises persistVisitLog's early-out
// for a scraper that recorded nothing (e.g. a scrape that failed before
// visiting any section) - it must not create a log file at all.
func TestPersistVisitLogNoopWhenNoRecords(t *testing.T) {
	dir := t.TempDir()
	sc := scraper.New("", "", "", "", "")

	if err := persistVisitLog(sc, dir); err != nil {
		t.Fatalf("persistVisitLog with no records returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, visitlog.FileName)); !os.IsNotExist(err) {
		t.Fatalf("expected no visit log file to be created, stat err: %v", err)
	}
}

// TestRunListVisitReportReadsExistingLogWithoutScraping exercises the
// `list --visit-report` CLI path end-to-end against a real config.yaml and a
// pre-seeded visit log: it must print the aggregate report and return
// without ever constructing a scraper (i.e. no browser/login attempted) -
// confirmed here by the command succeeding entirely offline in a unit test.
func TestRunListVisitReportReadsExistingLogWithoutScraping(t *testing.T) {
	dir := t.TempDir()
	downloadPath := filepath.Join(dir, "downloads")
	if err := os.MkdirAll(downloadPath, 0o755); err != nil {
		t.Fatalf("MkdirAll downloadPath: %v", err)
	}

	logPath := filepath.Join(downloadPath, visitlog.FileName)
	records := []visitlog.Record{
		{Course: "Analysis", SectionTitle: "Forum", SectionURL: "https://opal/forum", FilesFound: 0, Timestamp: time.Now()},
		{Course: "Analysis", SectionTitle: "Forum", SectionURL: "https://opal/forum", FilesFound: 0, Timestamp: time.Now()},
	}
	if err := visitlog.Append(logPath, records); err != nil {
		t.Fatalf("seeding visit log: %v", err)
	}

	configPath := filepath.Join(dir, "config.yaml")
	configYAML := "download_path: " + downloadPath + "\ncourses:\n  - \"*\"\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("writing config.yaml: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runList([]string{"--config", configPath, "--visit-report"}); err != nil {
			t.Errorf("runList --visit-report returned error: %v", err)
		}
	})

	if !strings.Contains(out, "Forum") {
		t.Fatalf("expected report to mention the seeded Forum section, got:\n%s", out)
	}
	if !strings.Contains(out, "empty on all 2 visit(s)") {
		t.Fatalf("expected report to flag Forum as always-empty, got:\n%s", out)
	}
}
