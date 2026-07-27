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

**Nothing in flight. One measurement is owed before the preview blocker can be
turned on.**

The blocker is built, verified safe, and **off by default**
(`OPAL_BLOCK_FILE_PREVIEWS=1` enables it). Paired full-account A/B, 2026-07-27:

| run | files | wall clock |
|---|---|---|
| previews kept (ground truth) | **345** | **248.3s** |
| previews blocked | **345** | **324.3s** |

`diff` of the two sorted file lists (course, section, name, URL) was **empty**.
Safety is settled: nothing is lost.

Speed came back the wrong way — 31% slower, outside the 212–245s band this
account has measured before. One pair is not proof of a slowdown, but it is the
only comparison that exists, so it ships off rather than on.

**The next measurement, and it is cheap to run:** repeat the pair (ideally
twice more) to find out whether 324.3s is real or an outlier. Command is in
`internal/scraper/filelist_probe_test.go`'s doc comment. If it is real, the
interesting question is *why* — the guess worth testing first is that an
aborted subframe leaves the parent page churning (an error state to render),
which is exactly what the 300ms settle-wait debounce is watching for. If so,
`route.Fulfill` with an empty body might behave differently from `route.Abort`;
that was reasoned about and guessed the other way round when this was built.

Whatever the timing turns out to be, blocking still saves OPAL ~30 MB per
course per pass, which is its own argument under `docs/server-load.md` — but
that is the maintainer's call, not an assumption to bake into a default.
