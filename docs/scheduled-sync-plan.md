# Scheduled daily sync — feasibility plan

Status: planning only (produced 2026-07-16). No scheduling code exists yet.
This document is the deliverable for that planning task; concrete follow-up
implementation tasks are filed in `.claude/queue/todo/` (see bottom of this
file for the list).

## Recommendation, up front

**Build a reduced version, opt-in, gated on PR #75 landing.**

- Ship Windows Task Scheduler registration with a **fixed daily time
  trigger** (not "on logon" as the primary mechanism), using Task
  Scheduler's built-in "run as soon as possible after a scheduled start is
  missed" behavior to cover the "machine was off/asleep at the scheduled
  time" case, instead of relying on an explicit logon trigger.
- **Opt-in**, exposed as a single GUI Settings toggle + time picker — not
  silently enabled by `init`/`setup`, and not opt-out.
- **Hard-require** the dedicated Chromium login profile as a precondition —
  which becomes close to automatic once PR #75 (chromium-only login,
  hardcoded `~/.opal-downloader/login-profile`) merges, since at that point
  there is no other profile to accidentally schedule against. The concrete
  follow-up tasks below are written against that post-PR#75 shape and are
  explicitly sequenced to depend on it.
- **Failure surfacing**: a local, credential-free JSON status file plus a
  GUI banner on next open, as the core mechanism. A native Windows toast
  notification is filed as a separate, lower-priority stretch task — it adds
  real implementation complexity (no existing dependency in this Go project
  does Windows toasts) for a benefit (proactive nudge vs. "you'll see it
  next time you open the GUI") that's real but smaller than the core log+banner
  mechanism.
- **Overlap guard** is a hard requirement of the core scheduling task, not
  an afterthought — a scheduled run must not collide with a manual GUI sync
  or a previous still-running scheduled run.

This is "build," not "don't" or "wait" — daily sync is exactly the kind of
recurring manual-trigger friction this project's stated philosophy
(`CLAUDE.md`: "friction reduction ... outranks almost everything else")
exists to remove, and the prerequisite pieces (headless session reuse,
headless-relaunch-after-interactive-login via PR #66, dedicated-profile
TU-Fast auto-2FA) are already proven to work. The "reduced version" framing
is about *scope*, not about declining to build: ship the scheduling +
failure-visibility core now, treat the toast notification and any richer UX
(retry policies, partial-sync diffing in the notification, etc.) as
optional follow-on refinement once the core mechanism has run in practice
for a while.

## 1. Unattended headless reliability

**Finding: a daily scheduled run can go fully end-to-end with zero human
interaction today, provided the dedicated second profile is configured and
TU-Fast is genuinely installed/working in it — this was true before PR #75
and becomes the *only* possible configuration after PR #75 merges.**

Reading `internal/scraper/session.go`'s `ensureSession` (current master, PR
#66 already merged):

- **Common case — saved session still valid**: `ensureSession(false)` stats
  `s.stateFile`, launches `launchBrowser(true, true)` (headless, saved
  cookies only, no persistent profile touched at all), confirms
  `isAuthenticated()`, and returns. Zero browser UI, zero profile lock,
  fully silent. This is today's normal `sync`/`list` path already.
- **Session expired case**: saved state exists but `isAuthenticated()`
  fails (or the state file is missing) → falls through to
  `launchBrowser(false, false)` — a **visible**, non-headless
  `LaunchPersistentContext` against `browser_user_data_dir`, with extensions
  enabled. If TU-Fast is installed and working in that profile, it
  auto-fills credentials and completes 2FA with **no human click required**
  (this is exactly what `investigate-independent-second-profile-for-login.md`
  and PR #75's own live-verification confirmed: "TU-Fast completed the
  Shibboleth/2FA exchange with no manual click needed"). Once
  `waitForLoggedInCourseLink()` resolves and `saveState()` persists the
  fresh session, `shouldRelaunchHeadlessAfterInteractiveLogin` (PR #66) closes
  that visible browser and relaunches headless (`launchBrowser(true, true)`)
  against the just-saved state before the crawl proceeds — so even the
  "expired session" case ends with the actual file crawl running headless.
  The only non-headless part is the brief TU-Fast auto-login window itself.

So: **yes**, criterion asked "can a daily run go fully end-to-end with zero
human interaction" — the answer is yes, conditioned on TU-Fast genuinely
being installed and working in whichever profile `browser_user_data_dir`
points at. That precondition is exactly what criterion 2 is about.

**What fraction of daily runs need the re-login step?** This is honestly
**unknown and not something this codebase or its docs currently record** —
there is no documented OPAL/Shibboleth session-cookie lifetime anywhere in
`docs/` or `HISTORY.md`, and Shibboleth SSO session lengths are an IdP-side
configuration this project doesn't control or have visibility into. Rather
than guess a number, the concrete recommendation is: **the failure/status
log this plan proposes (criterion 3) should record whether each run took
the headless-only path or the interactive-relogin path**, so after a few
weeks of real scheduled runs the maintainer has actual data instead of a
guess. This is cheap to add (one extra field in the status JSON) and turns
an unknown into an empirically-answerable question within the first month
of use.

One real UX wrinkle worth flagging for criterion 5: on the interactive-relogin
day, a visible Chromium window will flash on screen for the few seconds
TU-Fast needs to complete the exchange, even though no click is needed. If
the scheduled run fires while the user is physically at the machine (e.g.
they're logged in and awake at the scheduled time), this is a brief,
harmless surprise; if the trigger were "at logon" instead of a fixed
daily time, this would coincide with the moment right after the user just
sat down, which is a marginally worse look. This is part of the reasoning
for preferring a fixed off-peak daily time (see criterion 4).

Also directly relevant: **PR #75 status as of this writing (2026-07-16) is
OPEN, not merged** — `gh pr list` shows `#75 OPEN`, and
`.claude/queue/in-progress/resolve-pr75-conflict-and-claude-md-signoff.md`
shows an agent actively working the merge-conflict-resolution task right
now (`started_at: 2026-07-16T08:39:42Z`). So as of today, master still has
the pre-PR#75 two-profile-strategy code (`browser_executable`,
`browser_user_data_dir`, `browser_profile_directory` config fields; the
"real browser" option still exists). This matters directly for criterion 2
below and for how the follow-up tasks are sequenced.

## 2. Profile-lock precondition

**Finding: the current code-level default is *not* the dedicated profile —
it's the empty string (Playwright's bundled, extension-less browser). The
"dedicated profile as default" is a documentation recommendation only
(`docs/browser-profile-strategy.md`, PR #54), not something `init`/`setup`/the
GUI actually wires up automatically.**

Confirmed by direct inspection:

- `config.example.yaml` ships `browser_executable: ""` and
  `browser_user_data_dir: ""` — an empty default, not pre-pointed at
  `~/.opal-downloader/login-profile`. `docs/browser-profile-strategy.md`
  itself says this is deliberate ("leave the field itself defaulting to
  `""` ... don't ship it pre-pointed at either strategy's path").
- The GUI's Settings page (`internal/gui/settings.go`,
  `SuggestedBrowserUserDataDir`) does prefill `browser_user_data_dir` when
  empty — but the suggested value comes from `--suggested-browser-user-data-dir`
  (`cmd/opal-downloader/root.go:619-653`), which is fed by the **installer's
  Brave/Chrome detection page** (`add-installer-brave-detection-page`),
  i.e. it suggests the user's **real, everyday** browser profile, not the
  dedicated one. So the one piece of code-level "default steering" that
  exists today actually points toward Strategy 1 (real profile), the
  opposite of the documented recommendation.
- `internal/gui/tufast_setup.go`'s `defaultDedicatedProfileDir()`
  (`~/.opal-downloader/login-profile`) exists and is used by the
  `/tufast-setup` page's own copy-transplant flow, but that page is an
  opt-in destination a user has to navigate to — it doesn't change what
  `browser_user_data_dir` defaults to on Settings.

**This is exactly the collision risk criterion 2 asks about**: if scheduled
automation is built against *whatever* `browser_user_data_dir` currently
holds, a user who followed Strategy 1 (real everyday Brave/Chrome profile)
would have a scheduled background task periodically try to launch against
their real profile — which, per `isUserDataDirLocked`, fails outright with
`ErrProfileLocked` any time their everyday browser happens to be open. That
would make scheduled sync silently and unpredictably fail on a random
fraction of days, which is a bad automation experience.

**Recommendation: scheduled automation must hard-require the dedicated
profile, not just "whatever `browser_user_data_dir` is."**

- **Post-PR #75 (recommended sequencing — see below): this requirement
  becomes almost free.** PR #75 removes `browser_executable`/
  `browser_user_data_dir`/`browser_profile_directory` from config entirely
  and hardcodes a single profile
  (`~/.opal-downloader/login-profile` per `scraper.LoginProfileDir()`, per
  that task's own description). Once that lands, there is only one profile
  concept left, period — "hard-require the dedicated profile" stops being a
  runtime check and becomes true by construction. This is the strongest
  reason to sequence the scheduling work *after* PR #75 lands rather than
  building profile-discrimination logic against the soon-to-be-deleted
  two-strategy config.
- **If, for some reason, PR #75 stalls further and scheduling needs to ship
  first**, the fallback is: the "enable scheduled sync" GUI action must
  itself check (via the same filesystem health-check logic
  `docs/browser-profile-strategy.md` proposes for `status`) that
  `browser_user_data_dir` points specifically at a dedicated, non-"real
  browser" profile with TU-Fast present, and refuse to enable scheduling
  with a clear explanatory message otherwise. This fallback is described in
  the filed task but marked as only needed if PR #75 hasn't landed by the
  time that task is picked up.

## 3. Failure detection & notification

An unattended run has no terminal/GUI window for a human to read output
from, so today's `fmt.Printf` progress lines and returned errors go
nowhere. Two credential-safe mechanisms, both filed as concrete tasks:

**Core (required): a local JSON status file.**

- Write `~/.opal-downloader/last-scheduled-run.json` after every scheduled
  run (success or failure), containing: timestamp, outcome
  (`success`/`partial`/`failure`), a short human-readable message, course/file
  counts synced, and whether the run took the headless-only or
  interactive-relogin path (see criterion 1's "unknown fraction" point —
  this field is how that question gets answered empirically over time).
  **Never** include cookies, session tokens, or credentials — only
  aggregate counts and error strings that have already been designed
  (per `docs/setup-friction.md` #4) to be user-facing/sanitized, e.g.
  "could not reach OPAL at `<url>`", "session relogin required but TU-Fast
  not detected in dedicated profile", not raw Playwright stack traces
  containing headers/cookies. Any error surfaced from deeper in the stack
  should be passed through the existing sanitization boundary, not
  re-plumbed raw.
- GUI: on any page load (or specifically a small banner component reused
  across pages, similar to how `internal/gui/feedback.go`'s existing patterns
  work), read this file; if the last run's outcome isn't `success`, show a
  dismissible banner: "Last scheduled sync ({date}) failed: {message}. [Run
  now]". This directly satisfies "a log file surfaced next time the GUI
  opens" from the task brief, and costs no new dependency.

**Stretch (optional, separate lower-priority task): a native Windows toast
notification, failure-only.** Fires only when a scheduled run's outcome is
`failure` (not `partial`, not `success`) — avoids notification fatigue for
routine partial-sync situations (e.g. a single file's transient download
error) or noise every time it just insists on succeeding, and only tells
the user something when they'd actually want to know without opening the
GUI at all. Implementation approach to investigate rather than commit to
today: shelling out to `powershell.exe -Command "New-BurntToastNotification
..."` (BurntToast module — not guaranteed present on a fresh Windows
machine, would need a presence check and silent no-op fallback to
"log-only" if absent) vs. a direct Windows Runtime toast COM call from Go
(no existing dependency in `go.mod` does this; would be new surface area).
Filed as its own task specifically so it doesn't block the core log+banner
mechanism from shipping, and so its dependency footprint gets evaluated on
its own rather than bundled into the core task's scope.

Both mechanisms stay strictly local — no network call, no
opal-downloader-operated backend involved anywhere, consistent with
CLAUDE.md's "local-only tool" / "credentials and session data never leave
the machine unscrubbed" constraints.

## 4. Scheduling mechanism

**Recommendation: Windows Task Scheduler, single task, fixed daily time
trigger + "run as soon as possible after a missed scheduled start" enabled
— not an "on logon" trigger as the primary mechanism, and not both
registered as separate always-on triggers.**

Reasoning:

- A logon-triggered task fires **every single logon**, including multiple
  logons per day (lock/unlock on some configs re-triggers logon events,
  fast user switching, etc.) — without extra de-duplication logic ("have I
  already run today?"), this either needs its own "already ran today" guard
  or risks running far more than once daily. A fixed daily-time trigger
  with Task Scheduler's own idempotent "one occurrence per day" semantics
  needs no such extra guard.
- The "missed run" failure mode (criterion 5: machine off/asleep at the
  scheduled time) is a **built-in Task Scheduler feature** — the trigger's
  "Advanced settings → If the scheduled task is missed for any reason,
  start it as soon as possible" checkbox (`STARTWHENAVAILABLE` at the COM/
  `schtasks.exe` XML level) means a fixed-time trigger already behaves like
  "run at the fixed time, or at the next opportunity (wake/logon/scheduler
  restart) if that time was missed" — which is effectively "daily time,
  with a logon-like catch-up," without needing two separately-registered
  triggers to reason about together.
- A default time in the early morning (e.g. 06:00, configurable via the
  GUI) minimizes the odds of a visible-window flash (criterion 1) coinciding
  with the user actively watching their screen, while `STARTWHENAVAILABLE`
  still ensures a laptop that was asleep at 06:00 catches up shortly after
  the user wakes/logs in later that morning.

**Registration mechanism**: not during `init`/`setup` (those are
scriptable/non-interactive per `docs/browser-profile-strategy.md`'s own
"should this be a user choice" reasoning, which applies equally well here —
registering a recurring background task is exactly the kind of consequential,
opt-in action that shouldn't happen silently during a non-interactive
bootstrap command). Instead: a **new GUI Settings toggle** ("Enable daily
automatic sync" + a time-of-day input), calling `schtasks.exe /create` (or
the Windows Task Scheduler COM API via a small wrapper) under the hood to
register/update/delete a task that runs `opal-downloader.exe sync
--scheduled`. A CLI equivalent (`opal-downloader schedule enable|disable|status`)
should exist too, for parity with this project's existing CLI/GUI dual
front-end convention and for scriptability.

**Relationship to `docs/installer-plan.md`**: no interaction needed at
install time — the installer plan already explicitly defers all
config/onboarding decisions to the GUI's first-run settings flow (Section
4: "the installer does not collect ... any other config.yaml field ...
defers entirely to the GUI's first-run settings page"). Scheduled sync
should follow the same pattern: available as a GUI toggle post-install, not
an installer wizard page. No changes to the installer plan are needed.

**Opt-in vs. opt-out**: **opt-in**. Weighed against CLAUDE.md's ease-of-use
priority, but that principle is about reducing friction for outcomes the
user already wants, not about silently starting recurring background
network/browser activity without explicit action — that crosses into
"changing account/system behavior" territory this project's own action-
categorization (and the general good practice of not silently enabling
background automation) treats as consent-worthy. A single, prominent,
one-click GUI toggle keeps the friction to enable it minimal while still
requiring the user to affirmatively turn it on once.

## 5. Failure-mode UX

- **Partial/interrupted sync**: already partially handled by the existing
  incremental-manifest design (`internal/syncer`) — a scheduled run that
  dies partway through leaves the manifest in whatever partial state
  normal interrupted syncs already leave it in today (this is not new
  behavior introduced by scheduling). What scheduling adds: the run's
  outcome (including "partial" if some courses/files failed but others
  succeeded) must be captured in the status JSON from criterion 3 so a
  partial run is visible, not silently indistinguishable from a full
  success.
- **Missed run (machine off/asleep)**: covered by Task Scheduler's
  `STARTWHENAVAILABLE` catch-up behavior (criterion 4) — no extra
  opal-downloader-side code needed for the "missed" case itself, only for
  making sure a catch-up run doesn't collide with a fresh manual run the
  user might kick off around the same time (see overlap guard below).
- **Overlapping runs**: two scenarios: (a) a scheduled run still in
  progress when the next day's trigger fires (unlikely given typical course
  sizes, but not impossible for a very large first sync or a slow network
  day), and (b) a scheduled run colliding with a manual GUI-triggered sync
  the user starts around the same time. **Recommendation: a simple
  single-instance lock** (a lock file under `~/.opal-downloader/` written
  with the running process's PID at start and removed at clean exit, checked
  at the start of any `sync` invocation — scheduled or manual — with a clear
  "a sync is already running (PID N, started at T)" message if held).
  This is a small, self-contained addition and is included as an acceptance
  criterion of the core scheduling task rather than filed separately.
- **Retry policy**: **no automatic retry within a single scheduled
  invocation** for v1 — if a run fails, it fails, and the next opportunity
  is the next scheduled occurrence (or catch-up run). Building an in-process
  retry-with-backoff is real additional scope (how many retries, what
  backoff, does a retry re-attempt interactive login or only headless
  paths) that isn't justified until there's evidence (from the status-log
  data collected per criterion 1/3) of what actually causes scheduled
  failures in practice. Flagged as a candidate future refinement, not built
  now.

## Follow-up tasks filed

1. `.claude/queue/todo/add-scheduled-daily-sync-task-scheduler-registration.md`
   — core: `--scheduled` sync mode, Task Scheduler registration/GUI toggle,
   hard-required dedicated profile, overlap guard. Depends on
   `resolve-pr75-conflict-and-claude-md-signoff`.
2. `.claude/queue/todo/add-scheduled-sync-failure-log-and-gui-banner.md` —
   status JSON + GUI banner. Depends on task 1.
3. `.claude/queue/todo/add-windows-toast-notification-for-scheduled-sync-failures.md`
   — optional native toast, failure-only. Depends on task 2. Lower priority.
