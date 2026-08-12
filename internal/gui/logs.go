package gui

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/logging"
)

// The diagnostic log had no way in from the GUI at all: internal/logging writes
// to ~/.opal-downloader/logs/, the CLI's --help names the path, and the GUI -
// which is how most people use this, and the case where the log matters most,
// since a windowed app's stdout goes nowhere - offered no link, no viewer and
// no mention that the file exists. A diagnostic nobody can find is close to no
// diagnostic.
//
// Showing the contents in the page is safe by construction rather than by
// judgement: everything written to that file has already been through
// statuslog.SanitizeMessage (see CLAUDE.md), which is the same reason the file
// is safe to attach to a bug report.

// logTailBytes is how much of the end of the log to read. The file rotates at
// 2 MiB (internal/logging/rotate.go), so it can be large enough that reading
// all of it into a page would be silly; the end is also the only part anyone
// wants, since a problem being diagnosed just happened.
const logTailBytes = 256 << 10

// logTailLines bounds what actually reaches the page after that.
const logTailLines = 400

// reportLogTailLines is the much smaller tail that goes into a bug report.
// A prefilled GitHub issue carries its body in the URL, and an over-long URL
// is rejected outright (see report.IssueURLBudget), so this cannot be the
// 400 lines the log *page* shows. Sized to overshoot the budget rather than
// undershoot it: report.FitIssueURL drops whatever does not fit, so asking
// for more lines here costs nothing, while asking for too few would throw
// away lines that would have fitted.
const reportLogTailLines = 60

// readReportLogTail returns the last reportLogTailLines lines of the log for
// inclusion in a feedback report, or "" if there is no readable log. A
// missing or unreadable log is not an error worth showing: the report is
// still worth filing without it, and the feedback page has no good place to
// put a complaint about a file the user never asked about.
func readReportLogTail() string {
	tail, _, err := readLogTail(logPathForPage())
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(tail, "\n"), "\n")
	if len(lines) > reportLogTailLines {
		lines = lines[len(lines)-reportLogTailLines:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// Both seams exist for the same reason the scheduler ones do (see the GUI
// tests): a test must not open a file-manager window on the maintainer's
// desktop, and must not depend on whatever happens to be in their real log.
var (
	logPathForPage = logging.DefaultLogPath
	revealLogFile  = openContainingFolder
)

type logsPageData struct {
	Path      string
	Exists    bool
	Tail      string
	Truncated bool
	ReadError string
	OpenError string
	Opened    bool
}

func handleLogsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	path := logPathForPage()
	data := logsPageData{Path: path}
	switch r.URL.Query().Get("opened") {
	case "1":
		data.Opened = true
	case "":
	default:
		data.OpenError = r.URL.Query().Get("opened")
	}

	tail, truncated, err := readLogTail(path)
	switch {
	case os.IsNotExist(err):
		// Not an error worth a red box: a fresh install genuinely has no log
		// until something runs.
		data.Exists = false
	case err != nil:
		data.Exists = true
		data.ReadError = err.Error()
	default:
		data.Exists = true
		data.Tail = tail
		data.Truncated = truncated
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = logsPageTemplate.Execute(w, data)
}

// handleLogsOpen reveals the log in the OS file manager, so the user can attach
// it to a bug report or open it in a real editor. Redirects back to the page
// rather than rendering, so a refresh does not re-open a window.
func handleLogsOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	target := "/logs?opened=1"
	if err := revealLogFile(logPathForPage()); err != nil {
		target = "/logs?opened=" + template.URLQueryEscaper(err.Error())
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// readLogTail returns the last logTailLines lines of at most the last
// logTailBytes of path, plus whether anything was cut off.
func readLogTail(path string) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return "", false, err
	}

	truncated := false
	size := info.Size()
	start := int64(0)
	if size > logTailBytes {
		start = size - logTailBytes
		truncated = true
	}
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && len(buf) > 0 {
		// io.EOF on an exact-length read is normal for ReadAt at end of file;
		// anything else is a real read failure.
		if !strings.Contains(err.Error(), "EOF") {
			return "", false, err
		}
	}

	text := string(buf)
	if truncated {
		// A byte offset lands mid-line; drop the partial first line rather than
		// showing a fragment that reads like a corrupt entry.
		if _, rest, found := strings.Cut(text, "\n"); found {
			text = rest
		}
	}

	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > logTailLines {
		lines = lines[len(lines)-logTailLines:]
		truncated = true
	}
	return strings.Join(lines, "\n"), truncated, nil
}

// openContainingFolder opens the folder holding path, selecting the file where
// the platform supports it. Deliberately opens the *folder* rather than the
// file: a .log has no reliable default application, and an "open with?" dialog
// is a worse outcome than a file manager window.
func openContainingFolder(path string) error {
	dir := filepath.Dir(path)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// explorer's /select takes the file and highlights it in its folder.
		// It exits non-zero even on success, so its exit code is not checked -
		// only whether the process could be started at all.
		cmd = exec.Command("explorer", "/select,"+filepath.Clean(path))
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("opening %s: %w", dir, err)
		}
		return nil
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening %s: %w", dir, err)
	}
	return nil
}

