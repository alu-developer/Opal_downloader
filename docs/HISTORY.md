# Design decision history

This file holds detailed narrative for past design decisions — the "how we
got here" story, split out from `CLAUDE.md` so that file can stay a current
current-state-plus-direction reference without needing an edit every time
more history accumulates. `CLAUDE.md`'s "Login/session automation" section
keeps a short current-state pointer into this file; read here for the full
story and PR-by-PR reasoning.

## Browser profile handling: copy vs. direct-launch vs. dedicated second profile

**Current state** (see `CLAUDE.md`): the browser profile
(`browser_user_data_dir`/`browser_profile_directory` in `config.yaml`) is
opened directly by Playwright — there is no working copy made. This is the
result of the history below, and a dedicated *second* (never-copied) profile
is the direction currently being pursued as the friendlier default — see
`docs/browser-profile-strategy.md` for the current status of that push.

**PR #6 / #15 — the original copy-based approach.** To let the user's
everyday Brave/Chrome stay open while `opal-downloader` ran, an earlier
design copied the configured profile into a private working copy at
`~/.opal-downloader/browser-profile` and launched Playwright against the
copy instead of the real profile.

**PR #20 — why the copy approach was dropped.** Chromium's `Secure
Preferences` file is HMAC-integrity-protected specifically to detect
externally-modified extension state. Copying a profile into a new
user-data-dir invalidates that protection, and Chromium resets or drops
TU-Fast's permissions the moment it loads the copy — confirmed by live
testing, with no viable way found to relax that protection. `session.go`'s
`launchBrowser` was changed to launch Playwright directly against the real
`browser_user_data_dir`, with a pre-flight `isUserDataDirLocked` check
(`profile.go`) that returns a clear "please fully close Brave first" error
instead of a crash if another Chromium instance already holds the profile
open. Practical implication: `login`/`sync`/`list` require the real profile
on the user's own machine with the browser closed.

**Follow-up finding (2026-07-08,
`.claude/queue/done/investigate-independent-second-profile-for-login.md`) —
the second-profile approach.** The HMAC problem above only breaks a profile
that was *copied* from an existing one. A brand-new, never-copied
`browser_user_data_dir` (e.g. `~/.opal-downloader/login-profile`, manually
set up once by installing TU-Fast from the Chrome Web Store and logging into
OPAL/Shibboleth directly inside it) is self-consistent from creation and
works end-to-end — live-verified: `login` completes with no
HMAC/extension-drop issue, `list` reuses the saved session headlessly, and
the user's real Brave profile stays open and usable throughout. No code
change was needed — `browser_user_data_dir`/`browser_profile_directory`
already support pointing at this second profile; it's a setup/documentation
question, not an engineering one. Not yet wired into `init`/onboarding/docs
as of this writing — see `docs/browser-profile-strategy.md` for the
recommendation to make it the default, and `CLAUDE.md`'s "Login/session
automation" section for the current status of that decision.

## GUI: technology choice and rollout to primary interface

**Current state** (see `CLAUDE.md`): the local web GUI (`internal/gui/`) is
the default entrypoint (running the binary with no subcommand launches it)
and already drives settings, login, and `sync`/`list`/`dump-links` with live
progress (server-sent events to the browser) — not just a settings/onboarding
helper. The CLI subcommands remain fully functional alongside it.

**The decision (`docs/gui-concept.md`, exploratory groundwork doc):**
weighed three options — a native Go toolkit (Fyne), a Go-backend-plus-webview
app (Wails), and a local web UI served over `net/http` and opened in the
user's own browser. Chose the local web UI: zero new required Go
dependencies, no second build toolchain (no Node/npm, no CGO), and the
cleanest incremental path for a settings-only v1. That doc also flagged
Wails as the natural upgrade path later if the project ever wants a more
"installed app" feel (dock/taskbar icon, no browser chrome) — not ruled out,
just not pursued.

**The rollout:** shipped incrementally through several queue tasks —
`gui-server-foundation`, `gui-settings-page`, `gui-config-writer`,
`gui-login-trigger`, `gui-syncer-progress-reporting` (added the
`SyncCoursesWithProgress`/`ProgressFunc` plumbing to `internal/syncer`),
`gui-sync-page` (the SSE-streamed sync/list/dump-links page), and finally
`gui-primary-entrypoint` (made it the default when run with no subcommand,
landed via PR #31, commit `5fcccd0`). `docs/gui-concept.md` itself predates
all of this and still reads as pre-implementation exploration (its own
header says "Status: exploratory. No implementation") — treat its options
analysis (Fyne/Wails/local-web-UI tradeoffs) as still-useful reference if
packaging is ever reconsidered, but its Section 5 "open question" of
whether the GUI should drive `sync` is resolved: it does, already.
