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

**Next surface: 2 (CLI everyday use).** Walk 1 was the GUI. Keep this line
current at the end of every walk — it is what an unattended run reads to avoid
walking the same surface twice, which is the cheapest way for this campaign to
quietly stop finding anything.

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

#### Finding 5 — the main button never says how long it takes

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
