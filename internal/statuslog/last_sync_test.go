package statuslog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLastSyncRoundTrip is the basic contract: what a sync writes is what
// the landing page reads back.
func TestLastSyncRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-sync.json")
	ts := time.Now().Truncate(time.Millisecond)

	want := Status{
		Timestamp:       ts,
		Outcome:         OutcomeSuccess,
		Message:         "Synced successfully: 2 downloaded, 40 skipped.",
		FilesDownloaded: 2,
		FilesSkipped:    40,
	}
	if err := WriteLastSync(path, want); err != nil {
		t.Fatalf("WriteLastSync: %v", err)
	}

	got, ok := ReadLastSync(path)
	if !ok {
		t.Fatalf("ReadLastSync reported nothing after a successful write")
	}
	if !got.Timestamp.Equal(want.Timestamp) {
		t.Fatalf("timestamp = %v, want %v", got.Timestamp, want.Timestamp)
	}
	if got.Outcome != want.Outcome || got.FilesDownloaded != want.FilesDownloaded {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, want)
	}
}

// TestLastSyncDegradesSilently pins the contract the landing page depends
// on: a missing or corrupt record is "say nothing", never an error.
func TestLastSyncDegradesSilently(t *testing.T) {
	dir := t.TempDir()

	if _, ok := ReadLastSync(filepath.Join(dir, "does-not-exist.json")); ok {
		t.Fatalf("a missing last-sync file reported a usable status")
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seeding the corrupt file: %v", err)
	}
	if _, ok := ReadLastSync(corrupt); ok {
		t.Fatalf("a corrupt last-sync file reported a usable status")
	}
}

// TestLastSyncIsSeparateFromTheScheduledRecord is the whole reason this is
// a second file. The GUI's failure banner reads last-scheduled-run.json and
// is scheduled-only by design; a manual sync recording itself must not land
// there, or the banner starts announcing failures the user just watched
// happen live in front of them.
func TestLastSyncIsSeparateFromTheScheduledRecord(t *testing.T) {
	scheduledPath, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	lastSyncPath, err := DefaultLastSyncPath()
	if err != nil {
		t.Fatalf("DefaultLastSyncPath: %v", err)
	}

	if scheduledPath == lastSyncPath {
		t.Fatalf("the last-sync record and the scheduled-run record share a path (%s); the GUI banner would start reporting manual runs", scheduledPath)
	}
	if filepath.Dir(scheduledPath) != filepath.Dir(lastSyncPath) {
		t.Fatalf("expected both records under the same directory, got %s and %s", scheduledPath, lastSyncPath)
	}
}

// TestWriteLastSyncSanitizes confirms the last-sync record goes through the
// same sanitization boundary as everything else this package writes - it is
// written by more call sites than the scheduled record (CLI and GUI both),
// so the guarantee has to hold here too.
func TestWriteLastSyncSanitizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-sync.json")

	err := WriteLastSync(path, Status{
		Timestamp: time.Now(),
		Outcome:   OutcomeFailure,
		Message:   `failed: {"cookies":[{"name":"JSESSIONID","value":"secret"}]}`,
	})
	if err != nil {
		t.Fatalf("WriteLastSync: %v", err)
	}

	got, ok := ReadLastSync(path)
	if !ok {
		t.Fatalf("ReadLastSync reported nothing after a successful write")
	}
	if got.Message != redactedSessionStateMessage {
		t.Fatalf("session-shaped message reached disk: %q", got.Message)
	}
}
