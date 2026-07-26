package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/syncer"
)

// courseRow is one editable row of the merged courses table: a course
// name/pattern plus an optional per-course folder override. This replaces
// the old separate "Courses" textarea and "Course folder rules" table -
// having the user type each course name twice (once to select it for sync,
// once to give it a custom folder) was pure friction for the common case of
// one course with one specific folder.
type courseRow struct {
	Name   string
	Folder string
}

// sectionFolderRow is one editable row of the section_folder_names map: an
// OPAL section-name pattern mapped to the local folder name to use instead.
type sectionFolderRow struct {
	Pattern string
	Folder  string
}

// subfolderDestinationRow is one editable row of the subfolder_destinations
// map. Key is the raw "<course pattern>/<subfolder pattern>" string (kept as
// a single field, matching the on-disk YAML key syntax documented in
// config.example.yaml) and Destination is the target path.
type subfolderDestinationRow struct {
	Key         string
	Destination string
}

// settingsViewData is passed to the settings template.
//
// OpalURL and SessionStateFile are deliberately not exposed here: OPAL only
// has one real-world instance in practice, and the session state file path
// is an internal implementation detail. Both are always saved/loaded using
// config.DefaultOPALURL / config.DefaultStateFile - see parseSettingsForm.
// Advanced users who need something different can still hand-edit
// config.yaml directly; the GUI just won't show or round-trip those fields.
type settingsViewData struct {
	ConfigPath string
	Error      string
	Saved      bool
	Warnings   []string

	// FirstRun means there is no config.yaml yet, so this page is being seen
	// by someone who has never used the tool. It is a long form - browser,
	// folders, subfolder rules, notifications, scheduling - and almost none of
	// it needs a decision on day one. Without a word saying so, a stranger
	// arriving from "Set up opal-downloader" has to read all of it to find out
	// that one field matters.
	FirstRun bool

	DownloadPath        string
	SyncAllCourses      bool
	CourseRows          []courseRow
	Sync                bool
	DefaultCourseFolder string

	UseSectionSubfolders  bool
	SectionFolderNames    []sectionFolderRow
	SubfolderDestinations []subfolderDestinationRow

	// NotifyOnScheduledFailure drives the "Notify me if a scheduled sync
	// fails" checkbox (see internal/notify). Unlike the Schedule* fields
	// below, this IS a plain config.yaml field (config.App.
	// NotifyOnScheduledFailure) - saved/loaded through the main settings
	// form the same way as Sync/UseSectionSubfolders, not queried live from
	// an OS-level source.
	NotifyOnScheduledFailure bool

	// Schedule* fields drive the "Enable daily automatic sync" section (see
	// schedule.go/applyScheduleStatus) - deliberately not part of
	// config.yaml/config.App: the source of truth for whether scheduling is
	// on is the registered Windows Task Scheduler task itself, queried live
	// on every page load, not a persisted setting that could drift out of
	// sync with the real OS-level registration.
	ScheduleSupported bool
	ScheduleEnabled   bool
	ScheduleTime      string
	ScheduleError     string
	ScheduleSaved     bool

	// ScheduleNotice reports a repair the page carried out on its own (see
	// repairDoomedSchedule). Separate from ScheduleError because nothing is
	// wrong any more, and from ScheduleSaved because the user did not ask
	// for it and deserves to be told what changed.
	ScheduleNotice string
}

// isSyncAllCourses reports whether courses is the "sync everything" sentinel
// (a single "*" entry), matching config.CourseMatches' own treatment of it.
func isSyncAllCourses(courses []string) bool {
	return len(courses) == 1 && strings.TrimSpace(courses[0]) == "*"
}

// buildCourseRows merges the Courses list and CourseFolders map back into
// the single-table shape the settings form edits. When syncAll is true, the
// Courses list itself is just the "*" sentinel and carries no per-course
// information, so only CourseFolders entries are shown (still useful: a
// folder override can apply to a specific course even while syncing
// everything). When syncAll is false, one row is emitted per course, with
// its folder filled in from CourseFolders on an exact-name match; any
// CourseFolders entries that don't exactly match a listed course (e.g. glob
// patterns) are appended as extra rows so switching sync-all on and off
// never silently drops data.
func buildCourseRows(courses []string, courseFolders map[string]string) []courseRow {
	var rows []courseRow
	seen := map[string]bool{}

	if !isSyncAllCourses(courses) {
		for _, c := range courses {
			rows = append(rows, courseRow{Name: c, Folder: courseFolders[c]})
			seen[c] = true
		}
	}

	extraKeys := make([]string, 0, len(courseFolders))
	for k := range courseFolders {
		if !seen[k] {
			extraKeys = append(extraKeys, k)
		}
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		rows = append(rows, courseRow{Name: k, Folder: courseFolders[k]})
	}

	return rows
}

