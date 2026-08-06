# Sync speed: the causal model

**This file drives the work. `docs/sync-speed-campaign.md` is the archive from
here on** — measurements and graveyard, to look things up in, not to derive the
next step from.

Since 2026-08-03 there is no separate `opal-downloader-sync-speed` task:
`opal-downloader-autopilot` works the backlog first and lands here when nothing
unblocked is left there. Older entries below still name the old task — that is
history, not an open responsibility.

The difference is the point: a list of *approaches* runs out, and when it does,
it looks like "this can't be done". A list of open *questions* does not run out,
because every experiment creates new ones. It is ordered by how much the answer
would change everything — not by how easy it is to answer.

Introduced 2026-07-31, after the maintainer named the working method as the real
problem: *"have an idea, try the idea, it doesn't work, drop the approach"* —
with no step in between where anyone understands why.

## The three rules

1. **Every experiment writes down beforehand: expected number, suspected
   mechanism, and the number at which it counts as failed.** After that a bad
   result is not a verdict but a gap between prediction and reality — and that
   gap has to be explained.
2. **An approach may only be closed when the explanation is sharp enough that it
   would have predicted the failure in advance.** If there is still a hole, it
   stays open. ("HTTP loses courses" is a description; "OpenOLAT building-block
   type X renders client-side and is recognisable in the response by Y" would be
   a cause.)
3. **Every experiment must leave at least one new open question.** If it does
   not, that is exactly what gets reported.

## When ideas run out

No reason to stop — a solvable state. Fixed moves, in order:

- **Read the other side.** OPAL runs on OpenOLAT, which is open source. So far
  only the manuals have been read and the live server probed.
- **Look at how others solved the same thing** (other OPAL/OpenOLAT/LMS
  downloaders).
- **Compute the ceiling before building.** That has already saved a whole build
  here once (HTTP ceiling ~93s).
- **Change the question.** The goal is *"feels like one click"*, not "discovery
  is fast". Prefetching, background runs and partial results are a solution class
  of their own and were never on the list.
- **Ask which constraint is negotiable** — as options to the maintainer, not as
  an open question.
- **Measure instead of arguing.**

---

## What we know (numbers only, all from real runs)

| | |
|---|---|
| Target | **30s** for a no-op sync |
| Today's lossless floor | **~207s** (pure browser crawl, 282 sections) |
| Settle wait | 338ms/section, **64%** of the time in sections |
| Stability poll | 172ms/section, **32%** |
| Real work (extraction, navigation) | 14ms/section, **2%** |
| Rate limiter | held 0s — slows nothing down |

**96% of the time the tool is waiting on its own timers, and every wait carries
its weight.** Dropping the settle wait: 51% *slower*. Additionally asserting the
verdict: still 40% slower. The page really does need that time; the
MutationObserver is just the cheapest way to sit it out.

Further hard findings: no positive render-complete signal found in the DOM. No
AJAX during the initial section build (network trace, 2 courses, 0 unexplained).
`ctx.Route` costs ~30% of a run purely by existing. HTTP fetch 315ms/section,
91 KiB, ~93s projected serially — corrupted in parallel (OPAL serialises the
session server-side). Hybrid `mode=1`: 254s against 207s, i.e. slower, because
HTTP can only start after the browser has delivered the URLs. Section hash cache:
3.9% hit rate, 13% slower. Content only grows while rendering (278 sections:
never empty, never larger than at the end).

## What we don't know (sorted by leverage)

### 1. What is OPAL actually rendering? — now read up, see below
~~OpenOLAT is open source. This campaign spent ten days guessing at the live
server what it does.~~ Answered 2026-07-31, see "Next experiment" below for the
evidence. Short version: there is **no** marker, because **nothing is rendered
client-side that would have to finish** — tree and file table are pure server
HTML. That opens Question 7.

### 2. Why was HTTP empty on 2 of 6 courses?
"Some building blocks render server-side, some client-side" is the description,
not the cause. Which building-block type, and how is it recognisable in the
response? Probably answered by (1). This approach was the fastest there ever was
(22s) — it was dropped on day 1 without anyone ever diagnosing the cause.

### 3. ~~Why does `ctx.Route` cost 30%?~~ Answered 2026-08-01, see report below
On every route, whatever pattern is passed in, Playwright installs
`Fetch.enable` on the CDP side with `patterns: [{ urlPattern: "*", requestStage:
"Request" }]` — every single request in the browser pauses and needs a round trip
to the driver process before it continues. The caller-side pattern (e.g.
`**/FolderResource/**`) is only checked afterwards, in the driver process — too
late to avoid the pause/resume round trip. That exactly explains the observed
finding "a pattern that matches nothing still costs ~30%" — unavoidable with a
CDP-side `"*"`, and no choice of pattern rescues it. On top of that, the same
code path arms `Network.setCacheDisabled(true)` for as long as any route is
active — the session's entire HTTP cache is off while interception runs,
regardless of the pattern.

### 4. _(merged into Question 7 — see below)_

### 5. Is "30s" even tied to discovery?
The goal is *"feels like one click"*. Never tested: a background run before the
click, partial results during the run, changed courses first. This class does not
need faster discovery, it needs discovery that does not stand in front of the
user.

**Maintainer's decision, 2026-08-03:** *"work should still go into fast
discovery, but the rest is fine too."* So the campaign is **not** pivoting to the
concealment class — faster discovery stays the main line and keeps its priority
in the question list. But background runs/partial results are explicitly
permissible work rather than an evasive move: they may be picked up when the
discovery line is waiting on a measurement or a question there is exhausted. This
question therefore stays open and does not move up.

### 6. Why does 1 in 12 sections stay unstable across runs?
The rest was traced back to Wicket bookkeeping. This one was not. Possibly the
same cause as Question 17 — there the unstable node is known by name for the
first time and identified as paginated.

### 17. ~~Why does a paginated section lose its second page under contention?~~ Answered 2026-08-03 from data already on disk — and it is a bug, not a speed question
Question 16 found a reproducible loss that does **not** hang on the settle
budget: the same course building block (`CourseNode/1775615795226691003`, 6
files) was missing in 2 of 4 runs, once under the unchanged 500ms/6000ms
configuration and once under 150ms/4000ms.

The plan below was to spend a live run ruling out Candidate C. That run was not
needed. **The maintainer's objection was the reason (2026-08-03): "it is very
weird that consistently 6 files go missing — shouldn't the campaign explain why
that is, before ruling the option out?"** That is Rule 2 applied to our own work:
Question 16 had measured an effect and converted it straight into an exclusion
without ever naming the mechanism. Re-reading the archived run log instead of
launching a new run answered it in minutes, because the crawler had already
detected and reported the failure itself — `warnShowAllTruncated` (`crawl.go`)
exists for exactly this and had fired.

**The correlation in `tmp/frage16-run.log` is 4 of 4:**

| Run | `warnShowAllTruncated` for the Vorlesung node? | Files |
|---|---|---:|
| baseline-1 (500ms/6000ms) | no | **248** |
| baseline-2 (500ms/6000ms) | **yes** | 242 |
| override-1 (150ms/4000ms) | **yes** | 242 |
| override-2 (150ms/4000ms) | no | **248** |

The warning appears in exactly the two runs that lost the six files and in
neither of the two that did not. Its wording pins the branch down: *"expansion
completed but added nothing (41 rows before, 41 after)"*.

**What that settles:**
- **Candidate C (server-side variance) is refuted.** The content did not vary —
  the crawler's own expansion of that section failed, in those runs and not the
  others, and said so at the time.
- **Candidate A ("the click is never triggered") is refuted.** That path exits
  earlier through a different message, *"the control could not be activated"*,
  which does not appear. The control was found and the click was dispatched.
- **Candidate B stands:** the click is dispatched and reports done, then
  `waitForStableExpandedCandidates` reads back the same 41 rows it started with.
  Either Wicket dropped the expansion request, or its response landed after the
  read. Those two are separable by logging at the click itself — but note this is
  the same shape as the already-recorded 2026-07-21 finding that
  `AJAX_CALL_DONE` marks a call finished without marking the DOM complete.

Why exactly 6 and always the same 6: the lost files are `Vorlesung_7`, `_7p`,
`_8`, `_8p`, `_9_10`, `_9_10p` — the tail of one folder, i.e. one OPAL page
boundary. A failed expansion does not lose a random subset, it caps the section
at page 1. The consistency the maintainer flagged as weird is the strongest clue
in the data: intermittent *timing* producing an exact, repeatable *file set* is
the signature of a page boundary, not of a race that eats arbitrary rows.

**Consequence for `course_concurrency>1`: not ruled out, re-classified.** It is
not "concurrency loses files"; it is "an already-known expansion bug fires more
often when the renderer is under load". Fixing the expansion is the prerequisite,
and until then the setting stays as it is — untouched default of 1, no clamp, no
removal (maintainer's decision, 2026-08-03).

### 18. ~~Why is one section permanently truncated at every concurrency, in every run?~~ Answered 2026-08-03 — it never was. The detector was broken, not the crawl.
**Prediction refuted, in the direction the failure criterion named.** No files
are missing, and none ever were. `tmp/showall-href-run.log` (live, small
course, 15.5s): of every row that disappeared across both expanding sections,
**not one** was file-shaped.

`CourseNode/1775529461522481011` is the tutorial **enrolment** table — its
Wicket path says `enrollmentTable`, and its rows are seminar slots
("Nicht eingeschrieben", "Dienstag 2. DS", "APB/E009", "30 / 30"). It holds no
files at all. The five rows it "lost" were the `alle anzeigen` control plus
three pager links (`pager-last` "»", `pager-navigation-1-pageLink` "2",
`pager-next`) plus one untargeted row. Expanding a paginated table removes its
pager, so the raw row count falls while nothing is lost — which is the whole of
the "17 rows before, 14 after" mystery.

The comparison in `expandShowAllInSection` was counting raw candidate rows.
Fixed the same day to count file-shaped rows instead (`countFileShapedCandidates`,
`crawl.go`), and to stay silent on a section with no file rows at all.
Re-verified live: warning gone, file count unchanged at 38.

**The real damage was not the noise.** This warning is the only signal the
project has for a genuine truncation, and it was firing on every run of every
probe — which is exactly why Question 17's real loss sat unread in the same
logs for two days. A detector that always fires detects nothing. The Question 17
diagnosis only happened because the maintainer asked why it was consistently six
files; without that, this warning would still be crying wolf.

**What stays true from the original entry** (the reasoning below was right even
though its conclusion was not): a permanent, identical-every-run loss really
would be invisible to every gate this campaign has, because all of them are
diffs. That remains a live gap in the methodology — it just is not what was
happening here. Do not read "all runs agreed" as "no files lost"; it only ever
meant "nothing *varied*".

**Also confirmed, for Question 17:** the same run shows the paginated *folder*
node `CourseNode/1775615795226691003/Vorlesung` expanding correctly at
`course_concurrency=1` — 41 raw rows to 44, gaining 8 real files including
`Vorlesung_0.pdf`. So the expansion path works; under contention it sometimes
returns 41→41 and drops the tail. That is Candidate B, unchanged and still open.

---

**Original entry, kept for the record — its conclusion was wrong:**

Found while answering Question 17, and more serious than Question 17 itself,
because it is not intermittent and it hits the **shipping default**.

`CourseNode/1775529461522481011` (Algorithmen und Datenstrukturen) emits
`warnShowAllTruncated` in **every single archived run** — all 4 of Question 16 at
`course_concurrency=2`, all 4 of Question 14 at `course_concurrency=1`
(`tmp/debounce-override-run.log`), plus `tmp/cdp-metrics-run.log`,
`tmp/settle-timing-run.log` and `tmp/mutation-concentration-run2.log`. Question
14 had already noticed this section and set it aside as "an already-known
pagination gap independent of the debounce"; what was not drawn from it is that
a *permanent* truncation is invisible to every check this project has.

**Why our own methodology could not see it.** Every correctness gate in this
campaign is a diff — run against run, or run against a stored ground truth. A
section that loses the same rows on every run is byte-identical to itself and to
the ground truth, so all 8 runs of Questions 14 and 15 reported "no deviation"
while this section was truncated in all 8. The 345-file ground truth is very
likely short by the same rows, which would make it a record of the bug rather
than a baseline against it.

**Second, sharper oddity:** the reason string alternates between *"17 rows
before, 17 after"* and *"17 rows before, **14** after"*. The expansion sometimes
comes back with *fewer* rows than the collapsed page had — and
`expandShowAllInSection` returns `expanded` unconditionally (`crawl.go` line
600), which the caller then assigns over the candidate list (line 386-388). So on
those runs a 14-row result replaces a 17-row one. Part of that drop is expected
(the show-all control is itself one of the candidates and disappears once
expanded), but that accounts for one row, not three.

**Next step, decided, and it is not a timing run:** the question is what those 17
rows actually are and what the section holds in a browser. Concretely — (a) log
the candidate hrefs before and after expansion for this one node, rather than
just the counts, and (b) open the section by hand in the login profile and count
the real files. That establishes whether files are being lost, and how many,
before any fix is designed. Cheap, one section, no full crawl.

**Open regardless of the fix:** a truncation that reproduces identically forever
cannot be caught by a self-diff, so "all runs agree" must stop being read as "no
files lost". `warnShowAllTruncated`'s output is currently the only signal that
sees it, and nothing consumes that warning.

### 19. ~~Does the "show all" click get its Wicket signal under contention, or not?~~ Answered 2026-08-04 — the signal never arrives. Candidate A re-opens.
**Prediction refuted, on its own failure criterion.** The prediction (see the
closed "Previous experiment" write-up below) was that the failing runs would
show `expansionSignalled=true` — Wicket says the call finished, the rows just
are not there yet. Instead, both runs that reproduced the loss showed
`expansionSignalled=false`: the click was dispatched (`watchArmed=true`, no
"control could not be activated" warning — the Playwright-level click check
Candidate A was ruled out on before), but `AJAX_CALL_DONE` never arrived
within the 4000ms budget at all.

That is exactly the failure criterion's own re-opening clause: *"if the
failing runs show no signal at all... it is Candidate A after all and the fix
is at the click, not the wait."* Full data and the new open question (does a
longer budget catch it, or does the signal never come no matter how long you
wait) are in "Previous experiment (Question 19, closed 2026-08-04)" below —
that split is now Question 20, the next experiment.

