package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
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
// is an internal implementation detail. OpalURL is always saved/loaded using
// config.DefaultOPALURL; SessionStateFile is always saved using
// config.PerInstallStateFile(configPath), scoped to this config.yaml's own
// directory rather than a single machine-wide path (see that function's doc
// comment for the cross-install identity leak this fixes) - see
// parseSettingsForm. Advanced users who need something different can still
// hand-edit config.yaml directly; the GUI just won't show or round-trip
// those fields.
type settingsViewData struct {
	ConfigPath string
	Error      string
	Saved      bool
	Warnings   []string

	// FirstRun means there is no config.yaml yet, so this page is being seen
	// by someone who has never used the tool. It is a long form - browser,
	// folders, subfolder rules - and almost none of
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
// config.DefaultOPALURL / config.PerInstallStateFile(configPath) - the
// Settings form no longer exposes either for editing (see settingsViewData's
// doc comment).
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
	loaded.Credentials.URL = config.DefaultOPALURL
	loaded.Credentials.StateFile = config.PerInstallStateFile(configPath)

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
			//
			// These come from config.Defaults() rather than a struct built
			// here, because this page's Save writes every field it renders:
			// any default missing from the struct is silently saved as its
			// zero value. Found 2026-08-11 walking a fresh install of
			// opal-downloader-setup.exe - the hand-built version listed
			// three of the defaults and so wrote skip_enrollment_sections:
			// false on the first save, turning off a live-confirmed skip
			// for every new user.
			view := loadedToViewData(configPath, config.Defaults())
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
			// defaults - NOT to config.Loaded{}, which is what this did
			// until 2026-08-11. Save writes every field of base that the
			// form does not override, so a zero-valued base persisted
			// skip_enrollment_sections: false on a new user's first save
			// (an explicit false, which config.Load then honours over
			// DefaultSkipEnrollmentSections). Defaults() is what a user who
			// never opened this page would be running under, so it is the
			// only correct thing to start from when there is nothing to
			// preserve. Save's own validation still surfaces real problems.
			base, err := config.Load(configPath)
			if err != nil {
				base = config.Defaults()
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
