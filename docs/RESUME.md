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

**Closing a gap in my own server-load work.** internal/polite covers page
navigations via gotoPolitely, and docs/server-load.md is framed as bounding how
hard the tool hits OPAL - but the actual file downloads go through
reqCtx.Get on Playwright APIRequestContext, three call sites, none of them
limited. Those are the concurrent, byte-heavy requests. The ceiling missed the
load that matters most.

Plan: route those three through the limiter too, then measure. A routine no-op
sync downloads nothing so should be unaffected; a first sync of 345 files has a
250ms-per-file floor, which needs measuring rather than assuming.
