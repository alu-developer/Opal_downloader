# Backlog

The current state of work on opal-downloader. **This file is the answer to
"what should I do next?"** — read it at the start of a session, pick the top
item that isn't blocked, and get on with it.

Kept in git deliberately, so it survives a fresh clone, a reinstall, and a
lost `~/.claude` directory. Update it in the *same commit* as the work it
describes; a backlog that lags the code is worse than none.

Keep personal specifics out of this file — the repo is public. Absolute
paths, account details, and measured numbers that only make sense for one
machine belong in local memory, not here.

**Only open work belongs here: what is being worked on, and what is blocked.**
The moment an item is done, decided, or ruled out it leaves — closed work goes
to `docs/BACKLOG-archive.md` under "Done recently", answered-and-shut questions
go to the same file under "Settled". No history, no post-mortems, no "Fixed
2026-xx-xx" entries in this file; an entry says what is *left* and where the
detail lives. Ignoring that grew this file past 900 lines twice, most of it
closed work, until nobody could read it in one pass.

Where the detail lives: `docs/BACKLOG-archive.md` (closed work, settled
questions), `docs/sync-speed-model.md` (the speed campaign's ranked open
questions and its rules), `docs/friction-campaign.md` (walk findings),
`docs/installer-plan.md` (distribution, signing, releases).

---

## Now

- **Strip the explanatory prose out of the GUI — maintainer request,
  2026-08-12.** *"Es ist so nervig, immer einen Absatz zu haben, der einen
  Button erklärt."* Two parts, the second is the general one:

  **Hiding beats deleting where a fact is load-bearing** — maintainer,
  2026-08-12: *"man könnte auch Sachen hinter 'more information' oder so
  hiden."* A `<details><summary>` collapsed by default costs one line of
  page and keeps the information for whoever wants it, so it is the default
  move for anything too useful to delete outright. It is not a loophole for
  keeping prose that should just go: if a better label removes the need for
  the text, relabel and delete. Worked example already in the tree — the
  Feedback page's log block (`internal/gui/feedback.go`), where a collapsed
  `<details class="more">` replaced a paragraph telling the user to go fetch
  the log themselves.

  1. **Delete the "Configured elsewhere" block** on the Settings page
     (`internal/gui/settings_page.go:244-260`) — the pointers to
     `/schedule` ("running once a day on its own") and `/tufast-setup`
     ("automatic 2FA, so login needs no clicks"), plus any surviving mention
     of the dedicated login profile at `~/.opal-downloader/login-profile`.
     Both destinations are already reachable from the nav; the parenthetical
     explanations are the annoyance. This is the third cut to the same block
     (2026-07-27 removed two fake settings sections, 2026-08-10 removed the
     browser-profile sentence) — this time it goes entirely.
  2. **Sweep every GUI page for the same pattern and cut as much as
     possible.** ~26 `<p class="hint">` paragraphs still live across
     `settings_page.go` (11), `tufast_setup.go` (5), `schedule_page.go` (5),
     `sync.go` (2), `logs.go` (2), `gui.go` (1) — `feedback.go`'s two are
     already done, cut to one when the log started attaching itself. The rule
     the maintainer gave: **make the control itself say what it does — a
     clearer label, a better button word, a sensible default — and then
     delete the paragraph**, rather than keeping prose to compensate for a
     vague label. Prefer relabel-then-delete over delete-only where the
     control genuinely isn't self-evident.

  **One exception, added 2026-08-12 by the session that shipped it:** the
  landing page's `<p class="cta-note last-sync">Last sync: …</p>`
  (`internal/gui/gui.go`) is not explanatory prose and must survive this
  sweep. It states a fact the user cannot get from the control — when the
  work last actually happened — and it exists because the maintainer asked
  for it in the same conversation that produced this item. Delete the
  paragraphs that restate a button; keep the one that reports a state.

  Judgement call, not a mechanical pass: a few hints carry information a
  label cannot (e.g. the subfolder-layout hint at
  `settings_page.go:192` warns that ticking it *moves existing downloads on
  the next sync* — a consequence, not a description). Keep those, rewritten
  short; delete everything that only restates the control. Where a hint is
  genuinely load-bearing but long, a collapsible detail is acceptable —
  but the default state is "not shown". Expect the GUI walk tests
  (`browser_walk_test.go`, `first_run_journey_test.go`, `settings_test.go`,
  `schedule_test.go`, `tufast_setup_test.go`) to assert on some of this copy;
  update them with the change.