// loadedToViewData builds the settings view from a loaded config. There is
// no browser-related view data anymore (config.Credentials no longer has a
// BrowserExecutable/BrowserUserDataDir/BrowserProfileDir concept to show or
// edit here at all - opal-downloader always launches Playwright's bundled
// Chromium against the single hardcoded dedicated login profile, see
// scraper.LoginProfileDir and the /tufast-setup page).
func loadedToViewData(configPath string, loaded config.Loaded) settingsViewData {
	sectionRows := make([]sectionFolderRow, 0, len(loaded.App.SectionFolderNames))
	for pattern, folder := range loaded.App.SectionFolderNames {
		sectionRows = append(sectionRows, sectionFolderRow{Pattern: pattern, Folder: folder})
	}

	destRows := make([]subfolderDestinationRow, 0, len(loaded.App.SubfolderDestinations))
	for key, dest := range loaded.App.SubfolderDestinations {
		destRows = append(destRows, subfolderDestinationRow{Key: key, Destination: dest})
	}

	return settingsViewData{
		ConfigPath: configPath,
		Warnings:   config.Warnings(loaded.App),

		DownloadPath:        loaded.App.DownloadPath,
		SyncAllCourses:      isSyncAllCourses(loaded.App.Courses),
		CourseRows:          buildCourseRows(loaded.App.Courses, loaded.App.CourseFolders),
		Sync:                loaded.App.Sync,
		DefaultCourseFolder: loaded.App.DefaultCourseFolder,

		UseSectionSubfolders:  loaded.App.UseSectionSubfolders,
		SectionFolderNames:    sectionRows,
		SubfolderDestinations: destRows,

		NotifyOnScheduledFailure: loaded.App.NotifyOnScheduledFailure,
	}
}

// parseSettingsForm reads the posted form fields into a settingsViewData
// (for re-rendering on validation error) and a config.Loaded (to attempt to
// save). The view data always reflects exactly what the user submitted, so
// a validation error round-trips the user's input rather than silently
// reverting to the last saved config.
//
// base is the currently-persisted config (or the zero value if none exists
// yet). Only the fields the Settings form actually exposes are overwritten
// on top of base; everything else - notably DownloadConcurrency and
// CourseConcurrency, which this form has no inputs for - is carried through
// unchanged so Save doesn't silently wipe hand-edited config.yaml fields the
// GUI doesn't yet know how to edit. UseSectionSubfolders, SectionFolderNames,
// and SubfolderDestinations *are* form fields (see the "Subfolder
// organization" section) and are always overwritten from the submission,
// same as course_folders.
//
// Credentials.URL and Credentials.StateFile are always set to
// config.DefaultOPALURL / config.DefaultStateFile - the Settings form no
// longer exposes either for editing (see settingsViewData's doc comment).
func parseSettingsForm(r *http.Request, configPath string, base config.Loaded) (settingsViewData, config.Loaded) {
	_ = r.ParseForm()

	get := func(name string) string {
		return strings.TrimSpace(r.FormValue(name))
	}

	syncAll := r.FormValue("sync_all_courses") == "on"

	rowNames := r.PostForm["course_row_name[]"]
	rowFolders := r.PostForm["course_row_folder[]"]
	courseFolders := map[string]string{}
	var courseNames []string
	rows := make([]courseRow, 0, len(rowNames))
	for i, rawName := range rowNames {
		name := strings.TrimSpace(rawName)
		folder := ""
		if i < len(rowFolders) {
			folder = strings.TrimSpace(rowFolders[i])
		}
		if name == "" && folder == "" {
			continue
		}
		rows = append(rows, courseRow{Name: name, Folder: folder})
		if name == "" {
			continue
		}
		courseNames = append(courseNames, name)
		if folder != "" {
			courseFolders[name] = folder
		}
	}

	var courses []string
	if syncAll {
		courses = []string{"*"}
	} else {
		courses = courseNames
	}

	sectionPatterns := r.PostForm["section_folder_pattern[]"]
	sectionFolders := r.PostForm["section_folder_folder[]"]
	sectionFolderNames := map[string]string{}
	sectionRows := make([]sectionFolderRow, 0, len(sectionPatterns))
	for i := range sectionPatterns {
		pattern := strings.TrimSpace(sectionPatterns[i])
		folder := ""
		if i < len(sectionFolders) {
			folder = strings.TrimSpace(sectionFolders[i])
		}
		sectionRows = append(sectionRows, sectionFolderRow{Pattern: pattern, Folder: folder})
		if pattern == "" && folder == "" {
			continue
		}
		sectionFolderNames[pattern] = folder
	}

	destKeys := r.PostForm["subfolder_dest_key[]"]
	destPaths := r.PostForm["subfolder_dest_path[]"]
	subfolderDestinations := map[string]string{}
	destRows := make([]subfolderDestinationRow, 0, len(destKeys))
	for i := range destKeys {
		key := strings.TrimSpace(destKeys[i])
		dest := ""
		if i < len(destPaths) {
			dest = strings.TrimSpace(destPaths[i])
		}
		destRows = append(destRows, subfolderDestinationRow{Key: key, Destination: dest})
		if key == "" && dest == "" {
			continue
		}
		subfolderDestinations[key] = dest
	}

	syncEnabled := r.FormValue("sync") == "on"
	useSectionSubfolders := r.FormValue("use_section_subfolders") == "on"
	notifyOnScheduledFailure := r.FormValue("notify_on_scheduled_failure") == "on"

	view := settingsViewData{
		ConfigPath: configPath,

		DownloadPath:        get("download_path"),
		SyncAllCourses:      syncAll,
		CourseRows:          rows,
		Sync:                syncEnabled,
		DefaultCourseFolder: get("default_course_folder"),

		UseSectionSubfolders:  useSectionSubfolders,
		SectionFolderNames:    sectionRows,
		SubfolderDestinations: destRows,

		NotifyOnScheduledFailure: notifyOnScheduledFailure,
	}

	loaded := base
	loaded.App.DownloadPath = view.DownloadPath
	loaded.App.Courses = courses
	loaded.App.Sync = syncEnabled
	loaded.App.DefaultCourseFolder = view.DefaultCourseFolder
	loaded.App.CourseFolders = courseFolders
	loaded.App.UseSectionSubfolders = useSectionSubfolders
	loaded.App.SectionFolderNames = sectionFolderNames
	loaded.App.SubfolderDestinations = subfolderDestinations
	loaded.App.NotifyOnScheduledFailure = notifyOnScheduledFailure
	loaded.Credentials.URL = config.DefaultOPALURL
	loaded.Credentials.StateFile = config.DefaultStateFile

	view.Warnings = config.Warnings(loaded.App)
	if warning := sectionSubfolderToggleWarning(base, loaded); warning != "" {
		view.Warnings = append(view.Warnings, warning)
	}

	return view, loaded
}

