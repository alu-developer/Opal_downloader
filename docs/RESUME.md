# Resume note

Scratch state for work that is **in flight right now**. Kept in git so it
survives a killed turn, a dead session, and a fresh clone.

`docs/BACKLOG.md` says what should happen and stays tidy. This file is allowed
to be messy: it is the thought that would otherwise only exist in a context
window, and a context window does not survive the usage limit being hit
mid-turn.

**Keep it current while working**, not at the end — the end is exactly the part
that does not always arrive. Update it whenever the answer to "what am I doing
and what's next" changes materially. When the work lands, clear it back to the
placeholder line below.

The SessionStart hook reads this file and hands it to the next session. The
scheduled resume runner also treats a non-placeholder file here as "there is
work", so leaving stale content in it will wake an unattended run for nothing.

---

## In flight: section-level concurrency (maintainer signed off 2026-07-26)

The sync-speed campaign's last unexplored axis. Sign-off is explicit — this was
blocked on exactly that. Read `docs/sync-speed-campaign.md`'s final entry
first; the design is already argued there.

**Target:** the 160-section Softwaretechnologie course is 2m48s of a 5m22s
discovery phase. Course-level concurrency cannot touch it (one course), so
this is the only axis that can.

**Design chosen (narrower than the campaign log's most ambitious version):**
level-synchronised BFS *within* a course. Pop a whole BFS level, visit its
members concurrently on K tabs, then merge results **serially in the level's
original queue order**. The merge (appendSectionFiles → fileSeen dedupe →
recordSectionVisit → appendSectionFolderTargets) stays exactly as it is, so
file order and dedupe outcomes are identical to serial. Only the slow part
(Goto + settle + stability poll + show-all) runs in parallel.

Deliberately NOT doing the campaign log's "one global frontier across all
courses" yet: it interleaves courses, which changes per-course error accounting
(sectionsVisited/sectionsFailed) and progress reporting, in the part of the
codebase with a documented history of *silent* file loss. Contained change
first, measure, then decide if the bigger one is worth it.

**Non-negotiable before this is trusted (the campaign file's own rule):**
byte-for-byte file-set parity against the serial ground truth (345 files as of
2026-07-26's live run), multiple runs. A faster run that finds fewer files is a
failure, not a tradeoff. Concurrency 4 at course level already lost 9 files
once while looking 20% faster.

**Progress:** design settled, reading orchestrator.go for how per-course tabs
are opened and how SetCourseConcurrency is plumbed, so section concurrency is
configured the same way.
