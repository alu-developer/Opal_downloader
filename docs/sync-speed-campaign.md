# Sync speed campaign

> **This file is the archive. `docs/sync-speed-model.md` drives the work.**
> Reopened 2026-07-31: the campaign was closed on "every lever measured", but
> the maintainer's diagnosis is that the *method* failed — try an idea, it
> fails, drop it, with no step in between where anyone understands why. Read
> this file so a measured rejection is not repeated. Do **not** take a
> conclusion here as a result: several were written confidently and refuted
> later, and the rule below ("quote the measurement") is why. In particular,
> "there is no positive render signal" was inferred from the live DOM — the
> OpenOLAT source, which is public, has never been read.
>
> **Frame warning, added 2026-08-12.** Every "30s" statement in this file —
> including "30s is unreachable loss-free" and the "Standing conclusion on the
> 30s target" section — is about **discovery time**, and the target has since
> been redefined to the whole `sync`, end to end. Those conclusions are not
> wrong within their frame, they are answering a smaller question than the one
> now being asked: a measured no-op sync is 1147s, of which discovery is 45s.
> Read anything below about reaching or missing 30s as being about the 4%.

## 2026-07-31 (late): the lever the morning's entry said was "real" was measured and is NOT. 30s is unreachable loss-free.

The 2026-07-31 entry below ended on "the lever is real" — a navigation-only
tree-walk shortening the settle wait — based on a *timing* probe that counted
folder links at 50ms. That count was misleading: `looksLikeSectionFolderLink`
matches any `/coursenode/` href, and at 50ms the visible links are nav/
breadcrumb/other-course links, not the node's real content subtree. A second
probe (`navtreewalk_probe_test.go`) ran the actual lightweight BFS — navigate,
100ms wait, expand only folder targets — and it reached **1 section** (the
root), expanding **0 children**, on both the course root and the real content
node (`CourseNode/1615865126729195011`, which has 16 children in the full
crawl). A debug probe confirmed: that node at 100ms yields 24 candidates but
only 1 passes `looksLikeSectionFolderLink`, and it points back at the course
root — not real children. **The content tree is JS-rendered at every level, not
just the dashboard.** A navigation-only short wait cannot enumerate it.

**The concrete per-phase breakdown, finally measured** (`list --verbose` on a
renewed session, 282 sections — this is the number this campaign ran without
for its whole life):

| phase | time | per-section | share | removable? |
|---|---|---|---|---|
| settle wait (MutationObserver debounce) | 1m35.4s | 338ms | **64%** | **no** — skipping is measured 51% *slower* (it produces the `sectionCalm` signal the poll needs) |
| stability poll (candidateStabilityPoll) | 48.6s | 172ms | **32%** | **no** — 1-vs-4 reads lost real files past pagination (control not rendered yet) |
| everything else (extract, nav, all) | 3.8s | 14ms | 2% | already minimal |
| rate limiter | 0s | — | 0% | `0 delayed, 0s held` — brakes nothing |

**96% of in-section time is this tool waiting on its own timers**, and every
piece of that waiting is load-bearing. Also checked directly: there is **no
positive render-done signal** in OPAL's DOM (no loading indicator, no busy
flag, no "done" marker) — silence-inference is the only mechanism that works.

**`mode=1` measured live, the predicted improvement that isn't:**
`OPAL_HTTP_DISCOVERY=1 list` on the renewed session = **254.1s total** (HTTP
phase 55.8s, diff=0 verified, HTTP result returned) vs the **206.9s** plain
browser crawl measured the same session. **mode=1 is 47s slower, not faster.**
The reason is structural and was the morning's blind spot: HTTP can't run
before the browser yields section URLs, and the browser pays its full
per-section wait to get them — so HTTP adds time after the browser rather than
removing the wait. The "predicted improvement" required removing the browser's
wait, and that wait *is* the correctness mechanism on a tree OPAL renders
client-side at every level.

**Conclusion, measured not argued:** the realistic loss-free ceiling is the
~207s plain browser crawl. 30s is unreachable without caching section URLs
across runs (option B), which silently misses new sections — the one
constraint the maintainer refused to relax. The serial hybrid shipped behind
`OPAL_HTTP_DISCOVERY` is a correct, verified, independent file source (a
no-regression control), not a speed win. The mode=1 path also surfaces a
looser cross-section dedupe (Softwaretechnologie reports 246 vs the browser's
200 files — no loss, diff=0 on distinct names, but worth tightening before
it is ever a default).

This closes the campaign: every lever measured, every rejection diagnosed to
a named cause. What remains in the tree (`httpdiscovery*.go`,
`*probe_test.go`, `scrapeCoursesHybrid`) is reusable tooling, not a pending
speedup.

## 2026-07-31: REOPENED — the "needs a live human" close was more cautious than the actual risk

The 2026-07-31 closing entry (bottom of this file) declined to build the
tree-walk-only wait shortening autonomously, reasoning that it is the
highest-risk file in the repo with a documented history of *silent file
loss*, and that shipping it in one unattended turn with nobody able to watch
the diff first was the wrong place to spend that permission.

That framing was never checked against what "loss" actually means in this
code path, and per this file's own rule ("quote the measurement," not an
argument) it should have been. Checked now: `internal/syncer/syncer.go` has
no delete/remove path for course files at all — a sync only ever downloads
files it found; it never removes a local file because discovery didn't find
it. So every past "silent file loss" this campaign has documented — the
HTTP-first rejection, the section-concurrency rejection, the cache false-match
risk — means **a file that should have been downloaded silently wasn't**, not
an existing file being destroyed. Recoverable by a later successful run,
and, critically, **exactly what a byte-for-byte diff against the known
345-file ground truth already detects** — the same check this campaign has
used as its acceptance bar in every single entry below.

That changes what "needs a human" actually means here. The byte-for-byte
ground-truth diff is a machine-checkable completion criterion, not a
judgment call — the same distinction the general "when does an agent loop
need a human in it" literature draws (a loop with a scriptable pass/fail
condition doesn't need one watching; a loop whose result only a human can
judge does). Running the tree-walk-only wait shortening behind a flag, in a
diagnostic mode that changes no default, against the real account, and
diffing the result is exactly the same shape as `OPAL_HTTP_DISCOVERY=verify`,
`OPAL_BLOCK_FILE_PREVIEWS`, and `OPAL_SKIP_SETTLE_WAIT` above — all of which
were built and iterated on without a maintainer watching live.

