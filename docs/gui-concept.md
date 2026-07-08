# GUI Concept & Decision Groundwork

Status: exploratory. No implementation, no dependency changes, no scaffolding.
This document exists so a future "add a GUI" task can start from an informed
starting point instead of a blank page.

## 1. Why this document exists

Opal Downloader today is a single Go binary (`cmd/opal-downloader`) driven by
subcommands (`init`, `login`, `list`, `sync`, `dump-links`) and a YAML config
file (`config.yaml`, seeded from `config.example.yaml`). That's fine for a
CLI-comfortable user, but two things are pulling towards a GUI:

1. **Setup/settings UX** - editing YAML by hand (course glob patterns,
   `course_folders` map, browser executable paths) is the biggest friction
   point for a non-technical user, and is currently entirely manual.
2. **General approachability** - `login`, `sync --force`, `--dev` flags, and
   reading stdout for progress/errors all assume a terminal user.

This is groundwork only: it surveys options and states tradeoffs against
*this* codebase, so that when the user decides to proceed, the next task can
pick an approach in an afternoon rather than re-deriving all of this.

## 2. What the current runtime actually looks like

Relevant facts pulled directly from the code, since they constrain every GUI
option below:

- **Dependencies today** (`go.mod`): `github.com/mxschmitt/playwright-go`,
  `golang.org/x/text`, `gopkg.in/yaml.v3`, plus a handful of transitive
  packages. No web framework, no GUI toolkit, nothing graphical.
- **Distribution**: `go build -o opal-downloader .` produces a single static
  binary (see `README.md`). Playwright itself is *not* fully static - the
  README documents a separate one-time step
  (`go run github.com/mxschmitt/playwright-go/cmd/playwright@... install`)
  that downloads a Chromium binary into a Playwright-managed cache directory
  outside the Go binary. So "single static binary" is already slightly
  aspirational: the tool already depends on an out-of-band browser install
  step and a real Chromium process at runtime.
- **Process model**: `internal/scraper/session.go` launches Chromium via
  Playwright (`playwright.Run()`, `pw.Chromium.Launch(...)` or
  `LaunchPersistentContext(...)`), drives it with a single `page` object, and
  keeps that process alive for the duration of a command
  (`login`, `list`, `sync`, `dump-links`). `--dev` toggles headless vs.
  visible browser. So the tool is already a "spawn and puppet a browser"
  program, not a lightweight CLI.
- **Command shape** (`cmd/opal-downloader/root.go`): flat `switch` over
  `os.Args[1]`, each subcommand loads config, constructs an `*OpalScraper`,
  runs, prints progress to stdout, exits with a status code. Nothing here is
  event-driven or streaming (no progress callback structure yet) - `sync`
  prints a final `downloaded=%d skipped=%d errors=%d` summary line only, via
  `syncer.SyncCourses`.
- **Config surface** (`internal/config/config.go` + `config.example.yaml`):
  a single flat YAML file with two logical groups baked into one struct,
  `rawConfig`:
  - **Credentials-ish / connection settings**: `opal_url`,
    `session_state_file`, `browser_executable`, `browser_user_data_dir`,
    `browser_profile_directory`.
  - **Sync behavior settings**: `download_path`, `courses` (glob/exact list),
    `sync` (bool, incremental on/off), `default_course_folder`,
    `course_folders` (map of pattern -> folder), `use_section_subfolders`
    (bool, off by default), `section_folder_names` (map of OPAL
    section-name pattern -> local folder name, only applied when
    `use_section_subfolders` is true), `subfolder_destinations` (map of
    `"<course pattern>/<subfolder pattern>"` -> arbitrary destination path,
    same gating).
  Loading is split into `LoadCredentials` (cheap, used by `login` and
  `dump-links`) and `Load` (full `App` + `Credentials`, used by `list`/
  `sync`). There is no persisted secrets/token store beyond the Playwright
  `storage_state` JSON file (session cookies). A config *writer*
  (`config.Save`) now exists and the GUI Settings page (`internal/gui/settings.go`)
  covers the full config surface above, including the three
  subfolder-organization fields via add/remove row editors matching the
  `course_folders` pattern, plus an inline warning when
  `section_folder_names`/`subfolder_destinations` are set without
  `use_section_subfolders` enabled (see `config.Warnings`).

This matters for GUI design because: (a) there's already a real child-process
/ automation dependency, so "just add a GUI" doesn't make the process model
simpler, only adds a second front-end; and (b) the config has no existing
read-modify-write path, so any GUI settings screen is *also* the first
implementation of programmatic config writing, not just a new consumer of
existing plumbing.

## 3. Candidate GUI approaches

### A. Native Go GUI toolkit (Fyne, Wails, Gio, etc.)

