//go:build windows

package gui

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

//go:embed assets/icon.ico
var appIconICO []byte

// hasNativeWindow is true on Windows: opal-downloader ships a native
// WebView2-backed window on this platform (a dedicated app window, not the
// user's everyday browser tab), per maintainer decision.
const hasNativeWindow = true

// openNativeWindow shows the GUI in a native WebView2 window pointed at the
// local HTTP server's url, and blocks until the user closes that window (or
// an interrupt signal arrives). It returns nil once the window has closed.
//
// jchv/go-webview2 was chosen over github.com/webview/webview_go because it
// is a pure-Go binding (no cgo, no C/C++ toolchain needed to build) - it
// loads an embedded copy of WebView2Loader.dll via go-winloader at runtime
// instead of linking against it at compile time. That matters here because
// this repo's dev/build machines are not guaranteed to have a cgo-capable
// C compiler (gcc/clang) installed, only MSVC's cl.exe, which cgo does not
// support directly. The one runtime dependency this still has is the
// Microsoft Edge WebView2 Runtime itself, which ships preinstalled on
// Windows 10 (2004+) and Windows 11, and is otherwise a small evergreen
// redistributable - see "Platform-specific build considerations" in the PR
// description for what this implies for the installer.
func openNativeWindow(url string) error {
	// WebView2's window and message loop are thread-affine: they must be
	// created and pumped from the same OS thread for the lifetime of the
	// window. Run() is called synchronously from gui.Run(), which in turn
	// is called directly from main() with no other goroutines started
	// first, so this is safe to lock for the remainder of the process.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Opal Downloader",
			Width:  1024,
			Height: 760,
			Center: true,
		},
	})
	if w == nil {
		return fmt.Errorf("failed to open the native GUI window - this usually means the Microsoft Edge WebView2 Runtime is not installed; see https://developer.microsoft.com/microsoft-edge/webview2/")
	}
	defer w.Destroy()

	setWindowIcon(w.Window())

	// Let Ctrl-C in the terminal (if any) close the window too, so the HTTP
	// server shutdown path in gui.Run() still runs the same way it did
	// before this window existed.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go func() {
		if _, ok := <-sigCh; ok {
			w.Terminate()
		}
	}()

	w.Navigate(url)
	w.Run()
	return nil
}

// browseForFolder opens a native Windows folder-picker dialog and returns
// the chosen absolute path, or ("", nil) if the user cancelled.
//
// Browsers cannot return a real filesystem path from
// <input type=file webkitdirectory> (sandboxed - it returns a fake path
// only), so this shells out to a small PowerShell snippet that drives
// System.Windows.Forms.FolderBrowserDialog instead. That only works because
// this GUI's HTTP server and the browser tab showing it always run on the
// same machine (a local desktop tool, not a hosted web app) - see
// handleBrowseFolder in settings.go for the HTTP handler that calls this.
// -STA is required because WinForms dialogs need a single-threaded
// apartment; PowerShell's default apartment state does not guarantee that.
// powerShellUTF8Prelude must lead any PowerShell script whose stdout this
// program reads.
//
// [Console]::OutputEncoding must be set before anything is written, and it
// is not cosmetic: PowerShell encodes its stdout in the console's OEM code
// page, which on a German Windows is 850, and Go reads those bytes as
// UTF-8. A folder called "Übung" comes back as the single byte 0x9A, which
// is not valid UTF-8 and turns into U+FFFD downstream - so the user picks a
// real folder and the tool stores a path that points nowhere, silently.
//
// Measured on the maintainer's machine (2026-07-27), which has OEMCP 850:
// without this line "C:\x\Übung" arrives as ...,92,154,98,117,110,103;
// with it, as ...,92,195,156,98,117,110,103. Their live config.yaml carries
// exactly that damage in a subfolder_destinations path.
//
// A const rather than an inlined line so the test can run the same prelude
// under a forced legacy code page; see window_windows_test.go.
const powerShellUTF8Prelude = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n"

func browseForFolder() (string, error) {
	// See powerShellUTF8Prelude above for why the script has to start with it:
	// a picked folder containing "Ü" arrives corrupted otherwise.
	const script = powerShellUTF8Prelude + `Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
	Write-Output $dialog.SelectedPath
}`

	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-Command", script)
	hideWindow(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("opening folder picker: %w (%s)", err, strings.TrimSpace(out.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

var (
	moduser32            = windows.NewLazySystemDLL("user32.dll")
	procLoadImageW       = moduser32.NewProc("LoadImageW")
	procSendMessageW     = moduser32.NewProc("SendMessageW")
	procGetSystemMetrics = moduser32.NewProc("GetSystemMetrics")
)

const (
	imageIcon      = 1
	lrLoadFromFile = 0x00000010
	wmSetIcon      = 0x0080
	iconSmall      = 0
	iconBig        = 1
	smCxIcon       = 11
	smCyIcon       = 12
	smCxSmIcon     = 49
	smCySmIcon     = 50
)

// setWindowIcon overrides the window's default icon (go-webview2's own
// fallback - see CreateWithOptions in the webview2 module, which loads
// Windows' generic IDI_APPLICATION when WindowOptions.IconId is unset, as it
// always is here) with opal-downloader's own mark, via WM_SETICON. That
// covers every place a live HICON is shown - the title bar, the taskbar,
// Alt-Tab - because all of them read the window's icon, not the window
// class's.
//
// It does not touch the .exe file's own icon as seen in Explorer before the
// program runs; that is a build-time-embedded resource (a .syso, which needs
// a resource-compiling tool) and is a separate decision - see
// docs/BACKLOG.md.
//
// LoadImageW only reads from a file (or a module's own embedded resources,
// which this program has none of), not from a byte slice, so the embedded
// appIconICO is written to a temp file first. The file is safe to remove as
// soon as LoadImageW returns: Windows copies the icon's pixel data into the
// returned HICON at load time and keeps no reference to the source file
// afterwards.
func setWindowIcon(hwnd unsafe.Pointer) {
	h := uintptr(hwnd)
	if h == 0 || len(appIconICO) == 0 {
		return
	}

	tmp, err := os.CreateTemp("", "opal-downloader-icon-*.ico")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(appIconICO); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}

	pathPtr, err := windows.UTF16PtrFromString(tmpPath)
	if err != nil {
		return
	}

	cxBig, _, _ := procGetSystemMetrics.Call(smCxIcon)
	cyBig, _, _ := procGetSystemMetrics.Call(smCyIcon)
	if hIconBig, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, cxBig, cyBig, lrLoadFromFile); hIconBig != 0 {
		_, _, _ = procSendMessageW.Call(h, wmSetIcon, iconBig, hIconBig)
	}

	cxSmall, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	cySmall, _, _ := procGetSystemMetrics.Call(smCySmIcon)
	if hIconSmall, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, cxSmall, cySmall, lrLoadFromFile); hIconSmall != 0 {
		_, _, _ = procSendMessageW.Call(h, wmSetIcon, iconSmall, hIconSmall)
	}
}