var logsPageTemplate = template.Must(template.New("logs").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
` + faviconLink + `
<title>Opal Downloader - Diagnostic log</title>
<style>` + pageStyle + `
	pre.log { background: #f7f7f7; color: #444; padding: 0.75rem; border-radius: 6px; border: 1px solid #ddd; overflow-x: auto; max-height: 32rem; overflow-y: auto; font-size: 0.85rem; line-height: 1.35; white-space: pre; }
	code.path { background: #f2f2f2; padding: 0.15rem 0.35rem; border-radius: 4px; word-break: break-all; }
</style>
</head>
<body>
	` + bannerChrome + `
	<h1>Diagnostic log</h1>

	<p class="hint">
		This is what the tool recorded about its own runs. It is already
		stripped of passwords, cookies and session tokens, so it is safe to
		attach to a bug report.
	</p>

	<p>File: <code class="path">{{.Path}}</code></p>

	<form method="post" action="/logs/open">
		<button type="submit">Show the file in my file manager</button>
	</form>

	{{if .Opened}}
	<div class="status ok">Opened the folder containing the log.</div>
	{{end}}
	{{if .OpenError}}
	<div class="status warn">Could not open the folder ({{.OpenError}}). The path above is the file.</div>
	{{end}}

	{{if not .Exists}}
	<div class="status">
		No log file yet. It is created the first time you run a sync, a login,
		or a course lookup.
	</div>
	{{else if .ReadError}}
	<div class="status warn">Could not read the log ({{.ReadError}}).</div>
	{{else}}
	{{if .Truncated}}
	<p class="hint">Showing the end of the log. Open the file itself for the rest.</p>
	{{end}}
	<pre class="log">{{.Tail}}</pre>
	{{end}}

	<p class="back"><a href="/">&larr; Back</a></p>
</body>
</html>
`))

// handleLogsDownload serves the log as a file download.
//
// The feedback page has always told people the log "is safe to attach" and
// then offered no way to attach it - it linked to /logs, which shows the file
// and can reveal it in a file manager, leaving the user to go find it. The
// maintainer put it plainly (2026-07-27): a pointer to attach something,
// without the means to. A download is the means.
//
// Serves the whole file rather than the tail shown on the page: a bug report
// wants everything, and the tail exists only so a page render stays sane.
// Safe for the same reason the page is - every line went through
// statuslog.SanitizeMessage before it was written.
func handleLogsDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	path := logPathForPage()
	f, err := os.Open(path)
	if err != nil {
		// A fresh install has no log yet. Say so in words rather than serving
		// an empty file the user would attach to a report believing it holds
		// something.
		http.Error(w, "There is no log file yet ("+path+"). It appears the first time opal-downloader runs.", http.StatusNotFound)
		return
	}
	defer func() { _ = f.Close() }()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="opal-downloader-log.txt"`)
	_, _ = io.Copy(w, f)
}