### 22. ~~When the wait fails, what does it actually fail *with*?~~ Answered 2026-08-06 — `context-destroyed`, confirmed live, and the existing reclick fallback had a real gap. Fix landed; the gap it left open is Question 25.
**Prediction confirmed, on its own terms.** The third cycle of the same probe
(`showallsignallatency_probe_test.go`, `OPAL_SIGNAL_LATENCY_TRACE=1`)
reproduced the Vorlesung-tail loss with `expansionSignalled=false signalMs=400
signalWaitErr=context-destroyed` — exactly the predicted classification, not
`timeout` (the failure criterion) and not `other`. `tmp/signal-latency-probe.log`
now holds one confirmed `context-destroyed` sample alongside the two
`none`/clean and two earlier unclassified-`false` (pre-instrumentation)
samples.

**That closed the causal chain Questions 17-22 have been chasing, and reading
the code it pointed at found a real, separate bug.** `context-destroyed` on
the Wicket wait ties directly to the mechanism `waitForInteractiveLinks`'s own
doc comment (`navigation.go`) already names: contention destroys the section
page's execution context around click time, the in-flight AJAX response has
nowhere to land, and the expansion silently drops its tail. `crawl.go` already
had a fallback for exactly this (`expandShowAllInSection`, ~line 561) — reclick
once if `waitForInteractiveLinks`'s own re-probe sees a destroyed context. But
that re-probe runs *after* the click, on a fresh call, and only reports
`true` if it observes the destruction itself; if the context had already been
replaced by the time the re-probe ran, it read a normal working context and
never triggered the reclick — even though the *earlier* Wicket wait had
already told us, directly, that the context was destroyed at the moment that
mattered. Live evidence this happened: the confirmed sample above logged
`truncated=true` and 242 files (the known six-file loss) — the reclick never
fired.

