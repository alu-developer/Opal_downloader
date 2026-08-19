# Friction campaign: find the problems without being told

Opened 2026-08-11 by the maintainer, who named the job precisely: *"imitate a
normal user, who uses the tool, and find problems, errors, weird stuff,
annoying stuff, confusing stuff, too bloated stuff."*

The premise is that he should not have to find them. Every rough edge in this
project so far has been reported by the one person whose time is the scarce
resource — he clicks something, it is wrong, he says so, it gets fixed. That
loop only ever surfaces what he happens to touch, and it charges him for the
finding. This campaign moves the finding to me.

**Explicitly ruled out at open:** seeding this from his list of annoyances.
Asked, and refused, on the grounds that name the point: *"no. I won't do that.
you have to find the things. I don't want to click everything everytime."* So
a finding that only appears here because he pointed at it is not a result.

This file is modelled on `docs/sync-speed-model.md`, which exists because the
old working method — try an idea, it fails, drop it, with no step in between
where anyone understands why — was diagnosed as the real problem. The same
disease has a UX form: notice something feels bad, tweak it, move on, with no
step where anyone understands *why* it felt bad. The rules below are the cure,
translated.

## The four rules

1. **Write the expectation before the click.** Before each step: what does a
   normal user think will happen? A finding is the *gap* between that and
   reality. Without a registered expectation, "this is annoying" is an opinion
   and this campaign has no way to review it later. This is Rule 1 of
   `sync-speed-model.md`, in words instead of numbers.
2. **A finding may only be closed when the cause is named sharply enough that
   it predicts where else the same thing shows up.** "The button is confusing"
   is a description. "The control is labelled with the implementation verb
   (`sync`) rather than the user's goal, and so are three others" is a cause,
   and it tells you what to go look at next.
3. **Every walk leaves at least one new question.** If a walk finds nothing,
   that is the result to report — and the next walk changes surface, because
   finding nothing usually means I walked where I already knew the answer.
4. **Stay in persona, and log every break.** I am a student who has not read
   this source, does not know the flags, and does not open `docs/` unless the
   GUI itself sends me there. The moment I use insider knowledge to get past a
   step, the step was a finding and I missed it. Breaks get written down.

## Where findings go

**Into `docs/BACKLOG.md`, not into this file** — the maintainer's call when
this campaign was opened. This file records the *walk*: what was tried, what
was expected, what happened, and the named cause. The backlog records the
*work*. Anything that would be a duplicate of the backlog entry stays out.

Severity, used to rank them in the backlog:

| tag | meaning |
|---|---|
| **blocker** | a normal user cannot get through this at all |
| **wrong** | it does something other than what it says, or than what happened last time |
| **friction** | it works, but costs the user attention it should not have cost |
| **bloat** | it exists, and the user never needed it |

## Surfaces, and the order they get walked

Chosen 2026-08-11 with the maintainer. GUI everyday use first, because
`docs/work-quality.md` records that his "hier wurde gepfuscht" complaint came
from looking at the GUI — so that is where the unfound problems are most
likely to be sitting.

1. **GUI, everyday use** — open it, sync, read the status, change a setting,
   meet an error. *(cycle 1)*
2. **CLI, everyday use** — `login` / `list` / `sync`, flags, error text, logs.
3. **First run from zero** — installer or fresh clone, no config, no session.
   `docs/setup-friction.md` did one pass of this in the past; this is the
   ongoing version.

**Next surface: 1 (GUI), for a session with a browser tool.** Walk 1 was the
GUI, walk 2 the CLI, walk 3 first-run-from-zero, walks 4 and 5 the GUI again
(two concurrent sessions picked it independently before either had a
result), walk 6 the CLI again, walk 7 first-run-from-zero again (a
GUI-specific angle walk 3 never covered: walk 3 only exercised the CLI
build+`list` path from a fresh clone, never `gui`'s own zero-config first
load), walk 8 the CLI again (everyday use, found nothing wrong), walk 9 the
CLI again (`smoke-check`, two findings) — GUI has had four looks, CLI four,
first-run-from-zero two; GUI is due *by count*.

**But an unattended autopilot session cannot do a GUI walk at all** (found
walk 9, 2026-08-18): `preview_start` refuses to launch a dev server from a
scheduled-task/unattended session outright - "nobody is present to approve
the command" - so there is no browser tool available to drive one, full
stop, regardless of rotation. Walks 6, 8, and 9 all landed on CLI instead for
this same reason, whether or not they said so explicitly at the time. An
unattended run should treat CLI/first-run as the only two surfaces actually
in rotation for it and pick whichever of those is due; the GUI slot stays
reserved for a session with an interactive browser tool (or a human) to pick
up - it is not skipped, just not reachable from here.
Keep this line current at the end of every walk — it is what an unattended run
reads to avoid walking the same surface twice, which is the cheapest way for
this campaign to quietly stop finding anything. **This line went stale once
already** (still said "2 (CLI)" after walk 2 had already happened, caught only
because walk 3 cross-checked the walk log below instead of trusting it
blindly) - worth re-verifying against the walk log, not just reading this
line, until that has a better track record.

Who runs this: the **`opal-downloader-autopilot`** routine, phase 2, one walk
per run, after it has emptied the backlog and before it does a sync-speed
cycle. A live session can of course walk any time.

Blast radius granted at open: **anything, including install and uninstall.**
Breaking things on purpose is explicitly in scope — that is where the ugly
errors live and they cannot be found any other way. See "Breaking things
safely" below for how to do that without it costing anything, which is what
makes it available to unattended runs too.

The one standing constraint is unchanged and comes from elsewhere: one crawl
at a time (`sync.lock`, see `docs/BACKLOG.md`).

## Breaking things safely

Asked by the maintainer 2026-08-11, after the first version of this file
reserved all sabotage for live sessions: *"gibt es keinen weg gewisse sachen zu
breaken, aber auf eine ungefährliche art und weise?"* There is, for nearly
everything, and reserving it was too cautious.

**The principle: the app is fully parameterised by `--config`** — nine
commands take it — **and everything a user can break is either in the config
or under a path the config names.**

| what breaks | where it lives | config-scoped? |
|---|---|---|
| downloaded files | `download_path` | yes |
| the sync manifest | `.opal-sync.manifest.json` **inside** `download_path` (`syncer.go:435`) | yes |
| the session | `session_state_file` | yes |
| the server it talks to | `opal_url` | yes |
| every folder / pattern rule | the config itself | yes |

So there is no need to break the real thing: **build a disposable copy of the
environment and break that.** The real one is never involved, and a run that
dies halfway leaves nothing to repair — which is the property that makes this
safe unattended, not the size of the damage.

### The scratch environment

```
tmp/friction/config.yaml    copy of the real config, with download_path and
                            session_state_file redirected to the two below
tmp/friction/downloads/     scratch download_path — the manifest lands here too
tmp/friction/state.json     copy of the real session state, so a walk does not
                            have to log in first
```

Then pass `--config tmp/friction/config.yaml` to everything.

`tmp/` is gitignored (`.gitignore:39`), so the copied session token never
reaches the public repo. **Re-check that before copying it**, every time — it
is one line and the consequence of it having changed is a leaked credential.

### Green — break freely, including unattended

| break | how | what it tests |
|---|---|---|
| unreadable config | truncate the YAML mid-key, wrong types, unknown keys | does the error name the file, the line, and the fix? |
| impossible `download_path` | nonexistent drive, a file instead of a folder, read-only dir, 300-char path, emoji, UNC | the whole path-error class in one sitting |
| OPAL unreachable | point `opal_url` at a dead host | reproduces walk 1's Finding 3 error text **without touching networking** |
| expired / corrupt session | truncate or hand-edit the scratch `state.json` | the auto-relogin path, and what the user stares at while it runs |
| corrupt manifest | truncate it, half-write it, bump its schema version | does a bad manifest cost one file or the whole sync? |
| files disappear behind the user | delete / rename / chmod files in scratch downloads between two syncs | re-download logic, and the error when a file cannot be written |
| interrupted run | kill the process mid-sync | does the next run recover on its own, or does it need a human? |
| two at once | two syncs against the scratch config | `sync.lock` — untouched by walk 1 |
| a fresh install | the installer takes Inno Setup's `/DIR=`, so install to a scratch directory | the first-run path without disturbing the real install |

### Amber — snapshot first, then break, still fine unattended

The GUI's status files are **global, not config-scoped** — fixed paths under
`~/.opal-downloader/` (`statuslog.DefaultPath`): `last-scheduled-run.json`,
`scheduled-run-history.jsonl`, and the dismiss marker. Testing what the banner
does therefore has to touch the real ones.

**`last-sync.json` belongs in this list too (added 2026-08-18, found live, not
by inspection)** — it looked unlisted because it is not written by the
*scheduled*-run path this section was written to describe, but
`WriteLastSyncDefault` (`cmd/opal-downloader/root.go`) fires for *any*
`sync`, CLI or GUI, scheduled or manual, **and it is not config-scoped either
— a `sync --config tmp/whatever/config.yaml` overwrites it exactly the same
as a real one, with no marker distinguishing which config produced the
numbers on it.** Confirmed the hard way: Question 44's own live verification
runs (`docs/sync-speed-model.md`) overwrote the maintainer's real
`last-sync.json` with a scratch run's counts before this entry existed to
warn about it. Any sync-triggering action against ANY config counts as amber
from now on, not just direct edits to the status files themselves.

Copy them to `tmp/friction-restore/` first, then break them, then restore.
Stakes if a run is killed mid-way: a wrong or missing banner — which is
precisely what walk 1 documented as already happening, so the worst case is
the bug we are already reporting.

### Red — live session only, and exactly why

Three things, red because no copy of them exists to break instead:

1. **`~/.opal-downloader/login-profile`.** Holds the TU-Fast extension and its
   stored credentials. Rebuilding it is the one genuinely manual step left in
   this project. Never delete it. (Expiring a *session* is green — that is
   `session_state_file`, a different thing.)
2. **The real Windows task `OpalDownloaderScheduledSync`.** The name is a
   constant, so there is no scratch copy to register alongside it. A run killed
   between deregister and restore leaves automatic sync silently gone — and by
   walk 1's Finding 1, nothing in the app would ever report that. Reading it
   (`Get-ScheduledTask`, the event log) is green.
