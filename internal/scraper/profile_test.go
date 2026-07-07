package scraper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsUserDataDirLocked_NoUserDataDir(t *testing.T) {
	locked, err := isUserDataDirLocked("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if locked {
		t.Fatal("expected empty userDataDir to never be reported as locked")
	}
}

func TestIsUserDataDirLocked_MissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	locked, err := isUserDataDirLocked(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if locked {
		t.Fatal("a nonexistent userDataDir should not be reported as locked")
	}
}

func TestIsUserDataDirLocked_NoLockFilePresent(t *testing.T) {
	dir := t.TempDir()
	locked, err := isUserDataDirLocked(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if locked {
		t.Fatal("a fresh profile dir with no lock markers should not be reported as locked")
	}
}

func TestIsUserDataDirLocked_StaleLockFileIsNotLocked(t *testing.T) {
	dir := t.TempDir()
	// A lock marker that exists but isn't held open by any process (e.g. left
	// behind after a crash) must not be treated as "in use", since we can
	// still open it exclusively ourselves.
	if err := os.WriteFile(filepath.Join(dir, "lockfile"), []byte(""), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	locked, err := isUserDataDirLocked(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if locked {
		t.Fatal("a stale, unheld lockfile should not be reported as locked")
	}
}

func TestIsSharingViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"used by another process", errWithMessage("open x: The process cannot access the file because it is being used by another process."), true},
		{"sharing violation", errWithMessage("sharing violation"), true},
		{"unrelated error", errWithMessage("file not found"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSharingViolation(tt.err); got != tt.want {
				t.Fatalf("isSharingViolation(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

func errWithMessage(msg string) error {
	return simpleError(msg)
}