**Where a human still belongs, and only there:** flipping a verified result
from opt-in flag to production default. That's the point where an
undetected regression would silently degrade every future sync rather than
being caught by the next diff. That should be a quick "here's the number,
here's N empty diffs, OK to flip?" — not a live session.

**The recipe for whoever (or whatever session) picks this up next**, so it
does not need this conversation's context to execute it:

1. Build the tree-walk-only wait shortening behind a new env flag (same
   pattern as the existing ones above), in `internal/scraper/crawl.go` /
   `navigation.go`: a section visit that has no file-bearing content on the
   page (pure navigation/folder links) waits only for folder-link count to
   stabilise (~50ms per `treewalk_timing_probe_test.go`), skipping the
   300ms+ settle debounce that's needed for file tables. A section that
   *has* a file table still gets the full wait, unchanged.
2. Run it in **diagnostic/verify mode only** — log what it *would* have
   returned faster, still return the full-wait result — against the real
   account (`list` or discovery-only, no downloads). Non-interactive
   `list`/`sync` already works unattended via the saved TU-Fast session per
   `CLAUDE.md`; no human needs to be present for the run itself.
3. Diff byte-for-byte against the 345-file ground truth
   (`scripts/compare-visit-runs.ps1`, the pattern every prior entry here
   uses). Empty diff = correct this run.
4. **On any non-empty diff: diagnose why before trying again.** Read what
   the fast path actually returned for the section(s) that differ — is a
   hybrid section (folders + a file table together) being misclassified as
   pure-navigation? Fix the classification, don't just retune a timing
   number. This is the "a rejection needs a diagnosed cause" rule from
   `docs/work-quality.md`, applied on the way in this time rather than
   ten days later.
5. Repeat 2-4 until the diff is empty across **at least two independent real
   runs**. Record the measured wall-clock number here either way — a
   diagnostic pass that never reaches empty twice is a real result too
   (append a rejection entry with the diagnosed cause, same as every other
   row in the decision log below).
6. Stop there. Do not flip the flag to be the default in the same pass —
   that's the one step this file still asks the maintainer for, briefly, on
   the strength of the evidence from steps 1-5.

**This is a backlog/next-session item, not something to run inline in a chat
turn** — it needs a real, possibly multi-attempt run against the live
account, and that belongs in an unattended scheduled run (the
`opal-downloader-sync-speed` Routine) or a background agent task, not a
foreground conversation.

## 2026-07-27: where the second actually goes — measured, and it is our own debounce

**This is the first time anyone measured it.** Every earlier entry reasons about
approaches; none of them recorded where a section's wall time is spent. The
answer changes what the problem is.

Live `list` run, real account, 280 sections, 216.6s total:

| | | |
|---|---|---|
| **settle wait** (`waitForInteractiveLinks`) | **94.2s** | **63%** of in-section time, avg 336ms |
| stability poll (`waitForStableSectionContent`) | 49.5s | 33%, avg 177ms |
| everything else (extraction, the actual work) | 4.3s | **2%** |
| *total inside sections* | *148.0s* | *of a 216.6s run* |

Two things fall out immediately.

**1. The extraction is free.** 4.3 seconds across 280 sections. Nothing about
reading the page is slow. Roughly two thirds of the entire run is this tool
waiting on its own timers.

**2. The dominant cost is a debounce, and a debounce always costs its own
duration.** `mutationObserverDebounceMs` is **300ms**, and the measured average
settle wait is **336ms**. The page is finished rendering after about 36ms; the
remaining 300ms is spent proving that nothing *more* is coming. 280 sections ×
300ms ≈ 84s — around 39% of the whole run — is time spent waiting to be sure.

That reframes the target. ~30s was previously called unreachable. It is not
obviously unreachable if 84s of the run is a fixed toll paid per section for
silence.

### What NOT to do with this

Lower the debounce. That is the same class of mistake as lowering
`sectionContentRequiredStableReads` from 4 to 1 — which was live A/B tested and
lost files byte-for-byte identically to the unfixed code. The debounce exists
because Wicket renders a section in stages, and shortening it just moves the
loss back in.

Note also that the poll — the constant that reads as the obvious suspect, and
the one this project has argued about most — is only 33%. Lowering it caps out
at a third of the available win and carries the known correctness risk.

### The actual question this exposes

Both mechanisms *infer* completion from the absence of change. Neither ever
learns that the content arrived; they wait until it has been quiet long enough
to assume so. That inference is what costs 300ms every time, and it costs the
same 300ms whether the page took 20ms or 2s to render.

So: **is there a positive signal?** The file table arrives in a Wicket AJAX
response the browser already receives and parses. Something that keyed off *that
response* would know the content was there instead of inferring it from silence
— and would pay nothing for the certainty.

`internal/scraper/wicket.go` already documents that `AJAX_CALL_DONE` alone is
not sufficient (reading at that signal lost 52 files, 2026-07-21). That is a
real result and it rules out the naive version. It does not rule out the family:
"DONE fired *and* the response body contained the file-table markup" is a
different and stronger condition than "DONE fired", and nobody has tried it.

**Not attempted here.** This entry is the measurement only. Flagged as the
first lead in this campaign that attacks the cause rather than a symptom, and
the first one with a number behind it.

### 2026-07-27 — that lead is REFUTED. There is no AJAX response to key off.

The premise above — "the file table arrives in a Wicket AJAX response the
browser already receives and parses" — was written without checking, and it is
false. `navigation.go`'s own doc comment on `waitForInteractiveLinks` already
said so, from the 2026-07-16 load-completion research: *"network trace confirmed
no separate 'populate content' AJAX request exists to hook a response-based wait
on instead."* Two documents in this repo contradicting each other is exactly the
situation the "quote the measurement" rule exists for, so the trace was run
again rather than one of them believed.

