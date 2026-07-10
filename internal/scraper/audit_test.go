package scraper

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// whatever was written to it. auditLog (see audit.go) writes via fmt.Printf,
// so this is the simplest way to assert on its output without threading a
// io.Writer through OpalScraper just for this diagnostic flag.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = original }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("reading pipe: %v", err)
	}
	return buf.String()
}

func TestAuditLogSilentByDefault(t *testing.T) {
	s := &OpalScraper{}
	out := captureStdout(t, func() {
		s.auditLog("click", nil, "a[href*='x']", "test reason")
	})
	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected no output when debugClicks is off (default), got: %q", out)
	}
}

func TestAuditLogEmitsWhenDebugClicksEnabled(t *testing.T) {
	s := &OpalScraper{}
	s.SetDebugClicks(true)
	out := captureStdout(t, func() {
		s.auditLog("click", nil, "a[href*='x']", "test reason")
	})
	if !strings.Contains(out, "[audit]") {
		t.Fatalf("expected audit line to be printed, got: %q", out)
	}
	if !strings.Contains(out, `kind=click`) {
		t.Fatalf("expected audit line to include kind, got: %q", out)
	}
	if !strings.Contains(out, `selector="a[href*='x']"`) {
		t.Fatalf("expected audit line to include selector, got: %q", out)
	}
	if !strings.Contains(out, `reason="test reason"`) {
		t.Fatalf("expected audit line to include reason, got: %q", out)
	}
	if !strings.Contains(out, "page=?") {
		t.Fatalf("expected audit line to fall back to '?' for a nil page, got: %q", out)
	}
}

func TestSetDebugClicksDoesNotChangeDeveloperMode(t *testing.T) {
	s := &OpalScraper{}
	s.SetDebugClicks(true)
	if s.developerMode {
		t.Fatalf("SetDebugClicks must not implicitly toggle developerMode")
	}
}
