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

---

## Now

### The 2026-07-26 feedback batch needs your eyes
**Blocked:** on you looking at it. Everything an agent can check is checked.

All ten items are done — "Done recently" says what each turned out to involve,
and several turned out to be a different problem than the one reported.

What is left is judgement, not verification. Six pages changed shape: the sync
log, the settings page, the new `/schedule` page, and the course picker. Every
claim about how they read is a test assertion or a screenshot, and neither of
those can tell you whether a page makes sense to the person in front of it.

The one part still worth deciding: **`internal/scraper/crawl.go` (1250 lines) is
deliberately not split.** It is the most correctness-sensitive file here, with a
documented history of *silent* file loss from changes made to it, and tidying it
buys nothing that justifies going near it. Said out loud because "the big file
stayed big" should be a decision on the record rather than an oversight.

Code size otherwise stays a standing rule rather than a backlog item: keep the
big files from growing while touching them. Two byte-identical splits landed
this session (`gui.go` 1154 → 835, `settings.go` 1028 → 512).

### Sync speed: measured for the first time, and the lead is real
**Blocked:** every concrete next step needs either the maintainer (the
preview-blocker repeat needs a hand-run `login`) or a genuinely new idea this
file hasn't found yet (a DOM-level completion signal or an alternate OPAL
view — searched for once, 2026-07-27, nothing concrete found; still worth
another look if anyone has a lead). The parser only reads this heading's
first line, and it previously said "Not blocked" from the debounce
measurement below — correct when written, stale once that measurement
finished, and it was letting the resume-runner/autopilot gates count this as
actionable unattended work with nothing they could actually do. Two thirds
of a run is this tool waiting on its own timers, for context on what was
measured.

**2026-07-27, live run, 280 sections, 216.6s:** settle wait 94.2s (63% of
in-section time), stability poll 49.5s (33%), actual extraction 4.3s (**2%**).

The dominant cost is a debounce, and a debounce always costs its own duration.
`mutationObserverDebounceMs` is 300ms; the measured average settle wait is
336ms. The page finishes rendering in ~36ms and then we spend 300ms proving
nothing more is coming — ~84s per run, ~39% of the total, as a fixed toll for
silence.

**Do not just lower it.** Same class of mistake as lowering
`sectionContentRequiredStableReads` from 4 to 1, which was live A/B tested and
lost files byte-for-byte like the unfixed code. The debounce exists because
Wicket renders in stages.

The real question, in `docs/sync-speed-campaign.md`: both mechanisms *infer*
completion from absence of change, which costs the same 300ms whether the page
took 20ms or 2s. Is there a positive signal instead?

**The network-layer answer is no, measured 2026-07-27 and now closed.** The
proposed signal — "`AJAX_CALL_DONE` fired *and* the response carried the
file-table markup" — assumed the file table arrives in an AJAX response. It
does not. A live network trace of two courses (5 sections / 38 files, and the
160-section / 207-file Softwaretechnologie) recorded 263 and 8154 responses, of
which **2 and 3 were xhr, and every one of them was the already-handled
`showAllLink` expansion**. An ordinary section's initial render fires no AJAX
at all, confirming `navigation.go`'s existing claim over the campaign entry's
premise. There is no event to key off, so the 300ms toll stands.

Re-checkable in one command: `OPAL_NETWORK_TRACE=1 go test ./internal/scraper/
-run TestNetworkTraceDuringSectionCrawl -v` (add
`OPAL_NETWORK_TRACE_COURSE="<exact configured name>"`; results are written to
`tmp/`).

**But the trace found a bigger thing on the way, and this is the live lead.**
Discovery downloads ~29 MB of the course's own files that nothing ever reads.
Same run, one course, listing filenames only:

| document responses | count | in the main frame | bytes |
|---|---|---|---|
| section pages (`/opal/auth/…`) | 324 | **324** | — |
| the files themselves (`/opal/FolderResource/…`) | **72** | **0** | **30.6 MB** |
| other | 12 | 0 | 0.1 MB |

OPAL course nodes that show their file inline make the browser fetch the whole
file to render a preview, in a subframe — and this codebase has *no iframe
handling at all*, so nothing reads it. `crawl.go:1147` already keeps file links
out of the crawl queue, so this is the page doing it, not us following links.

Why it is worth doing next: it asks OPAL for **less** rather than for the same
things faster — the one direction `docs/server-load.md` encourages — and it may
attack the 94.2s settle wait at its cause, since a multi-megabyte PDF loading
into an iframe keeps generating the very mutations that debounce waits to stop.

**Approved, built, verified — and it is not a speed fix. Confirmed twice.**
Off by default; `OPAL_BLOCK_FILE_PREVIEWS=1` enables it. Two paired
full-account A/Bs, both 2026-07-27:

| pair | previews kept | previews blocked | files | delta |
|---|---|---|---|---|
| 1 (morning) | 248.3s | 324.3s | 345 / 345 | +30.6% |
| 2 (evening) | **210.3s** | **265.0s** | **345 / 345** | **+26.0%** |

Pair 2 settles what pair 1 could not. Its baseline (210.3s) is the fastest run
this account has ever recorded, so the first pair was not a slow day — and the
slowdown reproduced at a similar magnitude. This is a real cost, not noise.

**Safety: settled, twice.** The `diff` of the two sorted file lists — course,
section, name and URL for every file — was **empty in both pairs**. Nothing is
lost. A file count would not have been acceptable evidence here and both of
this project's known losses would have passed one.

**Speed: came back the wrong way, and stayed there.** ~26–31% slower across
two independent pairs. Opt-in, and now on measured grounds rather than on one
unreplicated comparison.

**What it still buys regardless: ~30 MB per course per pass that OPAL does not
have to serve.** That is a `docs/server-load.md` win on its own terms, and may
justify enabling it even if it is slower — a judgement call, not an assumption
to bake into a default.

**The one recorded guess for the slowdown is now dead, measured.** It was that
an aborted subframe leaves the parent churning over an error state — precisely
what the 300ms settle-wait debounce watches for — so `route.Fulfill` with an
empty body might behave differently from `route.Abort`. Same session, same
evening, 345 files, list byte-identical:

| refusal | wall clock |
|---|---|
| `route.Abort("blockedbyclient")` | 265.0s |
| `route.Fulfill` empty 200 `text/html` | **272.0s** |

**How the request is refused does not matter.** `previews.go` argued the
opposite in a comment; the argument was reasoned, never measured, and wrong in
both directions. Recorded in that file so nobody spends a fourth run on it.

**And that follow-up answered it. The slowdown was never the blocking — it is
`ctx.Route` itself.** Same session, same evening, 345 files every run, every
list byte-identical against the no-route ground truth:

| condition | wall clock |
|---|---|
| no route installed at all | **210.3s** |
| route + `Abort` | 265.0s |
| route + `Fulfill` (empty 200) | 272.0s |
| **route installed, always `Continue`** | **274.6s** |

The last row is the finding: install the route, block **nothing**, and the run
still costs ~64s more. Every explanation this campaign had written down for the
slowdown was about the blocking, and all of them were wrong.

**Two things follow, and the second is bigger than this item.**

1. The ~30 MB saving is real and its price tag belongs to something else. The
   blocker is not a speed/traffic trade-off; it is a free saving sitting behind
   an expensive delivery mechanism.
2. **`ctx.Route` costs ~30% of a run on this workload**, which is a fact about
   this codebase's tooling and not about previews. Anything else that reaches
   for request interception — the network trace probe already does — is paying
   it, and any past measurement taken with a route installed is suspect.

**Answered, and the tax is fixed.** The same route registered under
`**/no-such-path-xyz/**` — a pattern that matches nothing, so the handler never
fires once — came back at **272.2s**. Full picture, one session, one evening,
345 files and a byte-identical list every single time:

