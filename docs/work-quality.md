# Why the work comes out half-done, and what to do about it

Raised by the maintainer on 2026-07-30, after looking at the GUI:

> "nein es ist noch nicht perfekt... ich wundere mich, dass hier scheinbar
> gepfuscht wurde. Es fehlt iwie eine prüfinstanz oder so die sagt: ja, jetzt
> passts... und es werden halt meist minimalinvasive oder so halb-änderungen
> gemacht, nicht wirklich so wie man es am liebsten hätte."

Two complaints, and they have different causes. This file separates them,
names the causes with evidence from this repo rather than from intuition, and
says which are fixed by machinery and which are not fixable by machinery at
all.

---

## Complaint 1: nothing ever says "now it's good"

Correct, and it is structural. Everything this project currently checks is a
*floor*, not a standard:

| check | what it actually asserts |
|---|---|
| `go test ./...` | nothing I wrote today contradicts something I wrote earlier |
| `go vet`, `gofmt` | the code is syntactically unremarkable |
| `codebudget_test.go` | the repo did not get much bigger |
| `pre-push-gate.ps1` | the above ran before a push |

None of them can fail because the work is mediocre, incomplete against its own
description, or solves a smaller problem than the one reported. They are
regression detectors. A regression detector answers "is this worse than
before", never "is this good".

**The tests are written by the same agent, in the same turn, as the code.** So
a half-change arrives with tests that assert exactly the half that was built,
and the suite goes green *because* the work was scoped down. Green is
therefore evidence of internal consistency and of nothing else. This is the
mechanism, and no amount of adding more tests of the same kind fixes it.

**The one independent reviewer was deliberately removed.** A self-review pass
existed in the old `queue-run` workflow and was cut on 2026-07-17 for token
efficiency, on the grounds that it had found nothing in 4 attempts. That
measurement was taken over four tasks. It has been absent ever since.

## Complaint 2: half-changes are the default

Also correct, and the cause is more uncomfortable: **this repo's own hooks
instruct it.** `.claude/hooks/budget-guard.ps1` fires as the budget climbs and
says, verbatim:

> "avoid starting work that only pays off if a long turn completes"

That is a correct instruction for surviving a kill and a direct instruction to
prefer the small fix. Under budget pressure — which is most of the time, since
the guard escalates within every long session — the cheap fix is not merely
tempting, it is what the harness asks for. The maintainer is seeing the
intended behaviour of a mechanism nobody weighed against quality.

Two documented instances, both self-reported in this repo before the
maintainer ever complained:

- The section-cache probe's User-Agent was hardcoded as a literal "to keep the
  fix small enough to verify in one session" (BACKLOG, Noticed).
- `docs/BACKLOG.md` grew to ~1500 lines because appending a paragraph is
  cheaper than closing an entry, and an entry that is never closed is a
  half-change that survives in prose.

Two further causes worth naming:

- **No standard was ever written down.** "Wie man es am liebsten hätte" exists
  only in the maintainer's head. An agent cannot hit an unstated bar, and will
  reliably substitute "smallest change that makes the symptom go away".
- **The backlog rewards volume over completion.** Nothing in it produces a
  verdict. Entries accumulate measurements; none of them end.

---

## What machinery can and cannot fix

**Can:** whether the loop is *working*. Three failures the maintainer has had
to report by hand — too many tokens for too little, a hook silently dead, too
little actually built — are all observable from outside the agent's own
judgement. Commits per session, tokens per commit, hook liveness, tests
added versus lines added, whether a turn ended with everything committed.
Those are measurements, and measurements can be automated. That is
`.claude/hooks/` work and it is the honest scope of self-monitoring.

**Cannot:** whether the result is good. An agent auditing its own output
grades against the same understanding that produced it. The 0/4 self-review
result is weak evidence but it points the same way. **Do not build a hook that
claims to certify quality** — that reproduces the original problem with a
green checkmark on top, which is worse than having no check.

So the split is:

1. **Machine:** watch the *process*, report anomalies, refuse to call a
   session finished when it demonstrably was not. Never judge the product.
2. **Human:** the maintainer's review is the acceptance instance, and it
   should be *cheap and regular* rather than avoided — a short, specific list
   of what to look at, produced automatically, instead of "please look at the
   GUI".
3. **Written standard:** what "done properly" means here, so the smallest
   change stops being the default answer. Draft below; the maintainer amends
   it.

## Definition of done (draft — amend freely)

A change is done when all of these are true, and it is **not** done if any of
them was skipped for budget reasons:

1. It fixes the *cause*, or it says in the commit message which cause it is
   not fixing and why that was the right call. "Kept small to fit the session"
   is a reason to split the work, not a reason to ship half of it.
2. Nothing left behind contradicts it — no doc, comment or backlog entry still
   describing the old behaviour. (This repo has had several; they are how a
   reader learns to distrust the docs.)
3. It is verified the way its own failure mode demands: a UI change by looking
   at it, a data-loss-capable change by a byte-for-byte diff, a hook by a test
   that fails when the hook is removed.
4. Anything deliberately left out is written where the next person will hit
   it — the backlog, not the chat message.
5. The budget was not the deciding factor in any of the above. If it was, the
   work is *paused*, not finished, and `docs/RESUME.md` says so.

Rule 5 is the one with teeth, and it is the direct answer to complaint 2.
