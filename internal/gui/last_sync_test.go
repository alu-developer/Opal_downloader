package gui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/statuslog"
	"github.com/alu-developer/opal-downloader/internal/syncer"
)

// withLastSyncFake swaps readLastSyncFunc for the duration of one test, the
// same pattern withScheduledStatusFake already uses - no test may depend on
// whether the machine running it has ever synced.
func withLastSyncFake(t *testing.T, fn func() (statuslog.Status, bool)) {
	t.Helper()
	prev := readLastSyncFunc
	readLastSyncFunc = fn
	t.Cleanup(func() { readLastSyncFunc = prev })
}

// TestApplyLastSyncSaysNothingWithoutARecord pins the degrade-silently
// contract: a machine that has never synced, or an unreadable record, must
// leave the line off the page entirely. An absent record is not "never
// synced" and must not render as any claim at all.
func TestApplyLastSyncSaysNothingWithoutARecord(t *testing.T) {
	withLastSyncFake(t, func() (statuslog.Status, bool) { return statuslog.Status{}, false })

	var data landingData
	(&server{}).applyLastSync(&data)

	if data.LastSyncKnown {
		t.Fatalf("LastSyncKnown = true with no record on disk; the line must not render")
	}
	if data.LastSyncWhen != "" || data.LastSyncDetail != "" {
		t.Fatalf("expected empty last-sync fields, got when=%q detail=%q", data.LastSyncWhen, data.LastSyncDetail)
	}
}

// TestApplyLastSyncIgnoresAZeroTimestamp covers a record that parsed but
// carries no usable time - rendering "Last sync: 1 Jan 0001" would be worse
// than saying nothing.
func TestApplyLastSyncIgnoresAZeroTimestamp(t *testing.T) {
	withLastSyncFake(t, func() (statuslog.Status, bool) {
		return statuslog.Status{Outcome: statuslog.OutcomeSuccess}, true
	})

	var data landingData
	(&server{}).applyLastSync(&data)

	if data.LastSyncKnown {
		t.Fatalf("a zero timestamp rendered as a known last sync")
	}
}

// TestApplyLastSyncDetailPerOutcome pins that a clean success gets no
// trailing clause while every non-success outcome explains itself - a
// failed run must never read as though it synced fine at that time.
func TestApplyLastSyncDetailPerOutcome(t *testing.T) {
	cases := []struct {
		name       string
		status     statuslog.Status
		wantDetail string
	}{
		{
			name:       "success is bare",
			status:     statuslog.Status{Outcome: statuslog.OutcomeSuccess},
			wantDetail: "",
		},
		{
			name:       "failure says so",
			status:     statuslog.Status{Outcome: statuslog.OutcomeFailure},
			wantDetail: "it failed",
		},
		{
			name:       "partial names the count",
			status:     statuslog.Status{Outcome: statuslog.OutcomePartial, FilesErrored: 3},
			wantDetail: "3 file(s) failed",
		},
		{
			name:       "skipped is not a sync",
			status:     statuslog.Status{Outcome: statuslog.OutcomeSkipped},
			wantDetail: "skipped, another run had it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := tc.status
			status.Timestamp = time.Now().Add(-90 * time.Minute)
			withLastSyncFake(t, func() (statuslog.Status, bool) { return status, true })

			var data landingData
			(&server{}).applyLastSync(&data)

			if !data.LastSyncKnown {
				t.Fatalf("LastSyncKnown = false for a well-formed record")
			}
			if data.LastSyncDetail != tc.wantDetail {
				t.Fatalf("detail = %q, want %q", data.LastSyncDetail, tc.wantDetail)
			}
		})
	}
}

// TestApplyLastSyncSaysNothingWhenSetupNeeded pins the friction campaign's
// Walk 7 fix (2026-08-15): a config.yaml that has never been set up must
// never show a "Last sync" line at all, even when the machine-wide record
// this reads from (see statuslog.WriteLastSyncDefault's own doc comment -
// it is written by *any* config's sync, not scoped to this one) has a real,
// recent, successful entry. Before this, "First time here? This sets your
// download folder..." rendered directly above a stale, unrelated "Last
// sync: ..." line for a setup that had never run anything - two adjacent,
// contradictory claims about the same never-before-seen setup.
func TestApplyLastSyncSaysNothingWhenSetupNeeded(t *testing.T) {
	withLastSyncFake(t, func() (statuslog.Status, bool) {
		return statuslog.Status{Outcome: statuslog.OutcomeSuccess, Timestamp: time.Now().Add(-90 * time.Minute)}, true
	})

	data := landingData{SetupNeeded: true}
	(&server{}).applyLastSync(&data)

	if data.LastSyncKnown {
		t.Fatalf("LastSyncKnown = true while SetupNeeded; the line must not render on a first-time setup")
	}
}

