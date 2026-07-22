package syncer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// setup for a single already-downloaded file that OPAL reports no size or
// modified date for - the state 13 of 14 files in one real course are in.
func signallessFixture(t *testing.T, localContents string) (cfg config.App, manifest *Manifest, remote []scraper.RemoteFile, localPath string) {
	t.Helper()
	dir := t.TempDir()
	cfg = config.App{DownloadPath: dir, DownloadConcurrency: 1}

	localPath = filepath.Join(dir, "Course A", "U09.pdf")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, []byte(localContents), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest = &Manifest{
		Path:  filepath.Join(dir, ManifestFileName),
		Files: map[string]FileRecord{"Course A/U09.pdf": {}},
	}
	remote = []scraper.RemoteFile{
		{Name: "U09.pdf", Course: "Course A", Path: "U09.pdf", URL: "https://example.test/U09.pdf"},
	}
	return cfg, manifest, remote, localPath
}

// Identical bytes: the local file must be left completely untouched (the
// download folder is inside OneDrive, so a pointless rewrite would trigger a
// cloud re-upload every sync) and it counts as skipped.
func TestProcessRemoteFilesVerifiesSignallessFilesAndLeavesIdenticalOnesAlone(t *testing.T) {
	cfg, manifest, remote, localPath := signallessFixture(t, "same-bytes")

	before, err := os.Stat(localPath)
	if err != nil {
		t.Fatal(err)
	}

	fetched := 0
	downloadFn := func(fileURL, target string) error {
		fetched++
		return os.WriteFile(target, []byte("same-bytes"), 0o644)
	}

	stats := processRemoteFiles(context.Background(), remote, manifest, cfg, false, downloadFn, nil)

	if fetched != 1 {
		t.Fatalf("expected the signalless file to be re-fetched for verification, got %d fetches", fetched)
	}
	if stats.Skipped != 1 || stats.Downloaded != 0 {
		t.Fatalf("identical content must count as skipped, got %+v", stats)
	}

	after, err := os.Stat(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an unchanged file must not be rewritten - that would churn OneDrive on every sync")
	}

	// No scratch file may survive.
	if _, err := os.Stat(verificationTempPath(localPath)); !os.IsNotExist(err) {
		t.Fatalf("verification temp file was left behind: %v", err)
	}
}

// Different bytes: this is the bug the whole feature exists for - an
// upstream edit to a file OPAL reports no metadata for must actually land.
func TestProcessRemoteFilesVerifiesSignallessFilesAndReplacesChangedOnes(t *testing.T) {
	cfg, manifest, remote, localPath := signallessFixture(t, "old-content")

	downloadFn := func(fileURL, target string) error {
		return os.WriteFile(target, []byte("NEW-content-is-longer"), 0o644)
	}

	stats := processRemoteFiles(context.Background(), remote, manifest, cfg, false, downloadFn, nil)

	if stats.Downloaded != 1 || stats.Skipped != 0 {
		t.Fatalf("changed content must count as downloaded, got %+v", stats)
	}
	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW-content-is-longer" {
		t.Fatalf("local file was not updated, still %q", got)
	}
	if _, err := os.Stat(verificationTempPath(localPath)); !os.IsNotExist(err) {
		t.Fatal("verification temp file was left behind")
	}
}

// A file that DOES carry a signal must keep the cheap metadata fast path -
// verification must not quietly become "re-download everything".
func TestProcessRemoteFilesDoesNotVerifyFilesThatCarryASignal(t *testing.T) {
	cfg, manifest, remote, _ := signallessFixture(t, "same-bytes")
	size := int64(10)
	modified := "01.07.2026 um 09:06 Uhr"
	manifest.Files["Course A/U09.pdf"] = FileRecord{Size: &size, Modified: &modified}
	remote[0].Size = &size
	remote[0].Modified = &modified

	fetched := 0
	downloadFn := func(fileURL, target string) error {
		fetched++
		return nil
	}

	stats := processRemoteFiles(context.Background(), remote, manifest, cfg, false, downloadFn, nil)
	if fetched != 0 {
		t.Fatalf("a file with matching size/date must not be re-fetched, got %d fetches", fetched)
	}
	if stats.Skipped != 1 {
		t.Fatalf("expected 1 skip, got %+v", stats)
	}
}

// A brand-new file (no manifest entry) is a normal download, not a verify:
// there is nothing on disk to compare against.
func TestNeedsContentVerificationRequiresAPreviousEntry(t *testing.T) {
	signalless := scraper.RemoteFile{Name: "x.pdf"}
	if needsContentVerification(signalless, false) {
		t.Fatal("a file with no previous manifest entry must be a plain download")
	}
	if !needsContentVerification(signalless, true) {
		t.Fatal("a known file with no remote signal must be verified")
	}
}

func TestVerificationTempPathKeepsExtension(t *testing.T) {
	got := verificationTempPath(filepath.Join("dir", "U09.pdf"))
	if filepath.Ext(got) != ".pdf" {
		// scraper.DownloadFile validates payloads by target extension; losing
		// the suffix would silently skip that check.
		t.Fatalf("temp path must keep the .pdf extension, got %q", got)
	}
	if got == filepath.Join("dir", "U09.pdf") {
		t.Fatal("temp path must differ from the target")
	}
}
