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

Sync-speed Frage 15, fourth run in progress (autopilot, 2026-08-02): third
attempt succeeded mechanically (`tmp/debounce-override-probe.txt`) - override
self-consistent at 210 files across 2 runs, but that's +12 vs. the
2026-07-16 historical count of 198. Likely course-content drift (this is an
active SoSe 26 course, 2.5 weeks later), NOT necessarily an override-caused
gain/loss, but the skip-baseline design of that run can't tell the two apart
- there is no same-day 300ms baseline to diff against. Running the full,
un-skipped probe now (2 baseline + 2 override, same course) specifically to
get a real same-day crossDiff instead of guessing. Command:
`OPAL_DEBOUNCE_OVERRIDE_TRACE=1 OPAL_DEBOUNCE_OVERRIDE_COURSE="Softwaretechnologie (SoSe 26)" go test ./internal/scraper/ -run TestDebounceOverrideCorrectness -v -timeout 20m > tmp/frage15_run4.log 2>&1`.
Result overwrites `tmp/debounce-override-probe.txt` (save the run3 output
first if not already captured in docs/sync-speed-model.md before this lands).
Next: write the real crossDiff result into Frage 15, commit, close or open
Frage 16.