**Fixed (`crawl.go`):** `signalWaitErr` is now hoisted to function scope and
OR'd into the reclick trigger — `(contextWasDestroyed ||
signalWaitErr == "context-destroyed") && !navigated` — so the direct evidence
from the original wait is sufficient on its own, not only the independent
re-probe. This is strictly additive: it only adds a reclick attempt in cases
that already indicate the expansion was dropped, never removes the existing
path, and `attemptShowAllExpandClick`'s own bounded retry (3 tries) caps the
cost. Build and full local test suite (`go test ./...`) pass.

**Honest residual — Rule 2 is not fully satisfied yet.** A 4-run real-account
verification batch the same day (`tmp/q22-fix-verify-run.log`) caught the
condition again and confirmed the fix *fires* correctly: the audit log shows
the reclick dispatching (`click`/`click-success`, "try 1/3") right after the
`context-destroyed` classification. But the reclick's own AJAX call **also**
failed to add rows — `waitForStableExpandedCandidates` polled 4 times
afterward, all reading the same 41 raw rows, and the section was still
reported truncated. So the fix corrects the *trigger* (confirmed to broaden
detection beyond the old re-probe-only path) but is not, on this one sample,
sufficient to *recover* the data. That means the explanation is not yet sharp
enough to predict the fix's own outcome in advance, which is exactly Rule 2's
bar — this stays open rather than being declared solved. Whether a second
reclick, one that re-arms the Wicket watch and waits on its own signal
(the way the sibling `AJAX_CALL_FAILURE` retry path already does, `crawl.go`
~522-532) rather than falling through to the generic stability poll, would
fare better is untested and is the natural next step — **Question 25**.

**Separate finding, not caused by this fix:** immediately after the sample
above, the same verification batch's remaining 3 runs each reported **0
files for both courses** — not a partial loss, a total one, with no error
logged (`"crawled successfully but found 0 files"`). That is not a plausible
consequence of an OR'd boolean condition; the leading hypothesis is a
concurrent-session collision on the shared login-profile — a second live
Claude Code session was confirmed active in this exact checkout at the same
time (two commits, `5c8956a`/`5ebfacd`, landed on top of this run's own
commit while it was in progress) and the profile-lock bug this project
already has open (`docs/BACKLOG.md`, "Two concurrent Routines colliding...")
is exactly this shape. Tracked there, not here — this session stopped running
further real-account probes once the collision was confirmed, rather than
risk compounding it or misreading its noise as a Question 22 result.

### 25. ~~Does rearming the Wicket watch and waiting on its own signal make the context-destroyed reclick actually recover the section?~~ Answered 2026-08-06 — yes, confirmed live, 3/3
**Prediction confirmed.** Profile confirmed quiet first (no `chrome.exe`, last
commit 16 minutes old, clean tree — the concrete check Question 22's own
verification asked for). `crawl.go`'s context-destroyed reclick now mirrors
the sibling `AJAX_CALL_FAILURE` retry: rearm the watch before reclicking,
await `awaitWicketExpansionDone` on the retry, and only fall through to the
generic `waitForInteractiveLinks` wait if that signal doesn't come. New
`wicket-expand-reclick-signal` audit line records the outcome.

Same probe as Question 21/22 (`OPAL_SIGNAL_LATENCY_TRACE=1
OPAL_SIGNAL_LATENCY_RUNS=4`), `tmp/q25-verify-run.log`: 3 `context-destroyed`
events fired across the 4 runs (2 in run 2, 1 in run 3), and **all 3 rearmed
reclicks resolved `expansionSignalled=true signalWaitErr=none`** — the retry's
own signal arrived cleanly every time, none hit the counts-as-failed condition
(`context-destroyed` or timeout again). Zero `warnShowAllTruncated` firings in
any of the 4 runs, and every run reported the full **248/248** files for the
two-course set — including the two runs that actually hit the
context-destroyed condition, which is exactly the outcome the old,
unrearmed reclick (Question 22's fix-verification sample) failed to produce
on its one observed instance.

**This closes the causal chain Questions 17→25 have been chasing since
2026-08-03:** a paginated section's tail going missing under contention was
traced from "an unexplained six-file loss" through "a real expansion bug, not
a concurrency property" (17) → "the signal never arrives, not late" (19) →
"it fails with `context-destroyed`" (22) → "the existing reclick didn't see
its own trigger" (22's fix) → "the reclick's own retry needed the same signal
discipline as its sibling path" (25) → now measured recovering the section
live, 3 for 3.

**Honest bound on "closed":** n=3 live recoveries, all today, both hits on the
same known-flaky section (`Vorlesung`) plus one on the tutorial-enrolment
node. That is real evidence, not a coin flip, but it is not the same weight as
Question 20's 3-clean-runs-is-not-proof caution — a run where the *rearmed*
retry itself reports `context-destroyed` again remains the standing kill
condition, and the next few contention runs (from any future cycle, not a
dedicated batch) are free confirmation if it holds.

**Consequence for `course_concurrency>1` (flagging for the maintainer, not
deciding it here):** the mechanism Question 17 re-classified as "an
already-known expansion bug fires more often under load" now has a live-tested
fix. Whether that changes anything about the setting's default is a product
call outside this file's scope — noted here so it isn't rediscovered from
scratch, not acted on.

### 7. If nothing renders client-side — what fills the 336ms then? (replaces the old Question 4)
The campaign's conclusion from late 2026-07-31 ("the content tree is JS-rendered
at every level") and today's source-code finding ("everything is server HTML, no
client rendering") directly contradict each other — both rest on real evidence
(live DOM probe vs. Java source code + OpenOLAT's own docs), neither is a bare
assertion. That has to be resolved, not silently overwritten:
- **Candidate A:** settle time is network/transfer time of a large server
  response, not JS build time. Plausible, because every course-node page ships
  the course's complete `o_tree` — with 282 sections potentially a large HTML
  document per request. **Measured live 2026-08-01 (see "Next experiment"
  below): refuted in the form tested.** Bytes grow only 1.4x with 27x more
  sections, network share stays at 25–31% — a minority, not the explanation. Open:
  why the bytes do not scale (→ Question 9).
- **Candidate B:** the probe measured something other than the tree/table itself
  — e.g. the hit count of `looksLikeSectionFolderLink` simply grows because the
  browser is still parsing/laying out a large static HTML document, not because
  JS is building something.
- **Candidate C:** a narrowly bounded JS widget on the page (not the tree or the
  table itself) is responsible — untested which one.

### 8. ~~Which of the two `ctx.Route` costs dominates — cache-off or pause/resume?~~ Answered 2026-08-04 — cache-off dominates, and raw CDP genuinely decouples the two
**Both halves of the prediction confirmed, on a local synthetic probe needing
no OPAL account** (`TestCtxRouteCostSplit`,
`internal/scraper/ctxroutecost_probe_test.go`, `OPAL_ROUTE_COST_PROBE=1`): a
page with 25 cacheable static assets, navigated 12 times per condition, 3
repeats, real headless Chromium via `httptest`.

| Condition | Mean elapsed | Per-asset max request count |
|---|---:|---|
| baseline (no CDP) | 114ms | 1 (cache intact) |
| `ctx.Route`, pattern matching nothing | 291ms | 12 (cache defeated) |
| raw `Network.setCacheDisabled(true)` only | 222ms | 12 (cache defeated) |
| raw `Fetch.enable`+`continueRequest` only | 120ms | **1 (cache intact)** |

Against the 177ms `ctx.Route` gap over baseline: cache-off-only accounts for
**60.7%**, fetch-only-only for **3.1%** — cache-off is clearly dominant,
pause/resume is close to free. Consistent across all 3 repeats
(`tmp/ctxroute-cost-split-probe.log`), and the boundary case in the failure
criterion (≥40% for fetchOnly to refute) was not close.

**The sharper finding is the per-asset request count, not the timing.**
`fetchOnly` held every asset at exactly 1 request across all 3 repeats,
identical to baseline — enabling the CDP `Fetch` domain by itself, with no
`Network.setCacheDisabled` call anywhere, left the browser's cache fully
intact. **That refutes the "Playwright couples the two rigidly" framing this
question was opened with.** The coupling was never a CDP protocol
requirement — it is `ctx.Route`'s own driver-side implementation choice
(confirmed by Question 3's original network trace: `ctx.Route` always calls
`Network.setCacheDisabled(true)` regardless of pattern). A caller going
around `ctx.Route` and driving `Fetch.enable`/`Fetch.continueRequest`
directly through `CDPSession.Send` keeps the cache.

**What this changes for `previews.go`:** the file's own header comment
(written 2026-07-27, before this question was answered) assumed the fix
would need "a browser-side blocking mechanism without the CDP `Fetch`
domain" if cache-off turned out to be the main culprit — reasoning that
avoiding `Fetch` entirely was the only way out. That assumption is now
falsified in the direction that matters: `Fetch` itself is nearly free (3.1%
of the gap); the same domain that blocks `/FolderResource/` previews can stay,
it just needs to be driven through a raw `CDPSession` (`Fetch.enable` +
per-request `continueRequest`/`failRequest`) instead of through
`ctx.Route`, to avoid the implicit cache-disable. That is a real, not yet
built, path to recovering most of the ~30% `ctx.Route` tax while keeping the
~30 MB/course preview-blocking saving — see Question 23 below.

**Honest residual, not glossed over:** 60.7% + 3.1% = 63.8%, not 100% — about
36% of the combined `ctx.Route` gap is not explained by either mechanism
measured in isolation. The two are not simply additive when both are active
together (as `ctx.Route` does): something about running Fetch interception
*and* cache-disable simultaneously costs more than the sum of running each
alone. Not chased further here — the question as posed ("which dominates")
has a clear, decisive answer; the interaction term is a separate, smaller
question that would only matter once someone is actually trying to account
for the full 30% rather than just knowing where most of it comes from.

**Also worth recording as a probe-building lesson, not a finding about
`ctx.Route`:** the first run of this probe hung on the 3rd of 3 repeats —
`Fetch.requestPaused`'s handler was calling `session.Send` synchronously,
which re-enters the connection's own dispatch loop from inside that same
loop; with 25 requests pausing near-simultaneously per navigation this
recurses one dispatch frame per pending request, and usually (not always)
resolved before it didn't. Moving the `continueRequest` call onto its own
goroutine fixed it outright — any future raw-CDP event handler in this
codebase that calls `Send` from inside `On` needs to do the same.

### 23. ~~Can `previews.go` block previews through a raw `CDPSession` and keep the saving while paying only the ~3% tax?~~ Answered 2026-08-05 — implemented, and the real-account safety bar refused it
**Built, then failed its own non-negotiable gate.** The rewrite itself
(`attachInlinePreviewBlocker`, `previews.go`) works as designed — a local
no-account probe confirmed a subframe `FolderResource` load is blocked and a
main-frame one is not — but `filelist_probe_test.go`'s byte-diff against the
real account came back **316 files against a 349-file same-day baseline, 33
short**, all 33 in one section: "Softwaretechnologie (SoSe 26)" / Part-3
(`CourseNode/1615865126729195011`). `course_concurrency` and
`section_concurrency` were both 1, so this is not the contention setting
Questions 16/17 already cleared.

**Mechanism, not just a description.** Part-3's own `warnShowAllTruncated`
line in the failing run — *"expansion completed but added no files (18 file
rows before, 18 after; 72 raw rows before, 72 after)"* — is the exact
signature Question 17 already root-caused: Wicket's "show all" AJAX call
dispatches, the framework reports it done, and the resulting DOM rows never
land (Candidate B, still standing from Question 17: *"either Wicket dropped
the expansion request, or its response landed after the read"* — and that
question's own consequence line already says *"an already-known expansion
bug fires more often when the renderer is under load"*). Part-3 is the single
most preview-dense section in the entire account (the run blocked 79-80
previews total across all 6 courses; Part-3's expansion alone adds ~33
preview-bearing rows in one burst) — so it is exactly the page Question 23's
own implementation loads hardest, each of those ~33 near-simultaneous
`Fetch.requestPaused` events answered from its own goroutine
(`previews.go`'s own doc comment already flagged this pattern as
reentrancy-sensitive while the Question 8 probe was being built).

A scoped repro (`previewblockshowall_probe_test.go`,
`OPAL_PREVIEWBLOCK_SHOWALL_TRACE=1 OPAL_BLOCK_FILE_PREVIEWS=1`, Part-3's own
course crawled alone) did **not** reproduce the loss —
`expansionSignalled=true`, `signalMs=293`, `signalWaitErr=none`, poll trace
72→105 candidates, clean. That is consistent with the mechanism above rather
than refuting it: Question 17's own bug is already documented as
intermittent ("fires more often under load", not "fires whenever this code
runs"), and a single clean scoped sample carries the same evidentiary weight
Question 20 already warned about — *"at this condition's ~33-50% historical
failure rate that is not proof of X, just a plausible outcome either way"*.
One fail + one clean sample, under different conditions (5th-of-6-courses
sustained crawl vs. this section alone), is not enough to separate "raw CDP
amplifies the pre-existing bug" from "raw CDP does something additional" —
that residual is the open question below.

**Consequence: does not ship, and stays closed at this point, not deferred
for a retry.** `OPAL_BLOCK_FILE_PREVIEWS` was already off by default and
stays off — no user-visible change either way. The prerequisite is the same
one Question 17 already named for `course_concurrency>1`: fix the "show all"
expansion bug itself (Candidate B) before any code that adds load anywhere
near it, including this one, can be considered safe to enable. Rewriting
`blockInlineFilePreviews` again with a different concurrency model for the
`Fetch` handler (e.g. bounding how many `requestPaused` events are answered
concurrently) is a plausible mitigation but untested and not worth building
until the underlying bug has a real fix — it would only be patching the
symptom this implementation happens to trigger most easily.

**New open question (Question 24, ranked low — real-account load caution
already active today, 3 live crawls run for this question alone):** is
Question 23's loss purely downstream of Question 17's pre-existing Candidate-B
bug, or does the raw-CDP goroutine-per-`requestPaused` pattern add a distinct
failure mode of its own? Separating them needs either (a) Question 17's bug
fixed first, so Question 23 could be retested against a stable baseline, or
(b) several paired matched-condition runs targeting Part-3 specifically
(blocking on vs. off, same position in a multi-course crawl) to compare
failure rates directly — expensive against the real account and not worth it
until (a) is closer.

### 9. ~~Why does the section-page response barely grow with course size?~~ Answered 2026-08-01, see report below
**Candidate (a) confirmed, with evidence — rule 2 satisfied.**
`MenuTreeRenderer.isRenderChildren()` (OpenOLAT source, method from line 660)
only recurses into a child node if its `ident` is in `openNodeIds` or it lies on
the path to the currently selected node (`curSel == curRoot`); otherwise the
method returns `false` and `renderLevel()` (line 232: `if (renderChildren) {
renderChildren(...); }`) never even makes the recursive call for that subtree. So
the tree fragment is structurally limited to "open nodes + selection path", not
to "all sections" — exactly the mechanism that would have predicted the measured
1.4x/27.3x discrepancy. Candidate (b) (caching) is therefore no longer needed to
explain the finding, and was not tested separately.

What that means for Candidate A (Question 7): **finally dead**, now with a
mechanism instead of just a counter-proof. What stays open: if neither the tree
fragment nor the transfer time scales with course size, but settle+stable still
takes 511–525ms/section (Candidate B/C, 69–75% unexplained) — does that time
perhaps scale with something other than course size, e.g. the file count *inside*
the section currently being visited, rather than the course's total section
count? Untested, and that is now the concrete next question ahead of the browser
profiling already announced in Question 7.

---

## Next experiment

**Question 26 (new, 2026-08-06): now that Question 25 gives the
context-destroyed reclick a live-tested recovery path, does Question 23's
raw-CDP preview-blocking rewrite pass its own byte-diff safety bar on a
retry?** Question 23 built `attachInlinePreviewBlocker` (`previews.go`) —
recovers most of the ~30% `ctx.Route` tax while keeping the ~30 MB/course
preview-blocking saving — then refused itself on a 33-file loss in
"Softwaretechnologie (SoSe 26)" / Part-3, whose own `warnShowAllTruncated`
line was Question 17's Candidate-B signature exactly. Question 24 named the
prerequisite for a retry: "(a) Question 17's bug fixed first, so Question 23
could be retested against a stable baseline." Question 25 is that fix, live-
verified 3/3 today. **Prediction:** `OPAL_FILELIST=after
OPAL_BLOCK_FILE_PREVIEWS=1` against the full real account now diffs empty
against an `OPAL_FILELIST=before` run (`filelist_probe_test.go`, `tmp diff`
per its own header) — Part-3 no longer loses its 33 files, because the
reclick that used to fail to recover the section now does. **Counts as
failed at:** the diff is non-empty anywhere, in Part-3 or elsewhere — that
would mean either context-destroyed wasn't the whole Candidate-B story, or
Question 24's alternative holds (raw-CDP's goroutine-per-`requestPaused`
pattern adds a distinct failure mode of its own, not just amplifying the old
one). Either outcome is informative: a clean diff ships a real win Question 23
had ready and shelved; a dirty one separates the two Question 24 could not.
**Cost:** the most expensive experiment in this queue — two full 6-course
crawls (before/after), real account, no scoped shortcut (the whole point is
the full-account byte-diff). **Not run this cycle:** today's server-load
budget already spent one contention batch (`tmp/q25-verify-run.log`, ~15 min,
this cycle); stacking a two-pass full-account crawl on top of it the same day
is the kind of load-timing choice `docs/server-load.md` exists to make
deliberately, not by default. Next cycle, fresh day or clear budget.

---

## Previous experiment (Question 8, closed 2026-08-04)

**Question 8 — which of the two `ctx.Route` costs dominates: cache-off, or
the Fetch pause/resume round trip?** Question 3 found both mechanisms behind
the same ~30% tax (`previews.go`'s header comment: cache-off from
`Network.setCacheDisabled(true)`, plus a per-request CDP pause/resume round
trip, both triggered unconditionally the moment `ctx.Route` is installed,
independent of the pattern) but never separated which one actually costs the
time, and noted Playwright 1.61.1 does not let a caller decouple them through
`ctx.Route` itself. Untested until now whether the *raw* CDP protocol has the
same rigidity — `ctx.Route`'s coupling could be Playwright's driver-side
choice rather than a Chrome/CDP requirement.

**Mechanism and prediction:** OPAL's Wicket framework serves a large,
mostly-unchanging JS/CSS asset bundle on every section page — with caching
intact, later section navigations in the same session should mostly skip
re-fetching it (memory/disk cache hit); with `Network.setCacheDisabled(true)`
forced on, every one of ~284 section navigations re-fetches the whole bundle.
The pause/resume round trip, by contrast, is a small, roughly constant
per-request tax that does not scale with how many navigations reuse the same
asset. **Prediction: cache-off is the dominant component — at least 60% of
the gap between baseline and the full `ctx.Route` condition — not the
pause/resume round trip.** Sub-question, testable in the same run: does
raw `Session.Send("Network.setCacheDisabled", ...)` and raw
`Session.Send("Fetch.enable", ...)`/`"Fetch.continueRequest"` actually
decouple, i.e. does enabling the Fetch domain by itself (no explicit
`setCacheDisabled` call) leave the browser's asset cache intact? Predicted:
**yes, they decouple** — cache-off and Fetch interception are independent
CDP mechanisms, and Playwright's `ctx.Route` wrapper enables both together as
an implementation choice, not because Chrome requires it.

**Counts as failed at:** if the pause/resume-only condition (Fetch domain
active, cache left alone) accounts for ≥40% of the `ctx.Route` gap, cache-off
is not clearly dominant and the mechanism split needs a different framing.
If enabling the Fetch domain via raw CDP *also* silently defeats the cache
(assets re-fetched every navigation even without an explicit
`setCacheDisabled` call), the decoupling sub-question is refuted — that would
mean Chrome itself ties the two together at the protocol level, and no driver
change could ever separate them, closing off the "ship previews.go without
paying the cache tax" idea Question 8's problem statement was written for.

**Design:** a local `httptest` server (like `discovery_browser_test.go`'s
existing no-account browser-probe pattern) serves a page referencing ~30
small static assets with `Cache-Control: public, max-age=3600`, navigated to
repeatedly (same browser context, simulating repeated section visits) under
four conditions — no CDP at all (baseline), `ctx.Route` on a pattern matching
nothing (reproduces Question 3's coupled tax locally, confirms the harness is
comparable), raw `Network.setCacheDisabled(true)` alone (isolates cache-off),
raw `Fetch.enable`+immediate `continueRequest` alone (isolates pause/resume).
The server counts requests per asset path itself: cache intact means ~1
request per asset across all navigations, cache defeated means one request
per asset *per navigation* — a direct, non-timing-based check of which
conditions actually disable caching, independent of the wall-clock numbers.

**Cost:** local browser only, no OPAL account, no real-account load added.

---

## Previous experiment (Question 22, first cycle 2026-08-04 — no failure
reproduced, prediction untested this round)

**Question 22 — when the wait fails, what does it actually fail *with*?**
Opened by Question 21's first live cycle (below): the elapsed-time
instrumentation revealed that a failing wait does not reliably consume the
timeout budget at all, which the original Question 21 framing (and the
`signalMs` doc comment written earlier the same day) both assumed without
checking. Two contention runs both resolved in ~200ms with
`expansionSignalled=false` — the same order of magnitude as a successful
signal, nowhere near the 4000ms ceiling. The only way `WaitForFunction` exits
that fast without the predicate becoming true is an error, and the error text
itself was being discarded, so its cause was invisible.

**Cost paid already, ahead of the run:** `awaitWicketExpansionDone` (`wicket.go`)
now returns the real error instead of swallowing it, and `classifyWicketWaitError`
buckets it into a grep-stable category (`none`/`timeout`/`context-destroyed`/
`navigation`/`closed`/`other`) logged as `signalWaitErr` on the existing
`wicket-expand-signal` audit line. Not yet run live — this is the instrumentation
for the next cycle, the same pattern Question 20/21 used (build, predict, then
run next time).

**Prediction:** failing runs (expansionSignalled=false, signalMs well under
the 4000ms budget) show `signalWaitErr=context-destroyed` — i.e. the page's
execution context is invalidated by something (most likely a navigation)
shortly after the click, which is also the exact mechanism `waitForInteractiveLinks`'s
`contextWasDestroyed` fallback (`crawl.go`, a few lines below this code) already
exists to catch downstream. If that fallback is in fact already catching most
of these cases, this closes the causal chain Questions 17-21 have been
chasing since Question 16: contention → something destroys the section page's
execution context around click time → the in-flight AJAX response (if any)
has nowhere to land → `expansionSignalled=false`, fast, not slow.

**Counts as failed at:** `signalWaitErr=timeout` in a failing run — that would
mean the wait genuinely does consume the full budget sometimes (reviving
Candidate A1, pure delay) and the two ~200ms samples so far were a
coincidence, not the dominant shape. `signalWaitErr=other` with a wait
duration close to signalMs's earlier fast values, but no recognizable
navigation/context-destruction wording, would mean a third, not-yet-named
failure mode.

**Cost:** reuses the same probe and condition as Question 21 (below) — no new
run type needed, just read the new field once the next contention cycle runs.
Given today's already-heavy real-account load from this sub-thread (8
two-course contention crawls across Questions 19-21, see `docs/server-load.md`),
this explicitly waits for a later cycle rather than running immediately.

**Result of this cycle (2026-08-04, 2 runs, `tmp/signal-latency-probe.log`):
prediction untested — the failure did not reproduce, but the null result still
narrows the field.**

| Run | Files | Vorlesung node |
|---|---:|---|
| 3 | 248 | `expansionSignalled=true signalMs=167 signalWaitErr=none` |
| 4 | 248 | `expansionSignalled=true signalMs=177 signalWaitErr=none` |

Both runs came back clean — no loss, no `expansionSignalled=false` — so there
was no failing sample to classify `signalWaitErr` on. At the condition's own
historical ~33-50% failure rate, two clean runs in a row land at roughly
25-45% by chance alone (0.5²-0.67²): unlucky for this cycle, not surprising on
its own. **Counts as failed at** (from above) was never reached either, since
that requires a failing run to exist at all — this cycle answers neither
branch of the prediction.

**What did move: the accumulated `signalMs` distribution across both cycles
now includes clean and failing samples in the same narrow band.** Four
contention-run samples exist so far, two failing (Question 21: 196ms, 206ms)
and two clean (this cycle: 167ms, 177ms) — all four inside a 40ms span, with
no outlier anywhere near the 4000ms ceiling in either direction. If
`expansionSignalled=false` were ordinary queueing delay (Candidate A1, "pure
delay" reviving under contention), a failing sample should on average run
longer than a clean one — the AJAX response is either slow or absent, and
"slow" should show up as elevated latency before the eventual `false`. It
does not: on this small sample, failing and clean resolve equally fast, i.e.
duration alone cannot tell them apart. That is weak, not decisive (n=4), but
it points the same direction as the `context-destroyed` prediction rather
than against it — an invalidated execution context would abort the wait
immediately, not slowly, which is exactly what all four samples show
regardless of outcome. `signalWaitErr` on an actual failing run is still the
only way to confirm the mechanism rather than infer it from timing.

**Real-account load caution, updated:** this sub-thread has now spent 10
two-course contention crawls today (8 before this cycle, 2 more here) — see
`docs/server-load.md`. Deliberately not chasing a failing run further today;
the next cycle either lands on a failing sample by chance (as Question 21's
did) or it does not, and forcing a larger batch to guarantee one is exactly
the "large batch" this sub-thread has been avoiding on purpose.

---

## Previous experiment (Question 22, second cycle 2026-08-06 — confirmed, closed)

**Result: prediction confirmed exactly.** Run 2 of 2 reproduced the loss:
`expansionSignalled=false signalMs=277 signalWaitErr=context-destroyed
truncated=true`, 242 files. Not `timeout` (the failure criterion), not
`other` — `context-destroyed`, the predicted bucket, on the first failing
sample this instrumentation ever saw.

**Consequence:** see "### 22." in the ranked list above for the full
mechanism write-up, the fix landed in `crawl.go`, its live verification (the
fix's trigger fires correctly; the reclick itself did not recover the section
on the one sample tested — Question 25 opened), and the separate
concurrent-session collision this cycle's verification batch surfaced.

---

## Previous experiment (Question 21, first cycle 2026-08-04 — inconclusive on
its own question, but surfaced a sharper one)

**Question 21 asked:** how long does the signal actually take to arrive, on
every run, not just the failing ones? Question 20 raised the ceiling to
15000ms and got 3 clean runs in a row (248/248/248 files,
`expansionSignalled=true` every time) — consistent with Candidate A1 (pure
delay, the wider budget masks it) but, by its own written failure criterion,
*not proof*: at the observed ~33-50% base failure rate, 3 clean runs in a row
happen by chance alone something like 1-in-3 to 1-in-8 of the time. A boolean
(did it signal within N ms) cannot separate "it always takes ~150ms and this
run was just lucky" from "it usually takes ~150ms but occasionally spikes past
4000ms under load, and this run's spikes happened to land under 15000ms" —
both produce the exact same observation.

**Prediction (written before this cycle's run):** instrumenting the actual
elapsed time from click dispatch to `AJAX_CALL_DONE` (not just a threshold
boolean) across many runs — clean and failing alike — of the Vorlesung node
under contention shows a **bimodal** distribution: a tight cluster near the
previously-measured 156-184ms, plus a small number of outliers stretching
into the seconds, rather than a smooth spread that would suggest ordinary
queueing delay.

**Counts as failed at:** a smooth, unimodal distribution with no outliers
beyond ~500ms-1s.

**Result of this cycle (2 runs, `tmp/signal-latency-probe.log`): too few
samples to call bimodal-vs-smooth either way — but both samples independently
contradict an assumption the instrumentation's own doc comment made the same
day, before ever measuring it.**

| Run | Files | Vorlesung node |
|---|---:|---|
| 1 | 242 | `expansionSignalled=false signalMs=196` (loss reproduced) |
| 2 | 242 | `expansionSignalled=false signalMs=206` (loss reproduced) |

Both runs reproduced the Vorlesung-tail loss (242, the known "lost the six
files" count). Both show `signalMs` around 200ms — matching the historical
156-184ms *successful*-signal range, not anywhere near the 4000ms timeout
ceiling. **That refutes the assumption written into `crawl.go` earlier the
same day** ("when expansionSignalled=false the number is the timeout budget
itself, not a real latency") — a real timeout would show ~4000ms, not ~200ms.
The only way `WaitForFunction` can return this fast without the predicate
becoming true is an error, not a timeout — and that error was being discarded
entirely, so the mechanism was invisible until this run exposed the
contradiction. Fixed the same commit as this result (see Question 22 above).

**Why this isn't a clean answer to Question 21 as posed:** 2 samples is far
below "more than 3" the cost note itself called for, and both happen to be
close together rather than spread — consistent with either a genuinely tight
non-timeout failure mode (supports the new Question 22 hypothesis) or simply
too small a sample to say anything about shape. Rule 2 applies against
jumping to a conclusion here: "both failures are fast, not slow" is a
correlation across 2 points, not yet a mechanism — that is exactly what
`signalWaitErr` (Question 22) is built to name directly instead of inferring
from timing alone.

**What actually moved:** the instrumentation caught its own wrong assumption
within the same day it was written, before that assumption could mislead a
future cycle — worth recording as a case where writing the prediction down
first paid off in an unplanned way (the *comment*, not just the experiment,
turned out to have a falsifiable claim in it, and the very next run falsified
it).

---

## Previous experiment (Question 20, closed 2026-08-04)

**Result: inconclusive by its own failure criterion — 0 of 3 runs reproduced
the loss even with the ceiling raised to 15000ms, but the report said in
advance that a clean result here would not be proof on its own.** Raw data:
`tmp/showall-signal-timeout-probe.txt`.

| Run | Files | Vorlesung node (15000ms budget) |
|---|---:|---|
| 1 | 248 | `expansionSignalled=true` |
| 2 | 248 | `expansionSignalled=true` |
| 3 | 248 | `expansionSignalled=true` |

248 is the known "nothing lost" file count for this course pair (Question
16's clean runs). All three runs signalled cleanly, well inside even the
original 4000ms window judging by the total run time — no evidence of a
near-miss that the wider budget rescued.

**Why this is not the same as confirming Candidate A1.** The test file's own
verdict logic said so before the run: at a ~33-50% historical failure rate for
this exact condition, 3 clean runs in a row is not a rare event by chance
alone. A boolean pass/fail at one threshold cannot distinguish "the call is
always fast and this was luck" from "the call is fast until it occasionally
stalls past 4000ms, and today's stalls (if any) happened to land under
15000ms" — both look identical from outside. Rule 2 applies against our own
result here, not just against the original prediction: neither this run nor
Question 19's counts as a mechanism yet, only as two data points.

**What actually moved:** the diagnostic tool itself now exists and works
(`OPAL_WICKET_SIGNAL_TIMEOUT_MS_OVERRIDE`, `effectiveWicketExpansionSignalTimeoutMs`,
`crawl.go`) — off by default, ready for reuse. What is missing is not more
runs at a single threshold but the actual timing distribution, which needs one
more small instrumentation step (timestamping the arm/signal calls) rather
than another blind contention batch. That is Question 21.

---

## Previous experiment (Question 19, closed 2026-08-04)

**Result: prediction refuted, on its own failure criterion — the failing
runs got no signal at all, not a late-but-real one.** Raw data:
`tmp/showall-signal-probe.txt` (not committed, `tmp/` is gitignored, same as
every other probe's raw output in this file).

Implementation: `expandShowAllInSection` (`crawl.go`) now logs
`expansionSignalled` itself via `auditLog("wicket-expand-signal", ...)` —
previously this value was only ever observable indirectly, through whether
the settle wait got skipped. A new probe
(`internal/scraper/showallsignal_probe_test.go`,
`TestShowAllSignalUnderContention`, `OPAL_SHOWALL_SIGNAL_TRACE=1`) ran the
known-failing condition — Algorithmen und Datenstrukturen + Softwaretechnologie
at `course_concurrency=2`, unchanged/default debounce — 3 times, greeping the
captured `--debug-clicks` output for the Vorlesung node
(`CourseNode/1775615795226691003`) each run.

| Run | Files | Vorlesung node | `warnShowAllTruncated`? |
|---|---:|---|---|
| 1 | 205 | `expansionSignalled=true` | no |
| 2 | 199 | **`expansionSignalled=false`** | **yes** |
| 3 | 223 | **`expansionSignalled=false`** | **yes** |

Reproduced the loss in 2 of 3 runs — consistent with the 2-of-4 rate in the
archived Question 16/17 data, so this cycle's sample is in line with prior
ones, not an outlier. In both losing runs the click was dispatched (no
"control could not be activated" warning, `watchArmed=true`, 41 candidates
seen before the click, matching every other run) but Wicket's
`AJAX_CALL_DONE` never fired within the 4000ms
`wicketExpansionSignalTimeoutMs` budget. The one clean run (run 1) shows the
signal arriving normally (`expansionSignalled=true`).

**What that settles:** Candidate B (the prediction — "the signal arrives, the
read is just too early") is refuted outright: there is no late signal to be
too early for, because none arrived in the failing runs within the budget
this code actually waits on. **Candidate A re-opens**, but in a sharper form
than the original Question 17 framing: it is not "the click never registers"
(already ruled out — the Playwright-level click succeeds every time, in
failing and clean runs alike) but "the click's AJAX call does not signal
completion in time" — which could still be pure delay under contention (an
honest budget problem) or a call that is never actually issued/received (a
race at the click/arm boundary, not a timing one). Those two need opposite
fixes, which is why this closes into Question 20 rather than straight into
a fix.

**One loose end, explicitly not resolved by this run:** `waitForStableExpandedCandidates`
still runs its full poll budget (up to 15 × 400ms = 6s, 3 consecutive stable
reads required under contention) after `expansionSignalled=false`, and *still*
reads back 41 unchanged rows for the whole budget in both failing runs — so
whatever is happening to the AJAX call, it is not resolving somewhere in that
extra 6 seconds either. That rules out "the poll's own budget is merely too
short" as a fix on its own; the loss is upstream of the poll, at the signal
itself.

---

## Previous experiment (Question 18, closed 2026-08-03)

**Result: prediction refuted, on the failure criterion's own terms — no files
were being lost.** Full write-up in Question 18 above. The prediction and
criterion as written before the run:

**Prediction (written before the run):** opening
`CourseNode/1775529461522481011` by hand in the login profile shows **more than
17 files**, and the hrefs logged before/after expansion show the post-expansion
list is a subset of the pre-expansion one plus nothing — i.e. real files are
missing from every sync this project has ever done, including the 345-file
ground truth. Mechanism: the section advertises a "show all" control, so OPAL
itself says there is more than one page; an expansion that returns the same or
fewer rows therefore leaves page 2 unread.

**Counts as failed at:** if the browser shows exactly 17 files, nothing is being
lost and the warning is a false positive on a section whose control is
decorative — then the finding inverts into "`warnShowAllTruncated` over-reports",
which matters just as much, because it is the only signal that sees this class at
all. Either outcome is worth the run; there is no result here that wastes it.

**Cost:** one section, opened once, plus href-level logging on that one node. No
full crawl, no repeated runs, negligible server load.

**Why this goes ahead of any remaining speed lever:** it is a correctness
question about the shipping default, and this repo puts reliability over
features. It is also the cheapest open question in the file.

**How it actually went:** the run cost 15.5s and answered it outright. Worth
recording that the "either outcome is worth the run" clause paid off — the
prediction was wrong, and the run was still the most useful thing done all day,
because the inverted outcome (a detector that over-reports) was named in advance
as mattering just as much. A prediction written only to be confirmed would have
had nowhere to land this result.

---

## Previous experiment (Question 16, closed 2026-08-03)

**Question:** (16, new from Question 15) — does the 150ms
`mutationObserverDebounceMs` also hold under real `course_concurrency>1`
contention (several course tabs rendering at the same time, competing for
CPU/event loop)? Question 15 deliberately did not test that (see its "Reference
point": `OPAL_DEBOUNCE_MS_OVERRIDE` short-circuits the
`effectiveCourseConcurrency() > 1` branch in `contentSettleWaitBudget()`
entirely, so a solo-course run with `SetCourseConcurrency(2)` produces no real
concurrency at all). Contention is exactly where every real data loss in this
campaign historically happened (`docs/sync-speed-campaign.md`;
`course_concurrency=2` lost 9 files on 2026-07-26). The project itself already
considers 500ms/6000ms (instead of 300ms/4000ms) necessary as soon as contention
is present — the open question is whether a lowered serial debounce undercuts
that margin when the override sets both values instead of only the serial one.

**Reference point (read before the prediction):** under contention the override
lowers **two** values at once, not one. `contentSettleWaitBudget()`
(`navigation.go` lines 397-405) returns `(ms, mutationObserverHardCapMs)` when
the override is set — i.e. the **serial** hard cap (4000ms), not the concurrent
one (6000ms). A run with `OPAL_DEBOUNCE_MS_OVERRIDE=150` under
`course_concurrency=2` therefore runs 150ms/4000ms against the comparison base of
500ms/6000ms: debounce down to 30%, hard cap down to 67%. That is a sharper test
than Question 14/15 (only 150ms/4000ms against 300ms/4000ms there, hard cap
unchanged), and on failure the cause has to be separated between the two
quantities.

**Prediction:** all four runs (2× override 150ms/4000ms under real contention, 2×
unchanged 500ms/6000ms) find the same file set — no self-diff, no cross-diff.
Mechanism: the debounce measures *quiet after the last mutation*, not absolute
time. Contention delays the mutations themselves, so it shifts the window later
rather than shortening it; it only produces a loss if it tears *gaps inside* the
rendering wider than 150ms (the renderer does not get the CPU for >150ms even
though it is not finished). Question 9 (tree fragment structurally limited to open
nodes) additionally says the amount to be rendered per section does not grow under
contention. Time saving expected **below** the 28.7% of Question 15 — under
contention more of the time sits in actual rendering and in the hard cap, where
the override saves nothing.

**Counts as failed at:** any file/byte deviation in either direction — self-diff
between two runs of the same condition, or cross-diff between override and
baseline. Contention is exactly where every real data loss in this campaign
happened (`course_concurrency=2` lost 9 files on 2026-07-26), so a single missing
file here is a no, not measurement noise. On failure the next question is not "is
150ms too short?" but which of the two lowered values it was: a repeat run with a
150ms debounce **and** an explicit 6000ms hard cap separates that.

**Cost:** four runs of two courses crawled simultaneously (small + large, as in
Question 15). No default changes — the override is test-only and off by default.

**Result (2026-08-03): prediction refuted in both parts — but not where it was
attacked. Question 16 as posed is not answerable.** Raw data:
`tmp/debounce-contention-probe.txt`.

| Run | Files | settle+stable |
|---|---|---|
| baseline-1 (500ms/6000ms) | **248** | 130362ms |
| baseline-2 (500ms/6000ms) | **242** | 132070ms |
| override-1 (150ms/4000ms) | **242** | 63769ms |
| override-2 (150ms/4000ms) | **248** | 67055ms |

The file sets differ — but **the baseline differs from itself**. `baseline-1`
against `baseline-2` differ by exactly the same 6 files as every other
comparison, and every condition produced 248 once and 242 once. There is no
stable condition here, and therefore nothing to measure 150ms against: **the
unchanged, currently shipping configuration loses just as much under
contention.** That does not exonerate the override, it removes the experiment's
comparison base. A follow-up run with a 150ms debounce and the 6000ms cap
restored (the separation step planned above) would now be pointless — it would
measure against the same unstable baseline.

The 6 files are always the same, from **one** course building block
(`CourseNode/1775615795226691003`, `Vorlesung_7`/`7p`/`8`/`8p`/`9_10`/`9_10p`) —
and the run log says of exactly that node `offered a "show all" control`, so it is
**paginated**. So the loss does not point at the settle budget but at the Wicket
"show all" click path (`crawl.go`), which already carries this campaign's history
of losses anyway: under contention either the click is not executed or its result
is not read before the section counts as done. A settle debounce that measures
quiet after mutations cannot possibly wait for a second page that was never
requested.

The timing part of the prediction ("saving below 28.7%") was also wrong, for an
uninteresting reason: **50.1%** was measured, because the baseline here is 500ms
and not the 300ms of Question 15 — 150ms is a much larger relative cut against
500ms. That was derivable before the run and was overlooked while writing the
prediction. Wall clock saves considerably less (169.1s → 151.4s), because a
growing share of the runtime under contention does not sit in the settle wait.

**Today's users are unaffected:** `DefaultCourseConcurrency = 1`
(`internal/config/config.go` line 343), and at `concurrency=1` Questions 14 and 15
found identical file sets over four and four runs respectively. But the finding
rules out `course_concurrency>1` as a speed lever further — and for the first time
delivers a named mechanism instead of the previous observation "course=2 lost 9
files on 2026-07-26".

---

## Previous experiment (Question 15, closed 2026-08-02)

**Question:** (15, new from Question 14) — Question 14 confirmed the lowered
`mutationObserverDebounceMs` (150ms) only on the **small** course (6 sections, 38
files, `course_concurrency=1`; one already-known paginated section behaved
identically in all 4 runs). This campaign's historical data-loss incidents
(Wicket AJAX race, `docs/sync-speed-campaign.md`;
`sectionContentRequiredStableReads`'s own history, `crawl.go` line 920ff.) all
happened under **contention**: a large course with many paginated sections, often
additionally under `course_concurrency>1`, where the renderer no longer has the
machine to itself. Question 14's test case covers exactly none of that. Does the
lowered debounce also hold on the large course (Softwaretechnologie, 164
sections) and/or under `course_concurrency>1` — or does exactly the kind of loss
show up there that the existing safety measures (a separate, wider
`mutationObserverConcurrentDebounceMs` budget) already hint at?

**Reference point (read before the prediction):**
`mutationObserverConcurrentDebounceMs` (`navigation.go` line 127) stands at 500ms
against 300ms serial — the project itself already considers 67% more safety
margin necessary for contention. But `contentSettleWaitBudget()` (lines 397-401)
checks `OPAL_DEBOUNCE_MS_OVERRIDE` **before** the `effectiveCourseConcurrency() >
1` branch — if the override is set it comes back regardless of concurrency, so
the 500/300 margin does not exist for the override path at all. That means real
concurrency contention (competing tabs sharing CPU/event loop) cannot be tested
with the existing probe (`debounceoverride_probe_test.go`, only
`sc.collectCourseFiles` on a single course) — `SetCourseConcurrency(2)` on a
solo-course run would only populate the unused branch differently, without a
second tab ever actually rendering alongside. This round therefore deliberately
tests course size only (large course, `course_concurrency=1`); real multi-course
concurrency remains Question 16.

**Prediction:** two runs of the existing probe against the large course
(`Softwaretechnologie (SoSe 26)`, 164 sections) at a 150ms override find the same
file set in both runs (self-consistency) — the mechanism found in Question 9 (tree
fragment structurally limited to open nodes, not to course size) predicts that the
debounce acts per section independently of the total section count, so Question
14's correctness finding transfers from the small to the large course. A fresh
300ms baseline run on this course is deliberately NOT repeated: the 2026-07-16
live test (`navigation.go` lines 91-100) already confirmed 300ms on exactly this
course against a 344/344 ground truth — a new baseline run today would be a third
confirmation of the same long-established number, not new evidence. The comparison
is instead against this course's historical 198-file number.

**Counts as failed at:** any file/byte deviation between the two 150ms runs, or
against the historical 198-file number (same criterion as Question 14: by our own
history a single clean run is not enough, two are the minimum). Additionally: if
the saving on the large course is far below the ~29.6% Question 14 measured on the
small course (e.g. <15%), that does not refute correctness, but it does refute the
assumption "the mechanism is independent of course size".

**Cost:** two full crawls of the large course (164 sections) instead of the full
four-run probe (baseline dropped for the reason above) — at ~230s/run at 300ms
historically, faster at 150ms, estimated ~5-6 minutes total, fits in a single time
window. No production code change needed (`OPAL_DEBOUNCE_MS_OVERRIDE` already
exists). `course_concurrency>1` stays unanswered (see reference point above) —
that is the new Question 16, and it needs different tools (a real two-course
parallel crawl), not just this probe with a different flag.

**First attempt (2026-08-02, `opal-downloader-autopilot`): no result, collision
with a second routine run going at the same time, not the mechanism under test.**
`docs/BACKLOG.md`'s Noticed entry has the details. Two processes accessed
`login-profile` in the same real-time window; the other run failed with a raw
Playwright launch timeout, this run hung for 22 minutes until its own `go test
-timeout 20m` killed it — both without the clean `ErrProfileLocked` that
`acquireSessionLock` is supposed to deliver. No file finding, no regression
measured — the question was left open, not answered.

**Second attempt (2026-08-02, right afterwards, verified no other
opal-downloader process was running): no result again, this time the 2026-07-31
legacy 300s course-list timeout, with no detectable collision.** `ensureSession:
timed out after 300000ms waiting for the OPAL course list after login` after
305.98s — TU-Fast opened the login window, but the course list never appeared. No
debug flag was on, so nothing concrete was captured. Fixed for next time:
`waitForLoggedInCourseLink` (`session.go`) now folds the page URL directly into
the returned error on timeout, unconditionally, not hidden behind
`--debug-clicks`.

**Third attempt (2026-08-02): ran through mechanically, but with a discrepancy
that made another round necessary.** `OPAL_DEBOUNCE_OVERRIDE_SKIP_BASELINE` (no
fresh 300ms run, compared only against the historical 198-file number from
2026-07-16): 210 files, self-consistent across both 150ms runs — but 210 ≠ 198.
Two explanations were compatible with that: (a) the course is an active SoSe 26
course and genuinely gained 12 files in 2.5 weeks, or (b) the override is doing
something wrong. The skip-baseline design could not distinguish the two — exactly
the gap a fresh comparison on the same day closes.

**Fourth attempt (2026-08-02), full probe (2 baseline + 2 override, no skip):
prediction confirmed, discrepancy resolved.**

| Run | Files | settle+stable |
|---|---:|---:|
| baseline-1 (300ms) | 210 | 86670ms |
| baseline-2 (300ms) | 210 | 86376ms |
| override-1 (150ms) | 210 | 61583ms |
| override-2 (150ms) | 210 | 61837ms |

The fresh baseline run itself finds 210 files, not 198 — the discrepancy in the
third attempt was course-content drift (explanation a), not an override side
effect. All three comparisons (baseline self-consistency, override
self-consistency, baseline vs. override) are **exactly identical**, 210 files in
all 4 runs. Saving: mean settle+stable drops from 86523ms to 61710ms, **28.7%** —
practically identical to the small course's 29.6% (Question 14), despite a 35x
larger course (210 vs. 6 sections' worth of affected magnitude by file count).
That is the signature rule 2 demands: had the saving been course-size dependent,
the two courses would not have agreed to within one percentage point.

**Question 15 answered, with a caveat:** for `course_concurrency=1` the 150ms
debounce's correctness holds on both courses tested (small: 6 sections/38 files,
large: 164 sections/210 files), with practically identical savings (29.6%/28.7%) —
the mechanism (300ms is the binding constant, not course-size dependent render
work, see Question 9 and Question 13) predicts exactly this result and is
confirmed by both runs, not just by a lucky hit. **Explicitly not tested:**
`course_concurrency>1` — see "Reference point" above for why the existing probe
cannot measure it at all. That is precisely where every real data loss in this
campaign historically sat, so correctness under contention is the precondition for
a real default change, not this round — Question 16.

---

## Previous experiment (Question 14, closed 2026-08-02)

**Question:** (14, new from Question 13) — `mutationObserverDebounceMs` (300ms,
`navigation.go` line 99) and `sectionContentPollIntervalMs` (150ms, `crawl.go`
line 982) are fixed constants. Question 13 found that the measured settle time
(mean 326ms/section) matches the 300ms constant almost exactly and the stable time
(mean 193ms) lies close to a single 150ms poll interval — with CPU work (even
generously computed via `TaskDuration`) explaining at most ~24% of the time. Can
`mutationObserverDebounceMs` be lowered safely without risking file loss?

**Pre-research (2026-08-02, source reading, no live run):**
`sectionContentPollIntervalMs` itself has already been changed in exactly this
direction once — 400→150ms on 2026-07-21 (`crawl.go` line 965ff.), explicitly as a
*sampling-rate reduction, not a patience cut* (the overall budget
`sectionContentMaxPolls` was raised at the same time so total wait time stayed the
same). Measured live: 322/322 files at 150ms, no regression. But the same comment
carries an explicit warning still unresolved today: *"1 of 3 runs at the OLD 400ms
setting silently lost 15 files (...). That intermittent loss is NOT proven fixed by
this change; three clean runs are not enough to prove absence."*
`mutationObserverDebounceMs` itself, by contrast, was never tested in this
direction — the only documented live test (2026-07-16, `navigation.go` line 89ff.)
validated 300ms as correct against the old fixed 1100ms wait, but never tried a
lower value.

**Prediction:** lowering `mutationObserverDebounceMs` to 150ms (the same sampling
pattern as the already-proven poll-interval reduction, leaving
`mutationObserverHardCapMs` unchanged so total patience for slow sections stays the
same) loses no files against the 345-file ground truth over repeated runs, and
saves an average of ~150ms/section (≈46% of the current mean 326ms settle time,
≈29% of settle+stable).

**Counts as failed at:** any file/byte deviation against the ground truth over
**at least 2–3 repeated** runs (per our own history above, a single clean run is
*not* sufficient evidence of losslessness). Also, a run that finds all files but
where the mean saving stays under ~50ms/section refutes the claimed mechanism (then
300ms would not be the binding limit, the MutationObserver would really take longer
than the constant allows, and the mean 326ms would be coincidence, not evidence).

**Cost:** higher than any Question-13-and-earlier experiment — this is the
campaign's first question that actually changes scraper behaviour rather than only
measuring. Per task policy it has to sit behind an env flag, off by default
(`docs/RESUME.md`/scheduled-task rules: "Anything touching discovery goes behind an
env flag"). Needs `scripts/compare-visit-runs.ps1` and several live runs against the
real account — exactly the Wicket AJAX race risk this campaign has already been hit
by twice for real (`docs/sync-speed-campaign.md`,
`sectionContentRequiredStableReads`'s own history above).

**Implementation, deviating from the cost estimate:** `OPAL_DEBOUNCE_MS_OVERRIDE`
(new, in `navigation.go`'s `contentSettleWaitBudget`, off by default — when set it
replaces both the serial and the concurrency>1 debounce value;
`mutationObserverHardCapMs` stays unchanged in every case, as predicted).
`scripts/compare-visit-runs.ps1` was not needed — a new probe test
(`debounceoverride_probe_test.go`) compares file-URL sets directly in Go, without
the detour through a real `sync`/`list` run and its visit log. Four runs against the
small course (baseline×2, 150ms×2 — deliberately not just baseline-vs-override, see
above), `course_concurrency=1` (the default).

**Result (2026-08-02, `opal-downloader-sync-speed`, this cycle, live run,
`tmp/debounce-override-probe.txt`): prediction confirmed, on this course, at this
repeat count.**

| Run | Files | settle+stable |
|---|---:|---:|
| baseline-1 | 38 | 3094ms |
| baseline-2 | 38 | 3135ms |
| override-1 (150ms) | 38 | 2205ms |
| override-2 (150ms) | 38 | 2180ms |

All three comparisons (baseline self-consistency, override self-consistency,
baseline vs. override) are **exactly identical** — the same 38 file URLs in all 4
runs, no deviation. An already-known pagination gap independent of the debounce (one
section stops at 17 of what are actually more rows, the `show all` click having no
effect) occurred identically in all 4 runs — no new symptom, no degradation from the
change.

Time saving: mean settle+stable drops from 3114ms to 2192ms, **29.6%** — extremely
close to the estimate computed in advance (≈29% of settle+stable, ≈150ms/section:
measured 922ms/6 sections = 154ms/section). That is the signature rule 2 demands: the
saving hits the arithmetic prediction almost exactly, which confirms that the 300ms
constant really was the binding limit, not a coincidence in the mean-326ms
observation from Question 13.

**Why this still does not mean "Question 14 solved, change the default" (rule 2,
taking the prediction's scope seriously):** the prediction was explicitly limited to
correctness *without contention* — small course, 6 sections, `course_concurrency=1`.
Every historical data loss in this campaign occurred under contention (large course,
many paginated sections, partly `course_concurrency>1`) — exactly the case this run
does not test. Four identical runs on one course, on one day, are also literally the
order of magnitude our own 2026-07-21 comment on `sectionContentPollIntervalMs`
already marked as insufficient ("three clean runs are not enough to prove absence").
Question 14 is therefore **answered with a mechanism for the case tested (small,
serial)**, but not closed in general — new, sharper question: Question 15 above
(large course, possibly concurrency).

---

## Previous experiment (Question 13, closed 2026-08-02)

**Question:** (13, new from Question 12) — where does the remaining, unexplained
settle time actually sit (CPU/layout/paint), when three independent candidates
(network transfer 24–31%, section file count linear ~16–21% variance, section file
count quadratic ~29% variance) all explain only minorities? This is the "next step
needs real browser profiling" already predicted in Question 7 and Question 10 — now
no longer optional, because it is the only untested class of explanation left.

**Prediction (written before the run):** `Performance.enable` +
`Performance.getMetrics()` (lighter than a full `Tracing.start`, one synchronous CDP
call instead of stream evaluation), measured on the small course's slowest section
already known from Question 11 ("Vorlesung", 44 candidates), shows
LayoutDuration+RecalcStyleDuration+ScriptDuration as a real but not dominant share of
settle+stable — estimated 20–40%.

**Counts as failed / satisfied at:** >50% = CPU dominant (prediction wrong, but
informative); ~20–40% = prediction confirmed, the debounce constant becomes the prime
suspect for the rest; <10% = strong case for "the time is the constant, not work".

**Result (2026-08-02, `opal-downloader-sync-speed`, this cycle, live run against the
small course, 6 sections, `tmp/cdp-performance-metrics-probe.txt`): prediction
literally neither confirmed nor clearly refuted (11.4% aggregate, 14.5% for the
slowest section — between the two thresholds), but an additional, unplanned analysis
of the same data delivers a sharper, convergent explanation.**

| Section | Candidates | settle+stable | Script+Layout+RecalcStyle | % of it | TaskDuration | % of it |
|---|---:|---:|---:|---:|---:|---:|
| Algorithmen (root) | 12 | 482ms | 37.5ms | 7.8% | 83.6ms | 17.3% |
| Übungseinschreibung | 14 | 501ms | 90.3ms | 18.0% | 199.8ms | 39.9% |
| Materialien | 18 | 505ms | 63.8ms | 12.6% | 103.0ms | 20.4% |
| Probeklausur | 17 | 504ms | 35.1ms | 7.0% | 59.3ms | 11.8% |
| Übungsblätter | 27 | 532ms | 43.8ms | 8.2% | 88.5ms | 16.6% |
| Vorlesung | 44 | 588ms | 85.5ms | 14.5% | 226.8ms | 38.6% |
| **Total/aggregate** | | **3112ms** | **356.1ms** | **11.4%** | **761.0ms** | **24.4%** |

The metric named in advance (Script+Layout+RecalcStyle) comes in at 11.4% — just
above the <10% threshold, clearly below the predicted 20–40% band. `TaskDuration`
(Chrome's own, more comprehensive main-thread busy time — a superset containing
Script/Layout/RecalcStyle **and** everything else the browser counts as a "task" on
the main thread: GC, parsing, paint preparation, compositing registration) comes in
at 24.4% — inside the predicted band, but as an **upper bound**, not as confirmation
of the specific Candidate C hypothesis (quadratic `tr` mutation → layout/style
recalc): even of this most generous figure, Script+Layout+RecalcStyle make up less
than half (356 of 761ms) — the rest of `TaskDuration` is unnamed main-thread work,
not a confirmed mechanism.

**Methodological caveat, aimed at our own measurement:** the CDP metric window spans
`visitSection` as a whole (navigation + settle + stable), while the `settleStable`
denominator window covers only settle+stable. So part of the measured CPU time
presumably falls in the navigation/initial-parse phase, not in the settle+stable
phase itself — the 11.4%/24.4% reported here are therefore more likely an
**overestimate** of CPU's share of settle+stable than an underestimate. That sharpens
the finding rather than qualifying it.

**What that means for rule 2 — mechanism instead of just a number:** even with this
caveat in favour of "more CPU", browser work (in any form CDP can see) remains a
minority. Two constants already in the code explain where the majority goes instead:
`mutationObserverDebounceMs = 300` (`navigation.go` line 99) sits almost exactly at
this run's measured mean settle time (326ms, the `section timing` log line of the test
run) — 26ms difference, 8.7% deviation. `sectionContentPollIntervalMs = 150`
(`crawl.go` line 982) is in the same order of magnitude as the measured mean stable
time (193ms, well explained by a single poll cycle plus some overhead when
`initialStableReads` is low). That is the signature rule 2 demands: **the
settle+stable time does not look like variable render work but like two fixed wait
constants that happen to make up almost all of the time** — CPU work is real
(11–24%), but it sits *inside* those windows, it does not drive them.

**Candidate B/C (Question 7, Question 11) hereby filed as a secondary explanation
rather than still open as "the missing majority":** four independently tested
explanations (network 24–31%, file count linear 16–21%, file count quadratic 29%, now
CPU 11–24%) are all minorities, but two known wait constants fixed in the code fit the
remaining time almost exactly in numeric terms. That is a change of explanation, not
another eliminated candidate: the question is no longer "what is the browser building
there" but "are 300ms/150ms the right safety margin, or more than needed" — Question
14 above.

**Checked in advance (2026-08-02, source reading, no live run, no server contact):**
Playwright Go's own `Tracing` API (`playwright-go@v0.6100.0/tracing.go`) produces only
Playwright's own trace viewer (screenshots/snapshots/network/actions) — no CPU/layout
profile in the Chrome DevTools sense. But `BrowserContext.NewCDPSession(page)` already
exists (`browser_context.go` line 88, `CDPSession.Send(method string, params
map[string]any) (any, error)`, `generated-interfaces.go` line 630) — the same raw CDP
attachment point through which Question 3 already found `Fetch.enable`, only called by
us this time instead of merely read.

**Method refined (2026-08-02, before the run):** a full `Tracing.start` (Chrome trace
JSON, stream events via `Tracing.dataCollected`/`tracingComplete`, no ready-made parser
in the project) is more tool than the question needs. `Performance.enable` +
`Performance.getMetrics()` is a lighter member of the same CDP domain family: a single
synchronous call without a stream, returning cumulative second counters
(`ScriptDuration`, `LayoutDuration`, `RecalcStyleDuration`, `TaskDuration` since
`enable`). A before/after diff around one section navigation answers the same
qualitative question ("does the time sit in script, layout, style recalc, or in none of
them") without offline trace analysis.

**Prediction:** for the small course's slowest section already identified in Question
11 ("Vorlesung", Algorithmen und Datenstrukturen, 44 candidates, 533 mutations — no
third crawl of the large course needed in one day), the sum of LayoutDuration +
RecalcStyleDuration + ScriptDuration makes up a real but not dominant share of the
measured settle+stable time — estimated 20–40%, in the same order of magnitude as the
already measured candidates network (24–31%, Question 7) and section file count
(16–29%, Question 10/12), not absorbing the missing 70%+ on its own. Mechanism: the
quadratic `tr` mutation found in Question 11 should, if it triggers real style
recalcs/layout passes, be visible in LayoutDuration/RecalcStyleDuration — but browser
style recalc for a few dozen attribute changes on `tr` elements is typically in the
microsecond to low-millisecond range, not hundreds of ms.

**Failure criterion (qualitative, not just a threshold — lesson from Question 12's
too-lax criterion):**
- **>50%** of settle+stable time in Layout+RecalcStyle+Script: prediction refuted, but
  in the good sense — CPU work is the previously overlooked dominant driver, Question 13
  closes with a mechanism, new question: which of the three metrics specifically, and
  why.
- **~20–40%** (prediction confirmed): a real but further minority explanation alongside
  network and file count — then after four tested candidates (network, file count
  linear, file count quadratic, now CPU) none is dominant, and the remaining majority of
  the time is presumably **pure waiting with no measurable browser work** — the 300ms
  debounce in `waitForInteractiveLinks` itself then becomes the suspect, no longer what
  it is waiting for.
- **<10%** (near zero): strongest case for "the time is the debounce constant, not
  work" — the next question would then be whether the 300ms constant is too
  conservative, no longer what runs during that time.

**Cost:** lower than originally assumed — no trace parsing, only one CDP session per
page plus two `Performance.getMetrics()` calls per section. Run against the small course
already used for Question 11 (6 sections, no extra server load beyond what a section BFS
walk costs anyway), no diff against ground truth needed (no sync behaviour changed).

---

## Previous experiment (Question 12, closed 2026-08-02)

**Question:** (12, new from Question 11) — does the settle+stable **wait time** (not
just the mutation count) scale better with the squared candidate count than with the
linear one (Question 10: r=0.40 linear) — and does that explain the weak linear finding
so far as a quadratic relationship that a linear model underestimates?

**Prediction:** another run of the existing Question 10 probe
(`network_timing_probe_test.go`, `sectionProbe` hook, unchanged apart from an additional
Pearson r computed against `candidates²`) against the same large course
(Softwaretechnologie, 164 sections) shows a markedly higher Pearson r between candidate
count² and settle+stable time than the already measured r=0.40 for the linear candidate
count.

**Counts as failed at:** if r for candidate count² is not noticeably above r=0.40 (e.g.
stays below ~0.5), the quadratic mutation relationship found in Question 11 does not
explain the per-section wait time — then the main driver of the wait time remains open,
and the next step is the real browser profiling already predicted in Question 7/10
(CPU/layout/paint), not more counting of mutations.

**Cost:** a one-line addition to the existing Question 10 probe (Pearson r additionally
against `candidates²` instead of just `candidates`), one live run against the same large
course as Question 10 — no second crawl of that course today yet, so no reason to avoid
it (`docs/server-load.md`: one crawl a day is negligible). No diff against ground truth
needed, since nothing about sync behaviour changes.

**Result (2026-08-02, `opal-downloader-sync-speed`, this cycle, directly following
Question 11 with the same still-valid session): prediction not confirmed — only a small
improvement, not "markedly higher".**

Live run against the same course as Question 10 (Softwaretechnologie, 164 sections,
`tmp/settle-timing-network-trace.txt`): linear r=0.46 (slightly above the archived 0.40
from Question 10 — run-to-run scatter of a real server, not a regression), quadratic
(candidate count²) r=0.54. r² (explained variance) therefore rises from 21% to 29% — a
real but small difference, not one that explains Question 10's "weak correlation".

**Honest assessment of our own failure criterion:** taken literally ("stays below ~0.5")
the criterion is narrowly unmet at r=0.54 — but the criterion was formulated too
loosely. The spirit of the prediction was "explains the weak linear correlation", not
"is algebraically above 0.5". An improvement of 8 percentage points of explained
variance is not that. Lesson for future criteria in this file: a threshold alone is not
enough, the criterion also needs a qualitative formulation ("doubles the explained
variance" rather than just "above x").

**What that means for the overall picture:** three independently tested candidates for
the settle time now all land in the same "real, but a minority" range: network transfer
24–31% (Question 7), section file count linear r=0.40–0.46 / ~16–21% variance (Question
10, confirmed here), section file count quadratic r=0.54 / ~29% variance (here). None of
them is dominant. The next step already predicted in Question 7 and Question 10 is
therefore no longer optional but the only class left untested: real browser profiling
(CPU/layout/paint) of a single section navigation, to see where the remaining 70+% of
the time actually sits.

---

## Previous experiment (Question 11, closed 2026-08-02)

**Question:** (11, new from Question 10) — what actually mutates in the DOM during the
~338ms settle window, when neither network transfer (24%) nor section file count (r=0.40,
~16% variance) explains the majority?

**Refined (2026-08-01, after reading the source instead of guessing):** the first draft
of this question named "CPU/layout profiling" as the necessary next tool, untested and
expensive. `contentSettleWaitScript` (`internal/scraper/navigation.go` line ~452) shows
the mechanism directly: a `MutationObserver` on the content root with `{childList,
subtree, attributes, characterData}` — **every** mutation, however small, resets the
debounce timer. So settle time does not measure "how long until the content is finished"
but "how long until nothing at all happens anywhere in the root element". That is
directly observable without CPU profiling: record the mutation records themselves (target
element, type, `attributeName`) rather than just their frequency.

**Prediction:** a live recording of the mutation records during real section visits shows
that the mutations are concentrated on a few, tightly bounded elements (e.g. a recurring
widget, an attribute toggle, a live display) — not spread broadly over tree/file table,
which per Questions 1/9 are already finished server HTML on initial load and would have
no reason to mutate afterwards.

**Counts as failed at:** if the mutations are spread broadly over many different,
non-recurring elements (no clearly delimitable originator), the hypothesis of a narrow
Candidate C widget is refuted — then only a diffuse explanation remains, and only then
does real CPU/layout profiling become necessary, not before.

**Cost:** test-side instrumentation (a copy of `contentSettleWaitScript` with mutation
logging instead of just debouncing), a live run against a few sections of the small course
(Algorithmen, 6 sections — deliberately not the large one again, a third live crawl of the
same course in one day would be unnecessary server load, docs/server-load.md), no
production code change needed, no new tooling.

**Result (2026-08-02, `opal-downloader-sync-speed`, this cycle): prediction partly
confirmed, but with a shift that does not quite satisfy rule 2 — Candidate C refuted in its
narrow form, superseded by a sharper Question 12.**

`TestMutationConcentrationAcrossSections`
(`internal/scraper/mutationmarker_probe_test.go`) extends the existing
`mutationObserverInitScript` probe (previously only root + one section, only the last 8
records read by hand) to a complete BFS walk of all 6 sections of the small course, with
all mutations aggregated by target element. Live run against the real account,
`tmp/mutation-concentration-probe.txt`:

| Section | Candidates | Mutations | Mutations/candidate |
|---|---:|---:|---:|
| Algorithmen u. Datenstrukturen (root) | 12 | 43 | 3.58 |
| Übungseinschreibung | 14 | 52 | 3.71 |
| Probeklausur | 17 | 64 | 3.76 |
| Materialien | 18 | 84 | 4.67 |
| Übungsblätter | 27 | 164 | 6.07 |
| Vorlesung | 44 | 533 | 12.11 |

**Concentration confirmed:** across all 6 sections (940 mutations, 36 distinct element
keys) the top 3 keys contribute 79.8% — that does not meet the failure criterion (no
diffuse distribution), the "concentrated on a few" prediction holds.

**But the dominant cause contradicts the Candidate C premise:** by far the largest key is
an unnamed `tr` (70.2% of all mutations, attribute mutations directly on file table rows
without id/class) — not a "narrowly bounded widget outside tree/table", as Candidate C
explicitly required ("not the tree or the table itself"). The two candidates guessed by
hand on 2026-07-30 (`#veil`, the Wicket AJAX overlay; `#MathJax_Message`) are real, but at
0.9% and 1.3% a small minority, not the explanation — that earlier guess came from reading
only 8 tail records of a single (and unusually slow) section, and is refuted here by the
complete aggregation over 940 records.

