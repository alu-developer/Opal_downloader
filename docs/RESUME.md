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

The SessionStart hook reads this file and hands it to the next session. The
scheduled resume runner also treats a non-placeholder file here as "there is
work", so leaving stale content in it will wake an unattended run for nothing.

---

## In flight: the maintainer's 2026-07-30 evening batch

Four decisions given, plus one new standing request. Budget was already
critical when they landed, so this list is the handover.

**1. Section cache: DELETE IT. Approved** ("ok, wenn du sagst"). Remove
`internal/sectioncache/`, `internal/sectionhash/`,
`internal/scraper/sectioncachewiring.go` (+ its two tests,
`sectioncache_crawl_test.go`), the `OPAL_SECTION_CACHE` wiring in
`crawl.go`/`scraper.go`, and the `codebudget_test.go` entry. Keep the
*measurements* in `docs/sync-speed-campaign.md` — the record of why is the
point; the code is not. Rejected twice on numbers (warm 273.3s vs 241.0s
control, 3.9% hit rate).

**2. Volatility diagnostic: my call, and the answer is no** ("mir wurscht").
Third round on a twice-rejected approach, and `docs/server-load.md` prices each
attempt at a full crawl of OPAL. Not doing it. Say so in the backlog entry so
nobody reopens it looking for a decision that was never recorded.

**3. The real one: "hier wurde scheinbar gepfuscht."** They looked at the GUI —
"nein, es ist noch nicht perfekt" — and will review it properly soon. Their
diagnosis, which is the actual task:

> "es fehlt iwie eine prüfinstanz oder so die sagt: ja, jetzt passts... und es
> werden halt meist minimalinvasive oder so halb-änderungen gemacht, nicht
> wirklich so wie man es am liebsten hätte."

Two distinct complaints, do not merge them:
- **No acceptance authority.** Nothing ever declares work *finished to a
  standard*. Tests pass, a commit lands, the backlog entry grows another
  paragraph — none of that is "this is now good".
- **Half-changes are the default.** The minimal-invasive fix is chosen
  habitually rather than the right one. (Live evidence in this very repo: the
  hardcoded probe User-Agent under "Noticed" was written by hand explicitly "to
  keep the fix small enough to verify in one session".)

Asked for: find the underlying cause, design a *long-term* fix, implement it.
Not a checklist nobody reads — that is the same failure one level up.

**4. `.ico`: yes, I do it myself.** The blocker recorded in BACKLOG's "Next"
was "needs an SVG renderer". That is wrong on Windows: **WPF is an SVG path
rasteriser** — `System.Windows.Media.Geometry.Parse` takes the SVG path
mini-language, `RenderTargetBitmap` rasterises, all in .NET Framework via
PowerShell, no new dependency and nothing to add to `go.mod`. Plan: a script
that reads the path data out of `logoSVG`, renders 16/32/48/64/128/256, packs a
multi-size `.ico`, checks it in. Then the runtime wiring (`WM_SETICON` via
`syscall` on `w.Window()`) and/or a `.syso` for Explorer.

**5. NEW STANDING REQUEST — self-monitoring of my own workflow.** Verbatim:

> "Fange bitte selbständig an, deinen workflow zu überwachen. Also überleg dir
> was geschedultes, oder ne hook, oder keine ahnung, du weißt, was am besten
> funktioniert. Dann implementierst und testest du das, so dass du quasi auch
> bissl selbst den workflow prüfen kannst und ich dir nicht immer sagen muss:
> hier zu viele tokens, da hook nicht funktioniert, hier zu wenig dran
> gearbeitet usw."

Three named symptoms they are tired of reporting: **too many tokens**, **a hook
silently not working**, **too little actually worked on**. Note that all three
already happened and all three were caught by *them*, not by the machinery.

This and item 3 are the same problem seen from two sides — nothing in this repo
observes its own work and judges it. Build one thing, not two: an
end-of-session/periodic self-audit that produces a verdict, plus liveness
proof for the hooks themselves (a hook that is dead currently says nothing,
which is exactly how 2026-07-27 was lost).

### Order of work

1. Section-cache deletion (mechanical, approved, cheap). ✅ do first
2. `.ico` (self-contained, no decisions left).
3. The self-audit / acceptance instance — items 3 + 5 together. The big one.

### Do not lose

- The maintainer will do their own GUI review "demnächst". Do not treat the
  two "needs your eyes" backlog items as answered.
- Budget was CRITICAL (5h ≥87%) when this list was written. Commit per step.