- **Question 39 — decided 2026-08-12: (B), a monthly `verify` spot-check.**
  The maintainer handed the call back ("wenn das krasse Vorteile bringt gerne.
  Aber ich kenn mich damit nicht aus… ich weiß nicht, was es finden könnte"),
  so it was decided here, and the honest framing is the part worth keeping:
  **this is insurance, not an improvement.** It buys nothing on a good day. It
  exists for one failure mode — BPS changes OPAL's markup, HTTP-first silently
  discovers fewer files, and nothing in this project is positioned to notice,
  so the user simply ends up with fewer files and no error. That silent-loss
  class is the one this project has refused to accept everywhere else it found
  it. Chosen because the premium is small: ~4 minutes of unattended crawl once
  a month, against a failure that produces no error message at all. Build: a
  new Part C on the weekly-review pass running `OPAL_HTTP_DISCOVERY=verify`,
  guarded by its own `docs/last-verify-run.txt` at ~30 days, filing the diff's
  `missing` count as a backlog item. Deliberately **not** wired into `sync` or
  any daily path — `verify` runs a full extra browser crawl on top of the
  HTTP-first one, which would roughly double the sync time Step B2 shipped to
  cut. Note the scope change openly: that pass is review-only today, and this
  makes it run a live crawl. (C), the free structural fingerprint, stays a
  later independent addition, not a substitute. Detail:
  `docs/sync-speed-model.md` Question 39.

- **Question 5's background-run half — the only piece of Question 5 still
  open.** The "last sync" line shipped and is live-verified (see the archive);
  what it does *not* do is start any work early. Option **(C)** — a real
  background `list` triggered by the GUI opening — is deliberately not built:
  it needs its own opt-out and `sync.lock` interaction, and there is still no
  evidence GUI opens are frequent enough to justify it. Reopen only if the
  timestamp has been in use for a while and "feels like one click" still
  visibly fails; otherwise close it. Detail: `docs/sync-speed-model.md`
  Question 5.

---

## Next

`docs/sync-speed-model.md` holds the ranked list. **Question 43 (new,
2026-08-12) is now the top item and is unblocked**: source-confirmed that
OPAL's course folder sections run through OpenOLAT's `FolderController`,
which exposes a read-permission-only bulk "download as ZIP" action
(`doBulkDownload` → `FolderZipMediaResource`) — nothing on this list has ever
questioned the *download* phase before. Needs a live Step B (real browser,
one section, confirm the button and time it) to become an actual lever;
source-only so far. **Nothing on this list is blocked on the maintainer any
more** — Question 39 and Question 5's last half were decided 2026-08-12 (see
"Now"); both are build work now, not questions. Question 5's other two halves
(CLI silence, GUI `list`-only silence) are fixed — see
`docs/BACKLOG-archive.md`. Nothing further is planned on the course-level HTTP
concurrency thread — Question 41 closed 2026-08-11 as a no-go.

---

## Open findings

Found by using the tool as a normal user rather than reported by the
maintainer. Walk detail, expectations and named causes:
`docs/friction-campaign.md`. Tags: **blocker** / **wrong** / **friction** /
**bloat** / **question**.

### Friction campaign (GUI walks 1, 4 & 5, CLI walk 2, first-run walk 3)

- **[question] Every GUI settings save silently resets `opal_url` and
  `session_state_file` to hardcoded defaults**, discarding whatever was there
  before — deliberate and tested (`internal/gui/settings.go:47-52,289-290`,
  `settings_test.go:483-488`), not a bug to fix. The generalizable part: the
  save mechanism drops *any* config field the settings form doesn't expose,
  not just these two. Worth a comment at the config struct pointing future
  field additions at `parseSettingsForm` so the next one doesn't lose the same
  way silently; not worth a behavior change. Walk 4.
- **[friction] Real per-file download errors show raw Playwright internals to
  the user, on two surfaces Finding 3 never checked.** `internal/syncer.go:595`
  and `:662` (`fmt.Printf("  error: %s (%v)\n", ...)`) print the full wrapped
  error chain to the CLI's stdout; the GUI's live `/sync` log mirrors the same
  string. Live-observed 2026-08-12 (walk 5) on a real failure: a good first
  clause ("response is HTML, browser fallback click did not find downloadable
  link…") followed by a full Playwright locator/timeout call log glued on.
  `internal/scraper/download.go:244`'s verbosity is deliberate (its own
  comment: three past investigations, PRs #35/#89/#95, needed the detail to
  find the real cause) — the gap is that there's no split between "what the
  user reads" and "what the next investigation needs," unlike the
  already-fixed connectivity-error case (`No internet connection…
  (technical detail: …)`, from `netcheck`). Fix direction: apply the same
  short-clause + collapsible-detail split to both the CLI's `error:` line and
  the GUI's mirrored log line. Not built this walk. Walk 5.