**New, sharper finding (not part of the original prediction):** the mutations/candidate
ratio is not constant but grows from 3.58 (12 candidates) to 12.11 (44 candidates) — a
3.4x rise in the rate for only 3.7x more candidates. Regression over the 6 points:
candidate count vs. mutations linear r=0.976, candidate count² vs. mutations r=0.997,
log-log exponent 1.96 (r=0.993) — the data fit a quadratic relationship considerably
better than a linear one. That is the concrete, testable signature rule 2 demands:
something touches file table rows with a total effort that grows more like row count²
than like row count — e.g. a pairwise row comparison (duplicate/sort/highlight logic),
not a plain per-row initialisation. Which concrete attribute changes on `tr` (only the tag
was aggregated, not `attr`/`attrVal`) has not been investigated yet.

**Why Question 11 is nevertheless not simply closed:** Candidate C is refuted in its
original, narrow form ("widget outside tree/table"), with a mechanism (the aggregation
shows clearly: it is the table itself). But that is not yet a complete explanation for the
settle *time* — only for the mutation *count*. Whether the quadratic mutation finding also
explains the actual wait time (the real target quantity, not just a proxy for it) is
untested, and is exactly the new Question 12 above.

---

## Previous experiment (Question 10, closed 2026-08-01)

**Question:** (10, new from Question 9) — does settle+stable per section scale with the
file count *in the section currently being visited*, rather than with the course's total
section count?