`internal/scraper/network_trace_probe_test.go` (opt-in,
`OPAL_NETWORK_TRACE=1`) records every network response during a real section
crawl. Two courses, chosen as the small and the large end of the account:

| course | sections | files | responses | xhr/fetch | unaccounted |
|---|---|---|---|---|---|
| Algorithmen und Datenstrukturen | 5 | 38 | 263 | 2 | **0** |
| Softwaretechnologie (SoSe 26) | ~160 | 207 | 8154 | 3 | **0** |

Every xhr on both runs was a `pager-showAllLink` expansion — the post-click
"show all" call this code already issues and already waits on via
`AJAX_CALL_DONE` (`wicket.go`). **An ordinary section's initial render fires no
AJAX at all.** The file table comes down inside the section's own `document`
response and is then assembled by Wicket's scripts client-side, which is why
only silence-based inference has ever worked here.

So the stronger condition proposed above — "DONE fired *and* the response
carried the file-table markup" — has nothing to fire on. It is not a harder
version of a workable idea; there is no event.

`navigation.go`'s claim stands. The 2026-07-27 entry's premise does not, and
this table is why. The probe stays in the tree so the next person who doubts
either claim can re-check in one command instead of rebuilding it, and it now
writes its findings to `tmp/` — the first attempt at this ran in a background
process that died with the session that started it, losing the result entirely.

### 2026-07-27 — the trace found something else: discovery downloads ~30 MB of files it never looks at

Chasing "why 408 document responses for a ~160-section course" turned up a
larger finding than the question it came from. Same probe, one course,
`Softwaretechnologie (SoSe 26)`, discovery only — no downloading phase:

| document responses | count | in the main frame | bytes (content-length) |
|---|---|---|---|
| `/opal/auth/…` (section pages) | 324 | **324** | 0 (chunked) |
| `/opal/FolderResource/…` (the files) | **72** | **0** | **30,617,106** |
| `/opal/other/…` | 12 | 0 | 93,365 |

**84 of the 408 documents load in a subframe, and 72 of those are the course's
own PDFs and HTML pages — 29 MB of them — fetched during a pass whose entire
job is to write down filenames.**

These are OPAL course nodes that display their file inline. Arriving at the
section makes the browser fetch the whole file to render a preview, and this
codebase contains **no iframe handling at all** — no `FrameLocator`, no
`ContentFrame`, no `page.Frames()` — so nothing ever reads them.
`crawl.go:1147` already keeps file links out of the BFS queue, so this is not
the crawl following them: it is the section page pulling them in by itself.

**Why this is the most promising lead on this page.** It is the first one that
asks OPAL for *less* rather than for the same things faster, which is the
distinction `docs/server-load.md` draws and the direction it encourages. And it
plausibly attacks the 94.2s settle wait directly rather than by shortening it:
a multi-megabyte PDF loading into an iframe keeps generating mutations, and the
settle wait is a MutationObserver debounce. If preview loads are part of what
the page is waiting to go quiet about, the debounce is partly measuring
downloads nobody wants.

**Shape of the fix:** Playwright request interception, aborting `document`
requests that are (a) not in the main frame and (b) under `/opal/FolderResource/`.
Narrow by construction — it cannot touch a section navigation, which is always
main-frame.

**Not built, and deliberately so.** Blocking a request changes what the page
renders, and this repo has lost files silently to exactly that class of change
more than once (`AJAX_CALL_DONE`, `Attached`, the 700ms wait, HTTP-first
discovery). Before this is believed it needs a **byte-for-byte comparison of
the file list** against a ground-truth run — not a file count, and not one run.
The 345-file full-account ground truth and `scripts/compare-visit-runs.ps1`
already exist for this.

Two numbers to take from the run, whatever happens next: **29 MB per course per
discovery pass**, and **zero of it in the main frame**.

**Consequence for the ~84s debounce toll: it stands too.** The 300ms is the
price of having no positive signal, and there is no positive signal to be had
from the network layer. Anything that attacks it now has to come from a
different direction — a DOM-level completion marker Wicket itself sets, if one
exists, or a different OPAL view that serves the listing without the staged
client-side render. Neither has been looked for.

### 2026-07-27 — the preview-blocking fix: built, verified, and it is not a speed win

Following up on the ~30 MB finding above. Off by default;
`OPAL_BLOCK_FILE_PREVIEWS=1` enables it (Playwright request interception,
aborting `document` requests that are not in the main frame and are under
`/opal/FolderResource/`). Two paired full-account A/Bs, both 2026-07-27:

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
both directions. Recorded here so nobody spends a fourth run on it.

**And that follow-up answered a bigger question. The slowdown was never the
blocking — it is `ctx.Route` itself.** Same session, same evening, 345 files
every run, every list byte-identical against the no-route ground truth:

| condition | wall clock |
|---|---|
| no route installed at all | **210.3s** |
| route + `Abort` | 265.0s |
| route + `Fulfill` (empty 200) | 272.0s |
| **route installed, always `Continue`** | **274.6s** |

The last row is the finding: install the route, block **nothing**, and the run
still costs ~64s more. Every explanation this campaign had written down for the
slowdown was about the blocking, and all of them were wrong.

**Two things follow, and the second is bigger than the preview lead itself.**

1. The ~30 MB saving is real and its price tag belongs to something else. The
   blocker is not a speed/traffic trade-off; it is a free saving sitting behind
   an expensive delivery mechanism.
2. **`ctx.Route` costs ~30% of a run on this workload**, which is a fact about
   this codebase's tooling and not about previews. Anything else that reaches
   for request interception — the network trace probe already does — is paying
   it, and any past measurement taken with a route installed is suspect.

**Answered, and the tax is fixed (not the pattern).** The same route registered
under `**/no-such-path-xyz/**` — a pattern that matches nothing, so the handler
never fires once — came back at **272.2s**. Full picture, one session, one
evening, 345 files and a byte-identical list every single time:

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
a genuinely new direction rather than a re-run of a rejected one.

**Checked immediately, because it would have been the bigger prize:** nothing
in the normal code path installs a route. `previews.go` is the only
`ctx.Route` in the repo and it is off by default, so a routine sync pays none
of this tax. No free 30% was sitting there.

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
describes.