- **[question] What a *sync* does with an unwritable `download_path`** — fail
  clearly, or appear to succeed? `status` now catches a broken path before a
  sync starts, but a path that goes bad *between* the check and the sync is
  still unmeasured. Follow-up from walk 1.
- **Optional, not a commitment:** an outcome-independent "when did a sync last
  actually *succeed*" staleness signal — walk 1's Finding 1, repair (a).
  Repair (b) shipped and closes the failure mode that was actually observed;
  (a) would be a broader defence-in-depth layer on top.
- **The installer surface is still unwalked by the campaign proper.** The
  2026-08-11 installer work was engineering verification with full knowledge
  of the code, so none of it counts as a persona walk.
- **Walk 1's questions 3 (is the 08:00 default schedule time hostile to the
  logged-off failure) and 4 (do the three `download_path` slash conventions
  behave identically) are still open.** Walk 4 spot-checked one convention
  (backslash absolute) and it round-tripped correctly through Settings; the
  other two, and any interaction with the known `default_course_folder`
  doubled-path bug, are unchecked.

### Installer

- **Unverified fix: the post-uninstall message has never been compiled or
  run.** `CurUninstallStepChanged` (`installer/opal-downloader.iss`) now shows
  a `MsgBox` at `usPostUninstall` naming both the deliberately-kept ~680 MB
  Chromium cache and the `%USERPROFILE%\.opal-downloader` folder (session,
  settings, status files — never installed by Inno Setup, so never known to
  its uninstaller). Written 2026-08-12 from source only: `iscc` is not
  available in this environment, so the dialog text and the `ExpandConstant`
  usage are unchecked against a real Inno Setup run.
  **Confirmed 2026-08-12: Inno Setup is not installed on the maintainer's
  machine either** (neither on `PATH` nor in either `Program Files` location),
  so "just compile it locally" is not the free step it reads as. CI's
  `release.yml` is this project's only `iscc`.
  **Half closed 2026-08-12:** the `v0.1.1` release build (run `31604913539`)
  compiled this `.iss` successfully, so the script is syntactically sound and
  the `ExpandConstant` usage does not break the compile. What that does *not*
  prove is that the dialog *reads* correctly, or that it appears at all — a
  green compile says nothing about `usPostUninstall` firing.
  **What is left, and the only two ways to get it:** **(1, recommended)** the
  maintainer installs `v0.1.1`, uninstalls it once, and says whether a message
  appeared naming both the ~680 MB Chromium cache and
  `%USERPROFILE%\.opal-downloader` — ~1 minute, and the only thing that
  verifies the actual text. **(2)** close it on the compile alone and accept
  that a wrong-but-valid dialog string would ship unnoticed; defensible, since
  the worst case is a confusing sentence during an uninstall, not data loss.
  Do not leave this reading as a bare "someone should check".

---

## Noticed

Rough edges seen while working on something else, that would otherwise exist
only in one session's context window. Not commitments. An entry leaves in one
of two directions: up into the work above, or into `docs/BACKLOG-archive.md`
once it is done, decided, or shown not to matter.

_(Empty as of 2026-08-12 — everything that stood here was already closed or
decided and now sits in the archive's "Settled" section.)_

---

## Standing work

Not an item to finish — the work that fills a run when nothing above is
unblocked. The `opal-downloader-autopilot` task reaches it as its phase 2.

### Sync speed as an iteration loop

**`docs/sync-speed-model.md` is the driver** — known numbers, ranked open
questions, the three rules, and one experiment at a time with its predicted
number and kill criterion written down *before* the run.
`docs/sync-speed-campaign.md` is the archive. There is no cap on the campaign;
the kill criterion sits per experiment. A report every fifth cycle carries a
keep-going-or-stop recommendation, and the maintainer makes that call.

Two standing decisions govern it. Every experiment goes behind an env flag and
is diffed byte-for-byte against the 345-file ground truth, but **a default that
has passed that diff may be changed and shipped** without asking (2026-08-03),
so a measured win reaches the maintainer instead of sitting behind a flag. And
**correctness goes ahead of speed** where the two compete (2026-08-03). The
corollaries the campaign learned the hard way — including why a byte-for-byte
diff is not proof of losslessness — sit with the rules in
`docs/sync-speed-model.md`.