**Prediction:** within the same large course (Softwaretechnologie, 164 sections), the
per-section settle+stable time correlates with that one section's file count — sections
with many files take noticeably longer than empty/file-poor ones.

**Counts as failed at:** if settle+stable per section stays flat within the same course
even across very different file counts (e.g. 0 vs. 20+ files), file count is not the
explanatory variable — then the remaining time is a fixed overhead per section page
(navigation, Wicket bookkeeping, layout/parsing of an essentially constant-sized page),
and per the model (Question 7) that needs real browser profiling, no more source reading
or network tracing.

**Cost:** extending the existing probe (`network_timing_probe_test.go`) with a file count
per section alongside `sectionTiming`, one live run against the real account (only the
large course already crawled), no diff against ground truth needed.

**Result (2026-08-01, `opal-downloader-sync-speed`, this cycle): neither confirmed nor
cleanly refuted — a weak, non-dominant relationship.** `OpalScraper.sectionProbe` (new
hook, nil in production, `internal/scraper/scraper.go`/`crawl.go`) measures settle+stable
per section against the candidate count (`candidateStabilityPoll` hit count, a proxy for
file count). Live run, large course only (164 sections, candidate count 21–72): **Pearson
r = 0.40** between candidate count and settle+stable time per section. That is real (not
0, so not "flat" in the sense of the failure criterion), but weak — r²≈16% of variance
explained, far from "noticeably longer with many files" as the main explanation.

