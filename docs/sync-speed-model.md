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

## Corollaries, learned the hard way

Moved here from `docs/BACKLOG.md` on 2026-08-12, where they were sitting under
"Standing work" — they are rules for this campaign, and belong with the rules.

**A byte-for-byte diff is not proof of losslessness (learned 2026-08-03).** It
only catches losses that *vary* between runs. A section truncated identically on
every run is identical to itself and to the ground truth, and passes every gate
this project has — which is exactly what Question 18 turned out to be, through
all 8 runs of Questions 14 and 15. Do not read "all runs agreed" as "no files
lost"; `warnShowAllTruncated` in the run log is currently the only signal that
sees this class, and nothing consumes it.

**Do not convert a measured effect into a rule without a mechanism (2026-08-03).**
Question 16 measured file loss at `course_concurrency>1` and went straight to
proposing the setting be clamped away. The maintainer refused the exclusion and
asked why the loss was consistently *six* files — which turned out to be
answerable from an archived log in minutes, and re-classified the whole thing
from "concurrency loses files" to "a known expansion bug fires more often under
load". Rule 2 applies to the campaign's own conclusions, not only to its
experiments.

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

### 36. Can the hybrid's phase 1 be seeded from `initial_data` instead of from a full browser tree walk? — OPEN, opened 2026-08-10 by Question 34's answer

**This is Question 34's consequence, and it is the largest single structural
change this campaign has ever had a reason to try.** `scrapeCoursesHybrid`
(`orchestrator.go`) runs the complete 207s browser crawl *first*, in every
mode including `mode=1`, and takes its section-URL set from
`s.VisitRecords()` — i.e. from the browser having already visited every
section. Its own comment states the reason: the browser is *"the only thing
that can enumerate the JS-rendered section tree"*. Question 34 refutes that
sentence. `mode=1`'s measured 254s vs 207s (i.e. slower) is entirely
explained by paying for the browser walk and then paying again for HTTP;
nothing about the HTTP half was the problem.

*Prediction, written 2026-08-10 before any implementation, per Rule 1.* Two
steps, and only the first is done in this cycle because only the first needs
no live account:

**Step A (offline, this cycle).** A parser over `initial_data` reproduces the
bare-`CourseNode` half of the crawl's own recorded section set exactly.
Expected numbers for `tmp/baseline/sw-root.html` against
`tmp/baseline/swt-all-sections.txt`: **147 of 147 bare CourseNode URLs
recovered, 0 missing, and 0 of the 16 sub-path URLs recovered.** Mechanism:
`initial_data` is a *course-node* tree, so a sub-path like
`/CourseNode/1615865126729195011/Part-3` is a folder entry inside one node's
own file browser and structurally cannot appear in it. Counts as failed at
any missing bare URL, or at any recovered URL the crawl never visited other
than the known 5 (the root itself and the 4 forums the existing filters drop
on purpose).

**Step A result (2026-08-10): confirmed on the registered numbers.**
`ParseCourseTreeNodes` (`internal/scraper/coursetree.go`) reads the payload;
`TestParseCourseTreeNodesAgainstCapturedCourseRoot` decides it. 152 tree
nodes from one response, **147 of 147** visited bare URLs covered, 0 missing,
0 sub-paths, and exactly the 5 never-visited nodes predicted. The one
correction was the test's, not the prediction's — an early draft asserted 4
never-visited nodes where this entry had registered 5, because the root node
carries its own `CourseNode` href unlike the bare `RepositoryEntry` URL the
crawl records.

