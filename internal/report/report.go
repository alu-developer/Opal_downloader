// Package report builds plain-text diagnostic/feedback reports and the
// prefilled GitHub "new issue" links used by both the GUI's Feedback page
// and the CLI's panic recovery output. It has no backend of its own - see
// CLAUDE.md's "Local-only tool" principle - a report is only ever shown to
// the user (GUI textarea, CLI stderr) or handed to the user's own browser
// as a prefilled github.com/.../issues/new URL that the user reviews and
// submits themselves. Nothing is ever transmitted by this package directly.
//
// Scrubbing rule (see CLAUDE.md "Credentials and session data never leave
// the machine unscrubbed"): everything built here is limited to build
// version, GOOS/GOARCH, Go runtime version, and - for crash reports - the
// panic value and stack trace. It never includes config values (OPAL
// username/password), session state file contents, cookies, or full
// filesystem paths under the user's home directory. Callers must not widen
// this by appending extra fields without checking that constraint.
//
// FeedbackReportWithLog is the one deliberate widening: it carries a tail of
// the diagnostic log, which names courses and files. That is already-written
// log text, so it has been through statuslog.SanitizeMessage and holds no
// credentials or session tokens - but it is *not* as narrow as the blocks
// above, and it goes into a public issue tracker. The GUI therefore shows it
// to the user in an editable field before it is ever built into a URL, and
// the user reviews it a second time on GitHub's own page. Do not call it
// with text that has not been through the sanitizer.
package report

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
)

// IssuesNewURL is the repo's "open a new issue" page. Reused by both the
// GUI (opened in the user's default browser) and printed as plain text by
// the CLI panic handler (no browser-opening from CLI, per task scope).
const IssuesNewURL = "https://github.com/alu-developer/Opal_downloader/issues/new"

// isDevBuildVersion reports whether v is the placeholder version used for
// unreleased/local builds ("dev", cmd/opal-downloader's default when built
// without -ldflags). Duplicated in internal/gui rather than shared to avoid
// a dependency for one one-line check; keep both in sync if this ever grows.
func isDevBuildVersion(v string) bool {
	return v == "" || v == "dev"
}

// VersionLine formats the build-version line used at the top of every
// report. A "dev"/empty version (unreleased/local build, see
// cmd/opal-downloader's buildVersion default) is called out explicitly
// rather than printed as a bare "dev", since that alone isn't useful to a
// maintainer triaging an issue.
func VersionLine(buildVersion string) string {
	if isDevBuildVersion(buildVersion) {
		return "Version: dev build (not a released version)"
	}
	return fmt.Sprintf("Version: %s", buildVersion)
}

// Diagnostics returns the environment block common to every report: build
// version, OS/architecture, and the Go runtime version the binary was
// built/run with. Deliberately narrow - see package doc comment for the
// scrubbing rule this must not violate.
func Diagnostics(buildVersion string) string {
	var b strings.Builder
	b.WriteString(VersionLine(buildVersion))
	b.WriteString("\n")
	fmt.Fprintf(&b, "OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "Go runtime: %s\n", runtime.Version())
	return b.String()
}

// FeedbackReport builds the full report body for a user-submitted feedback
// issue: the diagnostics block followed by the user's free-text
// description. description is used verbatim (the GUI shows it to the user
// in an editable textarea before this is ever built into a URL - see
// internal/gui's feedback page - so the user has already had a chance to
// remove anything sensitive they typed).
func FeedbackReport(buildVersion, description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		description = "(no description provided)"
	}
	var b strings.Builder
	b.WriteString("### Description\n\n")
	b.WriteString(description)
	b.WriteString("\n\n### Diagnostics\n\n")
	b.WriteString(Diagnostics(buildVersion))
	return b.String()
}

