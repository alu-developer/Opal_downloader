# Resume note

Scratch state for work that is **in flight right now**. Kept in git so it
survives a killed turn, a dead session, and a fresh clone.

`docs/BACKLOG.md` says what should happen and stays tidy. This file is allowed
to be messy: it is the thought that would otherwise only exist in a context
window, and a context window does not survive the usage limit being hit
mid-turn.

**Keep it current while working**, not at the end - the end is exactly the part
that does not always arrive. Update it whenever the answer to "what am I doing
and what's next" changes materially. When the work lands, clear it back to the
placeholder line below.

The scheduled Desktop task's prompt reads this file first, so stale content
here sends an unattended run after work that is already done. Clear it.

---

## In flight (2026-08-03): Frage 16 live run + PR #131 CI verification

**Frage 16 (sync speed, the main line).** Prediction is written and committed
*before* the probe existed (`docs/sync-speed-model.md`, "Nächstes
Experiment"): no file difference across four runs, savings below Frage 15's
28.7%. Probe is `internal/scraper/debouncecontention_probe_test.go`, driving
`collectCourseFilesConcurrently` with two real courses so the contention is
real rather than nominal.

Command:

    OPAL_DEBOUNCE_CONTENTION_TRACE=1 go test ./internal/scraper/ \
      -run TestDebounceOverrideUnderContention -v -timeout 60m

**The result lands in `tmp/debounce-contention-probe.txt`** (written by the
probe itself, so a killed session does not lose it). Four runs: baseline
500ms/6000ms twice, then override 150ms/4000ms twice - baseline first on
purpose, so a run that dies partway still leaves the unchanged configuration
measured.

Watch for the two known live hazards, both in `docs/BACKLOG.md` under
Noticed: a concurrent Routine colliding on the shared browser profile (hangs
rather than surfacing `ErrProfileLocked`), and the unexplained 300s login
timeout, which now folds the stuck page URL into its error.

**PR #131.** `release.yml` on `fix-installer-playwright-cache-path` got a
`workflow_dispatch` trigger plus a step that silently installs the built
installer and asserts Chromium lands under
`%USERPROFILE%\.opal-downloader\ms-playwright`. Run 30801186431 was dispatched
against that branch. Green means the installer bug is exercised, not merely
reviewed, and the PR can merge; the branch/PR trigger for it was "could not
verify", so the verification is exactly what removes it.
