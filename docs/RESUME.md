# Resume note

Scratch state for work that is **in flight right now**. Kept in git so it
survives a killed turn, a dead session, and a fresh clone.

`docs/BACKLOG.md` says what should happen and stays tidy. This file is allowed
to be messy: it is the thought that would otherwise only exist in a context
window, and a context window does not survive the usage limit being hit
mid-turn.

**Keep it current while working**, not at the end - the end is exactly the part
that does not always arrive. Update it whenever the answer to "what am I doing
and what's next" changes materially. When the work lands, clear it back to the
placeholder line below.

The SessionStart hook reads this file and hands it to the next session. The
scheduled resume runner also treats a non-placeholder file here as "there is
work", so leaving stale content in it will wake an unattended run for nothing.

---

Nothing in flight right now.

<!--RESUME-CONTENT-BELOW-DELETED-->
## In flight: sync-speed campaign, 30 s target (maintainer 2026-07-31)

**Goal (maintainer, verbatim):** sync in max 30 s, down from current ~200-300 s.
**Hard constraint:** no files lost. **Mode:** as autonomous as possible.

### Maintainer's corrections to the existing record (2026-07-31) — these override the campaign doc's framing
1. **"Every measurement run by hand by the maintainer" is FALSE.** TU-Fast is
   wired (`internal/scraper/tufast_transplant.go`, extension ID
   `aheogihliekaafikeepfjngfegbnimbk`) and `~/.opal_storage_state.json` holds a
   live session. An agent CAN run live tests itself, unattended. Fix the doc.
2. **Rejections were not diagnosed, only recorded.** HTTP-first discovery et al.
   were dropped on "files missing, oh well", never on "here is WHY they are
   missing and here is whether that's fixable". That is the change in standard.
   Re-open each "rejected" lead and find the actual cause, or prove it's truly
   unfixable — not "the test didn't pass".
3. **Target is SYNC 30 s**, not "crawl 30 s".

### GROUND TRUTH — measured by me, live, 2026-07-31 (not the doc's 216.6 s)
- `list --profile` against the real account via saved session:
  **200.3 s total, 345 files, 6 courses, ~280 sections.** Full output in
  `tmp/baseline/list.out`. This count (345) is the contract: any change must
  reproduce it exactly or it loses files.
- **Time correlates with SECTIONS, not files: r = 0.9998.** Regression
  `time = 0.67s × sections + 0.27s` fits all 6 courses almost perfectly
  (Algorithmen 5 sec→4.8s; Softwaretechnologie 163 sec→110.5s). Files only
  r = 0.97. **The section is the atomic unit of cost, not the file.**
- Implication: even deleting the 300ms debounce *entirely* caps at ~0.37s/section
  × 280 ≈ 115s. **Debounce tuning alone cannot reach 30s.** 30s needs the
  per-section cost to drop ~7×, which is only possible by not navigating each
  section in a browser at all — i.e. HTTP-first discovery, the "rejected" path.

### BREAKTHROUGH — the HTTP-first rejection does NOT reproduce (2026-07-31)
The doc (campaign.md L484-490) rejected HTTP-first claiming: *"OPAL renders some
course nodes server-side and others client-side... a JS-rendered section returns
144-172KB of markup with zero files."* Tested that exact claim on
**Softwaretechnologie** — the very course the doc said "came back with zero":

| SW section | HTTP probe found (existing predicate) | `data-file-name` attrs |
|---|---|---|
| Part-3 | **18 files** | 20 |
| sec 1713753392678496004 | **3 files** | 3 |
| sec 1720578755372414008 | **1 file** | 1 |

**The files ARE in the raw HTTP response** — both as `data-file-name` attrs and
as `<a>` tags the existing `looksLikeFileLink` predicate already catches. The
"144KB with zero files" state does not reproduce here. Raw dumps in
`tmp/baseline/sw-sec{1,2,3}.html`. This vindicates the maintainer's critique:
the rejection was recorded, never diagnosed.

**NOT yet proven:** completeness vs the browser. Running the decisive test now —
HTTP-probing all 164 SW sections (parallelism 1, to dodge the documented Wicket
session-serialization trap) and diffing the file set against the browser's 207.
Log: `tmp/baseline/swt-probe.log`; HTTP file set: `tmp/baseline/swt-http-files.txt`.

### DECISIVE DIAGNOSIS — the gap is pagination truncation, fully fixable (2026-07-31)
Browser ground truth (new probe `browsergroundtruth_probe_test.go`,
`OPAL_BROWSER_GROUNDTRUTH`): **SW = 200 distinct files.** HTTP probe: 158. Diff:

- In BOTH: 157. Browser-only (HTTP misses): **43.** HTTP-only (noise): 1.
- The **entire 43-file gap comes from 3 pager sections** (Part-1/2/3).
  Each returns ~20 rows over HTTP — **exactly the default page cap.**
