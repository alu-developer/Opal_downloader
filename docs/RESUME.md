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

**2026-07-29 unattended resume run — REAL correctness bug found, not the
budget theory.** The previous session's cold run (`tmp/cache_cold_run.log`,
220 bytes, no EXIT line) was indeed just the resume-runner-was-dead casualty,
not a test failure — confirmed by rerunning it clean.

**The rerun completed (`EXIT:0`, 61.62s) but is WRONG**: only 39 files found
against a 345-file ground truth (`tmp/filelist-cache_ground_truth.txt`), and
all 39 are a single course (`2026 LA20`, byte-identical to ground truth for
that course — confirmed via `diff`). The other 5 configured courses produced
**zero files each**, with **no visible error or warning in the console
log** — `logging.Warn`/`Error` calls in `crawl.go`/`orchestrator.go` are
diagnostic-level and the test never calls `logging.Setup`, so a per-course
crawl failure (`newCourseFileCollector`'s `logging.Warn("Course crawl error:
%v", ...)`, or the "crawled successfully but found 0 files" line) is
completely silent on stdout. This silence is itself worth fixing
independently of the cache bug (see step 4 below) — a probe test with no
visibility into per-course failure is not trustworthy.

**Diagnostic evidence so far**: `tmp/.opal-sync.sections.json` (written by
this run) has 39 sections cached total — 34 for `2026 LA20` (matches its
full crawl) and **exactly 1 section each** for the other 5 course repo IDs.
That 1-section-then-stop pattern for every other course means the root
section was reached and "succeeded" (no Goto/extraction error, so not the
`sectionsFailed` path) but produced zero file/subfolder candidates — not a
Goto failure. Something about the root section's *content* differed from
what real crawls have found before on these same courses (ground truth was
captured just yesterday, 2026-07-28, with full content for all 6).

**Isolation in progress**: running the exact same probe with
`OPAL_SECTION_CACHE` unset entirely (`tmp/cache_cold_run_control.log`,
`OPAL_FILELIST=cache_control`, backgrounded via `nohup`, being watched by a
Monitor task) to determine whether this is a regression introduced by the
section-cache feature (piece 3) or a pre-existing/unrelated issue (e.g. an
OPAL-side change now that it's a few days into looking like next-semester
course pages — note the course names literally say "2026" and "SoSe 26").
**Do not conclude anything about the cache feature until this control result
is in** — if the control run (cache fully off) also produces only ~39
files, the bug is unrelated to this campaign's changes and section-cache
piece 3 is not at fault.

A second interactive TU-Fast login happened during today's runs (session
expired again between the cache run and starting the control run) — that's
normal per CLAUDE.md's login automation and not itself suspicious, but it
does mean the two runs are on two different fresh sessions, which is
actually useful: if both show the same 1-course-only pattern, a
session-specific fluke is ruled out too.

**Next, once the control run's `EXIT:` line lands** (check
`tmp/cache_cold_run_control.log`):

1. Diff `tmp/filelist-cache_control.txt` against
   `tmp/filelist-cache_ground_truth.txt`.
2. If the control run is ALSO broken (only ~39 files): this is not a
   section-cache regression. Write it up in `docs/BACKLOG.md` as a fresh,
   serious finding (something outside this campaign broke course crawling
   for 5/6 courses) and stop touching the section-cache work until that's
   understood — don't let an unrelated fire block on this campaign's
   acceptance criterion, but don't paper over it either.
3. If the control run is clean (345 files, matches ground truth): the bug is
   isolated to section-cache piece 3. Read `crawl.go`'s BFS-level loop
   (`s.checkSectionCache` called before `pool.visitAll`) and
   `sectioncachewiring.go`'s HTTP probe path for why a plain-HTTP GET of a
   section (using only cookies, no JS, a synthetic User-Agent) immediately
   before the real browser Goto might cause the *browser's own* subsequent
   navigation to come back content-less for every course after the first —
   candidate theories to check: the probe fetch tripping an OPAL-side
   session/CSRF check that invalidates the session for the *browser*
   context too (would explain why only the first course, probed before
   anything could go wrong, came back complete); or the probe consuming a
   one-time-use redirect/token the real page load also needed. Do not
   guess — instrument or read the actual request path.
4. Either way, also add to `docs/BACKLOG.md`: the probe test's silence on
   per-course crawl failures (finding above) is a real gap — `logging.Warn`
   for course-level errors should not be invisible to a diagnostic test
   whose entire job is catching exactly this. Consider having
   `filelist_probe_test.go` call `logging.Setup` with `Verbose: true`
   pointed at a test-local file, or otherwise surface `wr.err` counts.

**Not this session's decision, and already flagged in `docs/BACKLOG.md`:**
whether to actually flip the default on, and whether to let the effective
request rate rise toward `docs/server-load.md`'s ceiling to reach the ~70s
floor rather than ~93s — both are the maintainer's call once the live number
exists.