### 2026-07-27 — the last unasked question: is the settle wait even needed? Answered, and it is a clear no

Every attempt in this campaign so far tried to make the settle wait shorter or
cheaper. None had ever asked whether it is needed — and there was a measured
reason to think it might not be, since the network trace above showed an
ordinary section's initial render fires no AJAX at all, implying the file
table might already be in the initial document.

**It is not.** Measured by reading every section immediately, before any
settling, and diffing that byte-for-byte against what the full wait returns —
same run, same page load, so no run-to-run variance:

| | sections |
|---|---|
| total | 278 |
| **identical with no settle wait** | **3** |
| early read was empty | 0 |
| early read was **incomplete** (fewer rows) | **274** |
| early read had more | 0 |
| **same row count, different rows** | **1** |

**The wait is load-bearing.** No AJAX does not mean no client-side rendering:
OPAL builds the file table progressively from the document it already has, and
an immediate read essentially always catches it mid-render.

Two things worth keeping from this:

- **Content only ever grows.** Never empty at the start, never larger than the
  final reading, not once in 278 sections. So "wait until it stops growing" is
  the right shape, and the stability poll is doing real work rather than
  guarding a case that never happens.
- **One section changed rows without changing their count.** That is exactly
  the failure a count cannot see, in a single run, on a real account — the
  reason this project refuses file counts as evidence, now with an instance
  attached to it rather than only a principle.

The probe was deleted again after reporting; it was written with an expiry and
the expiry was honoured. `git show 76a71fa` restores it if anyone wants to
re-measure.

**So the ~30s target needs the debounce itself to get cheaper, not skipped.**
The 300ms is spent proving silence on a page that finishes in ~36ms, and that
remains the single largest line item — but nothing here has yet found a
positive completion signal to replace it, and the two candidates that looked
most promising (an AJAX event, and the content already being present) are both
now measured dead.

**Then: can the settle wait simply go? Measured, and no — it pays for itself.**
It costs 94.2s of a 210s run, and the stability poll after it re-reads until
extraction stops changing anyway, so it looked like two mechanisms inferring the
same fact. Skipping it entirely (`OPAL_SKIP_SETTLE_WAIT`):

| | files | diff vs ground truth | wall clock |
|---|---|---|---|
| settle wait kept | 345 | — | **210.3s** |
| settle wait skipped | 345 | **empty** | **317.1s** |

**Nothing is lost — and it is 51% slower.** The wait is not overhead sitting in
front of the poll; it *produces* the `sectionCalm` signal that lets the poll
open impatient. Remove the wait and every section pays the poll's full patience
streak instead, which costs far more than the 336ms it saved.

That reframes the whole line of attack. This project has spent the campaign
looking for a positive completion signal to replace the debounce — and the
debounce **is** that signal, already built, already paying for itself. It is
not the tax; it is what keeps the tax down.

**And the sharper question is answered too: it is the time, not the verdict.**
Skipping the wait while *asserting* the calm verdict it would have produced —
the optimistic case, the one that could have lost files:

| | files | diff vs ground truth | wall clock |
|---|---|---|---|
| settle wait kept (ground truth) | 345 | — | **210.3s** |
| skipped, verdict `false` | 345 | empty | 317.1s |
| skipped, verdict asserted | 345 | **empty** | **293.5s** |

The verdict recovers only 24s of the 107s penalty. **So the 94.2s is not
signalling overhead that a cleverer signal could remove — it is time the page
genuinely needs**, and the MutationObserver is simply the cheap way to spend it.
The stability poll is the expensive way: every iteration is a full DOM
extraction, against an observer that costs nothing until something moves.

Nothing was lost in either direction, which is worth saying plainly given the
`4->1` history — the risk was real, the diff was the test, and the test passed
both times. The result is still a clear no.

**This closes the largest line item in the campaign.** The 300ms debounce is
not removable, not short-circuitable, and not replaceable by a better signal,
because it is already the cheapest available way to wait for something that
takes that long. Three independent attempts on it now, all measured, all
negative. Anyone reaching for it again needs a genuinely new mechanism, not a
new argument. (The DOM-marker lead found 2026-07-30, below, is exactly that
new mechanism.)

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
| 2026-07-28..30 | Section cache (the same change detector, rebuilt) | **REJECTED again — never hits, for the same reason.** Warm **273.3s** vs **241.0s** cache-off control, i.e. 13% *slower*; hit rate **11/280 = 3.9%** measured by diffing two back-to-back runs' cache files. `internal/sectionhash` normalises 8 volatile patterns against the first attempt's 4 — that lifted the hit rate from ~0.4% to 3.9% and no further. Correctness held (345 files, `diff` empty, all three runs). En route it produced a silent-loss bug that returned files for 1 of 6 courses; see below. |

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

### 2026-07-27 — the change-detection cache is REOPENED. The instability was Wicket bookkeeping.

The 2026-07-21 entry above rejected this design on a real number — normalised
section HTML matched across runs **0 of 276** — and named its own unfinished
business: *"the remaining volatile fragments were never isolated [...] Do that
diff properly before concluding the design is impossible rather than merely
unproven."* Nobody had. Done now
(`internal/scraper/htmlstability_probe_test.go`, test-only, opt-in).

**Everything that varies is Wicket's per-session bookkeeping:**

| fragment | example |
|---|---|
| page-version counter | `?2284-1.0-...`, `?2288"` |
| generated component ids | `id35a0c` -> `id35a85` |
| table-widget instance counter | `VFSItemTable_9072` -> `_9079` |

All three are widget identity, none touches file content. Adding them as
patterns took one file-bearing section from 168 differing lines to **0 —
byte-identical** — and the match rate across sections from 9/12 to **11/12**.

| comparison | identical |
|---|---|
| whole page, raw | 0 / 12 |
| whole page, normalised | **11 / 12** |
| content region, raw | 0 / 12 |
| content region, normalised | **11 / 12** |

**The safety half is tested and passes**, and it matters more than the hit
rate: a cache *miss* costs one section's crawl, a false *match* silently stops
downloading. `TestNormalisationDoesNotHideRealChanges` applies the edits a
lecturer really makes — a file renamed, a new row appended, one character
changed in the body — to the live page and requires each to survive
normalisation as a visible difference. All detected.

