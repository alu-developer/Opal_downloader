//go:build windows

package scheduler

import (
	"strings"
	"testing"
	"time"
)

func TestValidateTime(t *testing.T) {
	valid := []string{"00:00", "06:00", "23:59", "09:05"}
	for _, v := range valid {
		if err := ValidateTime(v); err != nil {
			t.Errorf("ValidateTime(%q) = %v, want nil", v, err)
		}
	}

	invalid := []string{"", "6:00", "24:00", "06:60", "06-00", "abc", "06:00:00", " 06:00"}
	for _, v := range invalid {
		if err := ValidateTime(v); err == nil {
			t.Errorf("ValidateTime(%q) = nil, want an error", v)
		}
	}
}

// TestBuildTaskXMLContainsExpectedSettings confirms buildTaskXML's output
// actually encodes every design decision from docs/scheduled-sync-plan.md
// section 4 that this task's acceptance criteria call out explicitly:
// StartWhenAvailable enabled, a daily (not logon) CalendarTrigger, and the
// configured exe path + "sync --scheduled" as the action.
func TestBuildTaskXMLContainsExpectedSettings(t *testing.T) {
	raw, err := buildTaskXML(`C:\fake\opal-downloader.exe`, "06:30")
	if err != nil {
		t.Fatalf("buildTaskXML: %v", err)
	}

	decoded, err := decodeMaybeUTF16(raw)
	if err != nil {
		t.Fatalf("decodeMaybeUTF16: %v", err)
	}
	xmlText := string(decoded)

	for _, want := range []string{
		"<StartWhenAvailable>true</StartWhenAvailable>",
		"<DaysInterval>1</DaysInterval>",
		"<LogonType>InteractiveToken</LogonType>",
		`<Command>C:\fake\opal-downloader.exe</Command>`,
		"<Arguments>sync --scheduled</Arguments>",
		`<WorkingDirectory>C:\fake</WorkingDirectory>`,
		"T06:30:00",
	} {
		if !strings.Contains(xmlText, want) {
			t.Errorf("expected task XML to contain %q, got:\n%s", want, xmlText)
		}
	}
}

func TestBuildTaskXMLRejectsInvalidTime(t *testing.T) {
	if _, err := buildTaskXML(`C:\fake\opal-downloader.exe`, "not-a-time"); err == nil {
		t.Fatal("expected buildTaskXML to reject an invalid time, got nil error")
	}
}

// TestParseStartTimeRoundTrip confirms parseStartTime can read back the
// exact time buildTaskXML encoded, via the same UTF-16-with-BOM encoding
// schtasks /Query /XML actually emits (see decodeMaybeUTF16's doc comment).
func TestParseStartTimeRoundTrip(t *testing.T) {
	raw, err := buildTaskXML(`C:\fake\opal-downloader.exe`, "14:45")
	if err != nil {
		t.Fatalf("buildTaskXML: %v", err)
	}

	got, err := parseStartTime(raw)
	if err != nil {
		t.Fatalf("parseStartTime: %v", err)
	}
	if got != "14:45" {
		t.Fatalf("parseStartTime = %q, want %q", got, "14:45")
	}
}

func TestParseStartTimeRejectsMissingTrigger(t *testing.T) {
	if _, err := parseStartTime([]byte(`<Task><Triggers></Triggers></Task>`)); err == nil {
		t.Fatal("expected parseStartTime to fail when no CalendarTrigger/StartBoundary is present")
	}
}

func TestDecodeMaybeUTF16PassesThroughPlainUTF8(t *testing.T) {
	input := []byte(`<Task>plain utf-8</Task>`)
	got, err := decodeMaybeUTF16(input)
	if err != nil {
		t.Fatalf("decodeMaybeUTF16: %v", err)
	}
	if string(got) != string(input) {
		t.Fatalf("decodeMaybeUTF16 changed plain UTF-8 input: got %q, want %q", got, input)
	}
}

func TestTaskNotFound(t *testing.T) {
	found := []byte("ERROR: The system cannot find the file specified.")
	if !taskNotFound(found) {
		t.Errorf("expected taskNotFound to recognize schtasks' real not-found message")
	}
	other := []byte("SUCCESS: The scheduled task is running.")
	if taskNotFound(other) {
		t.Errorf("expected taskNotFound to return false for an unrelated message")
	}
}

// TestBuildTaskXMLUsesTodayAsStartDate confirms the StartBoundary's date
// component is today (or, in the unlikely event this test runs across
// midnight, tomorrow) rather than some arbitrary/fixed placeholder date -
// see buildTaskXML's doc comment on why the date itself doesn't matter for
// a daily recurrence but should still look sane if a user inspects the
// task in Task Scheduler's own UI.
func TestBuildTaskXMLUsesTodayAsStartDate(t *testing.T) {
	raw, err := buildTaskXML(`C:\fake\opal-downloader.exe`, "06:00")
	if err != nil {
		t.Fatalf("buildTaskXML: %v", err)
	}
	decoded, err := decodeMaybeUTF16(raw)
	if err != nil {
		t.Fatalf("decodeMaybeUTF16: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	if !strings.Contains(string(decoded), today+"T06:00:00") {
		t.Errorf("expected task XML StartBoundary date to be today (%s), got:\n%s", today, decoded)
	}
}