// sectionSubfolderToggleWarning returns a warning to show when the user has
// just flipped use_section_subfolders on a config that already has sync
// history recorded under the previous folder layout.
//
// This toggle silently re-keys every manifest entry (a section component is
// inserted into, or removed from, every key), which historically meant the
// next sync treated every file as new: it re-downloaded everything and left
// the previous copies orphaned with no warning at all. The syncer now
// detects and migrates that (see internal/syncer/migrate.go), but the user
// should still be told at the moment they cause it - it is the main way this
// whole failure mode gets tripped, and it is one unlabelled checkbox click.
func sectionSubfolderToggleWarning(base, saved config.Loaded) string {
	if base.App.UseSectionSubfolders == saved.App.UseSectionSubfolders {
		return ""
	}

	downloadPath := strings.TrimSpace(base.App.DownloadPath)
	if downloadPath == "" {
		downloadPath = strings.TrimSpace(saved.App.DownloadPath)
	}
	if downloadPath == "" {
		return ""
	}

	manifest, err := syncer.LoadManifest(filepath.Join(downloadPath, syncer.ManifestFileName))
	if err != nil || len(manifest.Files) == 0 {
		return ""
	}

	return fmt.Sprintf("You changed \"Organize downloads into a subfolder per OPAL section\". Your existing sync history (%d files, recorded in %s) was written under the previous folder layout, so every file now belongs in a different place. The next sync will detect this and move already-downloaded files to their new locations instead of re-downloading them; anything it cannot match unambiguously is listed in the sync log so you can move or delete it yourself. No file is ever deleted automatically.",
		len(manifest.Files), filepath.Join(downloadPath, syncer.ManifestFileName))
}

// loadSettingsViewData loads configPath into a settingsViewData for
// rendering, handling the "no config.yaml yet" and "config exists but
// won't parse" cases the same way handleSettings' GET branch always has.
// Extracted so handleScheduleAction (schedule.go) can re-render the full
// Settings page after an enable/disable action without duplicating this
// logic.
func loadSettingsViewData(configPath string) settingsViewData {
	loaded, err := config.Load(configPath)
	if err != nil {
		if strings.Contains(err.Error(), "config file not found") {
			// No config.yaml yet: show the form pre-filled with defaults so
			// the settings page still works as the primary first-run
			// configuration path.
			view := loadedToViewData(configPath, config.Loaded{
				App: config.App{
					DownloadPath: "./downloads",
					Courses:      []string{"*"},
					Sync:         true,
				},
				Credentials: config.Credentials{
					URL:       config.DefaultOPALURL,
					StateFile: config.DefaultStateFile,
				},
			})
			view.FirstRun = true
			return view
		}
		return settingsViewData{
			ConfigPath: configPath,
			Error:      fmt.Sprintf("Could not load %s: %v", configPath, err),
		}
	}
	return loadedToViewData(configPath, loaded)
}

