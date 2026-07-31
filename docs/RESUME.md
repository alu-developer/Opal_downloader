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
