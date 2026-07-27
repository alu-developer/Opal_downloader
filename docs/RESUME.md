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

**Sync speed lead tested and refuted (2026-07-27) — writing up now.**

RESUME previously said the untested lead was: does a positive completion
signal exist ("AJAX_CALL_DONE fired AND the response carried the file-table
markup") to replace the 300ms settle-wait debounce that costs ~84s of a
216.6s run? `docs/sync-speed-campaign.md`'s 2026-07-27 entry proposed this on
the premise "the file table arrives in a Wicket AJAX response the browser
already receives and parses."

That premise contradicts `navigation.go`'s own existing doc comment on
`waitForInteractiveLinks`, from an earlier research task: "network trace
confirmed no separate 'populate content' AJAX request exists" for the
initial per-section render. Built a live probe
(`internal/scraper/network_trace_probe_test.go`, `OPAL_NETWORK_TRACE=1`) that
records every network response during a real section crawl and checked which
claim holds.

**Result on "Algorithmen und Datenstrukturen" (5 sections, 38 files): 263
responses total, exactly 2 were xhr/fetch, and both were the already-known
`pager-showAllLink` expansion calls** (already handled by
`wicket.go`'s `AJAX_CALL_DONE`). Zero AJAX fires for an ordinary section's
initial render. `navigation.go`'s claim holds; the campaign entry's premise
does not, for ordinary sections.

**Done — the lead is refuted, written up in `docs/sync-speed-campaign.md`
(2026-07-27 entry) and `docs/BACKLOG.md`.** Softwaretechnologie (SoSe 26):
8154 responses, 3 xhr, all three `showAllLink`. Zero unaccounted AJAX, same as
the small course. There is no network event to key a positive completion signal
off.

Next, if continuing: the two directions the write-up names as unexplored — a
DOM-level completion marker Wicket sets itself, or an OPAL view that serves the
file listing without the staged client-side render.

---

*History of this note's own failure, kept because the fix is in the tree:*

**The second run was lost, and had to be re-run.** The 06:52 unattended run
started it in the background, hit its iteration cap, and ended — taking the
only copy of the output with it (see "What does not survive: a background
process" in `docs/agent-operating-model.md`, added because of this). It also
failed on the first retry for a plain reason: the course is configured as
`Softwaretechnologie (SoSe 26)`, not `Softwaretechnologie`.

In flight: the probe against `Softwaretechnologie (SoSe 26)` (the 160-section,
largest and most JS-heavy course), to confirm the zero-AJAX result isn't a
small-course artifact. **Its result lands in
`tmp/network-trace-Softwaretechnologie (SoSe 26).txt`** — the probe now writes
its findings to a file, so a killed session no longer loses them.

Once it finishes: write the conclusion into `docs/sync-speed-campaign.md` and
`docs/BACKLOG.md`, re-affirming the existing "not reachable by any approach
identified so far" position, now covering this lead too. The probe stays in
the tree, opt-in like `httpdiscovery_probe_test.go`, so a future doubt about
this claim can be re-checked in one command instead of rebuilt.
