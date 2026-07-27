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

**Is ctx.Route's tax fixed, or per matched request? (2026-07-27, ~19:40)**

All findings below are committed. Only the named patch is pending.

Today, settled and committed:

- `62d0515` — discovery reports an error instead of "0 courses" when every
  source fails.
- `3316eb3` — the preview-blocker slowdown is real (two pairs, ~26-31%).
- `4696ed9` — Abort vs Fulfill makes no difference. Guess dead.
- `07804e2` — **the blocking was never the cost. `ctx.Route` is.** Route
  installed and always calling `Continue()`: 274.6s against a 210.3s no-route
  ground truth. ~64s, ~30% of a run, for interception alone.

**In flight:** `internal/scraper/previews.go` patched *uncommitted* with an
`OPAL_PREVIEW_ROUTE_NULLPATTERN` switch that registers the route under
`**/no-such-path-xyz/**` — a pattern matching nothing — while everything else
stays identical.

Running: `OPAL_FILELIST=repeat1_nullpattern OPAL_BLOCK_FILE_PREVIEWS=1
OPAL_PREVIEW_ROUTE_NULLPATTERN=1 go test ./internal/scraper/ -run
TestFileListSnapshot -v -timeout 30m`

**Reading it:**

- **Near 210s** → the tax is per *matched* request. A narrower pattern (only
  subframe document requests) rescues the ~30 MB saving for free, and the
  blocker becomes shippable-by-default after one confirming pair.
- **Near 274s** → merely *having* a route on the context costs ~30%,
  regardless of pattern. Then `ctx.Route` is unusable on this crawl, the
  preview saving needs a browser-level setting instead (stop the fetch without
  interception), and — more importantly — every measurement this project has
  ever taken with a route installed is suspect, starting with the network
  trace that found the 30 MB in the first place.

Afterwards: `git checkout internal/scraper/previews.go`. The switch is a
measurement tool, not something to ship. Record in `previews.go`,
`docs/sync-speed-campaign.md` and the backlog. File list must diff empty
against `tmp/filelist-repeat1_before.txt`.
