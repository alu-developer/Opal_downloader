# opal-downloader

Download and sync course files from [OPAL](https://tu-dresden.de/opal) (Bildungsportal Sachsen) to your local machine via Playwright-based scraping.

Built for TU Dresden students, but works with any Bildungsportal Sachsen OPAL instance.

## Features

- Download files from your visible course pages
- Configurable course selection via exact course names in `config.yaml` (or `*` for all courses)
- Incremental sync with local manifest (`.opal-sync.manifest.json`)
- Hybrid auth flow with persisted session state:
  - First login: interactive browser (TU-Fast / 2FA supported)
  - Later syncs/lists: reuse saved session state
  - Automatic fallback to interactive login when session expires

## Prerequisites

1. Go 1.23+
2. Chromium/Chrome (installed via Playwright)

## Download & Install (Windows installer)

A Windows `setup.exe` installer (built from [`installer/opal-downloader.iss`](installer/opal-downloader.iss)
with [Inno Setup](https://jrsoftware.org/isinfo.php)) is the intended
easiest way to get started — no Go toolchain, no `git`, no terminal. There
is no public download yet (no GitHub Release / CI release workflow has been
set up for it), so for now use the manual build steps below. Once releases
exist, grab `opal-downloader-setup.exe` from the
[Releases page](https://github.com/alu-developer/Opal_downloader/releases),
run it, and follow the wizard.

**About the Windows security warning:** because the installer isn't
digitally signed (that costs money and isn't worth it for a small
open-source tool — see [`docs/installer-plan.md`](docs/installer-plan.md)
Section 6 for the reasoning), Windows will likely show a blue
"Windows protected your PC" SmartScreen screen the first time you run
`opal-downloader-setup.exe`. **This is expected, not a sign of malware** —
it just means Microsoft hasn't seen this particular file enough times yet
to vouch for it. To continue: click **"More info"** on that screen, then
click the **"Run anyway"** button that appears. The installer will proceed
normally after that.

## Installation

```bash
git clone https://github.com/alu-developer/Opal_downloader.git
cd Opal_downloader

# Install browser binaries used by Playwright for Go (mxschmitt binding)
go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6100.0 install

# Build the binary (Linux/macOS)
go build -o opal-downloader .

# Build the binary (Windows / PowerShell)
go build -o opal-downloader.exe .
```

Windows note: `go build -o opal-downloader .` (without `.exe`) produces an
extensionless file that PowerShell's call operator (`./opal-downloader`) does
not reliably execute. Use the `.exe` form above on Windows.

## Quick Start (Web UI)

Once built (see Installation above), just run the binary with no arguments:

```bash
./opal-downloader
```

This starts a local web server bound to `127.0.0.1` and prints the URL to
open in your browser (default `http://127.0.0.1:<port>/`, an available port
is picked automatically unless `--port` is given) - equivalent to running
`./opal-downloader gui` explicitly. From there:

1. **Settings** - a form covering every `config.yaml` field described below,
   split into "Connection & browser" and "Sync behavior & folders" -
   including an add/remove row editor for `course_folders`. If no
   `config.yaml` exists yet, the form is pre-filled with sensible defaults
   instead of erroring, so saving it here is all you need to bootstrap a
   fresh setup - no need to run `opal-downloader init` first. Saving
   validates the form (e.g. rejects an empty download path or a malformed
   glob pattern) and shows errors inline instead of writing a broken config;
   a valid save writes `config.yaml` directly and keeps the previous version
   as `config.yaml.bak`.
2. **Login** - opens a separate, visible browser window to complete OPAL
   login (TU-Fast/2FA supported) and saves the session for later use.
3. **Sync / List / Dump links** - run the same operations as the CLI
   subcommands below, from the browser.

The web UI is the primary, recommended way to use opal-downloader. Everything
it does is backed by the same `config.yaml` and the same underlying code as
the CLI subcommands below, so you can freely mix the two - e.g. configure and
log in via the browser, then run `sync` from a script or cron job.

### Fast path: `setup`

Instead of running the Playwright install manually (see Installation above),
you can run:

```bash
./opal-downloader setup
```

This installs Playwright's browser binaries, creates `config.yaml` from the
example if it doesn't exist yet, and prints what's left to do. It assumes
you've already built the binary (`go build` above) - it can't rebuild itself.

## Scripting / Automation (CLI)

Every action available in the web UI is also available as a CLI subcommand,
for cron jobs, scripts, or anyone who prefers the terminal. All subcommands
below work exactly as before - the web UI does not replace or change them.

```bash
# Create config.yaml from example (optional - the GUI's Settings page can
# also bootstrap a missing config.yaml, see "Quick Start" above)
./opal-downloader init

# Interactive one-time login (opens browser)
./opal-downloader login

# Sync files (reuses saved session state if valid)
./opal-downloader sync

# Optional: visible browser to observe crawling/debug selectors
./opal-downloader sync --dev
```

### Commands

| Command | Description |
|---|---|
| `./opal-downloader` | Start the web UI (127.0.0.1) - default action when no command is given |
| `./opal-downloader gui` | Start the web UI explicitly (same as running with no command) |
| `./opal-downloader init` | Create `config.yaml` from example |
| `./opal-downloader setup` | Install Playwright browsers, create `config.yaml` if missing, print next steps |
| `./opal-downloader status` | Offline check: config parses and whether a session state file exists (no browser opened) |
| `./opal-downloader login` | Open browser, complete login, persist session state |
| `./opal-downloader list` | List detected courses and file counts |
| `./opal-downloader sync` | Download new/changed files |
| `./opal-downloader sync --force` | Re-download matched files |
| `./opal-downloader dump-links --url <url>` | Open a page and write all detected link candidates to a JSON file (debugging aid) |
| `./opal-downloader login --dev` | Developer mode (visible browser, useful for tracing) |
| `./opal-downloader list --dev` | Developer mode for listing/discovery tracing |
| `./opal-downloader sync --dev` | Developer mode for full crawl/download tracing |

## Configuration

### `config.yaml`

| Key | Description |
|---|---|
| `download_path` | Local destination path |
| `default_course_folder` | Folder used when no course rule matches; if omitted, the course name is used as before |
| `course_folders` | Mapping of course-name patterns to target folders; first match wins |
| `use_section_subfolders` | Optional, default `false`. When `true`, files are placed in a subfolder per OPAL section (e.g. `<course>/<section>/<file>`) instead of flat `<course>/<file>`. Off by default; output is unchanged unless this is enabled. |
| `section_folder_names` | Optional mapping of OPAL section-name patterns to a custom subfolder name (e.g. rename `"Exercises"` to `"Übungen"`). Only used when `use_section_subfolders` is `true`. Unmatched sections fall back to OPAL's own (sanitized) section name. |
| `subfolder_destinations` | Optional mapping of `"<course pattern>/<subfolder pattern>": "<destination path>"` to redirect a specific course's specific section to an arbitrary destination path, which may be outside `download_path` entirely. Both halves are matched with the same pattern rules as `course_folders`. Only used when `use_section_subfolders` is `true`. |
| `courses` | List of exact course names to sync (case-insensitive); use `"*"` to match every course. Partial/glob patterns are not matched. |
| `sync` | Keep for compatibility (`true` by default) |
| `opal_url` | Optional OPAL base URL override |
| `session_state_file` | Optional path for persisted browser session state |
| `browser_executable` | Optional browser executable path for real profile login |
| `browser_user_data_dir` | Optional browser profile directory. opal-downloader launches Playwright directly against this path (no copy) - see "TU-Fast / Brave Setup" below |
| `browser_profile_directory` | Optional profile within user-data (e.g. `Default`, `Profile 1`) |

Example:

```yaml
download_path: "D:/Uni/OPAL"
default_course_folder: "default"
course_folders:
  "*Programmierung*": "Informatik/Programmierung"
  "*Analysis*": "Mathematik/Analysis"

# Optional: organize each course's downloads into subfolders per OPAL section
# (e.g. "Übungen", "Vorlesung"). Default is false - files stay flat directly
# under the course folder, exactly as before.
use_section_subfolders: false

# Optional: rename/normalize specific OPAL section names to your own subfolder
# names. Only applies when use_section_subfolders is true. Sections that don't
# match any pattern here keep OPAL's own (sanitized) section name.
section_folder_names:
  "Exercises": "Übungen"

# Optional: redirect one course's specific section to an arbitrary destination
# path, bypassing the normal course folder entirely (the path may live outside
# download_path). Keyed as "<course pattern>/<subfolder pattern>": "<path>".
# Only applies when use_section_subfolders is true.
subfolder_destinations:
  "*Analysis*/*Vorlesung*": "D:/Elsewhere/AnalysisSlides"

courses:
  - "Lineare Algebra 2"
  - "Grundlagen der Programmierung"
sync: true

opal_url: "https://bildungsportal.sachsen.de/opal/"
session_state_file: "~/.opal_storage_state.json"
browser_executable: ""
browser_user_data_dir: ""
browser_profile_directory: ""
```

### TU-Fast / Brave Setup

If TU-Fast is only installed in Brave, configure `browser_executable` and `browser_user_data_dir` in `config.yaml` and run:

```bash
./opal-downloader login
```

This gives the browser access to your real Brave profile so existing extensions can be used.
If TU-Fast is installed in `Profile 1` (or another non-default profile), also set `browser_profile_directory`.

**How the profile is used:** opal-downloader launches Playwright directly against
`browser_user_data_dir` - there is no working copy. An earlier design copied the profile
into a private working copy so your everyday Brave could stay open at the same time, but
that was removed: Chromium's `Secure Preferences` file is integrity-protected (HMAC) in a
way that detects a copied user-data-dir and strips extension permissions (including
TU-Fast's) as soon as Chromium loads the copy - confirmed by live testing, with no
practical way found to avoid it (see `CLAUDE.md`'s "Key design decisions" section for the
technical detail). Practical implications:

- **Close Brave fully before running `login`/`sync`/`list`.** If `browser_user_data_dir` is
  still open in another Brave window when opal-downloader starts, it exits with a clear
  "please fully close Brave first" error instead of crashing.
- There is nothing to "re-copy" after installing/updating TU-Fast - opal-downloader always
  reads the real profile directly, so the latest extension state is picked up automatically
  next run.

## Notes and Limitations

- This project uses Playwright for Go via `github.com/mxschmitt/playwright-go`.
- Microsoft Playwright does not provide a first-party Go SDK.
- The project is still evolving; selectors may require small adjustments if OPAL UI changes.
- Only files visible through your OPAL web interface can be discovered.
- Session state is sensitive data; keep the state file private.

## Long-Term Maintenance

- CI runs on every push and pull request via [.github/workflows/ci.yml](.github/workflows/ci.yml).
- Use local quality checks with [scripts/dev.ps1](scripts/dev.ps1):

```powershell
./scripts/dev.ps1 all
```

- Operational checklist and incident steps are documented in [docs/OPERATIONS.md](docs/OPERATIONS.md).
- To re-validate the fresh-install experience (clone through `init`, no OPAL credentials needed), run [scripts/test-fresh-install.ps1](scripts/test-fresh-install.ps1). Known friction points from the last dry run are tracked in [docs/setup-friction.md](docs/setup-friction.md). The credential-requiring parts (`login`/`list`/`sync`) have a manual checklist in [docs/manual-setup-checklist.md](docs/manual-setup-checklist.md).

## License

MIT