func TestHumanizeSince(t *testing.T) {
	now := time.Date(2026, 8, 12, 15, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		ts   time.Time
		want string
	}{
		{"seconds", now.Add(-30 * time.Second), "just now"},
		{"under two minutes", now.Add(-90 * time.Second), "just now"},
		{"minutes", now.Add(-25 * time.Minute), "25 minutes ago"},
		{"one hour", now.Add(-70 * time.Minute), "1 hour ago"},
		{"hours", now.Add(-5 * time.Hour), "5 hours ago"},
		{"yesterday", now.Add(-30 * time.Hour), "yesterday"},
		{"days", now.Add(-4 * 24 * time.Hour), "4 days ago"},
		{"past a week falls back to a date", now.Add(-30 * 24 * time.Hour), "13 Jul 2026"},
		// Clock changes and DST make a future timestamp reachable. It must
		// not render as a negative age.
		{"future clamps to just now", now.Add(2 * time.Hour), "just now"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := humanizeSince(now, tc.ts); got != tc.want {
				t.Fatalf("humanizeSince = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLandingRendersLastSyncLine checks the template actually surfaces the
// data - the fields being populated is not the same as the user seeing them.
func TestLandingRendersLastSyncLine(t *testing.T) {
	var sb strings.Builder
	err := landingTemplate.Execute(&sb, landingData{
		LastSyncKnown: true,
		LastSyncWhen:  "3 hours ago",
	})
	if err != nil {
		t.Fatalf("rendering landing template: %v", err)
	}
	if !strings.Contains(sb.String(), "Last sync: 3 hours ago") {
		t.Fatalf("landing page did not render the last-sync line")
	}
}

// TestLandingOmitsLastSyncLineWhenUnknown is the other half: no record must
// mean no line, not an empty or half-rendered one.
func TestLandingOmitsLastSyncLineWhenUnknown(t *testing.T) {
	var sb strings.Builder
	if err := landingTemplate.Execute(&sb, landingData{}); err != nil {
		t.Fatalf("rendering landing template: %v", err)
	}
	if strings.Contains(sb.String(), "Last sync:") {
		t.Fatalf("landing page rendered a last-sync line with no record")
	}
}

// TestBuildGUISyncStatusOutcomes pins how a GUI-initiated run is recorded.
// The cancellation case is the load-bearing one: a run the user stopped
// early left the download folder in a partial state, so recording it as a
// success would make the landing page claim the files are current when they
// are not.
func TestBuildGUISyncStatusOutcomes(t *testing.T) {
	now := time.Now()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name        string
		ctx         context.Context
		runErr      error
		stats       syncer.Stats
		wantOutcome statuslog.Outcome
		wantMsgPart string
	}{
		{
			name:        "clean run",
			ctx:         context.Background(),
			stats:       syncer.Stats{Downloaded: 4, Skipped: 40},
			wantOutcome: statuslog.OutcomeSuccess,
			wantMsgPart: "4 downloaded",
		},
		{
			name:        "file errors are partial",
			ctx:         context.Background(),
			stats:       syncer.Stats{Downloaded: 1, Skipped: 2, Errors: 3},
			wantOutcome: statuslog.OutcomePartial,
			wantMsgPart: "3 file error(s)",
		},
		{
			name:        "hard failure",
			ctx:         context.Background(),
			runErr:      errors.New("could not reach OPAL"),
			wantOutcome: statuslog.OutcomeFailure,
			wantMsgPart: "could not reach OPAL",
		},
		{
			name:        "user cancelled is not a success",
			ctx:         cancelled,
			runErr:      context.Canceled,
			wantOutcome: statuslog.OutcomeFailure,
			wantMsgPart: "Cancelled by the user",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildGUISyncStatus(now, tc.ctx, tc.runErr, tc.stats)
			if got.Outcome != tc.wantOutcome {
				t.Fatalf("outcome = %q, want %q", got.Outcome, tc.wantOutcome)
			}
			if !strings.Contains(got.Message, tc.wantMsgPart) {
				t.Fatalf("message = %q, want it to contain %q", got.Message, tc.wantMsgPart)
			}
			if !got.Timestamp.Equal(now) {
				t.Fatalf("timestamp = %v, want %v", got.Timestamp, now)
			}
		})
	}
}

// TestBuildGUISyncStatusSanitizesTheMessage confirms a GUI-side failure goes
// through the same sanitization boundary the scheduled path uses - this
// record lands on disk, so a raw error carrying session-shaped content must
// not be written verbatim.
func TestBuildGUISyncStatusSanitizesTheMessage(t *testing.T) {
	raw := errors.New(`login failed: {"cookies":[{"name":"JSESSIONID","value":"abc"}]}`)
	got := buildGUISyncStatus(time.Now(), context.Background(), raw, syncer.Stats{})

	if strings.Contains(got.Message, "JSESSIONID") {
		t.Fatalf("session-shaped content survived into the last-sync record: %q", got.Message)
	}
}
