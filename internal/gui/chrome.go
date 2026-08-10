package gui

import (
	"io"
	"net/http"
)

// Page chrome: the pieces spliced into every GUI page rather than belonging to
// any one of them - the shared stylesheet, the favicon and app mark, the
// scheduled-sync failure banner, and the unsaved-changes guard.
//
// Split out of gui.go, which had grown to over a thousand lines by being the
// home for anything that did not obviously live elsewhere. The maintainer
// flagged the growth directly (2026-07-26: "code einkuerzen: sonst wird es
// immer mehr bloated"). This is a pure move - no behaviour changed - and it
// leaves gui.go about the server and the landing page, which is what its name
// suggests.
//
// These are constants spliced into template strings rather than a layout
// template every page inherits, which is the pattern this package has used
// from the start: each handler executes its own template with its own data
// type, and there is no shared page-data struct to hang a layout off. Changing
// that is a bigger job than this file.

// bannerChrome is the scheduled-sync failure banner: spliced into every
// page template right after <body> (see landingTemplate, syncTemplate,
// settingsTemplate, tuFastSetupTemplate, feedbackPageTemplate/
// feedbackOpenedTemplate, updatePageTemplate/updateStartingTemplate/
// updateErrorTemplate, panicPageTemplate), the same "shared constant
// spliced into every page" pattern this file already uses for pageStyle
// above - see docs/scheduled-sync-plan.md section 3.
//
// Deliberately implemented client-side (fetch + DOM, no server-rendered
// template data) rather than threading a Banner field through every
// handler's own page-data struct: this project has no shared "page chrome"
// render path today (every handler calls its own template.Execute with its
// own data type - see gui.go's Run/handleLanding, settings.go's
// renderSettings, sync.go's handlePage, etc.), so a client-side snippet
// that fetches its own small JSON endpoint (handleScheduledStatus below)
// is the smallest change that still puts the banner on every page without
// restructuring every handler's data type.
//
// Degrades silently by construction: handleScheduledStatus always responds
// 200 with a JSON body (see its doc comment), and this script's own
// .catch(function(){}) swallows any fetch failure - so a missing/corrupt
// status file, or the endpoint being unreachable for any reason, simply
// never shows a banner, never touches the DOM, and never surfaces an error
// to the user (per this task's "GUI must degrade silently" acceptance
// criterion).
//
// Dismissal is recorded on the server (POST /scheduled-status/dismiss,
// which writes the dismissed run's timestamp to a file next to the status
// file - see internal/statuslog's dismissFileName), and enforced there too:
// handleScheduledStatus answers `null` for a status that has been
// dismissed, so a dismissed banner never even reaches this script.
//
// It used to be tracked in the browser's localStorage, which did not
// survive closing the window: the GUI binds an ephemeral port
// (gui.Options.Port defaults to 0), so every launch is a different origin
// and gets a different, empty localStorage. That is the "dismiss it and it
// is back next time you open it" bug the maintainer reported (2026-08-10).
//
// Keyed by the status's own timestamp on both sides, so dismissing today's
// failure hides today's failure only - the next scheduled run writes a new
// timestamp and, if it fails too, that is news and shows again.
const bannerChrome = `<div id="scheduled-sync-banner" style="display:none;"></div>
	<script>
	(function () {
		function render(status) {
			if (!status || status.outcome === 'success') { return; }
			var el = document.getElementById('scheduled-sync-banner');
			if (!el) { return; }
			el.textContent = '';
			el.className = status.outcome === 'failure' ? 'error' : 'warning';

			var label = status.outcome === 'failure' ? 'failed' : 'had problems';
			var dateStr = status.timestamp;
			var when = new Date(status.timestamp);
			var ageDays = -1;
			try {
				dateStr = when.toLocaleString();
				ageDays = Math.floor((Date.now() - when.getTime()) / 86400000);
			} catch (e) {}

			// A scheduled run only ever overwrites this file by running, so an
			// old timestamp means nothing has run since - which is a different
			// and more urgent problem than the failure being reported, and the
			// one the user has to fix first. Without this the banner reads as
			// news about this morning no matter how long it has been stuck.
			var staleness = '';
			if (ageDays >= 2) {
				staleness = ' Nothing has run in the ' + ageDays + ' days since, so the daily sync is not working at all - check Automatic sync.';
			}

			// Split the plain-language part of the message from the
			// technical cause internal/netcheck appends for logs and bug
			// reports. The banner is read by someone who wants to know what
			// to do about it, and a Playwright/DNS error dragged across two
			// lines is exactly the "not meaningful for normal users"
			// complaint this pass is answering - but throwing the cause away
			// would make a real fault undiagnosable, so it moves into a
			// fold-out instead of disappearing.
			var message = String(status.message || '').replace(/\s+$/, '');
			var detail = '';
			var cut = message.indexOf('(technical detail: ');
			if (cut > -1) {
				detail = message.slice(cut);
				message = message.slice(0, cut).replace(/\s+$/, '');
			}
			// Only add the full stop the sentence needs if the message did
			// not bring its own: messages used to be error-string fragments
			// with no punctuation, while the plain-language ones are whole
			// sentences, and appending to those produced "...try again..".
			if (!/[.!?]$/.test(message)) { message += '.'; }

			var text = document.createElement('span');
			text.textContent = 'Last scheduled sync (' + dateStr + ') ' + label + ': ' + message + staleness + ' ';
			el.appendChild(text);

			if (detail) {
				var details = document.createElement('details');
				details.style.display = 'inline-block';
				details.style.margin = '0 0.5rem';
				var summary = document.createElement('summary');
				summary.textContent = 'Details';
				summary.style.cursor = 'pointer';
				summary.style.display = 'inline';
				details.appendChild(summary);
				var detailText = document.createElement('div');
				detailText.textContent = detail;
				detailText.style.fontSize = '0.85em';
				detailText.style.marginTop = '0.35rem';
				details.appendChild(detailText);
				el.appendChild(details);
			}

			var link = document.createElement('a');
			link.href = '/sync?autostart=1';
			link.textContent = 'Run now';
			el.appendChild(link);

			el.appendChild(document.createTextNode(' '));

			var dismiss = document.createElement('button');
			dismiss.type = 'button';
			dismiss.textContent = 'Dismiss';
			dismiss.style.marginLeft = '0.75rem';
			dismiss.addEventListener('click', function () {
				// Hide first, save second: the click must feel instant, and a
				// failed save only costs the banner coming back next launch -
				// which is what it did every time before this endpoint existed.
				el.style.display = 'none';
				fetch('/scheduled-status/dismiss', { method: 'POST' }).catch(function () {});
			});
			el.appendChild(dismiss);

			el.style.display = 'block';
		}
		fetch('/scheduled-status').then(function (r) { return r.ok ? r.json() : null; }).then(render).catch(function () {});
	})();
	</script>`

