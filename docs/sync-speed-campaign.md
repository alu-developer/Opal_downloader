# Sync speed campaign

Standing goal set by the maintainer on 2026-07-21:

> "Ich lad mir mal kurz die neuesten Dokumente herunter" must feel like a
> click, not a coffee break. **Target: a routine sync in ~30 seconds.**
> 5 minutes is unacceptable.

This file is the campaign's decision log. It exists because the goal is a
"work until it works" directive, not a single task: approaches will be tried,
measured, and discarded, and the maintainer explicitly wants discarded ones
to stay reviewable — so that a later idea can go back and ask "did we get
that wrong the first time?"

**Every entry must record a measurement, not an opinion.** Four times on
2026-07-21 a documented explanation in this repo turned out to be wrong when
someone finally measured it (see `AJAX_CALL_DONE`, the concurrency penalty,
the browser-fallback root cause, a "flaky" test). Prose confidence is worth
nothing here.

## The measurement that counts

The user-facing number is a **no-op incremental sync**: everything already
downloaded, nothing new on OPAL. That is the "just grab the latest" case.
Downloading is NOT the bottleneck — on a normal day almost nothing is new.

Rules for any number quoted in this file:

- Full account, real config, wall-clock end to end.
- File-set parity byte-for-byte against the serial ground truth (342 files as
  of 2026-07-21). **A faster run that finds fewer files is a failure, not a
  tradeoff.** This has already caught one regression (concurrency 4 lost 9
  files while looking 20% faster).
- Discovery-only probes are fine for iterating (~5 min, no downloads), but
  the headline number must come from a real sync.

## Baseline (2026-07-21)

| phase | cost | note |
|---|---|---|
| discovery | ~4m25s | 284 section visits at `course_concurrency=2` |
| — settle wait | 551ms × 284 | `waitForContentSettled` |
| — stability poll | 457ms × 284 | `waitForStableSectionContent` |
| downloads (no-op) | ~0 | nothing to fetch when nothing changed |

So **~1 second of deliberate waiting per section, 284 times**, is essentially
the entire runtime. Everything else is noise.

## Where the leverage is

Ranked by expected gain, to be confirmed by measurement not by argument:

