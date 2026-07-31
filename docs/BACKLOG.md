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

Empty right now. That's not nothing left to notice — it means the next thing
belongs here the moment it's seen, not that the well is dry.

---

## Done recently

Newest first, one line each. **Anything needing more than a line belongs in
`docs/BACKLOG-archive.md`** — this section exists so a session can see what just
happened, not to hold the reasoning. Trim to roughly the last ten entries and
move the rest across.

- Closed the last Noticed item (the User-Agent-fix theory) with a decision
  instead of another probe: isolating the mechanism further would mean
  deliberately sending a higher-volume burst at the real OPAL server to try
  to reproduce the original degradation, which is exactly what
  `docs/server-load.md` exists to weigh before doing — "read before any
  change that would make the tool ask OPAL for more, or ask faster" applies
  to a curiosity probe the same as a shipped feature. The result
  (`cd1282c`'s fix demonstrably works, 345 files byte-identical) stays cited
  regardless of the unexplained mechanism; nothing to build here.
- Closed the "unattended run can't wait for a background job" Noticed item
  without building a detector: `docs/work-quality.md` names building more
  watch-the-process machinery as the exact anti-pattern to stop, and the
  actual fix already shipped this session as a *rule*, not code — the
  resume-runner's own prompt template already says "do not start anything
  that outlives this turn... commit first and leave the check as the next
  action." Behavior lives in the prompt, hooks stay for enforcement only.
- Fixed a stdin BOM silently breaking JSON parsing in 5 hooks (budget-guard,
  turn-failure-checkpoint, noticed-gate, autopilot-gate, pre-push-gate) —
  `scripts/dev.ps1 all` is fully green for the first time this session
  (2026-07-31).
- **Closed the sync-speed campaign's remaining question myself, per
  `docs/work-quality.md`'s instruction to decide rather than defer:** decided
  NOT to build the risky wait-shortening change autonomously. Reasoning in
  `docs/sync-speed-campaign.md`'s closing entry (2026-07-31) — the short
  version: the verified HTTP-hybrid (diff=0, all 6 courses) ships as an
  opt-in diagnostic; the only path to an actual speedup needs an unreviewed
  change to the crawl's highest-risk code, for an estimated (not measured)
  ~60-90s that still misses the original 30s target. CLAUDE.md ranks
  reliability over features and over ease-of-use; an autonomous, unattended
  turn is not the place to gamble with a real user's file sync on an
  estimate. This is a decision, not a stall - reopen it only with a
  maintainer watching the diff live, not by re-measuring.
- Resolved two Noticed items with real evidence, both against my own
  same-session claims: the resume runner's Logon trigger genuinely fires
  (Task Scheduler event 119, "due to user log-on", 2026-07-31 01:04:24 -
  confirmed, not just registered) and the "overlapping resume-runner
  launches" I'd called confirmed the same day was wrong - the four Task
  Scheduler firings were the cheap gate script (one was a missed-schedule
  catch-up, one the real logon trigger, one the hourly tick), and
  `resume-runner.log` shows only one actual `claude` launch in that window.
  The real same-day collision was two ordinary sessions in one directory,
  not a bug to fix.
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
