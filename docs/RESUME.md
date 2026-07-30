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

**2026-07-30 unattended resume run - control run confirmed the bug is
isolated to section-cache piece 3, and a fix is in and awaiting live
verification.**

Control run (`OPAL_SECTION_CACHE` unset, `tmp/cache_cold_run_control.log`)
completed clean: 345 files, byte-identical (`diff` empty) against
`tmp/filelist-cache_ground_truth.txt`. So the 2026-07-29
5-courses-return-0-files bug is **not** an OPAL-side/pre-existing issue - it
is caused by the section-cache feature (piece 3), as suspected.

**Root cause, read from the data rather than guessed:** `tmp/.opal-sync.
sections.json.broken-uafix-baseline` (the cache written by the broken run,
kept as evidence, not loaded by anything) shows the failing courses' root
sections were not fetch failures - each got a real 200 with a non-empty hash,
and the BROWSER's own extracted `root_text` for those roots was a short
generic-chrome stub (765-2769 bytes: nav/help boilerplate, no course menu)
against course 1's real, full root (4155 bytes, matches ground truth exactly).
So the *browser's own render* came back stubbed for 5 of 6 courses - not a
probe-response content problem, since `root_text`/candidates are always
populated from the browser's visit, not the HTTP probe, whenever a section is
a cache miss (which every section was, this being a cold cache).

Course 1 crawled 39 sections start to finish with the same probe-then-visit
interleaving used for every course, so "probe right before a browser Goto to
the same URL" is not unsafe in general - it only broke on the **first**
section of every course after the first. The other distinguishing fact:
`course_concurrency: 1`, `section_concurrency: 1` in the config actually used
- fully serial, so this is not a concurrency race repeat.

**Leading theory, matches `docs/BACKLOG.md`'s own "Noticed" entry:** the
probe's HTTP client sent `User-Agent: "...opal-downloader"` (no
AppleWebKit/Chrome/Safari tokens - an obvious non-browser fingerprint) on
every probe fetch. ~33 such requests fired during course 1's own crawl before
course 2 was ever reached. A WAF/bot-heuristic tripping on that fingerprint
partway through, and then downgrading responses for the rest of that
*session* (not just that client) regardless of which local component
(browser vs. our HTTP client) sends the next request, would explain exactly
this shape: first course unaffected (already mostly rendered before any flag
could trip), every course after it stubbed.

**Fix committed** as `cd1282c` and pushed (2026-07-30, interactive session).
`internal/scraper/sectioncachewiring.go` now sends a realistic
Chrome-on-Windows `User-Agent` (matching the Chromium build `playwright-go
v0.6100.0` bundles) instead of the giveaway string. `scripts/dev.ps1 all`
green: all Go tests, `vet` clean, 148 hook tests. `codeLineBudget` raised by
one line in the same commit for the new named const.

**What went wrong on 2026-07-29/30, worth knowing:** the previous unattended
resume run left the fix *uncommitted* in the working tree while claiming
"committed-pending" here, and launched the live verification as a background
job that died with the run's own process - `tmp/cache_uafix_run.log` stopped
after `"Saved session state expired. Interactive login required."` with no
`EXIT:` sentinel and no output file. The runner logged `exit 0, 0 new
commit(s)` and looked fine. See the Noticed entry in `docs/BACKLOG.md`.

**Live verification relaunched** in the interactive session of 2026-07-30
(cache starts cold - only `tmp/.opal-sync.sections.json.broken-uafix-baseline`
exists, kept as evidence, loaded by nothing). Pass condition:
1. `diff tmp/filelist-cache_uafix.txt tmp/filelist-cache_ground_truth.txt` -
   empty diff and 345 files.
2. If it passes: update `docs/BACKLOG.md` (resolve the synthetic-UA Noticed
   entry, record the live result), and the section-cache campaign's
   correctness bug is closed. The separate open questions (flip the feature's
   default on; raise the request rate toward the ceiling) stay the
   maintainer's call, already flagged as such.
3. If it does NOT pass (still stubbed courses): the UA theory is wrong or
   incomplete. Do not guess further - next would be instrumenting the actual
   HTTP response status/headers/body length per probe request (temporary
   logging in `fetchSectionHTMLPolitely`) to see directly what's coming back,
   rather than inferring from the cache file again.

Also still open, independent of this bug (already in `docs/BACKLOG.md`'s "Now"
sync-speed entry's orbit, not re-litigating it): the probe test's silence on
per-course crawl failures - `filelist_probe_test.go` never calls
`logging.Setup`, so `logging.Warn`/`Error` lines are invisible in its output.
Worth fixing but not blocking this bug fix.
