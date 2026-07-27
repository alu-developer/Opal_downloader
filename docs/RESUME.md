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

**The change-detection cache is reopened by a measurement (2026-07-27, ~22:20).**

**Read this first if picked up cold: it is the most important sync-speed result
in the campaign, and it is only half-proven.**

`docs/sync-speed-campaign.md` rejected the change-detection cache in July on a
real number - normalised section HTML matched across runs **0 of 276 times** -
but that entry names its own unfinished business: the remaining volatile
fragments were never isolated, and the diagnostic meant to do it was botched
(it compared two standalone fetches). So the rejection was measured, and its
*cause* was never established.

It is established now. `internal/scraper/htmlstability_probe_test.go` (test
only, opt-in, no product code) fetches one section twice over plain HTTP 45s
apart with the saved session cookies and diffs the normalised forms.

**What varies is Wicket bookkeeping, nothing else:**

- `?2284-1.0-...` / `?2288"` - Wicket's per-session **page-version counter**,
  incremented on every render.
- `id35a0c` / `id35a85` - Wicket's **generated component ids**, the same
  counter in hex.

Both live only in the header navigation's Ajax glue (tabs, profile, logout,
search) and in `<title id=...>`. **Neither touches the file table.** With two
more patterns added, 168 differing lines went to 1, then to **0 - the
normalised forms are byte-identical.**

**So OPAL section HTML IS reproducible across runs.** The campaign's standing
conclusion ("not reproducible beyond the four normalised patterns") is wrong,
and the approach that could actually reach ~30s - skipping the browser crawl
for unchanged sections - is live again.

**What is NOT yet proven, and must be before anyone builds on this:**

1. One section, one pair, **same saved session**. The original rejection
   measured 276 sections across separate runs, where JSESSIONID differs too.
   Wicket's counters are per-session, so a fresh session starts them elsewhere -
   the normalisation should still cover it, but that is reasoning, not a
   measurement.
2. The safer design this suggests anyway: **hash the content subtree, not the
   page.** All the volatility found is in the page chrome; the extractor
   already targets `section#main-content`. Hashing only that sidesteps every
   pattern above instead of chasing them.

**Generalised, and it holds for most sections (2026-07-27, ~22:40).** Twelve
sections of one course, fetched twice 60s apart:

| comparison | identical |
|---|---|
| whole page, raw | 0 / 12 |
| whole page, normalised | **9 / 12** |
| content region, raw | **0 / 12** |
| content region, normalised | **9 / 12** |

Two corrections to what is written above, both from this measurement:

- **The content region is NOT stable on its own.** "All the volatility is in
  the chrome" was my inference from one section and it is wrong - the raw
  content region matched 0 of 12. Normalisation is required either way, so
  hashing the subtree buys correctness-of-scope, not freedom from patterns.
- **9 of 12, not 12 of 12.** Against the campaign's 0 of 276 that is the
  finding, but three sections still vary for a reason nobody has looked at.

**Why 75% would already be a large win:** a cache miss only costs a crawl of
that section - the safe direction. A false *match* is the dangerous one, and
nothing here has yet tested for that. So the two next steps, in order:

1. **What varies in the 3 outliers?** Same diff treatment as the single-section
   probe. They may share one more normalisable pattern, or they may be
   genuinely dynamic (a "last visited" stamp, a per-render token in a file
   row), which would matter a great deal.
2. **Does a match ever lie?** Change one file in a course, then confirm that
   section stops matching. Until that is measured the hit rate is worthless -
   a cache that reports "unchanged" for a changed section silently stops
   downloading, which is this project's worst failure mode.

Only after both: rebuild the cache and measure a warm no-op sync. The previous
build measured 317.6s warm because essentially nothing ever hit.

Beware the trap the campaign already recorded: a cache of extractor output
needs rootText interning, or the file is **52 MB** for 276 sections.
