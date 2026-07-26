package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotationStartsANewFileAndKeepsTheOldOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opal.log")
	r, err := openRotating(path)
	if err != nil {
		t.Fatalf("openRotating: %v", err)
	}
	defer r.Close()

	line := strings.Repeat("x", 4096) + "\n"
	for written := 0; written < maxLogBytes+len(line); written += len(line) {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	if _, err := os.Stat(backupName(path, 1)); err != nil {
		t.Fatalf("passing the size cap did not produce a rotation: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat current log: %v", err)
	}
	if info.Size() > maxLogBytes {
		t.Fatalf("the current log is still over the cap after rotating: %d bytes", info.Size())
	}
}

// The bound on disk usage is the reason rotation is size-based at all, so it
// is worth pinning that old files actually go away rather than accumulating.
func TestRotationKeepsABoundedNumberOfFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opal.log")
	r, err := openRotating(path)
	if err != nil {
		t.Fatalf("openRotating: %v", err)
	}
	defer r.Close()

	// Enough writing to roll over several times.
	line := strings.Repeat("y", 8192) + "\n"
	for i := 0; i < (maxLogBytes/len(line))*(keptRotations+3); i++ {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if got, want := len(entries), keptRotations+1; got > want {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("rotation kept %d files, want at most %d: %v", got, want, names)
	}
	if _, err := os.Stat(backupName(path, keptRotations+1)); err == nil {
		t.Fatalf("a rotation past the keep limit survived: %s", backupName(path, keptRotations+1))
	}
}

// Rotation shifts the numbered backups along, so .1 must always be the most
// recent. Getting this backwards would silently keep the oldest logs and
// discard the ones anyone would want.
func TestTheNewestBackupIsAlwaysNumberOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opal.log")
	r, err := openRotating(path)
	if err != nil {
		t.Fatalf("openRotating: %v", err)
	}
	defer r.Close()

	filler := strings.Repeat("z", 8192) + "\n"
	fillPastCap := func(marker string) {
		if _, err := r.Write([]byte(marker + "\n")); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		for written := 0; written < maxLogBytes; written += len(filler) {
			if _, err := r.Write([]byte(filler)); err != nil {
				t.Fatalf("write filler: %v", err)
			}
		}
	}

	fillPastCap("FIRST-GENERATION")
	fillPastCap("SECOND-GENERATION")

	first := readOrEmpty(t, backupName(path, 2))
	second := readOrEmpty(t, backupName(path, 1))
	if !strings.Contains(second, "SECOND-GENERATION") {
		t.Errorf(".1 is not the most recent rotation; it held %q", firstLine(second))
	}
	if !strings.Contains(first, "FIRST-GENERATION") {
		t.Errorf(".2 is not the older rotation; it held %q", firstLine(first))
	}
}

// An appended-to file has to start from its real size, or a log that is
// already over the cap would never rotate again.
func TestReopeningAnExistingFileKnowsItsSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opal.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("q", maxLogBytes)), 0o600); err != nil {
		t.Fatalf("seed an oversized log: %v", err)
	}

	r, err := openRotating(path)
	if err != nil {
		t.Fatalf("openRotating: %v", err)
	}
	defer r.Close()

	if _, err := r.Write([]byte("one more line\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(backupName(path, 1)); err != nil {
		t.Fatalf("an already-oversized log was not rotated on the next write: %v", err)
	}
}

func readOrEmpty(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("<missing: %v>", err)
	}
	return string(data)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
