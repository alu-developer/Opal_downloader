# opal-downloader

**Only the maintainer edits this file.** Ask before changing anything here,
including before adding a new entry.

---

## PRs, Branches and so on.

**No PRs.** Never open one, never close one, never merge one. If an open PR
turns up on GitHub, say so and leave it — that call is the maintainer's.

**No standing branches.** Work lands as a direct commit on `master`, pushed.

**Every agent that writes to the repo works in its own worktree** (Agent tool:
`isolation: "worktree"`). Read-only agents don't need one. The main session owns
the checkout and does the merging.

**Finishing means merging.** When an agent is done, merge its worktree branch
into `master` and delete branch and worktree — even if the work is partial or
tests fail, and then say plainly what is broken. A worktree left behind is
invisible work, which is the whole reason PRs are gone.

---


## Mistakes
**This file also collects mistakes.** Beliefs acted on here that turned out
to be false, each with the correction.


**"Unattended runs can't log in, because 2FA needs the maintainer."**
False. TU-Fast is installed in the dedicated login profile and completes
credentials *and* 2FA by itself — `login`/`sync`/`list` trigger it automatically
when the saved session is stale, with nobody at the machine. Live-verified
2026-08-01: expired state → auto-login → 8 courses in 3.7s, no click. So never
report "needs the maintainer for 2FA or fresh cookies", and never treat an
expired session as a blocker. Run the command. Only a run that actually failed is
a blocker, and then quote its error.

**"Needing a live crawl is a reason to defer a question."**
False, and the maintainer said so again on 2026-08-10. Live crawls against the
real account are wanted, not rationed — server load is already bounded by the
rate limiter and backoff in `docs/server-load.md`. Never park an item as
"waits for a fresh day", "needs real-account load", or "no question answerable
without a live run". Just run it. Write the prediction down first, then run.
