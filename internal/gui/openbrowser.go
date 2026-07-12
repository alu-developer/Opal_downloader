package gui

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openInDefaultBrowser opens target (a GitHub issue URL, in practice) in the
// user's normal default browser - deliberately NOT the WebView2 app window
// or the second Playwright-controlled automation window (see disclaimerHTML
// in gui.go): the user needs their own logged-in GitHub session and the
// ability to review/edit the prefilled issue before submitting it, neither
// of which the app's own windows provide. Uses only stdlib os/exec (no new
// dependency, per task scope).
func openInDefaultBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// "start" is a cmd.exe builtin, not an executable - must be invoked
		// via cmd /c. The empty "" argument is the (required) window title
		// placeholder so a URL containing "&" isn't misparsed as extra start
		// arguments.
		cmd = exec.Command("cmd", "/c", "start", "", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening browser: %w", err)
	}
	return nil
}