Together with Question 7 (network explains 24% of this run), the bulk of the
settle+stable time remains unexplained by both candidates tested so far (network bytes,
section file count). That matches the consequence already named in the model before this
cycle: the remaining time looks like a largely **fixed overhead per section page**, not
like something that scales with content volume (tree or file table) — whether measured
via course size (Question 9) or section file count (here). Pure source reading and
network tracing are therefore exhausted as tools for this question; the next step needs
real browser profiling (CPU/layout/paint), as Question 7 had already predicted.

---

## Previous experiment (Question 9, closed 2026-08-01, pure source reading)

**Question:** (9) — does `MenuTreeRenderer` serialise the whole course tree on every
section page, or only the visible/expanded subtree?

**Prediction:** the source shows conditional recursion (e.g. an `if (node.isOpen())`-style
check before the recursive call for child nodes) that explains why the measured response
size does not scale with the total section count.

**Counts as failed at:** if the code recurses unconditionally over all child nodes (no
open/closed gate visible), (a) is refuted and only (b) (caching) or a third, not yet named
explanation remains — then back to pure measurement: compare response bytes of two
different section pages *within the same large course* (do they vary with the section
clicked, or are they flat there too?).

**Cost:** source reading (`gh search code --repo OpenOLAT/OpenOLAT`), no build, no live run
needed.

