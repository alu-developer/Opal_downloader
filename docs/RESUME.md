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

**In flight (2026-09-02 autopilot): Question 45 option D verification, parts 1
(universality) + 3 (column-C-populated).**

- Prediction written into `docs/sync-speed-model.md` "Next experiment" and
  committed before the run.
- New probe mode `OPAL_TABLEDL_UNIVERSAL=1` /
  `TestTableDownloadUniversality` in `internal/scraper/bulkzip_probe_test.go`.
  Reads `tmp/sections-with-files.json` (58 folder sections with files, from
  the real `.opal-visit-log.json`'s most recent scheduled run), navigates
  each, checks for the "Tabelle herunterladen" control, downloads + hand-parses
  the XLSX, writes one JSONL line per section to
  `tmp/tabledl-universality-results.jsonl` as it goes (survives a kill).
- If killed mid-run: `tmp/tabledl-universality-results.jsonl` holds partial
  data. Re-run skips sections already in it, or just analyse what landed.
- Parts 1+3 only. Part 2 (date fidelity vs a 345-file byte-diff) is the next
  cycle if these hold; if column C is empty for the signal-less files, fall
  back to Question 45 option A.
