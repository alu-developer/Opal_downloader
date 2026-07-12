package report

import (
	"net/url"
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
