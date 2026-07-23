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

The SessionStart hook reads this file and hands it to the next session.

---

## In flight: usage-limit incident fix (2026-07-23)

**Why:** a run was killed mid-turn by the 5h limit around 12:00–14:00 local on
2026-07-23. Nothing recorded it; diagnosis meant comparing commit timestamps
(last commit 11:46) against window-reset arithmetic. Every guard the repo had
was on the **Stop** hook — between turns — so a single long turn ran past the
budget unwatched. Session record showed 1–2 continuations against a cap of 20:
the gate never got a chance to fire.

**Done and committed:**

- `budget-lib.ps1` — one shared floor reader; per-window `resets_at` rule
  (expired window ⇒ unknown, not "high"). Closes the freshness blind spot
  `agent-operating-model.md` flagged.
- `budget-guard.ps1` — PreToolUse, no matcher (all tools). The first check that
  runs *during* a turn. Escalating rungs (50/70/80 on 5h, 65/80/85 on 7d),
  throttled to once per rung then every 15 min. Denies `Agent` at rung 3.
  Replaces `rate-limit-gate.ps1` (deleted).
- `turn-failure-checkpoint.ps1` — StopFailure. Records the kill to
  `LAST_FAILURE.json` + `turn-failures.log`, and captures WIP via
  `git stash create` + a `refs/wip-checkpoints/*` ref (no working-tree change).
- `session-start-autopilot.ps1` — surfaces the last failure and this file;
  won't arm a full stretch on a spent budget (rung 3 ⇒ don't arm, rung 2 ⇒
  2h/6).
- `rate-limit-keepwarm.ps1` — `-NoWait`; the 42s cold-launch wait exceeded the
  Stop hook's 15s timeout and was silently killing autopilot. Stop timeout
  15s → 60s.

**Next action:** `scripts/test-hooks.ps1` — synthetic-stdin tests for all of
the above, wired into `scripts/dev.ps1 all`. Then update
`docs/agent-operating-model.md` (§2b is now wrong: it sells a session-only
CronCreate resume that didn't fire) and `docs/BACKLOG.md`.

**Verified so far:** `budget-guard.ps1` fired live at rung 3 on a real Write
call (`5h >=8%, 7d >=85%`) and injected its checkpoint advice — so PreToolUse
`additionalContext` without `permissionDecision` does pass through
non-blocking, as the docs claim.

**Known unverified:** `StopFailure` has not been observed firing for real —
that needs an actual API kill. Tests drive the script directly via synthetic
stdin, which covers everything except whether the harness invokes it.

**Open question for the maintainer** (do not decide alone): automatic
*resumption* after a kill would need something that spends budget unattended
(a scheduled headless `claude`). That is their money and their call.
