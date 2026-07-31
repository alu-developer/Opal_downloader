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

One thing left, and it is the maintainer's, not an agent's:

**Nobody has looked.** The walks assert structure and behaviour, headless. They
cannot catch a purely visual break, and `gui`/`main.exe gui` is still
unexercised because `Run` opens a real window unconditionally on Windows.

The other two entries that stood here were already answered: the "List courses"
rename shipped on 2026-07-26 (`2f811c5`, now "Preview sync (no download)",
guarded by `TestSyncPagePreviewButtonIsHonestAboutWhatItCosts`), and the hidden
course picker was decided on 2026-07-31 — keep "Sync all courses" as the
default, but show the list, muted, instead of hiding it. Both are done. They
reappeared here because the 2026-07-31 compaction (`9b51cf2`) rebuilt this item
out of the archived *findings* rather than the decisions that followed them,
directly under a line saying the decisions had shipped. **When compacting an
entry, check the code, not the older text you are summarising.**

Worth knowing independently of this item: **the scheduler's disable path has no
guard**, and `scheduler.TaskName` is a single global constant that the
maintainer's live daily sync is registered under.

---

## Next

Nothing queued. The next thing that lands here should come from the Noticed
list or from the maintainer.

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

- **The .exe has its own Explorer icon** (2026-07-31): `rsrc_windows_amd64.syso`
  is generated from `internal/gui/assets/icon.ico` and checked in, so building
  needs no new tool and `go.mod` is untouched — which is what the "needs the
  maintainer's OK for a build-time dependency" block was really about.
- **The course list is always on the settings page now** (2026-07-31):
  "Sync all courses" still starts ticked, but it mutes the list and says the
  ticks are inactive instead of hiding it. Folder inputs stay live, because
  `course_folders` applies under the wildcard too.
- **Deleted the self-built autonomy machinery** (2026-07-31): ten of twelve
  PowerShell files, the `OpalDownloader-ResumeRunner` Windows task, the
  keep-warm process, and all of `.claude/queue/`. Replaced by one first-party
  Claude Code Desktop scheduled task. Kept only the two hooks that *enforce*
  (pre-push gate, turn-failure checkpoint). Trigger: 102 of 193 commits in
  seven days touched only `docs/`, `.claude/` or `scripts/`. See
  `docs/agent-operating-model.md`.
- Closed the last Noticed item (the User-Agent-fix theory) with a decision
  rather than another probe — a higher-volume burst at the real OPAL server is
  what `docs/server-load.md` exists to prevent, curiosity included.
- Closed the "unattended run can't wait for a background job" item as a rule,
  not a detector: behaviour lives in the prompt, hooks enforce only.
- Fixed a stdin BOM silently breaking JSON parsing in 5 hooks (2026-07-31).
- **Closed the sync-speed campaign's remaining question with a decision:** the
  verified HTTP-hybrid (diff=0, all 6 courses) ships as an opt-in diagnostic;
  the actual speedup would need an unreviewed change to the crawl's
  highest-risk code for an estimated ~60-90s that still misses the 30s target.
  Reliability outranks features. Reopen only with the maintainer watching the
  diff live, not by re-measuring. Reasoning in `docs/sync-speed-campaign.md`.
- Corrected two of my own same-session claims with real evidence: the Logon
  trigger did fire (Task Scheduler event 119), and the "overlapping launches"
  I had called confirmed were the cheap gate script, not real launches.
- Moved 678 lines of closed work into `docs/BACKLOG-archive.md` (2026-07-31).
- Routed every live probe's diagnostic logs somewhere visible.
- Found a real completion-signal candidate: jsTree's `aria-busy`, across 4
  courses.
- Wired the app icon into the running window (WM_SETICON), rasterised from
  logoSVG.
- Deleted the section change-detection cache, budget and all.
