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

**Isolating the preview blocker's cost (2026-07-27, ~19:20).**

Everything below is committed except the one deliberate patch named at the end.

Settled today, in order:

- `62d0515` — discovery reporting "0 courses" instead of an error when every
  source fails. Done, tested, green.
- `3316eb3` — the A/B repeat. Slowdown is **real**: 248.3→324.3s (+30.6%) and
  210.3→265.0s (+26.0%), byte-identical 345-file lists both times.
- `4696ed9` — the abort-error-state guess is **dead**. `route.Fulfill` (empty
  200 `text/html`) came back 272.0s against `Abort`'s 265.0s. How the request
  is refused does not matter.

**In flight:** `internal/scraper/previews.go` is patched *uncommitted* with an
`OPAL_PREVIEW_ROUTE_NOOP` escape hatch — the route is installed and matches,
but always calls `route.Continue()`. Nothing is blocked; only the interception
happens.

Running: `OPAL_FILELIST=repeat1_routenoop OPAL_BLOCK_FILE_PREVIEWS=1
OPAL_PREVIEW_ROUTE_NOOP=1 go test ./internal/scraper/ -run TestFileListSnapshot
-v -timeout 30m`

**How to read the result** — three reference points, all same session, same
evening, all 345 files:

| condition | wall clock |
|---|---|
| no route at all (ground truth) | 210.3s |
| route + Abort | 265.0s |
| route + Fulfill | 272.0s |
| route + always Continue | *this run* |

- Comes back near **265s** → interception itself is the tax. The blocker is
  innocent, and the ~30 MB saving could be had for free if the route were
  installed more narrowly (it currently matches `**/FolderResource/**`, which
  is every file request including real downloads, and Playwright routing
  disables the browser cache for everything it matches).
- Comes back near **210s** → interception is free and the missing file really
  is what costs the time, most likely a subframe that never reaches a loaded
  state something is waiting on. That would make the blocker genuinely a
  speed/traffic trade-off and the campaign entry can say so and stop.

Either way: `git checkout internal/scraper/previews.go` afterwards — the
escape hatch is a measurement tool, not something to ship. Record the number in
`docs/sync-speed-campaign.md`, `previews.go`'s comment, and the backlog entry.
The file list must diff empty against `tmp/filelist-repeat1_before.txt`.
