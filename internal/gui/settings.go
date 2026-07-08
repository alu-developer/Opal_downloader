package gui

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/config"
)

// courseFolderRow is one editable row of the course_folders map, rendered as
// a pattern/folder pair. Rows are indexed so the form can add/remove them.
type courseFolderRow struct {
	Pattern string
	Folder  string
}

// settingsViewData is passed to the settings template.
type settingsViewData struct {
	ConfigPath string
	Error      string
	Saved      bool

	OpalURL            string
	SessionStateFile   string
	BrowserExecutable  string
	BrowserUserDataDir string
	BrowserProfileDir  string

	DownloadPath        string
	Courses             string // newline-separated for the textarea
	Sync                bool
	DefaultCourseFolder string
	CourseFolders       []courseFolderRow
}

func loadedToViewData(configPath string, loaded config.Loaded) settingsViewData {
	rows := make([]courseFolderRow, 0, len(loaded.App.CourseFolders))
	for pattern, folder := range loaded.App.CourseFolders {
		rows = append(rows, courseFolderRow{Pattern: pattern, Folder: folder})
	}

	return settingsViewData{
		ConfigPath: configPath,

		OpalURL:            loaded.Credentials.URL,
		SessionStateFile:   loaded.Credentials.StateFile,
		BrowserExecutable:  loaded.Credentials.BrowserExecutable,
		BrowserUserDataDir: loaded.Credentials.BrowserUserDataDir,
		BrowserProfileDir:  loaded.Credentials.BrowserProfileDir,

		DownloadPath:        loaded.App.DownloadPath,
		Courses:             strings.Join(loaded.App.Courses, "\n"),
		Sync:                loaded.App.Sync,
		DefaultCourseFolder: loaded.App.DefaultCourseFolder,
		CourseFolders:       rows,
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
// on top of base; everything else - notably UseSectionSubfolders,
// SectionFolderNames, SubfolderDestinations, DownloadConcurrency, and
// CourseConcurrency, none of which this form has inputs for - is carried
// through unchanged so Save doesn't silently wipe hand-edited config.yaml
// fields the GUI doesn't yet know how to edit.
func parseSettingsForm(r *http.Request, configPath string, base config.Loaded) (settingsViewData, config.Loaded) {
	_ = r.ParseForm()

	get := func(name string) string {
		return strings.TrimSpace(r.FormValue(name))
	}

	var courses []string
	for _, line := range strings.Split(r.FormValue("courses"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			courses = append(courses, trimmed)
		}
	}

	patterns := r.PostForm["course_folder_pattern[]"]
	folders := r.PostForm["course_folder_folder[]"]
	courseFolders := map[string]string{}
	rows := make([]courseFolderRow, 0, len(patterns))
	for i := range patterns {
		pattern := strings.TrimSpace(patterns[i])
		folder := ""
		if i < len(folders) {
			folder = strings.TrimSpace(folders[i])
		}
		rows = append(rows, courseFolderRow{Pattern: pattern, Folder: folder})
		if pattern == "" && folder == "" {
			continue
		}
		courseFolders[pattern] = folder
	}

	syncEnabled := r.FormValue("sync") == "on"

	view := settingsViewData{
		ConfigPath: configPath,

		OpalURL:            get("opal_url"),
		SessionStateFile:   get("session_state_file"),
		BrowserExecutable:  get("browser_executable"),
		BrowserUserDataDir: get("browser_user_data_dir"),
		BrowserProfileDir:  get("browser_profile_directory"),

		DownloadPath:        get("download_path"),
		Courses:             r.FormValue("courses"),
		Sync:                syncEnabled,
		DefaultCourseFolder: get("default_course_folder"),
		CourseFolders:       rows,
	}

	loaded := base
	loaded.App.DownloadPath = view.DownloadPath
	loaded.App.Courses = courses
	loaded.App.Sync = syncEnabled
	loaded.App.DefaultCourseFolder = view.DefaultCourseFolder
	loaded.App.CourseFolders = courseFolders
	loaded.Credentials.URL = view.OpalURL
	loaded.Credentials.StateFile = view.SessionStateFile
	loaded.Credentials.BrowserExecutable = view.BrowserExecutable
	loaded.Credentials.BrowserUserDataDir = view.BrowserUserDataDir
	loaded.Credentials.BrowserProfileDir = view.BrowserProfileDir

	return view, loaded
}

func handleSettings(configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/settings" {
			http.NotFound(w, r)
			return
		}

		switch r.Method {
		case http.MethodGet:
			loaded, err := config.Load(configPath)
			if err != nil {
				if strings.Contains(err.Error(), "config file not found") {
					// No config.yaml yet: show the form pre-filled with
					// defaults so the settings page still works as the
					// primary first-run configuration path.
					renderSettings(w, loadedToViewData(configPath, config.Loaded{
						App: config.App{
							DownloadPath: "./downloads",
							Courses:      []string{"*"},
							Sync:         true,
						},
						Credentials: config.Credentials{
							URL:       config.DefaultOPALURL,
							StateFile: config.DefaultStateFile,
						},
					}))
					return
				}
				renderSettings(w, settingsViewData{
					ConfigPath: configPath,
					Error:      fmt.Sprintf("Could not load %s: %v", configPath, err),
				})
				return
			}
			renderSettings(w, loadedToViewData(configPath, loaded))

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
				renderSettings(w, view)
				return
			}
			view.Saved = true
			renderSettings(w, view)

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

var settingsTemplateFuncs = template.FuncMap{
	"checked": func(b bool) string {
		if b {
			return "checked"
		}
		return ""
	},
	"add1": func(i int) int { return i + 1 },
	"itoa": strconv.Itoa,
}

var settingsTemplate = template.Must(template.New("settings").Funcs(settingsTemplateFuncs).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Opal Downloader - Settings</title>
<style>
	body { font-family: system-ui, sans-serif; max-width: 44rem; margin: 3rem auto; padding: 0 1rem; color: #1a1a1a; }
	h1 { margin-bottom: 0.25rem; }
	h2 { margin-top: 2.5rem; border-bottom: 1px solid #ddd; padding-bottom: 0.25rem; }
	.back { font-size: 0.9rem; }
	.hint { color: #666; font-size: 0.85rem; margin: 0.15rem 0 0.6rem; }
	.field { margin-bottom: 1.1rem; }
	label { display: block; font-weight: 600; margin-bottom: 0.25rem; }
	input[type=text], input[type=url], textarea {
		width: 100%; box-sizing: border-box; padding: 0.4rem 0.5rem;
		border: 1px solid #ccc; border-radius: 4px; font: inherit;
	}
	textarea { min-height: 4.5rem; font-family: ui-monospace, monospace; }
	.checkbox-row { display: flex; align-items: center; gap: 0.5rem; }
	.checkbox-row label { margin: 0; font-weight: 600; }
	.error { background: #fdecea; border: 1px solid #d93025; color: #7a1712; border-radius: 6px; padding: 0.75rem 1rem; margin: 1.25rem 0; }
	.success { background: #e6f4ea; border: 1px solid #34a853; color: #1e4620; border-radius: 6px; padding: 0.75rem 1rem; margin: 1.25rem 0; }
	table.folders { width: 100%; border-collapse: collapse; margin-bottom: 0.5rem; }
	table.folders th { text-align: left; font-size: 0.8rem; color: #666; font-weight: 600; padding-bottom: 0.25rem; }
	table.folders td { padding: 0.2rem 0.4rem 0.2rem 0; }
	table.folders td:first-child { padding-left: 0; }
	.remove-row-btn { background: none; border: 1px solid #ccc; border-radius: 4px; cursor: pointer; color: #a00; padding: 0.3rem 0.6rem; }
	.add-row-btn, .save-btn { padding: 0.5rem 1rem; border-radius: 4px; border: 1px solid #888; background: #f5f5f5; cursor: pointer; font: inherit; }
	.save-btn { background: #1a73e8; color: #fff; border-color: #1a73e8; margin-top: 1.5rem; font-weight: 600; }
	code { background: #f0f0f0; padding: 0.1rem 0.3rem; border-radius: 3px; }
</style>
</head>
<body>
	<p class="back"><a href="/">&larr; back</a></p>
	<h1>Settings</h1>
	<p class="hint">Editing <code>{{.ConfigPath}}</code>. Saving validates the form and writes this file directly (a <code>.bak</code> copy of the previous version is kept).</p>

	{{if .Error}}<div class="error"><strong>Could not save:</strong> {{.Error}}</div>{{end}}
	{{if .Saved}}<div class="success">Saved. Config written to {{.ConfigPath}} (previous version backed up to {{.ConfigPath}}.bak).</div>{{end}}

	<form method="post" action="/settings" id="settings-form">

	<h2>Connection &amp; browser</h2>

	<div class="field">
		<label for="opal_url">OPAL URL</label>
		<input type="text" id="opal_url" name="opal_url" value="{{.OpalURL}}">
		<p class="hint">Base URL of your Bildungsportal Sachsen OPAL instance.</p>
	</div>

	<div class="field">
		<label for="session_state_file">Session state file</label>
		<input type="text" id="session_state_file" name="session_state_file" value="{{.SessionStateFile}}">
		<p class="hint">Where the browser session (login cookies) is persisted between runs.</p>
	</div>

	<div class="field">
		<label for="browser_executable">Browser executable (optional)</label>
		<input type="text" id="browser_executable" name="browser_executable" value="{{.BrowserExecutable}}">
		<p class="hint">e.g. path to Brave, if TU-Fast is only installed there. Leave empty to use Playwright's bundled browser.</p>
	</div>

	<div class="field">
		<label for="browser_user_data_dir">Browser user data directory (optional)</label>
		<input type="text" id="browser_user_data_dir" name="browser_user_data_dir" value="{{.BrowserUserDataDir}}">
		<p class="hint">Your real browser's profile root. Never opened directly - a private working copy is made on first use.</p>
	</div>

	<div class="field">
		<label for="browser_profile_directory">Browser profile directory (optional)</label>
		<input type="text" id="browser_profile_directory" name="browser_profile_directory" value="{{.BrowserProfileDir}}">
		<p class="hint">e.g. <code>Default</code> or <code>Profile 1</code>, if not using the default profile.</p>
	</div>

	<h2>Sync behavior &amp; folders</h2>

	<div class="field">
		<label for="download_path">Download path</label>
		<input type="text" id="download_path" name="download_path" value="{{.DownloadPath}}">
		<p class="hint">Local destination folder for downloaded course files.</p>
	</div>

	<div class="field">
		<label for="courses">Courses (one per line)</label>
		<textarea id="courses" name="courses">{{.Courses}}</textarea>
		<p class="hint">Exact course names to sync, case-insensitive. Use <code>*</code> alone to match every course.</p>
	</div>

	<div class="field checkbox-row">
		<input type="checkbox" id="sync" name="sync" {{checked .Sync}}>
		<label for="sync">Incremental sync (skip unchanged files)</label>
	</div>

	<div class="field">
		<label for="default_course_folder">Default course folder (optional)</label>
		<input type="text" id="default_course_folder" name="default_course_folder" value="{{.DefaultCourseFolder}}">
		<p class="hint">Used when no course-folder rule below matches. If empty, the course name itself is used.</p>
	</div>

	<div class="field">
		<label>Course folder rules</label>
		<p class="hint">Maps a course-name pattern (glob, e.g. <code>*Analysis*</code>) to a target folder. First match wins.</p>
		<table class="folders" id="folders-table">
			<thead><tr><th>Pattern</th><th>Folder</th><th></th></tr></thead>
			<tbody>
			{{range $i, $row := .CourseFolders}}
				<tr>
					<td><input type="text" name="course_folder_pattern[]" value="{{$row.Pattern}}" placeholder="*Analysis*"></td>
					<td><input type="text" name="course_folder_folder[]" value="{{$row.Folder}}" placeholder="Mathematik/Analysis"></td>
					<td><button type="button" class="remove-row-btn" onclick="this.closest('tr').remove()">Remove</button></td>
				</tr>
			{{end}}
			</tbody>
		</table>
		<button type="button" class="add-row-btn" id="add-folder-row">+ Add rule</button>
	</div>

	<button type="submit" class="save-btn">Save settings</button>
	</form>

	<script>
		document.getElementById('add-folder-row').addEventListener('click', function () {
			var tbody = document.querySelector('#folders-table tbody');
			var tr = document.createElement('tr');
			tr.innerHTML = '<td><input type="text" name="course_folder_pattern[]" placeholder="*Analysis*"></td>' +
				'<td><input type="text" name="course_folder_folder[]" placeholder="Mathematik/Analysis"></td>' +
				'<td><button type="button" class="remove-row-btn" onclick="this.closest(\'tr\').remove()">Remove</button></td>';
			tbody.appendChild(tr);
		});
	</script>
</body>
</html>
`))
