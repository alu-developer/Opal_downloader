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

_Nothing in flight._

Questions 27, 28, 2, 6 and 30 all landed 2026-08-09 (autopilot, all local/no
live run): Q27 confirmed the warm-session prediction (4.03% total wall-clock
delta, but only 1.14% of it inside the crawl); Q28 pinned the rest on `go
test`'s own build/cache-staleness check; Q2 (the highest-ranked open item per
the 2026-08-07 report) closed by connecting Q1, Q9's `MenuTreeRenderer`
finding and `httpdiscovery.go`'s own design comment — the abandoned
2026-07-21 HTTP-first crawler failed on 2 of 6 courses because it never
walked the tree via the browser, not because of client-side rendering; Q6
closed as a stale premise the campaign had already retracted on 2026-07-30,
three days before this question was carried into the model file. Q30 opened
fresh (source-reading OpenOLAT's own GitHub repo, since `gh search code` is
currently returning empty for known-good queries — worked around via the
git-trees/contents API instead): OpenOLAT's folder browser does let a
participant bulk-download a whole `Ordner` subtree as one ZIP, no editor
rights needed, but this project's own `files.go` only ever reads a file's
size/modified date off the page it's rendered on, so nested-folder discovery
still costs one page load per level regardless — the lever is real but
bounded to the ~86s first-sync download floor, not the 207s crawl floor.
Appended the fifth-cycle report to `docs/sync-speed-model.md` per the
reporting cadence (recommendation: keep going).

**Maintainer decision, same day: the "one live-run batch per day" self-caution
this campaign had been applying is retired** — server load was never actually
bound by that (see `docs/server-load.md`'s real mechanisms, all unchanged),
it was just this campaign rationing its own live-run cycles. Proceeding
straight to Question 24 (correctness, top-ranked, overdue by two reports)
in the same run rather than waiting. See below/`docs/sync-speed-model.md`
for the outcome once it lands.
