package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setup installs a logger writing to a buffer and a temp file, and returns
// both the console text and a reader for the file.
func setup(t *testing.T, verbose bool) (console *bytes.Buffer, logPath string) {
	t.Helper()

	console = &bytes.Buffer{}
	logPath = filepath.Join(t.TempDir(), "logs", "opal.log")

	closeFn, err := Setup(Options{Console: console, FilePath: logPath, Verbose: verbose})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(closeFn)
	return console, logPath
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	return string(data)
}

// The central rule: a diagnostic is recorded but not shown. This is the whole
// reason the package exists - "Warning: skipping section ..." belongs in the
// file, not in front of a student who wants their slides.
func TestDiagnosticsGoToTheFileAndNotTheConsole(t *testing.T) {
	console, logPath := setup(t, false)

	User("Opening OPAL at %s", "https://example.invalid/opal/")
	Detail("visited section %q in %dms", "Woche 07", 812)
	Warn("skipping section %q: navigation failed after retry", "Woche 07")

	out := console.String()
	if !strings.Contains(out, "Opening OPAL at") {
		t.Errorf("a user-facing line did not reach the console, got %q", out)
	}
	if strings.Contains(out, "Woche 07") {
		t.Errorf("a diagnostic reached the console, which is the noise this package exists to stop: %q", out)
	}

	logged := readFile(t, logPath)
	for _, want := range []string{"Opening OPAL at", "visited section", "skipping section"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log file is missing %q; it should hold everything.\ngot:\n%s", want, logged)
		}
	}
}

// An error is the exception: the user hears about it however it was tagged,
// because an error nobody mentions is how a broken tool looks like a working
// one.
func TestErrorsAlwaysReachTheConsole(t *testing.T) {
	console, _ := setup(t, false)

	Error("could not download %s: %v", "woche01.pdf", os.ErrPermission)

	out := console.String()
	if !strings.Contains(out, "woche01.pdf") {
		t.Errorf("an error did not reach the console: %q", out)
	}
	if !strings.Contains(out, "Error: ") {
		t.Errorf("an error was not marked as one on the console: %q", out)
	}
}

// Verbose is the "turn it up" switch the tool did not have - dev mode meant
// "visible browser", never "more logging".
func TestVerboseShowsDiagnostics(t *testing.T) {
	console, _ := setup(t, true)

	Detail("visited section %q", "Woche 07")

	if out := console.String(); !strings.Contains(out, "Woche 07") {
		t.Errorf("verbose mode did not show a diagnostic: %q", out)
	}
}

// The file is what someone attaches to a bug report, so it has to be safe to
// attach without anyone remembering to check it.
func TestTheLogFileIsScrubbed(t *testing.T) {
	console, logPath := setup(t, true)

	const token = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEF"
	Detail("request failed, Cookie: shibsession=%s", token)

	if logged := readFile(t, logPath); strings.Contains(logged, token) {
		t.Errorf("a credential-shaped value was written to the log file verbatim:\n%s", logged)
	}
	// The console is the user's own screen in their own session, and it is
	// not what gets attached to anything - so this only pins the file.
	_ = console
}

// A console line should look like the tool has always looked. A CLI that
// suddenly emits `time=... level=INFO msg=...` is a downgrade for the person
// reading it.
func TestConsoleLinesAreJustTheMessage(t *testing.T) {
	console, _ := setup(t, false)

	User("Found %d course links", 6)

	out := strings.TrimSpace(console.String())
	if out != "Found 6 course links" {
		t.Errorf("console output carries machinery the user did not ask for: %q", out)
	}
}

// Failing to open the log must not fail the run the user actually asked for.
func TestAnUnusableLogPathStillLogsToTheConsole(t *testing.T) {
	console := &bytes.Buffer{}

	// A path whose parent is a regular file cannot be created as a directory.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	closeFn, err := Setup(Options{Console: console, FilePath: filepath.Join(blocker, "logs", "opal.log")})
	t.Cleanup(closeFn)
	if err == nil {
		t.Fatal("expected Setup to report that the log file could not be opened")
	}

	User("Found %d course links", 6)
	if out := console.String(); !strings.Contains(out, "Found 6 course links") {
		t.Errorf("console logging stopped working because the file sink failed: %q", out)
	}
}

// Without Setup - any test binary, any caller that never installs a logger -
// output still goes somewhere sensible rather than panicking or vanishing.
func TestTheZeroConfigurationLoggerWorks(t *testing.T) {
	console := &bytes.Buffer{}
	closeFn, err := Setup(Options{Console: console})
	if err != nil {
		t.Fatalf("Setup with no file: %v", err)
	}
	t.Cleanup(closeFn)

	User("hello")
	Detail("not shown")

	out := console.String()
	if !strings.Contains(out, "hello") {
		t.Errorf("console-only setup dropped a user line: %q", out)
	}
	if strings.Contains(out, "not shown") {
		t.Errorf("console-only setup showed a diagnostic: %q", out)
	}
}
