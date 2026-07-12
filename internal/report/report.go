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

// IssueURL builds a prefilled "new GitHub issue" link with body as the
// issue body (URL-encoded). The caller (GUI) opens this in the user's
// default browser; the user still reviews the prefilled text on GitHub's
// own page and clicks "Submit new issue" themselves - opal-downloader never
// submits anything on the user's behalf.
func IssueURL(title, body string) string {
	q := url.Values{}
	if title != "" {
		q.Set("title", title)
	}
	q.Set("body", body)
	return IssuesNewURL + "?" + q.Encode()
}