3. **The real install and the real `download_path`.** Note the installer itself
   is green via `/DIR=`; it is *uninstalling the real one* that is red.

**If a red rehearsal is what a question needs, that is not a dead end** — file
it as a backlog item saying what it would test and what it would prove, and a
live session runs it. Do not quietly substitute a green approximation and
report it as the same evidence.

### Validated 2026-08-11, not just written down

The recipe above was built and run before being committed, because an
unattended run at 03:00 following an untested instruction is worse than no
instruction. `tmp/friction/` exists and works: `status --config
tmp/friction/config.yaml` reports the scratch download path and the scratch
session state, and nothing of the maintainer's is read or written.

Two green-tier breaks were run immediately, and they are the first evidence
that the tier is worth having — one pass, one finding, at zero risk:

- **Unreadable config: passes, cleanly.** An unterminated quote gives
  `Error: invalid yaml in <full path>: yaml: line 9: did not find expected
  key` — names the file, the line and the problem. No finding; recorded so
  nobody re-tests it.
- **`download_path: "Q:/nope/downloads"` on a drive that does not exist:
  `status` answers `(OK)`.** It prints `Download path: Q:/nope/downloads` and
  reports the config as fine. The one command whose entire job is to say
  whether the setup is sound validates the config's *syntax* and never its
  *substance*. Filed.

## How the GUI is driven

`opal-downloader gui --port <n>` serves the front-end over plain HTTP on
127.0.0.1 and wraps it in a WebView2 window. So the same UI can be driven in
a normal browser — real clicks, real screenshots, plus the console and network
logs a WebView2 window will not show you. No human has to click anything,
which was the maintainer's condition.

---

## Walks

### Walk 1 — 2026-08-11, GUI everyday use

Built current master, ran `gui --port 8765`, and used it the way somebody
would who wants their lecture PDFs: look at the front page, hit the obvious
button, look at the settings, look at the automatic sync. Persona held
throughout; the only breaks are marked, and all of them are *diagnosis after
a finding*, never a way of finding one. That distinction is the point — Rule 4
governs how a problem is discovered, not how its cause is established
afterwards, and Rule 2 makes the diagnosis mandatory.

**Expectation registered before opening it:** within a few seconds I can tell
whether the thing is set up, and there is one obvious way to get my files.

Half true. The one obvious button is there ("Sync now") and the login state is
stated clearly. But the first thing on the page, above the app's own name, is
a Playwright stack trace from a failure the day before yesterday.

#### Finding 1 — the automatic sync has silently not run on most days, and nothing says so

The largest finding of the walk, and the one no amount of reading the GUI
would reveal, because the GUI's own reporting is structurally incapable of it.

The front page's only statement about automatic sync is a red banner about
the **10/08 08:38** run failing. Today is 11/08. The schedule is set to
08:00 daily and the checkbox is on. A user reads that banner as "the last
thing that happened was two days ago and it failed", which is true, and
concludes their sync is otherwise fine, which is not.

Windows' own record for `OpalDownloaderScheduledSync`, last 9 days:

| | |
|---|---|
| runs that actually launched (event 200) | **2** — 08/08, 08/10 |
| runs dropped before launching (event 332) | **3** — 07/08, 09/08, 11/08 |

Event 332, verbatim, from today at 08:50:05:

> Task Scheduler did not launch task "\OpalDownloaderScheduledSync" because
> user "…\alois" **was not logged on** when the launching conditions were met.

**Cause, named sharply enough to predict the rest of its blast radius:** the
GUI's entire notion of "did it work?" comes from `internal/statuslog`, which
is written **by the sync process itself**. A run that Task Scheduler never
launched has no process, so it writes no record, so the banner cannot mention
it. The banner is therefore not "the state of your automatic sync" — it is
"the outcome of the most recent run that got far enough to have an outcome."
Those two are equal only when nothing goes wrong before the binary starts.

That predicts exactly where else this shows up, and the prediction is what
makes it closable per Rule 2: **every pre-launch failure is equally
invisible** — task deleted, the exe missing (see Finding 2), user logged off,
Windows refusing on battery. All of them leave the GUI showing a stale success
or a stale failure, indefinitely.

The interaction that produces it is worth stating on its own, because the
`/schedule` page discloses both halves and never their product. It promises
"catching up automatically if the machine was off or asleep" (true —
`StartWhenAvailable` is correctly set to `true`, verified on the live task)
and, separately, "it only runs while you are logged in to Windows" (also
true). What nobody can derive from those two sentences: **when the catch-up
fires and you are at the lock screen, the catch-up is consumed and
discarded, not deferred.** `NextRunTime` had already rolled to 12/08 by the
time I looked. One shot, silently spent.

*Break from persona, for diagnosis only:* Windows event log, `Get-ScheduledTask`,
and `internal/statuslog/statuslog.go`. The finding itself came from noticing
the banner's date was two days old on a "daily" sync.

**Fix direction (not built this walk).** Two separate repairs, and only the
first needs any judgement:

- *Reporting.* The GUI should answer "when did a sync last actually
  succeed?" — a question it currently cannot answer at all — and say so when
  the answer is old. This is the general fix; it covers every pre-launch
  failure at once rather than the logged-off one specifically.
- *The missed run itself.* Adding an **on-logon trigger** alongside the daily
  one is the repair that costs nothing and breaks no promise: it needs no
  stored password, so the page's "no password is stored for it" stays true.
  The alternative — "run whether the user is logged on or not" — requires
  storing a password and would make that sentence false. Recommendation is
  the logon trigger; the choice between them is the only part worth a
  maintainer's ten seconds.

#### Finding 2 — the daily sync has been running a 19-day-old binary

The scheduled task's action is:

```
C:\07_Arbeitszeug\Open_github\Opal_downloader\main.exe sync --scheduled
```

`main.exe` last written **23/07/2026** — nineteen days before this walk. So
every fix merged since then has never once run in the automatic sync,
including HTTP-first discovery becoming the default the day before.

Worse than stale: `main.exe` is **untracked and gitignored** (`.gitignore:23`,
`*.exe`). The user's automatic sync depends on a build artifact sitting loose
in a git working directory. `git clean -xfd` — an ordinary, recommended
command — deletes it, and afterwards the GUI still shows automatic sync as on,
the task still shows as `Ready`, and nothing ever syncs again. By Finding 1's
mechanism, that failure is invisible too: the binary never starts, so nothing
writes a status.

**Cause:** the scheduler registers whatever path the *currently running*
process reports, with no notion of an installed location that outlives a
build directory. The install path (`installer/opal-downloader.iss`) exists but
the schedule is not tied to it.

**Acted on this walk:** rebuilt `main.exe` from current master, so the daily
sync at least runs current code tonight. That is a stopgap on the staleness,
not a fix for the dependency — the `git clean` hole is untouched.

#### Finding 3 — user-facing errors carry raw Playwright internals

The banner reads, in full:

> could not reach OPAL at https://… - check your internet connection and
> opal_url in config.yaml: **Frame.Goto https://…: playwright:
> net::ERR_INTERNET_DISCONNECTED at https://… Call log: - navigating to "…",
> waiting until "domcontentloaded"**

The first clause is a genuinely good error message. Everything in bold is
then glued onto it.

**Cause:** the friendly message *wraps* the underlying error (`%w`) instead of
replacing it, and the GUI prints the wrapped string whole. **Prediction from
that cause, then confirmed** in `scheduled-run-history.jsonl` without looking
for it: the 01/08 and 02/08 entries show the same shape one order of magnitude
worse — a browser-launch failure that dumps an entire Chromium command line,
`--disable-field-trial-config --disable-background-networking …`, into the
same banner. Same bug, and it is the one a user would actually hit.

#### Finding 4 — the failure banner never expires

The 10/08 failure was a transient one (no internet, machine offline). The
network has been fine since, the session is valid for two more days, and the
banner is still the loudest element on every page of the app, ~30 hours later.
It is dismissible, but only by hand, and only after the user decides it is safe
to dismiss — which requires knowing more than the banner tells them.

**Cause:** the banner's condition is "the last recorded run was a failure",
with no notion of whether that failure is still true or still actionable.
Related to Finding 1 and not the same: Finding 1 is that a *missing* run says
nothing, this is that a *resolved* failure keeps shouting.

#### Finding 5 — the main button never says how long it takes (fixed 2026-08-11, same walk - see Walk 4's ruled-out note)

"Sync now — Downloads new and updated files for your 6 selected courses." No
duration, anywhere on the landing page. The `/sync` page then volunteers, for
the *secondary* action: "it therefore takes about as long as a sync — several
minutes". So the app knows the number and tells you only if you pick the
option you were not going to pick.

Worth saying plainly given `docs/sync-speed-model.md` exists to attack the 30s
target: whatever that campaign eventually lands on, the *expectation* costs
nothing and is not set today. A user who expects a click and gets four minutes
experiences a broken app; a user told "about four minutes" experiences a slow
one.

#### Finding 6 — a nav item labelled "developer tools" is where the main button goes

| control | destination |
|---|---|
| "Sync now" (primary button) | `/sync?autostart=1` |
| "Sync options & developer tools" (nav) | `/sync` |

The same page. A student avoids anything labelled "developer tools", and in
doing so avoids the page holding "Preview sync (no download)" and "Force
re-download" — the two options most likely to be useful to them.

**Cause:** the page does three jobs (run a sync, configure this run, expose dev
flags) and the nav label names only the third.

#### Finding 7 — the two-window sentence

> "A separate browser window opens for OPAL login/sync. Closing this window
> does not stop it, and closing it does not close this window."

Two windows, four pronouns, one sentence, and "this window" means the GUI
while "it" means the browser — except in "closing it", where "it" is the
browser again and "this window" is the GUI. It is correct and nearly
unreadable.

#### Finding 8 — the settings page ships a pattern-matching rules engine to every user

`/settings` is one page, no tabs, no advanced section, and it contains: download
path, incremental toggle, default course folder, sync-all toggle, a six-row
per-course folder table, three buttons acting on that table, a subfolder
organisation toggle, **section-name rewrite rules**, and **subfolder
destination overrides** keyed by `<course pattern>/<subfolder pattern>`, whose
own help text offers `*Analysis*/*Vorlesung*` as the example.