**Result (2026-08-01, `opal-downloader-sync-speed`, this cycle): prediction confirmed, with
evidence — candidate (a) proven.** `isRenderChildren()` (`MenuTreeRenderer.java`, from line
660) returns `true` only if the node is in `openNodeIds` or lies on the selection path
(`curSel == curRoot`); otherwise `false`, and `renderLevel()` (line 232) then never even
calls `renderChildren(...)` — the recursive descent ends structurally at every node that is
neither open nor selected. That is the sharp explanation rule 2 demands: the tree fragment
share of the response is limited to open nodes + selection path, not to the total section
count — exactly the pattern that predicts the 1.4x/27.3x discrepancy from the previous
experiment. Candidate A (Question 7) is therefore not merely refuted but closed with a
mechanism. New question (rule 3): Question 10 above.

---

## Previous experiment (Question 7, closed 2026-08-01)

**Question:** (7) — does the network/transfer time of a large server HTML response explain
the 336ms settle wait, rather than client JS?

**Prediction:** response size (Content-Length) and time-to-last-byte of a course-node page
correlate with course size (number of sections in the tree), and "network done" coincides
in time with "candidate count stable".

**Counts as failed at:** if the response bodies are small (a few KB, transfer < 50ms) while
settle/stability still needs 300ms+, transfer is not the explanation — then what runs during
that time stays open (back to Candidate B/C), and that needs real browser profiling, no more
source reading.

**Cost:** one live run against the real account (recording network timing per section), no
build risk — purely read-only instrumentation, no diff against the ground-truth sync needed,
because nothing about sync behaviour changes.

**Result (2026-08-01, `opal-downloader-sync-speed`, this cycle): live run performed,
prediction refuted — Candidate A (tree size drives response size) is dead, but with a new,
narrower question rather than closed (rule 2 not yet satisfied, see below).**

Result (`tmp/settle-timing-network-trace.txt`):

| | Algorithmen u. Datenstrukturen (6 sections) | Softwaretechnologie (164 sections) |
|---|---|---|
| avg. document response | 5604 bytes / 79ms | 7789 bytes / 65ms |
| settle+stable per section | 511ms | 525ms |
| network share of settle+stable | 31% | 25% |

Byte ratio (larger/smaller) **1.4x** at a section ratio of **27.3x**. The prediction
required response size to grow with course size (rationale: `MenuTreeRenderer` ships the
complete `o_tree` on every section page). That did not happen — the bytes stay practically
flat across a 27-fold size difference, and the transfer duration even drops slightly.
Matches the failure criterion: small bodies (5.6–7.8 KB), transfer in the 65–130ms/section
range (2 document requests per section), while settle+stable stays at 511–525ms/section —
network explains at most 25–31%, never the majority.

Side finding, not part of the prediction but informative: settle+stable per section is
almost identical between the two courses (511 vs. 525ms) — matching the already known
aggregate from the table above (338+172=510ms/section). So the timing itself does not scale
with course size either way; only the prediction of *why* it does not (bytes do not scale
either) is new.

**Why Candidate A nevertheless stays open (rule 2):** only the prediction "response grows
with course size" is refuted, not the mechanism behind it. There are two unconfirmed
explanations for why `MenuTreeRenderer` sends barely more bytes despite 27x more sections:
- **(a)** the renderer does not serialise the whole tree but only the visible/expanded
  subtree (depth/open nodes instead of total section count) — then course size would be the
  wrong independent variable, and tree size as such would not be refuted.
- **(b)** the tree is cached/reused somewhere server-side and only a diff or a reference is
  sent.
Neither is tested. Also untested: the 25–31% network share is real, but a minority — what
fills the remaining 69–75%, if not client JS (Question 1, answered) and not network transfer
(now largely refuted)? That was Candidate B/C before this run and remains so.

