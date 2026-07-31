# Rate-limit readout (Claude Code tooling)

Shows how much of the plan's 5-hour and 7-day windows is left:

```powershell
powershell -NoProfile -File "C:\Users\alois\.claude\ratelimit.ps1"
```

**The live copy is `~/.claude/`** — that is where Claude Code's `statusLine`
setting points and where the cache and credentials live. This directory is the
versioned copy. After changing either one, copy it across and run the tests.

## Why it is versioned here

It broke five times in eleven days (2026-07-20 → 07-31), and the fifth break
was caused by the first four being forgotten. The rule "a window past its reset
time is meaningless, never show its number" was solved correctly on 2026-07-21
inside `.claude/hooks/autopilot-gate.ps1`. Those hooks were deleted on 07-31 —
correctly, they had become the thing that stopped the work — and the readout was
rebuilt the same afternoon without the rule. It then reported 87% used against a
real ~0%, telling the maintainer he was out of budget at the moment he wasn't.

Deleting code deletes what it knew. So the rules now live in one tested place.

## Design

Everything this reads is somebody else's undocumented internal: a beta usage
endpoint, the CLI's credential file, the shape of its `rate_limits` payload.
None of it has a contract, so it *will* keep changing. "Always correct" is
therefore not an achievable goal, and chasing it is what produced five rounds of
fixes. The goal is **never confidently wrong**: any path that cannot produce a
trustworthy number says so instead of printing a plausible one.

| file | role |
|---|---|
| `ratelimit_core.py` | all trust rules — parsing, ageing, window validity, rendering. One copy. |
| `ratelimit_fetch.py` | network + backoff. Detached; never called inline. |
| `ratelimit_show.py` | the CLI readout |
| `ratelimit.ps1` | thin wrapper so the documented command keeps working |
| `statusline.py` | the status line; shares the trust rules |
| `test_ratelimit.py` | 74 tests, almost all of them failure paths |

## Tests

```bash
python scripts/claude-tooling/test_ratelimit.py
```

`CLAUDE_RATELIMIT_DIR` points every module at a temp directory, so no test can
touch the live cache or the real credentials, and no test needs the network.

They spend nearly all their effort on bad-day paths, because every previous
break was found by the maintainer noticing a wrong number and never by a test —
the only thing ever exercised was the happy path, which is by definition the
state that was just fixed. Writing them surfaced four real bugs immediately:
the rolled-over window, a `next_attempt` clamp that could never recover, a
renamed API field that would silently discard the last good reading, and a
status-line crash on a BOM that would also have stopped the cache refreshing.