// unsavedChangesGuard warns before edits are thrown away. Spliced into any
// page template whose forms hold user data (settings.go, schedule_page.go),
// the same "shared constant spliced into every page" pattern as pageStyle,
// faviconLink and bannerChrome above.
//
// The problem it solves, reported by the maintainer (2026-07-26): change a
// field, navigate away, and the edit is gone with nothing said. Saving is a
// separate deliberate click, and the pages carry enough fields that "did I
// already save?" is not answerable by looking.
//
// Three layers, because no single one covers the ways out of a page:
//
//  1. A persistent bar while anything is unsaved. This is the layer that
//     actually helps - it removes the need to remember, rather than
//     interrupting at the moment of leaving. The other two are backstops.
//  2. A confirm() on in-page links, which is how the user navigates in the
//     real window (WebView2, no address bar and no back button).
//  3. beforeunload, for closing the window or a reload.
//
// Dirtiness is measured against a snapshot taken on load, not set by the
// first keystroke: typing a character and deleting it again leaves the form
// clean, and rows added and then removed do not linger as a false warning.
// The snapshot is re-taken after a save, because the page re-renders.
//
// Forms are found at runtime rather than named, so a page that grows another
// form is covered without anyone remembering to come back here.
const unsavedChangesGuard = `<div id="unsaved-bar" role="status" style="display:none;"></div>
	<style>
	#unsaved-bar {
		position: fixed; left: 0; right: 0; bottom: 0; z-index: 50;
		background: #fff6d8; border-top: 1px solid #e0c04a; color: #4a3a00;
		padding: 0.6rem 1rem; font-size: 0.95rem;
		display: flex; gap: 0.75rem; align-items: center; justify-content: center;
	}
	#unsaved-bar a { color: #4a3a00; }
	/* Keep the bar from covering the last control on the page. */
	body.has-unsaved { padding-bottom: 3.5rem; }
	</style>
	<script>
	(function () {
		var forms = Array.prototype.slice.call(document.querySelectorAll('form'));
		if (!forms.length) { return; }

		var bar = document.getElementById('unsaved-bar');
		var saving = false;
		var snapshots = new WeakMap();

		function stateOf(form) {
			try {
				return new URLSearchParams(new FormData(form)).toString();
			} catch (e) {
				// If a browser will not serialize the form, the guard cannot
				// tell dirty from clean. Reporting "clean" is the honest
				// answer: a bar that is always on teaches people to ignore it.
				return null;
			}
		}

		function snapshot() {
			forms.forEach(function (form) { snapshots.set(form, stateOf(form)); });
		}

		function dirtyForms() {
			return forms.filter(function (form) {
				var before = snapshots.get(form);
				var now = stateOf(form);
				return before !== null && now !== null && before !== now;
			});
		}

		function saveButtonFor(form) {
			return form.querySelector('button[type=submit], input[type=submit]');
		}

		function refresh() {
			var dirty = dirtyForms();
			document.body.classList.toggle('has-unsaved', dirty.length > 0);
			if (!dirty.length) { bar.style.display = 'none'; return; }

			bar.textContent = '';
			var text = document.createElement('span');
			text.textContent = 'You have unsaved changes.';
			bar.appendChild(text);

			// Point at the button rather than submitting from the bar: with
			// more than one form on a page, a bar that saves would have to
			// guess which, and guessing wrong writes the wrong thing.
			var target = saveButtonFor(dirty[0]);
			if (target) {
				var link = document.createElement('a');
				link.href = '#';
				link.textContent = 'Take me to ' + (target.textContent || 'Save').trim();
				link.addEventListener('click', function (ev) {
					ev.preventDefault();
					target.scrollIntoView({ block: 'center' });
					target.focus({ preventScroll: true });
				});
				bar.appendChild(link);
			}
			bar.style.display = 'flex';
		}

		document.addEventListener('input', refresh);
		document.addEventListener('change', refresh);
		// Those two events miss every change this page makes in JavaScript:
		// rows added by "+ Add course", rows removed by "Remove", and folder
		// paths written by "Suggest folders" and "Browse..." - assigning
		// .value fires nothing, and a MutationObserver would not see it
		// either, because it is a property, not an attribute. Re-checking on
		// a timer catches all of them for the cost of serializing two small
		// forms once a second.
		setInterval(refresh, 1000);

		forms.forEach(function (form) {
			form.addEventListener('submit', function () {
				saving = true;
				document.body.classList.remove('has-unsaved');
			});
		});

		document.addEventListener('click', function (ev) {
			var link = ev.target.closest ? ev.target.closest('a[href]') : null;
			if (!link || saving || link.getAttribute('href').charAt(0) === '#') { return; }
			if (link.target === '_blank') { return; }
			if (!dirtyForms().length) { return; }
			if (!window.confirm('You have unsaved changes on this page. Leave without saving?')) {
				ev.preventDefault();
			}
		}, true);

		window.addEventListener('beforeunload', function (ev) {
			if (saving || !dirtyForms().length) { return; }
			ev.preventDefault();
			// Assigning returnValue is what actually triggers the prompt in
			// Chromium, which is what WebView2 is; preventDefault alone is the
			// newer spelling and is not honoured everywhere yet.
			ev.returnValue = '';
		});

		snapshot();
		refresh();
	})();
	</script>`

