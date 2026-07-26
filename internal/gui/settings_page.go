package gui

import (
	"html/template"
	"strconv"
)

// The settings page's markup and its client-side behaviour.
//
// Split out of settings.go, which was a thousand lines of two unrelated jobs:
// reading and writing config.yaml, and rendering a long HTML form. Neither
// half is improved by sharing a file with the other, and the maintainer asked
// for the growth to be kept in check (2026-07-26: "code einkuerzen: sonst wird
// es immer mehr bloated").
//
// A pure move - the template's bytes are unchanged. settings.go keeps the
// handlers, the form parsing, and the view-data types; this file is what the
// user sees.

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
	/* color is not decoration here: pageStyle's base button rule sets
	   color:#fff for the blue primary buttons, and a class selector overriding
	   only the background leaves white text on a near-white fill. */
	.browse-btn { padding: 0.4rem 0.7rem; border-radius: 4px; border: 1px solid #888; background: #f5f5f5; color: #1a1a1a; cursor: pointer; font: inherit; white-space: nowrap; }
	table.folders { width: 100%; border-collapse: collapse; margin-bottom: 0.5rem; }
	table.folders th { text-align: left; font-size: 0.8rem; color: #666; font-weight: 600; padding-bottom: 0.25rem; }
	table.folders td { padding: 0.2rem 0.4rem 0.2rem 0; }
	table.folders td:first-child { padding-left: 0; }
	.remove-row-btn { background: none; border: 1px solid #ccc; border-radius: 4px; cursor: pointer; color: #a00; padding: 0.3rem 0.6rem; }
	.add-row-btn, .save-btn { padding: 0.5rem 1rem; border-radius: 4px; border: 1px solid #888; background: #f5f5f5; color: #1a1a1a; cursor: pointer; font: inherit; }
	/* A proposed folder is marked until the form is saved or the field is
	   edited, so a filled-in path is never mistaken for one the user chose. */
	input.suggested { background: #eef7ee; border-color: #4c9a4c; }
	.course-actions { display: flex; gap: 0.4rem; flex-wrap: wrap; align-items: center; }
	/* An unticked course stays on the list rather than vanishing, so it can be
	   ticked again - but it has to be obvious that it will not be synced. */
	tr.off input[type=text] { color: #999; background: #fafafa; }
	#find-courses-status { font-style: italic; }
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
		<label>Your courses</label>
		<p class="hint" id="courses-intro">Tick the ones you want synced. The list is
		read from your OPAL dashboard, so you should not have to type anything
		&ndash; but you can add a course by hand if one is missing.
		<span id="find-courses-status"></span></p>

		<table class="folders" id="courses-table">
			<thead><tr><th style="width: 1.5rem;"></th><th>Course</th><th>Folder (optional)</th><th></th></tr></thead>
			<tbody>
			{{range $i, $row := .CourseRows}}
				<tr>
					<td><input type="checkbox" class="course-on" checked title="Sync this course"></td>
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

		<div class="course-actions">
			<button type="button" class="add-row-btn" id="find-courses-btn">Refresh this list from OPAL</button>
			<button type="button" class="add-row-btn" id="add-course-row">Add one by hand</button>
			<button type="button" class="add-row-btn" id="suggest-folders-btn">Fill in folders for me</button>
			<span id="suggest-folders-status" class="hint"></span>
		</div>
		<p class="hint">Leave a folder blank and the course name is used. "Fill in
		folders for me" looks through your download path for folders that already
		match these course names (including abbreviations like <code>AlgData</code>)
		and fills in the blank ones &ndash; only where the match is obvious, so
		anything still blank afterwards is a guess it was not confident enough to
		make.</p>
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

	<button type="submit" class="save-btn">Save settings</button>
	</form>

	<h2>Running it automatically</h2>
	<p class="hint">Whether opal-downloader syncs on its own once a day, and
	what happens if one of those runs fails, is on its own page:
	<a href="/schedule">Automatic sync</a>.</p>

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

		// --- the course list -----------------------------------------------
		// One list, not two. It used to be a set of "discovered" checkboxes in
		// one box and a table of configured rows in another, with the user
		// expected to join them up mentally - which is what the maintainer
		// meant by "es gibt so mehrere stellen und so weiter.. fuehlt sich
		// weird an" (2026-07-26). Every course now appears exactly once, with
		// its tickbox and its folder on the same line.
		//
		// Unticking no longer deletes the row. The old version did, which meant
		// it had to refuse and pop an alert() when the row carried a folder
		// override, to avoid throwing that work away on a stray click. Keeping
		// the row and greying it out removes both the deletion and the alert:
		// unticked rows are simply dropped at submit time, and until then the
		// decision is reversible.
		var coursesTable = document.getElementById('courses-table');
		var coursesBody = coursesTable.querySelector('tbody');
		var findStatus = document.getElementById('find-courses-status');

		function courseRowElements() {
			return Array.prototype.slice.call(coursesBody.querySelectorAll('tr'));
		}

		function rowName(tr) {
			var input = tr.querySelector('input[name="course_row_name[]"]');
			return input ? input.value.trim() : '';
		}

		function findCourseRow(name) {
			var target = name.trim().toLowerCase();
			var match = null;
			courseRowElements().forEach(function (tr) {
				if (rowName(tr).toLowerCase() === target) { match = tr; }
			});
			return match;
		}

		function markRowState(tr) {
			var cb = tr.querySelector('.course-on');
			tr.classList.toggle('off', !(cb && cb.checked));
		}

		function addCourseRow(name, selected) {
			var tr = document.createElement('tr');
			tr.innerHTML = '<td><input type="checkbox" class="course-on" title="Sync this course"></td>' +
				'<td><input type="text" name="course_row_name[]" placeholder="Analysis I"></td>' +
				'<td><div class="path-field"><input type="text" name="course_row_folder[]" placeholder="Mathematik/Analysis">' +
				'<button type="button" class="browse-btn">Browse...</button></div></td>' +
				'<td><button type="button" class="remove-row-btn" onclick="this.closest(\'tr\').remove()">Remove</button></td>';
			coursesBody.appendChild(tr);
			tr.querySelector('input[name="course_row_name[]"]').value = name || '';
			tr.querySelector('.course-on').checked = !!selected;
			markRowState(tr);
			return tr;
		}

		coursesTable.addEventListener('change', function (e) {
			if (e.target && e.target.classList && e.target.classList.contains('course-on')) {
				markRowState(e.target.closest('tr'));
			}
		});
		courseRowElements().forEach(markRowState);

		// Only ticked courses are saved. Done by removing the others just
		// before the browser serializes the form, so the wire format stays
		// exactly what the server already parses - the alternative, a third
		// "selected" field per row, would mean teaching parseSettingsForm a new
		// shape for no gain.
		document.getElementById('settings-form').addEventListener('submit', function () {
			courseRowElements().forEach(function (tr) {
				var cb = tr.querySelector('.course-on');
				if (!cb || !cb.checked) { tr.remove(); }
			});
		});

		document.getElementById('add-course-row').addEventListener('click', function () {
			var tr = addCourseRow('', true);
			tr.querySelector('input[name="course_row_name[]"]').focus();
		});

		// mergeDiscovered adds what OPAL reports without disturbing what is
		// already there. A configured course that the dashboard did not return
		// keeps its row and its tick: it may be an enrolment that has ended, or
		// a name that no longer matches, and quietly dropping it from the list
		// would look like the tool had forgotten a choice the user made.
		function mergeDiscovered(names) {
			var added = 0;
			names.forEach(function (name) {
				if (findCourseRow(name)) { return; }
				addCourseRow(name, false);
				added++;
			});
			return added;
		}

		var findBtn = document.getElementById('find-courses-btn');
		var lookupDone = false;

		function lookupCourses(auto) {
			if (findBtn.disabled) { return; }
			findBtn.disabled = true;
			lookupDone = true;
			findStatus.textContent = ' Reading your OPAL dashboard...';
			fetch('/settings/discover-courses', { method: 'POST' })
				.then(function (r) { return r.json(); })
				.then(function (j) {
					if (j.error) {
						// On an automatic lookup this is routine rather than a
						// fault - a first run has usually not logged in yet -
						// so it says what to do instead of reading as a failure.
						findStatus.textContent = auto
							? ' Could not read your courses yet (log in first, then use "Refresh this list from OPAL"). You can also add them by hand.'
							: ' ' + j.error;
						return;
					}
					var added = mergeDiscovered(j.courses || []);
					if (!j.courses || !j.courses.length) {
						findStatus.textContent = ' No courses found on your OPAL dashboard.';
					} else if (added === 0) {
						findStatus.textContent = ' Your list is up to date with OPAL (' + j.courses.length + ' course(s)).';
					} else {
						findStatus.textContent = ' Added ' + added + ' course(s) from OPAL. Tick the ones you want.';
					}
				})
				.catch(function (err) { findStatus.textContent = ' Lookup failed: ' + err; })
				.finally(function () { findBtn.disabled = false; });
		}

		findBtn.addEventListener('click', function () { lookupCourses(false); });

		// --- folder suggestions --------------------------------------------
		// Only ever fills fields that are still empty. A folder the user
		// already typed is authoritative and is sent along as "taken", so no
		// two courses end up pointed at the same folder.
		var suggestBtn = document.getElementById('suggest-folders-btn');
		var suggestStatus = document.getElementById('suggest-folders-status');
		// Only ticked courses: suggesting a folder for a course that will not
		// be synced spends the user's attention on a row that is about to be
		// dropped at save time.
		function courseRows() {
			var trs = courseRowElements().filter(function (tr) {
				var cb = tr.querySelector('.course-on');
				return cb && cb.checked;
			});
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

		// Registered here, after lookupCourses and its state exist. Choosing to
		// pick specific courses is the moment someone wants to see their
		// courses, so they are fetched then rather than after hunting for a
		// button. Only when there is nothing to show: a list that is already
		// populated must not move under them.
		syncAllCheckbox.addEventListener('change', function () {
			if (!syncAllCheckbox.checked && !lookupDone && courseRowElements().length === 0) {
				lookupCourses(true);
			}
		});

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
	` + unsavedChangesGuard + `
</body>
</html>
`))
