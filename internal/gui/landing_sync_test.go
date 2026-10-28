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

func TestApplySyncReadinessCountsConfiguredCourses(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, "\n  - Analysis\n  - Numerik")}
	data := landingData{LoggedIn: true}
	s.applySyncReadiness(&data)

	if data.SetupNeeded {
		t.Fatal("a readable config is not a setup problem")
	}
	if data.CourseCount != 2 {
		t.Fatalf("expected 2 courses, got %d", data.CourseCount)
	}
}

// An empty courses list is NOT "nothing selected": config.Load turns it into
// []string{"*"}, meaning every course. The landing page must say so rather
// than report the wildcard as "your 1 selected course".
func TestApplySyncReadinessTreatsEmptyCourseListAsAllCourses(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, " []")}
	data := landingData{LoggedIn: true}
	s.applySyncReadiness(&data)

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

// Having no saved session must not block anything: sync's own ensureSession
// logs in via TU-Fast, 2FA included, without anyone at the machine. The old
// gate here turned that few-second detour into a dead button plus "run the
// login step first", which is the one thing the user cannot usefully do
// about it.
func TestApplySyncReadinessStillOffersSyncWhenNotLoggedIn(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, "\n  - Analysis")}
	data := landingData{LoggedIn: false}
	s.applySyncReadiness(&data)

	if data.SetupNeeded {
		t.Fatal("a valid config that just needs a login is not a setup problem")
	}
}

// No usable config is the only state that stops a sync, and the landing page
// turns it into a live setup button rather than a dead one.
func TestApplySyncReadinessBlockedWhenConfigMissing(t *testing.T) {
	s := &server{configPath: filepath.Join(t.TempDir(), "does-not-exist.yaml")}
	data := landingData{LoggedIn: true}
	s.applySyncReadiness(&data)

	if !data.SetupNeeded {
		t.Fatal("expected SetupNeeded so the page offers setup instead of a dead sync button")
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
	// Not logged in in this test, and that is no reason to withhold the
	// button - the sync it starts logs in by itself.
	if strings.Contains(body, `class="cta disabled"`) {
		t.Fatalf("expected a live CTA when only the login is missing, body was:\n%s", body)
	}
	// Scoped to the CTA element: "/sync?autostart=1" also legitimately
	// appears in the scheduled-status banner's script (gui.go's
	// bannerChrome), which every page carries, so asserting against the whole
	// body would fail for the wrong reason.
	if !strings.Contains(body, `<a class="cta" href="/sync?autostart=1"`) {
		t.Fatalf("expected the autostart CTA, body was:\n%s", body)
	}
	// `list`/`dump-links` are developer tools and should no longer be
	// advertised by name on the landing page.
	if strings.Contains(body, "Sync / List / Dump links") {
		t.Fatal("landing page still advertises the developer tools in its nav label")
	}
}

// A first run with no config yet cannot possibly be logged in - there's no
// OPAL URL or credentials to log into. Showing "Not logged in yet" above the
// setup button the finding is actually meant to reach is noise, not signal.
func TestLandingPageHidesLoginStatusWhenSetupNeeded(t *testing.T) {
	s := &server{configPath: filepath.Join(t.TempDir(), "does-not-exist.yaml")}
	rec := httptest.NewRecorder()
	s.handleLanding(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if strings.Contains(body, "Not logged in yet") {
		t.Fatalf("expected no login-status box before setup, body was:\n%s", body)
	}
	if !strings.Contains(body, "Set up opal-downloader") {
		t.Fatalf("expected the setup CTA, body was:\n%s", body)
	}
}

// A first run has to explain itself. The maintainer's call, 2026-07-26: the
// start page and the not-yet-configured state should give "eine gute
// Einführung", not just a button. Before this, a stranger's first screen was a
// disclaimer, an update box, and a "Set up opal-downloader" link with one
// sentence - nothing saying what the tool does with their files or their
// password.
func TestLandingPageIntroducesItselfOnAFirstRun(t *testing.T) {
	s := &server{configPath: filepath.Join(t.TempDir(), "does-not-exist.yaml")}
	rec := httptest.NewRecorder()
	s.handleLanding(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if !strings.Contains(body, `class="intro"`) {
		t.Fatalf("a first run should introduce the tool, body was:\n%s", body)
	}
	// The three things a stranger needs before trusting it with a login: what
	// it does, what they have to do, and where their credentials end up.
	for _, want := range []string{
		"What this does",
		"What you'll do once",
		"Log in to OPAL once",
		"runs on your own machine",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("first-run intro is missing %q, body was:\n%s", want, body)
		}
	}
}

// ...and it has to get out of the way once there is nothing to introduce.
func TestLandingPageDropsTheIntroOnceConfigured(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, "\n  - Analysis")}
	rec := httptest.NewRecorder()
	s.handleLanding(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if body := rec.Body.String(); strings.Contains(body, `class="intro"`) {
		t.Fatalf("the first-run intro is still shown after setup, body was:\n%s", body)
	}
}

// Once a config exists, the login state is meaningful again and must come
// back.
func TestLandingPageShowsLoginStatusOnceConfigured(t *testing.T) {
	s := &server{configPath: writeLandingConfig(t, "\n  - Analysis")}
	rec := httptest.NewRecorder()
	s.handleLanding(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "Not logged in yet") {
		t.Fatalf("expected the login-status box once configured, body was:\n%s", body)
	}
}
