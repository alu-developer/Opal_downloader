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

**In flight (2026-08-05, opal-downloader-autopilot, backlog item, not sync-speed cycle):**
Question 23 implementation landed (`internal/scraper/previews.go` and 5
page-creation call sites, commit "Question 23: rewrite inline-preview
blocking from ctx.Route to raw CDPSession") — `go build`/`go vet`/full
`go test ./...` clean, plus a new local no-account probe
(`previewsblocker_probe_test.go`, `OPAL_PREVIEW_BLOCK_PROBE=1`) confirming a
subframe FolderResource load is blocked and a main-frame one is not, against
a real headless Chromium.

**Byte-diff safety bar: FAILED. Question 23's raw-CDP implementation is NOT
safe and must not ship as-is (2026-08-05).**

`OPAL_FILELIST=before`: 349 files, 193.66s, clean, no warnings.
`OPAL_FILELIST=after OPAL_BLOCK_FILE_PREVIEWS=1`: **316 files, 33 short.**
`diff tmp/filelist-before.txt tmp/filelist-after.txt` isolates every missing
file to one section: "Softwaretechnologie (SoSe 26)" / Part-3 (CourseNode/
1615865126729195011). The after-run's own log carries the mechanism's usual
fingerprint: `warnShowAllTruncated` fired for exactly that section
("expansion completed but added no files (18 file rows before, 18 after; 72
raw rows before, 72 after)") - the before-run logged this warning nowhere.
config.yaml has `course_concurrency: 1` and `section_concurrency: 1`, so this
is not the known contention-loss mode from Questions 16/17/19-22 - whatever
broke here did so within one section, alone.

**In progress:** a scoped diagnostic probe
(`internal/scraper/previewblockshowall_probe_test.go`,
`OPAL_PREVIEWBLOCK_SHOWALL_TRACE=1 OPAL_BLOCK_FILE_PREVIEWS=1 go test
./internal/scraper/ -run TestPreviewBlockShowAllRegression -v -timeout
10m`) is running now, scoped to just this one course, with debug-click
auditing on, to read `expansionSignalled`/`signalWaitErr` and the
`showall-expand-poll` trace for Part-3 directly instead of guessing from the
file count. Prediction written into that file's doc comment before running:
expansionSignalled=true (Wicket's click signal is fine) and the poll trace
sits flat at 18 candidates for its whole budget (the DOM patch never lands),
which would point at the preview blocker's Fetch-domain burst interfering
with Chrome applying the AJAX response - not at Wicket's own signal. Output
goes to `tmp/question23-diagnosis.log`.

**When this lands:** write the diagnosis into `docs/sync-speed-model.md`
(Question 23 does NOT close as shipped - reclassify to "closed: raw CDP
preview-blocking is unsafe, `OPAL_BLOCK_FILE_PREVIEWS` stays off" or open a
new question for a fix, depending what the trace shows) and into
`docs/BACKLOG.md`. The flag is already off by default and the code was
already safe to leave as-is even before this - this finding does not change
what any real user's sync does, only whether this implementation can ever be
turned on.

---

**Two sync-speed cycles done today (2026-08-04). Question 22 (real-account,
deferred, still open) then Question 8 (local-only, closed, decisive).**

Question 22's first cycle: the failure did not reproduce, so `signalWaitErr`
was never read on a failing sample — but the 2 clean runs (167ms/177ms) sit
in the same tight band as Question 21's 2 failing runs (196ms/206ms), 4
samples inside a 40ms span with no outlier, weakly favouring
`context-destroyed` over "pure delay" without confirming it. Deferred to a
later cycle — real-account load from this sub-thread is now 10 two-course
contention crawls today (`docs/server-load.md`).

Question 8 (picked up specifically because it needs no real account): closed
with a clean local probe (`ctxroutecost_probe_test.go`,
`OPAL_ROUTE_COST_PROBE=1`, 3 repeats, no OPAL login) — cache-off is 60.7% of
the `ctx.Route` tax, the Fetch pause/resume round trip only 3.1%, and raw CDP
genuinely decouples them (a `Fetch.enable` session held the cache intact in
all 3 repeats, no `Network.setCacheDisabled` call needed). That refutes
"Playwright couples the two rigidly" — it's `ctx.Route`'s own driver-side
choice, not a CDP requirement. Opens **Question 23**: rewrite
`blockInlineFilePreviews` (`previews.go`) to drive `Fetch` through a raw
`CDPSession` instead of `ctx.Route`, to keep the previews saving while
mostly dropping the tax — an implementation task, not a probe, still needs
the byte-diff safety bar before shipping. Full write-up in
`docs/sync-speed-model.md`'s "Previous experiment (Question 8, closed)" and
"Next experiment" sections. (The first version of the probe hung on its 3rd
repeat from a reentrancy bug in its own event handler — fixed, documented in
the commit and the model file, not a finding about `ctx.Route` itself.)

Next up, either is available: **Question 22** on a later cycle (real-account,
deferred for load), or **Question 23** (local implementation + a real-account
byte-diff before shipping, bigger scope than a probe).

---

**Do not run Question 17's concurrency=1 control run.** It was the "next up" here
until 2026-08-03 and is now unnecessary: Question 17 was answered from the
archived run log instead (`tmp/frage16-run.log`, 4/4 correlation with
`warnShowAllTruncated`). Server-side variance is refuted, so there is nothing for
that run to rule out. No env knob needed, no probe change needed.

**Question 18 is closed and its alarm was false** - no files were ever missing
there, the detector was counting table rows instead of file rows and flagging an
enrolment table. Fixed and re-verified live the same day. If you find an older
note claiming the 345-file ground truth is short, it is wrong; that was my
prediction, and the run refuted it.
