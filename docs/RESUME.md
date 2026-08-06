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

**In flight (2026-08-07, autopilot): Question 26**, `docs/sync-speed-model.md`
"Next experiment" — retest Question 23's shelved `OPAL_BLOCK_FILE_PREVIEWS`
raw-CDP rewrite now that Question 25's reclick-recovery fix is live-verified.
Prediction (written before running, per rule 1): `OPAL_FILELIST=after
OPAL_BLOCK_FILE_PREVIEWS=1` diffs empty against an `OPAL_FILELIST=before` run
of the full real account — Part-3 of "Softwaretechnologie (SoSe 26)" no longer
loses its 33 files. Fails at: any non-empty diff, anywhere. Concurrency check
done first (no `chrome.exe`, no commit from another session in the last few
minutes, tree clean) — confirmed quiet. Plan: run `before`, then `after`, then
`diff`; write the result into `docs/sync-speed-model.md` and clear this note.
