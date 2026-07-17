//go:build !windows

package notify

import (
	"errors"
	"testing"
)

func TestScheduledSyncFailureUnsupportedOffWindows(t *testing.T) {
	if err := ScheduledSyncFailure("some message"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported off Windows, got %v", err)
	}
}