- Part-3 raw HTML proves the mechanism: **21 `<tr>`, 20 `data-file-name` rows,
  but the HTML literally says "57 Einträge"** (57 total). OPAL tells us the full
  count over HTTP; the first page just caps at ~20.
- The other **161 non-pager sections: HTTP finds 100% of files.**

**Root cause is NAMED, not guessed: pagination truncation** — not "JS-rendered
shells" (the doc's claim). And the fix is already known: the browser clicks
"show all" for these (PR #100/#109). `pager-showall` IS in the HTML and marks
exactly the sections that lose files — the doc dismissed it as a signal because
"only 5 sections advertise it", missing that those few account for ALL the loss.

**Cleaner fix than the browser click — discovered 2026-07-31:** Part-3's HTML
wires show-all as `Wicket.Ajax.ajax({"u":"<sectionURL>?<wicket-path>-pager-
showAllLink", ...})`. The `"u"` is a **plain HTTP endpoint** (a GET to the
section URL with the Wicket behavior-path query). The browser's "click" just
fires that AJAX GET. **So HTTP can fetch the full table with one extra request
per pager section — no browser needed at all.** Untested: whether the raw AJAX
response body is the full table markup (likely) or a Wicket delta (also
parseable). This is the next experiment.

### Path to 30 s, now concrete
- Fetch every section via HTTP serially (parallelism 1): 164 sections → ~32 s
  measured. 161 non-pager sections are already complete.
- For the ~3 pager sections, fetch the `showAllLink` AJAX URL over HTTP too.
- That eliminates the browser entirely for discovery → **~32 s + discovery
  overhead, no per-section settle wait, no pagination loss.** Well under the
  60 s "shape of the fix" the doc speculated, plausibly at the 30 s target.
- Anchored to the 345-file contract: every step re-diffed against browser
  ground truth until the gap is 0.

### VERIFIED — show-all AJAX over HTTP recovers the pagination gap (2026-07-31)
`showall_probe_test.go` (`OPAL_SHOWALL_URL`): fetched Part-3's
`pager-showAllLink` Wicket-AJAX URL over plain HTTP (no browser, no click):
**status 200, 363 KB, all 57 `data-file-name` entries** — exactly the "57
Einträge" the page advertised. Recovers **33/34** of Part-3's missing files.
The 1 holdout (`31-Terminverwaltung.zip`) is genuinely absent from this
section's show-all response (0 occurrences) — likely belongs to a different
section or is a browser-only artifact. **Needs full-account verification, not
a single-section conclusion.** Dump: `tmp/baseline/part3-showall.html`.

### NEXT (the actual build)
1. Build a real HTTP-first discovery path: fetch each section via HTTP; for
   any section whose HTML contains `pager-showAllLink`, also fetch that AJAX
   URL and parse its `data-file-name` attrs (the authoritative file list,
   not just `looksLikeFileLink` which misses ~6/57).
2. Gate it behind an env flag (`OPAL_HTTP_DISCOVERY=1`) so the browser path
   stays the default until the **full 345-file contract** diffs to zero.
3. Server-load check (docs/server-load.md): parallelism 1 = same serial rate
   as today's browser crawl (~1 req/s). Two requests per pager section is
   +3 requests total — negligible. The 30 MB of file bytes fetched during
   *browser* discovery (a separate doc finding) does NOT happen over HTTP,
   so this is also a load *reduction*.

### Approach
1. Settle completeness (the experiment above). If HTTP finds all 207 SW files,
   the rejection is overturned and HTTP-first is back on the table as the path
   to ~30-60s.
2. Only then change code. Every change re-run against the 345-file contract.

### Autonomy setup
In-session cron job (rescues a still-open session whose turn was killed) AND
this file (survives reboot / 7-day expiry / fresh clone) are both kept current.
The repo's own `resume-runner.ps1` + scheduled task handle cross-session resume.

### Autonomy setup
In-session cron job (rescues a still-open session whose turn was killed) AND
this file (survives reboot / 7-day expiry / fresh clone) are both kept current.
The repo's own `resume-runner.ps1` + scheduled task handle cross-session resume.

### HONEST OBSTACLE — the section TREE is JS-rendered, not just the files (2026-07-31)
Building on the verified file-level results above, the next thing measured was
whether HTTP can enumerate a course's section TREE (the BFS walk the browser
does: root → nested folder links → recurse). The browser finds 163 SW sections
this way. Over plain HTTP:

- SW course root (RepositoryEntry/53228666883): 8 CourseNode hrefs, but **7 of
  8 point at OTHER courses** (dashboard/breadcrumb nav) — only 1 is SW's own.
- SW content entry node (CourseNode/1615865126719828011): **1 child, 0 files.**
  The browser finds 163 sections + 207 files from here.