**Two corrections earned on the way, both worth keeping:**

- *"All the volatility is in the page chrome"* was an inference from a single
  section and is **wrong**: the raw content region matched 0 of 12.
  Normalisation is needed whichever scope is hashed.
- A probe against an **enrolment node** found zero file references in its HTML
  and nearly produced the conclusion that a hash cannot detect file changes at
  all. That node simply has no files; a file-bearing section carries its
  filenames in the server HTML. **Always probe a file-bearing node.**

**What this does and does not establish.** It establishes that OPAL section
HTML is reproducible enough to hash, which is precisely the premise the 2026-07-21
rejection lacked. It does **not** establish that a cache is fast: that
build measured 317.6s warm because essentially nothing ever hit, and the
reason it never hit is now fixed rather than the speed being re-measured.

**And the ceiling this design can reach is now measured, before building it.**
Ten sections fetched over plain HTTP with the saved session:

| | |
|---|---|
| median fetch | **315 ms** |
| mean fetch | **331 ms** |
| payload | **91 KiB/section** |
| projected, 280 sections, serial | **~93 s** |
| floor at the 4 req/s server-load ceiling | **~70 s** |
| today's browser crawl | 750 ms/section, **210.3 s** |

**So a perfect cache lands around 93s, not 30s.** Every section still has to be
fetched to find out whether it changed; the saving is the browser render, not
the request. That is a **2.3x** improvement on the campaign's central
complaint, and it is the largest single win any approach here has produced -
but the ~30s target is out of reach for this design too, and should stop being
quoted as though some combination will get there.

Worth putting beside `docs/server-load.md` rather than hiding: this asks OPAL
for the same *number* of things while dropping the payload enormously - 91 KiB
of HTML per section against a full page render that also pulls ~30 MB of file
previews per course. Whether the effective request *rate* may rise toward the
4/s ceiling to realise the 70s figure is a policy call, not a technical one.

**Next, in order:** the remaining 1 of 12; then rebuild the cache against the
content subtree — with rootText interning, or the file is **52 MB** for 276
sections — and measure a warm no-op sync against the 210.3s baseline.

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

**Re-checked 2026-08-04 against BPS's documentation — the n=2 spot check was
the documented model, not a quirk of those two courses.** Asked specifically
whether a course-wide subscription exists somewhere, hidden or newly enabled:

- The handbook's "Abonnierbare Inhalte" list contains only Kursbausteine
  (Aufgabe, Dateidiskussion, Mitteilung, Forum, Kalender, Ordner, Wiki, Blog,
  Podcast, Bewertung). There is no course entry.
- "Baustein abonnieren" documents the only route, per element: *"Öffnen Sie den
  betreffenden Kurs und dann den Kursbaustein. Klicken Sie in der rechten
  oberen Ecke ... auf den Button Abonnieren."*
- The "Kurs-Abonnements" entry that exists under Kurseinstellungen is the
  opposite of what it sounds like: a management view where course owners see
  per-element subscription counts and **end other people's subscriptions**,
  filterable by group. So even the per-folder route can be revoked by a
  lecturer at any time.
- Every subscription-related release note from OPAL 2025.11 through 2026.08 is
  a fix or cleanup — mail-delivery bugs, missing translations, deletion
  cascade, a renamed setting, Terminvergabe plumbing. No new capability.

Do not re-open this without a new fact from OPAL's side.

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

That second point contradicted the `DefaultCourseConcurrency = 2` decision of
the time, and it mattered beyond this campaign: the maintainer's live
`config.yaml` was set to 2, so real syncs had been quietly missing files. One
run was not enough to change a default on, but it reproduced a previously
documented result rather than standing alone.

**Resolved since — do not read the paragraph above as a live warning
(re-checked 2026-07-30).** `config.DefaultCourseConcurrency` is **1** and
`DefaultSectionConcurrency` is **1**, and the maintainer's `config.yaml` reads
`course_concurrency: 1` / `section_concurrency: 1`. Nothing is losing files to
concurrency today. The `docs/BACKLOG.md` entry this used to cite by the name
"Concurrency SOLVED" no longer exists under that heading; the current state of
the question lives in that file's sync-speed entry, under the section-level
concurrency table.

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

### 2026-07-30 — the section cache is REJECTED a second time, and the 92% was measured in the one condition that always worked

The 2026-07-27 reopening above was right that the instability is Wicket
bookkeeping, right that normalising it works, and right to say it had not
established that a cache is fast. It was built (pieces 1-3), and the warm run
has now been measured against a cache-off control on the same account:

| run | wall clock | files | diff vs ground truth |
|---|---|---|---|
| control, cache off | **241.0s** | 345 | — |
| cold cache | 283.9s | 345 | empty (includes an interactive login) |
| **warm cache** | **273.3s** | 345 | **empty** |

**13% slower than no cache at all**, because the hit rate in a real run is
**11 of 280 = 3.9%**, measured by diffing the cache file the first run wrote
against the one the second wrote (two back-to-back runs, unchanged account,
same `schema_version` and `PatternsVersion`). 269 sections paid for an HTTP
probe and then got crawled by the browser anyway.

**Correctness held throughout** — 345 files and an empty diff in all three
runs, including the cold one. The risk that mattered (a false match silently
skipping a changed section) did not materialise; `TestNormalisationDoesNotHide
RealChanges` appears to have been doing its job.

**Where the reasoning broke, precisely.** The reopening measured 11 of 12
sections matching and read it as the hit rate the design would get. But those
12 were compared as two HTTP fetches of the same URL; the cache's actual
comparison is a hash *stored during a full browser-interleaved crawl* against a
hash from the next crawl. The 2026-07-21 table above had already separated
those conditions, and had already reported the answer for the one that matters:

| comparison | hashes matching | which condition is this? |
|---|---|---|
| same URL fetched twice back to back | matches | what the 2026-07-27 probe re-measured |
| two pure-HTTP passes, identical fetch order | 13 / 276 | |
| two pure-HTTP passes, one minute apart | 0 / 276 | |
| **stored (from a crawl) vs. a later run** | **1 / 276** | **what a cache actually does** |