**Fyne** (pure Go, immediate-mode-ish widget toolkit, cross-platform,
compiles into the same binary):
- Pros: stays a single Go binary in spirit - no separate JS toolchain, no
  bundled web runtime, cross-compiles reasonably well for Windows/macOS/Linux
  (the user's own platform per `env` is Windows). Ships as one executable,
  matching the existing distribution story.
- Cons: pulls in a genuinely large dependency tree (OpenGL/graphics
  bindings, CGO on some platforms) into a project whose `go.mod` is currently
  tiny and CGO-free. Windows builds sometimes need MinGW/CGO toolchain setup,
  which raises the bar for contributors building from source. UI look-and-feel
  is "own design language," not native OS widgets - acceptable, but a
  different visual identity than a web UI. Have to hand-roll table/list
  widgets for things like the `course_folders` map editor.

**Wails** (Go backend + a native-ish window that renders HTML/CSS/JS via the
OS's system webview):
- Pros: front-end is normal HTML/CSS/JS (easy to make a good-looking settings
  form, tables, drag-drop, etc.), Go side stays close to today's `internal/`
  packages (Wails just exposes Go methods as JS-callable bindings), single
  packaged app per OS.
- Cons: adds a real build pipeline (Node/npm + a JS bundler) alongside the Go
  toolchain - a materially bigger contributor setup than "go build". Uses the
  OS's system webview (WebView2 on Windows, WebKit on macOS/Linux), which is
  usually already present but is one more environmental dependency to verify
  (WebView2 runtime on older Windows installs, in particular). Distribution
  is no longer "copy one .exe" in the purest sense - Wails produces an
  installer/bundle per platform, though still a single artifact for the end
  user to run.

**Gio**: mentioned for completeness; smaller ecosystem, steeper learning
curve, not obviously a better fit than Fyne for this project's scope. Not
analyzed further.

### B. Local web UI served over HTTP, opened in the user's own browser

Go's `net/http` (stdlib, zero new dependencies) serves a small settings/
dashboard UI; `Execute()` gains a `gui`/`serve` subcommand that starts a
local server (e.g. `127.0.0.1:PORT`) and opens the default browser to it
(or just prints the URL).

- Pros: zero new required dependencies for the server side (`net/http`,
  `html/template` are stdlib) - closest to "no new dependency" of any option.
  No new build toolchain requirement if the frontend stays server-rendered
  HTML forms + a little vanilla JS/fetch; could still add a small JS
  framework later without it being load-bearing for v1. Reuses the existing
  binary and process model almost unchanged: the HTTP handlers call straight
  into `internal/config` and `internal/syncer`/`internal/scraper`, the same
  way `root.go` does today. Naturally cross-platform (any OS with a browser).
  Easiest incremental path: config editing (read/validate/write YAML) can
  ship before any scraper-triggering UI exists.
- Cons: an extra browser window/tab is a slightly less "app-like" experience
  than a native window (no dock/taskbar icon, no single-click launch feel
  unless wrapped). Need to actively decide the lifecycle: does closing the
  browser tab stop the server? Does `sync` run in-process while the HTTP
  server also serves the UI, or do they need to be decoupled (a
  progress-reporting mechanism, e.g. SSE/WebSocket, would be new surface
  area)? Auth/exposure is a non-issue only if strictly bound to
  `127.0.0.1` - worth stating explicitly as a constraint, not an afterthought.

### Interaction with the existing Playwright/browser-automation dependency

This is worth calling out because it's easy to conflate "the tool already
drives a browser" with "the tool already has a GUI story," and they are
unrelated:

- Playwright's Chromium is a **hidden automation puppet** controlled entirely
  through the Playwright API - it is not, and cannot easily be repurposed as,
  the application's UI surface. It has its own lifecycle (headless in normal
  runs, visible only under `--dev` for debugging selectors) and shouldn't be
  confused with "the app already shows a window."
  - For approach A (Fyne/Wails): the automation browser and the GUI window
    are two entirely separate processes/surfaces running side by side. No
    conflict, but also no synergy - e.g. Wails' own webview is unrelated to
    Playwright's Chromium; they don't share a browser instance or profile.
  - For approach B (local web UI in the user's regular browser): same
    separation, but there's a superficial resemblance ("it's all just a
    browser") that could confuse users if the UI and the `--dev` automation
    window are ever open at once. Worth a one-line disclaimer in-app if this
    is built, not a technical blocker.
- Bundling concern specific to this repo: Playwright-Go already requires a
  separate `playwright ... install` step to fetch a Chromium build outside
  the Go binary (see README). Any GUI packaging story (especially Wails,
  which produces its own installer) will need to either (a) keep telling
  users to run that install step once, or (b) invest in bundling/downloading
  Chromium as part of GUI first-run - the latter is a real chunk of work and
  should stay out of scope for a first GUI iteration regardless of which
  toolkit is picked.

### Summary table

| | Fyne (native) | Wails (webview) | Local HTTP + browser |
|---|---|---|---|
| New required Go deps | Yes (graphics stack) | Yes (Wails runtime) | No (stdlib `net/http`) |
| New non-Go toolchain | No | Yes (Node/npm) | No (optional later) |
| Distribution model | Single binary | Installer/bundle per OS | Same binary, new subcommand |
| Looks native? | No (own widget style) | Yes (system webview) | It's just a browser tab |
| Contributor setup cost | Medium (CGO on some OS) | High (two toolchains) | Low (none) |
| Fit with existing Playwright dependency | Orthogonal, no conflict | Orthogonal, no conflict | Orthogonal, minor UX overlap in messaging |
| Incrementally shippable (settings-only v1) | Medium | Medium | High |

## 4. Mapping a GUI onto the existing config surface

Current state: `internal/config/config.go` has **no writer** - `rawConfig` is
unmarshalled from YAML and turned into `App`/`Credentials`, but nothing
marshals a struct back to `config.yaml`. `cmd/opal-downloader/root.go`'s
`runInit` only ever copies `config.example.yaml` byte-for-byte.

Two structurally different directions for a GUI settings screen, independent
of which toolkit is chosen:

**Option 1: GUI reads/writes the same `config.yaml`.**
- Pros: one source of truth; CLI and GUI stay interchangeable (a user can
  hand-edit YAML, then open the GUI, and vice versa, with no import/export
  step); matches the existing mental model in the README.
  Requires adding: (a) a YAML marshal path (`yaml.Marshal` on a struct shaped
  like `rawConfig`, since that's the on-disk shape - `App`/`Credentials` are
  the normalized in-memory shape and already lose information like which
  keys were merely defaulted vs. explicitly set), and (b) round-trip fidelity
  decisions - e.g. should hand-written comments in `config.yaml` survive a
  GUI-triggered save? `gopkg.in/yaml.v3` does not preserve comments through a
  plain struct marshal; preserving them would mean editing via `yaml.Node`
  instead, which is more code than a first pass needs.
- Cons: any GUI bug in the write path risks corrupting the one file the CLI
  also depends on; need explicit backup-before-write or at least a "written
  new config, old one kept as `config.yaml.bak`" convention to keep this
  safe for non-technical users.

**Option 2: GUI has its own settings store, separate from `config.yaml`.**
- Pros: isolates GUI read/write bugs from the CLI's config file; makes it
  easier to store GUI-only preferences (window size, "last synced at",
  onboarding-completed flag) that don't belong in a portable YAML config a
  user might share/version.
- Cons: introduces a second config format/location that must be kept in sync
  with, or take precedence over, `config.yaml` - exactly the kind of "which
  value actually wins" confusion that makes support harder. Actively works
  against goal #1 in the task framing (the GUI is meant to *replace/augment*
  the config-file approach, not fork it).

**Recommendation for this section**: Option 1 (same `config.yaml`, GUI grows
a proper marshal/write path in `internal/config`) fits this project's size
and stated goal better. The config is already small and flat; splitting
storage adds a synchronization problem this project doesn't need yet. If
GUI-only preferences appear later (window geometry, etc.), those are a good
candidate for a *second*, clearly GUI-scoped file (e.g. `.opal-gui-state.json`
next to the existing `.opal-sync.manifest.json` sync state), not a
replacement for `config.yaml`.

Either option, the credentials/session-state split already present in
`Credentials` vs `App` maps cleanly onto two logical GUI screens/sections
("Connection & browser" vs "Sync behavior & folders") without needing to
change the Go-side struct boundaries.

## 5. CLI-primary vs. GUI-primary - open question, not a decision

The task framing is explicit that this is undecided. Presenting the spectrum:

- **CLI stays primary, GUI is a settings/onboarding helper only.**
  The GUI's job is limited to: first-run `init` replacement, editing
  `config.yaml` fields with validation and folder pickers, maybe triggering
  `login` (since that already needs a visible browser window anyway) and
  showing the last `sync` summary. `sync`/`list`/`dump-links` stay CLI-only
  or scriptable. Smallest surface area, lowest risk of the GUI and CLI config
  logic drifting apart, plausible as a v1.
- **GUI becomes the primary/only interface for typical users, CLI kept for
  power users / automation (cron, scripting).**
  Requires the GUI to also drive `sync` itself (not just edit config),
  which means solving progress reporting (today `syncer.SyncCourses` returns
  a final stats struct with no incremental callback - streaming progress to
  a GUI is new plumbing in `internal/syncer`, not just a UI concern),
  long-running-task lifecycle in a windowed app (cancel button, "don't close
  while syncing"), and error surfacing beyond stderr text.
- **Full replacement, CLI becomes a thin wrapper or is deprecated.**
  Not warranted by anything in the current task; explicitly out of scope
  unless the user later says so.

**Recommendation**: start at the first point on the spectrum (GUI = settings
+ onboarding + login trigger; CLI keeps owning `sync`/`list`/`dump-links`
execution) and treat "should GUI also run sync with live progress" as a
deliberate, separate follow-up decision once the settings GUI exists and its
value is proven. This avoids taking on the streaming-progress redesign of
`internal/syncer` before it's clear the GUI is worth investing in further.

## 6. Minimal follow-up skeleton (once a technology is chosen - not now)

Sized as "smallest thing that proves the approach," assuming Option 1 config
handling and the CLI-primary starting point from Section 5. Not committed to
here; recorded so scoping the next task is fast:

- If **local web UI** is chosen: a new `gui` subcommand in `root.go` (sibling
  to `init`/`login`/`sync`) that starts `net/http` on `127.0.0.1:<port>`,
  serves one page listing current config values (read via
  `internal/config.Load`), a form that writes back through a new
  `config.Save`/`config.Write` function, and a button that shells out to the
  existing `login` flow. No new dependency needed for a v1 that's plain HTML
  forms.
- If **Wails** is chosen: a `gui/` directory with a minimal Wails app whose
  Go-exposed methods are thin wrappers over `internal/config.Load`/(new)
  `Save` - i.e. the GUI layer stays a thin client over the same internal
  packages the CLI already uses, rather than duplicating config logic.
- If **Fyne** is chosen: same shape, a `gui/` package with a window containing
  a form bound to the same `internal/config` structs.
- In all three cases, the actual net-new Go work is the same first increment
  regardless of toolkit: **add a config writer to `internal/config`**
  (`Save(path string, cfg ...)` performing validation + YAML marshal +
  write-with-backup). That's toolkit-agnostic and could arguably be built
  independent of, and before, the GUI technology decision - worth flagging
  to the user as a possible small unblocking task on its own.

## 7. Recommendation

Leaning towards **the local web UI approach (Option B / Section 3)** for a
first GUI iteration, for reasons specific to this repo rather than general
GUI-framework preference:

- Zero new required dependencies - keeps `go.mod` exactly as lean as it is
  today, which matters for a project that currently has three direct
  dependencies total.
- No new contributor toolchain (no Node/npm, no CGO/graphics stack) - lowest
  friction for a project that's a single maintainer's tool being open-sourced
  incrementally (per recent commit history: "Switched to Go" was itself a
  recent migration - adding a second language/toolchain stack now would be a
  lot of new surface area at once).
- Cleanest incremental path: a settings-only v1 (Section 5's first option)
  maps directly onto "serve a form, read/write `config.yaml`" with nothing
  exotic - no IPC bindings, no webview runtime version to track.
- Reuses the same `internal/config`/`internal/syncer`/`internal/scraper`
  packages the CLI already calls, so the GUI stays a thin second front-end
  rather than a parallel implementation.

If, later, the project wants a more "installed app" feel (dock/taskbar icon,
no visible browser chrome, offline-first packaging), Wails is the natural
upgrade path from a web-UI codebase, since the front-end (HTML/CSS/JS) would
largely carry over into a Wails webview - so choosing local-HTTP now doesn't
obviously waste work if the project later wants to go further.

## 8. Open decisions still owned by the user

1. Confirm the CLI-primary starting point (Section 5) or state a different
   target split now.
2. Decide if GUI-editing `config.yaml` should preserve comments (affects
   whether the eventual config writer is a plain struct marshal or a
   `yaml.Node`-based editor - meaningfully different amounts of work).
3. Decide whether "GUI triggers `sync`" is in scope for v1, or v1 is
   settings/login only with `sync` staying CLI-only until progress-reporting
   plumbing is designed (Section 5).
4. If local web UI is chosen: decide the process lifecycle - does `gui`
   block the terminal (like `login` does today), background itself, or need
   a `stop`/Ctrl-C story? Does it auto-open the browser or just print a URL?
5. Longer-term packaging appetite: is a plain `go build` + "run `gui`
   subcommand" acceptable indefinitely, or is there a desire for an
   installer/app-icon experience later (which would push towards Wails
   regardless of what ships first)?
6. Whether the config-writer work (Section 6's toolkit-agnostic first step)
   is worth doing as its own small task ahead of any GUI technology
   decision, since it's needed no matter which option wins.