A student who wants their PDFs in a folder has to scroll past a glob DSL to
find out there is nothing else below it. This is the clearest instance in the
app of the maintainer's "too bloated" — not because the feature is wrong, but
because it is presented at the same level as "where do my files go".

#### Ruled out — things that looked like findings and are not

Recorded because Rule 1 means registering the expectation, and an expectation
that turns out fine is still a result.

- *"The course table is empty."* It is not. Input `value` attributes do not
  appear in extracted page text; a JS read shows all six courses and their
  folders correctly populated.
- *"The GUI says automatic sync is off while Windows has it on."* It does not.
  The accessibility tree failed to expose the checkbox state; the checkbox is
  checked, with 08:00 in the time field, matching the live task exactly.
- *"Update checks unavailable for development builds (dev)"* — an artifact of
  my own from-source build, not what a normal user sees. Flagged rather than
  filed, but see the open question below.

#### New questions this walk leaves (Rule 3)

1. **The GUI process exited on its own after roughly five minutes**, while I
   was still browsing it and without anyone closing the window; the HTTP
   server stopped answering and no process remained. Relaunching worked. I
   cannot yet separate a real bug from an artifact of launching it from a
   background shell rather than a double-click, so it is a question, not a
   finding. Next walk: launch it the way a user does and leave it alone.
2. **Does the from-source path leave every such user on a "dev" build**, with
   update checks permanently disabled? `README.md`'s install route is a build
   from source, so this may be the normal case rather than my artifact.
3. **Is the 08:00 default schedule time actively hostile to the logged-off
   failure in Finding 1?** 08:00 is exactly when a laptop is booted but not
   yet unlocked. A later default might dodge the whole class.
4. **Three path conventions coexist in the settings form** — forward-slash
   absolute, backslash absolute, and relative — and the form accepts all
   three without comment. Whether any of them behave differently is unchecked,
   and interacts with the known `default_course_folder` doubled-path bug.

### Walk 2 — 2026-08-11, CLI everyday use

Rotation says CLI next (walk 1 was the GUI). Built current master, ran
`opal-downloader --help` cold, then drove the commands that help text
describes as the ordinary read-only ones a person checks first: `status`,
`list --visit-report`. No flags typed that weren't in the help text; no
source opened before the finding below was already named.

**Expectation registered before opening it:** `status` is described in its
own `--help` line as "Offline check: config parses and whether a session
state file exists" - I expected that second half to actually tell me
*whether I'm logged in*, not just that a file happens to be sitting there.

#### Finding — `status` reported a session file's presence, not its validity, and said strictly less than the GUI already knows from the same file