The 8 patterns lifted that last row from 1/276 (0.4%) to 11/280 (3.9%). Real,
and two orders of magnitude short of useful. This is the third time this
campaign has recorded the same lesson, now with the sharpest instance of it: a
stability result measured in one condition says nothing about the condition the
feature runs in. The doc even wrote *"a stability result measured on one
section in one condition says nothing about 276 sections in a real run"* nine
days before repeating the mistake at 12 sections.

**The bug it produced on the way, worth its own line.** Wiring the probe into
the crawl made a cold run return files for 1 of 6 courses. The probe's HTTP
client identified itself as `User-Agent: "...opal-downloader"` (no
`AppleWebKit`/`Chrome`/`Safari` tokens); the five failing courses each rendered
one page of generic nav chrome and never got past their own front page. A
Chrome-shaped UA fixed it (`cd1282c`), verified live. Anything in this repo
that fetches OPAL over plain HTTP alongside the browser should send a
browser-shaped UA — that is the transferable part, and it outlives this
rejection.

**What is still not known, and it is no longer cheap.** Which fragments make up
the remaining 96% has never been isolated *in the crawl-stored condition* —
every diagnostic so far ran in the back-to-back condition, which is exactly the
substitution described above. Isolating it properly means instrumenting a real
crawl rather than a probe, and `docs/server-load.md` means each attempt costs
OPAL a full pass. Whether that is worth a third round is a maintainer call, not
a plumbing one.

**Do not rebuild this from the 2026-07-27 entry alone.** That entry is accurate
and its conclusion ("reproducible enough to hash") is true of the condition it
measured. It is the second time this design has been built on a promising
narrow measurement; a third attempt needs a hit rate from the crawl-stored
condition *before* anything is built.

#### The obvious explanation for the 92%, tested and wrong

Worth recording because it was a good hypothesis and it is dead. The stability
probe sent a synthetic `User-Agent` (`"...opal-downloader-probe"`), the same
class of fingerprint that had just been shown to make OPAL serve stubs. If it
had been getting stubs, two fetches would match each other trivially — a stub
carries none of the per-session Wicket bookkeeping the probe normalises away —
so the failure would have inflated the exact number the probe reports. That
would have explained the 92%-vs-3.9% gap by mechanism rather than by condition.

Re-run 2026-07-30 with a browser-shaped UA, same 12 sections:

| | 2026-07-27 (synthetic UA) | 2026-07-30 (browser UA) |
|---|---|---|
| whole page normalised | 11 / 12 | **11 / 12** |
| content region normalised | 11 / 12 | **11 / 12** |
| body sizes | not measured | **min 78,158 / median 78,204 / max 171,515 bytes** |

**Unchanged, and the pages were never stubs** — 78 KB of HTML is a full course
page. The probe has been measuring real pages all along, so the condition
substitution described above stands on its own as the explanation and needs no
help from the UA.

The probe now reports its body-size distribution alongside the match counts
regardless, since a match count cannot by itself distinguish "reproducible" from
"identically empty", and that ambiguity is what made this hypothesis worth an
hour in the first place.

**A loose end this exposes, and it is about the fix rather than the cache.** A
synthetic UA on a *standalone* low-volume probe did not get stubbed here, while
the section-cache probe did — the difference being that the latter fired ~33
requests interleaved with a live browser crawl. So the `cd1282c` fix is verified
to *work* (345 files, byte-identical, per-course mechanism confirmed) but the
*reason* it works is still a theory, and this run is mild evidence against the
simplest version of it. Nothing depends on resolving that today; it is written
down so nobody cites the UA mechanism as established.

### 2026-07-30 — the DOM-marker lead: found a real candidate, not yet built

The 2026-07-27 entry above named the one genuinely unexplored lead left after
the network-layer investigation: "a DOM-level completion marker Wicket itself
sets, if one exists... Neither has been looked for." This is the DOM half of
that sentence (the "different OPAL view" half needs a human looking at OPAL's
own UI, not an automated probe).

**Built:** `internal/scraper/mutationmarker_probe_test.go`
(`OPAL_MUTATION_MARKER_TRACE=1`, `OPAL_MUTATION_MARKER_COURSE=<name>`).
Installs a `MutationObserver` via `page.AddInitScript` — so it attaches before
Wicket's own render starts, the same ordering the production settle-wait
depends on — and records every mutation's target/type/attribute-value/
timestamp across one section's full render-to-stable sequence (`visitSection`,
the same call the real crawl makes). The idea: if the render's last mutation
(or a small stable set of them) always targets the same non-content element —
Wicket's own chrome rather than the file table — that touch could be the
positive signal this campaign has never found.

**First attempt hit a login problem, not a probe problem.** The saved session
(last refreshed 13:47 that day) had expired by the time this first ran
(~21:00), so `ensureSession` fell back to interactive login. It did not
complete within a 5-minute test timeout and `go test` killed the run with a
panic before the probe ever reached a section. No orphaned browser process was
left behind — `internal/procguard`'s job-object mechanism killed the child
Chromium with the panicking parent, confirmed by checking for leftover
processes afterward. A plain `login` run straight after, with more patience
(under 8 minutes), completed normally in a few seconds — TU-Fast is not
broken; the extension is present (`Extensions/aheogihliekaafikeepfjngfegbnimbk/
8.3.0.0_0`, v8.3.0.0) and worked the very next attempt. One data point either
way: this was very likely a one-off (a slow 2FA push, or a cold-launch
first-run cost), not a regression — but worth remembering if it recurs.

**With a fresh session, the probe ran against 4 different courses' root
sections and found the same signature in all four:**

| course | last-8 tail includes | trailing MathJax? |
|---|---|---|
| Algorithmen und Datenstrukturen | `div#id215[aria-activedescendant]`, then MathJax | yes, MathJax finishes last |
| Softwaretechnologie (SoSe 26) | `div#id798.class="jstree jstree-1 jstree-default"`, `[aria-busy=false]`, `[aria-activedescendant=...]` — **the literal last mutation** | no MathJax in this course |
| Analysis | same jstree triple, **the literal last mutation**, MathJax fires earlier in the tail | yes, but finishes before jstree |
| 2026 LA20 | same `aria-activedescendant` mutation, MathJax trails after | yes, MathJax finishes last |

