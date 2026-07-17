//go:build windows

package notify

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"os/exec"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestXMLEscape(t *testing.T) {
	got := xmlEscape(`<b>Tom & "Jerry"</b>`)
	want := "&lt;b&gt;Tom &amp; &#34;Jerry&#34;&lt;/b&gt;"
	if got != want {
		t.Fatalf("xmlEscape: got %q, want %q", got, want)
	}
}

func TestPSSingleQuote(t *testing.T) {
	got := psSingleQuote(`it's a 'test'`)
	want := `'it''s a ''test'''`
	if got != want {
		t.Fatalf("psSingleQuote: got %q, want %q", got, want)
	}
}

func TestEncodeUTF16LEBase64RoundTrips(t *testing.T) {
	script := "Write-Output 'hällo — wörld'"
	encoded := encodeUTF16LEBase64(script)

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decoding base64: %v", err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("decoded byte length %d is not even (not valid UTF-16)", len(raw))
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	got := string(utf16.Decode(units))
	if got != script {
		t.Fatalf("round trip: got %q, want %q", got, script)
	}
}

// TestBuildToastScriptEscapesUntrustedContent guards against a
// sanitized-but-still-attacker-influenced message (e.g. a course/file name
// containing quotes, XML metacharacters, or PowerShell syntax) breaking out
// of either the XML text-content literal or the PowerShell single-quoted
// string literal it's embedded in.
func TestBuildToastScriptEscapesUntrustedContent(t *testing.T) {
	title := "Opal Downloader: scheduled sync failed"
	message := `sync failed for course "Analysis & Algebra" (it's broken) <script>bad</script>`

	script := buildToastScript(title, message)

	if strings.Contains(script, "<script>bad</script>") {
		t.Fatalf("expected message's raw XML-unsafe substring to be escaped, got script:\n%s", script)
	}
	if !strings.Contains(script, "&amp;") {
		t.Fatalf("expected ampersand to be XML-escaped, got script:\n%s", script)
	}
	// The message's single quote is XML-escaped to "&#39;" by xmlEscape
	// before it's ever embedded in the PowerShell single-quoted string
	// literal, so there is no bare "'" character left in the XML payload
	// that would need PowerShell's own quote-doubling.
	if !strings.Contains(script, "&#39;s broken") {
		t.Fatalf("expected embedded single quote to be XML-escaped, got script:\n%s", script)
	}
	if !strings.Contains(script, AppID) {
		t.Fatalf("expected script to reference AppID %q, got:\n%s", AppID, script)
	}
}

func TestScheduledSyncFailureSanitizesMessage(t *testing.T) {
	orig := execCommandContext
	var gotArgs []string
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		gotArgs = arg
		return exec.CommandContext(ctx, "cmd", "/c", "exit", "0")
	}
	t.Cleanup(func() { execCommandContext = orig })

	// A message shaped like it might contain session/cookie data must never
	// reach the toast, even if a caller forgot to sanitize it first - same
	// "last line of defense" contract as statuslog.Write.
	if err := ScheduledSyncFailure(`session dump: {"cookies": [{"name": "JSESSIONID", "value": "abc"}]}`); err != nil {
		t.Fatalf("ScheduledSyncFailure returned error: %v", err)
	}

	if len(gotArgs) == 0 {
		t.Fatal("expected execCommandContext to be called with arguments")
	}
	encoded := gotArgs[len(gotArgs)-1]
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decoding encoded command: %v", err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	decodedScript := string(utf16.Decode(units))

	if strings.Contains(decodedScript, "JSESSIONID") || strings.Contains(decodedScript, "cookies") {
		t.Fatalf("expected session-state-shaped message to be redacted before reaching the toast script, got:\n%s", decodedScript)
	}
}

func TestShowPropagatesCommandFailure(t *testing.T) {
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "cmd", "/c", "exit", "1")
	}
	t.Cleanup(func() { execCommandContext = orig })

	if err := show("title", "message"); err == nil {
		t.Fatal("expected an error when the underlying command fails, got nil")
	}
}
