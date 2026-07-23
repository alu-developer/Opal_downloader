package statuslog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-scheduled-run.json")

	want := Status{
		Timestamp:       time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC),
		Outcome:         OutcomeSuccess,
		Message:         "Synced successfully: 12 downloaded, 3 skipped.",
		FilesDownloaded: 12,
		FilesSkipped:    3,
		FilesErrored:    0,
		LoginPath:       LoginPathHeadlessOnly,
	}
	if err := Write(path, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, ok := Read(path)
	if !ok {
		t.Fatalf("Read reported not-ok for a file it just wrote")
	}
	if !got.Timestamp.Equal(want.Timestamp) || got.Outcome != want.Outcome || got.Message != want.Message ||
		got.FilesDownloaded != want.FilesDownloaded || got.FilesSkipped != want.FilesSkipped ||
		got.FilesErrored != want.FilesErrored || got.LoginPath != want.LoginPath {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, want)
	}
}

func TestReadMissingFileDegradesSilently(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	status, ok := Read(path)
	if ok {
		t.Fatalf("expected ok=false for a missing file, got status=%+v", status)
	}
	if status != (Status{}) {
		t.Fatalf("expected zero-value Status for a missing file, got %+v", status)
	}
}

func TestReadCorruptFileDegradesSilently(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(path, []byte("{ not valid json at all"), 0o644); err != nil {
		t.Fatalf("writing corrupt fixture: %v", err)
	}

	status, ok := Read(path)
	if ok {
		t.Fatalf("expected ok=false for corrupt JSON, got status=%+v", status)
	}
	if status != (Status{}) {
		t.Fatalf("expected zero-value Status for corrupt JSON, got %+v", status)
	}
}

