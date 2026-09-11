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

**In flight (2026-09-11 autopilot, continuing the 2026-09-02 prediction):
Question 45 option D verification, parts 1 (universality) + 3
(column-C-populated).**

- Prediction was written into `docs/sync-speed-model.md` "Next experiment"
  and committed 2026-09-02, but the probe code was never written that run -
  it moved to a Phase 2 walk instead. Written now: `OPAL_TABLEDL_UNIVERSAL=1`
  / `TestTableDownloadUniversality` in
  `internal/scraper/bulkzip_probe_test.go` (commit 7424fc7, pushed).
- `tmp/sections-with-files.json` built from the real
  `C:/Users/alois/OneDrive/.opal-visit-log.json`'s most recent scheduled-sync
  run (2026-09-11 13:44-13:47, the first sync since 2026-09-02 - a 9-day gap
  worth a separate look if it recurs): 58 folder sections with
  `files_found > 0`, deduped by `section_url`, across all 6 courses -
  matches the design doc's expected count exactly.
- Worktree has its own `config.yaml` (copied from the main checkout) and the
  probe resolves its repo root via `runtime.Caller` rather than a hardcoded
  path, so it runs correctly from here.
- Next step: run
  `OPAL_TABLEDL_UNIVERSAL=1 go test ./internal/scraper/ -run TestTableDownloadUniversality -count=1 -v -timeout 30m`
  live against the real account.
- If killed mid-run: `tmp/tabledl-universality-results.jsonl` holds partial
  data (not checked in - rebuild `tmp/sections-with-files.json` the same way
  if a fresh worktree needs it). Re-run skips sections already in it.
- Parts 1+3 only. Part 2 (date fidelity vs a 345-file byte-diff) is the next
  cycle if these hold; if column C is empty for the signal-less files, fall
  back to Question 45 option A.
