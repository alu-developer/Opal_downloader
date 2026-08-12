package report

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestDiagnosticsHasNoSensitiveMarkers(t *testing.T) {
	d := Diagnostics("v1.2.3")
	for _, forbidden := range []string{"password", "session_state", "cookie", "C:\\Users", "/home/"} {
		if strings.Contains(strings.ToLower(d), strings.ToLower(forbidden)) {
			t.Errorf("Diagnostics() unexpectedly contains %q:\n%s", forbidden, d)
		}
	}
}

func TestVersionLineDevBuild(t *testing.T) {
	got := VersionLine("dev")
	if !strings.Contains(got, "dev build") {
		t.Errorf("VersionLine(dev) = %q, want mention of dev build", got)
	}
	got = VersionLine("")
	if !strings.Contains(got, "dev build") {
		t.Errorf("VersionLine(\"\") = %q, want mention of dev build", got)
	}
	got = VersionLine("v1.2.3")
	if got != "Version: v1.2.3" {
		t.Errorf("VersionLine(v1.2.3) = %q", got)
	}
}

func TestFeedbackReportIncludesDescription(t *testing.T) {
	report := FeedbackReport("v1.0.0", "Sync fails on course X")
	if !strings.Contains(report, "Sync fails on course X") {
		t.Errorf("FeedbackReport missing description:\n%s", report)
	}
	if !strings.Contains(report, "Version: v1.0.0") {
		t.Errorf("FeedbackReport missing version:\n%s", report)
	}
}

func TestFeedbackReportEmptyDescription(t *testing.T) {
	report := FeedbackReport("v1.0.0", "   ")
	if !strings.Contains(report, "no description provided") {
		t.Errorf("FeedbackReport should note missing description:\n%s", report)
	}
}

func TestCrashReportIncludesPanicAndStack(t *testing.T) {
	report := CrashReport("v1.0.0", "boom: nil pointer", []byte("goroutine 1 [running]:\nmain.main()\n"))
	if !strings.Contains(report, "boom: nil pointer") {
		t.Errorf("CrashReport missing panic value:\n%s", report)
	}
	if !strings.Contains(report, "goroutine 1 [running]") {
		t.Errorf("CrashReport missing stack trace:\n%s", report)
	}
}

func TestIssueURLEncodesBody(t *testing.T) {
	body := "line one\nline two & more"
	got := IssueURL("Feedback", body)
	if !strings.HasPrefix(got, IssuesNewURL+"?") {
		t.Fatalf("IssueURL = %q, want prefix %q", got, IssuesNewURL+"?")
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("IssueURL produced unparseable URL: %v", err)
	}
	if parsed.Query().Get("body") != body {
		t.Errorf("decoded body = %q, want %q", parsed.Query().Get("body"), body)
	}
	if parsed.Query().Get("title") != "Feedback" {
		t.Errorf("decoded title = %q, want %q", parsed.Query().Get("title"), "Feedback")
	}
}

// The log section is the one part of a report that can grow without bound,
// and an over-long prefilled URL is refused by GitHub outright ("414 URI Too
// Long") - the user lands on an error page and the report is lost. These pin
// the two halves of the defence: it stays under budget no matter what
// arrives, and what it sacrifices to get there is the oldest log, never the
// newest and never the user's own words.

func TestFitIssueURLStaysUnderBudgetWithAHugeLog(t *testing.T) {
	huge := strings.Repeat("time=2026-08-12T16:30:33.887+02:00 level=DEBUG msg=\"Processing: some course\"\n", 500)
	body := FeedbackReport("v1.2.3", "sync failed")

	issueURL, finalBody, dropped := FitIssueURL("Feedback", body, huge)

	if len(issueURL) > IssueURLBudget {
		t.Fatalf("URL is %d chars, over the %d budget", len(issueURL), IssueURLBudget)
	}
	if dropped == 0 {
		t.Fatalf("500 log lines cannot have fitted; dropped = 0")
	}
	// Under budget is worthless if it got there by throwing everything away.
	if !strings.Contains(finalBody, "### Recent log") {
		t.Errorf("the whole log was dropped when some of it should have fitted:\n%s", finalBody)
	}
	if !strings.Contains(finalBody, "sync failed") {
		t.Errorf("the user's description must never be what gets cut")
	}
}

