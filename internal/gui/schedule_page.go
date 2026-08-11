package gui

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scheduler"
)

// Automatic sync gets its own page rather than a section of Settings.
//
// The maintainer's observation (2026-07-26): "irgendwie ist es weird, dass
// scheduled runs unter settings sind. Zumal settings auch einfach
// folder-configurations heissen koennte" - Settings is really about what to
// download and where to put it, and a daily schedule is a different kind of
// thing. They offered two options, its own page or folding it into the sync
// options, and left the call here.
//
// Its own page, because:
//
//   - /sync is where you make something happen now. Putting "run every day at
//     06:00" next to a button that runs immediately invites exactly the
//     mis-click it sounds like.
//   - The old arrangement had two independent <form>s on one page with two
//     separate save buttons, one of which ("Save settings") did not save the
//     schedule and the other of which ("Save schedule") did not save the
//     settings. That is not a layout problem, it is two features sharing a
//     page because nobody split them.
//   - "Notify me if a scheduled sync fails" was stranded under a
//     "Notifications" heading in the *settings* form, about a feature
//     configured further down the page in the *other* form. It moved here
//     with automatic sync, and then stopped being a choice at all - see
//     below.
//
// One form, one save button, and everything on the page is about the same
// question: should this run by itself.
//
// The failure notification is no longer a checkbox (2026-08-03). It always
// fires, and the page states that as a fact rather than offering it as a
// preference: an automatic run is by definition one nobody watched, so the
// notification is the only way its failure is ever noticed, and turning it
// off only buys silence about a sync that has quietly stopped happening.
// Removed from config.yaml too (internal/config's rawConfig), not just
// hidden here.

type schedulePageData struct {
	Supported bool
	Enabled   bool
	Time      string

	Error  string
	Saved  bool
	Notice string

	// SetupNeeded reports that there is no config yet. Scheduling a sync
	// before there is anything to sync is not a useful thing to let someone
	// do, and the page says so rather than failing later.
	SetupNeeded bool
}

func handleSchedulePage(configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/schedule" {
			http.NotFound(w, r)
			return
		}

		switch r.Method {
		case http.MethodGet:
			renderSchedulePage(w, loadSchedulePageData(configPath))
		case http.MethodPost:
			renderSchedulePage(w, applyScheduleForm(configPath, r))
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// loadSchedulePageData reads the live Task Scheduler state. It is
// deliberately re-queried every render rather than persisted: the registered
// task is the source of truth, and it can be changed or removed in Windows'
// own Task Scheduler UI without this program knowing.
func loadSchedulePageData(configPath string) schedulePageData {
	view := applyScheduleStatus(settingsViewData{})

	data := schedulePageData{
		Supported: view.ScheduleSupported,
		Enabled:   view.ScheduleEnabled,
		Time:      view.ScheduleTime,
		Error:     view.ScheduleError,
		Notice:    view.ScheduleNotice,
	}
	if data.Time == "" {
		// Not DefaultTime: every install proposing the same 06:00 means every
		// install hits OPAL at the same instant. See scheduler.SuggestedTime.
		data.Time = scheduler.SuggestedTime()
	}

	if _, err := config.Load(configPath); err != nil {
		data.SetupNeeded = true
	}
	return data
}

// applyScheduleForm registers or removes the scheduled task. It is the whole
// of the save button now that the notification preference is gone.
func applyScheduleForm(configPath string, r *http.Request) schedulePageData {
	_ = r.ParseForm()

	enable := r.FormValue("schedule_enabled") == "on"
	at := strings.TrimSpace(r.FormValue("schedule_time"))
	if at == "" {
		at = scheduler.DefaultTime
	}

	scheduleErr := applyScheduleRegistration(enable, at, configPath)

	data := loadSchedulePageData(configPath)
	if scheduleErr != nil {
		data.Error = scheduleErr.Error()
		// The live re-query above reflects reality, but the user's unsaved
		// intent is what they should see in the control they just used.
		data.Time = at
		return data
	}
	data.Saved = true
	return data
}

func renderSchedulePage(w http.ResponseWriter, data schedulePageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := schedulePageTemplate.Execute(w, data); err != nil {
		http.Error(w, "failed to render schedule page: "+err.Error(), http.StatusInternalServerError)
	}
}

var schedulePageTemplate = template.Must(template.New("schedule").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
` + faviconLink + `
<title>Opal Downloader - Automatic sync</title>
<style>` + pageStyle + `
	.field { margin-bottom: 1.1rem; }
	label { display: block; font-weight: 600; margin-bottom: 0.25rem; }
	input[type=text] { padding: 0.4rem 0.5rem; border: 1px solid #ccc; border-radius: 4px; font: inherit; }
	.checkbox-row { display: flex; align-items: center; gap: 0.5rem; }
	.checkbox-row label { margin: 0; font-weight: 600; }
	.save-btn { padding: 0.5rem 1rem; border-radius: 4px; border: 1px solid #1a73e8; background: #1a73e8; color: #fff; cursor: pointer; font: inherit; font-weight: 600; margin-top: 1rem; }
</style>
</head>
<body>
	` + bannerChrome + `
	<h1>Automatic sync</h1>

	{{if not .Supported}}
	<p class="hint">Automatic sync is only available on Windows, where it uses
	Task Scheduler. On other systems you can get the same result with cron or
	your desktop's own scheduler, running <code>opal-downloader sync
	--scheduled</code>.</p>
	<p class="back"><a href="/">&larr; Back</a></p>
	{{else}}

	{{if .SetupNeeded}}
	<div class="status warn">
		There is nothing to sync yet. <a href="/settings">Set up opal-downloader</a>
		first, then come back and decide whether it should run on its own.
	</div>
	{{end}}

	{{if .Error}}<div class="error"><strong>Could not change the schedule:</strong> {{.Error}}</div>{{end}}
	{{if .Saved}}<div class="success">Saved.</div>{{end}}
	{{if .Notice}}<div class="warning"><strong>Daily sync repaired:</strong> {{.Notice}}</div>{{end}}

	<p class="hint">Off by default. When it is on, opal-downloader asks Windows
	to run a sync once a day at the time you choose, catching up automatically
	if the machine was off, asleep, or you weren't logged in yet - including
	trying again the moment you next log in. No password is stored for it, and
	it never runs unless you are logged in to Windows.</p>

	<form method="post" action="/schedule" id="schedule-form">

	<div class="field checkbox-row">
		<input type="checkbox" id="schedule_enabled" name="schedule_enabled" {{if .Enabled}}checked{{end}}>
		<label for="schedule_enabled">Sync once a day, on its own</label>
	</div>

	<div class="field">
		<label for="schedule_time">Time of day</label>
		<input type="text" id="schedule_time" name="schedule_time" value="{{.Time}}" placeholder="06:00" style="width: 6rem;">
		<p class="hint">24-hour, e.g. <code>06:00</code>.</p>
	</div>

	<p class="hint">This needs TU-Fast already set up in the dedicated login
	profile (see <a href="/tufast-setup">TU-Fast setup</a>). Without it, an
	unattended run stops and waits for a 2FA click that nobody is there to
	make, so it fails fast instead.</p>

	<p class="hint">If an automatic run fails outright, Windows shows you a
	notification. Nothing pops up for a run that succeeded, or for one that
	downloaded some files and had trouble with others &ndash; only for a run
	that got nowhere.</p>

	<button type="submit" class="save-btn">Save</button>
	</form>
	{{end}}

	<p class="back"><a href="/">&larr; Back</a></p>
	` + unsavedChangesGuard + `
</body>
</html>
`))