func handleSettings(configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/settings" {
			http.NotFound(w, r)
			return
		}

		switch r.Method {
		case http.MethodGet:
			renderSettings(w, applyScheduleStatus(loadSettingsViewData(configPath)))

		case http.MethodPost:
			// Load the currently-persisted config first so fields the
			// Settings form has no inputs for (e.g. use_section_subfolders,
			// section_folder_names, subfolder_destinations) are preserved
			// rather than reset to zero values on save. If no config exists
			// yet, or the existing file can't be parsed, fall back to the
			// zero value - there's nothing on disk to preserve either way,
			// and Save's own validation will surface any real problem.
			base, err := config.Load(configPath)
			if err != nil {
				base = config.Loaded{}
			}

			view, loaded := parseSettingsForm(r, configPath, base)
			if err := config.Save(configPath, loaded); err != nil {
				view.Error = err.Error()
				renderSettings(w, applyScheduleStatus(view))
				return
			}
			view.Saved = true
			renderSettings(w, applyScheduleStatus(view))

		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func renderSettings(w http.ResponseWriter, data settingsViewData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := settingsTemplate.Execute(w, data); err != nil {
		http.Error(w, "failed to render settings page: "+err.Error(), http.StatusInternalServerError)
	}
}

// handleBrowseFolder opens a native folder-picker dialog on the machine
// running the GUI server and returns the chosen path as JSON. This exists
// because a browser's <input type=file webkitdirectory> cannot return a
// real filesystem path (sandboxed), which doesn't work for a local desktop
// tool that needs the user to point it at a real folder on disk. Since this
// GUI's server and the browser tab showing it always run on the same
// machine, shelling out to a native dialog server-side is the practical fix
// - see browseForFolder (window_windows.go / window_other.go) for the
// platform split.
func handleBrowseFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path, err := browseForFolder()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"path": path})
}

// discoverCoursesTimeout bounds the dashboard read. This is a
// names-only discovery (scraper.DiscoverCourseNames stops after the
// dashboard rather than crawling every section), so it is normally a few
// seconds; the generous ceiling only exists so a first run that has to
// establish a session, or an unusually slow OPAL, still completes rather
// than failing at the exact moment the user is trying to set the tool up.
const discoverCoursesTimeout = 3 * time.Minute

// handleDiscoverCourses backs the settings page's "Find my courses" button:
// it reads the user's OPAL dashboard and returns the course titles, so
// courses can be ticked from a list instead of typed by hand. Getting a
// course name subtly wrong is otherwise silent - the filter is an exact
// (case-insensitive) match, so a typo just means that course never syncs
// and nothing says why.
//
// Always responds 200 with a JSON body carrying either "courses" or
// "error"; the page renders the error inline rather than treating it as a
// transport failure, since the overwhelmingly common cause is simply "not
// logged in yet", which is a normal state during setup and not a fault.
func handleDiscoverCourses(configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		writeErr := func(msg string) {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
		}

		loaded, err := config.Load(configPath)
		if err != nil {
			writeErr("Could not read your configuration: " + err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), discoverCoursesTimeout)
		defer cancel()

		sc := scraper.New(loaded.Credentials.URL, loaded.Credentials.StateFile)
		defer func() { _ = sc.Close() }()

		names, err := sc.DiscoverCourseNames(ctx)
		if err != nil {
			writeErr("Could not read your courses from OPAL. Make sure you are logged in, then try again. (" + err.Error() + ")")
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"courses": names})
	}
}

var settingsTemplateFuncs = template.FuncMap{
	"add1": func(i int) int { return i + 1 },
	"itoa": strconv.Itoa,
}

