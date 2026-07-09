//go:build windows

package gui

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"

	webview2 "github.com/jchv/go-webview2"
)

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
