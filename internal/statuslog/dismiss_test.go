package statuslog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDismissRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), dismissFileName)
	ts := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	if got := ReadDismissed(path); !got.IsZero() {
		t.Fatalf("ReadDismissed on a missing file = %v, want the zero time", got)
	}
	if err := WriteDismissed(path, ts); err != nil {
		t.Fatalf("WriteDismissed: %v", err)
	}
	if got := ReadDismissed(path); !got.Equal(ts) {
		t.Fatalf("ReadDismissed = %v, want %v", got, ts)
	}
}

// Same "degrade rather than fail" contract Read has: a corrupt marker means
// the banner shows again, which is only as bad as the behaviour before this
// file existed - never a crash or an error page.
func TestReadDismissedToleratesGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), dismissFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadDismissed(path); !got.IsZero() {
		t.Fatalf("ReadDismissed on garbage = %v, want the zero time", got)
	}
}

func TestIsDismissedComparesTheInstantNotTheFormatting(t *testing.T) {
	utc := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	berlin := utc.In(time.FixedZone("CEST", 2*60*60))

	if !IsDismissed(utc, berlin) {
		t.Fatal("the same instant written in another zone must still count as dismissed")
	}
	if IsDismissed(time.Time{}, utc) {
		t.Fatal("no dismissal recorded must never count as dismissed")
	}
	if IsDismissed(utc, utc.Add(24*time.Hour)) {
		t.Fatal("a later run must not inherit an earlier run's dismissal")
	}
}