var settingsTemplate = template.Must(template.New("settings").Funcs(settingsTemplateFuncs).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
` + faviconLink + `
<title>Opal Downloader - Settings</title>
<style>` + pageStyle + `
	.field { margin-bottom: 1.1rem; }
	label { display: block; font-weight: 600; margin-bottom: 0.25rem; }
	input[type=text], input[type=url], textarea {
		width: 100%; box-sizing: border-box; padding: 0.4rem 0.5rem;
		border: 1px solid #ccc; border-radius: 4px; font: inherit;
	}
	textarea { min-height: 4.5rem; font-family: ui-monospace, monospace; }
	.checkbox-row { display: flex; align-items: center; gap: 0.5rem; }
	.checkbox-row label { margin: 0; font-weight: 600; }
	.path-field { display: flex; gap: 0.4rem; }
	.path-field input[type=text] { flex: 1; }
	.browse-btn { padding: 0.4rem 0.7rem; border-radius: 4px; border: 1px solid #888; background: #f5f5f5; cursor: pointer; font: inherit; white-space: nowrap; }
	table.folders { width: 100%; border-collapse: collapse; margin-bottom: 0.5rem; }
	table.folders th { text-align: left; font-size: 0.8rem; color: #666; font-weight: 600; padding-bottom: 0.25rem; }
	table.folders td { padding: 0.2rem 0.4rem 0.2rem 0; }
	table.folders td:first-child { padding-left: 0; }
	.remove-row-btn { background: none; border: 1px solid #ccc; border-radius: 4px; cursor: pointer; color: #a00; padding: 0.3rem 0.6rem; }
	.add-row-btn, .save-btn { padding: 0.5rem 1rem; border-radius: 4px; border: 1px solid #888; background: #f5f5f5; cursor: pointer; font: inherit; }
	/* A proposed folder is marked until the form is saved or the field is
	   edited, so a filled-in path is never mistaken for one the user chose. */
	input.suggested { background: #eef7ee; border-color: #4c9a4c; }
	.save-btn { background: #1a73e8; color: #fff; border-color: #1a73e8; margin-top: 1.5rem; font-weight: 600; }
</style>
</head>
<body>
	` + bannerChrome + `
	<h1>Settings</h1>

	{{if .Error}}<div class="error"><strong>Could not save:</strong> {{.Error}}</div>{{end}}
	{{if .Saved}}<div class="success">Saved.</div>{{end}}
	{{if .Warnings}}
	<div class="warning">
		<strong>Heads up:</strong>
		<ul>
		{{range .Warnings}}<li>{{.}}</li>{{end}}
		</ul>
	</div>
	{{end}}

	{{if .FirstRun}}
	<div class="intro">
		<h2>First time? Only one field needs you</h2>
		<p>Set <strong>Download path</strong> below to where you want your
		course files, press <strong>Save settings</strong>, and you're done
		here. Everything else on this page already has a sensible default and
		can stay untouched &ndash; you can come back and change any of it later,
		once you know whether you care.</p>
	</div>
	{{end}}

	<form method="post" action="/settings" id="settings-form">

	<h2>Browser</h2>

	<p class="hint">
		Login and sync always use Playwright's bundled Chromium against a
		single dedicated profile (<code>~/.opal-downloader/login-profile</code>).
		First time? Log in manually, or
		<a href="/tufast-setup">set up TU-Fast</a> once for automatic 2FA on
		every future login (fewer clicks, and an optional shortcut if TU-Fast
		is already logged in elsewhere on this computer).
	</p>

	<h2>Sync behavior &amp; folders</h2>

	<div class="field">
		<label for="download_path">Download path</label>
		<div class="path-field">
			<input type="text" id="download_path" name="download_path" value="{{.DownloadPath}}">
			<button type="button" class="browse-btn" id="browse-download-path">Browse...</button>
		</div>
		<p class="hint">Local destination folder for downloaded course files.</p>
	</div>

	<div class="field checkbox-row">
		<input type="checkbox" id="sync" name="sync" {{if .Sync}}checked{{end}}>
		<label for="sync">Incremental sync (skip unchanged files)</label>
	</div>

	<div class="field">
		<label for="default_course_folder">Default course folder (optional)</label>
		<div class="path-field">
			<input type="text" id="default_course_folder" name="default_course_folder" value="{{.DefaultCourseFolder}}">
			<button type="button" class="browse-btn" id="browse-default-course-folder">Browse...</button>
		</div>
		<p class="hint">Used when a course below has no folder override. If empty, the course name itself is used.</p>
	</div>

	<div class="field checkbox-row">
		<input type="checkbox" id="sync_all_courses" name="sync_all_courses" {{if .SyncAllCourses}}checked{{end}}>
		<label for="sync_all_courses">Sync all courses</label>
	</div>
	<p class="hint">While this is ticked, every course you are enrolled in gets
	synced and there is nothing to choose. <strong>Untick it to pick specific
	courses</strong> &ndash; "Find my courses" then fetches the courses you are
	actually enrolled in, so you tick the ones you want instead of typing names.
	This is the default on a first run, which is why the course list below is
	hidden until you untick it.</p>

	<div class="field" id="courses-field">
		<label>Courses</label>
		<p class="hint">One row per course to sync, with an optional folder override for that course. Exact name match, case-insensitive.</p>

		<div id="course-picker">
			<button type="button" id="find-courses-btn">Find my courses</button>
			<span id="find-courses-status" class="hint"></span>
			<div id="discovered-courses"></div>
		</div>
		<table class="folders" id="courses-table">
			<thead><tr><th>Course name</th><th>Folder (optional)</th><th></th></tr></thead>
			<tbody>
			{{range $i, $row := .CourseRows}}
				<tr>
					<td><input type="text" name="course_row_name[]" value="{{$row.Name}}" placeholder="Analysis I"></td>
					<td>
						<div class="path-field">
							<input type="text" name="course_row_folder[]" value="{{$row.Folder}}" placeholder="Mathematik/Analysis">
							<button type="button" class="browse-btn">Browse...</button>
						</div>
					</td>
					<td><button type="button" class="remove-row-btn" onclick="this.closest('tr').remove()">Remove</button></td>
				</tr>
			{{end}}
			</tbody>
		</table>
		<button type="button" class="add-row-btn" id="add-course-row">+ Add course</button>
		<button type="button" class="add-row-btn" id="suggest-folders-btn">Suggest folders</button>
		<span id="suggest-folders-status" class="hint"></span>
		<p class="hint">Suggest folders looks through your download path for folders that already match these course names (including abbreviations like <code>AlgData</code>) and fills in the empty ones. Only obvious matches are filled; anything left blank is a guess it wasn't confident enough to make.</p>
	</div>

	<h2>Subfolder organization</h2>

	<div class="field checkbox-row">
		<input type="checkbox" id="use_section_subfolders" name="use_section_subfolders" {{if .UseSectionSubfolders}}checked{{end}}>
		<label for="use_section_subfolders">Organize downloads into a subfolder per OPAL section</label>
	</div>
	<p class="hint">Places files in <code>&lt;course&gt;/&lt;section&gt;/&lt;file&gt;</code> instead of flat <code>&lt;course&gt;/&lt;file&gt;</code>. The two editors below only apply while this is checked. Changing this after you have already synced moves your existing downloads into the new layout on the next sync - nothing is deleted, and anything that can't be matched is listed in the sync log.</p>

	<div class="field">
		<label>Section folder names</label>
		<p class="hint">Rename an OPAL section (e.g. <code>*Exercises*</code>) to a custom folder name (e.g. <code>Übungen</code>). Unmatched sections keep OPAL's own name.</p>
		<table class="folders" id="section-folders-table">
			<thead><tr><th>OPAL section pattern</th><th>Local folder name</th><th></th></tr></thead>
			<tbody>
			{{range $i, $row := .SectionFolderNames}}
				<tr>
					<td><input type="text" name="section_folder_pattern[]" value="{{$row.Pattern}}" placeholder="Exercises"></td>
					<td><input type="text" name="section_folder_folder[]" value="{{$row.Folder}}" placeholder="Übungen"></td>
					<td><button type="button" class="remove-row-btn" onclick="this.closest('tr').remove()">Remove</button></td>
				</tr>
			{{end}}
			</tbody>
		</table>
		<button type="button" class="add-row-btn" id="add-section-folder-row">+ Add rule</button>
	</div>

	<div class="field">
		<label>Subfolder destination overrides</label>
		<p class="hint">Sends one course's specific section to a different destination path. Key is <code>&lt;course pattern&gt;/&lt;subfolder pattern&gt;</code>, e.g. <code>*Analysis*/*Vorlesung*</code>.</p>
		<table class="folders" id="subfolder-dest-table">
			<thead><tr><th>Course pattern / subfolder pattern</th><th>Destination path</th><th></th></tr></thead>
			<tbody>
			{{range $i, $row := .SubfolderDestinations}}
				<tr>
					<td><input type="text" name="subfolder_dest_key[]" value="{{$row.Key}}" placeholder="*Analysis*/*Vorlesung*"></td>
					<td>
						<div class="path-field">
							<input type="text" name="subfolder_dest_path[]" value="{{$row.Destination}}" placeholder="D:/Elsewhere/AnalysisSlides">
							<button type="button" class="browse-btn">Browse...</button>
						</div>
					</td>
					<td><button type="button" class="remove-row-btn" onclick="this.closest('tr').remove()">Remove</button></td>
				</tr>
			{{end}}
			</tbody>
		</table>
		<button type="button" class="add-row-btn" id="add-subfolder-dest-row">+ Add rule</button>
	</div>

	<h2>Notifications</h2>

	<div class="field checkbox-row">
		<input type="checkbox" id="notify_on_scheduled_failure" name="notify_on_scheduled_failure" {{if .NotifyOnScheduledFailure}}checked{{end}}>
		<label for="notify_on_scheduled_failure">Notify me if a scheduled sync fails</label>
	</div>
	<p class="hint">
		Shows a native Windows toast notification when a scheduled
		<code>sync --scheduled</code> run's outcome is a failure (not for a
		partial sync with some file errors, and not on success). Separate
		from the "Enable daily automatic sync" toggle below - you can want
		one without the other. Windows only.
	</p>

	<button type="submit" class="save-btn">Save settings</button>
	</form>

	<h2>Automatic sync</h2>
	{{if not .ScheduleSupported}}
	<p class="hint">Scheduled automatic sync is only available on Windows (via Task Scheduler).</p>
	{{else}}
	{{if .ScheduleError}}<div class="error"><strong>Could not update schedule:</strong> {{.ScheduleError}}</div>{{end}}
	{{if .ScheduleSaved}}<div class="success">Schedule updated.</div>{{end}}
	{{if .ScheduleNotice}}<div class="warning"><strong>Daily sync repaired:</strong> {{.ScheduleNotice}}</div>{{end}}
	<form method="post" action="/settings/schedule" id="schedule-form">
		<div class="field checkbox-row">
			<input type="checkbox" id="schedule_enabled" name="schedule_enabled" {{if .ScheduleEnabled}}checked{{end}}>
			<label for="schedule_enabled">Enable daily automatic sync</label>
		</div>
		<p class="hint">
			Off by default. When enabled, opal-downloader registers a Windows
			Task Scheduler task that runs <code>sync --scheduled</code> once a
			day at the time below (catching up automatically if the machine
			was off/asleep at that time), using the dedicated login profile's
			saved session/TU-Fast - no password is stored for the task itself,
			and it only runs while you're logged on to Windows. Requires
			TU-Fast to already be set up in the dedicated profile (see
			<a href="/tufast-setup">TU-Fast setup</a>) - otherwise a scheduled
			run will fail fast instead of waiting for a 2FA click that will
			never come.
		</p>
		<div class="field">
			<label for="schedule_time">Time of day</label>
			<input type="text" id="schedule_time" name="schedule_time" value="{{.ScheduleTime}}" placeholder="06:00" style="width: 6rem;">
			<p class="hint">24-hour HH:MM, e.g. 06:00.</p>
		</div>
		<button type="submit" class="save-btn">Save schedule</button>
	</form>
	{{end}}

	<p class="back"><a href="/">&larr; Back</a></p>

	<script>
		// Preserve scroll position across the form's full-page POST/re-render:
		// without this, a save from e.g. the subfolder-destinations section
		// near the bottom snaps back to the top on reload, and the "Saved."
		// banner appears out of view.
		(function () {
			var STORAGE_KEY = 'opal-settings-scroll';
			document.getElementById('settings-form').addEventListener('submit', function () {
				try { sessionStorage.setItem(STORAGE_KEY, String(window.scrollY)); } catch (e) {}
			});
			var saved = null;
			try { saved = sessionStorage.getItem(STORAGE_KEY); } catch (e) {}
			if (saved !== null) {
				try { sessionStorage.removeItem(STORAGE_KEY); } catch (e) {}
				window.scrollTo(0, parseInt(saved, 10) || 0);
			}
		})();

		var syncAllCheckbox = document.getElementById('sync_all_courses');
		var coursesField = document.getElementById('courses-field');
		function updateCoursesVisibility() {
			coursesField.style.display = syncAllCheckbox.checked ? 'none' : '';
		}
		syncAllCheckbox.addEventListener('change', updateCoursesVisibility);
		updateCoursesVisibility();

		document.getElementById('add-course-row').addEventListener('click', function () {
			var tbody = document.querySelector('#courses-table tbody');
			var tr = document.createElement('tr');
			tr.innerHTML = '<td><input type="text" name="course_row_name[]" placeholder="Analysis I"></td>' +
				'<td><div class="path-field"><input type="text" name="course_row_folder[]" placeholder="Mathematik/Analysis">' +
				'<button type="button" class="browse-btn">Browse...</button></div></td>' +
				'<td><button type="button" class="remove-row-btn" onclick="this.closest(\'tr\').remove()">Remove</button></td>';
			tbody.appendChild(tr);
		});

		// --- course picker -------------------------------------------------
		// Ticking a discovered course adds/removes a row in the same courses
		// table that already existed, rather than replacing it: the table also
		// carries per-course folder overrides, so anything that discarded and
		// rebuilt it would silently throw those away. Manual rows stay fully
		// usable for a course discovery misses.
		function courseNameInputs() {
			return Array.prototype.slice.call(
				document.querySelectorAll('#courses-table input[name="course_row_name[]"]'));
		}
		function findCourseRow(name) {
			var target = name.trim().toLowerCase();
			var match = null;
			courseNameInputs().forEach(function (input) {
				if (input.value.trim().toLowerCase() === target) { match = input.closest('tr'); }
			});
			return match;
		}
		function addCourseRow(name) {
			var tbody = document.querySelector('#courses-table tbody');
			var tr = document.createElement('tr');
			tr.innerHTML = '<td><input type="text" name="course_row_name[]" placeholder="Analysis I"></td>' +
				'<td><div class="path-field"><input type="text" name="course_row_folder[]" placeholder="Mathematik/Analysis">' +
				'<button type="button" class="browse-btn">Browse...</button></div></td>' +
				'<td><button type="button" class="remove-row-btn" onclick="this.closest(\'tr\').remove()">Remove</button></td>';
			tbody.appendChild(tr);
			tr.querySelector('input[name="course_row_name[]"]').value = name;
			return tr;
		}
		function renderDiscovered(names) {
			var box = document.getElementById('discovered-courses');
			box.innerHTML = '';
			if (!names.length) {
				box.textContent = 'No courses found on your OPAL dashboard.';
				return;
			}
			names.forEach(function (name) {
				var id = 'disc-' + Math.random().toString(36).slice(2);
				var row = document.createElement('div');
				row.className = 'checkbox-row';
				var cb = document.createElement('input');
				cb.type = 'checkbox';
				cb.id = id;
				// Pre-tick whatever is already configured, so revisiting the
				// page shows the current selection rather than a blank slate.
				cb.checked = !!findCourseRow(name);
				cb.addEventListener('change', function () {
					var existing = findCourseRow(name);
					if (cb.checked && !existing) { addCourseRow(name); }
					// Only remove a row the user has not customised: dropping a
					// row that carries a folder override would discard their work
					// on a single stray click.
					else if (!cb.checked && existing) {
						var folder = existing.querySelector('input[name="course_row_folder[]"]');
						if (folder && folder.value.trim()) {
							cb.checked = true;
							alert('This course has a folder override. Clear the folder first, or remove the row directly.');
							return;
						}
						existing.remove();
					}
				});
				var label = document.createElement('label');
				label.htmlFor = id;
				label.textContent = name;
				row.appendChild(cb);
				row.appendChild(label);
				box.appendChild(row);
			});
		}
		var findBtn = document.getElementById('find-courses-btn');
		var findStatus = document.getElementById('find-courses-status');
		findBtn.addEventListener('click', function () {
			findBtn.disabled = true;
			findStatus.textContent = ' Reading your OPAL dashboard...';
			fetch('/settings/discover-courses', { method: 'POST' })
				.then(function (r) { return r.json(); })
				.then(function (j) {
					if (j.error) {
						findStatus.textContent = ' ' + j.error;
						return;
					}
					findStatus.textContent = ' Found ' + j.courses.length + ' course(s). Tick the ones you want.';
					renderDiscovered(j.courses);
				})
				.catch(function (err) { findStatus.textContent = ' Lookup failed: ' + err; })
				.finally(function () { findBtn.disabled = false; });
		});

		// --- folder suggestions --------------------------------------------
		// Only ever fills fields that are still empty. A folder the user
		// already typed is authoritative and is sent along as "taken", so no
		// two courses end up pointed at the same folder.
		var suggestBtn = document.getElementById('suggest-folders-btn');
		var suggestStatus = document.getElementById('suggest-folders-status');
		function courseRows() {
			var trs = Array.prototype.slice.call(document.querySelectorAll('#courses-table tbody tr'));
			return trs.map(function (tr) {
				var nameInput = tr.querySelector('input[name="course_row_name[]"]');
				var folderInput = tr.querySelector('input[name="course_row_folder[]"]');
				return {
					name: nameInput ? nameInput.value.trim() : '',
					folder: folderInput ? folderInput.value.trim() : '',
					folderInput: folderInput
				};
			}).filter(function (row) { return row.name !== ''; });
		}
		suggestBtn.addEventListener('click', function () {
			var rows = courseRows();
			if (!rows.length) {
				suggestStatus.textContent = ' Add or find your courses first.';
				return;
			}
			var blank = rows.filter(function (r) { return r.folder === ''; }).length;
			if (!blank) {
				suggestStatus.textContent = ' Every course already has a folder.';
				return;
			}
			suggestBtn.disabled = true;
			suggestStatus.textContent = ' Searching your download folder...';
			fetch('/settings/suggest-folders', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					download_path: document.getElementById('download_path').value,
					default_course_folder: document.getElementById('default_course_folder').value,
					courses: rows.map(function (r) { return { name: r.name, folder: r.folder }; })
				})
			})
				.then(function (r) { return r.json(); })
				.then(function (j) {
					if (j.error) {
						suggestStatus.textContent = ' ' + j.error;
						return;
					}
					var filled = 0;
					rows.forEach(function (r) {
						var proposed = j.suggestions ? j.suggestions[r.name] : '';
						if (!proposed || r.folder !== '' || !r.folderInput) { return; }
						r.folderInput.value = proposed;
						r.folderInput.classList.add('suggested');
						filled++;
					});
					suggestStatus.textContent = filled === 0
						? ' No folder matched closely enough to suggest. Fill them in by hand.'
						: ' Filled in ' + filled + ' of ' + blank + '. Check them, then Save.';
				})
				.catch(function (err) { suggestStatus.textContent = ' Suggestion failed: ' + err; })
				.finally(function () { suggestBtn.disabled = false; });
		});

		// Typing over a suggestion makes it the user's own answer.
		document.addEventListener('input', function (e) {
			if (e.target && e.target.classList && e.target.classList.contains('suggested')) {
				e.target.classList.remove('suggested');
			}
		});

		document.getElementById('add-section-folder-row').addEventListener('click', function () {
			var tbody = document.querySelector('#section-folders-table tbody');
			var tr = document.createElement('tr');
			tr.innerHTML = '<td><input type="text" name="section_folder_pattern[]" placeholder="Exercises"></td>' +
				'<td><input type="text" name="section_folder_folder[]" placeholder="Übungen"></td>' +
				'<td><button type="button" class="remove-row-btn" onclick="this.closest(\'tr\').remove()">Remove</button></td>';
			tbody.appendChild(tr);
		});

		document.getElementById('add-subfolder-dest-row').addEventListener('click', function () {
			var tbody = document.querySelector('#subfolder-dest-table tbody');
			var tr = document.createElement('tr');
			tr.innerHTML = '<td><input type="text" name="subfolder_dest_key[]" placeholder="*Analysis*/*Vorlesung*"></td>' +
				'<td><div class="path-field"><input type="text" name="subfolder_dest_path[]" placeholder="D:/Elsewhere/AnalysisSlides">' +
				'<button type="button" class="browse-btn">Browse...</button></div></td>' +
				'<td><button type="button" class="remove-row-btn" onclick="this.closest(\'tr\').remove()">Remove</button></td>';
			tbody.appendChild(tr);
		});

		function browseInto(inputEl) {
			fetch('/settings/browse-folder', { method: 'POST' })
				.then(function (r) { return r.json(); })
				.then(function (j) {
					if (j.path) { inputEl.value = j.path; }
					else if (j.error) { alert(j.error); }
				})
				.catch(function (err) { alert('Browse failed: ' + err); });
		}

		document.getElementById('browse-download-path').addEventListener('click', function () {
			browseInto(document.getElementById('download_path'));
		});

		document.getElementById('browse-default-course-folder').addEventListener('click', function () {
			browseInto(document.getElementById('default_course_folder'));
		});

		// Event delegation for the "Browse..." buttons inside dynamic table
		// rows (course folder overrides, subfolder destination paths) - this
		// covers rows added later by the "+ Add ..." buttons too, since it's
		// attached to the document rather than the individual buttons.
		document.addEventListener('click', function (e) {
			var btn = e.target.closest ? e.target.closest('.path-field .browse-btn') : null;
			if (!btn) { return; }
			var input = btn.previousElementSibling;
			if (input && input.tagName === 'INPUT') {
				browseInto(input);
			}
		});
	</script>
</body>
</html>
`))
