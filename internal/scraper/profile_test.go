package scraper

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestIsUserDataDirLocked_OpenHandleIsLocked(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("exclusive-open detection relies on Windows file sharing semantics")
	}
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lockfile")
	if err := os.WriteFile(lockPath, []byte(""), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Simulate a running browser by holding the file open exclusively, the
	// same way Chromium's ProcessSingleton does on Windows.
	f, err := openExclusiveForTest(lockPath)
	if err != nil {
		t.Fatalf("setup: could not open lock file: %v", err)
	}
	defer f.Close()

	locked, err := isUserDataDirLocked(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !locked {
		t.Fatal("expected an exclusively held lockfile to be reported as locked")
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

func TestPrepareBrowserProfile_NoUserDataDirConfigured(t *testing.T) {
	s := &OpalScraper{}
	dir, err := s.prepareBrowserProfile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "" {
		t.Fatalf("expected empty result when browser_user_data_dir is unset, got %q", dir)
	}
}

func TestPrepareBrowserProfile_LockedSourceReturnsClearError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("exclusive-open detection relies on Windows file sharing semantics")
	}
	source := t.TempDir()
	lockPath := filepath.Join(source, "lockfile")
	if err := os.WriteFile(lockPath, []byte(""), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	f, err := openExclusiveForTest(lockPath)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer f.Close()

	s := &OpalScraper{browserUserDataDir: source, workingProfileDir: filepath.Join(t.TempDir(), "working")}
	_, err = s.prepareBrowserProfile()
	if err == nil {
		t.Fatal("expected an error when the source profile is locked")
	}
	if !errorIs(err, ErrProfileLocked) {
		t.Fatalf("expected error to wrap ErrProfileLocked, got: %v", err)
	}
}

func errorIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestPrepareBrowserProfile_CopiesOnlyNeededFiles(t *testing.T) {
	source := t.TempDir()
	// Root-level file that should be copied.
	if err := os.WriteFile(filepath.Join(source, "Local State"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Root-level noise that should NOT be copied.
	if err := os.WriteFile(filepath.Join(source, "CrashpadMetrics-active.pma"), []byte("noise"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	profileDir := filepath.Join(source, "Default")
	if err := os.MkdirAll(filepath.Join(profileDir, "Extensions", "someext"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Extensions", "someext", "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Preferences"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Cache-like noise that should NOT be copied.
	if err := os.MkdirAll(filepath.Join(profileDir, "Cache"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Cache", "big-blob"), []byte("lots of bytes"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "working-profile")
	s := &OpalScraper{
		browserUserDataDir: source,
		browserProfileDir:  "Default",
		workingProfileDir:  dest,
	}

	got, err := s.prepareBrowserProfile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dest {
		t.Fatalf("expected returned dir %q, got %q", dest, got)
	}

	mustExist := []string{
		filepath.Join(dest, "Local State"),
		filepath.Join(dest, "Default", "Preferences"),
		filepath.Join(dest, "Default", "Extensions", "someext", "manifest.json"),
	}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist after copy: %v", p, err)
		}
	}

	mustNotExist := []string{
		filepath.Join(dest, "CrashpadMetrics-active.pma"),
		filepath.Join(dest, "Default", "Cache"),
	}
	for _, p := range mustNotExist {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("expected %s to be excluded from the copy", p)
		}
	}
}

func TestPrepareBrowserProfile_ReusesExistingCopyWithoutRecopying(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "Local State"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	profileDir := filepath.Join(source, "Default")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Preferences"), []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "working-profile")
	s := &OpalScraper{
		browserUserDataDir: source,
		browserProfileDir:  "Default",
		workingProfileDir:  dest,
	}

	if _, err := s.prepareBrowserProfile(); err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}

	// Mutate the source after the first copy; a second call should NOT pick
	// up this change, because the copy is one-time-by-design (see the
	// documented trade-off in prepareBrowserProfile).
	if err := os.WriteFile(filepath.Join(profileDir, "Preferences"), []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	if _, err := s.prepareBrowserProfile(); err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "Default", "Preferences"))
	if err != nil {
		t.Fatalf("reading copied Preferences: %v", err)
	}
	if string(got) != `{"v":1}` {
		t.Fatalf("expected working copy to remain untouched by the second prepareBrowserProfile call, got %s", got)
	}
}
