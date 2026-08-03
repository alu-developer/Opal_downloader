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

**Do not run Question 17's concurrency=1 control run.** It was the "next up" here
until 2026-08-03 and is now unnecessary: Question 17 was answered from the
archived run log instead (`tmp/frage16-run.log`, 4/4 correlation with
`warnShowAllTruncated`). Server-side variance is refuted, so there is nothing for
that run to rule out. No env knob needed, no probe change needed.

Next up, already decided and needing nobody: **Question 18**
(`docs/sync-speed-model.md`) - `CourseNode/1775529461522481011` warns
`warnShowAllTruncated` in every archived run at every concurrency setting, so
files are probably missing from every sync ever done here, including the 345-file
ground truth. Two cheap steps, neither a full crawl: log the candidate hrefs
before and after expansion for that one node, and open the section by hand in the
login profile to count the real files. Prediction and failure criterion are
already written down in the model file - read them before running, not after.
