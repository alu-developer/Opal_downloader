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
<title>Opal Downloader - Feedback</title>
<style>` + pageStyle + `
	textarea, input[type=text] { width: 100%; font: inherit; box-sizing: border-box; padding: 0.5rem; border-radius: 6px; border: 1px solid #ccc; }
	textarea.diagnostics { background: #f7f7f7; color: #444; }
	label { display: block; font-weight: 600; margin: 1rem 0 0.3rem; }
</style>
</head>
<body>
	` + bannerChrome + `
	<h1>Feedback / Problem melden</h1>

	<p class="hint">
		This opens a prefilled GitHub issue in your browser - nothing is sent
		automatically. You can edit the text below (and on GitHub itself)
		before submitting, and you'll need a GitHub account to submit it.
	</p>

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
}

func (s *server) handleFeedbackPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	data := feedbackPageData{Title: "Feedback", Diagnostics: report.Diagnostics(s.buildVersion)}
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

	<label>Report text that was sent to GitHub as the issue body</label>
	<textarea rows="14" readonly>{{.Body}}</textarea>

	<p class="back"><a href="/">&larr; Back</a></p>
</body>
</html>
`))

type feedbackOpenedData struct {
	IssueURL  string
	Body      string
	OpenError string
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

	issueURL := report.IssueURL(title, body)

	data := feedbackOpenedData{IssueURL: issueURL, Body: body}
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
