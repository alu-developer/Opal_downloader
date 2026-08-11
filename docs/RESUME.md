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

**2026-08-11, autopilot, phase 2 (sync speed).** Working Question 38 (day-two
rerun of Step B1's probe, `TestHTTPFirstSectionDiscovery`) after Question 36
Step B2 merged as the production default (PR #133, not #134 - #134 was a
duplicate, closed). Commits so far this session: `d437f35` (prediction
registered before running, per Rule 1) and `add1c24` (a real bug found along
the way: `discoverSectionsHTTP`, the new default's discovery path, never
called `recordSectionVisit`, so `internal/visitlog`'s persistent cross-run
log silently stopped growing the moment B2 shipped - fixed, regression test
added, full suite green).

**In flight right now:** re-running the probe in the background
(`internal/scraper` test `TestHTTPFirstSectionDiscovery`,
`OPAL_HTTP_FIRST_PROBE=1`) to (a) confirm the visit-log fix actually
populates `VisitRecords()` live, and (b) get Question 38's real timing number
now that the ground-truth comparison isn't silently empty. First attempt (no
fix) returned 0 courses compared - that's what surfaced the bug. Second
attempt (with the fix) hit the tool's 10-minute foreground cap partway
through the probe's own second HTTP pass, after some transient
TLS/socket-hang-up errors on 2 requests (no chrome.exe orphaned, safe to
retry) - now running a third attempt as a background task so it can finish.
When it lands: write Question 38's result into `docs/sync-speed-model.md`
against the 2026-08-11 prediction already committed there, note the
transient network errors as a data point (or not, if the retry comes back
clean), and clear this note.