func TestReadEmptyOutcomeTreatedAsNothingToReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty-outcome.json")
	if err := os.WriteFile(path, []byte(`{"timestamp":"2026-07-17T06:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	_, ok := Read(path)
	if ok {
		t.Fatalf("expected ok=false when outcome is empty/missing")
	}
}

// TestWriteNeverIncludesSessionStateContent is the hard-requirement test
// from the queue task (add-scheduled-sync-failure-log-and-gui-banner):
// CLAUDE.md's "credentials and session data never leave the machine
// unscrubbed" principle means this status file - even though it never
// leaves the machine either - must not carry cookie/session-token/
// credential-shaped content, in case some deeper layer ever passed a raw,
// unwrapped error (containing real session_state_file-shaped content)
// through to the message field. It asserts this at the SanitizeMessage/
// Write boundary directly, rather than only asserting the feature "works".
func TestWriteNeverIncludesSessionStateContent(t *testing.T) {
	dir := t.TempDir()

	// A realistic session_state_file (Playwright's storage_state.json
	// shape: cookies with name/value/domain, plus localStorage origins) -
	// the exact kind of content that must never reach the status file.
	sessionStateContent := `{"cookies":[{"name":"JSESSIONID","value":"AbCdEf0123456789ZzYyXxWwVvUuTtSsRrQq","domain":"bildungsportal.sachsen.de","path":"/","expires":-1,"httpOnly":true,"secure":true,"sameSite":"Lax"}],"origins":[{"origin":"https://bildungsportal.sachsen.de","localStorage":[{"name":"shib_idp_session","value":"_1a2b3c4d5e6f7890abcdef1234567890fedcba"}]}]}`

	sessionStatePath := filepath.Join(dir, "session-state.json")
	if err := os.WriteFile(sessionStatePath, []byte(sessionStateContent), 0o644); err != nil {
		t.Fatalf("writing session-state fixture: %v", err)
	}

	// Simulate the worst case this test guards against: some deeper layer
	// passing a raw error that happens to embed the actual session-state
	// content (rather than one of this codebase's already-wrapped,
	// human-readable error messages) straight through to the status
	// writer, plus a plain Cookie header and a bearer token for good
	// measure.
	maliciousMessage := "sync failed unexpectedly: " + sessionStateContent +
		" | Cookie: JSESSIONID=AbCdEf0123456789ZzYyXxWwVvUuTtSsRrQq" +
		" | Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.abcdefghijklmno"

	statusPath := filepath.Join(dir, "last-scheduled-run.json")
	status := Status{
		Timestamp: time.Now(),
		Outcome:   OutcomeFailure,
		Message:   maliciousMessage,
		LoginPath: LoginPathInteractiveRelogin,
	}
	if err := Write(statusPath, status); err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("reading back status file: %v", err)
	}
	written := string(raw)

	forbidden := []string{
		"JSESSIONID",
		"AbCdEf0123456789ZzYyXxWwVvUuTtSsRrQq",
		"shib_idp_session",
		"_1a2b3c4d5e6f7890abcdef1234567890fedcba",
		"bildungsportal.sachsen.de",
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
	}
	for _, needle := range forbidden {
		if strings.Contains(written, needle) {
			t.Fatalf("written status file contains credential/session-shaped content %q; full contents:\n%s", needle, written)
		}
	}
}

func TestSanitizeMessageRedactsCookieHeader(t *testing.T) {
	got := SanitizeMessage("request failed, Cookie: session=abc123def456ghi789jkl012mno345pqr678")
	if strings.Contains(got, "abc123def456ghi789jkl012mno345pqr678") {
		t.Fatalf("expected cookie value to be redacted, got %q", got)
	}
}

func TestSanitizeMessageTruncatesLongMessages(t *testing.T) {
	long := strings.Repeat("a ", 1000)
	got := SanitizeMessage(long)
	if len(got) > maxMessageLen+len("... [truncated]")+1 {
		t.Fatalf("expected truncated message, got length %d", len(got))
	}
}

func TestSanitizeMessageEmptyFallsBackToPlaceholder(t *testing.T) {
	got := SanitizeMessage("   ")
	if got == "" {
		t.Fatalf("expected a non-empty placeholder for an empty message")
	}
}

func TestAppendHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scheduled-run-history.jsonl")

	for i := 0; i < 3; i++ {
		status := Status{
			Timestamp: time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour),
			Outcome:   OutcomeSuccess,
			Message:   "run",
		}
		if err := AppendHistory(path, status); err != nil {
			t.Fatalf("AppendHistory: %v", err)
		}
	}

	got, err := ReadHistory(path)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 history entries, got %d: %+v", len(got), got)
	}
	if !got[0].Timestamp.Before(got[2].Timestamp) {
		t.Fatalf("expected entries in append (oldest-first) order, got %+v", got)
	}
}

func TestAppendHistoryTrimsToMaxEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scheduled-run-history.jsonl")

	for i := 0; i < maxHistoryEntries+5; i++ {
		status := Status{
			Timestamp: time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour),
			Outcome:   OutcomeSuccess,
			Message:   fmt.Sprintf("run %d", i),
		}
		if err := AppendHistory(path, status); err != nil {
			t.Fatalf("AppendHistory: %v", err)
		}
	}

	got, err := ReadHistory(path)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(got) != maxHistoryEntries {
		t.Fatalf("expected history capped at %d entries, got %d", maxHistoryEntries, len(got))
	}
	if got[len(got)-1].Message != fmt.Sprintf("run %d", maxHistoryEntries+4) {
		t.Fatalf("expected the most recent run kept, got last entry %+v", got[len(got)-1])
	}
	if got[0].Message != "run 5" {
		t.Fatalf("expected the oldest 5 runs trimmed off, got first entry %+v", got[0])
	}
}

func TestReadHistoryMissingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.jsonl")

	got, err := ReadHistory(path)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no entries for a missing file, got %+v", got)
	}
}

func TestReadHistorySkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scheduled-run-history.jsonl")

	content := `{"timestamp":"2026-07-17T06:00:00Z","outcome":"success","message":"ok"}
not valid json at all
{"timestamp":"2026-07-17T07:00:00Z","outcome":"failure","message":"also ok"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	got, err := ReadHistory(path)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected the corrupt line to be skipped and both good ones kept, got %d: %+v", len(got), got)
	}
}

func TestAppendHistoryNeverIncludesSessionStateContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scheduled-run-history.jsonl")

	maliciousMessage := `sync failed: {"cookies":[{"name":"JSESSIONID","value":"AbCdEf0123456789ZzYyXxWwVvUuTtSsRrQq"}]} | Cookie: JSESSIONID=AbCdEf0123456789ZzYyXxWwVvUuTtSsRrQq`
	if err := AppendHistory(path, Status{Timestamp: time.Now(), Outcome: OutcomeFailure, Message: maliciousMessage}); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back history file: %v", err)
	}
	if strings.Contains(string(raw), "JSESSIONID") || strings.Contains(string(raw), "AbCdEf0123456789ZzYyXxWwVvUuTtSsRrQq") {
		t.Fatalf("history file contains credential/session-shaped content; full contents:\n%s", raw)
	}
}
