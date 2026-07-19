package smokecheck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/scraper"
)

func TestEvaluateDiscoveryEstablishesBaselineOnFirstRun(t *testing.T) {
	pass, msg, next := EvaluateDiscovery(map[string]int{"Course A": 10}, Baseline{}, false, DefaultDropThresholdPercent)
	if !pass {
		t.Fatalf("expected first run (no baseline) to pass, got fail: %s", msg)
	}
	if next.TotalFiles != 10 {
		t.Fatalf("expected new baseline TotalFiles=10, got %d", next.TotalFiles)
	}
	if !strings.Contains(msg, "no baseline found") {
		t.Fatalf("expected message to mention establishing a baseline, got: %s", msg)
	}
}

func TestEvaluateDiscoveryFailsOnZeroFiles(t *testing.T) {
	baseline := Baseline{TotalFiles: 50, UpdatedAt: time.Now()}
	pass, msg, next := EvaluateDiscovery(map[string]int{}, baseline, true, DefaultDropThresholdPercent)
	if pass {
		t.Fatalf("expected zero files to fail regardless of threshold, got pass: %s", msg)
	}
	if next.TotalFiles != 50 {
		t.Fatalf("expected baseline to stay at 50 (never ratchet to 0), got %d", next.TotalFiles)
	}
}

func TestEvaluateDiscoveryFailsOnLargeDrop(t *testing.T) {
	baseline := Baseline{TotalFiles: 100, UpdatedAt: time.Now()}
	// 30% drop, exceeds default 20% threshold.
	pass, msg, next := EvaluateDiscovery(map[string]int{"Course A": 70}, baseline, true, DefaultDropThresholdPercent)
	if pass {
		t.Fatalf("expected a 30%% drop to fail the default 20%% threshold, got pass: %s", msg)
	}
	if next.TotalFiles != 100 {
		t.Fatalf("expected baseline to stay at 100 on failure, got %d", next.TotalFiles)
	}
	if !strings.Contains(msg, "30.0%") {
		t.Fatalf("expected message to report the drop percentage, got: %s", msg)
	}
}

func TestEvaluateDiscoveryPassesWithinThresholdButDoesNotLowerBaseline(t *testing.T) {
	baseline := Baseline{TotalFiles: 100, UpdatedAt: time.Now()}
	// 10% drop, within the default 20% threshold.
	pass, _, next := EvaluateDiscovery(map[string]int{"Course A": 90}, baseline, true, DefaultDropThresholdPercent)
	if !pass {
		t.Fatalf("expected a 10%% drop to pass within the default 20%% threshold")
	}
	if next.TotalFiles != 100 {
		t.Fatalf("expected baseline to stay at the high-water mark of 100 (not ratchet down to 90), got %d", next.TotalFiles)
	}
}

func TestEvaluateDiscoveryRatchetsUpOnGrowth(t *testing.T) {
	baseline := Baseline{TotalFiles: 100, UpdatedAt: time.Now()}
	pass, _, next := EvaluateDiscovery(map[string]int{"Course A": 120}, baseline, true, DefaultDropThresholdPercent)
	if !pass {
		t.Fatalf("expected growth to pass")
	}
	if next.TotalFiles != 120 {
		t.Fatalf("expected baseline to ratchet up to 120, got %d", next.TotalFiles)
	}
}

// fakeDiscoverer is a test double for Discoverer, mirroring the fakeDownloader
// pattern in internal/syncer/syncer_test.go.
type fakeDiscoverer struct {
	files                []scraper.RemoteFile
	usedInteractiveLogin bool
	scrapeErr            error
}

func (f *fakeDiscoverer) ScrapeWithSavedSession(ctx context.Context, courseFilter []string) ([]scraper.RemoteFile, error) {
	if f.scrapeErr != nil {
		return nil, f.scrapeErr
	}
	return f.files, nil
}

func (f *fakeDiscoverer) UsedInteractiveLogin() bool {
	return f.usedInteractiveLogin
}

func TestRunPropagatesScrapeError(t *testing.T) {
	fake := &fakeDiscoverer{scrapeErr: errors.New("boom")}
	_, err := Run(context.Background(), fake, Options{BaselinePath: filepath.Join(t.TempDir(), "baseline.json")})
	if err == nil {
		t.Fatal("expected Run to propagate the scrape error")
	}
}

func TestRunEstablishesAndReusesBaseline(t *testing.T) {
	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	fake := &fakeDiscoverer{files: []scraper.RemoteFile{
		{Course: "A", Name: "1.pdf"},
		{Course: "A", Name: "2.pdf"},
		{Course: "B", Name: "3.pdf"},
	}}

	result, err := Run(context.Background(), fake, Options{BaselinePath: baselinePath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pass {
		t.Fatalf("expected first run to pass (establishing baseline), got: %+v", result)
	}
	if result.TotalFiles != 3 {
		t.Fatalf("expected TotalFiles=3, got %d", result.TotalFiles)
	}
	if _, err := os.Stat(baselinePath); err != nil {
		t.Fatalf("expected baseline file to be written: %v", err)
	}

	// Second run with far fewer files should fail against the just-saved
	// baseline of 3.
	fake.files = []scraper.RemoteFile{{Course: "A", Name: "1.pdf"}}
	result2, err := Run(context.Background(), fake, Options{BaselinePath: baselinePath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.Pass {
		t.Fatalf("expected second run (1 of 3 files) to fail, got: %+v", result2)
	}
}

func TestRunReportsInteractiveLogin(t *testing.T) {
	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	fake := &fakeDiscoverer{
		files:                []scraper.RemoteFile{{Course: "A", Name: "1.pdf"}},
		usedInteractiveLogin: true,
	}
	result, err := Run(context.Background(), fake, Options{BaselinePath: baselinePath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.UsedInteractiveLogin {
		t.Fatalf("expected UsedInteractiveLogin to be true")
	}
	if !result.Pass {
		t.Fatalf("expected an interactive relogin to still pass overall (not itself a failure): %+v", result)
	}
	if !strings.Contains(result.LoginMessage, "relogin") {
		t.Fatalf("expected LoginMessage to mention the relogin, got: %s", result.LoginMessage)
	}
}

func TestRunResetBaselineIgnoresExisting(t *testing.T) {
	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	if err := SaveBaseline(baselinePath, Baseline{TotalFiles: 1000, UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("seeding baseline: %v", err)
	}

	fake := &fakeDiscoverer{files: []scraper.RemoteFile{{Course: "A", Name: "1.pdf"}}}
	result, err := Run(context.Background(), fake, Options{BaselinePath: baselinePath, ResetBaseline: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pass {
		t.Fatalf("expected --reset-baseline run to pass despite a huge old baseline, got: %+v", result)
	}
}

func TestBaselineRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "baseline.json")
	want := Baseline{TotalFiles: 42, Courses: map[string]int{"A": 42}, UpdatedAt: time.Now().UTC().Truncate(time.Second)}
	if err := SaveBaseline(path, want); err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}
	got, ok := LoadBaseline(path)
	if !ok {
		t.Fatalf("expected LoadBaseline to succeed")
	}
	if got.TotalFiles != want.TotalFiles {
		t.Fatalf("TotalFiles mismatch: got %d want %d", got.TotalFiles, want.TotalFiles)
	}
}

func TestLoadBaselineMissingFileDegradesSilently(t *testing.T) {
	_, ok := LoadBaseline(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if ok {
		t.Fatalf("expected LoadBaseline to report false for a missing file")
	}
}