// logoSVG is the app mark: a "D" (for downloader) whose counter is knocked
// out as a downward triangle, on a tile filled with an opal-ish iridescent
// gradient. Kept as a plain const (not read from internal/gui/assets/, which
// exists only for the rasterised icon.ico window_windows.go embeds) so this
// SVG stays servable as a literal string from handleLogo below with no file
// read on the request path.
//
// Deliberately simple geometry (one rect, one even-odd path): it has to stay
// readable at 16px in a browser tab, where facet lines or a thin arrow shaft
// would turn to mush.
const logoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" width="64" height="64">
<defs><linearGradient id="opal" x1="0" y1="0" x2="1" y2="1">
<stop offset="0%" stop-color="#3d8bfd"/><stop offset="45%" stop-color="#7b5cff"/>
<stop offset="75%" stop-color="#e15fd0"/><stop offset="100%" stop-color="#2fd6c3"/>
</linearGradient></defs>
<rect width="64" height="64" rx="14" fill="url(#opal)"/>
<path fill="#fff" fill-rule="evenodd" d="M20 14H34a18 18 0 0 1 0 36H20Z M27 24H45L36 42Z"/>
</svg>
`

// handleLogo serves logoSVG. Cached hard: the mark only ever changes when a
// new binary ships, and every page pulls it as a favicon.
func handleLogo(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.WriteString(w, logoSVG)
}

// faviconLink is spliced into every page template's <head>, the same
// "shared constant spliced into every page" pattern as pageStyle and
// bannerChrome. An explicit <link> rather than relying on the browser's
// implicit /favicon.ico probe, because that probe expects an .ico and
// browsers disagree about honouring an SVG content-type there.
const faviconLink = `<link rel="icon" href="/logo.svg" type="image/svg+xml">`

// logoMark is the inline mark next to the landing page's <h1>. Decorative
// only (the heading already says the name), hence the empty alt.
//
// The id is for konamiEasterEgg, which is the only thing that ever looks it
// up.
const logoMark = `<img id="logo-mark" src="/logo.svg" alt="" width="30" height="30" style="vertical-align: -5px; margin-right: 0.5rem;">`

// konamiEasterEgg spins the landing page's logo when someone types the
// Konami code. Landing page only, and nothing else in the program knows it
// exists.
//
// It touches only a CSS transform on one decorative <img>, so the worst case
// if it misbehaves is a crooked logo. Deliberately no sound, no persisted
// state, no network call, and no interference with typing: the sequence is
// arrow keys and two letters, and the handler bails out the moment focus is
// in a field, so it cannot eat a keystroke meant for a folder path.
const konamiEasterEgg = `<script>
(function () {
	var CODE = ['ArrowUp','ArrowUp','ArrowDown','ArrowDown','ArrowLeft','ArrowRight','ArrowLeft','ArrowRight','b','a'];
	var at = 0;
	var logo = document.getElementById('logo-mark');
	if (!logo) { return; }
	logo.style.transition = 'transform 1.2s cubic-bezier(.2,.8,.2,1)';
	document.addEventListener('keydown', function (ev) {
		var tag = (ev.target && ev.target.tagName) || '';
		if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') { return; }
		var want = CODE[at];
		var got = ev.key.length === 1 ? ev.key.toLowerCase() : ev.key;
		at = (got === want) ? at + 1 : (got === CODE[0] ? 1 : 0);
		if (at < CODE.length) { return; }
		at = 0;
		var turns = (parseInt(logo.dataset.turns || '0', 10) + 3);
		logo.dataset.turns = turns;
		logo.style.transform = 'rotate(' + (turns * 360) + 'deg)';
	});
})();
</script>`

// pageStyle is the shared look for every GUI page (landing, login, update,
// settings, sync): same fonts/spacing/colors for headings, status boxes,
// buttons, hints, and the back link, so no page looks structurally
// different from another. Page-specific templates append their own extra
// rules (forms, tables, log view, etc.) after this.
const pageStyle = `
	body { font-family: system-ui, sans-serif; max-width: 44rem; margin: 3rem auto; padding: 0 1rem; color: #1a1a1a; }
	h1 { margin-bottom: 0.25rem; }
	h2 { margin-top: 2.5rem; border-bottom: 1px solid #ddd; padding-bottom: 0.25rem; }
	.disclaimer { background: #fff8e1; border: 1px solid #e0c46c; border-radius: 6px; padding: 0.75rem 1rem; font-size: 0.9rem; margin: 1.5rem 0; }
	.hint { color: #666; font-size: 0.85rem; margin: 0.15rem 0 0.6rem; }
	nav ul { list-style: none; padding: 0; }
	nav li { padding: 0.5rem 0; border-bottom: 1px solid #eee; }
	.soon { color: #888; font-size: 0.85rem; }
	/* Pointers to other pages. Deliberately not an <h2> section: reading like
	   a settings group while containing nothing settable is what made the old
	   "Browser" and "Running it automatically" headings confusing. */
	.elsewhere { color: #666; font-size: 0.85rem; border-top: 1px solid #eee; padding-top: 0.9rem; margin-top: 2rem; }
	.intro { background: #f7f9fc; border: 1px solid #d7e2ef; border-radius: 6px; padding: 0.25rem 1.25rem 1rem; margin: 1.5rem 0; }
	.intro h2 { margin-top: 1.25rem; font-size: 1rem; border-bottom: none; padding-bottom: 0; }
	.intro ol { margin: 0.25rem 0 0; padding-left: 1.2rem; }
	.intro li { margin: 0.35rem 0; }
	.intro p { margin: 0.35rem 0; font-size: 0.95rem; }
	.intro .hint { margin-top: 1rem; }
	.status { border-radius: 6px; padding: 0.75rem 1rem; margin: 1rem 0; font-size: 0.9rem; }
	.status.ok { background: #e6f4ea; border: 1px solid #8cc98f; }
	.status.warn { background: #fdecea; border: 1px solid #e0a6a6; }
	.status.neutral { background: #f0f0f0; border: 1px solid #ccc; }
	.error { background: #fdecea; border: 1px solid #d93025; color: #7a1712; border-radius: 6px; padding: 0.75rem 1rem; margin: 1.25rem 0; }
	.success { background: #e6f4ea; border: 1px solid #34a853; color: #1e4620; border-radius: 6px; padding: 0.75rem 1rem; margin: 1.25rem 0; }
	.warning { background: #fff8e1; border: 1px solid #e0c46c; color: #6b5300; border-radius: 6px; padding: 0.6rem 1rem; margin: 0.75rem 0; font-size: 0.9rem; }
	.warning ul { margin: 0.25rem 0 0; padding-left: 1.2rem; }
	button { font: inherit; padding: 0.6rem 1.2rem; border-radius: 6px; border: 1px solid #2a6fb0; background: #2a6fb0; color: #fff; cursor: pointer; }
	button:hover { background: #1f588f; }
	button:disabled { opacity: 0.5; cursor: not-allowed; }
	code { background: #f0f0f0; padding: 0.1rem 0.3rem; border-radius: 3px; }
	.back { margin-top: 1.5rem; font-size: 0.9rem; }
	.back a { color: #2a6fb0; }
	.primary-action { margin: 1.5rem 0; }
	.cta {
		display: inline-block; padding: 0.75rem 1.75rem; font-size: 1.1rem;
		font-weight: 600; color: #fff; background: #2a6fb0; border-radius: 6px;
		text-decoration: none;
	}
	.cta:hover { background: #235d94; }
	.cta.disabled { background: #b8b8b8; cursor: not-allowed; }
	.cta-note { margin: 0.5rem 0 0; font-size: 0.9rem; color: #555; }
`
