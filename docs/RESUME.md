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

**Is the settle wait's time needed, or only its verdict? (2026-07-27, ~21:35)**

Committed and settled tonight: the settle wait **cannot** simply go (`f54fa7f`)
- skipping it is byte-identical but **51% slower** (317.1s vs 210.3s), because
it produces the `sectionCalm` verdict that lets the stability poll open
impatient. It is not the tax; it is what keeps the tax down.

**In flight:** `internal/scraper/crawl.go` patched *uncommitted* with
`OPAL_SKIP_SETTLE_WAIT_CALM`, which skips the wait **and** asserts the verdict
it would have produced. That separates the two things the previous run
conflated.

Running: `OPAL_SKIP_SETTLE_WAIT_CALM=1 OPAL_FILELIST=noSettleCalm go test
./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m`

**Acceptance is the diff.** `tmp/filelist-noSettleCalm.txt` must diff empty
against `tmp/filelist-repeat1_before.txt` (345 files). The risk here is real and
named: an optimistic verdict is exactly how the
`sectionContentRequiredStableReads` 4->1 attempt lost files at a staged-render
false plateau, and it looked healthy while doing it. A count proves nothing.

- Diff empty **and** faster than 210.3s -> only the verdict matters, the 94.2s
  of waiting is recoverable, and this is the campaign's first real win. Confirm
  with a second pair before touching the default.
- Diff empty but not faster -> the poll was never the bottleneck; record and
  stop pulling this thread.
- **Any diff** -> the wait's time is genuinely load-bearing. Revert, record
  which sections lost rows; this is the expected outcome given the 4->1
  history, and it would close the question for good.

Afterwards: `git checkout internal/scraper/crawl.go` unless it ships. Numbers
go to `docs/sync-speed-campaign.md` and the backlog.
