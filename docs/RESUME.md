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

**Testing whether the settle wait can go entirely (2026-07-27, ~21:15).**

Autopilot was dead all session and is fixed and re-armed (`6c8a1cf`); the
backlog item that made it look like there was no work is unblocked.

**In flight:** `internal/scraper/crawl.go` patched *uncommitted* with an
`OPAL_SKIP_SETTLE_WAIT` switch that skips `waitForInteractiveLinks` entirely
and goes straight to the stability poll.

The argument, and it rests on tonight's early-read measurement rather than on
reasoning: the settle wait costs **94.2s of a 210s run** proving a page stopped
mutating, and the stability poll then re-reads until extraction stops changing
anyway - two mechanisms inferring the same fact. The probe measured that
content **only ever grows** (278/278 sections: never empty at the start, never
larger than the final read), so the shrink the poll could miss does not occur.
Skipping leaves `sectionCalm` false, so the poll opens on its *full* patience
streak - the conservative direction, and it may eat some of the saving.

Running: `OPAL_SKIP_SETTLE_WAIT=1 OPAL_FILELIST=noSettle go test
./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m`

**Acceptance is the diff, not the clock.** `tmp/filelist-noSettle.txt` must
diff **empty** against `tmp/filelist-repeat1_before.txt` (345 files, ground
truth). A file count is not acceptable evidence - the
`sectionContentRequiredStableReads` 4->1 attempt lost files byte-for-byte while
looking healthy, and tonight's probe caught a section that changed rows without
changing their count.

- Identical **and** meaningfully faster than 210.3s -> the settle wait goes, and
  this is the first real win of the campaign. Confirm with a second pair before
  changing the default.
- Identical but not faster -> the poll absorbed the saving; keep the wait,
  record it.
- Any diff at all -> the wait is load-bearing beyond what the probe showed.
  Revert, record which sections differed.

Either way: `git checkout internal/scraper/crawl.go` unless it ships, and write
the number into `docs/sync-speed-campaign.md` and the backlog.
