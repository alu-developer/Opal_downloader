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

The SessionStart hook reads this file and hands it to the next session. The
scheduled resume runner also treats a non-placeholder file here as "there is
work", so leaving stale content in it will wake an unattended run for nothing.

---

**2026-07-30: the section cache's warm-cache measurement is running.** The
correctness bug is fixed, verified live and pushed (`99d1fbe`); what was never
measured is whether the cache is actually *faster*, because every timing so
far came from a cold cache, which can only cost. See the section-cache entry
under "Now" in `docs/BACKLOG.md`.

The run: `OPAL_SECTION_CACHE=1 OPAL_FILELIST=cache_warm go test
./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m`, output to
`tmp/cache_warm_run.log` with an `EXIT:<code>` sentinel appended. Its cache is
the one the verification run left behind (280 sections, all 6 courses); a copy
is preserved as `tmp/.opal-sync.sections.json.warm-baseline` since the run
overwrites it.

Numbers to compare against, same account: **241.0s** control with the cache
off, **283.9s** cold cache (that one included an interactive login, so it is
not a clean comparison in either direction).

Two conditions, and the second is not optional:
1. `diff tmp/filelist-cache_warm.txt tmp/filelist-cache_ground_truth.txt` must
   be **empty** at 345 files. A cache hit skips the browser entirely, so a hit
   on a section that actually changed loses files silently — the exact failure
   mode this project refuses file counts as evidence for.
2. Wall clock materially under 241.0s is the only thing that justifies the
   feature existing. If it is *not* faster warm, that is the answer and the
   feature should be reported as not worth its risk rather than tuned until it
   looks good.

Whatever comes back: record it in the section-cache entry under "Now",
including a negative result, and clear this file to
`_Nothing in flight._` (the underscores matter - `resume-runner.ps1` matches
on that exact string).
