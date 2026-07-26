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

**Sync speed: a lead nobody has pulled, with numbers.** The maintainer pushed
back (2026-07-27) on the campaign being treated as dead. They are right that
nobody ever asked what the ~1s per section is actually *made of*. Reading the
constants rather than the conclusions:

- `contentFallbackWaitMs = 1100ms` (navigation.go) - the settle wait before
  extraction even starts.
- `sectionContentPollIntervalMs = 150ms` x `sectionContentRequiredStableReads
  = 4` = **600ms minimum** of our own polling per section, after that.
- Sections with a "show all": `showAllExpansionPollIntervalMs = 400ms` x
  `showAllExpansionRequiredStableReads = 3` = **1200ms more**.

284 sections x ~600ms is ~170s of *self-inflicted* wait against a ~227s total
run. That is not OPAL being slow. That is our own poll loop, and it has never
been measured - only reasoned about.

**Do not just lower these.** The 4-stable-reads value is load-bearing: a
1-read version was live A/B tested and lost files byte-for-byte identically to
the unfixed code (see the long comment at crawl.go:894). The question is not
"is 4 too many" but "why is a *time-based* poll the mechanism at all" - the
files are arriving via a Wicket AJAX response the browser already receives.

Next step, and it is measurement, not a change: instrument per-section where
the wall time actually goes (settle wait vs poll loop vs extraction), run it
once against the real account, and find out whether the poll loop is really
three quarters of the runtime. Only then decide anything.

`docs/sync-speed-campaign.md` should get this either way - a negative result
is still the first real measurement of where the second goes.
