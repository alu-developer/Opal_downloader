# Sync speed campaign

Standing goal set by the maintainer on 2026-07-21:

> "Ich lad mir mal kurz die neuesten Dokumente herunter" must feel like a
> click, not a coffee break. **Target: a routine sync in ~30 seconds.**
> 5 minutes is unacceptable.

This file is the campaign's decision log. It exists because the goal is a
"work until it works" directive, not a single task: approaches will be tried,
measured, and discarded, and the maintainer explicitly wants discarded ones
to stay reviewable — so that a later idea can go back and ask "did we get
that wrong the first time?"

**Every entry must record a measurement, not an opinion.** Four times on
2026-07-21 a documented explanation in this repo turned out to be wrong when
someone finally measured it (see `AJAX_CALL_DONE`, the concurrency penalty,
the browser-fallback root cause, a "flaky" test). Prose confidence is worth
nothing here.

## The measurement that counts

The user-facing number is a **no-op incremental sync**: everything already
downloaded, nothing new on OPAL. That is the "just grab the latest" case.
Downloading is NOT the bottleneck — on a normal day almost nothing is new.

Rules for any number quoted in this file:

- Full account, real config, wall-clock end to end.
- File-set parity byte-for-byte against the serial ground truth (342 files as
  of 2026-07-21). **A faster run that finds fewer files is a failure, not a
  tradeoff.** This has already caught one regression (concurrency 4 lost 9
  files while looking 20% faster).
- Discovery-only probes are fine for iterating (~5 min, no downloads), but
  the headline number must come from a real sync.

## Baseline (2026-07-21)

| phase | cost | note |
|---|---|---|
| discovery | ~4m25s | 284 section visits at `course_concurrency=2` |
| — settle wait | 551ms × 284 | `waitForContentSettled` |
| — stability poll | 457ms × 284 | `waitForStableSectionContent` |
| downloads (no-op) | ~0 | nothing to fetch when nothing changed |

So **~1 second of deliberate waiting per section, 284 times**, is essentially
the entire runtime. Everything else is noise.

## Where the leverage is

Ranked by expected gain, to be confirmed by measurement not by argument:

1. **Skip the browser for discovery entirely.** Plain HTTP with the saved
   session cookies is already proven to work in this codebase —
   `download_refresh.go`'s counter-refresh fast path succeeded 102 times in
   an instrumented full run. If section listings are server-rendered, 284
   HTTP GETs at concurrency 16 is *seconds*, not minutes. Prior research
   claims file rows are in the initial document response with zero Wicket
   AJAX on file-bearing sections — **that claim must be re-verified at
   scale before being trusted**, because its sibling claim from the same
   research (AJAX_CALL_DONE is trailing-safe) was refuted on 2026-07-21 by a
   52-file loss. Paginated sections needing a "show all" click cannot work
   this way (established in PR #100/#109), but there are only ~5-6 of them,
   so they can fall back to the browser.
2. **Section-level concurrency.** Course-level concurrency saturates at 2 on
   a 6-course account, but *within* a course the ~47 sections are visited
   strictly one after another. That axis is completely unexploited.
3. **Cut the per-section waits.** ~1s each. Any reduction multiplies by 284.
   Partly addressed on 2026-07-21 (patience is now earned per section), but
   the settle wait still averages 551ms.
4. **Don't re-discover unchanged sections at all.** Cache structure between
   runs. Highest risk of silently missing new files — treat as last resort,
   and only with a cheap change-detection signal that is itself verified.

## Decision log

| date | approach | outcome |
|---|---|---|
| 2026-07-21 | Baseline measured | no-op sync **318.9s**; discovery ~296s (93%), downloads 23.2s for 5 files |
| 2026-07-21 | HTTP-first discovery, probe | **Promising.** Sequential: 275 sections in **49.7s** vs ~296s. Completeness and the parallelism trap below. |

### 2026-07-21 — HTTP-first section discovery (probe, not yet implemented)

`internal/scraper/httpdiscovery_probe_test.go` (env-guarded, skips by
default). Fetches canonical section URLs through Playwright's
`APIRequestContext` — no browser page, no settle wait, no stability poll —
and extracts file links with the crawl's own `looksLikeFileLink` predicate.

**Speed: confirmed.** 275 sections in 49.7s sequentially, against ~296s of
browser-driven discovery. Zero request errors, zero non-200 responses.

**The parallelism trap — the most important finding here:**

| parallelism | wall-clock | distinct file names found |
|---|---|---|
| 1 | 49.7s | **257** |
| 2 | 47.9s | 176 |
| 3 | 20.3s | 219 |
| 12 | 4.4s | 164 (and varied run to run: 161/174/164) |

Non-monotonic, and parallelism 2 is *no faster* than 1 while finding a third
fewer files. Read that carefully: **OPAL serialises the session server-side
anyway, so concurrency buys no time and actively corrupts results.** Wicket
keeps per-session page state, and concurrent requests to different course
nodes interfere with each other. A single section fetched alone returns its
full content (18 files + its pager control); the same section in a
12-parallel batch returns less. Any future attempt at parallel HTTP discovery
must beat this table first.

**Completeness: not yet parity.** 257 of 312 distinct ground-truth names
sequentially. The gap is plausibly the paginated sections — only **5**
sections advertise a `pager-showall` control in their HTML, and those hide
everything past ~20 rows, which the browser must click to reveal (PR
#100/#109). 257 + those tails is about 312, but **that is arithmetic, not a
measurement — it must be verified before anything is built on it.**

**Shape of the fix this points to:** sequential HTTP for all sections
(~50s), plus the browser for only the handful that advertise a pager
(~5 sections). Estimated ~60s against 296s today. That is 5x, not the 10x
the 30s target needs — so the remaining per-request cost (~180ms x 275) will
need attacking too, likely by not visiting nodes that structurally cannot
hold files.

(Append one row per attempt. Include the measurement, and for a rejection,
enough detail that a later reader can judge whether it deserves another look
rather than having to redo the experiment.)
