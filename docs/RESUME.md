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

Phase 3 sync-speed cycle in flight, 2026-09-14 (autopilot). Prediction
registered in `docs/sync-speed-model.md`'s "Next experiment" (top entry):
what course-node type are `Woche 05`..`13` (So26 Programmieren) built from,
since the 2026-09-11 cycle found the folder-browser toolbar entirely absent
on those 10 sections. Plan: new `internal/scraper/coursenodetype_probe_test.go`,
`OPAL_COURSENODE_TYPE=1`, one HTTP GET of the course root via the existing
`ParseCourseTreeNodes`/`initial_data` machinery (no DOM guessing this time),
report the `node-<type>` class for every `Woche` title plus two `node-bc`
controls. Next step: write the probe, run it live, record the result in
`docs/sync-speed-model.md`, clear this file.
