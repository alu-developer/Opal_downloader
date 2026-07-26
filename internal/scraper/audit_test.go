package scraper

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/logging"
)

// captureConsole installs a logging sink for the duration of fn and returns
// whatever reached the console.
//
// It replaced an os.Stdout swap when auditLog moved onto internal/logging.
// The swap had stopped working and could not have kept working: the logging
// package resolves its console writer when the logger is built, so reassigning
// the os.Stdout variable afterwards changes nothing. The test failed loudly,
// which is the good version of that discovery.
//
// verbose is passed because an audit line is a diagnostic - it only reaches a
// console at all when someone asked for diagnostics, which is exactly what
// --debug-clicks does on the command line (see cmd/opal-downloader's
// hasVerboseFlag).
func captureConsole(t *testing.T, verbose bool, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	closeFn, err := logging.Setup(logging.Options{Console: &buf, Verbose: verbose})
	if err != nil {
		t.Fatalf("logging.Setup: %v", err)
	}
	t.Cleanup(func() {
		closeFn()
		// Put the package back to its default so a later test in this package
		// is not left writing into this test's dead buffer.
		restore, _ := logging.Setup(logging.Options{Console: os.Stdout})
		t.Cleanup(restore)
	})

	fn()
	return buf.String()
}

func TestAuditLogSilentByDefault(t *testing.T) {
	s := &OpalScraper{}
	out := captureConsole(t, true, func() {
		s.auditLog("click", nil, "a[href*='x']", "test reason")
	})
	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected no output when debugClicks is off (default), got: %q", out)
	}
}

func TestAuditLogEmitsWhenDebugClicksEnabled(t *testing.T) {
	s := &OpalScraper{}
	s.SetDebugClicks(true)
	out := captureConsole(t, true, func() {
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

func TestAuditLogNoFileWrittenWithoutEnableDebugLogFile(t *testing.T) {
	s := &OpalScraper{}
	s.SetDebugClicks(true)
	captureConsole(t, true, func() {
		s.auditLog("click", nil, "a[href*='x']", "test reason")
	})
	if s.debugLogFile != nil {
		t.Fatalf("expected no debug log file to be opened without an explicit EnableDebugLogFile call")
	}
}

func TestEnableDebugLogFileWritesJSONLLines(t *testing.T) {
	s := &OpalScraper{}
	s.SetDebugClicks(true)
	dir := t.TempDir()

	path, err := s.EnableDebugLogFile(dir)
	if err != nil {
		t.Fatalf("EnableDebugLogFile: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("expected log file under %s, got %s", dir, path)
	}

	captureConsole(t, true, func() {
		s.auditLog("click", nil, "a[href*='x']", "first reason")
		s.auditLog("section-content-poll", nil, "", "poll #1: 3 candidates, err=nil")
	})

	if err := s.CloseDebugLogFile(); err != nil {
		t.Fatalf("CloseDebugLogFile: %v", err)
	}
	if s.debugLogFile != nil {
		t.Fatalf("expected debugLogFile to be nil after CloseDebugLogFile")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading debug log file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d: %q", len(lines), string(data))
	}

	var first debugLogLine
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshaling first line: %v", err)
	}
	if first.Kind != "click" || first.Selector != "a[href*='x']" || first.Reason != "first reason" || first.Page != "?" {
		t.Fatalf("unexpected first line contents: %+v", first)
	}
	if first.Time == "" {
		t.Fatalf("expected a non-empty timestamp")
	}

	var second debugLogLine
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("unmarshaling second line: %v", err)
	}
	if second.Kind != "section-content-poll" || second.Reason != "poll #1: 3 candidates, err=nil" {
		t.Fatalf("unexpected second line contents: %+v", second)
	}
}

func TestCloseDebugLogFileIsNoOpWhenNoneOpen(t *testing.T) {
	s := &OpalScraper{}
	if err := s.CloseDebugLogFile(); err != nil {
		t.Fatalf("expected no error closing a debug log file that was never opened, got: %v", err)
	}
}
