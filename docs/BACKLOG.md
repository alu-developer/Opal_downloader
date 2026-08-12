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

- **Blocked on the maintainer: Question 39 — pick A, B or C.** Should anything
  periodically re-verify HTTP-first discovery against an independent browser
  crawl, now that shipping it as the default silently removed the comparison
  every run used to get for free? `OPAL_HTTP_DISCOVERY=verify` already does
  the comparison; nothing calls it outside a test. **(A)** do nothing, accept
  the risk; **(B)** a monthly verify spot-check from the weekly-review pass
  (~one extra full crawl a month); **(C)** a free structural-fingerprint
  tripwire, weaker signal, no extra crawl. Recommendation: (B), with (C) as a
  later independent addition. Options written up in `docs/sync-speed-model.md`
  Question 39. Needs a pick, not further research.

- **Blocked on the maintainer: Question 5's last half — pick A, B or C.**
  "Feels like one click" doesn't actually need faster discovery if the work
  already happened in the background — and it mostly already has:
  `OpalDownloaderScheduledSync` runs daily plus an on-logon catch-up. **(A)**
  do nothing further; **(B)** when the landing page's staleness signal says
  the scheduled sync already succeeded recently, change the primary button's
  copy to something like "Up to date as of \<time\> — Sync now to check
  again" instead of implying work is about to start — zero new network
  activity, reuses `internal/statuslog`; **(C)** a genuine background `list`
  triggered by the GUI opening, independent of the schedule — bigger change,
  needs its own opt-out and `sync.lock` interaction, no evidence yet that GUI
  opens are frequent enough to justify it. Recommendation: (B), with (C)
  worth reopening only if (B) ships and still doesn't move the needle.
  Options written up in `docs/sync-speed-model.md` Question 5. Needs a pick,
  not further research.

- **Blocked on the maintainer: cut `v0.1.1`.** The only published release
  (`v0.1.0`, 2026-07-14) is broken and predates its own fix by three weeks —
  its installer stages Chromium where the binary in that same release no
  longer looks, and `NeedsPlaywrightSetup` probed the same wrong path, so it
  reported "present" and skipped the `setup` fallback that would have
  recovered. The GUI opens; `login`/`sync` cannot start a browser. Fixed on
  master by `9e9ac47` (2026-08-03), and an installer built from current master
  was walked end to end on 2026-08-11 and is sound — but no tag was ever
  pushed, so the fix has never reached a user. Publishing is the maintainer's
  call. Detail: `docs/installer-plan.md`.

---

## Next

`docs/sync-speed-model.md` holds the ranked list: Question 39 and Question 5's
last half, both blocked above with options written up, needing a pick rather
than more research. Question 5's other two halves (CLI silence, GUI
`list`-only silence) are fixed — see `docs/BACKLOG-archive.md`. Nothing
further is planned on the course-level HTTP concurrency thread — Question 41
closed 2026-08-11 as a no-go. **The ranked list has no unblocked live-run or
source-reading experiment left right now** — the next cycle should say so
plainly rather than manufacture a third sub-question to stay busy.

---

## Open findings

Found by using the tool as a normal user rather than reported by the
maintainer. Walk detail, expectations and named causes:
`docs/friction-campaign.md`. Tags: **blocker** / **wrong** / **friction** /
**bloat** / **question**.

### Friction campaign (GUI walks 1, 4 & 5, CLI walk 2, first-run walk 3)

- **[question] The GUI process's ~5-minute exit (walk 1) is now 2 deaths and 1
  survival across three background-shell launches (walks 1, 4, 5), plus one
  clean survival under a properly detached launch (walk 5).** Not a reliable
  every-launch failure, so still not closed as a real bug - but `Start-Process`
  (or equivalent full detachment) is the only launch method with a perfect
  record so far. Recommendation, actionable regardless of the root cause:
  future automated GUI walks should default to it. Needs a fourth data point,
  or someone who can double-click it for real, to close outright.
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
  usage are unchecked against a real Inno Setup run. Whoever next builds the
  installer should compile it and run one real uninstall before trusting it.

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
