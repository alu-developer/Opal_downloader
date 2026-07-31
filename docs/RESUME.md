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

The scheduled Desktop task's prompt reads this file first, so stale content
here sends an unattended run after work that is already done. Clear it.

---

_Nothing in flight._

### MEASURED — navigation-only tree-walk is DEAD: content subtree is JS-rendered too (2026-07-31)
The nav tree-walk probe (navtreewalk_probe_test.go) tested whether a lightweight
BFS - navigate, 100ms wait, extract folder links, descend - reaches the same
section set the full browser crawl does. It reached ONE section (the root),
0 children expanded, on both the course root AND the real content node
(CourseNode/1615865126729195011, which has 16 children in the full crawl).

Debug probe (navdebug_probe_test.go) on that content node at 100ms:
- 24 candidates extracted
- 1 passes looksLikeSectionFolderLink, and that 1 is the course-root link
  pointing back at itself - NOT real children.
- 0 file links.

The earlier tree-walk timing probe's "25 folder links at 50ms" was misleading:
looksLikeSectionFolderLink counts ANY href with /coursenode/, and at 100ms the
visible links are nav/breadcrumb/other-course links, not this node's real
content subtree. The content subtree itself is JS-rendered and is NOT in the
DOM at 100ms - it needs the full settle wait the browser crawl already pays.

CONCLUSION (named, not guessed): the section-content tree is JS-rendered at
EVERY level, not just the dashboard. A navigation-only short wait cannot
enumerate it. Option A's only speed lever does not exist: the browser cannot
walk the tree faster because the tree itself is what the settle wait renders.
The serial-hybrid as built (verify, diff=0) is the ceiling for a correct,
loss-free path. 30s is unreachable without the cache approach (B), which
silently misses new sections.

This closes the speed investigation: every lever measured, each rejection
diagnosed to a named cause. The remaining work is productizing the verified
HTTP discovery (it is correct and complete) - which is a correctness no-
regression, not a speed win.

### mode=1 activated (option A productized) — but session expired mid-run (2026-07-31)
scrapeCoursesHybrid now returns the HTTP result when OPAL_HTTP_DISCOVERY=1,
guarded: if HTTP finds fewer distinct files than the browser in ANY course,
it falls back to the trusted browser result (so a faster path can never
silently lose files). Builds, full scraper suite green.

Tried to measure mode=1's real wall-clock, but the OPAL session had expired
by then ("Saved session state expired. Interactive login required") - the
mode=1 run could not complete without an interactive login. Killed the
waiting process. The verify run earlier (diff=0, 323 files) IS valid - it ran
on an active session at ~03:00. Only the mode=1 timing measurement is
outstanding, and it needs a renewed session.

What mode=1 will measure (when the session is renewed): browser crawl (~200s,
unchanged - it still has to walk the JS-rendered tree) + HTTP phase (~56s).
So mode=1 is ~250-270s, NOT faster than the plain browser crawl. This was the
honest conclusion from the closed speed investigation: the HTTP path is a
correct, verified, independent file source (a useful no-regression control),
not a speed win, because the browser tree-walk is the bottleneck and cannot
be sped up safely. A is productized for correctness, not for the 30s target.

### CONCRETE BREAKDOWN — why the predicted improvement doesn't work (2026-07-31)
list --verbose on a renewed session, 282 sections, the real numbers:

| phase | time | per-section | share |
|---|---|---|---|
| settle wait (MutationObserver debounce) | 1m35.4s | 338ms | 64% |
| stability poll (candidateStabilityPoll) | 48.6s | 172ms | 32% |
| everything else (extract, nav, all) | 3.8s | 14ms | 2% |

Plus: `rate ceiling: 286 navigation(s), 0 delayed, 0s held` - the rate limiter
holds back ZERO requests. All 207s is this tool waiting on its own timers.

WHY EACH PREDICTED IMPROVEMENT FAILS - checked, not asserted:

1. "Skip the settle wait" -> measured WORSE. crawl.go:310 records it: skipping
   is byte-identical AND 51% SLOWER (317s vs 210s), because the settle wait
   PRODUCES the sectionCalm signal that lets the poll open impatient. Without
   settle, every section pays the poll's full patience streak. The 64% is not
   removable overhead; it is the cheaper of two waiting mechanisms.

2. "HTTP replaces browser extraction, saves the settle" -> HTTP can't run
   before the browser yields section URLs, and the browser pays settle on
   every section to get them. HTTP afterward (mode=1) ADDS ~56s instead of
   removing the ~95s. That is exactly why mode=1 measures ~250s, not faster.

3. "Cut the stability poll (32%)" -> not optional. navigation.go:545 records
   it: a poll requiring 1 stable read instead of 4 lost real files past
   pagination, because the show-all control had not rendered at extract time.
   The poll is a safety requirement, not waste.

4. "Rate limiter is the bottleneck" -> it is not: 0 delayed, 0s held. Server-
   load policy brakes nothing here.

NET: the 207s is structurally this tool proving its own completion (96% is
waiting for silence/stability), on a tree OPAL renders client-side at every
level. There is no lever left that removes waiting without removing the
correctness guarantee that waiting provides. 30s is unreachable without
caching section URLs across runs (option B), which silently misses new
sections - the one constraint the maintainer refused to relax.

### MODE=1 MEASURED LIVE — 254.1s, slower than the plain browser crawl (2026-07-31)
On the renewed session, OPAL_HTTP_DISCOVERY=1 list: Total 254.1s (HTTP phase
55.82s, diff=0 verified, HTTP result returned). vs the plain browser crawl's
206.9s measured the same session. mode=1 is 47s SLOWER, exactly as the
breakdown predicted: the browser crawl still pays its full ~207s (it can't be
sped up), then HTTP adds 56s on top.

NOTE for follow-up (not a speed issue): mode=1 reported Softwaretechnologie at
246 files vs the browser's 200. The HTTP result does not apply the browser's
cross-section fileSeen dedupe the same way (HTTP fetches per-section and the
merge is looser). The diff=0 is on distinct NAMES per course, so no files are
LOST - but the HTTP path can surface name-collisions the browser's stricter
dedupe collapses. Worth tightening before mode=1 ships as a default; for now
it is opt-in and the diff guard still catches any genuine loss.

This is the final, measured answer to "why doesn't the predicted improvement
work": it was never going to, because the improvement requires removing the
browser's per-section wait, and that wait (settle 64% + poll 32% = 96% of
in-section time) is the mechanism that makes file extraction correct on a
tree OPAL renders client-side at every level. HTTP can't replace it because
HTTP needs the section URLs only the browser's waited-out crawl produces.
