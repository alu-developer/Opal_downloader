package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	// maxLogBytes is when the current file gets rolled aside. A full sync
	// writes on the order of a few hundred diagnostic lines, so this holds
	// many runs' worth - long enough that a problem reported days later is
	// still in the file.
	maxLogBytes = 2 << 20 // 2 MiB

	// keptRotations is how many older files survive. Three plus the current
	// one is a few weeks of ordinary use, and bounds the tool's footprint at
	// 8 MiB, which nobody will notice.
	keptRotations = 3
)

// rotatingFile is an io.WriteCloser that starts a new file once the current
// one passes maxLogBytes, keeping a bounded number of older ones.
//
// Hand-rolled rather than pulled in from a library: this project has three
// direct dependencies on purpose (see CLAUDE.md), and "rename the file when
// it gets big" does not justify a fourth. It is size-based rather than
// date-based because the thing being bounded is disk usage, and a tool that
// runs once a day would otherwise keep a file per day forever.
type rotatingFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func openRotating(path string) (*rotatingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}
	r := &rotatingFile{path: path}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	// Size is tracked rather than stat'ed per write, but it has to start from
	// the real size or an appended-to file would never rotate.
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("measuring log file: %w", err)
	}
	r.f, r.size = f, info.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return 0, os.ErrClosed
	}

	// Rotate before writing, not after: checking afterwards lets a single
	// large record push the file past the limit and leave it there until the
	// next write, which may not come for a day.
	if r.size+int64(len(p)) > maxLogBytes && r.size > 0 {
		if err := r.rotate(); err != nil {
			// A rotation that fails must not lose the line. Keep writing to
			// the current file; it grows past the cap, which is a far smaller
			// problem than a silently dropped diagnostic.
			_, _ = fmt.Fprintf(r.f, "log rotation failed, continuing in this file: %v\n", err)
		}
	}

	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate closes the current file, shifts the numbered backups along, and
// opens a fresh one. Called with the lock held.
func (r *rotatingFile) rotate() error {
	if err := r.f.Close(); err != nil {
		return err
	}
	r.f = nil

	// Oldest first, so nothing is overwritten before it has been shifted.
	_ = os.Remove(backupName(r.path, keptRotations))
	for i := keptRotations - 1; i >= 1; i-- {
		_ = os.Rename(backupName(r.path, i), backupName(r.path, i+1))
	}
	if err := os.Rename(r.path, backupName(r.path, 1)); err != nil {
		// Reopen regardless: without this the writer is left with no file at
		// all and every later write fails.
		if openErr := r.open(); openErr != nil {
			return openErr
		}
		return err
	}
	return r.open()
}

func backupName(path string, n int) string {
	return fmt.Sprintf("%s.%d", path, n)
}

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