func TestFitIssueURLKeepsTheNewestLogLines(t *testing.T) {
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, "time=2026-08-12T16:30:33.887+02:00 level=DEBUG msg=\"line "+strconv.Itoa(i)+"\"")
	}
	_, finalBody, _ := FitIssueURL("Feedback", FeedbackReport("v1.2.3", "broke"), strings.Join(lines, "\n"))

	// The last line describes whatever just happened; the first is ancient
	// history. Cutting from the wrong end would keep the URL legal and the
	// report useless, which no length check would ever catch.
	if !strings.Contains(finalBody, `msg="line 499"`) {
		t.Errorf("the newest line was dropped:\n%s", finalBody)
	}
	if strings.Contains(finalBody, `msg="line 0"`) {
		t.Errorf("the oldest line survived a cut, so lines were dropped from the wrong end")
	}
}

func TestFitIssueURLLeavesAShortReportAlone(t *testing.T) {
	issueURL, finalBody, dropped := FitIssueURL("Feedback", FeedbackReport("v1.2.3", "broke"), "one line\ntwo lines")
	if dropped != 0 {
		t.Errorf("a two-line log fits easily; dropped %d", dropped)
	}
	if !strings.Contains(finalBody, "one line") || !strings.Contains(finalBody, "two lines") {
		t.Errorf("both lines should be present:\n%s", finalBody)
	}
	if !strings.HasPrefix(issueURL, IssuesNewURL+"?") {
		t.Errorf("not a GitHub issue URL: %s", issueURL)
	}
}

func TestFitIssueURLWithNoLogProducesThePlainReport(t *testing.T) {
	plain := FeedbackReport("v1.2.3", "broke")
	_, finalBody, dropped := FitIssueURL("Feedback", plain, "   \n  ")
	if dropped != 0 {
		t.Errorf("there was no log to drop; dropped %d", dropped)
	}
	if finalBody != plain {
		t.Errorf("a whitespace-only log must not add a section:\n%s", finalBody)
	}
}

// A crash body plus a log is the largest report this tool can build, and it
// is also the one a user is most likely to file. The crash path must not be
// the one that produces a broken link.
func TestFitIssueURLHandlesACrashBodyPlusALog(t *testing.T) {
	crash := CrashReport("v1.2.3", "runtime error: index out of range",
		[]byte(strings.Repeat("goroutine 1 [running]:\nmain.main()\n\t/build/main.go:42 +0x1d\n", 60)))
	huge := strings.Repeat("time=2026-08-12T16:30:33.887+02:00 level=DEBUG msg=\"Processing: a course\"\n", 300)

	issueURL, _, _ := FitIssueURL("Crash report", crash, huge)
	if len(issueURL) > IssueURLBudget {
		t.Fatalf("crash URL is %d chars, over the %d budget", len(issueURL), IssueURLBudget)
	}
}

// The escape hatch: if the user's own text alone blows the budget, the report
// is handed over long rather than silently trimmed. Better a link GitHub may
// refuse - which the user can see and act on - than a report quietly missing
// the half they typed.
func TestFitIssueURLNeverTruncatesTheUsersOwnText(t *testing.T) {
	// TrimSpace'd here because FeedbackReport trims what it is given; the
	// point of the test is that nothing in the *middle* goes missing.
	essay := strings.TrimSpace(strings.Repeat("I typed a great deal about this problem. ", 400))
	_, finalBody, _ := FitIssueURL("Feedback", FeedbackReport("v1.2.3", essay), "a log line")
	if !strings.Contains(finalBody, essay) {
		t.Errorf("the user's description was truncated")
	}
}

func TestWithLogSectionFencesTheLog(t *testing.T) {
	got := WithLogSection("### Description\n\nbroke\n", "line one\nline two")
	// Unfenced, a log full of #, * and _ renders as mangled markdown in the
	// issue and becomes hard to read at exactly the moment it matters.
	if !strings.Contains(got, "```\nline one\nline two\n```") {
		t.Errorf("log is not inside a code fence:\n%s", got)
	}
}