**Diagnosed cause (named, not guessed):** OPAL's course-content navigation
tree (the left-sidebar folder structure) is rendered client-side via JS. The
HTTP response contains neither the nested section-folder links nor the leaf
file tables until the tree is *navigated* (clicked open). This is the OPPOSITE
split from the campaign doc's claim: it is not "files are JS-rendered" (they
aren't — leaf file tables are server-rendered, proven above); it is the
**section-navigation tree** that is JS-driven.

**Implication for the 30 s target:** the pure-HTTP approach (fetch every
section URL) only works if you ALREADY have the section URLs. The browser's
value isn't reading files — it's walking the tree to discover which sections
exist. So the realistic design is NOT "HTTP replaces the browser entirely":
it is **hybrid** — browser walks the tree once to enumerate section URLs
(cheap, no per-section settle wait needed for navigation), then HTTP fetches
each leaf section's file table in bulk (where the 0.67s/section × 280 =
~190s actually goes). That still attacks the dominant cost (the per-section
page-load+settle wait), but it does not eliminate the browser.

This is exactly the kind of cause-naming the maintainer asked for instead of
"it didn't work". The earlier "show-all recovers files" result still holds for
the leaf-table pagination gap; this finding scopes WHERE HTTP applies (leaf
file tables) vs where the browser is still needed (tree enumeration).

### Maintainer idea (2026-07-31) — "use cache for fast results, run the slow version in parallel"
A genuinely better framing than my A-vs-B either/or: run BOTH concurrently.
Fast HTTP pass over cached section URLs returns most files quickly (~35s);
the full browser tree-walk runs in PARALLEL and supplies any sections the
cache didn't know (new folders). Merge when both done. The user gets fast
visible results AND no silent file loss (browser pass always completes the
set). This needs feasibility-checking before building, not promising:

UNANSWERED QUESTIONS (must measure, not assume):
1. Can cache-HTTP and browser results merge without dupes/conflicts? The
   syncer keys files by path; if both sources report the same file, does the
   merge dedupe cleanly or double-process?
2. Is the cache-HTTP pass actually faster than the browser pass STARTING
   concurrently from zero? If both take ~60s, parallelism buys nothing. The
   35s HTTP number was for 164 ALREADY-KNOWN urls; the browser must still
   enumerate them. So the win is: HTTP returns files WHILE browser still walks.
3. Does running HTTP and browser against the same OPAL session concurrently
   re-trigger the Wicket session-serialization trap (the documented parallel-
   ism corruption)? This is the highest-risk unknown.

If (3) is a problem, the parallel idea dies on the same reef the old
concurrency attempts did. If (3) is fine, it's the best design. MEASURE FIRST.

### MEASURED — the parallel idea is DEAD: concurrent HTTP+browser corrupts the session (2026-07-31)
Tested the maintainer's "run both in parallel" idea directly: started a
browser SW crawl, then fired the HTTP probe CONCURRENTLY against the same
saved session. Result:

| | serial (alone) | concurrent (HTTP + browser at once) |
|---|---|---|
| Browser | 200 files | **75 files** (lost 125!) |
| HTTP | 158 files | 156 files (stable) |

Running HTTP and the browser against the same OPAL session concurrently made
the BROWSER's "show all" expansion fail ("71 rows before, 71 after, expansion
added nothing") and drop 125/200 files. HTTP stayed stable (156 vs 158).

**Named cause:** the documented Wicket session-serialization trap. Browser and
HTTP share session cookies; concurrent requests collide in Wicket's server-side
per-session page state, corrupting the browser's DOM/AJAX state. This is the
SAME reef the old concurrency attempts died on, now measured for HTTP-vs-
browser specifically. **Parallel HTTP+browser discovery is not viable.**

Decision: build the serial HYBRID (option A). The browser owns the session
during tree-walk; HTTP fetches leaf tables ONLY when the browser is done (or
never concurrently). This is always-complete (no silent loss) and skips the
per-section settle wait by bulk-fetching leaf tables. The maintainer's parallel
idea would have been faster but is impossible against this OPAL session model.

### BUILD — serial hybrid wired in, gated behind OPAL_HTTP_DISCOVERY (2026-07-31)
Three new files + entry-point branch:
- httpdiscovery.go: parseHTTPSectionCandidates + extractShowAllURLFromHTML (pure,
  7 offline unit tests green).
- httpdiscovery_fetch.go: fetchSectionFilesHTTP (fetch + pager-showAll follow-up,
  3 offline unit tests green with a fake httpFetcher).
- scraper.go ScrapeWithSavedSession: branch on OPAL_HTTP_DISCOVERY.
- orchestrator.go scrapeCoursesHybrid: runs the browser crawl (source of truth),
  then STRICTLY serial HTTP fetch of every section the browser visited
  (VisitRecords is the section-URL source, since RemoteFile drops SectionURL),
  and logs a per-course diff.

