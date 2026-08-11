# opal-downloader

**This file collects mistakes.** Beliefs I acted on here that turned out to be
false, each with the correction. Nothing else belongs in it.

**Only the maintainer edits it.** Ask before changing anything here, including
before adding a new entry.

---

## Mistakes

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

**"Opening a PR for a risky change gets it a second look before it lands."**
False, 2026-08-11. Two sessions independently built the same backlog item as
separate PRs, five hours apart, because an open PR's own backlog edit only
lives on its branch — master still reads "open work" until something merges,
so a later session can't see the PR exists. The "second look" never happened
either way; it went through a `/decide` round like everything else waiting on
the maintainer. **Never create branches or PRs in this repo.** Commit
directly (to `master`, unless mid-conflict-resolution). Worktrees are fine —
use them for isolation (checking out a different ref, verifying something
without disturbing the shared checkout) — but don't turn a worktree into a
standing branch or push it anywhere as a PR.
