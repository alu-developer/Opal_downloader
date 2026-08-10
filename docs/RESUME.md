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

**In flight (2026-08-10, autopilot): Backlog "Now" item, Question 36 Step B2.**
`scrapeCoursesHTTPFirst` (`internal/scraper/httpfirst.go`) is written, unit
tested, and committed (`7740858`), gated behind `OPAL_HTTP_DISCOVERY=2`.
Next: a live byte-diff against the browser baseline using the established
`TestFileListSnapshot` harness (`internal/scraper/filelist_probe_test.go`):

    OPAL_FILELIST=browser-baseline go test ./internal/scraper/ -run TestFileListSnapshot -v -count=1 -timeout 30m
    OPAL_FILELIST=httpfirst OPAL_HTTP_DISCOVERY=2 go test ./internal/scraper/ -run TestFileListSnapshot -v -count=1 -timeout 30m
    diff tmp/filelist-browser-baseline.txt tmp/filelist-httpfirst.txt

Prediction already registered in `docs/sync-speed-model.md` Question 36 Step
B2: 90-110s, zero diff, counts as failed above 130s or on any non-empty diff.
If it passes: write the result into Question 36, and per the "Now" item this
still goes as a PR (not straight to master) before it can become a default -
open it against `master`, do not merge unattended. If it fails: diagnose from
the diff (course/section/name/URL are all in the snapshot), do not just retry.
