# Backlog

The current state of work on opal-downloader. **This file is the answer to
"what should I do next?"** — read it at the start of a session, pick the top
item that isn't blocked, and get on with it.

Kept in git deliberately, so it survives a fresh clone, a reinstall, and a
lost `~/.claude` directory. Update it in the *same commit* as the work it
describes; a backlog that lags the code is worse than none.

Keep personal specifics out of this file — the repo is public. Absolute
paths, account details, and measured numbers that only make sense for one
machine belong in local memory, not here.

**Keep entries short.** An item says what is left and where the detail lives.
It is not the place to record what was already done — that goes in the commit
message, the relevant `docs/` file, or `docs/BACKLOG-archive.md`. This rule
exists because ignoring it grew the file to 1057 lines by 2026-07-31, most of
it closed work, until nobody could read it in one pass. Reintroducing history
here is the failure mode to watch for.

---

## Now

### The 2026-07-26 feedback batch needs your eyes
**Blocked:** on the maintainer looking at the GUI.

All ten items shipped and everything an agent can check is checked. What is
left is judgement: six pages changed shape (sync log, settings, the new
`/schedule` page, course picker) and no test can say whether they read well to
the person in front of them.

One decision on the record: **`internal/scraper/crawl.go` (1250 lines) stays
unsplit** — the most correctness-sensitive file here, with a documented history
of silent file loss from changes to it. Tidying buys nothing worth that risk.

### Sync speed: the lead is real, implementing it needs sign-off
**Blocked:** **open question for the maintainer — is the jsTree+MathJax
completion-signal approach worth attempting, given the risk profile?**

The standing goal (2026-07-21): a no-op sync should feel instant, ~30s, not
5+ minutes. `docs/sync-speed-campaign.md` is the full decision log — every
measurement and every rejected approach. Read it before reaching for one.

Where it stands: the 300ms MutationObserver settle-wait dominates the run
(94.2s of 210s) and three attempts to shorten it all lost files. Section-level
concurrency was tried with sign-off and rejected. On 2026-07-30 the course-nav
jsTree widget's `aria-busy="false"` was confirmed as a genuine render-complete
signal across 4 courses — but its ordering against MathJax is inconsistent, so
it is not a safe drop-in.

The block is not the idea, it is the code: `waitForInteractiveLinks` /
`waitForContentSettled` is where the silent file loss came from, and the
concurrency rewrite needed explicit sign-off for the identical reason. Next
step once given: combined wait condition behind an env flag, byte-for-byte A/B
against the 345-file ground truth (`scripts/compare-visit-runs.ps1`). A file
*count* is not acceptable evidence here — that has burned this project twice.

### Dogfood the whole first-run journey
**Blocked:** on the maintainer opening the GUI as a stranger would.

All four decisions from 2026-07-26 shipped. The journey is now a permanent test
(`internal/gui/first_run_journey_test.go`), every nav page loads in real
headless Chromium (`browser_walk_test.go`), and the live "List courses" path is
covered too (`live_list_walk_test.go`, `OPAL_GUI_LIVE_LIST=1`). What each pass
found is in `docs/BACKLOG-archive.md`.

Three things left that are the maintainer's call, not an agent's:

1. **"List courses" is not a quick list.** It crawls every section of every
   course — 210s and 482s measured. It sits next to "Sync" and reads like a
   cheap lookup. Rename, warn up front, or serve from the dashboard listing?
2. **A stranger who wants specific courses has to guess.** With no config,
   "Sync all courses" renders checked and hides the picker behind it.
   Reasonable default, discoverable only by unticking something.
3. **Nobody has looked.** The walks assert structure and behaviour, headless.
   They cannot catch a purely visual break, and `gui`/`main.exe gui` is still
   unexercised because `Run` opens a real window unconditionally on Windows.

Worth knowing independently of this item: **the scheduler's disable path has no
guard**, and `scheduler.TaskName` is a single global constant that the
maintainer's live daily sync is registered under.

---

## Next

### The .exe's own Explorer icon still shows the Go default
**Blocked:** needs the maintainer's OK to add a build-time dependency.

The running window shows the real icon since 2026-07-30. This is only the
file's icon before the program runs, which needs a build-time-embedded `.syso`.
Producing one needs `rsrc`/`goversioninfo` or a hand-built COFF object, and
this repo's "three direct Go deps total" framing makes a new dependency the
maintainer's call. **Open question: worth it, and if so, `rsrc` or by hand?**

---

## Noticed

Things seen while working on something else and passed over. Not commitments —
rough edges that would otherwise only exist in one session's context window.
Delete an entry when it is done, or when it turns out not to matter.

- **The resume runner's unlock/logon triggers have not been seen firing
  (2026-07-29).** Registered and accepted by Task Scheduler; the only honest
  proof is an event 107 in `Microsoft-Windows-TaskScheduler/Operational` after
  a real lock/unlock. Check with `scripts/register-resume-task.ps1 -Status` —
  a climbing `missed` count while work sits in `docs/RESUME.md` means the
  triggers did not take and event 332 is back.

- **A killed background `go test > log 2>&1` leaves a log with no marker that
  it is incomplete.** Hit 2026-07-28: a truncated log was indistinguishable
  from a real silent failure. An `EXIT:<code>` sentinel helps only if the kill
  lands after that line.

- **An unattended run cannot wait for a background job, and reports success
  anyway (2026-07-30).** The job dies with the run's own process; the fix sat
  uncommitted while `docs/RESUME.md` claimed otherwise. Half-detected now —
  `unattended-run.ps1` scores clean-exit-with-changes-but-no-commit as
  `run-left-uncommitted`. **Not** fixed: nothing notices an orphaned background
  job, and detection is not prevention. The lesson stands: commit first, verify
  second.

- **Why the User-Agent fix works is still a theory.** `cd1282c` demonstrably
  restores all 6 courses (345 files, byte-identical). But a standalone probe
  sending the same synthetic UA at low volume was served full pages, not stubs
  — so "OPAL flags that fingerprint" is not the whole story. Probably volume or
  interleaving with a live crawl; nobody has isolated it. Cite the result, not
  the mechanism. Anything this project adds that talks to OPAL over plain HTTP
  should look like the browser it runs next to;
  `internal/scraper/htmlstability_probe_test.go` keeps the only surviving copy
  of the string.

---

## Done recently

Newest first, one line each. **Anything needing more than a line belongs in
`docs/BACKLOG-archive.md`** — this section exists so a session can see what just
happened, not to hold the reasoning. Trim to roughly the last ten entries and
move the rest across.

- Moved 678 lines of closed work into `docs/BACKLOG-archive.md` (2026-07-31).
- Released the autonomy brakes: budget-guard advises instead of denying,
  autopilot caps raised from 4h/20 to 12h/60 (2026-07-31).
- Routed every live probe's diagnostic logs somewhere visible.
- Fixed the Blocked marker so the autopilot gate's parser actually reads it.
- Found a real completion-signal candidate: jsTree's `aria-busy`, across 4
  courses.
- Wired the app icon into the running window (WM_SETICON), rasterised from
  logoSVG.
- Gave every hook a heartbeat, so a silently dead hook is observable.
- Deleted the section change-detection cache, budget and all.
