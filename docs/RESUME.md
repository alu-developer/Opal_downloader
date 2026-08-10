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

## In flight: Question 34 (2026-08-10, autopilot)

**Status: the concealed-structure half is answered, and the pre-registered
prediction was wrong — the whole course tree IS in the first response.**

`var initial_data=[...]` in the course page's own HTML carries the *complete*
152-node tree for Softwaretechnologie, nested to depth 3, every node with its
absolute `CourseNode/<id>` href and its `node-<type>` class. Checked against
the crawl's own recorded visit set (`tmp/baseline/swt-all-sections.txt`):
**147 visited course-node ids, 0 of them absent from that tree.** Only 1 node
carries `"state":{"opened":true}` (the root), so the payload is emphatically
not scoped to open branches the way the rendered DOM is.

Present identically in every saved page of that course (root, entry, sec1-3,
part3-raw, part3-showall) and in the unrelated course
`internal/scraper/tmp/htmlstability-a.html` (38 nodes).

**What this bears on:** `docs/sync-speed-campaign.md`'s "Hybrid mode=1: 254s
against 207s, slower, *because HTTP can only start after the browser has
delivered the URLs*". That premise is what this finding removes.

**Next step when resuming:** check `internal/scraper/httpdiscovery.go` (parked,
verify-mode-only, byte-verified on all 6 courses 2026-07-31) — can it be seeded
from `initial_data` after one page load instead of after a full browser tree
walk. Then write the prediction for that experiment BEFORE running it.

Not yet done: the "reuse" half of Question 34 (does a page already fetched
carry file data the crawl re-fetches by navigating).
