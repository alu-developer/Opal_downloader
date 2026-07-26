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

## In flight: repeating the course_concurrency=2 measurement

The backlog's top item. It was filed rather than acted on because one run pair
is not grounds for changing a default, and this is the repeat.

### What is already known
| run | course | files | Analysis | note |
|---|---|---|---|---|
| live GUI list (earlier today) | 2 | **345** | 30 | clean |
| run A | 2 | **336** | 21 | −9 |
| ground truth | 1 | **345** | 30 | — |

So course=2 is **intermittent**, not reliably broken — which matches the
campaign's 2026-07-17 entry ("3 of 4 runs") and is the reason a single clean
run proves nothing either way. That cuts both directions: my run A was not
necessarily a fluke, and my GUI run was not necessarily proof of health.

### Method
Three consecutive `list --course-concurrency 2 --section-concurrency 1` runs,
same binary, counting files and Analysis specifically. Serial ground truth is
345/30, established twice today.

### Decision rule, fixed in advance so the result cannot be rationalised
- **Any run below 345** → course concurrency 2 is unsafe. Change
  `DefaultCourseConcurrency` to 1, since it is measurably not faster either
  (228.2s vs 227.9s) and therefore costs files for nothing.
- **All three clean** → run A was the outlier; leave the default alone, record
  4-of-5-clean honestly, and say plainly that intermittent loss at this rate
  cannot be ruled out by five runs.

The maintainer's live `config.yaml` sets 2 explicitly, so the code default does
not protect them either way — that edit is theirs to approve, and is flagged
in the backlog rather than made here.
