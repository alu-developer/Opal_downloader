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

The scheduled Desktop task's prompt reads this file first, so stale content
here sends an unattended run after work that is already done. Clear it.

---

**2026-08-18, autopilot.** Question 44's policy half shipped and pushed
(commit a9da8cb) — negative-manifest-entry-with-backoff for repeatedly
failing downloads, `internal/syncer/syncer.go`. Prediction for a two-run
live verification is committed in `docs/sync-speed-model.md`'s "Next
experiment" section. Currently running that verification: `main.exe sync`
twice back to back against a scratch config
(`tmp/policy-verify/config.yaml`, scratch `download_path` under this
worktree's own `tmp/`, real account/session) in this worktree
(`.claude/worktrees/indexed-zooming-sunset`). If this session dies
mid-verification: the code is already shipped and safe regardless of the
outcome (worst case the prediction is refuted and the doc needs the result
written in) — build with `go build -o main.exe .` from the worktree **root**
(not `./cmd/opal-downloader` - that directory is `package opaldownloader`,
a library, not `package main`; building it silently produces a `.a` archive
copied to whatever `-o` name you gave it, no linker ever runs, and it is
not executable - check the first two bytes are `4d 5a`/"MZ" before trusting
any build here), then run `main.exe sync --config tmp/policy-verify/config.yaml`
twice from that worktree, record the two runs' `Done. downloaded=... skipped=...
errors=... backing_off=...` lines and wall-clock times into
`docs/sync-speed-model.md`, commit, push, then continue with backlog/phase
2/phase 3 as normal.