| condition | wall clock |
|---|---|
| no route installed at all | **210.3s** |
| route + `Abort` | 265.0s |
| route + `Fulfill` (empty 200) | 272.0s |
| route installed, always `Continue` | 274.6s |
| route under a pattern matching **nothing** | **272.2s** |

**`ctx.Route` costs ~30% of a run just by existing.** Not the pattern, not the
handler, not the blocking. A narrower pattern cannot rescue the saving; that
was exactly the hypothesis this row was run to test.

**Where that leaves the preview blocker:** the ~30 MB per course per pass is
real, otherwise-free, and stuck behind a delivery mechanism costing ~64s.
Shipping it means dropping request interception for something browser-level
that stops the fetch without a route. Nobody has looked for that yet — it is
the one open thread here, and it is a genuinely new direction rather than a
re-run of a rejected one.

**Checked immediately, because it would have been the bigger prize:** nothing
in the normal code path installs a route. `previews.go` is the only
`ctx.Route` in the repo and it is off by default, so a routine sync pays none
of this. No free 30% was sitting there.

**But this does invalidate measurements taken with a route installed** —
including the network trace that discovered the 30 MB in the first place. Its
*finding* stands (the bytes are really fetched; that is a count, not a
timing), but any timing from a traced run is inflated by roughly a third and
should not be compared against untraced numbers.

**No longer blocked: the session is fresh again (2026-07-27 18:31).** The
maintainer ran the GUI by hand and TU-Fast completed Shibboleth on its own in
**5 seconds** (18:31:05 opened OPAL → 18:31:10 saved state), so TU-Fast is
*not* broken — the earlier 13:53–14:00 failure (`timed out after 300000ms
waiting for the OPAL course list after login`) was an unattended run against
an expired session with nobody present, exactly the case `CLAUDE.md`
describes. The A/B repeat can now be run.

Also unexplored, and now second in line: a DOM-level completion marker Wicket
sets itself, or an OPAL view that serves the file listing without the staged
client-side render.

of the crawl's concurrency model in the most correctness-sensitive part of
this codebase, with a documented history of *silent* file loss from past
concurrency changes - it needs the maintainer's explicit sign-off before being
built (see the last entry in `docs/sync-speed-campaign.md`). Nothing else
here is unblocked without new evidence; re-measuring or re-arguing already-
rejected approaches wastes a round trip.

**The live lead, and tonight's measurements are what produced it.** Two facts
that were never put next to each other:

1. The network trace found that **an ordinary section's initial render fires no
   AJAX at all** — the file table arrives in the initial document, not in a
   later response. (Measured 2026-07-27; it is why the AJAX-completion-signal
   idea died.)
2. The settle wait costs 94.2s (63%) waiting for DOM mutations to stop, and the
   stability poll another 49.5s (33%). Actual extraction: **4.3s**.

If the file table is already in the initial HTML, then **the mutations the
debounce is waiting out are not the file table arriving.** The obvious
candidate for what they actually are is the inline file previews — 72 per
course, multi-megabyte, loading into subframes and churning the DOM the whole
time. That is the same thing the preview blocker targets, and it would explain
why blocking them *should* help while `ctx.Route`'s fixed ~30% tax buries the
gain (measured tonight, see above).

**The experiment that settles it, and it is a diff rather than a timing:** read
the file rows immediately at `domcontentloaded`, before any settle wait, and
compare against what the current stable-wait path returns — for all 280
sections, byte for byte. If they match, the entire 143s of settle-plus-poll is
removable and the ~30s target stops being unreachable. If they differ, the
sections where they differ are the whole problem, and there will be few enough
to look at individually.

This is not a re-run of a rejected approach. Every previous attempt tried to
make the *wait* cheaper or shorter; this asks whether the wait is needed at
all, which no measurement here has ever tested. Note the trap the project
already fell into once: `sectionContentRequiredStableReads` 4→1 lost files
byte-for-byte, so **a file count is not acceptable evidence** — only the diff
is.

Standing goal (2026-07-21, `docs/sync-speed-campaign.md`): a routine no-op
sync should feel instant, target ~30s. That file is the full decision log -
read it before touching this, several plausible-looking approaches (HTTP-fast-
path discovery, hash-based change caching, an OPAL notification signal) were
already built and live-tested against the real account, and are rejected for
concrete, measured reasons. Re-litigating those without new evidence wastes a
round trip.