1. **Skip the browser for discovery entirely.** Plain HTTP with the saved
   session cookies is already proven to work in this codebase —
   `download_refresh.go`'s counter-refresh fast path succeeded 102 times in
   an instrumented full run. If section listings are server-rendered, 284
   HTTP GETs at concurrency 16 is *seconds*, not minutes. Prior research
   claims file rows are in the initial document response with zero Wicket
   AJAX on file-bearing sections — **that claim must be re-verified at
   scale before being trusted**, because its sibling claim from the same
   research (AJAX_CALL_DONE is trailing-safe) was refuted on 2026-07-21 by a
   52-file loss. Paginated sections needing a "show all" click cannot work
   this way (established in PR #100/#109), but there are only ~5-6 of them,
   so they can fall back to the browser.
2. **Section-level concurrency.** Course-level concurrency saturates at 2 on
   a 6-course account, but *within* a course the ~47 sections are visited
   strictly one after another. That axis is completely unexploited.
3. **Cut the per-section waits.** ~1s each. Any reduction multiplies by 284.
   Partly addressed on 2026-07-21 (patience is now earned per section), but
   the settle wait still averages 551ms.
4. **Don't re-discover unchanged sections at all.** Cache structure between
   runs. Highest risk of silently missing new files — treat as last resort,
   and only with a cheap change-detection signal that is itself verified.

## Decision log

| date | approach | outcome |
|---|---|---|
| 2026-07-21 | Baseline measured | no-op sync **318.9s**; discovery ~296s (93%), downloads 23.2s for 5 files |
| 2026-07-21 | HTTP-first discovery, probe | **Promising.** Sequential: 275 sections in **49.7s** vs ~296s. Completeness and the parallelism trap below. |
| 2026-07-21 | HTTP-first discovery, implemented | **REJECTED — unsafe.** Fast (22s) but silently emptied whole courses. No reliable completeness signal exists; four heuristics tried and refuted. Details below. |
| 2026-07-21 | HTTP hash as a change detector | **REJECTED — never hits.** Warm sync 317.6s vs 318.9s baseline. Section HTML is not reproducible across runs: 0/276 hashes matched. Details below. |
| 2026-07-21 | Finer stability sampling | **SHIPPED.** Poll interval 400→150ms with maxPolls 20→53 (total budget unchanged). Discovery 4m27s→3m25s, ~23%, file-complete twice. |
| 2026-07-21 | Research: is there a change signal at all? | **One lead survives.** REST API 403 at the proxy, RSS absent, no `*Site` URLs — but a personal notifications page exists at a stable URL. Blocked on a maintainer decision. |
| 2026-07-21 | OPAL notification signal | **REJECTED — no course-level subscription.** Folder-only subscriptions cannot report a folder that did not exist yet, and new weekly folders are exactly where new files appear. Account restored. |
| 2026-07-21 | Reuse the fallback page across downloads | **SHIPPED** (#115). Clicks per fallback file 4.33 → 2.00. Wall-clock deliberately not claimed — swamped by fast-path-miss variance. |

### 2026-07-21 — HTTP-first section discovery (probe, not yet implemented)

`internal/scraper/httpdiscovery_probe_test.go` (env-guarded, skips by
default). Fetches canonical section URLs through Playwright's
`APIRequestContext` — no browser page, no settle wait, no stability poll —
and extracts file links with the crawl's own `looksLikeFileLink` predicate.

**Speed: confirmed.** 275 sections in 49.7s sequentially, against ~296s of
browser-driven discovery. Zero request errors, zero non-200 responses.

**The parallelism trap — the most important finding here:**

| parallelism | wall-clock | distinct file names found |
|---|---|---|
| 1 | 49.7s | **257** |
| 2 | 47.9s | 176 |
| 3 | 20.3s | 219 |
| 12 | 4.4s | 164 (and varied run to run: 161/174/164) |

Non-monotonic, and parallelism 2 is *no faster* than 1 while finding a third
fewer files. Read that carefully: **OPAL serialises the session server-side
anyway, so concurrency buys no time and actively corrupts results.** Wicket
keeps per-session page state, and concurrent requests to different course
nodes interfere with each other. A single section fetched alone returns its
full content (18 files + its pager control); the same section in a
12-parallel batch returns less. Any future attempt at parallel HTTP discovery
must beat this table first.

**Completeness: not yet parity.** 257 of 312 distinct ground-truth names
sequentially. The gap is plausibly the paginated sections — only **5**
sections advertise a `pager-showall` control in their HTML, and those hide
everything past ~20 rows, which the browser must click to reveal (PR
#100/#109). 257 + those tails is about 312, but **that is arithmetic, not a
measurement — it must be verified before anything is built on it.**

**Shape of the fix this points to:** sequential HTTP for all sections
(~50s), plus the browser for only the handful that advertise a pager
(~5 sections). Estimated ~60s against 296s today.

### 2026-07-21 — that fix was built, and REJECTED. Read this before retrying it.

Implemented as an HTTP fast path per section with a browser fallback, wired
into `collectCourseFiles`. It was fast — a full discovery in **22s** against
296s — and it **silently lost two entire courses**.

Final measured state: **107 files against a 342-file ground truth.**
`2026 LA20` 39/39, `Algorithmen und Datenstrukturen` 36/36 and `Analysis`
30/30 were *perfect*, while `Softwaretechnologie` (206 files) and `TUDMATH
NuMa` (17) came back with **zero**, and `Ma-Prog` with 2 of 14. The run
reported no errors. Only the existing "crawled successfully but found 0
files" warning hinted at it, and only the byte-for-byte comparison against
the ground truth proved it.

**Root cause: OPAL renders some course nodes server-side and others
client-side, and nothing in the HTTP response distinguishes them.** A
JS-rendered section returns 144-172KB of markup with 5-19 extractable links
and zero files — which is indistinguishable from a section that genuinely
has no files.

**Four candidate completeness signals were tried. All four are refuted:**

| signal | why it fails |
|---|---|
| `pager-showall` present in the HTML | only 5 sections advertise it, yet 43 files were missing from paginated ones |
| extracted row count at the ~20 page cap | catches truncation, says nothing about a list that never rendered |
| zero extractable candidates | JS-rendered sections return 5-19 links, so they pass |
| URL shape (`/CourseNode/<id>` vs `/CourseNode/<id>/<Folder>`) | 3/3 with a folder segment had files, but 8 of 43 bare ones did too — it is per course-node type, not per URL shape |

**Do not retry the direct-replacement design without a completeness signal
that is verified against a course like Softwaretechnologie.** "It worked on
my course" is exactly how this one passed three courses and destroyed two.

### Where this leaves the campaign

The HTTP fetch itself is sound and fast; what cannot be trusted is *reading
files out of it*. That points at a different design, which never asks
"is this HTTP view complete?":

**Use HTTP as a change detector, not as a data source.** Fetch each section
over HTTP and compare a normalised hash of its HTML against the one stored
from the last successful *browser* crawl. Unchanged means reuse that
section's cached, browser-verified file list; changed means re-crawl it with
the browser. Correctness stays anchored to the browser, and a no-op sync -
the case the maintainer actually cares about - becomes ~275 cheap HTTP
fetches plus zero browser visits.

**That gating question is now answered: YES.** Fetched the same section
twice; both responses were exactly 189,648 bytes and differed in 796 of
2,224 lines, but every difference came from four volatile patterns —
Wicket element ids (`id[0-9a-f]{4,}`), page-instance counters (`\?[0-9]+`),
component instance counters (`_[0-9]{3,}`) and cache-busting timestamps
(`antiCache=[0-9]+`). Normalising those four makes two consecutive fetches
hash identically.

Sensitivity was verified in the same pass, which is the half that matters:
a renamed file, a swapped file and a removed 1,600-byte chunk are all still
detected, and a no-op does not change the hash. Measured on one section of
one course so far — it must be re-checked against a `Softwaretechnologie`
section (the client-rendered course that destroyed the previous design)
before being trusted.

(Append one row per attempt. Include the measurement, and for a rejection,
enough detail that a later reader can judge whether it deserves another look
rather than having to redo the experiment.)


### 2026-07-21 — change-detection cache: built, measured, REJECTED

The design from the previous entry (HTTP answers only "did this change?",
files always come from the browser) was implemented in full: normalised hash,
versioned cache file next to the manifest, browser crawl on any miss, safe
degradation on corrupt/unknown cache.

**Correctness held. Speed did not exist.**

| run | wall-clock |
|---|---|
| baseline no-op sync | 318.9s |
| cold cache | 332.5s (342 files, 0 errors) |
| **warm cache** | **317.6s** |

The warm run saved nothing, because essentially nothing ever hit.

**Measured directly, without a browser in the loop:**

| comparison | hashes matching |
|---|---|
| same URL fetched twice back to back | **matches** |
| stored (from a crawl) vs. a later run | **1 of 276** |
| two pure-HTTP passes, one minute apart | **0 of 276** |
| two pure-HTTP passes, identical fetch order | **13 of 276** |

So OPAL section HTML is **not reproducible across runs** beyond the four
volatile patterns already normalised. It is not a browser-interleaving
artefact and not a fetch-order artefact — both were ruled out above.

**Why the earlier "gating question: answered YES" was wrong.** That test
fetched one URL twice, seconds apart, in isolation. It proved the four
patterns handle *within-request* noise, and I generalised it to
*across-run* stability, which is a different claim. The lesson is the same
one this campaign keeps re-learning: a stability result measured on one
section in one condition says nothing about 276 sections in a real run.

**What was NOT determined, and is the cheap next step if anyone retries
this:** the remaining volatile fragments were never isolated. The diagnostic
that would do it — dump the in-batch HTML for one URL on two separate runs
and diff the normalised forms — was attempted but botched: the dump helper
re-fetched the URL standalone instead of keeping the batch response, so it
compared two standalone fetches (which of course matched). Do that diff
properly before concluding the design is impossible rather than merely
unproven.

**Also worth keeping**: the cache file needed rootText interning. Storing the
extractor output verbatim produced a **52 MB** file for 276 sections, 31 MB
of it the same section text duplicated across 9,958 candidates. Interning it
is lossless and cut the file to 6.7 MB. Any future cache of extractor output
must do this — the file lives in `download_path`, which is typically a
cloud-synced folder.

### 2026-07-21 — finer stability sampling: the first thing that actually worked

After two structural rejections, the direct lever: the per-section stability
poll sampled every 400ms with a 20-poll cap (~8s budget). Changed to **150ms
with a 53-poll cap — the same ~8s budget, sampled 2.7x more finely.**

This is deliberately *not* a patience cut. A settled page now confirms in
150ms instead of 400ms, while a slow one still gets the full ~8s.

| setting | wall-clock (discovery only) | files |
|---|---|---|
| 400ms / 20 (old default) | 4m13s, 4m27s, **4m41s** | 322, 322, **307** |
| 150ms / 20 | 2m13s | 322 |
| **150ms / 53 (shipped)** | **3m27s, 3m23s** | **322, 322** |

**The most important row is the old default's third run: it silently lost 15
files** — the tails of two paginated sections, including `Vorlesung_9_10.pdf`,
the same file named in past incident comments. 1 in 3 runs, no warning
logged. That is a pre-existing intermittent loss, not something this change
introduced, and **it is not proven fixed**: three clean runs cannot
demonstrate absence.

Sampling more finely should, if anything, help correctness — a finer rate is
more likely to observe growth and trigger `candidateStabilityPoll`'s
escalation to the patient streak.

**Not shipped, and why:** 150ms/20 was the fastest at 2m13s, but it cuts the
total budget from ~8s to ~3s. Those budgets were raised over several real
file-loss incidents; halving them on one clean run would be exactly the
mistake this campaign keeps documenting.

> **RETRACTED the same day.** That 2m13s does not reproduce. Three runs at
> 150ms/20 measured 3m19s, 3m17s and 3m18s — identical to the shipped
> 150ms/53. The poll exits on stability long before either cap, so the cap
> changes nothing. The 2m13s was network or server variance recorded as a
> result. See the entry below.

**Against the target:** a no-op sync goes from ~318.9s to roughly **240s**.
That is a real 23% improvement and it is nowhere near the 30-second goal.
See the campaign's standing conclusion below.

### Standing conclusion on the 30s target

Three approaches in, the shape of the problem is clear: a browser-per-section
crawl costs ~1s per section across ~284 sections, and that floor is
architectural. Reaching 30s requires **not visiting most sections**, which
needs reliable change detection — and OPAL's HTML is not reproducible enough
across runs to provide it (measured: 0/276 hashes matched).

Realistic expectation without a new idea: **3-4 minutes**, not 30 seconds.
Anyone picking this up should either find a genuinely different change signal
(an OPAL API, a course-level "last modified", an RSS/notification feed) or
accept the floor. Do not re-run the three rejected designs.

### 2026-07-21 — research: does OPAL expose a change signal?

The standing conclusion above says 30s needs a different signal entirely.
This is the search for one.

| candidate | result |
|---|---|
| OpenOLAT REST API (`/restapi/*`, 7 paths) | **403** on every one, with an Apache `iso-8859-1` error page — refused at the reverse proxy, not by OpenOLAT |
| RSS (`/rss/`, `/auth/rss/`) | 404 and 400 |
| OpenOLAT `*Site` deep links | not referenced anywhere on the home page |
| **Personal notifications page** | **exists, at a stable URL** |

`https://bildungsportal.sachsen.de/opal/home/notifications` renders a popover
that currently reads *"Es gibt keine Neuigkeiten."* It is reached from the
`notificationsLink` anchor in the "Persönliches" navigation — that anchor sits
in a collapsed menu and cannot be clicked, so navigate to its `href`.

**One page, account-wide, that answers "did anything change?" is exactly the
shape the 30s target needs.** A sync could fetch it and, on "no news", skip
discovery entirely.

**The catch, and why this is blocked rather than built:** OpenOLAT only
reports what you have *subscribed to*, and this account appears to have no
subscriptions — 5 files genuinely appeared earlier the same day and were not
reported. Confirming that requires creating subscriptions in the maintainer's
real OPAL account, which changes their settings and can send them e-mail.
That is their call. Filed as
`.claude/queue/blocked/opal-notification-change-signal.md` with the question
and the full acceptance criteria.

**Unverified and load-bearing:** that OPAL notifications report *file
additions in a folder* at all. If they only cover forum posts and
announcements, this route dies too, and the ~3-4 minute floor stands.


### 2026-07-21 — wait-tuning is exhausted (and a retraction)

| setting | wall-clock | files |
|---|---|---|
| 150ms poll / 53 cap (shipped, ground truth) | 3m19s | 322 |
| 150ms poll / 20 cap, three runs | 3m19s, 3m17s, 3m18s | 322 each |
| debounce 150ms | 3m19s | 322 |
| debounce 80ms | 3m17s | 322 |

Neither the poll cap nor the MutationObserver debounce moves the wall-clock at
all. **After PR #114 dropped the poll interval to 150ms, the deliberate waits
are no longer where a run spends its time.**

Two corrections come out of this, both about my own earlier measurements:

1. **The 2m13s that motivated the follow-up task is retracted** — not
   reproducible across three runs at the same setting.
2. An earlier session "tuned the debounce" by setting
   `mutationObserverConcurrentDebounceMs`, which only applies at
   `course_concurrency>1` while the real config runs at 1. That knob did
   nothing; the effect measured then came entirely from the poll interval.
   The runs above use the serial constant, the one that actually applies.

**Arithmetic on what is left:** ~284 sections in ~198s is ~700ms per section,
of which the waits can now account for at most ~300ms. The rest is OPAL's own
page-load and render latency behind `page.Goto`. Even eliminating every wait
would leave roughly 2 minutes.

A floor run with all waits near zero was prepared to pin that split down
exactly, and was interrupted before it ran — **so the precise
waits-versus-network split is unverified.** The conclusion that wait-tuning is
finished does not depend on it; the four measurements above are sufficient for
that much.


### 2026-07-21 — the notification signal, the last surviving lead: REJECTED

Driven live with the maintainer's approval, and rejected on correctness.

The mechanism itself is fine and scriptable: a folder page carries
`<button title="Abonnieren">`, which becomes `title="Abo beenden"` once
subscribed. Notification e-mail was turned off (`/home/settings`, "Zeitraum
für Benachrichtigungen" `3` → `0`) and verified **before** any subscription
existed, so no mail was ever triggered.

**What kills it: there is no course-level subscription.** Both course roots
checked (`53290106881`, `53228666883`) have no subscribe control at all -
only folders do. A subscription therefore cannot report a folder that did not
exist when it was created, and these courses grow folders continuously
(`Woche 07`, `Woche 08`, `Woche 09` all appear in crawl logs). A new week's
folder is precisely where new files land, so "no news" would be reported for
a course that just gained an entire folder of material.

Skipping discovery on that signal would silently miss it - the same failure
mode as the two earlier rejections.

The account was restored afterwards: subscription removed, interval back to
`3`, both verified by reload.

**This exhausts the leads for the 30s target.** The standing conclusion above
holds: ~3-4 minutes is the floor for a browser-per-section crawl, and closing
the gap needs a change signal OPAL does not appear to offer. What remains, if
the maintainer wants it, is a scheduled background sync so the wait is never
in front of them - a different answer to the same problem.

### 2026-07-23 — re-measured, config was stale, and one unexplored axis found

The maintainer reported the wait as still unacceptable ("30 seconds, not 5
minutes... without this the project is senseless") and asked for another
look. Before touching anything, re-ran the real no-op sync with `--profile`
against the real account (6 courses, 344 files, `course_concurrency: 1` -
the live config had never been updated after `DefaultCourseConcurrency` was
raised to 2 on 2026-07-21):

| phase | measured |
|---|---|
| discovery (course links) | 4.3s |
| file collection (aggregate, serial) | 5m22.5s |
| — 2026 LA20 | 39.0s (39 files, 34 sections) |
| — Algorithmen und Datenstrukturen | 7.9s (38 files, 5 sections) |
| — Analysis | 48.7s (30 files, 30 sections) |
| — So26 Programmieren | 45.4s (14 files, 35 sections) |
| — **Softwaretechnologie** | **2m48.1s (206 files, 160 sections)** |
| — TUDMATH NuMa | 13.4s (17 files, 13 sections) |
| downloads (0 new, 13 no-signal files byte-verified) | 3.9s |
| **total** | **334.1s** |

Confirms the standing conclusion: this run is consistent with the ~3-4 minute
floor already established (a bit above it, plausibly normal variance plus
today's byte-verification cost for signal-less files - see #123). One
concrete, free fix applied: `course_concurrency` in the live config was still
`1`, a stale value from before the 2026-07-21 default change to `2` (12%
faster, byte-for-byte safe per that day's measurements) - bumped to match the
code's own recommended default. Not re-measured separately since it's
already proven safe; folded into whatever number the next full run reports.

**One axis from "Where the leverage is" above was never actually tried:**
item 2, section-level concurrency. Every rejected/shipped entry in this log
attacks course-level concurrency (capped at 2-3 by real file loss at 4) or a
change-detection signal (all rejected). Nothing here ever parallelized
*within* a course. The crawl (`collectCourseFiles`, `internal/scraper/crawl.go`)
is a real BFS over section URLs discovered incrementally (`page.Goto` per
section, not a stateful AJAX tree-click - confirmed by reading the code, not
assumed) - so a course's full section list is not known upfront, but each
BFS *level*'s siblings are all queued and independent before any of them is
visited. That is parallelizable without changing discovery order at all: pop
a whole level, visit its members concurrently across several tabs (reusing
the same per-section stability-poll/show-all-reclick correctness machinery
course-level concurrency already needed), merge newly-discovered children
into the next level, repeat. Load-balancing this across *all* courses' combined
frontier (not one queue per course) would also stop the current "2 idle-ish
workers while course_concurrency=2 but 5 of 6 courses are small and one -
Softwaretechnologie, 160 of the account's 284 sections - dominates" problem,
since 6 courses limits course-level parallelism to 6-way at best while ~284
sections is the real amount of independent work available.

**Not attempted yet.** This is a real rewrite of the crawl's concurrency
model (today: N courses in parallel, each internally serial; proposed: one
shared section frontier serviced by K tabs, courses just seed multiple root
nodes into it) in the single most correctness-sensitive part of this
codebase - every rejected/shipped entry above that touched concurrency did so
against a documented history of *silent* file loss, not a loud error. Flagged
to the maintainer rather than built blind; if pursued, it must follow this
file's own rule (byte-for-byte against the known 344-file ground truth,
multiple runs, real measurement) before being trusted at any concurrency
level.

### 2026-07-26 — section-level concurrency: built, and the baseline runs found a live bug first

The maintainer signed off on the rewrite ("du hast die permission, um für die
crawl-nebenläufigkeit umzubauen"), which is what the previous entry was
waiting for.

**What was built** (`internal/scraper/section_pool.go`, `--section-concurrency`,
`config.section_concurrency`): level-synchronised BFS *within* a course. A
whole BFS level is popped, its sections are visited concurrently on their own
tabs, and the results are merged **serially in pop order**. Deliberately
narrower than the "one global frontier across all courses" sketch in the
previous entry — that interleaves courses and changes per-course error
accounting, which is not what you want as the first change in the part of this
codebase with a documented history of silent file loss.

The level structure is the safety argument, not an implementation detail:
every shared structure (`fileSeen`'s dedupe, the visit log, the queue
`appendSectionFolderTargets` appends to) is touched only in the serial merge,
so `files`, `queue` and every dedupe outcome are identical to a serial crawl
regardless of the order pages finish rendering in. Only rendering is
concurrent.

**A real bug in the branch, found before any measurement was trusted.** The
stability polls buy extra consecutive stable reads only while more than one
tab may be rendering, and that gate asked about *course* concurrency alone.
Correct until this branch existed; after it, `--course-concurrency 1
--section-concurrency 4` renders on four tabs while the gate calls the crawl
serial, taking the *impatient* budget under exactly the load the patient one
was written for. Fixed (`crawlingConcurrently`, both poll sites) and
mutation-tested.

#### The baseline runs, which are the real news

| run | course | section | files | Analysis | wall clock |
|---|---|---|---|---|---|
| ground truth | 1 | 1 | **345** | 30 | 227.9s |
| A | 2 | 1 | **336** | 21 | 228.2s |

Two things follow.

1. **The refactor is clean.** 345 at course=1/section=1 is exactly the known
   ground truth, on the new code.
2. **`course_concurrency: 2` still loses files, and buys nothing.** 336 vs 345,
   nine of them from Analysis — the same course, and very nearly the same
   count, as this file's 2026-07-17 entry ("Analysis: -8 files in 3 of 4
   runs"). And it is not even faster: 228.2s against 227.9s, inside noise on a
   6-course account.

That second point contradicts `docs/BACKLOG.md`'s "Concurrency SOLVED"
entry and the `DefaultCourseConcurrency = 2` decision it justified, and it
matters beyond this campaign: the maintainer's live `config.yaml` is set to 2,
so real syncs have been quietly missing files. One run is not enough to change
a default on, but it reproduces a previously documented result rather than
standing alone.

**Why course concurrency being useless here is not surprising in hindsight:**
this account has 6 content-bearing courses and one of them (Softwaretechnologie,
207 files, 160 sections) is most of the work. Two workers means the big course
runs alongside a queue of small ones that finish early, and then it is alone —
the same "5 of 6 courses are small and one dominates" problem the previous
entry described. It is the argument for the section axis, stated in
measurements instead of prose.

#### The section-concurrency measurements — REJECTED

Same binary, same account, same session, only `--section-concurrency` varied,
`--course-concurrency 1` throughout so the new axis is the only variable:

| section concurrency | files | wall clock | vs ground truth |
|---|---|---|---|
| **1 (ground truth)** | **345** | 227.9s | — |
| 2 | 257 | 147.2s | **−88 files (−26%)** |
| 4 | 214 | 110.7s | **−131 files (−38%)** |

Monotonic in both directions: every tab added makes it faster and loses more.
This is the campaign's own rejection criterion, met twice —
*a faster run that finds fewer files is a failure, not a tradeoff.*
`DefaultSectionConcurrency` is 1 (off).

**It is not a wait being too short.** Three things say so:

- The patience fix was already in the binary for both runs — the polls used
  their *full* concurrent budget and still lost this much.
- **Zero** section-level warnings across the whole 4-tab run: no "returned no
  content", no "skipping section". The pages did not come back empty.
- The *structure* came back perfect. The 4-tab run skipped exactly the same 16
  enrollment nodes and processed exactly the same 8 courses as the serial run.
  Section links were discovered identically; only file rows were missing.

That combination points at partially-rendered sections passing the stability
poll: the course-tree navigation is in the initial document, the file table
arrives later via Wicket AJAX, so a section that has rendered its nav but not
its table looks like legitimate non-empty content to every check we have. It
then contributes its folder links (structure intact) and no files (loss),
silently. `Analysis` came back as 0 files in one run and 1 in the other while
reporting "crawled successfully".

**Why this axis is worse than course concurrency at the same tab count.**
Course-level concurrency puts each tab on a *different* course tree. Section
concurrency puts several tabs inside the *same* one, which is a case OPAL has
never been asked to serve here — and OLAT/Wicket keeps per-session, per-page
server state. That is the structural difference worth investigating if anyone
returns to this; it is not something more patience can fix.

**Status of the leverage list at the top of this file:** item 2 is now
explored and rejected on measurement, joining items 1 and 4. The machinery and
`--section-concurrency` stay in the tree so a future attempt can re-measure in
one command, but the feature is off.