mode=verify (current): returns the browser result, logs the diff. This is the
345-file contract check. mode=1 (future): returns HTTP result once diff=0.

RUNNING NOW: OPAL_HTTP_DISCOVERY=verify list against the real account. Expect
the summary line "total missing=0 extra=0". If anything is missing, the per-file
"missing:" log lines name exactly which and (from the diagnosis) why. Log in
tmp/baseline/verify-run.{out,err}.

### VERIFICATION PASSED — 345-file contract diff = 0 (2026-07-31)
OPAL_HTTP_DISCOVERY=verify list against the real account:

| course | browser | http | missing | extra |
|---|---|---|---|---|
| 2026 LA20 | 25 | 25 | 0 | 0 |
| Algorithmen | 38 | 38 | 0 | 0 |
| Analysis | 30 | 30 | 0 | 0 |
| So26 Prog | 13 | 13 | 0 | 0 |
| Softwaretechnologie | 200 | 200 | 0 | 0 |
| TUDMATH NuMa | 17 | 17 | 0 | 0 |
| TOTAL | 323 | 323 | 0 | 0 |

(The 345 baseline count vs 323 here: the difference is cross-section duplicate
file names that the per-course distinct sets collapse. Both sources count
identically, which is what the contract requires.)

HTTP phase alone: 282 sections, 287 requests, 56.0s. Log: tmp/baseline/verify-run.out.

### THE HONEST NUMBERS — and what they mean for 30 s
- verify mode runs BOTH phases serially: browser crawl (200s) + HTTP (56s) =
  267s total. Adding HTTP after the browser is SLOWER, not faster.
- The HTTP leaf-fetch is sound and complete (diff=0), but it only saves time
  if it REPLACES the browser's leaf extraction (mode=1) rather than running
  after it. And even then, the browser still has to walk the section TREE
  (the JS-rendered part) — which is the bulk of the 200s.
- So the realistic speedup from option A is NOT 200s -> 30s. It is:
  browser tree-walk (no leaf settle wait) + HTTP leaf fetch. The tree-walk's
  settle wait exists to render the FILE table the browser currently reads;
  if the browser only needs folder links (tree nav), a shorter wait might do.
  THAT is the unmeasured lever left, and it touches visitSection's wait logic
  - exactly the code with a documented silent-file-loss history.

Next decision point: is the remaining work (shortening the tree-walk's wait,
risk-testing it for file loss) worth it, given 30s is now clearly out of reach
without the rejected cache approach (B)? The diff=0 result means A is SAFE to
ship as-is for correctness; the question is whether ~60-90s is acceptable or
whether to stop here with a verified, correct-but-not-30s improvement.

### CORRECTION to my own framing — current mode=1 is not faster (2026-07-31)
Honest self-check: my current scrapeCoursesHybrid runs the FULL browser crawl
(200s, including its own leaf extraction and settle waits) and THEN runs HTTP.
That is verify-only; mode=1 returning HTTP would not be faster either, because
the browser crawl - the slow part - runs unchanged either way. Adding HTTP
cannot speed up a crawl that still does all the work itself.

The ONLY way A produces a speedup is: browser walks the tree WITHOUT extracting
leaf file tables (so it does not pay the settle wait that exists to render
them), and HTTP fetches the leaf tables instead. That requires changing
visitSection's wait budget for the tree-walk case - the code with documented
silent-file-loss history. So "A fertigstellen" and "wait-logic messen" are the
SAME task, not alternatives: the speedup IS the wait-logic change.

This is the real next experiment: does the section TREE (folder nav links) need
the same settle wait the file table does, or do folder links render early
enough that a navigation-only wait suffices? Measure, don't assume - this is
the exact class of change that lost files silently before.

### HANDOVER — clear next-session starting point (2026-07-31)
State: 9 commits this session on master. Everything builds, full scraper suite
green, HTTP discovery verified diff=0 against the 345-file contract.

The ONE remaining lever for speed (without the silent-loss cache B): make the
browser walk the section tree WITHOUT paying the settle wait that exists to
render file tables, and let HTTP fetch the leaf tables instead. This means a
navigation-only variant of the visitSection wait, which is the exact code with
documented silent-file-loss history. Do NOT just shorten the debounce - the
campaign measured that (150ms -> 322/345 files, real loss). The tree-walk case
must be measured on its own: do folder-nav links render early enough (the page
finishes structurally in ~36ms per the campaign) that a short wait finds them
even when the file table has not settled?

Concrete first experiment for the next session: capture a section with
subfolders at multiple time points and diff folder-link counts. If folder
links are present at ~50ms but file tables need 300ms+, a navigation-only
short wait is viable and the speedup is real. If they appear together, the
tree-walk cannot be sped up safely and A tops out where it is.