**The element is jsTree — a generic, well-documented jQuery tree widget — and
its own initialization completion is marked exactly the way this lead hoped
for.** `class="jstree jstree-1 jstree-default"` identifies it unambiguously
(that class string is jsTree's own signature, not OPAL/Wicket bookkeeping);
`aria-busy` flips to `"false"` and `aria-activedescendant` gets set to the
tree's default-focused node, together, as jsTree's last act of finishing its
own render. This is almost certainly the course-navigation tree rendered
alongside every section's content — present regardless of course subject,
which is why it showed up in all four.

**What this does and does not establish.** It establishes that a real,
semantically-meaningful, cross-course completion signal exists and is
identifiable (`.jstree[aria-busy="false"]`, or a MutationObserver scoped to
that attribute). It does NOT establish that watching for it alone is
sufficient or faster in practice:

- Courses using MathJax need *its* completion too (MathJax's own async
  typesetting), which is a separate subsystem with no relationship to Wicket
  or jsTree - in 2 of the 4 courses here it finished after jsTree, so a
  jsTree-only signal would fire too early on those and risk exactly the
  "read before the render is done" failure mode `docs/sync-speed-campaign.md`
  has lost files to before (the `4->1` history, `AJAX_CALL_DONE`).
- All four samples here are course *root* sections. Whether every subfolder
  section re-renders (and re-completes) the same jstree widget, or whether it
  persists across navigations within a course and only initializes once, is
  unknown - if it is the latter, this signal would only ever fire on the
  first section of a crawl and say nothing about the other few hundred.
- 4 root sections is enough to say "this is not a fluke of one page" but not
  enough to say "this generalizes to every section this tool visits" - this
  project's own standard (`docs/sync-speed-campaign.md`'s repeated lesson
  about single-condition measurements) applies here too.

**Not built, deliberately, same reasoning as every prior lead here:** this
touches the settle-wait/stability-poll pair directly, in the most
correctness-sensitive part of this codebase, with a documented history of
*silent* file loss from exactly this kind of change. Before anything is
built: confirm the signal fires on non-root sections too, confirm the
MathJax-ordering question (does it ever fire before MathJax on a page that
has math content, and does that matter), and any implementation needs the
same byte-for-byte ground-truth comparison every other change here has been
held to. That is real follow-up work, not a next-session one-liner - flagged
in `docs/BACKLOG.md` rather than started here.

#### Same session, same evening — both open questions above now answered

The probe (`internal/scraper/mutationmarker_probe_test.go`) was extended to
also visit one real subfolder section per course: `appendSectionFolderTargets`
— the exact function the production BFS crawl uses to turn a section's own
candidates into its next queue entries — turns the root visit's candidates
into a real non-root URL with no extra navigation needed to find one.

**Question 1, non-root sections: yes, the same signal fires.** Two
subfolders tested (Softwaretechnologie, Analysis), both non-math and math
courses respectively. Both show the identical `.jstree[aria-busy="false"]`
+ `[aria-activedescendant=...]` pair as the section's last or
near-last mutation — same shape as every root section. This was not a
root-only artifact.

**Question 2, MathJax ordering: genuinely inconsistent, confirming the
risk.** Six sections now sampled total (4 roots + 2 non-root):

| section | jsTree vs MathJax |
|---|---|
| Algorithmen und Datenstrukturen (root) | MathJax finishes **after** jsTree |
| Softwaretechnologie (root) | no MathJax on this course |
| Analysis (root) | MathJax finishes **after** jsTree |
| 2026 LA20 (root) | MathJax finishes **after** jsTree |
| Softwaretechnologie (non-root) | MathJax finishes **before** jsTree |
| Analysis (non-root) | no MathJax visible on this particular section |

Both orderings occur — sometimes on the very same course (Softwaretechnologie
and Analysis each show one ordering on one section and the other/none on a
different section within the same course). **A jsTree-only wait is therefore
not safe as a drop-in replacement for the debounce**, exactly the risk named
above: on a section where MathJax finishes after jsTree, stopping at jsTree's
signal would read the page before its math content has actually rendered —
the file table itself is unaffected either way (jsTree and MathJax both sit
outside it), but this project has no evidence yet that nothing *else*
finishes after jsTree on some section this sampling didn't happen to catch.

**Where this leaves the lead: real, generalizes across root/non-root, and
still needs a MathJax-aware wait condition, not a jsTree-only one.** The
shape such a condition would need: wait for jsTree's `aria-busy="false"`
AND, only on pages where `typeof MathJax !== 'undefined'` is true, MathJax's
own completion (it exposes queueable "done" callbacks in the versions OPAL
appears to use). Building and byte-for-byte A/B testing that combined
condition against the 345-file ground truth is the next concrete step, and
is exactly the kind of change to this file that needs care rather than
speed — not started this session.

## 2026-07-31: the HTTP-first rejection re-diagnosed, a serial hybrid built, and why it still isn't 30s