The probe (`internal/scraper/network_timing_probe_test.go`, `OPAL_SETTLE_TIMING_TRACE=1`)
crawls the smallest and the largest content course in the account one after the other and,
per course, puts bytes/duration of the section pages' document responses
(`Request.Sizes()`/`Request.Timing()`) next to `sectionTiming` (settle+stable; this document
already knew the mechanism, see above).

Before the first live run, a look at the already archived trace from 2026-07-27
(`tmp/network-trace-Softwaretechnologie (SoSe 26).txt`, from a different probe): 324 main
frame document responses, **0 bytes content-length total**. A Java/Wicket servlet that builds
a page dynamically does not buffer it completely in order to compute `Content-Length`
beforehand — it sends chunked transfer encoding, which omits the header entirely. The first
version of this probe would therefore have reported "0 bytes" for every section page in both
courses and wrongly considered Candidate A refuted — not because the response was small, but
because the instrument was blind. Switched to `Request.Sizes()` (real transferred bytes, not
the header) — that needs a round trip into the browser process, which is why it is
deliberately *not* called in the `OnResponse`/`OnRequestFinished` handler (the same deadlock
`network_trace_probe_test.go` already had live for 55 minutes on 2026-07-27), but afterwards,
when the dispatch loop is free again.

**Live run blocked:** the saved session had expired (`ensureSession: timed out after
300000ms waiting for the OPAL course list after login`). The rationale given at the time —
"login needs 2FA in an open browser window, cannot be bridged unattended" — **was wrong** and
is left standing here because it cost a whole cycle. TU-Fast in the dedicated profile handles
login and 2FA itself; an expired session is not a blocker. Re-measured 2026-08-01: `list`
with expired state → auto-login → 8 courses in 3.7s, no click. So the 300s timeout on
2026-07-31 was a real failure with an unknown cause, not a structural limit — next time it
happens, investigate the error message, do not call the maintainer. Browser/profile were
closed cleanly (`sc.Close()` ran via the regular `defer`, `rate ceiling: 2 navigation(s), 0
delayed` confirms it), nothing was left hanging.

Question 7 stays open — no new live measurement this time. But the probe now runs, and the
byte measurement path is already verified against a real bug; the next cycle with a valid
session can measure directly instead of building first.

---

## Reports

Every 5 cycles, each ending with a recommendation: keep going or stop, and why. The stop
decision is the maintainer's, not a counter's — no cap on the campaign, the kill criterion
sits per experiment (decision of 2026-07-31; counter-arguments noted in the same session:
every abort condition this repo ever had became the thing the work stopped at).

### 2026-08-04 (autopilot): five cycles, Questions 17-21 — keep going, correctness thread deepens

Cycles since the last report: Questions 17, 18, 19, 20, 21 (first cycle). All five
are correctness work on the contention-loss thread the last report opened, not
speed levers — consistent with that report's own call to put correctness ahead
of speed.

**What changed since the last report.** Question 17 closed without a new live
run: re-reading the archived log answered it in minutes (`warnShowAllTruncated`
correlated 4/4 with the Vorlesung-node loss), re-classifying `course_concurrency>1`
from "loses files" to "an already-known expansion bug fires more often under
load" — the setting itself stayed untouched (default 1, no clamp). Question 18
closed the other way: the "permanently truncated section" alarm that looked like
the campaign's most serious open bug turned out to be a broken detector counting
table rows instead of file rows on an enrolment table with no files in it at
all — fixed, re-verified live, nothing was ever missing there.

Questions 19-21 then chased the real mechanism behind the Vorlesung loss
directly. Question 19: the click's Wicket `AJAX_CALL_DONE` signal does not
arrive *late* in failing runs, it does not arrive *at all* within the 4000ms
budget — refuting the "early read" theory. Question 20: raising that budget to
15000ms produced 3 clean runs, but its own failure criterion correctly called
that inconclusive rather than proof, given the condition's own ~33-50%
historical failure rate. Question 21 (first cycle): instrumenting the actual
elapsed wait time found something the previous three questions could not see —
failing runs resolve in ~200ms, not anywhere near either timeout budget. That
is only possible if the wait errors out fast rather than genuinely timing out,
which means the error text (previously discarded entirely) had to be captured
to go further. Fixed the same cycle; Question 22 is instrumented and predicts
`context-destroyed`, not yet run.

**The correction that matters more than any single answer, again.** Question 21's
own doc comment, written the same day as the instrumentation it describes,
assumed a `expansionSignalled=false` reading meant "consumed the full timeout
budget" — nobody had actually checked, because nothing had ever measured it
before. The first live run refuted that assumption within hours of it being
written. Same shape as the Question 16→17→18 correction chain the last report
flagged: an assumption stated confidently in passing turned out to be
checkable, and checking it changed the diagnosis.

**Still open:** Question 22 (what the wait error actually says — instrumented,
not yet run, real-account load is the limiter today, not idea supply).
Untouched since the last report: Question 8 (cache-off vs. pause/resume,
locally testable without an account), Question 5 (is 30s tied to discovery at
all), Question 6 (1 in 12 sections unstable, possibly the same root as
Question 17). No default has been shipped or reverted this round — the 150ms
debounce from the last report stands unchanged, and nothing here is behind an
env flag ready to promote yet.

**Recommendation: keep going.** Five cycles running, and the campaign has now
found and explained two real bugs from a starting point of "some files go
missing under contention sometimes" — a broken detector (Question 18, fixed)
and a specific, narrowing mechanism for a real intermittent loss (Questions
19-21, not yet fixed but for the first time pointed at a concrete Playwright-
level cause instead of a vague "Wicket timing" guess). That is squarely the
correctness-before-speed mandate the last report's decision set, not a new
direction. The binding constraint is server load, not questions to ask —
Question 22 needs only a couple more real-account runs on a day (or later
cycle) with room left in the budget this file already tracks
(`docs/server-load.md`).

### 2026-08-03 (decision round): five cycles, Questions 12-16 — keep going, and the first shipped win

Cycles since the last report: Questions 12, 13, 14, 15, 16. Report due on the
five-cycle cadence, and the keep-or-stop call is the maintainer's.

**What changed since the last report.** The settle time was traced to its cause
and then cut. Question 12 killed the quadratic-mutation explanation as a driver
of wait time (r² rose only 21%→29%). Question 13 measured browser work at
11-24% of settle+stable and named the real mechanism: the time is not work, it
is two fixed constants — measured mean settle 326ms against a 300ms constant.
Questions 14 and 15 tested that directly and held it, byte-identical across 8
live runs on two courses 27x apart in size, saving 29.6% and 28.7%. Question 16
tried to extend that to `course_concurrency>1` and could not, because the
unchanged baseline turned out to differ from itself.

**Shipped this round (maintainer's decision, 2026-08-03):**
`mutationObserverDebounceMs` 300 → 150 is now the default, not an env flag. That
is the campaign's first user-visible improvement since it reopened on 2026-07-31
— roughly 29% off the dominant component of a sync.

**The correction that matters more than the win.** Question 16's exclusion of
`course_concurrency>1` was overturned in this round, on the maintainer's
objection that a consistent 6-file loss demands an explanation before it is
converted into a rule. It did, and the explanation was already sitting in the
archived run log: `warnShowAllTruncated` had fired on exactly the two runs that
lost files, 4/4 correlation, naming the branch. See Question 17 above. This is
the failure Rule 2 was written to prevent, committed by the campaign against
itself — a measured effect promoted straight to a verdict without a mechanism —
and it survived a whole cycle before an outside question caught it.

That find then produced Question 18, which is the more serious one: a section
that is truncated in **every archived run at every concurrency setting**,
including the shipping default. It is invisible to every gate this campaign has,
because all of them are diffs and a permanently truncated section is identical to
itself. "All 8 runs agreed" was accepted as proof of losslessness in Questions 14
and 15 while this was happening in all 8.

**Still open:** Question 18 (permanent truncation — a correctness bug, now ahead
of everything else), Question 17's A-vs-B tail, Question 8 (cache-off vs.
pause/resume, locally testable without an account, never touched), Question 5
(is 30s tied to discovery at all), Question 6 (1 in 12 sections unstable).

**Recommendation: keep going — and the maintainer chose to keep going.** The
reason is not the 29%: it is that this round produced the first correctness bug
the campaign has found rather than caused, and one of them (Question 18) is
losing files today at the default setting. Speed work has also stopped being the
best use of the loop — the remaining discovery levers are the 4000ms hard cap and
the 150ms poll interval, both smaller than the one just taken, and even a perfect
result there leaves ~150s against a 30s target. Next cycles go to Question 18
first, correctness before speed, consistent with this repo's stated order.

### 2026-08-02 (opal-downloader-sync-speed): first report of this task, Question 11 closed with a shift

The first report of **this** scheduled task (`opal-downloader-sync-speed`) — earlier cycles
(Question 3, 7-build, 7-live, 9, 10, 11) ran without one. Overdue by more than 5 cycles.

**Known since the last (non-existent) report, i.e. since the campaign started 2026-07-31:**
`ctx.Route` costs ~30% (CDP pause/resume, unavoidable, Question 3 closed). The tree fragment
share of a section page is limited to open nodes + selection path, not to course size
(`MenuTreeRenderer.isRenderChildren`, Question 9 closed). Network transfer explains only
24–31% of the settle time (Question 7). Section file count explains it only weakly in linear
terms (r=0.40, Question 10). Today (Question 11): DOM mutations during the settle time are
concentrated (top 3 elements = 79.8%), but the cause is the file table itself (`tr`, 70%),
not an external widget — and the mutation count grows with the square of the row count
(exponent ≈1.96, r=0.997), not linearly.

**What that means for the state of the model:** four of five tested explanations for the
settle time are now individually either closed (Questions 3, 9) or quantified and dismissed
as secondary (Question 7: 24–31%; Question 10: r=0.40). Question 11 delivers, for the first
time, an explanation that looks strong enough to be dominant (quadratic growth instead of a
weak linear correlation) — but so far it is shown only for the mutation *count*, not for the
actual wait time. That is Question 12, already set up as the next experiment.

**Still open:** Question 12 (does the wait time itself scale quadratically with row count?),
Question 8 (cache-off vs. pause/resume share of the 30% from Question 3, reproducible locally
without an account, never touched), Question 5 (is "30s" even tied to discovery, or do
background runs/partial results solve the real goal "feels like one click" without faster
discovery?), Question 6 (1 in 12 sections stays unstable across runs, unexplained why).

**Recommendation: keep going.** For the first time since the campaign began there is an
explanation with a clear quantitative signature (quadratic, not just "not network, not file
count linear") instead of a list of eliminated candidates. Question 12 decides it in a single
cheap run (no new instrumentation, only an additional correlation on code that already
exists), whether that explains the wait time itself or not — a sharp result either way, not
another guess.

### 2026-07-31 (autopilot): Question 1 read, not measured

**Sources (primary, `gh search code --repo OpenOLAT/OpenOLAT`):**
- `src/main/java/org/olat/core/gui/components/tree/MenuTreeRenderer.java` — builds
  `.o_tree`/`.o_tree_l{n}` as Java `StringBuilder` HTML, synchronously, server-side. No JS
  templating.
- `src/main/java/org/olat/core/gui/components/table/TableRenderer.java` + the FlexiTable
  renderers (`.o_table_wrapper`, `.o_table_flexi`) — the same pattern: a Java renderer
  produces complete table HTML including paging links.
- The old `Table` class (`org.olat.core.gui.components.table.Table`, probably NOT the one
  used for course folders — that is FlexiTable) knows a URL parameter
  `COMMAND_PAGEACTION_SHOWALL="a"`. Not verified live whether the course-folder file list uses
  that path at all — the existing Wicket AJAX click in `wicket.go` is already measured live (0
  errors, byte-identical parity) and was **not** replaced by this.
- The REST API (`/repo/courses/{id}/elements/folder/{nodeId}/files`, `VFSWebservice`) exists
  in the source — but was already measured at the reverse proxy as 403
  (`docs/sync-speed-campaign.md` line 899), independently confirmed dead. WebDAV likewise
  already measured with a blank 200 (dead backend) — `docs/webdav-propfind-research.md`.
  Neither was retested, the source finding was only reconciled against the measurements
  already on hand.
- Secondary, for confirmation: OpenOLAT's own `.claude/openolat-frontend-knowledge.md` in the
  same repo says literally "No client-side framework (no React/Angular/Vue). All state lives
  on the server."

**Result: prediction (60% table-state class) refuted, but with an explanation that predicts
the absence (rule 2 satisfied) — no marker, because nothing is built client-side that would
need one.** Matches the already known "file rows are server-rendered in the initial response,
zero Wicket AJAX" from `wicket.go`. Question (1c, pager parameter) stays unclear, but changes
nothing about the existing, working Wicket signal approach. Question (1b, alternative view)
stays answered no — both known alternatives have already been independently measured as dead.

**New open question (rule 3):** Question 7 above — the source finding contradicts the live DOM
probe of 2026-07-31 that led to dropping the nav-walk lever. Not resolved, only named
precisely.

**Not executed in this cycle:** the next experiment defined above (measuring network timing
live) — that is a run against the real account, no longer reading, and belongs in an
`opal-downloader-sync-speed` cycle with its own reporting cadence rather than in this general
autopilot run.