**Step A2 (live, prediction registered 2026-08-10 before the run).** Step A is
n=1: one course, and the course this repo's saved HTML is richest for — the
exact shape of evidence that has misled this campaign before (Question 31's
2-course probe, Question 24's cached-replay). Before Step B is built on it,
one ordinary crawl carries a rider: `TestCourseTreeCoverage`
(`coursetree_probe_test.go`) runs the crawl unchanged, then issues **one**
extra HTTP GET per course root — strictly after the browser is done, per
`fetchSectionFilesHTTP`'s concurrency invariant — and compares each course's
tree against that course's own `VisitRecords`.

*Expected:* **0 missing bare CourseNode URLs, in all 6 courses**, and 0
sub-path URLs in any tree. *Mechanism:* `initial_data` is emitted by the
course-run page unconditionally for the whole tree; nothing observed in it is
conditional on course size, depth or element type, and the one course
measured is the account's largest and deepest (152 nodes, depth 3). *Counts
as failed:* any single course missing ≥1 bare URL — and that failure would be
the more interesting result, since the only mechanism that produces it is a
course whose tree really is lazy (`"children":true` branches, which the
parser tolerates and which would then show up precisely as missing nodes).
Cost: 6 requests on top of a crawl that was going to run anyway.

**Step A2 result (2026-08-10, live, 6 courses, 349 files): confirmed exactly —
0 missing, in every course.** Full report `tmp/tree-coverage.txt`. **261 of 261
visited bare CourseNode URLs recovered from 6 HTTP requests**, 282 tree nodes,
0 sub-path URLs in any tree, 0 courses short. Per course (tree nodes / visited
bare / missing): Softwaretechnologie 158/152/0, So26 Programmieren 37/34/0,
2026 LA20 36/33/0, Analysis 33/29/0, TUDMATH NuMa 14/12/0, Algorithmen und
Datenstrukturen 4/1/0. The gap between tree nodes and visited is in every case
the root node plus the enrollment/forum nodes the existing filters drop on
purpose — the run log shows each one being skipped by name.

Two things worth keeping from it. **TUDMATH NuMa is one of the two courses the
abandoned 2026-07-21 HTTP crawler returned 0 files for**, and its tree comes
back complete (12/12) from one request — independent corroboration of Question
2's diagnosis that the old implementation scraped rendered anchors rather than
this payload, and that nothing about those courses was ever unreachable.
**Algorithmen und Datenstrukturen is the counter-shape to watch:** only 1 of
its 4 visited section URLs is a bare course node, the other 3 are sub-paths, so
for that course the tree seeds almost nothing. Total sub-paths across the
account are 19 of 280 (16 Softwaretechnologie + 3 AuD), so the seed covers
93% — but it is not evenly distributed and Step B must not assume it is.

*Whole-run cost of the rider:* 6 requests on a 234s run.

**Step B1 (live, prediction registered 2026-08-10 before the run).** Step A2
proved the *seed* is complete; it did not prove the seed plus expansion
reproduces the whole section set. The 7% it cannot seed (19 sub-paths of 280)
is unevenly distributed — 3 of Algorithmen und Datenstrukturen's 4 sections —
so "93% covered" is not a safe basis for the restructure.
`TestHTTPFirstSectionDiscovery` (`httpfirst_probe_test.go`) runs the Step B
algorithm as a rider on an ordinary crawl: seed each course from its root's
`initial_data`, then BFS over plain HTTP using the crawl's *own* predicates
(`parseHTTPSectionCandidates` + `appendSectionFolderTargets`), and diff the
resulting section set against that same run's `VisitRecords`, keyed by
`sectionKey`.

*Expected:* **0 missing sections in all 6 courses.** *Mechanism:* the tree
supplies every bare course node directly, and a sub-path is a folder row in
its parent node's server-rendered file table — the same markup the browser
reads, reached by the same two functions, so a sub-path the browser queues
from a rendered candidate is a sub-path HTTP queues from the identical
candidate. *Counts as failed:* one missing section. *Secondary expectation:*
HTTP-first discovery finishes in **80–110s** (280 sections at the 315ms/section
measured 2026-07-31, plus 6 root fetches), against the browser crawl's ~207s;
counts as failed above 130s.

*Deliberately not re-tested:* file extraction from a section's HTML, verified
diff=0 on all 6 courses 2026-07-31 and untouched since. Re-fetching every
section to re-prove it would double the request count for a known answer.
Section discovery is the only thing Step B changes.

**Step B1 result, run 1 (2026-08-10, live): the section prediction FAILED at
4 missing of 286 — and the cause is sharp enough to have predicted it.** Report
`tmp/httpfirst-sections.txt`. Result: 286 browser sections, 303 HTTP fetches,
**4 missing, 21 extra**, HTTP-first discovery **62.9s** against the same run's
browser crawl at 152.6s.

*The 4 missing are all one mechanism, and it is pagination.* Every one is a
sub-path under the single Softwaretechnologie node `1615865126729195011`:
`32-st-faq-komplexe-objekte.md`, `33-st-faq-kontextmodell.md`,
`34-35-st-faq-statecharts.md`, `36-37-st-faq-scenarios.md`. Diagnosed offline
against dumps already on disk, not guessed: all four appear in
`tmp/baseline/part3-showall.html` and **none** of them appears in
`tmp/baseline/part3-raw.html`, while `30-st-faq-*.md` — inside the same
section's first 20 rows — appears in both. So they live past a paginated
section's first page. `fetchSectionFilesHTTP` already follows the
`pager-showAllLink` toggle for *files*; the probe's folder-target expansion
fetched the raw body and did not. **The generalisable finding: rows beyond a
pagination cap include sub-sections, not only files** — pagination is a
discovery boundary, not just a file-listing one. This is the same
"show all" surface Questions 17/19/22/25 have chased on the browser side,
appearing for the first time on the HTTP side.

*The 21 extra are benign and expected in hindsight.* They are the enrollment,
forum and root nodes the browser path skips — the run log names each one
("structurally cannot hold files"). The probe seeded every tree node without
applying `isNonFileSectionType`, because that filter lives inside
`appendSectionFolderTargets`, which the seed bypasses. Extras cost requests,
never files, so they are a cost item and not a correctness one.

*The timing half beat its prediction.* 62.9s registered against 80–110s
predicted and a 130s kill line, on 303 fetches — i.e. ~208ms/section against
the 315ms/section the 2026-07-31 probe measured. Not investigated this cycle;
recorded as Question 38 below.

**Step B1 run 2 — amended prediction, registered 2026-08-10 before re-running.**
The fix is the one line of mechanism the diagnosis names: follow
`extractShowAllURLFromHTML` in the expansion path and merge its candidates,
exactly as `fetchSectionFilesHTTP` does for files. *Expected:* **0 missing in
all 6 courses**, extras unchanged at 21, fetches up by roughly the number of
paginated sections (3 were flagged in Softwaretechnologie on 2026-07-31), and
discovery still under 90s. *Counts as failed:* any missing section — and a
*different* missing section than these four would mean pagination was only one
of several boundaries, which is the outcome worth knowing.

**Step B1 result, run 2 (2026-08-10, live): confirmed on every registered
number.** Report `docs/measurements-httpfirst-run2-2026-08-10.txt`. **286 of
286 sections discovered, 0 missing, in all 6 courses**; extras unchanged at 21
exactly as predicted; fetches 303 → 313, i.e. 10 paginated sections across the
account; discovery **71.4s** against the same run's browser crawl at 173.8s,
inside the under-90s line. The diagnosis would have predicted the failure in
advance and the fix it named produced the predicted result, so Rule 2 is met
and Step B1 closes.

**What this establishes.** The browser is not needed to discover *anything*:
not the tree (Step A/A2) and not the sub-sections behind a pagination cap
(Step B1). One authenticated HTTP GET per course root plus one per section —
plus one per paginated section — reproduces the crawl's entire section set,
in ~40% of the browser crawl's wall clock, on a path whose file extraction was
already verified diff=0. The 207s floor in "What we know" is a property of
this project's discovery method, not of OPAL.

**Step B2 (production restructure, now unblocked).** Replacing phase 1 with one root
fetch per course and feeding the tree's URLs into the existing HTTP phase
lands the full 6-course run at **90–110s** against the 207s floor, with a
byte-for-byte empty diff. Mechanism: the 164-URL HTTP probe measured 31.7s
serial for this course (`tmp/baseline/swt-probe.log`), the browser walk it
replaces is the 207s, and 6 root fetches are noise. Counts as failed above
130s, and counts as *rejected regardless of speed* on any non-empty
byte-diff. Known incompleteness to design for rather than discover: the 16
sub-paths, and the 3 sections the same probe flagged as advertising a pager.

*Why it is not simply "ship the hybrid":* the sub-path and pager gaps mean
the tree gives phase 1 a *seed*, not a finished section set. The honest shape
is seed-from-tree, then let the existing HTTP phase's own discovery expand
what the seed does not cover — which is what `appendSectionFolderTargets`
already does for the browser.

**Step B2 result (2026-08-10, autopilot, production code written, prediction
registered before the live run).** `scrapeCoursesHTTPFirst`
(`internal/scraper/orchestrator.go`) and `discoverSectionsHTTP`
(`internal/scraper/httpdiscovery_seed.go`) implement exactly the rider's
algorithm as the thing `ScrapeWithSavedSession` actually calls
(`OPAL_HTTP_DISCOVERY=2`) instead of a comparison alongside a browser crawl —
the browser is used only for `discoverCourseLinks` (the course list, three
page loads), never for a single section. `discoverSectionsHTTP` applies
`isNonFileSectionType` at the seed itself (not just during expansion) and
follows `extractShowAllURLFromHTML` during expansion, the two corrections
Step B1 named. 5 offline unit tests
(`internal/scraper/httpdiscovery_seed_test.go`) cover the seed-skip, the
pagination-recovery mechanism (reproducing Step B1 run 1's exact miss against
a fake fetcher), and that one section's fetch failure is logged and skipped
rather than losing the whole course. Build, vet, full suite pass.

*Prediction for the live run, written before running it:* `OPAL_FILELIST=after
OPAL_HTTP_DISCOVERY=2` against `OPAL_FILELIST=before` (plain browser crawl,
same session) produces **an empty diff, 349 files both sides**, matching the
Step B1 rider's 286/286 sections and this project's own current ground truth.
Wall clock for the after run: **under 130s** (Step B2's own kill line),
expected near the rider's 71.4s plus whatever `discoverCourseLinks` costs on
top (not measured by the rider, which ran after an already-open browser
session). Counts as failed at any non-empty diff, regardless of speed — the
byte-diff is the gate, not the timing.

**Live run 1 (2026-08-10): FAILED, but not on the byte-diff — the run never
finished.** `OPAL_FILELIST=before` (plain browser) completed normally: 349
files, matching the ground truth, though its own log shows real transient
network trouble during the window (`net::ERR_CONNECTION_TIMED_OUT` on a
handful of sections, one 3m1s no-progress warning that then recovered).
`OPAL_FILELIST=after OPAL_HTTP_DISCOVERY=2`, run immediately after in a fresh
process, made visible progress (dozens of sections fetched, correctly
reporting 0 files for several genuinely-empty ones) and then stalled for
**20+ minutes** with zero progress before Go's own `-timeout 20m` killed the
test. The goroutine dump at kill time is unambiguous:
`discoverSectionsHTTP` → `httpGetText` → Playwright
`apiRequestContextImpl.Fetch`, blocked inside `fetch.Get`.

*Diagnosed, not guessed, and sharp enough to predict the failure: neither
`discoverSectionsHTTP` (new) nor the pre-existing `fetchSectionFilesHTTP`
ever passed a `Timeout` to `fetch.Get`.* Playwright's own docs claim a
30000ms per-request default when none is passed — not observed in practice
here (the block ran past 20 minutes with no error surfacing at all). Whatever
the exact reason the documented default didn't fire, the mechanism explaining
the *failure* doesn't depend on it: **the entire HTTP discovery/fetch path,
including the already-shipped `verify`/`1` modes, has had no explicit
per-request timeout since it was written on 2026-07-31.** Contrast with the
browser path, where every single `Page.Goto` carries an explicit 15–20s
`Timeout` (`session.go`'s `SetDefaultTimeout`/`SetDefaultNavigationTimeout`)
and `crawl.go` already retries a timed-out navigation and moves on. The two
runs' network trouble was very likely the same ordinary transient flakiness
(both hit it in the same few-minute window against the same account) — the
finding is not "the network was unusually bad for this test," it's that nothing
in the HTTP path was ever built to survive that trouble the way the browser
path already was.

*Fix (2026-08-10, same cycle): every `fetch.Get` call in both files now
passes an explicit 20000ms `Timeout` (`httpGetOptions()`,
`httpdiscovery_fetch.go`) — matching the browser's own
`SetDefaultNavigationTimeout(20000)` budget. A timed-out section fetch is now
just another per-section error: logged via `onSectionError`/`logging.Warn`
and skipped, exactly like a 403 or a malformed response already was, rather
than hanging the whole run. Offline tests unaffected (`fakeHTTPFetcher.Get`
already accepted and ignored the variadic options). Rule 2: this
explanation — no bounded timeout anywhere on this path — would have predicted
today's specific failure shape (progress, then total silent stall, then a
hard kill with no error) in advance, so run 1 counts as diagnosed, not just
failed.

**Live run 2, amended prediction, registered before re-running:** same
comparison, same expectation — **0 missing, 0 extra beyond the seed's known
21, 349 files, empty diff** — with the addition that any individual section
allowed to fail (logged via `onSectionError`) now counts as a *miss* in the
diff rather than a silent hang, so a transient stall this time shows up as a
small, bounded, explainable gap instead of taking the whole run down. Timing
prediction unchanged (under 130s), now genuinely testable since the run can
actually finish.

**Live run 2 result (2026-08-10): the byte-diff PASSED — 349 files, empty
diff — but the timing prediction failed badly, and the cause was this
method's own design, not the network.** `tmp/filelist-after.txt` vs
`tmp/filelist-before.txt`: identical, 349 lines each. But the run logged `6
courses, 302 section requests, 302 file requests, 4m45.5s` — **604 HTTP
requests**, roughly double Step B1's 313-request rider, and the wall clock
blew through the 130s kill line by more than 2x.

*Diagnosed, sharp enough to have predicted it in advance if anyone had
checked the request count against the rider's before running:*
`scrapeCoursesHTTPFirst`'s first version called `discoverSectionsHTTP` to
find each course's section URLs, then called the existing
`fetchSectionFilesHTTP` **again, per section**, to get its files — two full
fetches of every one of 302 sections, where the browser path (and the rider)
only ever fetch a section once. The rider never measured this because it
deliberately only tested discovery ("Deliberately not re-tested: file
extraction..."); nothing before this live run exercised the full two-phase
shape end to end at production scale.

*Fix (2026-08-10, same cycle):* `discoverSectionsHTTP` now extracts files
from each section's body at the exact point it is already being parsed for
child-folder candidates — one fetch serves both jobs, via a new
`extractSectionFiles` helper that runs the identical `appendSectionFiles`
predicate `fetchSectionFilesHTTP` uses (so the merge key and dedupe behavior
are unchanged). `fetchSectionFilesHTTP` itself is untouched, since the
existing `verify`/`1` modes still call it directly and it is not the thing
that needed fixing. `scrapeCoursesHTTPFirst` simplified to match — no more
per-section second loop. All 5 `httpdiscovery_seed_test.go` tests rewritten
around the function's new `[]FileRef` return and still pass, including a
fixture where a file lives two levels below the tree seed (root → tree node
→ sub-path → file), proof the one-fetch shape still reaches it.

**Live run 3, amended prediction, registered before re-running:** same
349-file empty-diff expectation, and now a request-count check too — **at
most ~320 requests** (313 the rider measured, plus room for `discoverCourseLinks`'s
overhead and normal account drift since 2026-08-10's earlier measurements),
counted as failed above 400. Wall clock: **under 130s**, counted as failed
above that line for a *speed* verdict, but note a byte-diff pass at any
speed is still real evidence the algorithm is correct — only the "ship as
the default" question depends on the timing number.

**Live run 3 result (2026-08-10): PASSED both gates.** `302 HTTP requests`
(inside the predicted ≤320), HTTP phase **65.6s** (`3.5s` course discovery +
`65.6s` HTTP), matching Step B1's own 71.4s rider closely — and
`diff tmp/filelist-before.txt tmp/filelist-after.txt` is **empty, byte for
byte, 349 files both sides**. This run's saved session had expired, so it
also incidentally re-confirmed unattended TU-Fast login still completes on
its own mid-test (`CLAUDE.md`'s standing note) — the extra ~57s that shows
in the test's total 122.42s belongs to that re-login, not to discovery; the
number that answers Question 36 is the 65.6s HTTP-phase line the code logs
separately.

**Step B2 is closed: the browser is no longer needed anywhere in
discovery.** `OPAL_HTTP_DISCOVERY=2` reproduces the full 349-file ground
truth in ~69s total HTTP+discovery cost (excluding one-time login), against
the plain browser crawl's ~207s floor named at the top of this file — a
structural win, not a tuning one, because it changes what the 207s floor
even measures. Shipped behind the flag on branch
`restructure-hybrid-http-first-discovery`, not yet the default — per
`docs/BACKLOG.md`'s own instruction this is one of the three paths that has
silently lost files before, so flipping the default is a PR for the
maintainer to land, not an autopilot decision. See that PR for the
default-now vs. wait-a-day options.

**New open questions this closure leaves, ranked:**
1. Does `OPAL_HTTP_DISCOVERY=2` still pass the byte-diff on a *different*
   day/account-state, the same confirmation step Questions 31–33 applied to
   `course_concurrency`? Not yet run — today's three live runs (including
   this one) are all 2026-08-10.
2. Question 35 (raise `course_concurrency` past 2) was explicitly deferred
   pending this closure (`docs/BACKLOG.md` "Next", recommendation (a)):
   worth re-asking now, since it tunes a browser crawl this mode no longer
   runs during discovery — though `course_concurrency` still governs how
   many courses `discoverSectionsHTTP` could run in parallel if that were
   ever added (it currently runs courses serially; not measured whether
   that costs anything against the 65.6s already achieved).
3. `scrapeCoursesHTTPFirst` currently discovers all courses' files, then
   returns everything in one batch — no `PhaseSection` progress events
   during the HTTP phase (removed when the two-fetch design was collapsed
   into one), so a GUI sync using this mode shows less granular live
   progress than the browser path. Not a correctness gap, but worth a
   follow-up if this becomes the default and the GUI's progress bar looks
   wrong to real use.

**Resolved 2026-08-11 (decision round): two independent sessions had built
this same step as separate PRs without knowing about each other — #133
(above, `restructure-hybrid-http-first-discovery`) and #134
(`http-first-discovery-b2`, a lighter-weight rebuild of the same algorithm in
a single new file, 2 live runs, 349/349 zero-diff on both). #134's own
`fetch.Get` call carried no timeout — the identical unbounded-hang bug run 1
above found and fixed here — it simply never fired in #134's two runs.
Maintainer decided: merge #133 (it already found and fixed that bug, plus the
double-fetch inefficiency, with 3 live runs and 5 tests to #134's 2 runs),
close #134 as superseded, and ship `OPAL_HTTP_DISCOVERY=2` as the default
immediately rather than wait for a different-day confirmation run — same
call as the `course_concurrency=2` precedent (2026-08-10), same residual
caveat (all live evidence is one day/account-state). Open question 1 above
(different-day confirmation) is therefore answered by policy, not by a
fresh run: accepted risk, not proof. Root cause of the duplication and both
PRs' provenance: see `docs/BACKLOG.md` Noticed section.

### 34. ~~Does the HTML the crawl already receives point at content it has to navigate for — and if so, how much of the tree can be read without the per-branch navigation?~~ Answered 2026-08-10 (autopilot, saved HTML + source reading, no live run): the concealed-structure half is a **hit**, and the prediction this file had pre-registered for it was wrong

**The pre-registered prediction failed, and that is the finding.** This
entry said on 2026-08-10, before the work: *"What would make it a dead end:
`isRenderChildren()` gating the serialized output too, i.e. unopened branches
genuinely absent from the bytes. That is the likely outcome for the
concealed-structure half and should be stated as the prediction rather than
discovered as a disappointment."* It is not the outcome. The serialized bytes
are far less frugal than the rendered tree.

**What is actually in the response.** Every course page carries
`var initial_data=[...]` in a plain `<script>` — jstree's own data payload,
emitted server-side. For Softwaretechnologie it is the **complete 152-node
course tree**, nested to depth 3, each node carrying its absolute
`.../CourseNode/<id>` href in `a_attr.href` and its `node-<type>` class in
`li_attr.class`. Exactly **one** node carries `"state":{"opened":true}` — the
root — so this is emphatically not the open-branch scoping Question 9
measured. The adjacent jstree config is
`data: function(node,datacb){if(node.id==="#"){datacb(initial_data)}else{load(datacb,node)}}`,
i.e. the lazy per-branch `load()` path Question 9 found is only ever reached
for nodes *not already in* `initial_data` — and here that set is empty.

**Checked against the crawl's own record, not by eye.** Of the 164 distinct
section URLs in `tmp/baseline/swt-all-sections.txt`, 147 are bare
`CourseNode` URLs and **every one of the 147 is present in that single
response's tree**; zero visited-but-absent. The 5 tree nodes never visited
are the root itself and 4 `node-fo` forums the existing filters exclude
deliberately. Depth histogram 1/14/54/83 — so the tree the crawl reaches
through at least 4 sequential BFS levels is fully present after **one** page
load.

**Present everywhere, not a property of the root page.** All six saved
Softwaretechnologie dumps (root, entry, sec1–3, part3-raw, part3-showall)
carry the identical 152-node payload, and an unrelated course's dump
(`internal/scraper/tmp/htmlstability-a.html`) carries its own 38-node one. It
also arrives over plain authenticated HTTP: `tmp/baseline/sw-root.html` is a
server response, not a browser DOM dump — its tree is still raw JSON in a
script tag, with only 54 `<li>` elements in the whole document against 152
tree nodes, because jstree had not run.

**The honest limit of the result.** The tree is a *course-node* tree and
nothing more. The other 17 of the 164 URLs are 1 course root and 16 sub-paths
(`/CourseNode/1615865126729195011/Part-1…Part-4` and 11 `.md` documents) —
folder entries inside a single node's own file browser, which no course-node
tree can contain. So this removes the tree walk, not all discovery: **90% of
this course's section URLs come free, the remaining 10% still need that one
node's own response.**

**The reuse half of the question is not answered** and stays open below as
Question 37 — this cycle spent its budget on the sharper half.

**What it changes:** `httpdiscovery.go`'s design comment (*"OPAL renders the
course-content navigation TREE client-side (a browser has to walk it)"*) has
steered this project since 2026-07-21 and is misleading. jstree does render
client-side, but the *data* is server-delivered in the first response, so no
browser is needed to enumerate it. Corrected in place. The consequence is
Question 36 above.

### 38. Why is an HTTP section fetch ~208ms now when it measured 315ms on 2026-07-31? — OPEN, opened 2026-08-10 by Question 36 Step B1

Step B1's run did 303 authenticated section fetches in 62.9s — ~208ms each,
a third faster than the 315ms/section the 2026-07-31 probe measured over the
same account and the same code path. Nothing in this cycle touched the fetch
path, so the candidates are external (server load, time of day, network) or
methodological (the older number came from a probe that fetched a fixed
164-URL list; this one interleaves parsing between requests, which would make
it *slower*, not faster).

*Why it matters rather than being trivia:* every projection of an HTTP-first
crawl's floor uses this constant. At 315ms the 280-section account projects
~88s; at 208ms it projects ~58s. The 30s target is a different distance away
depending which is real, and one of the two numbers is measured under
conditions nobody wrote down. *Cheapest decisive step:* re-run Step B1's
probe on a different day and compare, since it now records its own timing —
no new instrument needed.

**Downgraded 2026-08-10 (autopilot, later the same day): still open, but no
longer load-bearing.** This question only ever mattered as a way to *project*
Step B2's real-world floor before Step B2 existed. Step B2 now exists and has
been measured directly, end to end, on a live account: 71.99s and 78.90s
wall-clock for the whole 6-course crawl (PR #134), both inside the
90-110s/130s-failure prediction band regardless of which per-fetch constant
is real. So the practical question this was a proxy for is answered by a
better measurement than either 208ms or 315ms could give. What is left open
is the "why" itself (server load vs. methodology vs. time-of-day), which is
worth knowing but no longer worth spending a live run on ahead of anything
else - re-rank below Question 35 rather than above it. *Also worth noting,
not yet reconciled:* today's own numbers cluster in the 200-250ms range
across four independent fetches-per-second-implied measurements (Step B1 run
2's 208ms, and the two Step B2 production runs' rougher ~230-250ms
wall-clock/fetch-count estimates) - i.e. 2026-08-10 is internally consistent
with itself, and it is specifically 2026-07-31's 315ms that stands apart. That
shifts suspicion toward "that day was slower" over "today is unrepresentative,"
but this is an observation, not the live re-run the *why* still needs.

**Prediction, written 2026-08-11 before running, per Rule 1.** Cheapest
decisive step named above: re-run `TestHTTPFirstSectionDiscovery`
(`internal/scraper/httpfirst_probe_test.go`, unchanged since 2026-08-10) on a
different calendar day. Nothing in the fetch path or the test's own
methodology has changed since the four 2026-08-10 measurements, so this run
isolates exactly the external factor (server load / time-of-day / network)
that is the leading candidate.

*Expected:* per-fetch time (`httpElapsed / totalFetched` from
`tmp/httpfirst-sections.txt`) lands in the **200-260ms** range, consistent
with all four 2026-08-10 data points, and `totalMissing = 0` (Step B1 run 2's
pagination fix is unchanged in this file, so the section-discovery half
should replicate clean). *Mechanism:* if 2026-08-10 was itself representative
and 2026-07-31 was the slow day, a second day drawn independently of
2026-08-10 should reproduce the same cluster rather than split the
difference. *Counts as failed:* a result outside 180-300ms — either back
toward 315ms (would mean 2026-08-10 was the outlier, not 2026-07-31, and the
"which day is normal" question stays open with one more data point against
it) or below 180ms (would mean neither number is stable and something else,
not day-to-day variance, is producing the spread). A missing section would be
a separate, higher-priority finding (a pagination-fix regression) and stops
the timing question from being answerable this cycle.

**Result (2026-08-11, live, autopilot): inside the failure bound but under
the expected band — and the run surfaced two findings bigger than the timing
question itself.** First attempt at this rerun found `browser sections 0` in
every course - not a timing result, a broken instrument. Between Question
38's prediction being written (2026-08-10 evening) and this run, PR #133
(Question 36 Step B2) merged and HTTP-first became `scrapeCoursesHTTPFirst`'s
production default. That silently broke two things this probe design leaned
on, and both are now written up and fixed/reframed separately: the visit-log
regression (below) and Question 35's premise (also below). Re-ran after
fixing the first; the second remained (see Question 35).

*The timing number itself, second attempt:* production path (the probe's own
"browser crawl" ground-truth step, which - per the same finding above - is no
longer a browser crawl but `scrapeCoursesHTTPFirst` itself) did **303
requests in 55.93s = 184.6ms/request**; the probe's own separate HTTP-first
pass did **314 fetched in 59.97s = 191.0ms/request**. Both inside the
180-300ms line (not failed), both *below* the 200-260ms expected band -
faster than predicted, not slower. Three data points now cluster at
185-250ms (2026-08-10's four measurements plus this one) against
2026-07-31's one measurement at 315ms - a full calendar day later, at a
different time of day (this run: 2026-08-11 ~13:45 CEST, inferred from a
response header logged mid-run; 2026-08-10's times were never recorded), the
result still sits nowhere near 315ms. That is another point in favor of "07-31
was the slow day" over "08-10 was optimistic," per Rule 1's own registered
logic, even though the specific 200-260ms band undershot.

**Not closed (Rule 2): the *why* is still not answered, and is not worth
a fourth live run to chase.** This result narrows "which day is normal" a
little further but does not name a mechanism for 2026-07-31's slowness
(server load / time-of-day / something else) - it only adds a data point
against it. Given the question was already downgraded 2026-08-10 as
non-load-bearing (Step B2's own direct measurement answers the practical
question this was ever a proxy for), and every data point since has kept
saying the same thing, further live runs here would spend account load
sharpening trivia rather than the "why," which needs an actual mechanism
(e.g. a captured OPAL server response-time header, or a controlled
same-account request at a different hour) that no rerun of this same probe
can provide. **Parked**, not closed - the honest state is "still don't know
why, but it stopped mattering before the answer did."

**Finding 1 (bigger than the timing question): shipping Step B2 as the
default silently stopped `internal/visitlog`'s persistent cross-run log from
accumulating anything.** `scrapeCoursesHTTPFirst` never called
`recordSectionVisit` - only the browser path did. Every real sync/list run
since PR #133 merged had been recording 0 section visits, with no error or
warning (`persistVisitLog`'s own no-op-on-empty behavior, by design, for the
"scrape failed before visiting anything" case - which this wasn't, but looked
identical to it). Not a file-loss bug - the crawl's own file-finding
correctness is unaffected and already byte-verified separately - but a real
regression in a resource this campaign itself has used as evidence (Question
37's admission bar: "10 distinct nodes across 7 of the 8 courses, zero
cross-contamination", built entirely from this log). **Fixed same cycle:**
`discoverSectionsHTTP` (`internal/scraper/httpdiscovery_seed.go`) now takes
an `onSectionVisited` callback, called once per section actually reached
(root included) with its new-files-only count - the same semantics
`recordSectionVisit`'s own doc comment states for the browser path. Wired in
`scrapeCoursesHTTPFirst`. New regression test
(`TestDiscoverSectionsHTTPReportsVisitsForVisitLog`,
`httpdiscovery_seed_test.go`) asserts one call per reached section with the
correct count; full local suite green; live-verified working in this same
cycle's second attempt (298 sections came back in `VisitRecords()`, not 0).

**Finding 2: Question 35's whole premise no longer matches which code path
ships.** See Question 35's own entry below - `course_concurrency` was never
wired into `scrapeCoursesHTTPFirst` at all, only into the browser path it
replaced as the default.

### 37. Does a page the crawl already fetches carry file data the crawl then navigates again to fetch? — OPEN, the unanswered half of Question 34

Question 34's reuse half, deliberately left for its own cycle after the
concealed-structure half turned out to be a hit and consumed the budget. The
sharp version now that the tree is understood: the crawl visits every one of
a course's 147 nodes, but `initial_data` already tells it each node's *type*
(`node-sp` 74, `node-bc` 32, `node-st` 22, `node-iqtest` 8, `node-bib` 6,
`node-fo` 4, `node-tu` 2, `node-info` 1, `node-en` 1, `node-ll` 1 for
Softwaretechnologie). `isNonFileSectionType` (`section_type.go`) already
skips `node-en` — but only *after* a navigation has revealed the class in the
DOM. Reading it from the tree instead would skip those nodes without ever
fetching them, and the same live cross-check that admitted `node-en`
(documented in that file) could then be run over the other types cheaply,
against saved HTML rather than the account.

*Kill criterion:* if the type-to-file-capability check cannot be made from
saved dumps alone, this drops behind Question 36 rather than spending live
runs — 36 is worth ~100s and this is worth at most the 4 forum visits plus
whatever `node-iqtest`/`node-bib` turn out to be.

**Closed 2026-08-10 (autopilot, no live run - the persistent visit log
already had the answer): the type-to-file-capability check for five more
classes is single-course-strong but not worth building.** Cross-referenced
`tmp/baseline/sw-root.html`'s tree (via `ParseCourseTreeNodes`) against
`internal/visitlog`'s persistent cross-run log, which records every section
this account's real crawls have ever visited going back weeks - a resource
this cycle didn't need a new run to read. For Softwaretechnologie, every node
of five classes came back at **0 files across 84-85 independent historical
runs each**: `node-bib` (6 nodes, "Literaturverzeichnis"/bibliography
entries), `node-iqtest` (8, self-test quizzes), `node-ll` (1, "Linkliste"),
`node-info` (1, "News"), `node-tu` (2, Videocampus embeds/playlists) - the
same order of evidence `node-en`'s own entry in `section_type.go` was
admitted on (`nonFileSectionTypeClasses`'s doc comment: "10 distinct nodes
across 7 of the 8 courses, zero cross-contamination").

**Why it stops here instead of shipping.** That comment's own bar is
*cross-course* confirmation, and this data is one course's tree dump - no
other course's root HTML was ever saved, so the same check can't be repeated
elsewhere without either a fresh live fetch (cheap, but a new dependency this
low-value a question doesn't justify on its own) or waiting for one to be
saved incidentally by other work. More decisive: **the payoff shrank out from
under this question while it sat open.** It was scoped against the browser
crawl, where skipping a node saves a real navigation-plus-settle-wait
(~180-360ms measured elsewhere in this file). Question 36 Step B2 (this same
day) replaced that with a bare HTTP GET at ~200ms, so the total available
saving - at most 18 nodes in this one course - is now on the order of a few
seconds account-wide, and drops further once B2 merges and HTTP-first is the
only path left to optimize. Not worth the false-positive risk (a
mis-classified type silently drops real files, the exact failure class this
whole file exists to avoid) for a saving that small. Left as a reference for
whoever revisits it: the audit method (tree class × visit-log file count, no
live run) is cheap to repeat if a future cycle saves another course's root
HTML anyway.

#### 34, as it was pre-registered (kept verbatim — this is the prediction that failed)

**Opened 2026-08-10 (maintainer's request, decision round).** The maintainer's
words: *"look back at html. Look if you can maybe use some of the stuff loaded
there. Also really look in the pages, whether or not you could find anything
that indicates stuff that is not directly loaded there."* Ranked top because
it is the only open question that could change the crawl's *shape* rather
than its constants, and because the whole campaign so far has measured the
timing of the existing navigation without ever auditing what the responses
already contain.

Two halves, deliberately kept together since one pass over the same saved
HTML answers both:

- **Reuse.** Does a page the crawl already fetches carry file or section data
  that the crawl subsequently goes and fetches again by navigating? A hit
  here is free — fewer requests, no new risk surface.
- **Concealed structure.** Is there anything in the *response payload* that
  reveals a node's children without opening that branch — embedded JSON,
  Wicket component metadata or behaviour URLs, `data-` attributes, inline
  script config, `<link rel>` hints, `display:none` subtrees the renderer
  emitted anyway? This is the sharp version, because Questions 2 and 9
  established that the tree is only ever revealed one navigation per
  newly-opened branch — but both looked at the *rendered DOM*, via
  `MenuTreeRenderer.isRenderChildren()`. If the served bytes are less
  frugal than the rendered tree, the ~207s crawl floor is a property of how
  this project reads the response, not of what the server sends.

*How it gets answered:* source reading plus saved HTML, no live account run
needed to start — OpenOLAT is open source (`MenuTreeRenderer` and the
`Ordner`/`BCWebService` path are already cited in this file and in
`docs/opal-webdav-student-access.md` §4), and a single captured section
response is enough to check the payload half. Prediction gets written before
anything is measured, per Rule 1.

*What would make it a dead end:* `isRenderChildren()` gating the *serialized*
output too, i.e. unopened branches genuinely absent from the bytes. That is
the likely outcome for the concealed-structure half and should be stated as
the prediction rather than discovered as a disappointment.

### 35. Is `course_concurrency=3` byte-clean at full scale on the shipped 150ms/6000ms concurrent budget?

**Opened 2026-08-10 (maintainer's request, decision round).** With 2 now
shipped (Question 33), the maintainer asked to push concurrency higher. This
is a parity sweep, not an open mechanism question: 3 has not been measured
since *any* of the 2026-08 work (its last data point is 2026-07-21, before
the debounce change, before Question 25's fix, before Question 33's
decoupling), and 4 lost 9 files when last measured. Same discipline as
Questions 31–33: `OPAL_COURSE_CONCURRENCY_OVERRIDE=3`, full 6-course
`TestFileListSnapshot` with `-count=1`, diffed byte-for-byte against a fresh
same-session 349-file conc=2 baseline.

*Kill criterion, written now:* any non-empty diff stops this at 2 for good
rather than opening a hunt — the mechanism behind loss under contention is
already known (Questions 16/17's Wicket "show all" bug, still unfixed) and
finding it again at a higher concurrency teaches nothing new. Do not test 4
unless 3 is clean twice.

*Sequencing note:* ranked below Question 34 on purpose. 34 needs no account
and could invalidate the premise that more concurrent tabs is the lever
worth pushing; 35 costs live runs against the real account either way.

**Overtaken 2026-08-11 (autopilot, source reading, found while working
Question 38): this question's premise no longer matches which code path
ships.** `course_concurrency` / `effectiveCourseConcurrency()` is only ever
read in `scrapeCoursesBrowser` (`orchestrator.go` line 75,
`collectCourseFilesConcurrently`) - confirmed by reading, not inferring.
`scrapeCoursesHTTPFirst`, the production default since PR #133
(2026-08-11), loops its courses with a single unadorned `for` (`orchestrator.go`
lines 296-321) - no concurrency knob exists on that path at all. Question 35
was scoped as "push concurrency higher on the shipped default" when the
default was still the browser crawl; the default underneath it has since
changed, so a byte-diff sweep at `course_concurrency=3` would now be tuning
`OPAL_HTTP_DISCOVERY=0`'s rollback path, not what anyone actually runs.

**Not closed, reframed.** The rollback path still exists and a maintainer
could still fall back to it, so this isn't nothing - but it is low value
compared to what the same 55.93s/59.97s HTTP-first timing in Question 38's
result cycle actually suggests: `scrapeCoursesHTTPFirst` is *itself*
100% serial across courses today (confirmed above), and each of its 6
courses' fetch chunks is visible in `tmp/httpfirst-sections.txt`'s per-course
breakdown (Softwaretechnologie alone: 179 fetched). Concurrent HTTP GETs
carry none of the browser path's shared-mutable-page hazard that
`course_concurrency`'s whole cautious history (Questions 16/17/22/25) was
about - they're stateless round trips over the same authenticated
`fetch`/`httpFetcher`, already proven safe to reuse (Step B1/B2 both use one
fetcher for hundreds of sequential requests without incident). **Opens
Question 40** (below): does course-level concurrency on `scrapeCoursesHTTPFirst`
itself replicate a speed win the way Question 33 found for the browser path,
on a code path that structurally avoids the mechanism that made that
question hard the first time around? Ranked above the original Question 35's
residual (the rollback path is not what ships) but behind Question 39
(correctness safety net) per the standing correctness-first rule.

### 41. ~~Does a second confirming run (different day) also produce an empty diff at `OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE=2`, and is promoting it to the shipped default then in scope for that cycle?~~ Closed 2026-08-11 (autopilot, second live run, same day but a fresh interactive login and ~6h after the first pair) — **no: the second run lost 6 files. The hazard the question was written to rule out is real, just intermittent, and this closes the promotion question as a no-go.**

Question 40 (below) found an empty diff at concurrency=2 on its first live
run: 349/349 files, 41.6s discovery against a 56.7s serial baseline, both
inside the pre-registered prediction. Not itself in question anymore is
whether the mechanism works *at all* - one clean run already shows the
shared `APIRequestContext` tolerates two concurrent goroutines' `.Get()`
calls. What is still open is purely a repetition/promotion question: this
project's own bar for shipping a discovery-path change as the default was
two clean byte-diffs (Step B2, PR #133/#134, 2026-08-10), not one, and both
of Question 40's runs happened minutes apart in the same session against the
same OPAL server session and course set - closer to one data point on
incidental conditions than two independent ones. A second run on a different
day (different account/server state, different time of day) is the cheap
next step; if it also comes back empty, the question becomes whether
whoever picks it up should also fold in the `course_concurrency` /
`OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE` unification (Question 40's own noted
follow-up) as part of the same promotion decision, or ship the override
alone first and unify later. *Counts as failed:* any missing file on the
second run - if it fails once out of two, that is not noise to average away,
it is exactly the "shared object under concurrent load" hazard Question 40
was written to rule out, and it should stop the promotion, not average
against the clean run.

**Result, run 2026-08-11 (autopilot, ~15:00 -> ~21:22, machine and account
confirmed quiet first - no `chrome.exe`/`go`/`opal-downloader`/`node`
processes, `git log -3` nothing from another session in the preceding hours).
Not literally a different calendar day, but materially different conditions
from the first pair: hours later, a different session, and a saved session
that had expired, so the baseline run went through a fresh interactive
TU-Fast/2FA login rather than reusing the first run's warm session.**
`OPAL_FILELIST=before` (serial, concurrency=1): **349 files**, 76.0s
discovery / 97.4s test (slower than the first run's 56.7s baseline - the
fresh login's own cost, not a discovery regression). `OPAL_FILELIST=after
OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE=2`: **343 files**, 52.3s discovery /
57.3s test. `diff tmp/filelist-before.txt tmp/filelist-after.txt`: **6 files
missing**, all from one section: "Algorithmen und Datenstrukturen" ->
"Vorlesung" -> `Vorlesung_7.pdf`/`_7p.pdf`/`_8.pdf`/`_8p.pdf`/`_9_10.pdf`/
`_9_10p.pdf`.

**Mechanism, named, not just observed (Rule 2).** All 6 missing files come
from a single section, and that section is exactly the shape
`fetchSectionFilesHTTP` (`httpdiscovery_fetch.go`) treats specially: a
paginated one, needing a *second* HTTP GET (the show-all AJAX URL) beyond
the first page's ~20-row cap to recover the rest. Question 40's own
implementation notes named the precise risk before any code was written:
`s.httpDiscoveryFetcher()` returns the browser context's single
`APIRequestContext` object, not one per goroutine, so concurrency here
means N goroutines calling `.Get()` on the *same* Playwright object at once
- "something nothing in this codebase has done before," in that section's
own words. The first run (ad42760) showed the transport *can* multiplex
those calls correctly; this run shows it does not always - and the failure
lands specifically on the two-GET paginated case, consistent with a race
between one course's show-all follow-up request and a concurrent course's
own `.Get()` on the same shared object (a dropped/misattributed response,
not a timeout - `fetchSectionFilesHTTP` logs a warning on an actual GET
error or non-200 status, and this run's log shows neither, meaning the
section's first-page fetch itself silently under-reported, or the
show-all follow-up silently returned the wrong body). This predicts where
else the same thing would show up: any paginated section (a second `.Get()`
within one course's own crawl), not sections in general - which fits: this
run's 6 missing files are the *only* section-shaped loss in the whole diff,
and the 5 non-paginated courses came through untouched.

**Closes the promotion question definitively.** Per the pre-registered kill
criterion, one miss out of two runs is not noise to average against the
first clean run - it is confirmation of the exact hazard the question was
written to rule out. `OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE` stays a
test-only override, defaulting to 1 (serial), never wired to
`config.yaml`'s `course_concurrency` or shipped as a default - no
production behavior changes as a result of this finding, because none ever
depended on the override being safe. Not worth a root-cause chase beyond
the mechanism named above: the override has no path to production without
someone reopening this question, and the underlying object
(`playwright.APIRequestContext`) is a third-party dependency, not this
project's own code - fixing it would mean either giving each goroutine its
own browser context (heavier than this question's scope) or serializing
just the paginated-section follow-up fetch, neither of which is worth
building for a lever that was already secondary to Question 39's
correctness-safety-net thread and Question 5.

**Next:** nothing further on this thread unless someone wants
course-level HTTP concurrency badly enough to fund the isolated-context
rewrite. Question 39 (blocked on the maintainer's pick among three options)
and Question 5 are what is left on the ranked list.

### 40. Does `scrapeCoursesHTTPFirst` benefit from course-level concurrency, given it has none today and the hazard class that made the browser path's version hard (Questions 16/17/22/25) does not obviously apply to stateless HTTP GETs? — OPEN, opened 2026-08-11 by Question 35's reframe

What is already known: `scrapeCoursesHTTPFirst` processes its 6 courses fully
serially (`orchestrator.go` lines 296-321, no goroutines), and Question 38's
2026-08-11 result measured 55.93s/303 requests end to end for that serial
loop. Naively parallelizing N courses' HTTP fetches could shrink that close to
the single slowest course's own share (Softwaretechnologie's 179 of 303
fetches is already the long pole) rather than the sum. The open question is
real, not rhetorical: OPAL's HTTP session is authenticated per-cookie and this
campaign has never load-tested N concurrent authenticated GETs against it the
way the browser path's concurrency was tested - `Question 33`'s finding that
a *browser* crawl's correctness hazard was Wicket-AJAX-specific (an artifact
of DOM state and in-page JS, not of the HTTP session itself) is suggestive
but not proof that concurrent stateless GETs are equally safe.

**A second, sharper risk found while scoping the implementation (2026-08-11,
before writing any code):** `s.httpDiscoveryFetcher()` returns
`ctx.Request()` - the browser CONTEXT's single `APIRequestContext` object,
not a fresh one per call. There is no way to give each concurrent course
worker its own isolated fetcher without opening a second browser context (a
different, heavier change than this question is scoped for), so
"concurrent HTTP GETs" here specifically means **N goroutines calling
`.Get()` on the exact same Playwright object at once** - something nothing in
this codebase has done before. The nearest precedent is
`collectCourseFilesConcurrently` already calling `ctx.NewPage()` concurrently
from N goroutines against one shared `BrowserContext` (a related but
different object, over the same underlying driver-process transport) without
incident - suggestive that the transport multiplexes concurrent calls safely,
but not proof for `APIRequestContext` specifically. This is the real
correctness question here, not Wicket - if it fails, the likely symptom is
requests timing out, erroring, or (worse, and what the byte-diff exists to
catch) a response landing against the wrong in-flight request.

**Implementation, gated per the standing rule ("every experiment behind an
env flag, off by default").** Reused `collectCourseFilesConcurrently`
(orchestrator.go) verbatim rather than writing a second worker pool -
`newHTTPCourseFileCollector` wraps `discoverSectionsHTTP` in the same
`func(CourseRef) (courseCrawlResult, error)` shape `newCourseFileCollector`
already provides for the browser path, so both discovery paths now share one
hardened, already-tested concurrency implementation instead of one bespoke
serial loop and one worker pool. `discoverSectionsHTTP` also gained a
`downloadCandidates` map, previously discarded (`extractSectionFiles` always
passed `nil`) - a real but low-severity gap found while touching this code:
without it, any HTTP-first-discovered file whose direct-GET download fails
skips the free counter-refresh retry the browser path gets and falls straight
to the slow browser-click fallback. Fixed as part of the same refactor since
`courseCrawlResult` already carries the field and `mergeDownloadCandidates`
already exists to merge it - not a separate change. New env var
`OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE` (mirrors `OPAL_COURSE_CONCURRENCY_OVERRIDE`'s
existing test-only pattern) controls concurrency in a probe test; unset,
`scrapeCoursesHTTPFirst`'s default concurrency is **1 (serial, today's
unchanged behavior)** - config.yaml's `course_concurrency` is deliberately
NOT wired into this path yet, so shipping this code does not silently change
production behavior for anyone, even though `effectiveCourseConcurrency()`
exists and could seem like the obvious wire-up. Unifying the two concurrency
knobs is a follow-up decision, not this cycle's.

**Prediction, written 2026-08-11 before running, per Rule 1.** Same
discipline as Questions 31-33: full 6-course `TestFileListSnapshot`,
`OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE=2` against a fresh same-session
serial (concurrency=1) baseline, diffed byte-for-byte.

*Expected:* **empty diff, 349/349 files both sides.** *Mechanism:* if the
shared-`APIRequestContext` concurrency risk above is unfounded (the
transport multiplexes correctly, matching the `NewPage()` precedent), two
courses' independent HTTP fetches should not interact at all - unlike the
browser path, there is no shared mutable DOM/Wicket state between courses
here, only independent GET/response pairs. *Counts as failed:* any missing
file (stops at 2, no hunt, mirroring Question 35's kill criterion - if this
fails it teaches "the shared fetcher isn't safe for concurrent use," and
finding that again at concurrency=3 adds nothing). *Secondary, speed:*
expected **35-45s** (Softwaretechnologie's 179-fetch share dominates a
2-way split of ~303 total requests, so not a full 2x speedup), counted as
uninteresting (not failed, just not the win hoped for) above 50s.

**Result, run 2026-08-11 (machine and account confirmed quiet first - no
`chrome.exe`, no `go`/`opal-downloader`/`node`, `git log -3` nothing from
another session in the preceding minutes).** `OPAL_FILELIST=before` (serial,
concurrency=1, same as Question 38's baseline): 349 files, 56.7s discovery /
61.9s test. `OPAL_FILELIST=after OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE=2`:
349 files, 41.6s discovery / 46.3s test. `diff tmp/filelist-before.txt
tmp/filelist-after.txt`: **empty.** Both halves of the prediction held:
349/349 files identical byte-for-byte, and 41.6s sits inside the predicted
35-45s window - the shared `APIRequestContext` handled two goroutines'
concurrent `.Get()` calls with no missing, duplicated, or misattributed
response, and the speedup landed almost exactly where Softwaretechnologie's
179/303-fetch dominance predicted it would (not a full 2x, as expected).
**The correctness hazard named above (shared Playwright object across
goroutines) is empirically unfounded at concurrency=2, on this one run.**

**Not yet promoted to default.** This is one run. PR #133/#134's own
byte-diff (Question 38's HTTP-first-as-default change) was required to pass
**twice**, 2026-08-10, before shipping - matching that bar before wiring
`OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE=2` (or unifying it with
`course_concurrency`, per the still-open follow-up decision above) into the
shipped default would need a second confirming run, ideally on a different
day/account-state than this one. Left as the next step below rather than run
back-to-back in the same session as the first - two runs minutes apart share
more incidental state (same OPAL server session, same course set, same
network path) than two runs on different days, and the thing being checked
for is exactly the kind of rare transport-level interaction that repetition
across *different* conditions catches better than repetition across
identical ones.

**Next open question, ranked above the original Question 35 residual:**
Question 41 - does a second confirming run (different day) also produce an
empty diff at concurrency=2, and if so, is promoting the default in scope
for a future cycle to decide alongside the `course_concurrency` unification
follow-up?

### 39. Now that HTTP-first discovery is the production default, does anything still cross-validate it against an independent browser crawl - or did shipping it as default quietly remove the only thing that was catching a regression like Question 38's visit-log finding? — OPEN, opened 2026-08-11 by Question 38's result

**Why this ranks above Question 40 (correctness before speed, standing rule).**
Before PR #133, every live run of `TestHTTPFirstSectionDiscovery` compared
HTTP-first's output against a *real* browser crawl - two independently-coded
paths agreeing was the evidence base the whole Step B1/B2 sequence stood on,
and it is what caught Step B1 run 1's pagination miss and Step B2's
double-fetch bug. Now that `scrapeCoursesHTTPFirst` **is**
`ScrapeWithSavedSession`'s default, that comparison silently stopped: this
same probe, re-run 2026-08-11 for Question 38, reported "MISSING 0" between
two invocations of the *same* algorithm (the "browser crawl" step and the
probe's own HTTP pass are both HTTP-first now), which proves internal
consistency, not correctness against an independent source. The one
remaining independent verification is the PR #133/#134 byte-diff itself
(349/349, twice, 2026-08-10) - a point-in-time check, not an ongoing one.

**What would make this urgent vs. not:** if OPAL's server-side rendering of
`initial_data` or a section's file table ever changes shape (a platform
upgrade, a config change on BPS's side), HTTP-first could start silently
missing files with nothing in this project's own test suite positioned to
notice, since `OPAL_HTTP_DISCOVERY=0` (the browser path, the only thing that
could still catch it) is opt-in and nobody runs it by default anymore.
Against that: OPAL/OpenOLAT's markup has not changed shape once in this
entire campaign's history (2026-07 to 2026-08-11), so the risk is real but
not evidenced as live.

*Candidate next step, not yet a registered prediction:* a periodic (not
every-run) correctness spot-check - e.g. `OPAL_HTTP_DISCOVERY=verify` mode
already exists and does exactly the two-path comparison this question wants;
the open question is whether anything should *run* it periodically (a
scheduled Routine, a monthly manual check) now that nothing does by default,
or whether the one-time PR byte-diff plus this project's general practice of
re-verifying before any further discovery-path change is enough. A product/
process decision more than a code one - options belong to whoever picks this
up, not a live run.

**Options, written 2026-08-11 (autopilot, source reading only, no live run) -
this is a maintainer call on a real cost/benefit trade, not something to just
implement.** Confirmed by reading `orchestrator.go`/`scraper.go`: `verify`
mode is real and already wired (`ScrapeWithSavedSession` case `"verify"` runs
`scrapeCoursesHybrid` - full browser crawl *and* full HTTP fetch, serially,
diffed per course, returns the trusted browser result), but nothing calls it
outside the `TestHTTPFirstSectionDiscovery` probe test - no CLI flag, no
Routine, no cron. The existing `opal-downloader-weekly-review` local
scheduled task (Mondays+Thursdays, `~/.claude/scheduled-tasks/opal-downloader-
weekly-review/SKILL.md`) is review-only by its own stated rule ("you read and
report, you do not implement fixes") and already has the worktree +
config-copy pattern this would need, but running a live crawl is a different
kind of action than what it does today.

- **(A) Do nothing further.** Keep the one-time PR #133/#134 byte-diff
  (349/349, twice, 2026-08-10) plus the project's general practice of
  re-verifying before the *next* discovery-path change. Cost: none. Risk: if
  OPAL/OpenOLAT's server-side markup ever changes shape between discovery-path
  changes - which could be a long gap, since this path is not expected to
  change often now that it ships as default - nothing in this project notices
  until a human sees missing files. Consistent with "OPAL/OpenOLAT's markup
  has not changed shape once in this campaign's history," i.e. betting the
  risk stays as low as it has been.

- **(B) A monthly `OPAL_HTTP_DISCOVERY=verify` spot-check, run from a new Part
  C on the weekly-review pass, not from `sync`.** Guarded the same way that
  pass already guards itself (`docs/last-review-commit.txt`'s 2-day check;
  this would need its own `docs/last-verify-run.txt` timestamp, gated at
  roughly 30 days). Deliberately *not* wired into `sync --scheduled` or any
  daily path: `verify` mode runs a full extra browser crawl on top of the
  HTTP-first one it's checking, which would roughly double the very sync time
  Step B2 shipped to cut, on every single run. Filing the diff's `missing`
  count as a `docs/BACKLOG.md` item (mirroring how the friction campaign already
  files its findings) would surface a real regression at the review pass's own
  cadence (3-4 days late at worst) instead of never. Cost: one extra full
  browser crawl (~3-4 minutes going by this session's own 61.9s HTTP-first
  timing plus a comparable browser-crawl share) roughly once a month, on a
  pass that already runs unattended. This would be new code, not review - a
  scope change to what that pass does, which is itself worth flagging rather
  than sliding in quietly.

- **(C) A cheaper structural tripwire that never runs a second live crawl.**
  Track a small fingerprint of course shape - e.g. section count per course,
  or a hash of each course's `initial_data` tree - already visible in data
  `scrapeCoursesHTTPFirst` collects, and warn if it changes unexpectedly
  between two ordinary runs. Free (no extra crawl), but a weaker signal: a
  shape change isn't necessarily a file-count regression, and a real
  regression (e.g. a file table's markup changing while section shape stays
  identical) would not necessarily move this fingerprint at all.

**Recommendation: (B), with (C) as a cheap independent addition later, not a
substitute.** The failure mode Question 39 exists to catch - OPAL silently
changing shape with nothing positioned to notice - is exactly the class of
risk this project has treated as unacceptable everywhere else it has found it
(the whole Questions 17/19/22/25 Wicket chain, the fileChanged nil-guard trap,
the manifest key migration). A monthly cost of one extra crawl on a pass that
already runs unattended and already reads `docs/BACKLOG.md` is cheap insurance
against a systemic blind spot that (A) accepts indefinitely. (C) is worth
having too, since it is free, but it should not be sold as covering the same
ground as an actual independent-path comparison - it can be its own small
follow-up once B (or a decision against B) is settled. Left for the
maintainer to choose between, since (B) is a real scope change to what the
weekly-review pass does and (A) is a real acceptance of standing risk - both
are judgment calls, not something to implement speculatively.

### 1. What is OPAL actually rendering? — now read up, see below
~~OpenOLAT is open source. This campaign spent ten days guessing at the live
server what it does.~~ Answered 2026-07-31, see "Next experiment" below for the
evidence. Short version: there is **no** marker, because **nothing is rendered
client-side that would have to finish** — tree and file table are pure server
HTML. That opens Question 7.

### 2. ~~Why was HTTP empty on 2 of 6 courses?~~ Answered 2026-08-09 (autopilot, pure re-analysis of data already on disk, no live run) — it was never about building-block type; the abandoned crawler never reached those courses' file sections at all
**"Some building blocks render server-side, some client-side" was the wrong axis.** That
theory was already narrowed 2026-07-31 (campaign doc, "the HTTP-first rejection
re-diagnosed"): re-fetching one Softwaretechnologie section's *own* URL over plain
HTTP found the files sitting in the raw response as `data-file-name` attributes —
`looksLikeFileLink` just hadn't been taught to look for them yet. So a section's leaf
content, once you have its URL, was never the problem. What was never re-examined is
*how the abandoned 2026-07-21 implementation got its URLs in the first place* — and
that is where the two zero/near-zero courses (Softwaretechnologie 0/206, TUDMATH NuMa
0/17) actually broke, distinct from the pagination-cap gap (43 files) the 2026-07-31
retest found and fixed separately.

**The mechanism, sourced rather than guessed:** Question 9 already proved
`MenuTreeRenderer.isRenderChildren()` (OpenOLAT source) returns a tree fragment
containing only currently-*open* nodes plus the path to whatever is currently
selected — never the whole course tree in one response. A node becomes visible in a
response only once its ancestor chain has actually been navigated/opened; there is no
single URL that returns a complete tree. `internal/scraper/httpdiscovery.go`'s own
design comment (written the same day as the original rejection, `339dd23`) states the
consequence plainly: *"OPAL renders the course-content navigation TREE client-side (a
browser has to walk it), but renders leaf file tables SERVER-SIDE."* "Client-side"
there means "requires sequential navigation to enumerate," not "needs JS to render" —
consistent with, not contradicting, Question 1's finding that individual pages are
pure server HTML with no client-side markup to wait on.

The 22s the abandoned implementation achieved is the tell: that is in the same range
as a *pure per-section HTTP fetch with no tree navigation at all* (49.7s for 275
sections, measured the same day). An implementation that fast could not have been
paying for `isRenderChildren()`'s one-navigation-per-newly-opened-branch cost — so it
must have started from whatever section URLs a shallow seed fetch already exposed and
never discovered anything nested under a currently-closed branch. For a course whose
graded/file content sits behind branches not open by default, that predicts exactly
zero — TUDMATH NuMa (13 sections, 17 files, all missed) and effectively all of
Softwaretechnologie (206 files, only recovered to 158/200 once the 2026-07-31 retest
supplied section URLs a different way). The three courses that came back perfect
(2026 LA20, Algorithmen und Datenstrukturen, Analysis) are the ones whose content
apparently already sits on default-open tree paths.

**Corroborated by the surviving design, not just inferred.** The only HTTP-discovery
code ever committed (`httpdiscovery.go`) was built around exactly this finding —
"browser enumerates section URLs first, HTTP fetches their file tables afterwards" —
and was verified byte-for-byte correct (diff = 0) against all 6 real courses on
2026-07-31, the same 6 courses named above. A design that never HTTP-walks the tree
cannot hit the failure mode described here, and measurement confirms it doesn't.

**Honest residual (rule 2):** the abandoned 2026-07-21 implementation was never
committed — confirmed via `git log --diff-filter=D` on every path with "http" in its
name, nothing found — so "it derived its URL set from a shallow seed fetch" is the
best-fitting inference from the surviving numbers and design comments, not a
code-level proof of that specific historical bug. It does not affect the current
(parked, verify-mode-only) design's correctness, which is independently verified.

### 29. ~~Does the browser crawler's own tree walk ever re-fetch a node's page more than once?~~ Closed 2026-08-10 (autopilot, pure source reading, no live run needed) — no, by construction

**Opened by Question 2's close:** now that `isRenderChildren()`'s open-node/
selection-path scoping is established as the reason a full tree enumeration
costs one navigation per newly-revealed branch, does the crawler's own BFS
ever re-fetch a node's page more than once — once per child discovered
under it?

**Answered by rereading the two functions that actually gate the BFS, not
by running anything.** `collectCourseFiles` (`crawl.go:122-137`) only ever
pops and visits a URL after checking `visited[currentKey]` — a duplicate
pop is dropped before the section is ever navigated to. The other half,
`appendSectionFolderTargets` (`crawl.go:1324-1333`), is the only place new
children get queued, and it checks *both* `visited[key]` and `queued[key]`
before appending — a child already visited, or already sitting in the queue
from some other section's candidates linking to the same node, is silently
dropped rather than queued a second time. Between the two, a node's
`sectionKey` can enter the queue at most once and be navigated to at most
once, for the lifetime of one course's crawl — this is not a measured
property, it is what the code does on every path, so Rule 2's bar (a
mechanism sharp enough to have predicted the answer) is met without a live
run.

**Honest residual, not chased further:** this proves no re-fetch *given*
`sectionKey` correctly treats two URLs that refer to the same underlying
OPAL node as equal. `sectionKey` (`crawl.go:1391+`) already does real
normalization work for `/CourseNode/` URLs (extracting the ID and a
lower-cased, percent-decoded suffix via `courseNodeSectionKeyRe`) — if some
OPAL URL variant for the same node fell outside what that regex + suffix
handling normalizes, the dedup could miss it and this question's answer
would flip for that specific URL shape. No such variant is known to exist
today, and none of this campaign's byte-diffs (345/349-file ground truth,
many runs) have ever shown a duplicate-content symptom that would indicate
one. Not worth its own live-instrumented run: a synthetic re-fetch would
have to be manufactured to test it, and the real-world signal that would
reveal a gap (an unexpectedly high section-visit count against a known
section total) has never fired in any of this campaign's real-account runs.
Low-priority residual, not a reopening.

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

**First experiment, 2026-08-12 (autopilot): the discovery line is waiting
(Question 39 blocked on the maintainer, Question 41 closed with nothing else
runnable), so this question's first pass is in scope per the paragraph above.**

*Prediction, registered from what this cycle already knew before opening
`orchestrator.go`, not from a blank guess - friction-campaign Walk 3 (same
day, `docs/friction-campaign.md`) had just live-measured a real `list` run
sitting silent for 2m44s during discovery and traced the proximate cause
(`publishProgress` fires once, CLI never subscribes). The open question this
cycle picked up from there: is that silence forced by the crawl's own
architecture (batches every course's result until the whole crawl ends), or
is there already an internal per-course completion point nothing happens to
be wired to? Predicted: the latter - `collectCourseFilesConcurrently`'s
worker-pool shape (a `resultCh` drained one item at a time) has to know each
course's result as soon as that course's own goroutine finishes, because
`onResult` already does per-course work (merging download candidates) at
that exact point. Counts as wrong if the function turns out to buffer
all results and only invoke `onResult`/return once every course is done.

**Result: confirmed by source reading, no live run needed for the diagnosis
itself.** `orchestrator.go:644-654`'s result-draining loop calls
`timing.PrintProfileLine` (profile-gated) and `onResult` once per course, the
instant that course's worker sends to `resultCh` - the completion point
already exists and already fires per-course. Nothing about "partial results
during the run" needs new architecture; it needs an existing, correct signal
wired to something the user without `--profile` actually sees. Fixed the same
cycle: `timing.PrintCourseProgress` (new, always-on, no `Profile` gate) is now
called from that same loop, printing `  <course>: <n> files (<elapsed>)` as
each course finishes - covers both `list` and `sync`, both the browser and
HTTP-first discovery paths, since both go through this one shared function.
Live-verified against the real account: `list --config config.yaml` now
prints a line per course as discovery proceeds instead of the previous
2m44s silent stretch. New `TestPrintCourseProgress_AlwaysOn`
(`internal/timing/timing_test.go`) pins the always-on behavior so a future
`--profile`-gating "cleanup" can't quietly regress it. Full suite green.

**What this does and does not close.** This is the cheap half of Question 5
- streaming already-available per-course results as they land, which existed
as an architectural fact this cycle just had to surface. It does not touch
the two harder halves the question names: **a background run started before
the user clicks anything** (needs a decision about *when* to trigger one
without surprising the user or burning quota unasked - a product question,
not a code one), and **partial/incremental results shown while a run is still
in flight for the GUI specifically** (this fix is CLI-only; the GUI has its
own separate `jobEvent`-based progress mechanism, and Walk 3's own open
question #2 - does the GUI already stream per-course during
`scrapeCoursesHTTPFirst` or does it share this exact gap under a different
surface - is still unanswered and is the natural next cycle for this thread).
Question 5 stays open, re-ranked: the CLI-silence half is done, the GUI half
and the background-run half remain.

**Second experiment, same day (autopilot): Walk 3's open question #2, and a
correction to the first experiment's own framing.** Picked up immediately -
still nothing else unblocked on the ranked list, same condition as above.

*Prediction, registered before opening `internal/gui/sync.go`.* Two
sub-questions in one pass, cheap because both are source reading: (1) does
the GUI's `sync` job (the primary "Sync now" button) already stream per-course
progress during discovery, or does it share the gap the CLI just had; (2)
does the GUI's `list`-only job (`/sync?list_only=1`) share it. Predicted,
from the shared-function argument the first experiment already made: neither
should, because `SyncCoursesWithProgress`/`ListAvailableCourses`-equivalent
GUI code presumably calls the same `collectCourseFilesConcurrently` this
cycle just instrumented, so the printf fix should already cover them
transitively via stdout... except the GUI doesn't read its own child's
stdout, it drives everything through a `jobEvent`/SSE channel that has
nothing to do with `timing.PrintCourseProgress`. So the real prediction:
**both GUI paths are independent of the CLI fix and need checking separately
from scratch**, with no confident guess yet on which (if either) already
works.

**Result: split - `sync` already worked, `list`-only did not, and the first
experiment's own framing was imprecise about *why*.** `internal/syncer.SyncCoursesWithProgress`
(`internal/syncer/syncer.go:446`) already calls `scraper.SetDiscoveryProgress`
- a public, pre-existing hook on `*OpalScraper` (`internal/scraper/progress.go`)
that both `newCourseFileCollector` (browser path) and `newHTTPCourseFileCollector`
(HTTP-first path) already call at `PhaseCourseStarted`/`PhaseCourseDone`,
symmetrically, for every course, in both discovery modes. The GUI's `sync` job
(`internal/gui/sync.go`'s `progress` closure, kind `jobKindSync`) already
subscribes to it via `EventDiscovery`, and already renders it live over SSE -
this was never silent. **Correction to the first experiment's own claim:**
"nothing was surfacing [the per-course completion point] to a user" was true
for the CLI specifically, but imprecise about the *mechanism* - a whole,
designed-for-this `DiscoveryProgress`/`SetDiscoveryProgress` event system
already existed and was already wired into the GUI's `sync` path; the CLI
just never called `SetDiscoveryProgress` at all (confirmed: zero references
in `cmd/opal-downloader/root.go`). `timing.PrintCourseProgress` is a second,
parallel signal for the same event, not a replacement for a total absence -
both now coexist, which is mild redundancy worth naming rather than hiding.
The GUI's `list`-only job (`runJob`'s `jobKindList` branch), by contrast,
never called `SetDiscoveryProgress` either - it called
`sc.ScrapeWithSavedSession` directly and only published courses after the
full crawl returned, exactly the CLI's old shape. **Fixed the same cycle:**
the list branch now registers `sc.SetDiscoveryProgress` before scraping,
publishing one `jobEvent{Kind: "log", Course: ..., Message: "N files"}` per
`PhaseCourseDone` - the same wording the old post-hoc loop used, just live
instead of batched. Live-verified end to end in a real headless browser
against the real account (`TestLiveListCoursesInBrowser`,
`OPAL_GUI_LIVE_LIST=1`): course rows now appear in the SSE log as discovery
proceeds rather than all at once at the end.

**Both halves of Walk 3's open question #2 are now closed.** The GUI does
not share the CLI's gap for its primary action (`sync`); it did share an
equivalent gap for its secondary one (`list`-only), now fixed the same way.
Question 5's remaining open half is exactly one thing now: **a background run
started before the user clicks anything** - a product decision (when, and
whether it would surprise a user or spend quota unasked), not a code
experiment, and the natural place to pick this question back up once it
reaches the top of the ranked list again.

**Third pass, same day (autopilot): a blocked question with no alternatives
is itself unblocked work, so turned into concrete options rather than left as
an open question with nothing to run.** The model's own "when ideas run out"
move applies directly here: *"ask which constraint is negotiable - as options
to the maintainer, not as an open question."*

The background-run half isn't actually unaddressed today - it's just not
framed as one. `OpalDownloaderScheduledSync` already runs a full sync
automatically (daily + on-logon catch-up, shipped by friction-campaign walk
1's Finding 1 repair), which *is* a background run before any click. The real
question is narrower than "should a background run exist" - it's **"should
the GUI's landing page lean on that existing background run harder, so a
click that's usually a no-op *feels* instant because the work already
happened"**, versus building a second, independent background-run mechanism
specifically for GUI-open time.

- **(A) Do nothing further.** The scheduled sync already runs in the
  background; the landing page already shows when it last succeeded. Leaves
  the "feels like one click" goal exactly where friction-campaign walk 1's
  Finding 1/5 left it - accurate, but not leaned on for perceived speed.
- **(B) Lean on the existing scheduled run harder in the UI, no new trigger.**
  When the landing page's last-run staleness signal says the scheduled sync
  already succeeded recently (say, within the last hour), change the primary
  button's copy/state to something like "Up to date as of \<time\> - Sync now
  to check again" instead of a flat "Sync now" that implies work is about to
  start. Zero new network activity, zero new surprise - purely a framing
  change riding data the app already has (`internal/statuslog`). Cheapest
  option that directly targets "feels like one click" for the common case
  (scheduled sync already ran today).
- **(C) A genuine background run triggered by the GUI opening**, independent
  of the daily schedule - e.g. a low-priority `list` kicked off on `gui`
  startup so results are ready before the user reaches for the button. Closer
  to what "background run" evokes literally, but a real behavior change:
  spends network/quota on every GUI open whether or not the user was going to
  sync, needs its own opt-out, and interacts with `sync.lock` (a manual click
  during the background run would need the existing "already running, follow
  along" handling walk 1 already found missing for the primary button once
  before). Most work, most user-visible change, no measurement taken yet on
  whether GUI opens are frequent enough for this to matter.

**Recommendation: (B).** It is the smallest change that actually addresses
what Question 5 asked (perceived speed, not discovery speed), spends nothing
new, and reuses infrastructure this project already built and already trusts
(the scheduled-sync staleness signal). (C) is not rejected, just bigger than
this question has evidence to justify yet - worth reopening if (B) ships and
still doesn't move the "feels like one click" needle, at which point actual
GUI-open frequency would be worth measuring first. Needs the maintainer's
pick, not further research - written up in `docs/BACKLOG.md`.

### 6. ~~Why does 1 in 12 sections stay unstable across runs?~~ Closed 2026-08-09 (autopilot, pure re-analysis of data already on disk, no live run) — stale premise, superseded by the campaign's own later correction before this question was ever copied into this file
**The "1 in 12" figure was already retracted three days before this file existed.**
It comes from the 2026-07-27 change-detection-cache reopening (`docs/sync-speed-campaign.md`):
fetching each of 12 sections' URLs *twice, back to back*, and normalising known-volatile
Wicket bookkeeping (page-version counters, generated component ids, table-widget instance
counters) made 11/12 byte-identical, leaving one unexplained outlier.

**The 2026-07-30 entry, same file, next section down, named exactly why that number cannot
mean what Question 6 assumes:** "same URL fetched twice back to back" is not the condition
a real sync runs under — the condition that matters is *a hash stored during one crawl
against a hash from the next crawl*, and that comparison had already been measured, in the
2026-07-21 entry the 07-27 reopening was supposed to be improving on: **1 of 276** (0.4%),
not 11 of 12 (92%). The reopening's own words, quoted directly in the 07-30 entry: "a
stability result measured in one condition says nothing about the condition the feature
runs in." Question 6 restates the 07-27 number as an open mystery without carrying forward
the 07-30 line that already explained why it was the wrong number to be asking about.

**What actually distinguishes real (consequential) instability from cosmetic instability was
answered separately, by a different thread, without ever being connected back here.**
Correctness held in every one of the cache experiments' live runs (345 files, empty diff,
every time) — so the pervasive byte-level "instability" the 12-section probe was chasing is
Wicket session bookkeeping churn that never touches file content, already fully catalogued
(the three normalisation patterns above) and inconsequential. The instability that *is*
consequential — real files going missing across runs — is a separate, already-named,
already-live-tested mechanism: Questions 17/19/22/25's Wicket "show all" pagination bug,
which fires intermittently even without concurrency (Question 17's own baseline-2 run, at
the unchanged 500ms/6000ms settings, lost the same six files a contention run did). Question
6's guess that its outlier "is possibly the same cause as Question 17" was on the right
track, but the two threads never needed reconciling by name, because the 12-section
byte-hash outlier and the file-loss mechanism are different classes of "unstable" — one
cosmetic (Wicket ids), one real (a failed AJAX expansion) — and only the second one costs
files or matters to a user.

**Nothing left to run.** The feature this question was diagnostic for
(`internal/sectionhash`, `htmlstability_probe_test.go`) was deleted outright on 2026-07-31
(`docs/BACKLOG-archive.md`, "Deleted the section change-detection cache, budget and all") —
there is no surviving code path this question's answer would change. Closed as stale rather
than merged, since — unlike Question 4 into Question 7 — the two threads answer genuinely
different questions, not the same one twice.

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

### 7. ~~If nothing renders client-side — what fills the 336ms then?~~ Answered 2026-08-06 (autopilot, pure re-analysis of data already on disk, no live run) — none of the three candidates are dominant; it is mostly the crawler's own wait constants, not the browser doing anything
**Closed without a new run, because the evidence to close it was already collected for
other questions and never connected back to this one.**

- **Candidate A** (network/transfer time) was already refuted live 2026-08-01 — bytes
  grow only 1.4x with 27x more sections, network share stays at 25–31%.
- **Candidate B** (browser still parsing/laying out a large static HTML document) and
  **Candidate C** (a narrowly bounded JS widget) both predict that *some* form of
  browser-side work — parsing, layout, style recalc, or script — should dominate the
  remaining 69–75%. Question 13's CDP `Performance.getMetrics()` probe (2026-08-02,
  live, real account) measured exactly that bucket directly: Script+Layout+RecalcStyle
  at **11.4%** aggregate (14.5% on the slowest section), and even Chrome's broader
  `TaskDuration` metric — a superset that also counts GC, parsing, and paint/compositing
  prep, i.e. Candidate B's mechanism specifically — comes in at **24.4%**, flagged in
  the same result as more likely an *overestimate* than an underestimate (its
  measurement window spans navigation as well as settle+stable). Both candidates
  therefore have a hard ceiling around a quarter of the time, at best, on data already
  gathered to answer a different question (Question 13, not this one).
- That leaves roughly three quarters of settle+stable unaccounted for by any browser
  activity CDP can see. Question 13's own conclusion named the shape directly: measured
  mean settle time (326ms) sits within 8.7% of the `mutationObserverDebounceMs` constant
  (300ms at the time), and mean stable time (193ms) is one `sectionContentPollIntervalMs`
  cycle (150ms) plus overhead — i.e. the two windows track two fixed software timers,
  not variable render work.

**What makes this a closed mechanism rather than a restated correlation (rule 2):**
Questions 14 and 15 then tested that claim directly, not just observationally — halving
`mutationObserverDebounceMs` (300ms → 150ms, a debounce that only fires after mutations
go quiet) lost **zero files** across 8 live runs on two real courses 27x apart in
section count, while saving 28.7–29.6% of settle+stable. A debounce constant can only be
cut that hard with no data loss if the browser was already quiet well before the old
constant elapsed — i.e. the removed 150ms was margin the code was sitting out, not time
the page needed. That is the predictive test Candidates B and C fail: if either were the
real driver, cutting the constant that ships *after* the browser finishes its own work
should not have been safe, and it was, twice, on courses of very different size. The
150ms cut shipped as the default on 2026-08-03 on the strength of exactly this evidence.

**Honest residual:** this closes "is it browser work" (no, mostly not) but not "what is
the debounce constant still paying for, if not real completion." The two windows are
still there for a reason — dropping them entirely was measured and rejected early in the
campaign (51% slower with the wait removed, 40% slower even with the verdict still
asserted, see "What we know" above) — so some real signal-detection value remains, just
much less than the original 300ms/150ms assumed. **New question (rule 3), not run this
cycle:** now that the constants are understood as margin rather than measured
completion, how much further can `mutationObserverDebounceMs` go below 150ms before
correctness breaks — is there a real floor, or was 300ms simply never calibrated against
actual behaviour at all? This needs the same live 8-run byte-diff protocol Questions
14/15 already used, so it is real-account load, not free — ranked below Question 26 in
this cycle's queue for that reason, and named here so it is not rediscovered from
scratch.

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

### 26. ~~Now that Question 25 gives the context-destroyed reclick a live-tested recovery path, does Question 23's raw-CDP preview-blocking rewrite pass its own byte-diff safety bar on a retry?~~ Answered 2026-08-07 — yes, clean, and it shipped as the default

**Confirmed live, zero-diff.** Full real-account before/after
(`filelist_probe_test.go`, `tmp/q26-before-run.log` / `tmp/q26-after-run.log`):
`OPAL_FILELIST=before` (no preview blocking) found 349 files;
`OPAL_FILELIST=after OPAL_BLOCK_FILE_PREVIEWS=1` also found 349, and
`diff tmp/filelist-before.txt tmp/filelist-after.txt` was empty — no
truncation, no `warnShowAllTruncated` line in either log, including in
Part-3 of "Softwaretechnologie (SoSe 26)", the section that lost 33 files on
Question 23's first attempt. That confirms the diagnosis from Questions 17
and 25: the loss was the context-destroyed reclick failing to recover the
section, not something raw CDP's `requestPaused` concurrency adds on top —
Question 24's first alternative is refuted, its second (a distinct raw-CDP
failure mode) did not show up either, though one clean full-account pass
cannot fully rule out a rare mode the way a repeated-trial design could.

**Shipped as the default the same cycle** (`previews.go`,
`attachInlinePreviewBlocker`): the gate inverted from "blocks only if
`OPAL_BLOCK_FILE_PREVIEWS` is set" to "blocks unless
`OPAL_BLOCK_FILE_PREVIEWS=0`". This is the standing rule from the 2026-08-03
merge decision (`docs/BACKLOG.md`, "Standing work") — a default that has
passed the byte-diff may be changed and shipped, not held for a separate
sign-off round. `go build`, `go vet`, and the full non-account test suite
(`go test ./... -short`) all pass unchanged.

**Timing number is directional, not load-bearing — see Question 27.** Total
test wall-clock was 185.2s (before) vs 172.6s (after), but the before-run
paid for a fresh TU-Fast login (session had expired) that the after-run did
not; the byte-diff, not this number, is what decided the ship.

---

### 30. ~~Does OpenOLAT's unified folder browser let a nested `Ordner` course node be fetched as one ZIP instead of one page load per subfolder level?~~ Narrowed 2026-08-09, same cycle — metadata forces the discovery pass to stay unchanged, so the win is real but smaller than first framed

**Opened and partly answered 2026-08-09 (autopilot, pure source reading of
both codebases, no live run).** Originally ranked above Questions 24/29 on
the guess that this could collapse the crawl's 207s page-load floor itself.
That guess doesn't survive this project's own code, checked the same cycle
(see "Narrowed" below) — it still ranks as real, live-run-worthy work, but
below Question 24 (a correctness risk on a shipped default, per the
standing "correctness ahead of speed" rule) and roughly level with Question
29.

**The mechanism, sourced on both ends, not guessed:**

- **This project's own crawler already pays a full page load per nested
  subfolder level inside an `Ordner` course node.**
  `looksLikeSectionFolderLink` (`internal/scraper/files.go:255`) matches
  `target=fold_` / `/coursenode/` links found on a folder page and queues
  them as further sections to crawl — there is no code path today that reads
  a subfolder's contents without separately navigating to it. `node-bc` is
  this project's own confirmed CSS-class marker for a real content folder
  (`internal/scraper/section_type_test.go`, "true negative: real content
  folder, node-bc class").
- **OpenOLAT's server-side folder browser (`FolderController`, the class
  every `Ordner` course node's participant view is built from — confirmed by
  `BCCourseNodeRunController.java:167`, `folderCtrl = new
  FolderController(...)`) has a bulk "Download" action that is not gated
  behind editor rights.** `bulkDownloadButton.setVisible(!trashView)`
  (`FolderController.java:526`, no `canEditCurrentContainer` check, unlike
  the neighbouring `bulkZipButton` at line 529 which *is* editor-gated).
  Clicking it on one or more selected rows calls `doBulkDownload`
  (`FolderController.java:2656`), which for a `VFSContainer` row (i.e. a
  subfolder) wraps it in `FolderZipMediaResource` — and that class recurses
  the *entire* subtree via `ZipUtil.addToZip` into one streamed
  `application/zip` response (`FolderZipMediaResource.java:97-105`). The
  per-row participant permission check is `isItemNotAvailable(ureq, row,
  false)` (`FolderController.java:2666`), not an edit check. Even a single
  row's plain "download" link already takes this path when the row is a
  container: `doDownload` (`FolderController.java:2639-2653`) zips it the
  same way.
  Source: `gh api repos/OpenOLAT/OpenOLAT/contents/<path>` reads of
  `FolderController.java`, `FolderZipMediaResource.java`,
  `BCCourseNodeRunController.java` at `master` HEAD, 2026-08-09. (GitHub's
  `search/code` endpoint was returning `total_count: 0` for known-good
  queries all cycle — confirmed a genuine index outage, not absence, by
  fetching `MenuTreeRenderer.java` directly by path — so this cycle's
  reading used the repo's git-trees API plus direct content fetches instead
  of `gh search code`. Note for whoever runs the next source-reading cycle:
  check whether `gh search code` is back before assuming it still works the
  way the 2026-07-31/2026-08-01 entries used it.)
- **This account's course material predominantly lives in exactly this node
  type.** Already established today, independently, by the WebDAV letter
  work: "the place course material actually lives — the folder course
  elements (*Kursbausteine 'Ordner'*)" (`docs/opal-webdav-student-access.md`
  line 68). This is not a marginal case for this account.

**Narrowed, same cycle, no live run needed: metadata for nested files is only
ever seen by visiting each subfolder, so discovery cannot shrink.**
`parseRowSizeBytes`/`parseRowModified` (`internal/scraper/files.go:178-219`,
called from `appendSectionFiles`) parse a file's size/modified date from
`rowText` — the DOM row of *whatever page is currently loaded*
(`extractSectionContentCandidates`, `files.go:18-96`, scopes `rowText` to
the row's own closest table/list ancestor on the live page). Nothing in
`internal/scraper/discovery.go` or `crawl.go` sources this metadata any
other way — grepped for `parseRowSizeBytes`/`parseRowModified`/`Modified`/
`SizeBytes` outside `files.go` itself: no hits. A parent `Ordner` page's row
for a subfolder link carries only the subfolder's *own* row text (name,
maybe an item count) — it cannot carry the modified dates of files that
haven't been rendered yet, because OPAL never sends them until that
subfolder's own page is requested (the same `MenuTreeRenderer` "only
currently-open nodes" behaviour Question 1/9 already established for the
course tree applies identically here — a folder page is exactly this kind
of Wicket-rendered tree fragment). So **every subfolder level still needs
its own page load for discovery/change-detection, zip or no zip** — the
207s crawl floor (page loads + settle waits) is untouched by this lever.

**What survives:** only the *download* step for files already known to have
changed can move to a single bulk-ZIP request per top-level `Ordner`,
instead of one `getPolitely` fetch per file. That is exactly the class of
cost `docs/server-load.md` already named and explicitly declined to
optimize for a routine sync ("Files are fetched only when they have
actually changed, which on a routine sync is almost none of them") — the
real payoff, if any, is on a *first* sync, against the same ~86s floor that
document already reasoned about and marked "deliberately not measured."
Bulk-ZIP could plausibly cut a meaningful fraction of that 86s (one request
instead of N queued-behind-the-rate-limiter fetches), but that is a bounded,
one-time cost this project has already decided not to chase, not the
recurring 207s this campaign exists to shrink.

**Kill criterion, revised:** worth a live-run cycle only once Questions 17
(Candidate B, unfixed) and 24 (its residual risk on the shipped default) are
resolved, since correctness ranks first by the standing rule. When picked
up, the live check needed is narrow: does `bulkDownloadButton` actually
render for a participant on a real `node-bc` section, and is the resulting
first-sync-download-time saving big enough to bother with, given it can
only ever address the 86s floor, not the 207s one. If the account's
`Ordner` sections turn out mostly flat (0-1 nested levels) that shrinks the
one-time saving further and may not be worth the Playwright bulk-select
automation it would take to claim it.

---

### 31. ~~Now that Question 25's fix has 6 clean single-threaded trials behind it, does it also eliminate the `course_concurrency>1` contention-condition failure rate?~~ Answered 2026-08-09: yes, at full 349-file scale — but concurrency alone is not faster, only no-longer-lossy; the speed case moved to Question 32

**Opened 2026-08-09 (autopilot), by Question 24's closure.** Every past
rejection of `course_concurrency>1` (Question 16's 6-file loss, Questions
17/20/21's ~33–50% failure rate under contention) predates Question 25's
fix (2026-08-06): rearming the Wicket watch and waiting on its own signal
when a reclick follows a `context-destroyed` failure. That fix has now been
tested clean 6 times serially (Question 24, this cycle) plus twice at full
6-course scale (Questions 26/27) — but never once *under the actual
contention condition it was designed to survive*. If contention's failure
mode was always downstream of the same context-destroyed-reclick gap
Question 25 closed, re-enabling `course_concurrency>1` might now be safe,
which would be a genuine, previously-closed speed lever (concurrent course
crawling), not just a correctness fix.

**Prediction, written before running (2026-08-09), per Rule 1.** Design:
reuse `TestDebounceOverrideUnderContention`
(`internal/scraper/debouncecontention_probe_test.go`) exactly as it already
exists — no new code. It already targets the right course pair (small
"Algorithmen und Datenstrukturen" + large "Softwaretechnologie (SoSe 26)",
the same pair Question 16's original 9-file loss and Question 17's 6-file
loss both used), already sets `course_concurrency=2`, and already runs 4
matched trials internally (`base1`, `base2` at today's shipped defaults;
`over1`, `over2` at a 150ms-debounce override) via
`collectCourseFilesConcurrently`, diffing every pair's file set against
every other. One invocation therefore already delivers the "3-4 run
repeated batch" this question needs, with no code changes and no new probe
to write. Command: `OPAL_DEBOUNCE_CONTENTION_TRACE=1 go test
./internal/scraper/ -run TestDebounceOverrideUnderContention -v -count=1
-timeout 60m` (`-count=1` is now mandatory per Question 24's finding above).

*Expected numbers:* Questions 16/17's own archived rate for this exact pair
under `course_concurrency=2` was consistent file loss (9 files, then 6),
not a coin-flip — closer to "every contention run before Question 25 lost
something" than a genuine 33–50%; the ~33–50% figure belongs to a different
section (the Wicket signal-timeout condition, Questions 19–21), reused here
only as an order-of-magnitude reference, not the same measurement. If
Question 25's fix generalizes, predict all 4 of this probe's runs
(`baseSelfDiff`, `overSelfDiff`, `crossDiff`) come back empty — the probe's
own `VERDICT` line already says exactly this in its source.

**Kill criterion:** any non-empty diff in any of the three comparisons
(`baseSelfDiff`, `overSelfDiff`, `crossDiff`) — even one file — closes this
as "contention is a distinct mechanism, not fixed by Question 25" and
`course_concurrency` stays at its default of 1. A fully clean run does not
by itself prove safety (a single 4-trial batch is still a small sample
against a bug that was never claimed to fire every single time) but would
be strong enough to justify a larger byte-diff-verified batch before
considering a default change — any default change still needs the full
345-file byte-diff bar per the standing shipping rule, not just a clean
file count from this narrower two-course probe.

**Running this now, same session** — the maintainer's rationing retirement
(see "Next experiment" above) applies here too, and the experiment is
already built and cheap (one `go test` invocation, no new code).

**Result: clean.** All 4 runs found the same 248 files
(`tmp/q31-contention-run.log`) — `baseSelfDiff`, `overSelfDiff`, and
`crossDiff` all empty, matching the predicted "all empty" branch exactly.
`baseline-1` logged one transient `net::ERR_NETWORK_IO_SUSPENDED` navigation
warning (a section retried and, per the final matching count, still landed
in the file set — a local network hiccup, not a Wicket/OPAL signal issue;
noted, not chased, since it did not cost a file). Timing is a second,
unplanned finding: `override` (150ms debounce) averaged 100.7s wall clock
against `baseline`'s 668.2s for the same 2-course, concurrency=2 crawl — a
~85% reduction, and the 49.6% settle+stable-time saving Questions 14/15
measured serially now replicates under real contention too.

**This is strong evidence at 2-course/248-file scale, but not yet the
project's own non-negotiable bar.** `docs/BACKLOG.md`'s "Non-negotiable"
section requires the byte-for-byte diff against the full 345-file ground
truth for anything touching discovery — this probe covers only 2 of the 6
configured courses. Rather than stop on a promising-but-partial result,
added a minimal, reusable override (`OPAL_COURSE_CONCURRENCY_OVERRIDE`,
`internal/scraper/filelist_probe_test.go` — no other code touched, `go
build`/`go vet` clean) so `TestFileListSnapshot` can run the full account at
`course_concurrency=2` without ever editing the maintainer's real
`config.yaml` (which stays at its shipped default of 1 throughout).

**Second prediction, written before running, per Rule 1:** two full-account
`TestFileListSnapshot` runs, `OPAL_COURSE_CONCURRENCY_OVERRIDE=1` then `=2`,
diffed. Expect identical file sets at whatever count the account currently
has (349 in Questions 26/27's most recent snapshots) — a direct extension of
this cycle's 2-course result to the full 6-course/345-ground-truth scale.
**Counts as refuted by any diff at all**, in which case `course_concurrency`
stays at 1 and this closes as "safe at 2 courses, not at 6" rather than a
blanket reopening. **Counts as strong (not yet final) support for
reconsidering the default** if the diff is empty — final because a single
run at each concurrency is still one sample, not the repeated-trial
standard this cycle otherwise used, and because shipping a default change
needs the maintainer's own sign-off regardless of the data, per the standing
correctness-first rule.

**Result: empty diff, 349 files both sides
(`tmp/filelist-conc1.txt`/`filelist-conc2.txt`, `diff` output empty).**
`course_concurrency=1`: 349 files, 286 sections, 171.94s. `course_concurrency=2`:
349 files, same 286 sections, 201.79s. The correctness half of the
prediction holds cleanly at full scale — Question 25's fix generalizes past
the one course pair Questions 16/17 originally broke, and `Ordner`
sections/nested folders across all 6 courses came back identical.

**But the timing half needed a correction, caught immediately by reading
the run's own numbers rather than declaring victory (Rule 2 against this
cycle's own earlier framing):** `course_concurrency=2` was not faster here —
it was 17% *slower* (201.79s vs 171.94s), and its own settle-wait average
jumped to 531ms (71% of section time) against 179ms (48%) at concurrency=1.
This is not a new finding — it matches what this project already knew and
had filed as unresolved (`docs/BACKLOG.md`, "Concurrency REOPENED": the
2026-07-26 remeasurement found `course_concurrency=2` "lost 9 files and was
no faster"). What changed today is only the *first* half of that sentence:
concurrency alone still isn't faster, but at 349/349 files it no longer
loses any. **The 85% wall-clock reduction this cycle's earlier 2-course
probe found came from pairing concurrency=2 with the 150ms debounce
override together — not from concurrency by itself** — and that pairing has
not yet been run at full 6-course scale. Reopening "`course_concurrency>1`
is a speed lever" would be overclaiming from this result alone; what this
result actually reopens is narrower and still real: **the correctness
objection to `course_concurrency>1` is refuted at full scale**, and the
speed case now rests entirely on whether the concurrency+debounce
combination — not concurrency alone — replicates its 2-course win at 349
files. That is a new, distinct, well-motivated question (31 only tested
correctness; the speed pairing is untested at this scale), opened below per
Rule 3.

### 32. Does `course_concurrency=2` combined with the 150ms debounce override replicate its ~85% wall-clock win at full 6-course/349-file scale, and does that combination pass the full byte-diff?

**Opened 2026-08-09, by Question 31's full-scale result.**

**Mechanism, sharpened 2026-08-10 (autopilot) by rereading `navigation.go`
rather than assuming Question 31's own framing — this is why concurrency
alone was slower, not just that it was:** `contentSettleWaitBudget`
(`navigation.go:416`) already ships `mutationObserverDebounceMs = 150.0` as
the `course_concurrency=1` default (lowered from 300 on 2026-08-03, Question
14/15's own win) — so Question 31's "conc=1" baseline (171.94s, 349 files)
already includes the 150ms debounce; there was never a slower serial
baseline still on 300ms to compare against. But at `course_concurrency>1`
the same function silently switches to
`mutationObserverConcurrentDebounceMs/-HardCapMs = 500.0/6000.0`
(`navigation.go:156-157`) — a >3x wider settle-and-cap budget, unconditional,
with no override needed to trigger it. `OPAL_DEBOUNCE_MS_OVERRIDE` is the
only path back down to 150ms once concurrency is on, and it forces the
*serial* hard cap (4000ms) too, not the wider concurrent one
(`navigation.go:426-430`). So Question 31's "concurrency alone, 17% slower"
result is explained without needing a new mechanism: two courses ran in
parallel, but each individual section inside them waited over 3x longer to
be called settled, and that per-section tax outweighed the parallelism gain
at 6-course scale. Question 32 is therefore not a fresh unknown so much as
completing an equation whose other three terms are already measured
(serial debounce win: Questions 14/15; concurrency's correctness: Question
31; concurrency's default-budget cost: Question 31 again) — the only untested
term is whether combining them holds together at full scale rather than just
at the 2-course pair Question 31 tried it on first.

**Prediction, written before running (2026-08-10), per Rule 1.** Design: two
fresh same-session `TestFileListSnapshot` runs (not reusing yesterday's
`tmp/filelist-conc1.txt` — the account is student-facing course content that
can change day to day, e.g. a new upload, and a stale baseline would
confound a byte-diff whose entire job is catching exactly that kind of
silent difference):
- `OPAL_FILELIST=q32-conc1 OPAL_COURSE_CONCURRENCY_OVERRIDE=1` — same
  conditions as Question 31's already-shipped default, refreshed today as
  this cycle's baseline.
- `OPAL_FILELIST=q32-conc2-deb150 OPAL_COURSE_CONCURRENCY_OVERRIDE=2
  OPAL_DEBOUNCE_MS_OVERRIDE=150` — the untested combination.

Both via `go test ./internal/scraper/ -run TestFileListSnapshot -v -count=1
-timeout 30m` (`-count=1` mandatory per Question 24's caching-hazard finding).

*Expected numbers:* file sets identical between the two runs (whatever the
account's current total is — 349 at last count, may differ slightly today,
which is exactly why a fresh baseline matters more than the absolute
number). Wall-clock: the conc=1 baseline should land close to Question 31's
171.94s (same config, one day later). The combo run's mechanism predicts it
should beat that baseline by a real margin — reclaiming the >3x per-section
settle-time tax the concurrent-default budget was paying in Question 31's
"17% slower" result — but not by the 2-course pair's ~85%, because that
figure came from a small course paired with the account's single largest
course and is not representative of the full 6-course mix. A defensible
range: 90-150s (roughly a 15-45% improvement on 171.94s), with anything
outside that range still reported honestly rather than rounded to fit.

**Kill criterion:** any non-empty diff between the two file sets closes the
*correctness* half as failed — `course_concurrency` stays at 1 regardless of
timing, no matter how large a speed win the same run shows, per the
standing correctness-first rule. If the diff is empty but the combo run is
not meaningfully faster than the fresh conc=1 baseline (i.e. within measurement
noise or slower, mirroring Question 31's own "17% slower" surprise), the
*speed* half closes as "the combination does not generalize past the
2-course pair" — a real, useful negative result, not a failed cycle, since
Rule 2 only requires the explanation to be sharp enough to have predicted
it, not that the prediction turn out right. A shipped default change either
way still needs the maintainer's own sign-off, per the standing rule.

**Running this now, same session** — both runs are the existing test with
existing env-var hooks, no new code, and the rationing retirement (see
"Next experiment" above) still applies.

**Result: correctness fails, exactly per the kill criterion, and the loss is
sharply diagnosable.** Fresh conc=1 baseline: 349 files, 213.7s wall
(`tmp/q32-conc1-run.log`) — this run needed a fresh login (saved session had
expired), so its wall-clock is not directly comparable to Question 31's
warm-session 171.94s; the file count is what matters here. Combo run
(`OPAL_COURSE_CONCURRENCY_OVERRIDE=2 OPAL_DEBOUNCE_MS_OVERRIDE=150`): **343
files, 6 short**, in 132.0s wall (`tmp/q32-conc2-deb150-run.log`) — and its
own log names the cause directly: `Warning: section .../Vorlesung offered a
"show all" control but the expansion did not add any files ... this section
is capped at its first page (20 files) and later files are missing`. The
byte-diff (`diff tmp/filelist-q32-conc1.txt tmp/filelist-q32-conc2-deb150.txt`)
confirms exactly 6 files missing, all `Vorlesung_7`/`_7p`/`_8`/`_8p`/`_9_10`/
`_9_10p` in Algorithmen und Datenstrukturen's Vorlesung section — the same
course, same section, same file-shaped loss Questions 16 and 17 originally
found before Question 25's fix existed.

**Diagnosed, not just observed, before writing this down (Rule 2):**
rereading `contentSettleWaitBudget` (`navigation.go:426-430`) shows
`OPAL_DEBOUNCE_MS_OVERRIDE` unconditionally returns `mutationObserverHardCapMs`
(4000ms, the *serial* hard cap) regardless of `course_concurrency` — so the
combo run gave the Wicket "show all" AJAX signal a 4000ms budget to arrive
under real 2-course contention, not the 6000ms the concurrent-default path
would have given it. That is a 33% narrower window on exactly the mechanism
this campaign has repeatedly found to be contention-sensitive (Questions
17/19/20/21: the signal doesn't arrive late under load, it sometimes doesn't
arrive at all within budget). Question 31's clean 4-trial contention probe
used this same override+concurrency=2 combination on a smaller 2-course pair
and did not lose files there — so this is not "the override is broken", it
is "a 4000ms budget is not always enough once a *third* course's worth of
rendering is competing for the same event loop," which a 2-course probe
would not reliably surface. This sharpens, rather than contradicts, Question
25's fix: the reclick-recovery path works when it gets a chance to run, but
a hard cap tight enough can end the wait before Wicket's signal — or even
the failure that would trigger a reclick — ever fires.

**Closed: the untested combination does not ship.** `course_concurrency`
stays at 1, `OPAL_DEBOUNCE_MS_OVERRIDE` stays off by default, exactly as the
kill criterion specified. The mechanism above is precise enough that it
would have predicted this outcome in advance (Rule 2) — the debounce-only
change (Questions 14/15, safe, shipped) and the hard-cap-tightening change
(new here) were never actually validated separately; Question 32 tested them
bundled and the bundle is what failed. That bundling is worth undoing rather
than treating this as a dead end — Question 33, opened below, tests the
debounce shortening alone with the wider concurrent hard cap left intact.

### 33. Does the debounce shortening alone — without also tightening the hard cap — recover Question 32's 6-course speed win while keeping the concurrent hard cap's correctness margin?

**Opened 2026-08-10 (autopilot), by Question 32's diagnosed failure.** Added
`OPAL_DEBOUNCE_MS_KEEPCAP_OVERRIDE` (`navigation.go`) — same debounce
override as `OPAL_DEBOUNCE_MS_OVERRIDE`, but preserves whichever hard cap
`course_concurrency` already selected (6000ms at concurrency>1) instead of
pinning it to the serial 4000ms value. No other code touched; `go
build`/`go vet` clean.

**Prediction, written before running (2026-08-10), per Rule 1.** Design: one
more `TestFileListSnapshot` run, `OPAL_COURSE_CONCURRENCY_OVERRIDE=2
OPAL_DEBOUNCE_MS_KEEPCAP_OVERRIDE=150`, diffed against this cycle's already-
fresh `tmp/filelist-q32-conc1.txt` baseline (349 files, same session, same
day — no staleness risk this time).

*Expected numbers:* if Question 32's diagnosis is right — that the *hard
cap*, not the debounce, is what starved the Wicket signal — this run should
come back with all 349 files (empty diff), because the section that lost
files gets its full 6000ms patience again even though sections finish
sampling faster once they do go quiet. Wall-clock: expect a smaller win than
Question 32's failed combo (132.0s), since a slow-to-settle section can still
consume up to 6000ms same as today's shipped default, but still faster than
the fresh conc=1 baseline (213.7s) on the (majority of) sections that were
never anywhere near the cap — most of the win Questions 14/15 measured was
in avoiding an oversized *debounce*, not the rarely-hit hard cap, so shaving
the debounce alone should still recover a real chunk of it.

**Kill criterion:** any non-empty diff against the 349-file baseline closes
this exactly like Question 32 — the hard cap was not the (whole) explanation,
`course_concurrency` stays at 1, and whatever is left of the mechanism goes
back to open. If the diff is empty, this becomes the first
`course_concurrency>1` configuration in the whole campaign to pass the full
byte-diff *and* show a real wall-clock improvement over the conc=1 default —
worth taking to the maintainer as a proposed default change, not shipping it
unilaterally, per the standing correctness-first/maintainer-sign-off rule.

**Running this now, same session** — one more live run, no new unknowns
introduced beyond what the prediction above already names.

**Result: clean, and repeated once given how consequential this one is.**
Run 1 (`OPAL_COURSE_CONCURRENCY_OVERRIDE=2
OPAL_DEBOUNCE_MS_KEEPCAP_OVERRIDE=150`): **349/349 files, empty diff against
this cycle's fresh conc=1 baseline**, 132.93s (`tmp/q33-conc2-keepcap150-run.log`).
Question 31's own write-up flagged that a single run at each concurrency is
"still one sample, not the repeated-trial standard" for a result this
consequential, so ran a second trial before writing this up: run 2 — again
349/349, empty diff against both the baseline and run 1
(`tmp/q33-run2.log`), 137.68s. Two clean runs, consistent ~135s average, no
variance in file count either time.

**This is the campaign's first `course_concurrency>1` configuration to pass
the full byte-diff with a real, repeated wall-clock win** — a ~36% reduction
against this cycle's own fresh conc=1 baseline (211.4s test time) and still
~21% faster than Question 31's warm-session conc=1 figure (171.94s),
despite this cycle's baseline having to pay for a fresh login. The
mechanism holds up exactly as diagnosed: decoupling the debounce shortening
from the hard-cap tightening keeps Questions 14/15's proven-safe debounce
win on the common path (most sections settle in ~180ms, nowhere near either
cap) while leaving the rare slow-to-settle Wicket "show all" section its
full 6000ms patience — the section that lost 6 files in Question 32 came
back complete in both runs here.

**Not shipped — reaches the maintainer as a decision, not a unilateral
default change, per the standing correctness-first/sign-off rule.** See
`docs/BACKLOG.md` "Now" for the concrete options.

**Leaves one open question per Rule 3:** why did Question 31's contention
probe (2-course, `OPAL_DEBOUNCE_MS_OVERRIDE=150`, same tightened 4000ms cap
Question 32 also used) come back clean 4/4 times on a *smaller* 2-course
pair, while Question 32 lost files with the same tightened cap at full
6-course scale? The working theory above is "a third course's worth of
render load was enough to push the same section past a 4000ms cap that held
at 2 courses" — plausible (more concurrent tabs competing for the same
event loop is a real mechanism, not a guess) but not directly measured; no
instrumentation from either run captured per-section wait duration to
confirm the Vorlesung section's signal actually took longer at 6-course
scale than it did in Question 31's 2-course probe. Low priority given
Question 33 already found the fix that matters (decouple the caps), but
worth naming so nobody assumes the 4000ms-cap mechanism is fully nailed
down rather than well-supported.

**Third confirmation run, prediction written before running (2026-08-10,
autopilot, new session).** `docs/BACKLOG.md`'s "Now" item recommends "at
least one more clean run, ideally on a different day" before treating this
as proven. This is not that — same calendar day as the two runs above,
just a later session with its own fresh login — recorded honestly as a
same-day third sample, not a different-day one; the different-day
confirmation the recommendation actually asked for is still open. Not
opening a new ranked question (RESUME.md's own note: no new speed question
until the maintainer decides on shipping) — this only adds evidence to
Question 33, already closed.

Design: `OPAL_COURSE_CONCURRENCY_OVERRIDE=2 OPAL_DEBOUNCE_MS_KEEPCAP_OVERRIDE=150`,
one more `TestFileListSnapshot` run (`-count=1`), diffed against
`tmp/filelist-q32-conc1.txt` (this session's own fresh 349-file conc=1
baseline — same session as runs 1/2, so still a valid comparison point,
config.yaml unchanged throughout at `course_concurrency: 1`).

*Expected:* empty diff, 349/349 files, wall-clock in the ~130-140s band the
first two runs both landed in (132.93s, 137.68s) — no reason to expect a
third run to behave differently given the mechanism (decoupled hard cap)
is a static code change, not a timing coincidence.

*Kill criterion:* any non-empty diff, or a wall-clock materially outside
that band (e.g. back near the 211s conc=1 baseline, which would suggest
the override isn't taking effect this session), reopens Question 33 rather
than just adding a third clean tally mark.

**Result: matches prediction — 349/349 files, empty diff against the
conc=1 baseline** (`tmp/filelist-q33confirm3.txt` vs
`tmp/filelist-q32-conc1.txt`, `diff` clean). This run's saved session had
expired, so `go test`'s own wall-clock (179.28s) includes a fresh
interactive login and is not directly comparable to runs 1/2's 132.93s/
137.68s (which reused a warm session, like the conc=1 baseline's 211.44s
included its own fresh login for the same reason). The crawl-only figure
the tool logs separately from login — `section timing over 286 sections:
total 2m1.03s` — is the correct like-for-like number: 121.03s, against
run 1/2's 111.5s/117.8s and the conc=1 baseline's own crawl-only 107.0s.
Squarely the same band, no regression. Raw log: `tmp/q33-confirm3-run.log`.

Three clean runs now, same mechanism each time, still all in one session
(2026-08-10). The different-day confirmation `docs/BACKLOG.md`'s "Now" item
asked for is still genuinely open — this only rules out "the first two runs
were a same-session fluke," not "this needs to be observed on a different
day's server conditions too." Not closing that ask; leaving it for whichever
session runs on 2026-08-11 or later.

**SHIPPED 2026-08-10 (maintainer's decision, decision round).** Asked whether
to ship now or after a different-day confirmation run, the maintainer chose
*sofort ausliefern* — ship immediately, without waiting. Recorded plainly: the
different-day run was the recommendation and it did not happen, so the residual
risk named above is now carried in production rather than retired. What
actually shipped, as two constants that are one change:
`config.DefaultCourseConcurrency` 1 → 2, and
`mutationObserverConcurrentDebounceMs` 500 → 150 with
`mutationObserverConcurrentHardCapMs` left at 6000 (the decoupling this
question found). `OPAL_DEBOUNCE_MS_KEEPCAP_OVERRIDE` stays for A/B work but no
longer gates the behaviour. The maintainer's own `config.yaml` had an explicit
`course_concurrency: 1` overriding the code default, so that was set to 2 too —
otherwise the decision would have changed nothing on the one account that
actually runs. Symptom to watch for if a later day disagrees: a silently short
count from one paginated section, with `warnShowAllTruncated` in the run log —
the same shape 2026-07-26 saw.

---

## Next experiment

**Updated 2026-08-12 (autopilot, fourth update same day): both ranked items
genuinely still need the maintainer, so this cycle takes the next "when ideas
run out" move instead of re-stating that.** Question 39 and Question 5 are
unchanged since the third update below - no pick has landed in
`docs/BACKLOG.md`'s Now section. Of the six fixed moves at the top of this
file, five have already been used this campaign (read the other side: OpenOLAT
source via `gh search code`, Questions 30/34; compute the ceiling: the ~93s
HTTP ceiling; change the question: Question 5 itself; ask which constraint is
negotiable: Questions 5 and 39's option-writeups; measure instead of arguing:
the whole campaign). The one never tried: **"look at how others solved the
same thing" — other OPAL/OpenOLAT/LMS downloader tools.**

**Question 42 (new). Does any existing third-party OPAL/OpenOLAT downloader or
scraper reveal a discovery mechanism faster or simpler than this project's
`initial_data`-tree-seed HTTP-first approach?**

*Prediction, written before searching, per Rule 1.* OPAL/Bildungsportal Sachsen
is a regional Saxon platform (a handful of universities), not a mass-market
LMS like Moodle or Canvas, so I expect **few or no OPAL-specific tools to
exist at all** — and any that do are more likely course-content browsers or
WebDAV clients than sync tools, since this project's own history already
found OPAL's WebDAV mount is role-gated away from students
(`docs/BACKLOG-archive.md`'s WebDAV entry), so a generic tool leaning on
WebDAV would not even clear the bar this project already cleared. For generic
OpenOLAT tools (the platform OPAL is built on, and open source, unlike OPAL's
own customizations), I expect at most a handful of small unmaintained scripts,
none discovered by this campaign's own multi-week search across
`Questions 1/2/9/30/34` — if OpenOLAT exposed a bulk/tree API meaningfully
better than `initial_data`, it is more likely this campaign would already have
tripped over it while reading `MenuTreeRenderer`/OpenOLAT source for those
questions than that a third party found it first. *Counts as a real new
lead:* a maintained tool with >10 stars, or any documented API endpoint this
project's own OpenOLAT source reading hasn't already covered. *Counts as
closed with nothing found:* zero relevant projects, or projects that exist but
don't reveal a new mechanism (e.g. browser-automation clones of what this
project already does, or WebDAV-only tools that don't apply to students).
Either outcome is a valid, useful result — "the field is empty" closes the
"when ideas run out" list's second move cleanly either way.

**Result (2026-08-12, same day): confirmed, field is empty — no new lead.**
`gh search repos` for `"OPAL bildungsportal"`, `"opal downloader"`,
`"opal scraper"`, `"opal sync"`, and `"openolat"` (30-result cap, sorted by
relevance), plus a web search, turned up exactly one project that is
genuinely OPAL/Sachsen-scoped and does file retrieval:
[`spyfly/videocampus-sachsen-downloader`](https://github.com/spyfly/videocampus-sachsen-downloader) —
a single-URL manual video downloader (paste one `.ts` stream URL you already
found by hand via the browser's Network tab) with no course/section discovery
of any kind, so nothing to compare against. Every `"openolat"` repo result is
either the LMS server codebase itself (mirrors, forks, Docker packaging) or
unrelated - zero third-party OpenOLAT course-sync/downloader tools exist.
`"opal sync"`'s one hit (`mariaannagoeschl/opal-sync`) is a same-name
collision with an unrelated Obsidian notes plugin, exactly the kind of false
positive the platform's regional niche-ness predicted. `gh search code
"initial_data" "opal"` hit a transient GitHub search-index 503 (same outage
class Question 30 hit and worked around) and was not retried, since the repo
search already answered the actual question - no tool exists to search the
code *of*.

**Diagnosis, since the result matched the prediction exactly:** the mechanism
predicted this correctly - OPAL's own regional scope keeps the tool-building
population too small for a competing implementation to exist, and this
campaign's own multi-week OpenOLAT source reading (Questions 1/2/9/30/34) was
always going to out-pace a hypothetical third party for the same reason a
third party doesn't exist to begin with. **Closes the "when ideas run out"
list's second move with nothing found.** No new question opened by this one
specifically - the honest next move, per the list's own remaining item order,
is back to the maintainer for Question 39/Question 5, or the *first* move
("read the other side") going deeper than the manuals-plus-`gh search code`
pass already done, if a future cycle wants to push further rather than wait.

---

**Superseded by the above - Updated 2026-08-12 (autopilot, third update same
day): the ranked list has
no unblocked experiment left - both items now have written-up options and
need the maintainer's pick, not more research.** Question 5's remaining half
(background run before the click) turned out not to be a code question at
all: the "when ideas run out" move ("ask which constraint is negotiable, as
options to the maintainer") applied directly, and closed it the same way
Question 39 was already closed - three options (do nothing / lean harder on
the UI messaging around the scheduled sync that already exists / build a
genuine GUI-open-triggered background run) with a recommendation, written up
in Question 5's own entry and `docs/BACKLOG.md`. **A future cycle should
check `docs/BACKLOG.md`'s Now section for a pick on either Question 39 or
Question 5 before assuming there's nothing to do** - if neither has moved,
say so plainly rather than opening a new question to stay busy.

---

**Superseded by the above - Updated 2026-08-12 (autopilot, second update same
day): Question 5's second experiment closed both halves of Walk 3's open
question #2** - see Question 5's own entry above for the full result. The
GUI's `sync` job already streamed per-course progress (a pre-existing
`DiscoveryProgress`/`SetDiscoveryProgress` mechanism the first experiment's
own framing had under-credited); its `list`-only job did not and now does,
fixed and live-verified in a real browser against the real account.
**Question 5's only remaining open half is the background-run-before-the-click
question, and it is a product decision, not a code experiment** - the natural
thing to pick up once it reaches the top of the ranked list again, but not
something an autopilot cycle should just decide unilaterally. **The ranked
list is Question 39 (blocked on the maintainer), then Question 5's
background-run half (also effectively blocked, on a product decision rather
than research)** - with both effectively stalled pending the maintainer, the
next unattended cycle should say so plainly rather than manufacture a third
sub-question to stay busy.

---

**Superseded by the above - Updated 2026-08-12 (autopilot): Question 5's
first experiment closed (the cheap half only) - see Question 5's own entry
above for the full result.** Source reading found `collectCourseFilesConcurrently`
already has a per-course completion point nothing was wired to;
`timing.PrintCourseProgress` now fires from it, live-verified fixing the
exact silent 2m44s stretch friction-campaign Walk 3 measured the same day.
Question 5 stays open for its two harder halves (background run before the
click; the GUI's own progress stream, unchecked - Walk 3's open question
#2). **The ranked list is still Question 39 (blocked on the maintainer),
then Question 5's remaining two halves** - the GUI-progress half is the
cheaper of the two to pick up next (source reading again,
`internal/gui/sync.go`, before any live run), the background-run half needs
a product decision first (when does a background run trigger without
surprising the user or spending quota unasked), closer in kind to Question
39 than to a code experiment.

---

**Superseded by the above - Updated 2026-08-11 (autopilot, third update same
day): Question 41 closed - the second confirming run lost 6 files (one
paginated section), overturning the first run's clean result and confirming
the shared-`APIRequestContext` hazard the question existed to rule out. No
production impact (the override was never wired to a default); nothing
further planned on that thread. The ranked list is Question 39, then Question
5 - and Question 39 is Blocked on the maintainer's pick among three
already-written-up options, so there is no unblocked live-run experiment left
on this list right now.** Question 5 (is "30s" even tied to discovery, i.e.
background runs/partial results) has no registered concrete experiment - it
was explicitly kept low-ranked by the maintainer's 2026-08-03 decision, and
picking it up means designing the first experiment for it, not running an
existing one.

**Updated 2026-08-11 (autopilot, second update same day, superseded by the
above): Question 40's live run landed - empty diff, 349/349 files,
concurrency=2 on `scrapeCoursesHTTPFirst` cut discovery from 56.7s to 41.6s,
squarely inside the predicted 35-45s window. Not promoted to default (one
run; the project's own bar for this kind of change is two, per Step B2's
2026-08-10 practice). The ranked list is Question 39, then Question 41, then
Question 5.** **Question 39** (is HTTP-first's correctness still
cross-validated by anything now that it's the default) still ranks first per
the standing correctness-before-speed rule - it is a process/product
question, not a live-run one, so whoever picks it up should bring options,
not run an experiment. **Question 41** (does a second confirming run, on a
different day, also produce an empty diff at concurrency=2 - and if so, is
promoting `OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE=2` to the shipped default,
alongside the still-open `course_concurrency` unification, in scope for that
cycle to decide) is the speed line's new head, replacing Question 40 now
that its one open sub-question is the repeat-on-a-different-day gate rather
than the mechanism itself. Question 5 remains lowest (declined pivot,
2026-08-03).

---

**Superseded by the above, kept for the record - Updated 2026-08-10 (autopilot, later the same day): Question 36 (both
steps) and Question 37 are closed; the ranked list is Question 38, then
Question 35, then Question 5.** Step B2 (the production HTTP-first
restructure) is written, unit-tested, and live-verified at zero diff
(349/349 files, two runs) - it lives on PR #134
(`http-first-discovery-b2`), not master, per `CLAUDE.md`'s rule for the
three discovery paths that have silently lost files before, so it is a
maintainer decision now rather than an open question. Question 37 (does
`initial_data`'s node type let more sections be skipped without a fetch)
closed the same day: five more classes came back 0-files-ever in the one
course with a saved dump, but the project's own admission bar needs
cross-course confirmation this data doesn't have, and Step B2 shrank the
payoff to a few seconds anyway - not worth chasing further. **Question 38**
(why an HTTP fetch measured ~208ms this run against 315ms on 2026-07-31) is
next: cheap (re-run Step B1's probe on a different day, it already records
its own timing) and the more interesting number now, since every HTTP-first
timing projection - including Step B2's own result - depends on which of the
two is real. **Question 35** (`course_concurrency=3` sweep) is blocked until
PR #134 (Step B2) gets the maintainer's look, per that PR's own recommendation
against spending live runs tuning a browser path B2 may retire. (This
corrects a same-commit inconsistency: Question 38's own entry above says
"re-rank below Question 35," this paragraph said the opposite order - moot
either way since 35 cannot run before 134 is resolved, but 38 then 35 then 5
is the order that doesn't contradict itself.) Question 5 remains lowest
(declined pivot, 2026-08-03).

**No local-only ranked question left; the campaign's live-run arm just
closed with the biggest result it has produced.** Question 32
(2026-08-10, autopilot) tested `course_concurrency=2` +
`OPAL_DEBOUNCE_MS_OVERRIDE=150` together at full 6-course scale and found it
loses 6 files — diagnosed to that override unconditionally pinning the hard
cap to the serial 4000ms value even under concurrency, starving the same
contention-sensitive Wicket "show all" signal Questions 17/19/20/21 already
chased. Question 33, opened the same cycle, decoupled the two
(`OPAL_DEBOUNCE_MS_KEEPCAP_OVERRIDE`, debounce shortened, hard cap left at
the concurrent default) and got the first `course_concurrency>1`
configuration in the whole campaign to pass the full byte-diff *with* a real
speed win — 349/349 files clean twice, ~135s average against a 211s fresh
baseline (~36% faster). Question 29 closed the same cycle by source reading
(the BFS's own `visited`/`queued` dedup makes a re-fetch structurally
impossible).

**Decided and shipped 2026-08-10 (decision round): the maintainer chose to
ship Question 33's configuration immediately, without the different-day
confirmation run the recommendation asked for.** Details and the residual
risk are at the end of Question 33's entry above.

**The ranked list is no longer empty.** The same decision round added two
questions from the maintainer directly: **Question 34** (does the HTML the
crawl already receives point at content it has to navigate for — reuse, and
concealed structure in the payload) and **Question 35** (`course_concurrency=3`
parity sweep on the newly shipped budget). 34 goes first: it needs no live
account run and could change the crawl's shape rather than its constants,
whereas 35 spends real-account runs to move a constant. Question 5 also
remains (is "30s" even tied to discovery; maintainer declined the pivot
2026-08-03, stays low). Everything below this paragraph is earlier history,
read newest-relevant-first.

**Questions 2 and 6 both closed this cycle (2026-08-09, autopilot, no live run) —
see "What we don't know" above.** Question 2 had stood as the highest-ranked
genuinely-unanswered item since the 2026-08-07 report; closing it needed only
re-reading Question 1, Question 9, and `httpdiscovery.go`'s own design comment
together, never previously connected. Opened Question 29 (real-account load, waits
for the same fresh day as Question 24 below). Question 6 turned out to be a stale
premise — a number the campaign itself had already retracted (2026-07-30) before
this question was carried into this file (2026-07-31) without the retraction coming
along; its diagnostic feature (`internal/sectionhash`) no longer exists, so there is
nothing left for a future cycle to run there.

**Question 30 opened and mostly closed the same cycle (2026-08-09, autopilot,
pure source reading, no live run): the bulk-ZIP-download lever this cycle
went looking for is real but smaller than hoped.** OpenOLAT's folder browser
does expose a participant-reachable "download this subtree as one ZIP"
action (sourced in "What we don't know" above), and this account's material
does predominantly live in the course-element type that offers it — but
this project's own metadata parsing (`files.go`) only ever reads a file's
size/modified date off the page that file is rendered on, so every nested
subfolder still needs its own page load for discovery no matter what
downloads it. The lever only ever reaches the download step, bounded by the
~86s first-sync floor `docs/server-load.md` already named and declined to
optimize — not the 207s crawl floor this campaign targets. Left open,
ranked behind Question 24 (correctness on a shipped default outranks a
bounded one-time speed win, per the standing rule) and roughly level with
Question 29.

**That leaves the ranked list empty of anything both high-value and
answerable without a live run.** What remains open — Questions 5, 24, 29,
and 30 — all need either a maintainer product decision (5, already given:
stays low-priority, picked up only when the discovery line is exhausted) or
real-account load (24, 29, 30 — and 30 specifically only after 24 is
resolved, per its own kill criterion).

**Maintainer decision, 2026-08-09: the "one live experiment batch per day"
self-caution this campaign had been applying is retired.** It was never a
rule in `docs/server-load.md` itself — that file's actual mechanisms (the
`polite.Limiter` rate ceiling, `429`/`503` backoff, scattering
`scheduler.SuggestedTime` across the hour) all still apply unchanged and
still bound how hard any single run hits OPAL. What is retired is the
separate, self-imposed rationing of how many *live-run cycles this campaign
does per day* — the maintainer confirmed server load is not a concern here.
Questions 24, 29 and 30 (in that order, correctness first) may all run in
the same day going forward; no more "waits for a fresh day" gating.

**Question 24 — ~~is Question 23's loss purely downstream of Candidate B, or does raw CDP add a distinct failure mode?~~ Answered 2026-08-09: no truncation in 6 live trials, but the prediction's own reference rate was wrong, and the run uncovered a real methodology hazard.**

Preview-blocking is no longer this maintainer's own account
under an env flag — it is now every user's default. Question 26's one clean
pass, now joined by Question 27's second clean pass below, is reassuring but
still not the repeated-trial design that would separate "raw CDP amplifies
Question 17's pre-existing Candidate-B bug under load" from "fires rarely
enough not to matter in practice". Question 17's Candidate B (Wicket's "show
all" AJAX call reporting done while its DOM rows never land) is still open
and unfixed. Nothing here blocks shipping — Question 26 passed the project's
own safety bar and the maintainer's standing shipping rule — but the fix for
Candidate B now matters to every installation, not just a retest condition,
and should not sit indefinitely behind lower-ranked questions.

**Prediction, written before running (2026-08-09), per Rule 1.** Design:
paired matched-condition runs of `TestPreviewBlockShowAllRegression`
(`internal/scraper/previewblockshowall_probe_test.go`), single-course
(`"Softwaretechnologie (SoSe 26)"` only, not the full 6-course account —
this probe already scopes to it), alternating `OPAL_BLOCK_FILE_PREVIEWS=1`
(blocking on, today's shipped default) and `OPAL_BLOCK_FILE_PREVIEWS=0`
(blocking off, the pre-Question-26 baseline), 3 runs each side, reading the
probe's own `truncatedPart3` signal (did Part-3's "show all" expansion add
zero files) each time.

*Mechanism suspected:* `previews.go`'s own doc comment already flags its
`Fetch.requestPaused` handling as reentrancy-sensitive — one goroutine per
paused request. Part-3 is the section with by far the most inline-preview
files (30+ PDF variants), so its "show all" click coincides with a burst of
near-simultaneous paused requests specifically when blocking is on. If that
burst interferes with how Chrome applies the Wicket AJAX response's DOM
patch, blocking-on should truncate Part-3 *more often* than blocking-off,
on top of whatever rate Candidate B already causes by itself.

*Expected numbers:* Candidate B's own baseline rate on this exact section,
from the archived Question 17/19/20 data this file already cites, is
~33–50% independent of preview-blocking. Blocking-on predicted to land
visibly above that band (call it >60% failure, i.e. 2 or 3 of 3 truncated);
blocking-off predicted to land inside the archived band (roughly 1 of 3,
maybe 2).

*Counts as refuted (closes this question toward "no distinct raw-CDP
effect")* if both conditions' rates fall in the same ~33–50% band within
the noise a 3-run-per-side sample allows — that would mean Question 26's
one earlier clean pass generalizes, and the loss really is purely
downstream of the pre-existing Candidate-B bug, not amplified by raw CDP.
*Counts as confirmed* if blocking-on's rate is clearly higher across all 3
pairs, in which case the mitigation `previews.go`'s doc comment already
named (bounding concurrent `requestPaused` handling) moves from
"plausible, not worth building yet" to worth building.

**Mid-run methodology finding, worth recording before the result: `go test`
silently cached and replayed a live-account trial instead of re-running
it.** Run 2 of the blocking-on batch, invoked identically to run 1 (same
env vars, no `-count=1`), came back in the exact same wall-clock time
(107.20s) with a byte-for-byte identical log — `go test`'s own package-level
test-result cache treats identical inputs (source + env vars actually read
via `os.Getenv`) as a cache hit and skips execution entirely, printing
`ok ... (cached)` instead of really invoking Chrome/OPAL again. This is
different from Question 28's finding (build/cache-*staleness* adding
latency) — this is the test simply **not running a second time**, silently,
with no visual difference in the pass output beyond the `(cached)` marker on
the summary line. Confirmed by `diff`: the two logs differed only in that
one line. Every subsequent run in this batch used `-count=1` to force real
execution, verified by distinct timestamps and distinct per-run timing in
each log. **This is a real risk to any past or future repeated-trial design
in this campaign that invokes the identical condition back-to-back without
`-count=1`** — Questions 20 and 21's "3 clean runs in a row" batches fit
that exact shape (same env override, repeated invocation) and were run
before this hazard was known; their raw logs (`tmp/showall-signal-timeout-probe.txt`,
`tmp/signal-latency-probe.log`) no longer exist to audit for `(cached)`
markers, so this cannot be resolved retroactively — recorded here as an
open integrity caveat on those two closed questions, not a re-opening,
since no evidence either confirms or refutes it for them specifically.
**Going forward, `-count=1` is now required for any repeated-trial live-run
design in this campaign.**

**Result (6 live trials, all independently verified non-cached): 0 of 6
reproduced the Part-3 truncation — 0/3 with blocking on, 0/3 with blocking
off.** All 6 found 210 files, `expansionSignalled=true` every time
(signal latency 243–295ms, well inside budget), `truncatedPart3=false`
every time. Raw logs: `tmp/q24-on-run{1,2,3}.log`, `tmp/q24-off-run{1,2,3}.log`.

**The prediction failed on its own written numbers, and the gap is
explainable, not a mystery (Rule 2):** the ~33–50% reference rate the
prediction borrowed from Questions 17/19/20 belongs to a *different*
condition than this probe runs under. `TestPreviewBlockShowAllRegression`'s
own doc comment (written 2026-08-05, re-read only now) already says so:
"course_concurrency=1 and section_concurrency=1 in config.yaml, so this is
NOT the known course/section-contention loss mode." Questions 17/19/20's
33–50% figure was measured specifically under `course_concurrency>1`
contention — a setting this project rejected and shipped off by default.
This prediction copied that number across to a single-course, no-contention
condition without checking whether the mechanism it describes even applies
there, which is exactly the kind of unchecked reuse Rule 2 exists to catch.
The correct comparison class is Question 23's own single prior data point at
these exact settings (2026-08-05, blocking on: 33 files lost) — one data
point, now followed by 3 clean ones under the same condition.

**What this does answer, honestly stated:** Question 25's reclick-recovery
fix (landed 2026-08-06, one day *after* Question 23's loss) already turned
Question 26 into one clean pass (2026-08-07) and Question 27 into a second
(2026-08-09) under a full 6-course crawl; this cycle adds a genuine
repeated-trial design — the thing Question 24 said was missing — narrowly on
the one section that has ever lost files, and finds no loss in 6 tries, half
of them with blocking off as a same-day control. That raises confidence the
fix generalizes and that raw CDP is not adding a distinct failure mode at
today's shipped settings (course/section concurrency both 1) beyond what
Question 26/27 already showed. It does **not** touch the separate,
still-real concurrency-contention condition (Questions 17/20/21's ~33–50%),
which stays open and stays excluded by the existing `course_concurrency=1`
default — this cycle says nothing new about it either way.

**Closing per Rule 2:** Question 24's original two-way split (a: purely
downstream of Candidate B, vs b: raw CDP adds its own failure mode) is
answered **(a)**, with the caveat that "purely downstream" now specifically
means "downstream of Candidate B *before* the Question 25 fix" — post-fix,
neither condition shows the failure at all in this batch. Closed. Candidate
B itself, under concurrency, remains open and unfixed — separately tracked,
not resolved by this cycle.

**Ranked low, a methodology fix rather than a live question (opened by
Question 28, closed below):** would standardizing future sync-speed timing
protocols on a precompiled binary (removing `go test`'s several-seconds-scale
build/cache-staleness noise at the root, instead of decomposing around it
after the fact) meaningfully change any past percentage figure if that
experiment were rerun? Not worth a dedicated real-account cycle on its own —
the honest answer from Question 28 is "probably not enough to matter" — but
worth adopting as the default protocol the next time a genuinely close call
(a delta under ~5%) needs deciding.

---

## Previous experiment (Question 28, closed 2026-08-09)

**Question 28 — does `time go test` wall-clock noise invalidate the small
deltas this campaign has been reading off it?** Opened by Question 27's own
result (above): decomposing its 4.03% total wall-clock delta against the
audit log's own section-timing total showed only 1.14% of that delta lives
inside the crawl itself — the other 6.0s of 7.25s unaccounted for by
anything the crawler logs.

**The literal prediction failed, on its own written criterion.** Precompiled
(`go test -c`) binary, invoked directly three times back to back, no code
change, no OPAL account touched (`-test.run TestFileListSnapshot` with
`OPAL_FILELIST` unset, so the test skips in <1ms): 1.181s, 0.033s, 0.034s.
That is nowhere near the 6.0s gap Question 27 needed explained, and clears
the written failure bar (`<~2s variance`) — raw binary process spin-up is
not the source.

**But that same run pointed straight at the actual mechanism, one layer up.**
The precompiled-binary test isolates the binary's own startup by construction
— it removes exactly the step (`go test`'s own build-graph staleness check)
that was the actual suspect. A direct follow-up measured that step in
isolation: `go test ./internal/scraper/ -run TestFileListSnapshot -v`, same
skip-fast condition, twice back to back —

| Invocation | `time` (real) | `go test`'s own reported time |
|---|---:|---:|
| 1st (cache cold from the `go build ./...` + `go vet ./...` gate run earlier this cycle) | 4.151s | 2.130s |
| 2nd (immediately after, nothing changed) | 0.550s | `(cached)` |

A 3.6s gap between two back-to-back invocations of the identical test,
touching no network, no OPAL account, and skipping before any real test code
runs — from cache/build-graph state alone. That is the same order of
magnitude as Question 27's unaccounted 6.0s, and a sharper fit than the
original "compile/process noise" framing: it is specifically `go test`'s
package-staleness scan and cache-key bookkeeping, not the binary's own
process spin-up (already cleared above) and not raw compilation (the 2nd
run's `(cached)` result means it did not even re-execute the test binary).

**What this settles, and what it doesn't.** It settles the shape of the
answer to the original question: a several-second, run-to-run wall-clock
swing with zero relation to previews-blocking or crawl work is a real,
demonstrated property of this project's own `go test`-based timing harness,
not a hypothetical. It does not pin down *why* the 1st invocation above was
cache-cold (most likely: the `go build ./...`/`go vet ./...` gate this same
cycle ran moments before it, touching the same package graph) or whether
Question 27's own before/after gap had exactly this cause rather than a
same-order-of-magnitude coincidence — the two runs in that experiment were
~3 minutes apart with a live crawl in between, not back-to-back, so cache
state at each invocation was not controlled for or logged.

**Consequence for the campaign's own numbers, applying Rule 2 against our own
work:** every past "X% wall-clock" figure in this file that came from
`time go test` rather than the crawler's own section-timing total (Question
26's 6.8%, Question 27's 4.03%) carries an unquantified few-seconds-scale
error bar from this source. None of them are large enough deltas to flip a
conclusion — Question 26's ship decision rested on the byte-diff, not the
timing number, exactly as its own writeup already said — but the
section-timing total (the audit log line, not `time`) is the more
trustworthy number for any future close call, and future timing protocols
should prefer it or use a precompiled binary directly.

---

## Previous experiment (Question 27, closed 2026-08-09)

**Question 27 — with a warm session on both sides, does previews-blocking's
wall-clock saving clear Question 8's ~3% fetch-only prediction, or land
closer to the ~30 MB/course bandwidth story with little time effect?**
Warmed the session first with a standalone `login` run (see below — it
independently reproduced the tracked 300s Shibboleth timeout, then succeeded
on retry in 5.95s, confirming the profile was left clean and authenticated).
Both timed passes then ran back to back, same warm session,
`filelist_probe_test.go` (`OPAL_FILELIST=before OPAL_BLOCK_FILE_PREVIEWS=0`
then `OPAL_FILELIST=after`), `tmp/q27-before-run.log` / `tmp/q27-after-run.log`.

**Result: prediction confirmed, and the decomposition sharpened it further
than the top-line number alone would.**

| | before (previews off) | after (previews on, default) |
|---|---:|---:|
| Files | 349 | 349 |
| `diff` | — | empty |
| `warnShowAllTruncated` | none | none |
| Total wall-clock (`time`) | 180.135s | 172.884s |
| Section-timing total (audit log) | 106.802s | 105.584s |

Total wall-clock delta: **7.251s, 4.03%** — well clear of the failure
criterion (anywhere near Question 26's 6.8%) and inside "a few percent" of
Question 8's ~3% prediction. Zero-diff, zero-truncation, same as Question 26
— a second clean full-account pass on the correctness question Question 24
still tracks separately.

**The sharper finding: only 1.14% of that 4.03% lives inside the crawl
itself.** The section-timing total the crawler logs on every run — the same
number Questions 7, 13-15 already used as the load-bearing metric, unlike
the `time`-wrapped wall-clock this file has otherwise reported — moved by
only 1.218s (106.802s → 105.584s, 1.14%), a fifth of the total delta. The
remaining 6.033s is not accounted for by discovery time either (3.4s → 3.2s,
noise-sized). That leaves the majority of this cycle's headline number
sitting in `go test`'s own per-invocation overhead (compile, process
spin-up) — never separately measured until this cycle needed to explain a
gap between two numbers that should have moved together and did not.

**What that means for Question 8's transfer:** the number that actually
compares to Question 8's local-probe prediction (~3% fetch-only tax,
cache-off dominant at 60.7%) is the in-crawl 1.14%, not the top-line 4.03% —
and 1.14% is, if anything, *closer* to "close to free" than the prediction
itself called for. The local probe's ~3% was itself an upper-bound-flavoured
estimate (Question 8's own header numbers), so a full-account number landing
below it is consistent, not surprising.

**Honest residual, why this isn't fully closed on the letter of Rule 2:** the
6.033s gap has one plausible mechanism (compile/process noise) but no
confirming measurement yet — a `go test -c` precompiled-binary back-to-back
comparison would either confirm it as noise or reopen it as something real.
That is Question 28, below, and it is explicitly a methodology question, not
a previews-blocking one: it also puts a caveat on every past "X% wall-clock"
number in this file that came from `time go test` rather than the audit log's
own section-timing total, including Question 26's own 6.8%.

**Also recorded, not a Question 27 finding but discovered while running
it:** warming the session hit the tracked "one unexplained 300s login
timeout" (`docs/BACKLOG.md`, Noticed) a third time — this occurrence, for
the first time, showed *where* the page was stuck (Shibboleth's own
`shiblogin` URL, before ever reaching OPAL), narrowing the suspect list to
TU-Fast/Shibboleth rather than this project's own post-login code, which was
never reached. Full write-up and the immediate-retry evidence (5.95s,
ruling out a standing block) in `docs/BACKLOG.md`.

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

### 2026-08-12 (autopilot): eight cycles since the last report, Questions 36-B2/37/38/40/41/5(×3)/39/42 — keep going, but the campaign has hit its first pure-maintainer-decision wall

Cycles since the last report (2026-08-10, second run): Question 36 Step B2
(the production HTTP-first restructure, written/tested/live-verified at zero
diff, shipped as the default), Question 37 (closed, not worth chasing further),
Question 38 (closed, confirmed the ~208ms HTTP-fetch timing that let B2 ship),
Question 40 (one clean live run at `course_concurrency=2`, not promoted - the
project's own bar needs two), Question 41 (closed as a **no-go**: the second
confirming run lost 6 files, overturning Question 40's clean result and
killing course-level HTTP concurrency as a direction), Question 5's three
sub-cycles (CLI's silent 3-minute discovery gap fixed; the GUI `list` job's
matching gap fixed; the remaining "background run before the click" half
turned into three written-up options rather than decided unilaterally),
Question 39 (opened, also turned into three written-up options), and Question
42 (closed this cycle - see above - no third-party OPAL/OpenOLAT tool exists
to borrow an idea from).

**What is known now that was not eight cycles ago.** The campaign's biggest
structural bet paid off and shipped: HTTP-first discovery is now the
production default, cutting the discovery floor from the ~207s browser crawl
to the 56-90s range depending on concurrency, at zero correctness cost on
every byte-diff run this campaign has thrown at it. Course-level concurrency
as a further lever on top of that is now **closed, not just paused** -
Question 41's second run losing 6 files is the second time this exact class
of bug (contention corrupting a shared browser context) has been caught by
this project's own two-confirming-runs bar, which is doing its job. Both of
Question 5's cheap halves (CLI and GUI silent-discovery gaps) are fixed and
live-verified. Question 42 confirms there is no external prior art this
campaign has been missing - the field is empty for the reason predicted
(regional platform, small population).

**What is still open.** Exactly two items, and both are now explicitly stalled
on the maintainer rather than on more research: Question 39 (should anything
periodically re-verify HTTP-first now that shipping it as default silently
removed the free comparison every run used to get) and Question 5's last half
(does "feels like one click" need a background run before the click). Both
have three written-up options and a recommendation each in `docs/BACKLOG.md`'s
Now section, unpicked as of this report. **No unblocked live-run or
source-reading experiment remains on the ranked list** - this is the first
time in the campaign's life that the blocker is a decision, not a question.

**Recommendation: keep going as a standing capability, but the honest next
move for *this specific campaign* needs roughly ten minutes of the
maintainer's attention on Questions 39 and 5, not another autopilot cycle.**
Manufacturing a third sub-question to stay busy was explicitly rejected
several updates in a row in "Next experiment" above, for good reason - the
two blocked items already have recommendations attached, and inventing a
third path around them would spend effort avoiding a decision rather than
reaching one. The next autopilot cycle that finds Questions 39/5 still
unpicked should say so plainly (as several already have) rather than repeat
the search; if a cycle wants genuinely new ground instead of waiting, "When
ideas run out"'s first move (read the other side, deeper than the manuals-
plus-`gh search code` pass already done) is the one honest option left,
and it is a bigger undertaking than a single cycle, not a quick check.

### 2026-08-10 (autopilot, second run of the day): five cycles, Questions 34/36-A/36-A2/36-B1×2 — keep going; the browser has been shown unnecessary for discovery

Cycles since the last report: Question 34 (answered from saved HTML — the
pre-registered prediction failed), Question 36 Step A (parser, offline,
confirmed), Step A2 (live, 6 courses, confirmed), Step B1 run 1 (live, failed
at 4 missing, diagnosed), Step B1 run 2 (live, confirmed and closed).

**What is known now that was not five cycles ago.** The premise the whole
crawl rests on is false. Since 2026-07-21 this project has believed a browser
is required to enumerate a course's section tree — `httpdiscovery.go`'s design
comment said so, Questions 2 and 9 supported it from OpenOLAT's
`MenuTreeRenderer.isRenderChildren()`, and `scrapeCoursesHybrid` runs the full
207s browser crawl *in every mode* because of it. `isRenderChildren()` scopes
the **rendered DOM**. The **response** carries `var initial_data=[...]`, the
complete course-node tree, in every course page. Measured, not argued: 261 of
261 visited course-node URLs across all 6 courses, from 6 HTTP requests.
Then Step B1 closed the remaining 7%: seeding from that tree and expanding with
the crawl's own predicates over plain HTTP reproduces **286 of 286 sections,
0 missing**, in 71.4s against the same run's 173.8s browser crawl.

Two mechanisms were added to the model on the way. Pagination is a **discovery**
boundary, not only a file-listing one — the rows past a section's ~20-row cap
include sub-sections, which is what run 1's four missing files were, diagnosed
from dumps already on disk rather than by another run. And the 21 "extra"
sections HTTP-first finds are the enrollment/forum/root nodes the browser skips,
because `isNonFileSectionType` lives inside `appendSectionFolderTargets`, which
a tree seed bypasses.

**What is still open.** Step B2, the production restructure, which is the only
reason any of this matters and is now the campaign's top item — with the
honest note that it touches the code path that has silently lost files twice,
so it is byte-diff-gated and PR-gated per `CLAUDE.md`. Question 38 (an HTTP
section fetch measured ~208–228ms here against 315ms on 2026-07-31, and every
floor projection uses that constant: ~58s vs ~88s for this account). Question
37, Question 34's untouched reuse half. Question 35 (`course_concurrency=3`)
is unchanged but its value has dropped — it tunes the browser crawl that Step
B2 would largely replace; recommendation in `docs/BACKLOG.md`.

**Recommendation: keep going, and spend the next cycles on Step B2 rather than
on any timing lever.** This is the first time the campaign has had a change
that alters the crawl's shape instead of its constants, it is measured rather
than argued at every step, and the 30s target is reachable from a ~71s
discovery floor in a way it never was from 207s. The competing item
(concurrency 3) optimises the thing Step B2 replaces, and running it first
would spend live runs on a path that may be about to become dead code.

### 2026-08-10 (autopilot): five cycles, Questions 24/31/32/33/29 — keep going, and this round produced the campaign's biggest single result

Cycles since the last report: Question 24 (closed live, 6 trials, 0
truncated, surfaced the `go test -count=1` caching hazard), Question 31
(closed live at full 6-course scale: `course_concurrency>1`'s correctness
objection refuted, but concurrency alone is 17% slower with the concurrent-
default 500ms/6000ms settle budget), Question 32 (closed live: the untested
`course_concurrency=2` + `OPAL_DEBOUNCE_MS_OVERRIDE=150` combination loses 6
files at full scale — diagnosed to that override unconditionally tightening
the Wicket "show all" signal's hard cap to the serial 4000ms value even
under concurrency), Question 33 (closed live: a new decoupled override,
`OPAL_DEBOUNCE_MS_KEEPCAP_OVERRIDE`, keeps the concurrent 6000ms hard cap
while still shortening the debounce — 349/349 files clean across two runs,
~36% faster than the fresh conc=1 baseline), and Question 29 (closed by
source reading, no live run: the crawl's own `visited`/`queued` maps make a
node re-fetch structurally impossible).

**What changed since the last report.** This round did what the previous
one predicted it might not be able to — the ranked list had gone genuinely
empty of local-only questions, and every remaining one needed a live run.
Four live crawls against the real account (Questions 24's 6 trials plus
Question 31's 4+2 plus Question 32's 2 plus Question 33's 2 — 14 live runs
total this round, the campaign's heaviest single reporting period) produced
the sharpest chain of the whole campaign: each question's result directly
motivated and de-risked the next one, from "does the old fix survive
contention" (31) through "why isn't concurrency alone faster" (32's
diagnosis) to "fix the actual bug, not just avoid it" (33). The result is
the first `course_concurrency>1` configuration ever to pass this project's
own non-negotiable full byte-diff *and* show a real, twice-confirmed
wall-clock win (~36%) — bigger than any single lever this campaign has
found since the 150ms debounce change itself. It is not yet shipped; it
reaches the maintainer as a two-option decision in `docs/BACKLOG.md` "Now"
per the standing correctness-first/sign-off rule, with a recommendation
(ship after one more day's confirmation run) rather than an open question.

**What is still open.** The ranked list in "What we don't know" is now
empty except Question 5 (concealment-class work; maintainer already
declined to pivot to it 2026-08-03, stays low). Two known residuals, both
explicitly low-priority and not reopenings: Question 33's own note that
Question 31's 2-course contention probe didn't reproduce Question 32's
6-file loss at the same tightened hard cap (plausible mechanism: a third
course's render load pushing the same section past 4000ms, not directly
instrumented); and Question 29's residual that its "no re-fetch" proof
depends on `sectionKey` correctly normalizing every real OPAL URL variant
for the same node, untested beyond the campaign's own byte-diffs never
showing a duplicate-content symptom. Neither blocks anything currently
queued. `docs/BACKLOG.md`'s session-lock collision bug and the login-timeout
recurrence are both still open, unchanged this round.

**Recommendation: keep going, but the next cycle should be the maintainer's
call, not another autopilot experiment.** Five questions closed, the ranked
list is empty for the first time with nothing waiting on a live run either,
and the one open item left (ship the concurrency+debounce default) is
explicitly the maintainer's decision per the standing rule — manufacturing
another speed question right now would be working around that decision
point rather than reaching it. If the maintainer approves shipping, the
natural next autopilot cycle is the one more same-day-or-later confirmation
run the recommendation calls for, then wiring the override into the
non-test code path. If they decline, the ranked list is genuinely empty and
"When ideas run out" (top of this file) is the honest next move.

**Answered same day (decision round, 2026-08-10).** The maintainer approved
shipping and skipped the confirmation run, so the override is wired into the
non-test path now (Question 33's closing note). He also set the direction
this report said was his to set, and it is not another timing lever: the
correctness thread first (`docs/BACKLOG.md`'s session-lock collision bug is
now the top item), then Question 34 (read the HTML the crawl already gets —
"When ideas run out"'s *read the other side* move, applied to the payload
rather than the manuals) and Question 35 (concurrency 3). So this campaign
continues, but no longer as the only thread.

### 2026-08-09 (autopilot): five cycles, Questions 27/28/2/6/30 — keep going, but the ranked list is now genuinely empty without a live run

Cycles since the last report: Question 27 (warm-session timing, confirmed),
Question 28 (`go test` cache-staleness noise, opened by 27 and closed the
same window), Question 2 (HTTP-discovery's 2-of-6-courses gap, closed by
connecting three already-written findings), Question 6 (closed as a stale
premise the campaign had already retracted before this file existed), and
Question 30 (opened and mostly closed: OpenOLAT's participant-reachable
bulk-ZIP folder download, real but bounded to the ~86s first-sync floor,
not the 207s crawl floor).

**What changed since the last report.** No new shipped default this round —
unlike the 17→26 chain, this was a documentation-debt-clearing and
diminishing-returns round. Question 27 confirmed the warm-session prediction
(4.03% total wall-clock delta) but decomposed it: only 1.14% lives inside
the crawl, the rest is `go test`'s own build/cache-staleness bookkeeping,
which Question 28 then pinned down precisely (a 3.6s gap between cache-cold
and cached invocations of the identical test, unrelated to previews or crawl
work). Net effect: every past "X% wall-clock" figure in this file that came
from `time go test` rather than the crawler's own section-timing total now
carries an acknowledged few-seconds error bar — none large enough to flip a
past conclusion, but the audit-log total is now the preferred number for any
future close call. Questions 2 and 6 were both closed by re-reading data
already on disk, no live run for either — 2 by finally connecting Question
1's tree-rendering finding to `httpdiscovery.go`'s own design comment (the
abandoned HTTP-first crawler never walked the tree at all, so it could only
ever see default-open branches, exactly the two courses that came back
empty), 6 by noticing the campaign had already retracted its own premise
three days before this file was created. Question 30 was this cycle's one
genuine new-ground attempt — reading OpenOLAT's own source (via the GitHub
git-trees/contents API, after discovering `gh search code` is currently
returning empty results for known-good queries, apparently a search-index
outage rather than an absence) found a real, participant-reachable
bulk-ZIP-download feature in the folder browser every `Ordner` course node
uses. It looked like a possible attack on the 207s crawl floor itself; this
project's own `files.go` metadata parsing (size/modified date read only off
whatever page is currently rendered, confirmed by grep, no other code path)
closed that hope the same cycle — nested-folder discovery still needs one
page load per level regardless of how the eventual download happens, so the
lever is real but bounded to the same ~86s first-sync floor
`docs/server-load.md` already named and declined to optimize.

**What is still open.** The ranked list (`docs/sync-speed-model.md`,
"What we don't know") now holds only Questions 5 (maintainer decision
already given, stays low), 24, 29, and 30 — all three of the latter need
real-account load, and 30 additionally waits on 24 by its own kill
criterion (correctness before a bounded speed win). No local-only,
unanswered question remains on the ranked list for the first time since the
model file was introduced 2026-07-31. The session-lock collision bug
(`docs/BACKLOG.md` Noticed) and the login-timeout recurrence are both still
open but neither produced new evidence this round.

**Recommendation: keep going.** Five questions closed or narrowed, zero
regressions, every closure left a new question exactly per Rule 3 — the
method is still working. This report is also the first to say plainly that
local-only source-reading has run out for now: the three remaining open
questions all need a live run. Previously that would have meant stopping
here for the day — the maintainer retired that self-imposed rationing this
same session (server load was never actually the constraint; see the note
above "Next experiment"), so this cycle proceeds straight into Question 24's
live run rather than waiting. See the entry below (or `docs/RESUME.md`) for
the outcome.

### 2026-08-07 (autopilot): five cycles, Questions 22 (2nd)/25/7/26 — keep going, the correctness thread paid off in a shipped default

Cycles since the last report: Question 22's second cycle, Question 25, Question 7,
and Question 26 — four questions across five cycles (Question 22 spanned two: an
inconclusive first pass, then a confirmed second one).

**What changed since the last report.** The correctness thread the previous report
flagged as "keep going" closed cleanly: Question 22 confirmed the Vorlesung-loss
wait fails with `context-destroyed`, not a genuine timeout; Question 25 then showed
that rearming the Wicket watch and awaiting its own signal on the reclick recovers
the section, 3/3 live. That fix is what let Question 26 retest Question 23's
shelved raw-CDP preview-blocking rewrite — Question 23 had refused itself in
2026-08-05 on a 33-file loss in exactly the section Question 25 now fixes. Question
26 came back clean (349 files, zero-line diff, no truncation anywhere) and
**shipped as the default the same cycle** — `OPAL_BLOCK_FILE_PREVIEWS` flipped from
opt-in to opt-out, per the 2026-08-03 standing rule that a byte-diff-proven default
may ship directly. That is the campaign's second user-visible win (after the 150ms
debounce) and the first one that traces back through four chained questions
(17 → 22 → 25 → 26) rather than a single measurement. Question 7 also closed this
window, for free — re-reading data already on disk from Questions 13-15 settled
what fills the settle wait, no live run needed.

**What is still open.** Question 27 (does previews-blocking's real timing saving
match Question 8's ~3% local prediction, once the fresh-login confound in
Question 26's own before-run is removed) is queued and cheap. The session-lock
collision bug (`docs/BACKLOG.md` Noticed) got a source-reading pass this cycle
that ruled out two of its three original candidates and named a better-fitting
one — still unfixed, still not urgent, but it now matters to every installation
rather than one test account, because Question 26 just shipped more real load
onto the exact mechanism (Wicket "show all" expansion) the still-open Question
17 Candidate B already knows is load-sensitive. Question 2 (why HTTP discovery
was empty on 2/6 courses) remains the highest-ranked genuinely-unanswered item
on the list, and has had no cycle spent on it since Question 1 was read in
2026-07-31.

**Recommendation: keep going.** Four questions closed, one shipped default, zero
regressions (build/vet/full test suite all pass), and every closure left the
required new open question. The chain from Question 17 to Question 26 is exactly
the "understand why, not just try-drop-try" method the campaign was reopened to
fix on 2026-07-31 — it took eight days end to end, across five reports now, but
it produced a real, verified win rather than another abandoned lever.

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