`status`'s login line read `Logged in: session state file present
(C:\Users\alois\.opal_storage_state.json)` regardless of whether that
session was five minutes old or five weeks expired. Opening the GUI's
landing page for the same account read `Logged in, valid until Fri 14 Aug,
21:22 (2 days left)` - a materially more useful sentence, from the same
file, and both are meant to be *offline, no-browser* checks.

**Cause, named sharply enough to predict where else it shows up:** this is
the identical "checks presence, not substance" pattern walk 1 already found
and fixed for `download_path` in this same command - `status` stat'd the
state file and stopped, exactly like it once did for the download path. The
substance was not missing from the codebase, only from this call site:
`internal/sessionstate.Inspect` has existed since 2026-08-03 specifically to
answer "am I still logged in, and until when?" from one offline file read
(it already reads OPAL's own `authenticated-marker` cookie expiry), and
`internal/gui` has used it for its landing page ever since. `status` was
never wired to it - the prediction ("another offline-check call site skips
the substance the tool already knows how to compute") held on the first
place I looked.

**Fixed this walk**, not deferred: `status` now calls
`sessionstate.Inspect` and reports one of the same four states the GUI
does (not logged in / present but expiry unknown / expired / valid with
time remaining), in matching wording. Verified live against the real
account's own session file - output now reads `Logged in: valid until Fri
14 Aug, 21:22 (2 days left)`, byte-identical in substance to what the GUI
already said for the same file. Full test suite green;
`cmd/opal-downloader/root_test.go` gained four cases (not-logged-in,
expired, valid-with-remaining, present-but-unknown-expiry) plus a
`humanizeDuration` unit test.

*Break from persona, for diagnosis only:* read `internal/sessionstate`'s
package doc and `internal/gui/gui.go`'s status block once the gap was
already named, to confirm a ready-made fix existed rather than needing new
design.

#### New questions this walk leaves (Rule 3)

1. **`list --visit-report`'s output is dominated by rows that are "empty on
   all visits" every single time** (roughly 80% of ~325 rows on the real
   account, e.g. every course's own top-level node and every purely
   organisational subfolder). Structurally these may simply be container
   nodes that never hold files themselves - normal, not a bug - but the
   report does not distinguish that class from a section that *should* have
   files and doesn't, so the handful of rows that would actually flag
   instability (partial-empty, not always-empty) are buried in noise. Not
   chased further this walk: distinguishing "structurally file-less
   container" from "a section losing files" needs either a data model change
   or cross-referencing against actual file counts, not a quick read. Next
   step for whoever picks this up: check whether any row has `Empty <
   Visits` at all (this walk did not confirm one either way) - if none do,
   the always-empty rows may be entirely explainable and the finding is just
   "the report needs to filter or sort them out of the way," a much smaller
   fix.
2. **A real sync ran against the real account partway through this walk
   (2026-08-11 21:58-22:24, 1 downloaded/299 skipped/49 errored) that this
   session did not trigger** - none of `status`, `list --visit-report`
   (confirmed offline by reading their own flag-handling code, not by
   assumption), or the scratch-config GUI instance used for the `/settings`
   verification touch the real account or a browser. A second worktree
   (`worktree-lazy-plotting-diffie`) was found active at the same time,
   which this project's own conventions treat as the likely explanation
   (a concurrent session using the shared login profile/`sync.lock`, exactly
   the "several sessions at once" pattern `CLAUDE.md` expects) rather than
   evidence of a new bug - but it was not confirmed, since chasing another
   session's live state was judged not worth interfering with. The 49 file
   errors themselves are real and current and worth the maintainer's own
   look (no per-file error detail survives in the shared
   `~/.opal-downloader/logs/opal-downloader.log` - only discovery-phase
   entries are logged there, so whatever failed on those 49 files left no
   trace to read after the fact).

### Walk 3 — 2026-08-12, first run from zero

Rotation said CLI next per the (stale, see above) "Next surface" line, but the
walk log shows walks 1 and 2 already covered GUI and CLI - surface 3
("installer or fresh clone, no config, no session") had never had a real
campaign walk. `docs/BACKLOG.md`'s "Installer walk" section covers similar
ground but explicitly disclaims itself: no registered expectations, insider
knowledge used throughout, so it does not count against this campaign (Rule 4).
This walk does the same ground properly, in persona.

Fresh `git clone` of the real repo into a scratch temp directory outside this
worktree entirely (not `tmp/friction/` - that recipe is for breaking an
*existing* config, this is "no config exists yet" from true zero), then
followed only what `README.md` says, as someone who has never seen this source
would.

**Expectation registered before cloning:** the README is the only thing I've
read; I expect to get from a fresh checkout to "my files are downloading" by
following it literally, and I expect to notice if it lies about what happens
at any step.

*Tooling break, logged immediately rather than smoothed over:* the first
`setup`/`go build` pair actually ran against this **worktree's** directory,
not the scratch clone - the shell tool's working directory silently did not
persist between two Bash calls (a recurring quirk of this environment, not the
product). Caught because `setup`'s own output named the worktree's config.yaml
path instead of the scratch one. No product code was touched (`setup` only
creates a config if one is missing, and the worktree's already existed;
verified the real `download_path` was unchanged and deleted the stray
gitignored binary before continuing) - recorded here because Rule 4 asks for
every persona break to be logged, and this one is closer to a research-hygiene
bug in me than in the tool, but it is the reason the walk below re-ran the
build step with every command explicitly `cd`-chained.

#### Finding — a stray debug-output file has shipped in the repo root since the very first commit

`ls` in the fresh clone's root shows `sync_run.log` sitting next to
`README.md`, `LICENSE`, `go.mod` - alongside a fresh clone, before building
anything, this is the first thing a new user's file browser or `ls` shows
them. Reading it: a UTF-16-encoded, three-line transcript reading `Download
path: C:\TEMP\Opal_downloader_test`, `Course patterns: *` - someone's local
test-run output. `git log --follow` shows exactly one commit touching it,
ever: the very first commit in the repository's history (`18f875d`, "Added
Playwright", 2026-07-02). Never referenced anywhere else in the codebase or
docs, and `.gitignore` has no `*.log` rule that would have caught a repeat.

**Cause, named sharply enough to predict where else it shows up:** a debug
artifact from local testing got swept into the initial commit (plausibly a
`git add .` before `.gitignore` existed or was complete) and nothing since has
either deleted it or added a rule that would stop it recurring. Tag: **bloat** -
it exists, cost someone nothing to create, and a new user never needed it. Low
severity (three harmless lines, no credentials, no path relevant to anyone
else), but free to fix.

#### Finding — `list`'s discovery phase is completely silent for ~3 minutes on the CLI, with no indication anything is happening

Ran `./opal-downloader.exe list` with `courses: ["*"]` against the real
account (safe: `download_path` was the scratch clone's own `./downloads`,
never touched since `list` doesn't download anyway). TU-Fast auto-login fired
unattended as expected (one reload retry, then success - matches this
project's own "unattended runs complete 2FA by themselves" finding, nothing
new here). Total wall clock: **3m17s**. Between `Discovery: 4.2s (8 courses)`
and `OPAL_HTTP_DISCOVERY=2 summary: 8 courses, 314 HTTP requests, 2m43.8s`,
the terminal printed **nothing** for the full 2m44s - source-confirmed, not a
capture artifact: `scrapeCoursesHTTPFirst` (`internal/scraper/orchestrator.go`)
calls `s.publishProgress` exactly once, before the per-course HTTP fetch loop
starts, and `internal/scraper/httpdiscovery.go` has no print statements at
all. A first-time user watching this has every reason to conclude the program
hung - nothing on screen distinguishes "working" from "frozen" for the single
longest wait in the whole first-run experience.

**Cause, named sharply enough to predict where else it shows up:** `publishProgress`
exists and is called during discovery, but nothing in `cmd/opal-downloader/root.go`
(confirmed by grep - zero matches for `publishProgress`/`DiscoveryProgress` in
that file) ever subscribes to it; the mechanism is wired to the GUI's SSE
stream only (`internal/gui/sync.go`), not the CLI. **Predicted and confirmed**
without a live run: `sync` shares the identical discovery code path before its
own download phase starts (`internal/syncer/syncer.go`'s per-file `downloaded:`/
`error:` lines only begin *after* `Discovered %d remote files` prints, i.e.
after the same silent stretch this walk measured) - so a `sync` run costs a
CLI user the same ~3 minutes of "is this even running" before any per-file
feedback appears at all. Tag: **friction** - it works, but every first-time CLI
user pays this, and it costs exactly the kind of attention/trust Finding 5
(walk 1, "the button doesn't say how long it takes") was already about, on the
CLI's own hardest-to-miss stretch.

#### Finding — the tool finds 8 course links but silently reports only 6 courses, with no way to tell "genuinely empty" from "discovery found nothing here"

The same `list` run's early line said `Found 8 course links`; the final
summary said `Found 6 courses:` with no line anywhere explaining the other 2.
Source-confirmed (`internal/syncer/syncer.go:701-710`, and independently the
identical pattern at `internal/gui/sync.go:120-127` for the GUI's own course
list) - the final list is built by grouping the **discovered files**, not the
**discovered courses**: any course whose crawl returned zero files simply
never gets a map entry, silently. No "8 found, 2 had no files" line exists
anywhere in either code path.

**Plausible-not-confirmed, per Rule 2 - checked what's cheap, not what needs a
live probe:** the real account's own hand-curated `config.yaml` (the one this
worktree already has, from the maintainer's real setup) names exactly these
same 6 courses by full name, and `list --visit-report`'s accumulated history
(built from many real runs) shows the identical 6 course names and no others.
That is circumstantial evidence the other 2 enrolled courses are genuinely,
consistently empty for this account (plausibly why the maintainer's own config
never listed them) rather than a fresh instance of the silent-partial-loss
pattern this project's sync-speed campaign has spent the most effort chasing
elsewhere (Questions 17/19/22/25) - but it is not the same as confirming it,
and the tool itself gives a user no way to reach that conclusion without doing
what this walk just did (cross-referencing two other data sources by hand).
Tag: **friction** (possibly **wrong** if it ever turns out to be a genuine
discovery loss rather than genuinely-empty courses - that distinction is
exactly what a fix would need to preserve).

**Update 2026-08-12 (same day, autopilot): confirmed, not just plausible.**
Question 5's sync-speed cycle (`docs/sync-speed-model.md`) fixed this walk's
other finding (the silent discovery phase) by printing a line per course as
it finishes - the live verification run for that fix named both previously-
unidentified courses directly: `[WS25/26] Programmierung: 0 files` and
`Helfende DMS: 0 files`. Both are crawled successfully and are genuinely
empty; this is not a silent partial-discovery loss. The **friction** half of
this finding still stands (no "8 found, 2 empty" line exists anywhere), but
the **possibly wrong** half is closed.

#### Ruled out — things that looked like a finding and were not

- *"`status` said 2 days of session validity left; `list` said the session had
  expired and needed interactive login - a contradiction."* It is not one.
  `status` (`internal/sessionstate.Inspect`) reads the storage-state cookie's
  own **claimed** expiry offline; `list`'s live `ensureSession` call actually
  asked OPAL, which rejected the session before its claimed expiry. This is
  precisely the caveat `status` itself already prints ("OPAL can end it sooner;
  if it does, the next sync logs in again by itself") - an unplanned but clean
  live confirmation that the documented fallback path works, not a new gap.

#### New questions this walk leaves (Rule 3)

1. ~~Are the 2 courses missing from `list`'s summary genuinely content-free, or
   an undetected instance of this project's known silent-partial-discovery-loss
   pattern?~~ **Answered 2026-08-12, same day** - see the finding's own update
   above: genuinely content-free, confirmed live.
2. **Does the GUI's own `list`/`sync` progress stream (`internal/gui/sync.go`'s
   `jobEvent{Kind: "log", ...}` publishes) actually cover this same ~3-minute
   discovery gap, or does it have the identical blind spot under a different
   surface?** `publishProgress`/`DiscoveryProgress` is wired to something in
   `internal/gui`, but this walk did not check whether that something renders
   per-course ticks during `scrapeCoursesHTTPFirst`'s HTTP-first loop
   specifically, versus just the one before/after event this walk's grep of
   `orchestrator.go` found. If the GUI has the same gap, Finding 2 above is a
   `publishProgress` gap, not a CLI-only one - if it doesn't, the CLI fix is
   "subscribe to the mechanism that already exists" rather than "build one."
   **Answered 2026-08-12, same day, by Question 5's second sync-speed
   experiment** (see `docs/sync-speed-model.md`): the GUI does not share the
   gap for `sync`. Walk 4 below independently reproduced this live.

### Walk 4 — 2026-08-12, GUI everyday use (second GUI walk)

Rotation: walk 1 (GUI) and walk 2 (CLI) are both 2026-08-11, walk 3 (first-run)
is 2026-08-12 - GUI has gone longest without a fresh look, and walk 1 itself
named the thing this walk should check ("launch it the way a user does and
leave it alone" - open question 1). Scratch env per the recipe: real course
list and folder layout copied into `tmp/friction/config.yaml` with
`download_path`/`session_state_file` redirected under `tmp/friction/`, and the
real session state copied in so no login was needed. Built current master,
launched `gui --port 8877 --config tmp/friction/config.yaml`, drove it from
the Browser pane exactly as walk 1 did (front page, settings, schedule, sync,
TU-Fast, update, feedback), and left it running between checks rather than
closing it.

**Expectation registered before opening it:** walk 1's five findings and four
questions are either fixed or still open; I expect to be able to tell which
just by using the app, without re-reading walk 1's own text first.

Mostly true, and mostly good news - four of walk 1's items are visibly already
addressed:

- **Finding 6 fixed.** The nav item is now "Preview sync, force re-download &
  developer tools", not the old "developer tools" label that hid the two
  options students would actually want.
- **Finding 1/2 partially addressed.** The `/schedule` page now proactively
  names the exact staleness risk walk 1 found invisible: "Your daily sync is
  registered but points at a program that will not keep working
  (`C:\...\main.exe`), so it is not running reliably. Install opal-downloader
  somewhere permanent..." - read-only (this is the maintainer's real scheduled
  task, so nothing was clicked or saved on this page, per the campaign's own
  red-tier rule).
- **Walk 3's question 2 already answered same-day** (see the update on that
  finding above) - not new here, just confirmed still holding.
- **Walk 1's question 2 (does every from-source build show "dev" forever)
  ruled out, not a bug.** `README.md` scopes `go build` explicitly to
  "Build from source (contributors) ... end users should use the installer" -
  the shipped installer is built by `.github/workflows/release.yml`, and
  `cmd/opal-downloader/root.go:120` documents the `-ldflags -X ...buildVersion`
  the release build uses. A contributor's own from-source build correctly
  shows "dev"; that was never the end-user path this question worried about.

#### Question 1 (walk 1): the ~5-minute exit did not reproduce this walk

Same launch method as walk 1 (background shell, `&`, not a double-click - this
walk cannot fully separate "launch method" from "double-click" either, logged
per Rule 4 rather than claimed as a clean test). Left running and polled with
`curl` plus periodic page navigations instead of one continuous stare:
confirmed alive at 200s, 270s, 310s, 340s, 360s, and a live process check via
`Get-CimInstance Win32_Process` at **388s (6m28s)** still showed the PID with
its full command line. Stopped by hand afterward (`Stop-Process`), not by
another silent exit. **This weakens the "real bug" hypothesis without closing
it** - one non-reproduction under the same launch method that produced one
reproduction is not enough to call it a pure environment artifact, but it does
mean the death is not a reliable every-launch behavior. Downgrading from
"question, next walk should check" to "question, needs either a third data
point or the double-click test neither walk could perform."

**CLOSED 2026-08-12 — the double-click test ran, and the GUI survived.** The
maintainer launched it the way a user does (PID 20576, started 15:59:30)
while a watcher polled the process; it was alive through the full 25-minute
observation window and still running at **26 minutes**, roughly five times
past the failure window walk 1 saw. Both outcomes were written down before
the result came back (see `docs/BACKLOG.md`'s entry at the time), so this
closed on a pre-registered rule, not on a reading taken after the fact.

Verdict: an artefact of agent-started launches, not a defect users can reach.
Every death on record came from a process an agent had started from a shell
it also owned; no death has ever been observed under full detachment or a
real double-click. Nothing in the app was wrong and no code changed. **The
standing consequence, unchanged and still worth keeping:** automated GUI
walks default to `Start-Process` or equivalent full detachment - it remains
the only launch method with a perfect record, and it is the right default
whatever the root cause was. If this ever recurs, it will be under an
agent-started launch, and the thing to examine is the parent shell's
lifetime, not the GUI.

#### New questions this walk leaves (Rule 3)

1. **Every GUI settings save silently discards `opal_url` and
   `session_state_file`, resetting both to hardcoded defaults, regardless of
   what was in the config before the save** - confirmed live (saved a
   backslash `download_path` change and watched `session_state_file` in
   `tmp/friction/config.yaml` flip from the scratch path back to
   `~/.opal_storage_state.json`). **Ruled out as a fresh finding, not filed**:
   `internal/gui/settings.go:47-52,289-290` documents this as deliberate
   ("OPAL only has one real-world instance in practice... Advanced users who
   need something different can still hand-edit config.yaml directly"), and
   `internal/gui/settings_test.go:483-488` asserts exactly this behavior on
   purpose. Left as a question rather than dropped entirely because the
   *mechanism* - the settings save silently overwrites any config field the
   form does not expose, not just these two - is real and generalizes; today
   it only touches two fields that happen to equal the defaults for every
   real user of this single-instance platform, but the next field added to
   `config.yaml` without a matching form field would silently lose any
   hand-edited value the same way, with nothing telling the user. Worth a
   comment at the config struct's definition site pointing future editors at
   `parseSettingsForm`, not worth a behavior change today.
2. Walk 1's questions 3 (is 08:00 actively hostile to the logged-off failure)
   and 4 (do the three path conventions behave identically) are still open -
   this walk spot-checked one path convention (backslash absolute in
   `download_path`, round-tripped through Save correctly, verified in the
   saved YAML) but did not check the other two or whether they interact with
   `default_course_folder`'s known doubled-path bug.

### Walk 5 — 2026-08-12, GUI everyday use (third GUI walk, concurrent with walk 4)

Started from the same rotation line walk 4 read ("1 (GUI everyday use),
cycling back") in a parallel session, before walk 4's result was visible -
both walks independently picked GUI next and overlapped rather than
sequenced. Built
current master (`go build -o main.exe .` from the repo root - `./cmd/...` alone
produces a non-executable archive, since `main.go` and `func main` live at the
repo root and `cmd/opal-downloader` is an imported library package, not its
own `main` - a five-minute detour, not a finding), set up `tmp/friction/`
(config, scratch `downloads/`, a copy of the real session state), and drove the
GUI over plain HTTP exactly as `docs/friction-campaign.md`'s "How the GUI is
driven" section describes.

**Expectations registered before opening it:** (1) the landing page's "Sync
now" duration note from walk 1's Finding 5 should either still be missing or
already fixed - worth a quick check since campaign entries don't currently say
which; (2) walk 1's open question 1 (does the GUI process vanish on its own
after ~5 minutes?) should be checkable this time by leaving it running instead
of closing anything; (3) clicking "Sync now" and watching the live log should
show me, as a normal user, whether a real error looks any friendlier than
Finding 3's banner text did.

#### Ruled out — Finding 5 (no duration shown) is already fixed, just never marked as such

The landing page already reads "Downloads new and updated files for your 6
selected courses. **Takes several minutes.**" `git blame` traces the sentence
to `a965ab7` - walk 1's own fix commit. The finding was real when written and
is resolved; `friction-campaign.md`'s Finding 5 section just never said so.
Not a new finding, but worth recording so nobody re-opens it: this file's
"Finding 5" heading below now needs a `(fixed 2026-08-11)` marker, which this
commit adds.

#### Question 1 (walk 1) again: a third data point, and a launch-method lead — still not closed, but `Start-Process` is now the safer default regardless

Run concurrently with, and without visibility into, walk 4's own attempt at
this same question (see above - both walks picked "GUI, cycling back" from
the same stale-looking rotation line before either had published a result).
Where walk 4's single background-shell (`&`) launch survived 6m28s, this
walk's **first instance**, launched the same way walk 1 did (`nohup ./main.exe
gui ... & disown` inside a single shell-tool call), went unreachable
(`ERR_CONNECTION_REFUSED`) roughly 5-8 minutes in, while `tasklist` still
showed it resident with steady, non-growing memory - not a crash, a process
that stopped serving while still "running." A few minutes later it disappeared
from `tasklist` entirely. No error text reached its captured stdout/stderr,
and Windows' Application-Error event log has no entry for it, ruling out a Go
panic or access violation.

**Second instance**, launched via PowerShell's `Start-Process` (a properly
detached child process, structurally closer to what happens when Explorer
starts an app than any shell background job is) against a second scratch
port: still fully responsive and actively streaming a live sync's progress
**12+ minutes** after launch, with no sign of degrading.

**Combined state of this question after three attempts, two walks:** walk 1
died (background shell), walk 4 survived 6m28s (plain `&`, stopped by hand),
this walk died again (`nohup ... & disown`) then survived cleanly under
`Start-Process`. Two deaths and one survival under background-shell launches
is not a reliable every-launch failure, so this does **not** close walk 1's
question outright, in line with walk 4's own conclusion - but the one launch
mechanism that has been clean in every attempt so far is full detachment.
**Recommendation, actionable regardless of the root cause:** future automated
GUI walks in this environment should default to `Start-Process` (or
equivalent), both because it is the only launch method with a perfect record
and because it removes this variable from any future attempt at the question.
Whether the background-shell deaths are this environment's own process
lifecycle management (a Windows Job Object tied to the shell-tool call would
fit the "alive-but-unreachable, no crash trace" shape) or something rarer
remains open - still needs either a fourth data point or the double-click test
no automated walk can perform.

*Break from persona, for diagnosis only:* `tasklist`, Windows' Application
Error event log, and a second launch via a different mechanism, all done after
the first instance had already gone unreachable.

#### Finding — a real per-file download error shows the same raw internals Finding 3 already named, on a surface Finding 3 never checked

Clicking "Sync now" and watching the live log (both the rendered `/sync` page
and its `/sync/stream` SSE feed) against the real account, with the scratch
`download_path`: discovery streamed a "Scanning course N of 6" / "Finished -
found X file(s)" line per course exactly as Question 5's fix promises, then
downloads streamed `downloaded: <course> / <path>` lines - normal, matches
Finding 5's own promised timing (the whole 6-course sync finished discovery in
~87s). Two of "Algorithmen und Datenstrukturen"'s files then failed for real,
and the line shown to me, live, in the GUI's own log was:

> ERROR: Algorithmen und Datenstrukturen / …/Vorlesung_7.pdf - response is
> HTML, browser fallback click did not find downloadable link after 2
> attempts: on https://…: downloadable link not found on page: href-match
> a[href*='…']: **playwright: timeout: Timeout 5000ms exceeded. Call log: -
> waiting for locator('a[href*=\'…\']').first() text-match "Vorlesung_7.pdf":
> playwright: timeout: Timeout 5000ms exceeded. Call log: - waiting for
> getByText('Vorlesung_7.pdf').first()** (page has no click-expandable
> pagination to retry)

The first clause is again a genuinely good error message; everything in bold
is the same kind of glue-on Finding 3 already diagnosed for the
scheduled-sync banner - except this is a **different surface** Finding 3 never
checked (a live per-file error during an in-progress sync, not the
banner) and a **different root cause site**: `internal/scraper/download.go:244`
builds this message on purpose, verbosely, because its own comment
(lines 259-264) explains that a past generic one-line version cost three
separate investigations (PRs #35, #89, #95) their ability to re-derive the
real cause. That is a legitimate reason to keep the detail *somewhere* - it is
not a legitimate reason to put it, unfiltered, in the one channel a normal
user is watching live. **Confirmed on both surfaces that read it**: the GUI's
job log mirrors it verbatim, and `internal/syncer/syncer.go:595` and `:662`
(`fmt.Printf("  error: %s (%v)\n", targetKey, err)`) print the identical full
chain to the CLI's own stdout - so a CLI `sync` user watching a real error hits
the exact same wall, unchecked by any of the three CLI-surface walks so far
because none of them happened to hit a real per-file failure.

**Cause, restated at the level that predicts the fix:** the message is correct
to keep in full *somewhere* (developer diagnosis has already needed it three
times), but there is no split today between "what the user reads" and "what
the next investigation needs" - unlike the connectivity-error case, which
already has exactly that split (`No internet connection… (technical detail:
…)`, from the `netcheck` work that closed the original Finding 3 instance).
The fix this predicts is the same pattern, applied here: a short first clause
for the user, the full chain behind a "(technical detail: …)" tail or
equivalent, on both the CLI's `error:` line and the GUI's mirrored log line.
Not built this walk - filed to the backlog instead, tagged **friction** (the
sync still worked; two files needed a human to notice and retry).

#### Ruled out — things that looked like findings and were not

- *Does reconnecting to `/sync` mid-run show current progress, or a stale/blank
  page?* Navigated away and back while the second instance's sync was running:
  the page picked the job straight back up, live counts and all. No gap here.

#### New questions this walk leaves (Rule 3)

1. **Is the Job-Object-cleanup theory for the first instance's death provable,
   rather than just the best fit for the evidence?** This walk did not attach
   a debugger or inspect the actual job object; it inferred from "no crash
   trace + only the launch mechanism differed between a dead and a healthy
   instance." Worth confirming if a future walk needs to trust backgrounded
   launches for something time-sensitive, but low priority - `Start-Process`
   already sidesteps the question for every walk after this one.

### Walk 6 — 2026-08-13, CLI everyday use (autopilot, phase 2)

Rotation said CLI next (stale line said GUI, but the walk log below it showed
GUI had already had three looks against CLI's one - the same cross-check
walk 3 used). `sync.lock` was held by a concurrent live session for this
run's entire phase 1 and phase 2 (`pid 14804`, later `54224`), so this walk
stayed to commands that don't take the overlap lock (`status`, `--help`,
`schedule status`, config-parse error paths) rather than `sync`/`list`/
`login`, which walk 2 already covered anyway.

**Expectation registered before running anything:** `--help`'s command list
and `status`'s output are the two things a CLI user reads before anything
else; I expect both to be internally consistent with what the app actually
does right now, including things fixed by earlier walks.

`--help` and `status` both held up - the download-path substance check walk 1
found and fixed (`status` now runs the same `os.MkdirAll` a sync does) still
answers `(BROKEN: mkdir Q:/nope: ...)` for a nonexistent-drive path,
live-reconfirmed this walk. No finding there.

#### Finding — the on-logon catch-up trigger `docs/BACKLOG-archive.md` recorded as "shipped" (2026-08-11) has never actually reached the real scheduled task, and cannot until the app is installed somewhere permanent

`schedule status` read `Scheduled daily sync: enabled, daily at 08:00` -
accurate as far as it goes, which is exactly what made this worth checking
further: `/schedule`'s own page promises more than that; the two commands
were compared instead of trusted separately, per Rule 1's registered
expectation. The page (`internal/gui/schedule_page.go:195-198`) states as
plain fact: *"Catches up automatically if the machine was off, asleep, or you
weren't logged in yet, including trying again the moment you next log
in."* `Get-ScheduledTask -TaskName OpalDownloaderScheduledSync` on the real
task shows exactly one trigger, `MSFT_TaskDailyTrigger` - no
`MSFT_TaskLogonTrigger`. The "moment you next log in" clause is false, live,
for the maintainer's real automatic sync, right now.

**Cause, named sharply enough to predict where else it shows up.** The
LogonTrigger fix (`docs/BACKLOG-archive.md`'s "Finding 1's recommended repair
(b) shipped" entry) is real code, live-verified - but only against a scratch
task name, per that entry's own words ("the real
`OpalDownloaderScheduledSync` task was never touched"). Getting it onto the
*real* task requires either `schedule enable` (CLI) or the GUI's
`repairDoomedSchedule` self-heal to actually call `scheduler.Enable()` again,
and both gate on `scheduler.CheckExecutableStable(exePath)` first
(`cmd/opal-downloader/root.go:1379`, `internal/gui/schedule.go:100`) -
checked, by source, not assumed. That function (`internal/scheduler/exepath.go`)
treats *any* path inside a git working tree as doomed
(`findGitWorkingTreeRoot`, walks up looking for a `.git` entry, no depth
limit reason to exclude a checkout's own root). The real task's registered
action is `C:\07_Arbeitszeug\Open_github\Opal_downloader\main.exe` - the
repo root, which contains `.git` at zero levels up, so it is doomed by this
project's own definition, confirmed by `ls "C:/Program Files/opal-downloader"`
etc. all failing (nothing is installed anywhere permanent yet). Both repair
paths refuse to run against a doomed *target*; the GUI path additionally
requires the *running* GUI's own exe to be stable before it will repoint
anything - and every way this project is currently run (main checkout,
worktrees) is itself inside a git working tree. **The prediction this
supports: no scheduler change of any kind - this one, or any future one that
goes through `scheduler.Enable` - can ever reach the real task until the app
is installed via `installer/opal-downloader.iss` to a location outside any
git checkout, or someone runs `schedule enable` once from such a location by
hand.** Nothing in the app currently says that to the user in those terms;
the closest is `/schedule`'s existing doomed-path error ("install
opal-downloader somewhere permanent"), which is accurate but doesn't mention
that *this specific promise* (the sentence two paragraphs above it, on the
same page) is the thing silently not true in the meantime.

Tag: **wrong** - the page states behavior as an unconditional fact, and the
real system does not do it. Not filed as **blocker**: the daily 08:00 trigger
still runs and still self-heals its *own* staleness (Finding 2's original
repair), so sync is not broken, only the specific missed-run-at-logon
promise.

*Break from persona, for diagnosis only:* `Get-ScheduledTask`,
`internal/scheduler/exepath.go`, `internal/gui/schedule.go`,
`cmd/opal-downloader/root.go`, and `ls` against the three most likely install
locations, all read after the mismatch between the page's promise and the
real task's trigger list was already the finding.

#### New questions this walk leaves (Rule 3)

1. **Has `schedule enable` ever actually succeeded from a real installed
   location, on this or any machine?** The installer itself is unwalked by
   the campaign proper (per the "Open findings" note already on this file);
   this walk's finding means that gap now also blocks a specific, real,
   already-promised feature, not just "the installer surface is generally
   unwalked" in the abstract - worth folding into whoever eventually does that
   walk.
2. Should `CheckExecutableStable`/the repair path have an escape hatch for
   someone who knowingly wants to run a scheduled task from a git checkout
   anyway (accepting the `git clean`/branch-switch risk), or is refusing
   outright the correct hard line? Not answered this walk - a product
   question, not a code one.

### Walk 7 — 2026-08-13, first run from zero (autopilot, phase 2), GUI angle

Rotation said first-run-from-zero next (walk 6's own updated line), which the
walk log confirmed was accurate this time - GUI three looks, CLI two,
first-run one. But walk 3 already did a thorough first-run pass for the CLI
build+`list` path; repeating that would violate Rule 3 ("finding nothing
usually means I walked where I already knew the answer"). Walk 3 never
touched `gui`'s own zero-config first load - the README's "Quick Start (Web
UI)" section calls the web UI "the primary, recommended way to use
opal-downloader" and specifically promises the Settings page bootstraps a
missing `config.yaml` with sensible defaults. That claim had never been
walked. `sync.lock` was free for this walk's whole duration (checked before
and after; a concurrent live session held it briefly for an unrelated
sync-speed experiment but released it before this walk needed anything live),
though nothing here needed it anyway - see below.

**Scratch setup, green per "Breaking things safely."** Built current master
(`go build -o main.exe .`), created an empty `tmp/friction-gui-zero/` (no
`config.yaml`, no session state - true zero, not `tmp/friction/`'s "existing
config, redirected" recipe), and ran `main.exe gui --port 8791` with that
empty directory as the working directory, so config/session resolution has
nothing of the real setup to find.

**Expectation registered before opening it:** per the README, a first load
with no `config.yaml` should show a setup/bootstrap experience - pre-filled
defaults on Settings, no error, and since this environment has never synced
or logged in anything, no page should claim otherwise.

**Tooling note, not a product finding:** `computer{action:"screenshot"}` and
coordinate-based clicks failed in this session ("Browser pane is not
displayed") - worked around with `read_page`/`get_page_text` for verification
and `javascript_tool`'s `.click()` for the one button (`Save settings`) that
wasn't reachable through `read_page`'s interactive filter (a `type="submit"`
button apparently outside what that filter surfaces). Logged per Rule 4 even
though it's an environment quirk, not the product, matching walk 3's own
precedent for tooling breaks.

#### Finding — a fresh setup silently reuses whatever OPAL login already exists on the machine, skipping the promised "log in once" step

**Fixed 2026-08-15 (autopilot).** `config.PerInstallStateFile(configPath)`
scopes the implicit `session_state_file` default to configPath's own
directory instead of the fixed global path; wired into config loading and
into the Settings-save/landing-page call sites this finding named. Live-
verified with the same "brand-new scratch config, no login" setup this walk
used, plus a check that the real global session file this walk's own
inheritance depended on still exists on this machine (confirming the old code
would still reproduce it) - see `docs/BACKLOG-archive.md`'s "Done recently"
entry for the full trail. The narrower "Cause, named sharply enough..."
paragraph below stays for the record.

The landing page's own copy states the plan up front: *"What you'll do once:
... Log in to OPAL once. Your login stays on this computer."* Bootstrapped a
`config.yaml` through Settings exactly as README describes (left every field
at its pre-filled default, including `download_path: ./downloads`, clicked
**Save settings**, got `Saved.`) - no login of any kind was performed in this
scratch setup. The very next landing-page load read: `Logged in, valid until
Sun 16 Aug, 13:59 (2 days left).`

**Cause, named sharply enough to predict where else it shows up.**
`internal/config.DefaultStateFile = "~/.opal_storage_state.json"` - one fixed
path under the user's home directory, applied whenever `session_state_file`
is left unset. Not just a fallback: Settings' save path writes this exact
literal string into every newly-bootstrapped `config.yaml` (confirmed - the
scratch config.yaml on disk has `session_state_file:
~/.opal_storage_state.json` written out, not left absent), so it is the
*standing* default for every fresh setup, not an edge case. Because that path
already held a valid, real login (this machine's own real usage), the new
setup inherited it wholesale - no browser window opened, no login step ran,
nothing on the page distinguishes "you just logged in" from "this reused
whatever was already sitting at a fixed shared path." Predicts the identical
outcome for: reinstalling on a machine that was used before, testing a second
OPAL account on one Windows profile, or (this project's own routine
situation) a second worktree pointed at its own fresh `config.yaml` while an
earlier worktree already completed a login - all share nothing except that
one unscoped path. This is a different mechanism from the already-filed
global-status-file findings (walk 1's Finding 1, walk 6): those are read-only
reporting artifacts going stale; this is the actual authentication identity a
"first-time" setup silently starts using. Tag: **wrong** - the page states
"log in once" as the plan and then never does it, without saying so.

*Not tested further, on purpose:* clicking "Sync now" from this state would
have exercised a real, live sync using the maintainer's real session - safe
in principle (`download_path` was scratch-scoped) but unnecessary, since the
finding was already fully demonstrated by the offline session-validity read
alone (the same offline claimed-expiry check walk 3 already established
`status`/the landing page use, not a live OPAL round-trip - confirmed by
source, no network call observed). Left undone rather than run for
completeness.

#### Finding — the same first-run landing page shows a stale, unrelated "Last sync" line directly under its own "First time here?" message

**Fixed 2026-08-15 (autopilot).** `applyLastSync` now suppresses the line
whenever `data.SetupNeeded` is true, rather than rescoping the underlying
record - `statuslog.WriteLastSyncDefault` is deliberately machine-wide (every
config's sync writes the same file), so the schedule banner's own use of the
same file (walk 1's Finding 1, the "Optional, not a commitment" entry) is
unaffected and stays open. Live-verified with a brand-new scratch config
landing page against this machine's real `last-sync.json` - see
`docs/BACKLOG-archive.md`'s "Done recently" entry for the full trail.

Before any config existed (the very first load, before Settings was ever
saved), the landing page read, in order: `First time here? This sets your
download folder and picks the courses to sync.` immediately followed by
`Last sync: 33 minutes ago – 49 file(s) failed.` Reproduced on a second load
a minute later (`34 minutes ago`, same `49 file(s) failed`) - stable, not a
one-off render glitch.

**Cause, source-confirmed (`internal/gui/gui.go`).** The two lines come from
independent, differently-scoped sources on the same render:
`SetupNeeded`/`SyncBlockedReason` (the "First time here?" line) come from
`config.Load(s.configPath)` failing - correctly scoped to *this* setup's
config path. `LastSyncKnown`/`LastSyncWhen`/`LastSyncDetail` (the "Last sync"
line) come from `readLastSyncFunc` = `statuslog.ReadLastSyncDefault` - a
fixed global path, unrelated to `s.configPath`, already named by this
campaign as the root cause of walk 1's Finding 1 (the schedule banner) and
the "Optional, not a commitment" backlog entry. This walk confirms the same
mechanism reaches a second surface: not just a banner about the *scheduled
task*, but the landing page's own headline claim about *this setup's* last
sync, shown while the page is simultaneously telling the same user they have
no setup yet. Tag: **wrong** - lower severity than the login-reuse finding
above (nothing is silently acted on here, it's confusing text, not a silent
identity switch) but the same underlying "state read from a global path
rendered as if it belonged to the current config" cause family.

#### Confirmed working, no finding

- Settings' pre-filled-defaults bootstrap claim from README held exactly as
  described: `download_path` defaulted to `./downloads` (relative to the
  working directory, sensible), saving with every other field untouched
  produced `Saved.` and a valid `config.yaml` on disk with expected keys/
  defaults (`courses: ['*']`, `download_concurrency: 3`,
  `skip_enrollment_sections: true`, etc.) - "saving here is all you need to
  bootstrap a fresh setup" is accurate.

#### New questions this walk leaves (Rule 3)

1. **Should `session_state_file`'s default be scoped per-install (e.g. next
   to `config.yaml`, or derived from its path) instead of one fixed
   home-directory location - or is machine-wide session sharing actually the
   intended, convenient behavior for the common case of one person, one
   machine, one account, and the real gap is only that the GUI never says
   "reusing an existing session" out loud?** Not answered this walk - a
   product question about intended scope, not a code-correctness one. Worth
   deciding before touching `DefaultStateFile`, since "fix" could mean either
   isolating it or just labeling it honestly.
   **Answered 2026-08-15 (autopilot): scoped per-install.** One person/one
   machine/one account was never actually the only real case this project
   itself hits daily - a second worktree with its own `config.yaml` is this
   project's own routine situation, and it shares the same machine. Scoping
   removes the silent-inheritance failure mode outright rather than just
   labeling it; an explicit `session_state_file` still lets anyone opt back
   into sharing one session across configs by hand. See
   `docs/BACKLOG-archive.md`'s "Done recently" entry.
2. Do the GUI's other config-scoped-looking numbers share the same
   global-path leak the "Last sync" line has now been shown twice to have -
   specifically, does `/preview`'s force-re-download or developer-tools page
   read anything through a similarly fixed, unscoped path? Not checked this
   walk; cheap to check next time that page comes up for another reason.
   **Answered for the write side, 2026-08-18/19:** found live during
   Question 44's verification runs that `statuslog.WriteLastSyncDefault`
   itself is unconditional regardless of `--config` (`cmd/opal-downloader/
   root.go`'s `runSync` and `internal/gui/sync.go`'s sync handler both called
   it no matter what config a run used), so a scratch `--config` run could
   clobber the maintainer's real last-sync record. Fixed 2026-08-19
   (autopilot): both call sites now skip the write unless
   `config.IsDefaultPath(configPath)` - see `docs/BACKLOG-archive.md`'s
   "Done recently" entry. `/preview`'s own pages remain unchecked.

### Walk 8 — 2026-08-15, CLI everyday use (autopilot, phase 2)

Rotation said CLI next. Walks 2 and 6 already covered CLI everyday use twice,
so this walk deliberately looked for ground those two hadn't: the bare
no-argument invocation, `--help`, `status`, a real `list` against the real
account starting from an *already-expired* copied session (forcing the
TU-Fast auto-recovery path to actually fire rather than just being read
about), and error text for a typo'd command and a missing config file.

**Scratch setup, green per "Breaking things safely."** Built current master
(`go build -o tmp/friction/opal-downloader.exe .`), copied the real
`config.yaml` into `tmp/friction/config.yaml` with `download_path`,
`default_course_folder`, `course_folders`, and `subfolder_destinations` all
redirected under `tmp/friction/downloads`, and copied the real session state
(`~/.opal_storage_state.json`) to `tmp/friction/session_state.json`,
referenced by the scratch config's own explicit `session_state_file` - the
real file was only ever read, never written to or moved. `sync.lock` was
free before and after.

**Expectation registered before the first command:** running the binary with
no arguments should behave like the near-universal CLI convention - print
usage/help and exit - since that is what most first-time users try first.

**Investigated, not a finding.** No-argument invocation instead launched the
full GUI server in a native window (`Opal Downloader GUI opening in a native
window (http://...)`), which looked like a hang from the outside (the
20-second window before I checked showed zero captured output and a live,
77MB-resident process with no `sync.lock` and no downloads yet). Read
`cmd/opal-downloader/root.go`'s `Execute()` to diagnose (Rule 4 permits
breaking persona to diagnose something already found): `len(os.Args) < 2`
deliberately runs `runGUI(nil)`, documented in the code itself ("the web UI
is the primary/default way most users interact with opal-downloader (see
docs/gui-concept.md Section 5)"). Not a bug - a GUI server is *supposed* to
keep running until killed, and `--help` (checked immediately after) prints
clean, fast, and correct usage text without touching the GUI path at all.
Killed the backgrounded GUI process (scratch-scoped, nothing to repair) and
moved on.

**Investigated, not a finding.** `status` reported the copied session
"valid until Mon 17 Aug, 11:01 (38 hours left)"; the very next command,
`list`, reported that same session file expired server-side and needed
interactive re-login. A discrepancy worth chasing on its face, but the
CLI's own `status` output already carries the exact caveat that predicts
this ("OPAL can end it sooner; if it does, the next sync logs in again by
itself" - matching `internal/gui/gui.go`'s own documented design, "The
expiry is display only"). More likely explanation specific to this walk's
own method than a product bug: copying a *live* session file that the real
environment might still be touching is not a clean substitute for a
genuinely idle one. `list` then exercised the exact self-healing path this
project's memory already documents live-verified (TU-Fast auto-completing
login with nobody at the keyboard): "Login page has not progressed yet
(TU-Fast may not have fired) - reloading it (attempt 1 of 2)..." then a
clean real discovery, 8 courses, 349 files, 73.7s total - consistent with
the sync-speed campaign's own known ~50-90s discovery window. Confirms the
self-healing path works exactly as documented rather than revealing
anything new.

**Confirmed working, no finding:** `--help`'s usage/options text (complete,
accurate against every subcommand actually checked); a typo'd command
(`sycn`) failed with a clean `Error: unknown command: sycn`, exit 1; a
missing `--config` path failed with `Error: config file not found: ...`,
exit 1; the real log file (`~/.opal-downloader/logs/opal-downloader.log`)
correctly split `audience=user` (the same lines the console showed) from
`audience=diag` (per-section skip detail, rate-ceiling stats) - the
`--verbose` doc claim holds. `list` wrote only its visit-log metadata to the
scratch `download_path`, no files - correct, `list` never downloads.

