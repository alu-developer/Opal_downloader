//go:build windows

package notify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
)

// showTimeout bounds how long shelling out to powershell.exe is allowed to
// take, so a hung/slow PowerShell process can never block runSync's
// deferred scheduled-run cleanup (cmd/opal-downloader/root.go) indefinitely.
const showTimeout = 10 * time.Second

// execCommandContext is a package-level indirection over
// exec.CommandContext (matching this codebase's existing test-override
// convention - see e.g. scheduleStatusFunc in internal/gui/schedule.go) so
// `go test` never shells out to a real powershell.exe.
var execCommandContext = exec.CommandContext

// show renders title/message into a ToastGeneric XML payload and shows it
// via Windows.UI.Notifications.ToastNotificationManager, invoked through a
// small inline PowerShell script passed via -EncodedCommand (a
// base64-encoded UTF-16LE script, the standard way to hand PowerShell an
// arbitrary script without fighting the outer process's argv-quoting rules
// - see buildToastScript/encodeUTF16LEBase64). No PowerShell module
// (BurntToast or otherwise) is required; see notify.go's package doc
// comment for the live investigation that confirmed this works without
// registering AppID via a Start Menu shortcut/AUMID.
func show(title, message string) error {
	script := buildToastScript(title, message)
	encoded := encodeUTF16LEBase64(script)

	ctx, cancel := context.WithTimeout(context.Background(), showTimeout)
	defer cancel()

	cmd := execCommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
	// HideWindow avoids a brief console-window flash - this can fire during
	// an otherwise-invisible headless scheduled run, so a flashing console
	// window would be a confusing surprise disproportionate to what it's
	// doing (matches scraper's own preference for a human-free unattended
	// run staying unattended-looking).
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("showing toast notification via powershell.exe: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// buildToastScript renders the PowerShell script that loads title/message
// into a ToastGeneric XML payload and shows it via
// Windows.UI.Notifications.ToastNotificationManager. title/message are
// XML-escaped (xmlEscape) before being embedded in the toast XML, and the
// resulting XML string is PowerShell-single-quote-escaped (psSingleQuote)
// before being embedded in the script - two independent escaping layers for
// two different syntaxes, so neither XML metacharacters nor PowerShell
// quote characters in message (e.g. a sanitized error string containing a
// stray "'" or "&") can break out of their respective literal.
func buildToastScript(title, message string) string {
	toastXML := fmt.Sprintf(
		`<toast><visual><binding template="ToastGeneric"><text>%s</text><text>%s</text></binding></visual></toast>`,
		xmlEscape(title), xmlEscape(message),
	)

	return fmt.Sprintf(`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType=WindowsRuntime] > $null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType=WindowsRuntime] > $null
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml(%s)
$toast = New-Object Windows.UI.Notifications.ToastNotification $xml
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier(%s).Show($toast)
`, psSingleQuote(toastXML), psSingleQuote(AppID))
}

// xmlEscape escapes s for use as XML text content (&, <, >, quotes).
func xmlEscape(s string) string {
	var buf bytes.Buffer
	// xml.EscapeText never returns an error for a bytes.Buffer destination.
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// psSingleQuote wraps s as a PowerShell single-quoted string literal,
// doubling any embedded single quotes (PowerShell's own escaping rule for
// single-quoted strings - unlike double-quoted strings, no other character
// needs escaping inside one).
func psSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// encodeUTF16LEBase64 encodes script as base64-encoded UTF-16LE text, the
// format powershell.exe's -EncodedCommand flag requires.
func encodeUTF16LEBase64(script string) string {
	encoded := utf16.Encode([]rune(script))
	buf := make([]byte, len(encoded)*2)
	for i, v := range encoded {
		binary.LittleEndian.PutUint16(buf[i*2:], v)
	}
	return base64.StdEncoding.EncodeToString(buf)
}
