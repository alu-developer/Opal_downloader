package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The landing page's "Sync now" button is the one action a returning user
// opens the GUI for, so it has to be honest about whether clicking it can
// actually work. These tests pin each state it can be in.

func writeLandingConfig(t *testing.T, courses string) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yaml := `download_path: ./downloads
courses:` + courses + `
sync: true
opal_url: https://bildungsportal.sachsen.de/opal/
session_state_file: ./state.json
`
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func TestApplySyncReadinessReadyWhenConfiguredAndLoggedIn(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, "\n  - Analysis\n  - Numerik")}
	data := landingData{LoggedIn: true}
	s.applySyncReadiness(&data)

	if !data.SyncReady {
		t.Fatalf("expected sync to be ready, blocked reason was %q", data.SyncBlockedReason)
	}
	if data.CourseCount != 2 {
		t.Fatalf("expected 2 courses, got %d", data.CourseCount)
	}
	if data.SyncBlockedReason != "" {
		t.Fatalf("expected no blocked reason when ready, got %q", data.SyncBlockedReason)
	}
}

// An empty courses list is NOT "nothing selected": config.Load turns it into
// []string{"*"}, meaning every course. The landing page must say so rather
// than report the wildcard as "your 1 selected course".
func TestApplySyncReadinessTreatsEmptyCourseListAsAllCourses(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, " []")}
	data := landingData{LoggedIn: true}
	s.applySyncReadiness(&data)

	if !data.SyncReady {
		t.Fatalf("expected sync to be ready with the wildcard filter, blocked reason was %q", data.SyncBlockedReason)
	}
	if !data.SyncAllCourses {
		t.Fatal("expected SyncAllCourses for an empty course list, which config.Load expands to the * wildcard")
	}
}

func TestApplySyncReadinessDetectsExplicitWildcard(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, "\n  - \"*\"")}
	data := landingData{LoggedIn: true}
	s.applySyncReadiness(&data)

	if !data.SyncAllCourses {
		t.Fatal("expected SyncAllCourses for an explicit * entry")
	}
}

func TestApplySyncReadinessNamedCoursesAreNotWildcard(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, "\n  - Analysis\n  - Numerik")}
	data := landingData{LoggedIn: true}
	s.applySyncReadiness(&data)

	if data.SyncAllCourses {
		t.Fatal("named courses must not be reported as the all-courses wildcard")
	}
}

func TestApplySyncReadinessBlockedWhenNotLoggedIn(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, "\n  - Analysis")}
	data := landingData{LoggedIn: false}
	s.applySyncReadiness(&data)

	if data.SyncReady {
		t.Fatal("expected sync to be blocked when not logged in")
	}
	if !strings.Contains(data.SyncBlockedReason, "Not logged in") {
		t.Fatalf("expected a login-specific reason, got %q", data.SyncBlockedReason)
	}
}

func TestApplySyncReadinessBlockedWhenConfigMissing(t *testing.T) {
	s := &server{configPath: filepath.Join(t.TempDir(), "does-not-exist.yaml")}
	data := landingData{LoggedIn: true}
	s.applySyncReadiness(&data)

	if data.SyncReady {
		t.Fatal("expected sync to be blocked when the config cannot be loaded")
	}
	if !strings.Contains(data.SyncBlockedReason, "configuration") {
		t.Fatalf("expected a config-specific reason, got %q", data.SyncBlockedReason)
	}
}

// A sync already in flight must not be offered as a fresh start: the
// overlap guard would reject the second run, turning the landing page's
// primary action into a confusing error.
func TestApplySyncReadinessReportsRunningJob(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, "\n  - Analysis"), syncJob: newJob()}
	if !s.syncJob.start(jobKindSync, func() {}) {
		t.Fatal("could not start the fake job")
	}

	data := landingData{LoggedIn: true}
	s.applySyncReadiness(&data)
	if !data.SyncRunning {
		t.Fatal("expected SyncRunning to be true while a job is in flight")
	}

	s.syncJob.finish()
	data = landingData{LoggedIn: true}
	s.applySyncReadiness(&data)
	if data.SyncRunning {
		t.Fatal("expected SyncRunning to be false once the job finished")
	}
}

// A nil syncJob (no sync routes registered, as in several existing tests)
// must not panic - the landing page is the entry point and has to render.
func TestApplySyncReadinessToleratesNilJob(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, "\n  - Analysis")}
	data := landingData{LoggedIn: true}
	s.applySyncReadiness(&data)
	if data.SyncRunning {
		t.Fatal("expected SyncRunning false when there is no job at all")
	}
}

func TestLandingPageRendersPrimarySyncAction(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, "\n  - Analysis")}
	rec := httptest.NewRecorder()
	s.handleLanding(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	// Not logged in in this test, so the button must render disabled rather
	// than linking somewhere that would fail.
	if !strings.Contains(body, `class="cta disabled"`) {
		t.Fatalf("expected a disabled CTA when not logged in, body was:\n%s", body)
	}
	// Scoped to the CTA element: "/sync?autostart=1" also legitimately
	// appears in the scheduled-status banner's script (gui.go's
	// bannerChrome), which every page carries, so asserting against the whole
	// body would fail for the wrong reason.
	if strings.Contains(body, `<a class="cta" href="/sync?autostart=1"`) {
		t.Fatal("a blocked sync must not render an enabled autostart CTA")
	}
	// `list`/`dump-links` are developer tools and should no longer be
	// advertised by name on the landing page.
	if strings.Contains(body, "Sync / List / Dump links") {
		t.Fatal("landing page still advertises the developer tools in its nav label")
	}
}
