package gui

import (
	"html/template"
	"net/http"
	"strings"
	"sync"

	"github.com/alu-developer/opal-downloader/internal/report"
)

// feedbackState holds the most recent panic recovered from a GUI request
// handler (see withRecover), so the resulting error page's "report this
// crash" link can carry it into the feedback page without stuffing a
// potentially large stack trace into a URL query string. Only the single
// most recent crash is kept - this is a short-lived local single-user tool
// (see internal/gui's job.go for the same reasoning applied to jobs), not a
// crash-history log.
type feedbackState struct {
	mu     sync.Mutex
	report string // report.CrashReport output, or "" if no crash recorded yet
}

func (f *feedbackState) setCrash(rep string) {
	f.mu.Lock()
	f.report = rep
	f.mu.Unlock()
}

func (f *feedbackState) crash() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.report
}

var feedbackPageTemplate = template.Must(template.New("feedback").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
` + faviconLink + `
<title>Opal Downloader - Feedback</title>
<style>` + pageStyle + `
	textarea, input[type=text] { width: 100%; font: inherit; box-sizing: border-box; padding: 0.5rem; border-radius: 6px; border: 1px solid #ccc; }
	textarea.diagnostics { background: #f7f7f7; color: #444; }
	textarea#log { font-family: ui-monospace, Consolas, monospace; font-size: 0.8rem; white-space: pre; }
	label { display: block; font-weight: 600; margin: 1rem 0 0.3rem; }
	details.more { margin-top: 1rem; }
	details.more > summary { cursor: pointer; font-weight: 600; }
</style>
</head>
<body>
	` + bannerChrome + `
	<h1>Feedback / Problem melden</h1>

	{{/* Two long hint paragraphs stood here until 2026-08-12. The first
	     explained the button ("this opens a prefilled GitHub issue, nothing
	     is sent automatically, you can edit it, you need an account"); the
	     second told the user to go download the log and attach it by hand.
	     Both went on the maintainer's standing complaint about a paragraph
	     explaining every button - and the second one is simply obsolete now
	     that the log rides along in the form below by itself. What survives
	     of the first is the one fact a button label cannot carry: that this
	     does not send anything. */}}
	<p class="hint">Nothing is sent automatically &ndash; this opens a
		prefilled issue on GitHub for you to review and submit.</p>

	{{if .CrashDetected}}
	<div class="status warn">A crash was detected. Its details are included below.</div>
	{{end}}

	<form method="post" action="/feedback/open">
		<label for="title">Issue title</label>
		<input type="text" id="title" name="title" value="{{.Title}}">

		<label for="description">What happened?</label>
		<textarea id="description" name="description" rows="6" placeholder="Describe the problem, what you expected, and what happened instead...">{{.Description}}</textarea>

		{{if .CrashBlock}}
		<label for="crash">Crash details (included automatically)</label>
		<textarea id="crash" class="diagnostics" rows="10" readonly>{{.CrashBlock}}</textarea>
		<input type="hidden" name="crash" value="1">
		{{end}}

		<label for="diagnostics">Diagnostics (included automatically)</label>
		<textarea id="diagnostics" class="diagnostics" rows="4" readonly>{{.Diagnostics}}</textarea>

		{{if .LogTail}}
		{{/* Editable, not readonly like the blocks above it, and that is the
		     whole privacy design. The log is already stripped of credentials
		     and session tokens - but it names courses and files, and this
		     ends up in a public issue tracker, which the narrow diagnostics
		     block never did. Rather than spend a checkbox and a paragraph
		     explaining the trade-off, the text is simply put in the user's
		     hands: select, delete, submit. Collapsed by default so it is not
		     a wall of log above the button.

		     Nothing is trusted from here. handleFeedbackOpen takes whatever
		     comes back in this field as the log section, which is correct -
		     an edit must survive - but it fits the result to
		     report.IssueURLBudget regardless of how much text arrives. */}}
		<details class="more">
			<summary>Recent log (included automatically) &ndash; edit or clear it here</summary>
			<textarea id="log" name="log" class="diagnostics" rows="12">{{.LogTail}}</textarea>
		</details>
		{{end}}

		<p style="margin-top: 1.5rem;"><button type="submit">Open GitHub issue</button></p>
	</form>

	<p class="back"><a href="/">&larr; Back</a></p>
</body>
</html>
`))

type feedbackPageData struct {
	Title         string
	Description   string
	CrashDetected bool
	CrashBlock    string
	Diagnostics   string
	LogTail       string
}

func (s *server) handleFeedbackPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	data := feedbackPageData{
		Title:       "Feedback",
		Diagnostics: report.Diagnostics(s.buildVersion),
		LogTail:     readReportLogTail(),
	}
	if r.URL.Query().Get("crash") == "1" {
		if crash := s.feedback.crash(); crash != "" {
			data.CrashDetected = true
			data.CrashBlock = crash
			data.Title = "Crash report"
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = feedbackPageTemplate.Execute(w, data)
}

var feedbackOpenedTemplate = template.Must(template.New("feedback-opened").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
` + faviconLink + `
<title>Opal Downloader - Feedback</title>
<style>` + pageStyle + `
	textarea { width: 100%; font: inherit; box-sizing: border-box; padding: 0.5rem; border-radius: 6px; border: 1px solid #ccc; background: #f7f7f7; color: #444; }
</style>
</head>
<body>
	` + bannerChrome + `
	<h1>Feedback</h1>

	{{if .OpenError}}
	<div class="status warn">
		Could not open your browser automatically ({{.OpenError}}). Open this
		link yourself: <a href="{{.IssueURL}}" target="_blank" rel="noopener">{{.IssueURL}}</a>
	</div>
	{{else}}
	<div class="status ok">
		Opened a prefilled GitHub issue in your browser. Nothing has been
		submitted yet - review the text there and click "Submit new issue"
		when ready. If it didn't open, use this link:
		<a href="{{.IssueURL}}" target="_blank" rel="noopener">{{.IssueURL}}</a>
	</div>
	{{end}}

	{{/* Only shown when lines were actually cut. This is the one case where
	     "go download the log and attach it" is worth saying - the report
	     genuinely does not carry everything - so the sentence that used to
	     greet every visitor unconditionally now appears exactly when it is
	     true. A GitHub issue body travels in the URL, and an over-long one
	     is refused outright; see report.IssueURLBudget. */}}
	{{if .DroppedLogLines}}
	<div class="status">
		The report was too long for a prefilled link, so its oldest
		{{.DroppedLogLines}} log line{{if ne .DroppedLogLines 1}}s were{{else}} was{{end}}
		left out. If the rest matters,
		<a href="/logs/download">download the full log</a> and drag it into
		the issue on GitHub.
	</div>
	{{end}}

	<label>Report text that was sent to GitHub as the issue body</label>
	<textarea rows="14" readonly>{{.Body}}</textarea>

	<p class="back"><a href="/">&larr; Back</a></p>
</body>
</html>
`))

type feedbackOpenedData struct {
	IssueURL        string
	Body            string
	OpenError       string
	DroppedLogLines int
}

// handleFeedbackOpen builds the report body from the submitted description
// (plus the last recorded crash, if the form carried crash=1) and opens a
// prefilled GitHub "new issue" link in the user's default browser. The user
// still reviews and submits the issue themselves on GitHub - this only ever
// opens a browser tab, it never transmits anything on its own.
func (s *server) handleFeedbackOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}

	description := r.FormValue("description")
	var body string
	if r.FormValue("crash") == "1" {
		if crash := s.feedback.crash(); crash != "" {
			body = feedbackBodyWithCrash(description, crash)
		}
	}
	if body == "" {
		body = report.FeedbackReport(s.buildVersion, description)
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = "Feedback"
	}

	// The log comes from the form, not from a fresh read of the file: the
	// field is editable precisely so a user can cut lines out of it, and
	// re-reading here would put them straight back. A form that carries no
	// "log" field at all (there was no log to show, or an older cached page)
	// simply produces a report without the section.
	issueURL, body, droppedLogLines := report.FitIssueURL(title, body, r.FormValue("log"))

	data := feedbackOpenedData{IssueURL: issueURL, Body: body, DroppedLogLines: droppedLogLines}
	openFn := s.openBrowser
	if openFn == nil {
		openFn = openInDefaultBrowser
	}
	if err := openFn(issueURL); err != nil {
		data.OpenError = err.Error()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = feedbackOpenedTemplate.Execute(w, data)
}

// feedbackBodyWithCrash combines the user's free-text description with an
// already-built crash report block (report.CrashReport output, which
// already contains its own diagnostics/panic/stack sections) so the
// diagnostics block isn't duplicated.
func feedbackBodyWithCrash(description, crashBlock string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		description = "(no description provided)"
	}
	var b strings.Builder
	b.WriteString("### Description\n\n")
	b.WriteString(description)
	b.WriteString("\n\n")
	b.WriteString(crashBlock)
	return b.String()
}