**This walk's own verdict: nothing wrong, blocked, or bloated found** - six
distinct paths through the CLI (bare invocation, `--help`, `status`, a real
`list`, a bad command, a missing config) all matched documented or
already-known-correct behavior, including one path (the expired-session
auto-recovery) that is usually only read about rather than actually
triggered live. Per Rule 6: reported plainly rather than manufacturing a
finding: two candidate discrepancies were investigated and both resolved to
"working as designed," not left unexamined to pad this walk with a false
positive.

#### New question this walk leaves (Rule 3)

`smoke-check` and `dump-links` are both documented, real CLI subcommands
(`--help` lists full flag syntax for each) that no everyday-use walk has
exercised yet - walks 2, 6, and this one all stayed on the six subcommands
above. Does `smoke-check --full-sync` (a real sync into a disposable scratch
directory, per its own `--help` text) or `dump-links` behave as cleanly as
what this walk checked, or is that untrodden ground for a reason? Cheap to
check next time CLI is due: both take the same scratch-config recipe this
walk already built.

### Walk 9 — 2026-08-18, CLI everyday use (autopilot, phase 2)

Rotation said GUI next (four looks vs CLI's three at the time). **Not
possible from this session**: `preview_start` refuses to launch a dev server
from an unattended/scheduled-task session outright ("nobody is present to
approve the command") - there is no browser tool available to drive a GUI
walk here at all, not a persona choice. Fell back to CLI, following walks 6
and 8's precedent of picking whatever the rotation note *actually* allowed
that day. Picked up walk 8's own leftover question instead: does
`smoke-check` behave as cleanly as the six commands walk 8 already checked?

**Scratch setup, green per "Breaking things safely."** `tmp/friction/
config.yaml` - the real `config.yaml`'s course list, `download_path`
redirected under `tmp/friction/downloads`, `session_state_file` pointed at a
copy of the real session token (`tmp/friction/session_state.json`, gitignored
`tmp/`).

**Expectation registered before running:** `smoke-check`'s own `--help` line
- "Local, on-demand check that the crawl still actually works against real
OPAL" - reads like `list` with a pass/fail wrapper, so I expected it to
crawl *this config's* courses and compare against a saved baseline.

#### Finding — `smoke-check` always crawls every enrolled course, ignoring `--config`'s `courses:` filter entirely

Ran it against the 6-course scratch config; it found and baselined against
**8** courses, two of which (`[WS25/26] Programmierung`, `Helfende DMS`)
are not in `config.yaml` at all. Source-confirmed:
`internal/smokecheck/smokecheck.go:212` calls
`sc.ScrapeWithSavedSession(ctx, []string{"*"})` - hardcoded, `cfg.Courses`
never read. Tag **friction**: may well be the intended design (checking the
whole account's reachability rather than one config's slice of it is a
defensible smoke-test goal), but nothing in `--help` says so, and the
identically-worded-sounding `list` command respects the filter - a user has
no way to know these two commands disagree here without reading source.
Full detail and the baseline-file side note: `docs/BACKLOG.md`'s friction
section.

#### Finding — Softwaretechnologie dropped out of course discovery entirely on the second of two `smoke-check` runs, eight minutes apart

First run: `Softwaretechnologie (SoSe 26): 0 files (0.3s)`. Second run,
started immediately after to check whether the first was reproducible:
Softwaretechnologie is not in the per-course output *or* the baseline
breakdown at all, despite both runs reporting the same "Found 8 course
links" count first. No error, timeout, or skip message anywhere in the CLI
output explains the gap. **Read this with the load this session put on the
account already priced in**: by the time of run 2, this same session had
already run two full live syncs (Question 44's verification, ~35 minutes of
crawling) and one smoke-check against the same account, and run 2 itself
needed an interactive TU-Fast relogin mid-run (the saved session had
expired) - unusually heavy, self-inflicted churn, not a normal day's use.
Filed as a **question**, not a bug claim, because it sits next to Question
44's own open cause question (`docs/sync-speed-model.md`: does a second
course's presence perturb Softwaretechnologie's state) closely enough that
a future cause-hunt pass should know course-level dropout, not just
file-level HTML failures, showed up once. Full detail:
`docs/BACKLOG.md`'s friction section.

**This walk's own verdict:** two findings, both filed - `smoke-check`'s
course-filter surprise (friction, source-confirmed, likely by design but
undocumented) and the Softwaretechnologie dropout (a question, explicitly
not chased further given the confound named above).

**Closed 2026-08-19 (autopilot, phase 1):** kept the behavior (an
account-wide reachability check is a defensible smoke-test goal, and
changing it would narrow what the command actually verifies), fixed the
"undocumented" half instead - the top-level command list and the
`smoke-check`-only options block in `--help` now both say plainly that it
ignores `courses:` and always crawls everything, and
`internal/smokecheck/smokecheck.go`'s `Run` carries a comment at the `"*"`
call pointing back to this walk. The Softwaretechnologie-dropout question
is unaffected and still open.

*Break from persona, for diagnosis only:* read
`internal/smokecheck/smokecheck.go` once the course-count mismatch was
already observed live, to find the `[]string{"*"}` line and its baseline
path.

#### New question this walk leaves (Rule 3)

A clean repro of the Softwaretechnologie dropout - one `smoke-check` run
against a rested session, no other activity that hour - would separate "real
course-level instability, same family as Question 44" from "this session's
own unusually heavy testing load." Nobody has run it that way yet; every
data point on this specific symptom so far comes from a session that was
already hammering the account for an unrelated reason.

### Walk 10 — 2026-08-19, first-run-from-zero (autopilot, phase 2), CLI angle

Rotation said GUI or first-run next (both tied at last touched 2026-08-13,
Walk 7; CLI was Walk 9, just done, so excluded by the "don't repeat" rule).
Tried GUI first, in persona: `preview_start` with the repo's own
`opal-gui-friction` launch config. Refused outright - *"Dev servers can't be
started from unattended sessions (scheduled-task runs and remote-dispatched
trees) - nobody is present to approve the command."* Same wall Walk 9 hit for
plain CLI-vs-GUI reasons; confirms it also blocks a *named* launch config, not
just the generic dev-server path. Fell back to first-run-from-zero, done via
the CLI as Walk 3 already established that surface doesn't require the GUI.
Went further than Walk 3, which stopped after `list`: this walk tried to
reach an actual completed `sync`.

**Scratch setup, green per "Breaking things safely."** A genuine fresh
`git clone` of the real repo into an unrelated scratch temp directory (not
`tmp/friction/` - true zero, no config.yaml, no session state file of its
own). Followed only `README.md`, as a first-time contributor would: `go build
-o opal-downloader.exe .`, then `setup` (installs Playwright browsers,
creates `config.yaml` from the example - both succeeded cleanly, no
surprises, matches the README's own description exactly). Left the generated
`config.yaml` almost untouched (`download_path: "./downloads"`, contained
inside the scratch clone - the one field the README tells a first-timer to
check), which also means `session_state_file` stayed at its documented
default `~/.opal_storage_state.json` - the real, shared, home-directory
session. Reading it is the project's own intended "one login reuses
everywhere on this machine" design (see `docs/BACKLOG-archive.md`'s
2026-08-15 entry closing Walk 7's session-reuse finding), so this is not a
red action - no login profile or download folder here is real, and
`download_path` was never anything but the scratch clone's own subfolder.

**Expectation registered before running `login`:** having just followed
`setup`'s printed next steps literally, I expected `opal-downloader login` to
open Chromium, complete TU-Fast auto-login unattended (this project's own
standing finding), and finish in seconds.

#### Finding — a first-time user who hits the real single-instance lock gets a bare PID and no guidance on what to do

`login` refused instantly: `Error: a sync is already running (PID 39684,
started at 2026-08-19T09:06:47Z)`. Not a fluke or a self-collision - checked
(persona break, diagnosis only): `Get-Process -Id 39684` names
`C:\07_Arbeitszeug\Open_github\Opal_downloader\main.exe`, the *real* checkout,
started 2026-08-19T11:06:12 local time - this is almost certainly the real
daily scheduled sync, running for real, on this machine, at the moment this
walk happened to try `login`. `status` (offline, no lock needed) confirmed
the account's session is otherwise healthy - "valid until Sat 22 Aug, 2 days
left." Source-confirmed the message and the design intent
(`internal/synclock/synclock.go`'s `ErrHeld`, `docs/OPERATIONS.md`'s own
line: *"That is the intended outcome, not a bug - wait for the first to
finish"*) - the lock itself is correct, deliberate, and already documented
for whoever maintains this code. The gap is narrower and still real: nothing
in the message itself, which is all an actual first-time CLI user sees,
says any of that. No ETA, no "this is probably today's scheduled sync,"
no "try again in a few minutes" - just a PID and a timestamp, which reads
like an internal diagnostic dump to someone who has never heard of this
project's lock file. Tag: **friction**. **Predicted and confirmed without a
second live run:** `ErrHeld` is one shared error
(`internal/synclock/synclock.go`) returned identically by `sync`, `list`,
and `login` alike (`docs/OPERATIONS.md`'s lock table) - so this is not a
`login`-specific rough edge, every command that touches the account hits the
same bare message under the same real-world condition (a scheduled sync
overlapping a manual attempt), which by design happens roughly once a day
for anyone who also uses the CLI/GUI by hand.

*Break from persona, for diagnosis only:* confirmed the holder PID via
`Get-Process`, read `internal/synclock/synclock.go` and
`docs/OPERATIONS.md`'s lock table after the refusal was already observed
live.

**This walk's own verdict:** the real overlap-guard fired on a genuine
first-attempt collision with the account's actual daily scheduled sync -
not a scratch-environment artifact, the most "real" a friction finding gets
in this campaign. One finding filed. The completed-`sync` half of this
walk's goal (going further than Walk 3) could not be reached: waiting out a
real scheduled sync's full duration was judged not worth blocking this run
for, so the walk stops at `login`'s refusal rather than a finished download.

#### Finding — `smoke-check` never takes `sync.lock`, so it can run a real crawl concurrently with a real `sync`/`list`/`login`, exactly the condition that has already caused two silent-failure incidents

Cheap to check (Rule 2's "check the prediction where checking is cheap"),
so checked immediately rather than left as a question:
`docs/OPERATIONS.md`'s lock table credits "every live probe test in
`internal/scraper`" as a holder via `beginLiveProbe` - source-confirmed that
is literally true, but only of the package's own `_test.go` probes
(`internal/scraper/probelogging_test.go`'s `beginLiveProbe`, referenced by
name in every `*_probe_test.go` file). The *production* path
`smoke-check` actually runs - `cmd/opal-downloader/root.go`'s
`runSmokeCheck` calling `smokecheck.Run` calling
`sc.ScrapeWithSavedSession` directly - calls `acquireCrawlOverlapLock`
nowhere. `list`, `sync`, and `login` all take the lock (grep for
`acquireCrawlOverlapLock` in `root.go` confirms it guards exactly those
three); `smoke-check` is a fourth command that opens the same authenticated
Playwright session against the same OPAL account and is not in that list.

Tag: **wrong**, not just friction - `docs/OPERATIONS.md`'s own words for
why the lock exists at all: *"concurrent crawls present one authenticated
identity to a Wicket backend that is stateful server-side per session"*,
and names two real incidents this already caused before the lock existed
(2026-08-02's raw Playwright launch timeout, 2026-08-06's silent 0-file
collapse - both in `docs/BACKLOG-archive.md`). `smoke-check` reopens
exactly that hole for itself: it is a real, unattended-safe,
TU-Fast-triggering command a user or a scheduled task could run at any
time, including the exact minute a scheduled `sync` is already crawling -
this walk's own login attempt above proves that overlap is not rare, it
happens roughly daily. **Retroactively relevant to Walk 9's own open
question**, filed 2026-08-18: that walk ran two full `sync`s and two
`smoke-check`s against the real account within about an hour and saw
Softwaretechnologie drop out of discovery entirely on the second
`smoke-check`, unexplained. Whether any of those runs' wall-clock windows
actually overlapped was never checked (walk 9 treated the load as
"unusually heavy" in aggregate, not specifically concurrent) - but this
finding means concurrency was *possible* the whole time, since nothing
would have stopped it, which walk 9 did not know to check for. This does
not confirm walk 9's dropout was caused by an actual overlap, only that the
mechanism this project has already twice traced session-corruption
symptoms to was live and unguarded during the exact session that produced
the dropout.

Fix, matching the existing pattern exactly: `runSmokeCheck` should call
`acquireCrawlOverlapLock`/release the same way `runList` already does,
before `smokecheck.Run`. Left for a Phase 1 pass (walks file, Phase 1
fixes) rather than fixed inline here, but flagged as high priority given
the specific, already-twice-real failure mode it reopens.

*Break from persona, for diagnosis only:* grepped `root.go` for
`acquireCrawlOverlapLock` and read `probelogging_test.go`/`probeoverlap_test.go`
after noticing `runSmokeCheck`'s code (already open from documenting the
walk above) had no lock call visible.

**Checked while cheap (Rule 2):** `dump-links` (`runDumpLinks`) has the
identical gap - no `acquireCrawlOverlapLock` call, same `sc.Close()`/
`closeBrowserOnInterrupt` shape as `smoke-check`. Lower real-world risk
(a maintainer-only debugging tool, not part of any automation or documented
end-user workflow - its own doc comment says so), so not filed as its own
finding, but the fix below should cover both commands, not just
`smoke-check`.

**Swept completely (Rule 2), not left as a question:** every
`scraper.New(` call site in `root.go` cross-checked against every
`acquireCrawlOverlapLock` call site. Five commands construct a scraper:
`login` (locked, line 553) and `list` (locked, line 696) call
`acquireCrawlOverlapLock` directly in `root.go`; `sync` locks one layer
down, inside `internal/syncer.SyncCoursesWithProgress`
(`docs/OPERATIONS.md`'s table, and live-confirmed by this walk's own
`login` refusal - the real holder PID was a real `sync`); `dump-links` and
`smoke-check` call neither, anywhere. Coverage is complete: exactly two
commands have the gap, no third exists.

#### New question this walk leaves (Rule 3)

Given the fix is a one-line, already-proven pattern (`list`'s own
`acquireCrawlOverlapLock`/`defer releaseOverlap()` pair, copied into
`runSmokeCheck` and `runDumpLinks`), is there a reason it was never added
for these two - deliberate (smoke-check is meant to run *during* a sync, to
test something about concurrent access?) or just an oversight from
`smoke-check`/`dump-links` being added after the lock, or predating it?
Not answered this walk - worth a maintainer call before the mechanical fix
lands, in case "smoke-check ignores the lock" is intentional the same way
Walk 9's course-filter finding turned out to be.