The maintainer reopened this campaign the same day, with a specific correction:
"every rejection was recorded, never diagnosed." Re-tested the doc's own
rejected claim above ("a JS-rendered section returns 144-172KB with zero
files") against Softwaretechnologie, the course the claim was about. It does
not reproduce — the files are in the raw HTTP response, both as
`data-file-name` attributes and as `<a>` tags the existing `looksLikeFileLink`
predicate already matches.

**What HTTP actually misses, named precisely:** a new ground-truth probe
(`browsergroundtruth_probe_test.go`) put Softwaretechnologie's true file count
at 200; a plain-HTTP pass over the same sections found 158 — 43 missing, all
of it concentrated in exactly 3 pager sections (Part-1/2/3), each capped at
OPAL's ~20-row default page. The page's own HTML states the true count ("57
Einträge") and carries a Wicket-AJAX `pager-showAllLink` — a plain HTTP GET —
that returns the full table. Fetching that one extra URL per pager section
recovered 33/34 of Part-3's gap with no browser involved.

**Then the maintainer proposed something better than either A or B:** run a
fast cached-HTTP pass and the full browser walk concurrently, merging results,
so the user sees fast output with no risk of silent loss. Measured directly
before building anything: started a browser crawl and the HTTP probe against
the *same saved session* at once. The browser's own "show all" expansion broke
under the concurrent load ("71 rows before, 71 after") and lost 125 of 200
files; HTTP stayed stable. **Named cause:** the same Wicket session-
serialization trap this campaign's earlier concurrency attempts already hit,
now confirmed for HTTP-vs-browser specifically. The parallel idea is dead —
not from taste, from this measurement.

**What was built instead — a serial hybrid, gated behind `OPAL_HTTP_DISCOVERY`:**
the browser crawl runs exactly as today (source of truth), then, only after
it finishes, every section it visited is re-fetched over HTTP (following
`pager-showAllLink` where present) and diffed against the browser's result.
`httpdiscovery.go` / `httpdiscovery_fetch.go` hold the pure parsing/fetch
logic (10 offline unit tests); `orchestrator.go`'s `scrapeCoursesHybrid` wires
it in behind the flag, defaulting to a `verify` mode that always returns the
browser's result and only logs the diff — no production behavior changes
unless the flag is set.

**Verification, full account, all 6 courses:** diff = 0. Every course's HTTP
leaf-fetch reproduced the browser's file set exactly (345-file contract
intact; see per-course table in commit `e3384fd`). The extraction logic is
correct, not just correct-on-one-course.

**The honest number this leaves:** verify mode runs both phases — browser
(200s) + HTTP (56s) = 267s, *slower* than the 200s browser-only baseline
being measured against. HTTP-first only saves time if it *replaces* the
browser's leaf-table reading (a `mode=1` that returns the HTTP result and
lets the browser skip reading file tables at all) — and even then, the
browser still has to walk the section tree, which this session also
established is JS-rendered and not reachable over plain HTTP at all (a
content course-node URL fetched directly returns 1 child, 0 files; the
browser finds 163 sections and 207 files from the same starting point).

**So the remaining lever is real but narrow:** if the browser only needs to
walk the tree (cheap, no file table to wait for) and HTTP supplies every leaf
table afterward, the settle wait shortens because it's no longer waiting for
a file table to finish rendering — just navigation links. That is the same
`waitForInteractiveLinks`/`waitForContentSettled` code with the documented
silent-file-loss history, and shortening it is exactly the class of change
this campaign has twice required explicit sign-off for before attempting.
**Not attempted this session — flagged to the maintainer in
`docs/BACKLOG.md` instead of decided here.** The realistic ceiling if it
works is an estimated ~60-90s, not 30s; 30s specifically remains out of reach
by any path measured so far.

**Concrete first experiment, if sign-off is given:** capture a section that
has subfolders at multiple time points after navigation and diff the
folder-nav-link count over time (the campaign already knows the page is
structurally finished at ~36ms while file tables need the full 300ms+
debounce). If folder links are present at ~50ms while the file table isn't
settled yet, a navigation-only short wait is viable and the speedup is real.
If folder links and the file table appear together, the tree-walk cannot be
sped up safely this way and option A tops out at the ~267s serial number
above. Do not shorten the shared debounce itself to test this — that was
already tried (150ms → 322/345 files, a real loss) and is a different, already
-rejected change from a tree-walk-only wait.

A later probe (`treewalk_timing_probe_test.go`) measured this directly on a
real 16-subfolder tree node: folder links reach their full count (25) by
~50ms and stay flat to 400ms across 3 runs. A pure-navigation node needs no
300ms settle wait. The lever is real.

## 2026-07-31, closing this campaign for now: a decision, not another measurement

`docs/work-quality.md`'s retrospective (same day) named this campaign
directly: 23 commits, zero shipped, every verb Reject/Measure/Log/Record/
Retract, and it said plainly that a campaign at that point is failing and
should say so rather than producing a 24th measurement. It also said the
sign-off question sitting in `docs/BACKLOG.md` was exactly the kind of
decision this project's standing permission (CLAUDE.md) already allows an
agent to make - and that leaving it "blocked" without making the call was
the failure, not a safety measure.

Taking that at face value, here is the decision, made rather than deferred:

**Not building the tree-walk-only wait shortening autonomously in this
session.** The reasoning:

- What's verified is real and stays shipped: the HTTP-hybrid discovery path,
  diff=0 against the browser across all 6 courses, available today behind
  `OPAL_HTTP_DISCOVERY=verify`. That is not nothing - it is a working,
  correct, opt-in diagnostic that overturned a rejection nobody had actually
  tested.
- What is NOT verified is the speedup itself. The tree-walk-only wait is
  measured safe for exactly one 16-subfolder pure-navigation node. It has
  never been run against a *hybrid* node (folders + a file table on the same
  page), which is the actual risk case, and it has never been wired into
  `collectCourseFiles`'s real BFS loop at all - that loop's file-candidate
  extraction and folder-target discovery currently share one extraction
  pass, and separating them is a real structural change to the single most
  correctness-sensitive function in this repository, with a documented
  history of *silent* file loss specifically from changes to this wait logic.
- The honest ceiling if it works is ~60-90s, not the 30s the campaign was
  named for. This is a real improvement but not the target, being weighed
  against the single highest-risk file change available in the whole repo.
- CLAUDE.md's own ordering is explicit and does not bend for this
  retrospective: "Reliability over features... robustness wins by default,"
  and safety here specifically means not losing the user's course files.
  Building, verifying (a live run against the real account is the only
  verification this kind of change accepts), and shipping this in one
  autonomous, unattended, unsupervised turn - with no human able to look at
  the diff before it starts touching their real sync - is the wrong place to
  spend that permission, whatever the retrospective says about deferral.

This is deliberately not the same failure the retrospective described. That
failure was measuring the same question five times without ever answering
it. This is answering it once, plainly, with reasons attached, and closing
the loop: **the campaign stops here, at a verified-correct diagnostic
feature, short of the 30s goal.** Reopening it needs either a maintainer
willing to review the wait-logic change live before it ships, or a
different approach nobody has found yet - not another round of measurement
on the same one.