Re-measured 2026-07-23 against the real account: 334.1s no-op sync, of which
discovery/crawling is ~322s (96.5%) and one course alone
(Softwaretechnologie, 160 of the account's 284 total sections) is 168s of
that. One free fix already applied: the live config still had
`course_concurrency: 1`, a stale value from before the campaign raised the
default to `2` - bumped to match.

**The last unexplored axis is now explored, and it is dead (2026-07-26).**
Section-level concurrency — parallelising *within* a course's BFS crawl — was
built with the maintainer's sign-off and rejected on measurement:

| section concurrency | files | wall clock |
|---|---|---|
| **1 (ground truth)** | **345** | 227.9s |
| 2 | 257 | 147.2s |
| 4 | 214 | 110.7s |

Every tab added makes it faster and loses more. Off by default; the machinery
and `--section-concurrency` stay so a future attempt can re-measure in one
command. Full evidence in `docs/sync-speed-campaign.md`, including why this is
structural (several tabs inside the *same* Wicket-stateful course tree) rather
than a wait being too short — the lossy runs produced **zero** warnings and
perfect structure, losing only file rows.

So every item on the campaign's leverage list has now been tried. **The ~30s
target is not reachable by any approach identified so far**, and the honest
position is that this needs a genuinely new idea rather than another attempt at
the existing ones. The one lead nobody has pulled on: the file table arriving
via Wicket AJAX *after* the document is what makes every section cost ~1s and
what makes concurrency unsafe — anything that gets the file list without
waiting for that render (a server-side listing endpoint, a different OPAL view)
would attack the cause instead of the symptom. Item 1 (HTTP-first discovery)
tried the closest thing and was rejected for concrete measured reasons; read
that entry before reaching for it again.

### Dogfood the whole first-run journey
**Blocked:** all four decisions below shipped on 2026-07-26 (first-run
introduction, "List courses" renamed, scheduling walked, picker explained),
plus one bug that only looking could find. What is left is the maintainer
opening the GUI and saying whether it now reads well — their eyes, not a test.
Everything an agent can check here is checked.

The four questions this was blocked on were answered by the maintainer on
2026-07-26. Their decisions, now delivered:

1. **A first run needs a real introduction.** Not just an unhidden picker — the
   start / no-courses-configured state should actually explain what to do.
   ("nein, es sollte beim start/nicht konfigurierten Kursen eine gute
   Einführung geben.")
2. **Rename "List courses".** The name is wrong twice over: it costs a full
   crawl, and listing courses is not really what it does. ("Ich meine, das
   macht es ja auch nicht.")
3. **Making it faster stays the dream, not this task.** The maintainer's
   position: past attempts were hard, but they believe it is possible — that is
   the sync-speed item above, which still needs their explicit sign-off before
   the section-level concurrency rewrite is attempted. Not folded in here.
4. **Walk scheduling.** Approved ("jo, mach mal"), including that it registers
   a real scheduled task on their machine.

Drive the GUI as a real first-time user — no config, through setup, login,
course selection, a sync, status, scheduling, then changing a setting — and
write down everything broken, confusing, or annoying. Findings get fixed if
trivial, filed here if not.

Explicitly from the perspective of a TU Dresden student who is *not* the
maintainer: a stranger's first run is in scope.

**First pass done (2026-07-23), no-credentials part only:** a fresh binary in
an empty folder, no config, walked through landing/settings/TU-Fast/sync in a
real browser. All findings from this pass are now fixed (see "Done recently"
below) except one deliberately left open: the "three status boxes" finding
led to hiding the login-state box before setup (it's meaningless with no
config to log into), but the update-check box was kept — knowing whether
you're on a stale binary seemed worth the one extra box regardless of setup
progress, and that's more a taste call than the login box was. Revisit if it
still feels cluttered.

**Scheduling/login/sync exercised for real (2026-07-23), but not through the
GUI:** fixing the scheduled-task working-directory bug (see "Done recently")
required actually triggering the real Windows Task Scheduler task against
the live account, which incidentally exercised login (interactive relogin
path), a real sync (2 downloaded, 342 skipped), and the scheduled-run path
end to end. That's real signal the underlying mechanics work, but it's not
the same as clicking through the GUI as a stranger would.

**The journey is now a permanent test (2026-07-26), not a one-off probe:**
`internal/gui/first_run_journey_test.go` walks no-config → landing → settings
form → course selection → save → landing/sync moving on → changing a setting
afterwards, against the real handlers and a real `config.yaml` in a temp dir.
Every other test in the package hits one handler with a prebuilt config; what
was missing was each step's on-disk result being the next step's input.

**Finding from writing it — a real structural fragility, now pinned.**
`parseSettingsForm` rebuilds `course_folders`, `subfolder_destinations` and
`section_folder_names` from the submitted rows *alone*; nothing in the handler
preserves them. The only thing standing between a returning user and silent
data loss is that `GET /settings` renders those rows as real server-rendered
form controls, which a browser then resubmits natively. That invariant was
load-bearing and completely untested — a template change dropping a `value=`
attribute would have made every save quietly wipe the user's mappings. It is
the same shape as the incident below, which is what made it worth looking for.
*Mutation-tested: removing one `value="{{$row.Key}}"` fails the test.*

**The browser walk is no longer blocked (2026-07-26).** The obstacle was
recorded as "the WebView2 window can't be driven here", but the window is only
a viewer for a plain local HTTP server, and the repo already ships Playwright's
Chromium. `internal/gui/browser_walk_test.go` serves the real mux over
`httptest` and drives it with headless Chromium: same pages, same JavaScript,
no window on anyone's desktop. Opt-in (`OPAL_GUI_BROWSER_WALK=1`), following
`internal/scraper`'s probe precedent, since a fresh clone has no browsers
installed; ~4s when it does run.
`Run`'s route table moved into `newMux` so the walk exercises the real routes -
wiring handlers up by hand in a test passes happily when a route is registered
at the wrong path or not at all.

**This is what the handler-level pass could not see.** Course selection is
largely JavaScript: "+ Add course" and "Find my courses" build their
`course_row_name[]` inputs client-side, so those rows exist in no
server-rendered HTML. *Mutation-tested, and the result is the argument for
keeping it: renaming the JS-created input to `course_row_name` (a row that
silently never submits) fails the browser walk and leaves the handler-level
journey test green.*

**First-run finding, not a bug:** with no config, `config.Load` defaults to the
wildcard course list, so "Sync all courses" renders checked and the whole
course picker is hidden behind it. Syncing everything is a reasonable
low-friction default, but a stranger who wants specific courses has to guess
that unticking a checkbox reveals the picker. Pinned in the walk as intended
behaviour rather than changed unilaterally - worth a maintainer's opinion.

**Every page in the nav now loads in a real browser too** (`/`, `/settings`,
`/sync`, `/tufast-setup`, `/update`, `/feedback`): HTTP 200, no uncaught
JavaScript on load, a heading, and a link home. That last one matters more than
it looks - the real window is WebView2 with no address bar and no back button,
so a page without a link home is a dead end the user cannot leave.
*No findings: all six were already clean. Mutation-tested by removing
`/feedback`'s back link, which fails the check.*
One thing worth knowing rather than fixing: `/sync` opens its SSE progress
stream on load and holds it, so the page never reaches network-idle even when
nothing is syncing. That is the page working; a walk that waits for idle there
times out on a healthy page.

**The live half is now covered too** (`internal/gui/live_list_walk_test.go`,
opt-in via `OPAL_GUI_LIVE_LIST=1`): "List courses" clicked in a real browser
against the real account, driving the parts the other walks stop short of —
reusing the saved session, the background job, the SSE progress stream, and the
result rendering. *Verified live: 6 courses reported, 345 remote files
discovered, and the run asserts the real session file is byte-identical
afterwards.* It reads the real `config.yaml` but writes its own in a temp dir
and works on a **copy** of the session state, so the 2026-07-23 config-wipe is
not repeatable here — there is no real path for it to write to.

**Finding: "List courses" is not a quick list.** It crawls every section of
every course, costing the same as a sync's discovery phase — measured at 210s
and 482s in two runs on the maintainer's account. The button sits next to
"Sync" and reads like a cheap lookup, so a user clicking it to see what is
there waits minutes with no warning. Worth either renaming, warning up front,
or serving from the dashboard listing rather than a full crawl; which of those
is a product call, so it is filed rather than decided.

**Scheduling walked (2026-07-26), and it turned up a hazard in the walks
themselves.** Rendering `/settings` calls `applyScheduleStatus`, which can
*write* to Task Scheduler during a page render (`repairDoomedSchedule`
re-points a registration whose executable looks doomed). A GUI test's
`os.Executable()` is a binary in the temp directory — so on a machine whose
task happened to look doomed, the browser walks committed earlier today would
have re-pointed the maintainer's real daily sync at a binary deleted seconds
later. It did not happen, because their task points at a stable path and the
repair never fired. That is luck. All GUI tests now stub the scheduler seams.

The walk itself (`TestBrowserSchedulingWalk`) ticks the box, sets a time,
saves, reloads, and unticks — asserting the toggle reflects the *scheduler's*
state rather than its own, since it is re-queried on every render. It runs
against stubs on purpose: `scheduler.TaskName` is a single global constant and
the maintainer's live daily sync is registered under it, so a real enable would
overwrite that task and a real disable would delete it — **the disable path has
no guard at all**. That is worth knowing independently of testing.

What *is* checked against the real Task Scheduler is the refusal
(`TestSchedulingRefusesToRegisterADisposableBinaryForReal`, opt-in via
`OPAL_GUI_LIVE_SCHEDULE=1`): enabling from a disposable binary must fail before
writing anything. *Verified live: the refusal rendered, and the real
registration was byte-identical before and after — still 08:00, same
executable.* Deliberately **not** mutation-tested: the mutation is "register
anyway", which would overwrite the maintainer's real scheduled sync. The
stubbed walk is mutation-tested instead (dropping the disable call fails it).

**Fixed a bug that only looking could find:** the secondary buttons on the
settings page — "Browse...", "+ Add course", "Suggest folders", "+ Add rule" —
rendered white text on a near-white fill, i.e. invisible. `pageStyle`'s base
`button` rule sets `color:#fff` for the blue primary buttons, and those class
rules overrode only `background`, leaving the inherited white. Every assertion
in the package passed throughout, because the markup was never wrong. Found by
screenshotting the page and reading the image. *Mutation-tested: dropping the
colour again fails the new test.*

Still genuinely unverified: nothing here is a human *looking* at the pages.
The walk asserts structure and behaviour, not that the result reads well, and
it runs headless so it would not catch a purely visual break. Also still not
run via `gui`/`main.exe gui`: `Run` calls `openNativeWindow` unconditionally
on Windows, which would pop a real window on the maintainer's desktop with no
warning - not something to trigger from an unattended session.

**Handler-level pass done (2026-07-23)** against the real account instead:
stood up the real mux/handlers over `httptest`, hit them with plain HTTP.
Landing, settings, and sync pages all render correctly; course discovery
against the real account found 8 courses (2 more than the 6 in the current
`courses` filter - "[WS25/26] Programmierung" and "Helfende DMS" - not a bug,
just means the account has courses the config doesn't track, which is
expected/normal). No UX issues found at this level, though this still isn't
the same as watching a stranger actually click through it.

**Incident during this pass, already resolved:** the probe's own settings-
POST round-trip check briefly wiped the real `course_folders`,
`subfolder_destinations`, and `use_section_subfolders` in the live
`config.yaml` (a bug in the probe's crude form-scraper, which only
resubmitted a handful of hardcoded field names and silently dropped the
`name="...[]"` array-style rows those settings actually use) — caught
immediately via a manual backup taken before the run, restored exactly, no
lasting damage. Confirmed by reading `settings.go`'s real POST handler
(`r.PostForm["subfolder_dest_key[]"]` etc.) that the actual shipped
settings page is NOT vulnerable to this: its rendered `<input name="...[]">`
rows are real, server-rendered form controls that any normal browser
submission includes natively, JS or not - this was purely an artifact of the
probe's own incomplete field list, not a product bug. The probe file was
deleted rather than fixed, since a settings-round-trip check isn't valuable
enough to justify keeping a known-hazardous test around.

---

## Next

### The 2026-07-27 evening batch
Reported by the maintainer after running the GUI. **Four of five are done**
(see "Done recently"); one is left, and it is the one that needs a real
artefact rather than a code change.

**Left: the Windows binary has no icon.** There is no `.ico` and no `.syso` in
the repo, so the WebView2 window and the taskbar both show the generic default
even though the app has a perfectly good mark (`logoSVG`, served at
`/logo.svg` and already used as the favicon). Needs a real multi-size `.ico`
rasterised from that SVG and embedded as a Windows resource - the SVG cannot
do this job, and adding a build-time rasteriser or a fourth dependency for it
is a judgement call worth making deliberately rather than in passing.

### Nothing ever works off the "Noticed" section
The Stop hook appends one entry per session and no process consumes them, so
the list only grows. The maintainer asked directly: "was passiert eigentlich
mit den notizen?" — and the honest answer today is "nothing, unless someone
happens to read them". Either the backlog's own top-item rule should pull from
Noticed when Now is blocked, or entries should get promoted/dropped on a
cadence. Right now capturing them is real and acting on them is accidental.

---

## Noticed

Things seen while working on something else and passed over. Not commitments —
a list of rough edges that would otherwise only ever exist in one session's
context window. Delete an entry when it is done, or when it turns out not to
matter.

- **`docs/sync-speed-campaign.md` (2026-07-26 entry) references
  `docs/BACKLOG.md`'s "Concurrency SOLVED" entry** as the thing its
  measurements contradict — that heading no longer exists under that name
  (the backlog is trimmed periodically and the item has since moved to
  "Done recently" under different wording), so a reader following the
  cross-reference today would search and not find it.

- **Nothing ever prunes `refs/wip-checkpoints/`.**
  `turn-failure-checkpoint.ps1` writes two or three refs every time it fires
  and there is no expiry, so the repo now carries ~200 of them going back to
  2026-07-23 — each pinning a whole tree, none ever looked at again.
  `.claude/queue/` likewise accumulates a `resume-run-*.log`/`.err` pair per
  launch, mostly empty. Neither hurts anything today; both grow forever.

---

## Done recently

Newest first. Trimmed periodically — git history and PR bodies are the real
record.

- **Four things the maintainer hit running the GUI (2026-07-27 evening).**
  `/feedback` asked people to attach the diagnostic log and offered no way to
  obtain it - it linked to a viewer and left them to go find the file. There is
  a download now (`/logs/download`), serving the whole file rather than the
  page's tail, because a bug report wants all of it; no log yet returns an
  explanation instead of an empty file somebody would attach believing it held
  something. A `go run` build is no longer told it lives in "a temporary
  location" - accurate, but it reads as a fault when it is just how `go run`
  works - and now names the command that fixes it. The schedule page's error
  box said "Could not update", which reads as a failed app update rather than a
  schedule that did not change. And `/settings` had two `<h2>` sections styled
  exactly like real ones that contained nothing settable, only pointers
  elsewhere; they are one secondary line at the foot of the page now.
  *Verified in a real headless browser, not only asserted - the last GUI bug
  here was invisible white-on-white text that every assertion passed.*

- **A cancelled run reports as cancelled, not as a broken tool.** The
  maintainer cancelled the 18:31 run themselves - "da war alles normal" - and
  got a course-listing failure plus advice to leave their browser window open.
  Cancelling tears the browser down, so every source fails; `scrapeCoursesBrowser`
  checked `ctx.Err()` before discovery and after the crawl but not around
  discovery's own error, which is exactly the window a cancel lands in.
  **This also corrects that turn's own reporting**, which presented a
  cancellation as an incident.
  *The wiring is the part that breaks here: removing the call site passes the
  unit test, the build and `go vet`. So the probe cancels for real against a
  real browser, with a server that blocks until the cancel has landed so the
  earlier guard cannot catch it first. Mutation-tested: reverting the call site
  reproduces the exact message the maintainer would have seen.*

- **A run that read nothing no longer reports as a healthy empty account.**
  Found by the maintainer running `go run . gui` (2026-07-27 18:31): login
  succeeded, then all three course-listing sources failed with Playwright's
  `target closed` and the run finished as `Found 0 course links / Discovered
  0 remote files` — which is exactly what a successful sync of an empty
  account looks like. `discoverCourseLinks` warned per source, `continue`d,
  and returned an empty list with a **nil error**; "every source failed" and
  "you have no courses" were the same value to every caller.
  Now all-sources-failed is an error. A *partial* failure stays a warning on
  purpose — the sources overlap, and one transient navigation failure
  aborting a whole sync would be a worse bug than the one being fixed. An
  empty result with no failures also stays fine, since `courses:` can
  legitimately filter everything away.
  **Nothing was lost or damaged by the bad run**: checked rather than
  assumed — the syncer's only `os.Remove` is a temp file, and it never
  removes a local file on the strength of a remote listing.
  The likely trigger is worth knowing on its own: in developer mode the crawl
  keeps running in the *same visible window* the interactive login used, and
  nothing tells you to leave it open. So the error names that case
  specifically when the failure looks like a closed browser.
  *Verified against a real headless browser
  (`TestDiscoveryAgainstARealBrowser`, opt-in via
  `OPAL_SCRAPER_BROWSER_PROBE=1`), which reproduces the incident by closing
  the page mid-run — the unit tests cover the predicates, and this covers
  that `discoverCourseLinks` actually calls them, the gap the stall watchdog
  fell into. Mutation-tested: making the predicate always return false
  reproduces the original message verbatim. Both directions covered — a
  readable listing still discovers its course and does not error, and a plain
  timeout must not be reported as a closed window.*

- **`course_concurrency` default confirmed at 1, live config now matches.**
  Re-measured the real account five times: serial 227.9s/345 files; `2` came
  back 228.2s/**336 files** (`Übungsblätter` 29 → 20, one "show all" expansion
  silently not happening) and otherwise 345 across three more runs at
  230.4s/219.6s/229.4s — no longer faster (mean 226.9s vs. serial 227.9s,
  inside noise) and still loses files about one run in five. Root cause fixed:
  `expandShowAllInSection` now warns instead of silently returning a truncated
  section (`scripts/compare-visit-runs.ps1` turns that into a one-command
  diagnosis), though the warning itself is unverified in the wild — a
  deliberately lossy `--section-concurrency 4` run produced zero warnings
  while losing 160 files, a different failure mode (the file table never
  renders at all, so there's no "show all" control to find). The maintainer's
  own `config.yaml` explicitly set `course_concurrency: 2`; confirmed
  2026-07-27 it now reads `1`, so the measured-correct default reaches them.
- **Fixed the hook-output mojibake noticed in the previous session.** Root
  cause: `docs/RESUME.md` and `docs/BACKLOG.md` have no BOM, and
  `Get-Content` without an explicit `-Encoding` reads a BOM-less file as the
  system ANSI codepage in Windows PowerShell 5.1 — so a UTF-8 em dash
  (`E2 80 94`) was read as three separate CP1252 characters and then
  re-encoded as UTF-8 on the way out, doubling the corruption. Fixed in the
  three call sites where this prose actually reaches the model:
  `session-start-autopilot.ps1` (embeds `RESUME.md` in its `additionalContext`),
  `resume-runner.ps1` (reads `RESUME.md` to decide if there's work), and
  `budget-lib.ps1`'s `Get-BacklogItems` (titles feed directly into
  `autopilot-gate.ps1`'s Stop-hook reason text). *Verified by running the
  hook directly and inspecting the raw output bytes before and after: the em
  dash arrived as the single correct 3-byte sequence, not six mangled bytes.
  Mutation-tested in `scripts/test-hooks.ps1`: a non-ASCII backlog title now
  round-trips byte-exact through `Get-BacklogItems`.*

- **Removed the stale agent worktree flagged above.**
  `.claude/worktrees/agent-ae4c52c8caec1f5e0` (branch
  `worktree-agent-ae4c52c8caec1f5e0`) was a 2026-07-23 prototype ("Add
  section-level flattened crawl (shared frontier across courses)") that
  predates and was superseded by the per-course tab-pool section concurrency
  that actually shipped and was measured on 2026-07-26 (`ca299c5` "Build
  section-level concurrency" onward) — confirmed by comparing commit dates
  and `git merge-base` before removing anything. Uncommitted changes in the
  worktree (`.gitignore`, `section_crawl.go`) were an earlier iteration of
  the same dead approach. `git worktree remove --force` + `git branch -D`;
  nothing pushed, nothing referenced elsewhere.

- **The folder picker corrupted any path with a non-ASCII character.** Chased
  from a mojibake spotted in a live `config.yaml` (`...\Analysis\<U+FFFD>bung`,
  should be `Übung`) and it turned out to be a real bug, not a typo.
  `browseForFolder` runs a PowerShell script and reads its stdout, and
  PowerShell encodes stdout in the **console's OEM code page** — 850 on a
  German Windows, where `Ü` is the single byte `0x9A`. Go reads those bytes as
  UTF-8, `0x9A` is not valid UTF-8, and it becomes U+FFFD. So the user picks a
  real folder with the file browser and the tool stores a path that points at
  nothing — silently, with a successful-looking picker.
  One line fixes it (`[Console]::OutputEncoding` before anything is written).
  *Measured, not reasoned: under code page 850 the path arrives as
  `...,92,154,98,117,110,103` without it and `...,92,195,156,98,117,110,103`
  with it. Both directions are tests — one asserts the round trip survives, the
  other asserts the corruption still happens without the guard, so the guard
  cannot quietly stop being load-bearing.*
  **Why it hid:** it does not reproduce on a console already at 65001, which is
  what an interactive shell here happened to have. The machine's real OEM code
  page had to be read out of the registry to see it.
  The maintainer's own `config.yaml` was repaired in place (backup left beside
  it); the `Übung` folder it should have pointed at already existed.

- **The diagnostic log can be reached from the GUI now.** It was written to
  `~/.opal-downloader/logs/`, named in the CLI's `--help`, and mentioned
  nowhere in the GUI — which is how most people use this, and the case where
  the log matters *most*, since a windowed app's stdout goes nowhere. A
  diagnostic nobody can find is close to no diagnostic.
  `/logs` shows the path, the end of the file, and a button that reveals it in
  the file manager, linked from `/feedback` because a bug report is exactly
  when someone needs it. Showing the contents in a page is safe by
  construction, not by judgement: everything in that file has already been
  through `statuslog.SanitizeMessage`.
  Both the log path and the file-manager call are **seams stubbed in every
  test** — the same hazard as the scheduler one: a test must not open Explorer
  on the maintainer's desktop or depend on their real log.
  *Verified in a real browser (it is in `TestBrowserEveryPageLoads` now) and
  screenshotted, since the last GUI bug here was invisible white-on-white text
  that every assertion passed. Mutation-tested in two directions: dropping the
  tail cap and removing the feedback link both fail.*

- **Refused to schedule a daily sync when there is nothing to sync.** Found
  while building the `/schedule` page and left open at the time. Enabling the
  daily run with no `config.yaml` registered a Windows task that does not fail
  once — it fails *every morning*, silently, unless the failure notification
  happens to be on, in which case it becomes a daily toast about a job the user
  cannot tell they set up wrong. Pre-existing (the old settings-page handler did
  the same), but the new page shows the form right next to a "set up first"
  warning and then let you ignore it.
  Now the enable path refuses before writing anything and says what to do
  instead. **Disabling is deliberately never blocked**: somebody whose config
  has gone missing still has a task running every morning, and refusing to let
  them remove it would strand them with it.
  *Mutation-tested: dropping the guard re-registers the doomed task. All three
  directions covered — refused without a config, still works with one, and
  disable unaffected.*

- **The sync page notices when a run stops moving.** A sync was reported stuck
  once (2026-07-26) and the only evidence was a status line that had not
  changed — nothing noticed, and nothing could have, because the page rendered
  the last event it received and had no opinion about how long ago that was.
  After three minutes of silence during a run it now says how long it has been
  and points at Cancel. Deliberately not an alarm: a large section legitimately
  goes quiet for a while, so it reports elapsed time rather than declaring a
  fault, and any event clears it so it cannot latch on and cry wolf.
  A second bug fell out of writing the test: the page learned "a run is in
  flight" only from the SSE frame sent when it connects, so a run that started
  *after* the page was open was never watched — which is exactly the run worth
  watching. Events arriving now count as proof of a run in flight.
  *Verified in a real browser (`TestBrowserSyncPageNoticesAStalledRun`), which
  also checks the idle page stays quiet and the notice clears on activity.*
  **The other half is now closed too** (`internal/scraper/stallwatch.go`): a
  watchdog inside the crawl logs, every 30s of silence past 3 minutes, *which
  section it was on* — course, title and URL. That covers CLI and scheduled
  runs, which had nothing at all, and it records the thing that was actually
  missing the one time this happened: somewhere to go and look. It only logs;
  cancelling a crawl on suspicion would risk killing a slow-but-healthy run,
  and losing work to a false positive is worse than the stall.
  *Mutation-tested in three directions, and the third is the interesting one:
  deleting the call from `scrapeCoursesBrowser` passed every other test,
  because they all invoke `watchForStall` directly and so check the machinery
  rather than whether anything uses it. The watchdog now records that it was
  started, and the scrape is asserted to start it. Its position moved to the
  top of the function as a result — a watcher stopped by an early return costs
  microseconds, and starting there means nothing can later be added above it
  that hangs unwatched.*
  The original hang has still never been reproduced.
- **Server load is bounded, and the bound is written down.** The maintainer
  asked for this to be set up long-term rather than checked once. Three parts,
  in rough order of how much they matter:
  **Scattering the scheduled runs** is the cheapest and largest. Every install
  proposed `06:00`, so a few hundred of them would start several hundred page
  loads on the same tick — a spike created entirely by a default, for no
  benefit. The minute is now derived from the hostname: scattered but stable,
  so opening the page twice shows the same time.
  **A rate ceiling** every navigation passes through (`internal/polite`, via
  `gotoPolitely`, all fifteen call sites), defaulting to ~4 requests/second —
  about three times looser than what the crawl does on its own. The looseness
  is the design: a limiter that binds during normal operation makes every
  future performance measurement a measurement of the limiter. Its job is to
  stop a *future* change speeding past a defensible rate by accident.
  **Backoff** when OPAL reports overload (429/503), easing off again on a clean
  response. A transport error is deliberately not treated as overload — backing
  off on flaky wifi would turn a bad network into an ever-slower sync.
  `docs/server-load.md` is the policy and is referenced from `CLAUDE.md`,
  including the part that has to be said out loud: this pulls directly against
  `docs/sync-speed-campaign.md`, and the distinction that matters is asking for
  *more things* versus asking for the *same things faster*.
  *Measured live, not assumed: `284 navigation(s), 0 delayed, 0s held in
  total`, on a run that took 226.9s against 211.9s and 223.4s unthrottled. An
  intermediate run measured 244.6s and briefly looked like the ceiling binding
  — the instrumented run settled it. The limiter counts its own interference
  and a scrape logs it, so this stays checkable rather than becoming folklore.*

- **The stalled-login reload watches the page instead of a clock.** Reported as
  "der refresh bei tu-fast braucht viel zu lange" — and that was a description
  of the design, not a tuning complaint. The old code waited a flat 45 seconds
  before reloading, whether or not anything was happening, so a TU-Fast that
  never fired always cost 45 seconds of staring at a page that was never going
  to move. It now reads the login page between short probes and reloads after
  **8 seconds of no change at all** — no navigation, no field being filled, no
  change in how many fields there are.
  **This also fixes a real bug in the old behaviour, not just its speed.** A
  human typing their password by hand stays on a login URL, which was the only
  thing the timer checked — so after 45 seconds it would reload the page and
  wipe what they had typed. A non-empty field now means somebody or something
  is working, and the page is left alone.
  The reading counts fields and how many are non-empty. It never reads their
  contents, and an unreadable page (closed, mid-navigation, evaluation failed)
  counts as activity rather than as a stall — acting on a reading that could
  not be taken is how a working login gets interrupted. The retargeting the old
  code did by hand for flows that open a new tab now falls out for free, since
  the active page is re-read every pass.
  *Mutation-tested: dropping the "nothing typed in it" condition fails the
  test that pins the wiped-password case. The DOM reading is verified against a
  real headless browser and a real Shibboleth-shaped form
  (`OPAL_SCRAPER_BROWSER_PROBE=1`), because a wrong type assertion there fails
  silently as "unknown", which reads as "busy" and would disable stall
  detection entirely.*
  **Not yet seen in the wild:** the stall itself has never been reproduced on
  demand, so the 8-second threshold is reasoned, not measured. If TU-Fast is
  ever observed taking longer than that to fire on a page it *does* eventually
  act on, the reload is harmless (it acts on the reloaded page) but the
  threshold is worth revisiting.

- **Course selection is one list now.** The maintainer's words were "es gibt so
  mehrere stellen und so weiter.. fühlt sich weird an", and they were right
  about the cause: a box of discovered checkboxes, a separate table of
  configured rows, a "+ Add course" button producing a third kind of thing, and
  the user left to join them up mentally. Every course now appears exactly once,
  with its tickbox and its folder on the same line, under three plainly-named
  actions ("Refresh this list from OPAL", "Add one by hand", "Fill in folders
  for me").
  **Unticking no longer deletes the row.** The old version did, which is why it
  had to refuse with an `alert()` when the row carried a folder override — it
  was protecting the user from a deletion it had chosen to do. Keeping the row
  greyed out removes the deletion, the alert and the special case: unticked rows
  are dropped when the form is submitted, and until then the decision is free to
  change. The wire format is untouched, so `parseSettingsForm` did not have to
  learn anything new.
  Also: choosing "pick specific courses" now fetches the list straight away
  instead of leaving a button to be found, and a failed automatic lookup reads
  as "log in first, then refresh" rather than as an error, because on a first
  run that is exactly what it is.
  *Verified in a real browser (`TestBrowserCoursePickerIsOneList`) and
  screenshotted. Mutation-tested: making submit keep the unticked rows fails
  it.*

- **Automatic sync got its own page.** The maintainer's read was that Settings
  is really folder configuration and a daily schedule is a different kind of
  thing; they offered "own page or fold it into sync options" and left the
  call. Own page — `/schedule` — because `/sync` is where you make something
  happen *now*, and putting "run every day at 06:00" beside a button that runs
  immediately invites exactly the mis-click it sounds like.
  The move also fixed something that was never a layout problem: Settings had
  **two independent forms with two save buttons**, one of which did not save
  the schedule and the other of which did not save the settings. And "Notify me
  if a scheduled sync fails" sat under a *Notifications* heading in the
  settings form, about a feature configured further down the page in the other
  form. It now saves with the thing it is about, under one save button.
  **A data-loss hazard came with it, and is pinned by a test.**
  `parseSettingsForm` rebuilds the config from submitted form fields, and an
  unchecked checkbox is indistinguishable from an absent one — so once the
  notification input left the settings page, reading it there would have
  silently switched the preference off *every time anyone saved their folder
  settings*. It is now carried over from disk, and
  `TestSavingSettingsDoesNotClearTheScheduledFailureNotification` fails if that
  regresses. This is the same shape as the invariant already flagged in the
  first-run journey notes below.
  *All five browser walks pass against the new route, and the page was
  screenshotted rather than only asserted on.*

- **Gave the tool real logging, and moved the developer chatter into it.**
  Raised by the maintainer relaying their father's point that a long-lived
  project needs logging with more than one layer. Until now there was exactly
  one channel — `fmt.Printf` to stdout — doing two unrelated jobs: talking to
  the person running the tool, and recording what a crawl did. It served
  neither. The user read text written for a developer, and the developer's text
  scrolled away, or was never visible at all, because the GUI runs as a window
  and nobody sees its stdout.
  `internal/logging` splits it on two axes rather than one: a **level** (how
  bad) and an **audience** (who it is for), because "skipping section" is a
  genuine warning *and* of no interest to a student who wants their slides. Two
  sinks read those independently — the console takes user-facing records plus
  every error, and a rotating file under `~/.opal-downloader/logs/` takes
  everything. `--verbose` (any command) adds diagnostics to the console;
  `--debug-clicks` implies it, since asking for a trace and not being shown it
  would be absurd. Built on stdlib `log/slog`, so no fourth dependency, with a
  printf-shaped facade because that is what every existing call site looks like.
  The scraper's 25 prints are routed by audience. The CLI's own `fmt.Println`
  results are deliberately **not** migrated: a CLI printing its results to
  stdout is already the user channel.
  **Two bugs the first real log caught, which no test would have.** The shared
  credential scrub redacts any 32+ character run of the base64 alphabet — and
  `/` is in that alphabet, so every OPAL URL collapsed to
  `https://bildungsportal.sachsen.[redacted]`. The section URL is precisely
  what `scripts/compare-visit-runs.ps1` needs to answer "which section lost the
  files", so the log was being stripped of the one field that makes it worth
  keeping. URLs are now held out of the scrub and put back with their query
  string dropped — path identifies a course node, query is where a jsessionid
  would live. Second: migrated messages kept their literal `Warning: ` prefix,
  which now doubled up against `level=WARN`.
  *Verified live against the real account: a `list` run wrote user lines to the
  console and diagnostics only to the file. Rotation is mutation-tested
  (reversing the backup shift fails it), as is the audience split.*

- **Rewrote the sync log for a user instead of a developer.** The maintainer's
  account is ~345 files of which almost none change, so a routine sync printed
  ~345 `skipped: course / file` rows and buried the handful of lines that say
  what the run actually did. Worse, the live status line named whichever file
  was being checked, so it sat on one arbitrary filename for minutes — which
  reads as a hang, and was reported as one (`hybrid_quicksort.ipynb`, the
  separate hang item below). Now an already-up-to-date file is counted, not
  listed: the status line shows a running "N files checked, M downloaded" total
  that visibly ticks, downloads and errors still get their own rows, and the
  closing summary is a sentence ("Everything was already up to date (345 files
  checked)") rather than `downloaded=0 skipped=345 errors=0`, which made a
  successful no-op look like a run that did nothing for an unclear reason.
  *Verified in a real browser (`TestBrowserSyncLogIsWrittenForAUser`) by
  publishing events into the real job and letting the real SSE stream drive the
  real JavaScript — a live sync takes minutes and cannot produce an error on
  demand. Mutation-tested: restoring the per-file rows fails it.*

- **Warn before settings edits are thrown away.** Reported by the maintainer
  (2026-07-26): change a field, click away, and it is gone with nothing said.
  Three layers, because no one of them covers every way out of a page — a
  persistent bar while anything is unsaved (the layer that actually helps: it
  removes the need to remember, rather than interrupting at the moment of
  leaving), a confirmation on in-page links (how the user navigates in the real
  WebView2 window, which has no address bar and no back button), and
  `beforeunload` for closing the window. Dirtiness is measured against a
  snapshot taken on load rather than "has anyone typed", so an edit that is
  undone leaves the page clean — a warning that cries wolf gets clicked
  through. Re-checked on a timer as well as on input, because every change this
  page makes in JavaScript (added rows, "Suggest folders", "Browse...") assigns
  `.value`, which fires no event and is invisible to a MutationObserver too.
  *Verified in a real headless browser (`TestBrowserUnsavedChangesWalk`) and
  screenshotted, since the last GUI bug here was invisible white-on-white text
  that every assertion passed through. Mutation-tested: removing the guard
  fails the walk.*
- **Stopped shipping mojibake, and made it detectable.** The sync page's
  preview hint rendered its em-dashes as three junk characters each; two more
  sat in `config.go`'s comments. A human found the first by reading the running
  program, which is the only way any of them could have been found — the damage
  is invisible in review, because the reviewer's terminal renders the broken
  bytes as the characters they were mistaken for. So the fix is a guard
  (`encoding_test.go`) rather than three edits: it scans git-tracked text for
  the lead characters that produce essentially all mojibake, in combinations
  that cannot occur in German or English. Tracked files rather than a directory
  walk — a plain walk also reads the gitignored `tmp/` dumps of real OPAL
  pages, 77 findings this repo cannot fix, which would have made the guard
  useless on its first run. *Mutation-tested both directions.*

- **Made the self-resume runner able to actually start a session.** It never
  once did. Reported by the maintainer (2026-07-26) as "hasn't worked so far";
  `.claude/queue/resume-runner.log` showed six `launch-failed` lines over two
  days, every one of them `%1 is not a valid Win32 application`. All five gates
  were correct — they decided a resume was warranted, and then the launch died.
  Cause: `Start-Process -FilePath "claude"` does not resolve a bare name the way
  the shell prompt does. The prompt walks PATHEXT and finds `claude.cmd`;
  Start-Process hands the raw string to the Windows loader, which takes the
  first PATH match by name — npm's extensionless POSIX shim, not a PE binary.
  A second bug was sitting behind it, never reached: the multi-line prompt was
  passed as a `-ArgumentList` argument, and a `.cmd` runs under cmd.exe, which
  ends its command line at the first newline. It would have delivered line one
  and tried to *execute* the rest — `--model sonnet` included, so the run would
  not even have been on the intended model. The prompt now goes over stdin,
  which has no quoting or newline rules.
  **Why it stayed invisible for two days:** the runner's only output is a log
  line, and nothing reads that file. `SessionStart` now reports unacknowledged
  `launch-failed` entries to the next interactive session, once each — a
  watchdog whose failures are silent is worse than none, because it looks like
  a working safety net.
  The tests were fully green throughout: every resume-runner assertion used
  `-DryRun`, which returns before `Start-Process` is reached. The launch path is
  now testable via an `OPAL_RESUME_CLAUDE_CMD` stub, and `-WhichClaude` lets the
  suite ask the runner what it would execute rather than reimplementing the
  resolution and asserting two copies of the same idea agree.
  *Verified live end-to-end: the real runner launched the real `claude`, which
  read its prompt over stdin and replied — run in an isolated `OPAL_RESUME_REPO_ROOT`
  so an unattended agent was not turned loose on the working tree. Both bugs are
  mutation-tested: restoring the bare `claude` fails the resolution assertion,
  and restoring the argument form fails four, with the stub capturing the prompt
  truncated at line one exactly as predicted. 90 hook assertions, `dev.ps1 all`
  green.*
- **Stopped the resume runner joining a session already working in the tree.**
  Found by the fix above working: the very next hourly fire launched a real
  unattended agent into this worktree while an interactive session was editing
  it. Two agents, one tree, no lock between them — the run was killed before it
  committed anything, tree clean. The existing gate only asked whether a
  *previous unattended run* was alive, which says nothing about a human's
  session. Now `budget-guard.ps1` stamps `.session-heartbeat.json` on every tool
  call and the runner skips while that stamp is under 20 minutes old.
  The obvious implementation — "is any `claude` process alive?" — would have
  deadlocked: the keep-warm process is permanently alive and idle, so it would
  have vetoed every launch forever. Stamping from the tool-call hook separates
  *working* from *running*, and a stamp that ages out means a session dying
  cannot wedge the runner shut. An idle open session is the accepted false
  negative; it isn't editing anything.
  *Verified live: the real runner now reports `a session is active in this tree
  (0m since its last tool call)` against this session's own heartbeat. Both
  directions mutation-tested — removing the gate fails the "won't launch" test,
  and making the heartbeat immortal fails the ages-out test — plus a third
  proving the stamp really comes from `budget-guard` on a healthy budget, where
  it returns early.*
- **Made stopping an unattended run actually stop it.** Same night, same
  incident, third bug: the recorded pid is the `cmd.exe` wrapper, not the agent.
  Killing it left `claude.exe` orphaned and still editing the worktree for five
  more minutes, and its changes landed in an unrelated commit before anyone
  noticed. `resume-runner.ps1 -Stop` now kills the recorded pid *and its
  descendants*, and says which.
  That orphan's own half-finished work was kept rather than reverted — it was
  sound (stop counting `**Blocked:**` backlog items as work an unattended run
  can do, so an all-blocked backlog no longer forces hourly relaunches with
  nowhere to go) — but it had been killed before writing a single test for a
  change to the gate that decides whether autopilot keeps running. That gap is
  now closed: `Get-BacklogItems` has its own tests, including that the real
  `docs/BACKLOG.md` still parses into items, since a formatting change that made
  it parse as zero would stop autopilot dead in silence.
  *Verified: the orphan-kill is mutation-tested by reverting it to a plain
  `Stop-Process`, which reproduces the incident exactly (`orphaned: 38980`).
  `Get-BacklogItems` is mutation-tested in both directions — never flagging
  blocked, and flagging on any mention anywhere in the body. 107 hook
  assertions.*
- **Made work resume by itself once the budget recovers.** Closes the
  "should a killed run restart itself?" question — the maintainer asked for it
  directly (2026-07-23) after being told the cost. An hourly Windows scheduled
  task runs `.claude/hooks/resume-runner.ps1`, whose five gates (off switch,
  already-running, 2h cooldown, budget rung, is-there-work) all run in
  PowerShell and cost **zero tokens**, so a quiet hour is free and a `claude`
  process starts only when all five pass. Unattended runs are bounded by
  construction: 5 autopilot iterations instead of 20, `--model sonnet`, and a
  cooldown so a run that dies on startup cannot become a relaunch loop.
  An in-session cron job was considered as a second layer and rejected: its
  only advantage is preserving this conversation's context, which after a kill
  costs more to resume than a fresh session reading `docs/RESUME.md`.
  Set up / inspect / remove with `scripts/register-resume-task.ps1`.
  **A deadlock nearly shipped here**, caught by the maintainer asking where the
  runner gets fresh numbers from: `rate-limit-status.json` is only written by a
  live session's status line, and this runner exists for when no session is
  running. Once both windows' `resets_at` pass, every reading is unusable — and
  that is exactly when the quota came back. Giving up there meant needing fresh
  numbers to justify starting a session, while only a session produces fresh
  numbers: it would have logged `refusing to guess` hourly, forever, silently.
  An unusable reading now forces a keep-warm sync and re-reads; a usable one
  never does.
  *Verified live: the real registered task was triggered and correctly logged
  `skip  budget not recovered` without spawning anything, and fired again on its
  own hourly schedule. `keepwarm -Force` tested for real — killed the stale
  process, resynced in 14s, file genuinely updated. The deadlock fix is
  mutation-tested: removing the refresh reproduces `refusing to guess` exactly.
  **The launch path was flagged unverified here, and was in fact broken** — see
  the entry above; "tests cover it only in `-DryRun`" was the whole problem, not
  a caveat.*
- **Watch the token budget during a turn, not just between turns.** A run was
  killed mid-turn by the 5-hour limit (2026-07-23) and left no trace;
  diagnosing it meant comparing commit timestamps against window-reset
  arithmetic. Every guard lived on the `Stop` hook — *between* turns — so one
  long turn ran past the budget unwatched, with 1–2 autopilot continuations
  used against a cap of 20. A usage-limit kill never reaches `Stop`, so none of
  the existing guards could ever have fired.
  Now: `budget-guard.ps1` (`PreToolUse`, every tool call) escalates advice as
  the budget floor climbs — commit, update `docs/RESUME.md`, and at the top
  rung no new subagents; `turn-failure-checkpoint.ps1` (`StopFailure`) records
  the kill and captures uncommitted work as a `refs/wip-checkpoints/` commit
  without touching the working tree; `SessionStart` hands the next session the
  failure record and the resume note, and won't arm a full autonomous stretch
  on a budget the `Stop` gate would veto immediately.
  It deliberately does **not** try to predict the limit — the data is a floor
  that can be an hour stale, and the one precise estimator attempted here was
  removed the day it was written for reporting 83.5% against a real 46%. The
  goal is that a kill costs one turn, not a session's train of thought.
  Two latent bugs fixed on the way: keep-warm's 42s cold-launch wait sat inside
  a 15s `Stop` hook timeout and was silently ending autopilot, and
  `rate-limit-gate.ps1` (now deleted, folded into `budget-guard.ps1`) had no
  freshness check and would gate on an already-rolled-over window.
  *Verified: `budget-guard` fired live at rung 3 on a real tool call during
  this work; 58 new assertions in `scripts/test-hooks.ps1`, now part of
  `dev.ps1 all`, and mutation-tested to confirm they fail when the code is
  wrong. **Unverified:** `StopFailure` has not been observed firing for real —
  that needs an actual API kill; tests drive the script directly via synthetic
  stdin, which covers everything except whether the harness invokes it.*
- **Set up the recurring review pass as an actual weekly cron**, not just a
  backlog note. A scheduled cloud routine (Monday 06:00 UTC) reviews only the
  commits since its own last run (tracked via `docs/last-review-commit.txt`),
  looks for correctness bugs and simplification opportunities in that diff,
  files genuine findings here, and commits/pushes directly — matching how
  this repo already operates. "Nothing to report" is treated as a fine
  outcome, not padded with invented findings. Maintainer confirmed the
  ongoing-cost tradeoff (a real recurring cloud-agent run against their Pro
  plan budget) before this was created rather than assuming it.
- **Stopped treating "another sync already running" as a scheduled-sync
  failure.** This closes what used to be the "blocked, needs evidence" sync-
  lock-contention item above: reported live again (2026-07-19, "PID 34084,
  4 seconds after another"), and reading the code showed the GUI's own "Sync
  now" job runs a sync in-process (same PID as gui.exe) using the identical
  `synclock` lock a scheduled run acquires - so this is routine overlap
  between the GUI and the daily trigger, not an incident, and there was
  nothing actionable for the user regardless of which process actually won
  the race. Added `statuslog.OutcomeSkipped`, distinct from `OutcomeFailure`,
  for exactly this case (`synclock.ErrHeld`); it's still recorded in the
  status file/history for diagnosis but no longer fires the failure toast or
  GUI banner. The rolling history log added earlier the same day turned out
  not to be needed to close this - the fix didn't require catching another
  occurrence, just correctly classifying the one already reported.
- **Fixed the tufast-setup page's inconsistent "Home" link** — every other
  page uses "&larr; Back", this one alone said plain "Home" with no arrow.
- **Decided: leave legacy manifest orphans inert, don't prune.** Checked the
  real manifest (2026-07-23): 26 entries still use the pre-migration
  absolute-path key scheme (`_2. Semester/...`, `_4. Semester/...`), matching
  the count from the original migration run. `delete(manifest.Files, ...)` is
  used exactly once in the whole codebase, immediately followed by
  re-inserting under the new key (a rename, not a deletion) — nowhere does
  the manifest ever forget an entry outright, for files removed from OPAL or
  otherwise. Adding a prune path would break that invariant for 26 dead JSON
  keys in a 370-entry file: no perf or correctness cost either way. Not
  revisiting unless the manifest's never-delete design changes for other
  reasons.
- **Set the scheduled task's working directory.** Task Scheduler launches an
  action with no working directory set to `C:\Windows\System32`, not the
  exe's own folder; every subcommand resolves `config.yaml` relative to the
  current working directory, so a scheduled run failed with `config file not
  found: C:\windows\system32\config.yaml` — caught live on the maintainer's
  machine (2026-07-23), even though the registered exe path itself was
  already stable (a different failure than the still-doomed-path repair
  logic below covers). *Verified live end-to-end: rebuilt, re-registered the
  real scheduled task, triggered it, watched it complete
  (`LastTaskResult: 0`, "2 downloaded, 342 skipped").*
- **Hid the pre-setup landing page's login-state box.** A first run with no
  config yet can't be logged in - there's no OPAL URL or credentials to log
  into - so "Not logged in yet" above the setup button was noise, not signal.
  Comes back automatically once a config exists.
- **Auto-arm autopilot on session start**, instead of requiring the marker
  file to be created by hand (in practice it rarely was, so autopilot rarely
  ran even for sessions opened correctly in this directory). Does not help a
  session opened outside this directory - see the "gates are absent" section
  above, unchanged.
- **Gave the dev-build update note its own neutral status-box style**,
  instead of reusing "up to date"'s green on the landing page or the
  error/warn red on `/update`.
- **Gate the `/sync` page's own Sync/List buttons on the same readiness check
  the landing page already applies**, instead of leaving them live when no
  config exists or nobody is logged in. *Verified via handler-level tests
  (exact rendered HTML/disabled state); not exercised in a live browser
  window - this sandbox can't run the native WebView2 binary.*
- **Repair a scheduled sync that points at a disposable binary**, instead of
  telling the user to repair it themselves. Finishes what #122 started: that
  one only stopped new doomed registrations being created. *The repair branch
  is unobserved in the wild — verified live only in its refusing-to-repair
  form, since triggering the repair means rewriting a real Task Scheduler
  entry.*
- **Suggest a per-course download folder**, now measured against a real
  account and tree: 6 of 6 course→folder mappings correct, after a first pass
  that got 0 of 6. Three fixes made the difference — excluding the tool's own
  `default_course_folder` dumping ground (it name-matches perfectly and
  shadowed the real folders), and two tie-breaks for folders a name cannot
  separate (the `…/Downloads` convention, then recency, so this semester's
  "Analysis" beats last semester's). *A stranger's naming is still only as
  good as these signals; the thresholds are tuned to one real tree.*
- **#124** Reload a login page TU-Fast has not acted on, instead of waiting
  out the full timeout. *The stall itself was never reproduced; the reload
  branch is unobserved in the wild.*
- **#123** Verify files OPAL reports no size or date for by comparing bytes,
  instead of assuming they are unchanged. Closes the second half of the
  never-updating-file bug.
- **#122** Refuse to register a scheduled sync against a disposable binary.
- **#121** Discover courses so they can be ticked in setup, not typed.
- **#120** Don't treat a recycled PID as a running sync.
- **#119** Report what the crawl is doing while it runs.
- **#118** Put a primary "Sync now" action on the GUI start page.
- **#117** Heal manifest entries that carry no size/modified signal. First
  half of the never-updating-file bug.
