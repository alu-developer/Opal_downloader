package gui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// The folder picker itself cannot be tested: it opens a modal dialog and waits
// for a human. What can be tested is the part that was actually broken - what
// PowerShell's stdout looks like to a Go process when the picked path contains
// a non-ASCII character.
//
// The legacy console is modelled inside the script rather than by driving chcp
// from outside, for two reasons. A Go test inherits whatever console the runner
// has, so on a machine already at 65001 the bug does not reproduce at all -
// which is why it went unnoticed here until the registry's real OEMCP (850) was
// read. And forcing it from outside means going through cmd, which mangles a
// multi-line script before PowerShell ever sees it.
//
// Setting code page 850 first and then applying the real prelude is a faithful
// model of the actual situation: the console the GUI's child process gets is
// legacy, and the prelude has to override it.
const legacyConsoleSetup = "[Console]::OutputEncoding = [System.Text.Encoding]::GetEncoding(850)\n"

// writeAndRunPowerShell runs script via a file rather than -Command, so
// newlines and quoting cannot be corrupted on the way in.
func writeAndRunPowerShell(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.ps1")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	out, err := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path).Output()
	if err != nil {
		t.Fatalf("running powershell: %v (%s)", err, out)
	}
	return strings.TrimRight(string(out), "\r\n")
}

func TestPowerShellOutputSurvivesALegacyConsoleCodePage(t *testing.T) {
	// [char]0xDC is "Ü" - built from its code point so this test file's own
	// encoding cannot be the thing under test.
	const body = `Write-Output ('C:\x\' + [char]0xDC + 'bung')`

	got := writeAndRunPowerShell(t, legacyConsoleSetup+powerShellUTF8Prelude+body)
	if want := "C:\\x\\Übung"; got != want {
		t.Errorf("path came back as %q (bytes %v), want %q", got, []byte(got), want)
	}
	if !utf8.ValidString(got) {
		t.Errorf("path is not valid UTF-8: %v", []byte(got))
	}
}

func TestWithoutThePreludeTheSamePathIsCorrupted(t *testing.T) {
	// The other half, and the reason the prelude is not decoration: this is the
	// exact damage found in the maintainer's live config.yaml - a lone 0x9A,
	// which is not valid UTF-8 and becomes U+FFFD downstream, leaving a stored
	// path that points at no real folder.
	const body = `Write-Output ('C:\x\' + [char]0xDC + 'bung')`

	got := writeAndRunPowerShell(t, legacyConsoleSetup+body)
	if utf8.ValidString(got) {
		t.Fatalf("expected corrupted output without the prelude, got valid UTF-8 %q", got)
	}
	if !containsByte(got, 0x9A) {
		t.Errorf("output %v does not contain the expected 0x9A byte", []byte(got))
	}
}

func TestFolderPickerScriptSetsItsOutputEncodingFirst(t *testing.T) {
	// Ordering matters and is invisible at a glance: [Console]::OutputEncoding
	// only affects what has not been written yet.
	if !strings.HasPrefix(powerShellUTF8Prelude, "[Console]::OutputEncoding") {
		t.Errorf("the prelude does not start by setting the output encoding: %q", powerShellUTF8Prelude)
	}
	if !strings.HasSuffix(powerShellUTF8Prelude, "\n") {
		t.Errorf("the prelude does not end in a newline, so it would run into the next statement")
	}
}

func containsByte(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}

// TestAppIconICOIsAValidMultiSizeIcon guards the one link between
// scripts/build-icon.ps1's output and the running program: go:embed just
// copies bytes, so nothing else here would notice if internal/gui/assets/
// icon.ico were replaced with an unrelated or truncated file.
func TestAppIconICOIsAValidMultiSizeIcon(t *testing.T) {
	if len(appIconICO) == 0 {
		t.Fatal("appIconICO is empty - internal/gui/assets/icon.ico did not embed")
	}
	// ICONDIR header: reserved=0, type=1 (icon), count>=1.
	if len(appIconICO) < 6 || appIconICO[0] != 0 || appIconICO[1] != 0 || appIconICO[2] != 1 || appIconICO[3] != 0 {
		t.Fatalf("appIconICO does not start with an ICONDIR header (icon type 1): %v", appIconICO[:min(6, len(appIconICO))])
	}
	count := int(appIconICO[4]) | int(appIconICO[5])<<8
	if count < 6 {
		t.Errorf("appIconICO reports %d frames, want at least the 6 build-icon.ps1 renders", count)
	}
}

// TestSetWindowIconDoesNotPanicOnAZeroHandle documents and locks in the
// guard clause setWindowIcon needs precisely because it is called from
// openNativeWindow with no way to test the real window-creation path
// unattended (WebView2 pops a real, visible window). A nil/zero HWND is the
// one input this function can be exercised with outside of a live window.
func TestSetWindowIconDoesNotPanicOnAZeroHandle(t *testing.T) {
	setWindowIcon(nil)
}
