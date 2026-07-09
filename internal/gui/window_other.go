//go:build !windows

package gui

// hasNativeWindow is false on non-Windows platforms: the WebView2 binding
// used for the native window (github.com/jchv/go-webview2) is Windows-only,
// and Windows is this project's primary target platform (see CLAUDE.md).
// Non-Windows builds fall back to the previous behavior of printing the
// local server URL for the user to open manually, so `go build ./...` /
// `go vet ./...` / `go test ./...` keep working on other platforms (notably
// this repo's CI, which runs on ubuntu-latest) without needing a native
// webview implementation for them.
const hasNativeWindow = false

// openNativeWindow is never called when hasNativeWindow is false; it exists
// only so both build variants expose the same symbol.
func openNativeWindow(url string) error {
	return nil
}