// CrashReport builds the full report body for a crash/panic: the
// diagnostics block, the panic value, and the stack trace. panicValue is
// whatever recover() returned; stack is typically debug.Stack(). The stack
// trace contains source file paths from the machine the binary was *built*
// on (compiled into the binary's debug info), not the user's local
// filesystem layout or any of their data - safe to include per the
// scrubbing rule.
func CrashReport(buildVersion string, panicValue any, stack []byte) string {
	var b strings.Builder
	b.WriteString("### Crash report (auto-generated)\n\n")
	b.WriteString(Diagnostics(buildVersion))
	b.WriteString("\n### Panic\n\n")
	fmt.Fprintf(&b, "%v\n", panicValue)
	b.WriteString("\n### Stack trace\n\n```\n")
	b.Write(stack)
	b.WriteString("```\n")
	return b.String()
}

// WithLogSection appends a "Recent log" section holding logTail to an
// already-built report body (FeedbackReport's or a crash body's). An empty
// or whitespace-only logTail returns body unchanged, so callers do not need
// to branch on whether a log exists.
//
// logTail must already have been through statuslog.SanitizeMessage - which
// it has, because everything internal/logging writes to the log file goes
// through it before it is written (see internal/gui's logs.go). This
// function does no scrubbing of its own and must not be handed raw text
// from anywhere else.
func WithLogSection(body, logTail string) string {
	logTail = strings.TrimSpace(logTail)
	if logTail == "" {
		return body
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n\n### Recent log\n\n```\n")
	b.WriteString(logTail)
	b.WriteString("\n```\n")
	return b.String()
}

// IssueURLBudget bounds the length of a prefilled issue URL.
//
// GitHub answers an over-long issue-prefill URL with "414 URI Too Long" -
// the user's browser opens on an error page instead of a report form, and
// the report is simply lost. The observed ceiling is around 8 KB of URL, so
// this sits well under it: the cost of being conservative is a few dropped
// log lines, the cost of being wrong is the whole feature.
//
// The budget covers the *encoded* URL, not the raw body. Log text encodes
// badly - every newline costs three characters, as does every colon in a
// timestamp - so raw body length is not a usable proxy and callers must
// measure the built URL. FitIssueURL does that.
const IssueURLBudget = 6500

// FitIssueURL builds a prefilled issue URL that stays inside
// IssueURLBudget, shortening the log if it has to. It returns the URL, the
// body that URL actually carries, and how many log lines were dropped.
//
// body is the report without the log; logTail is the optional log section's
// content. The log is what gets cut, and oldest line first: the most recent
// lines are the ones describing whatever the user is reporting. If the
// report does not fit even with the log gone entirely, it is returned over
// budget rather than mangled - what the user typed is theirs, and silently
// truncating their own words would be worse than a long URL.
func FitIssueURL(title, body, logTail string) (issueURL, finalBody string, droppedLines int) {
	lines := strings.Split(strings.TrimSpace(logTail), "\n")
	if strings.TrimSpace(logTail) == "" {
		lines = nil
	}

	// Drop whole lines from the front until it fits. Line-at-a-time rather
	// than a binary search on bytes: this is at most a few dozen lines, and
	// cutting mid-line would put a fragment at the top of the block that
	// reads like a corrupt entry.
	for i := 0; i <= len(lines); i++ {
		candidate := WithLogSection(body, strings.Join(lines[i:], "\n"))
		if u := IssueURL(title, candidate); len(u) <= IssueURLBudget {
			return u, candidate, i
		}
	}
	// Nothing left to cut: the body alone is over budget.
	return IssueURL(title, body), body, len(lines)
}

// IssueURL builds a prefilled "new GitHub issue" link with body as the
// issue body (URL-encoded). The caller (GUI) opens this in the user's
// default browser; the user still reviews the prefilled text on GitHub's
// own page and clicks "Submit new issue" themselves - opal-downloader never
// submits anything on the user's behalf.
//
// Callers passing a body that can grow without bound (anything carrying log
// text) should go through FitIssueURL instead - see IssueURLBudget for what
// happens when a prefill URL gets too long.
func IssueURL(title, body string) string {
	q := url.Values{}
	if title != "" {
		q.Set("title", title)
	}
	q.Set("body", body)
	return IssuesNewURL + "?" + q.Encode()
}
