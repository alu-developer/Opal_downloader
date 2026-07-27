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

**Immediate next step:** run the probe across many sections and across two
genuinely separate sessions. If the match rate holds, rebuild the cache against
the content subtree and measure a warm no-op sync - the previous build measured
317.6s warm because essentially nothing ever hit.

Beware the trap the campaign already recorded: a cache of extractor output
needs rootText interning, or the file is **52 MB** for 276 sections.
