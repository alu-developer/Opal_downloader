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

**Generalised, corrected twice, and still alive (2026-07-27, ~23:00).**

Twelve sections, fetched twice 60s apart: whole page raw **0/12**, whole page
normalised **9/12**, content region raw **0/12**, content region normalised
**9/12**.

**Correction 1 (mine, from one section):** "all the volatility is in the page
chrome" is wrong - the raw content region matched 0 of 12. Normalisation is
required either way.

**Correction 2, and this one nearly killed the lead on a false premise.** A
probe against `CourseNode/1757385431705760008` found **zero** file references
in the server HTML and I was about to conclude that a hash cannot see file
changes at all. That node is an *enrolment* node with no files. A section that
actually holds files carries its filenames in the server HTML:
`AnalysisSkriptChill` appears 3 times, `.pdf` 3 times. **Always probe a
file-bearing node** - the default in the probe now is one.

**Safety, tested and passing.** `TestNormalisationDoesNotHideRealChanges`
applies the edits a lecturer really makes to the live page and requires the
normalisation to still see each: a file renamed, a new file row appended, a
single character changed in the body. All detected. (The href-change case skips
- file URLs are `/CourseNode/.../Name.pdf`, not `/FolderResource/`.) This is
the half that matters: a miss costs a crawl, a false match silently stops
downloading.

**Where it stands: viable, not proven.** The one remaining measured gap is that
the file-bearing section still shows **4 differing lines** after normalisation,
and 3 of 12 sections do not match at all. Nobody has looked at what those are.

**Immediate next step:** dump the 4 differing lines for the file-bearing
section (the single-section probe already prints them - just read them) and
decide whether they are one more bookkeeping pattern or something genuinely
per-render. That single answer decides whether the hit rate goes to ~100% or
stalls at 75%.

Then, only then: rebuild the cache against the content subtree, with rootText
interning (without it the file was **52 MB** for 276 sections), and measure a
warm no-op sync against the 210.3s baseline.

Beware the trap the campaign already recorded: a cache of extractor output
needs rootText interning, or the file is **52 MB** for 276 sections.
