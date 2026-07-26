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

## In flight: the maintainer's three dogfood decisions (2026-07-26)

Answers came in on the four blocked questions; see the backlog item for the
quotes. Three pieces of work, in this order:

1. **First-run introduction** (biggest). The start / no-courses-configured
   state must explain what to do, not just expose the picker. Currently: no
   config → wildcard course list → "Sync all courses" checked → picker hidden
   behind it, no explanation anywhere.
2. **Rename "List courses"** (`internal/gui/sync.go`, `#btn-list`). Wrong
   twice: costs a full crawl (210s/482s measured), and listing courses is not
   what it does — it reports per-course file counts, i.e. a preview of what a
   sync would fetch. Needs a name that says that, plus something about the
   cost.
3. **Walk scheduling in the browser.** Approved explicitly, including that it
   registers a real Windows scheduled task. Must clean up after itself —
   unregister whatever it creates, and never touch an existing real task.

NOT in scope: making discovery faster. The maintainer believes it is possible
but that is the sync-speed item, still needing their sign-off.
