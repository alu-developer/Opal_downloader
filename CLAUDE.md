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
