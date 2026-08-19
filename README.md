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

## Download & Install (Windows)

**[⬇ Download the latest `opal-downloader-setup.exe`](https://github.com/alu-developer/Opal_downloader/releases/latest)**

Run it and follow the wizard. That's the whole install — no Go toolchain, no
`git`, no terminal, and no separate browser download (Chromium ships inside the
installer). It installs per-user into `%LOCALAPPDATA%\Programs\opal-downloader`,
so Windows does not ask for administrator rights. Re-running a newer installer
over an existing install upgrades it in place.

The installer is built from [`installer/opal-downloader.iss`](installer/opal-downloader.iss)
with [Inno Setup](https://jrsoftware.org/isinfo.php) by
[`.github/workflows/release.yml`](.github/workflows/release.yml) on every `vX.Y.Z`
tag — nobody uploads a hand-built binary.

### Verify the download (optional)

Every release ships an `opal-downloader-setup.exe.sha256` sidecar next to the
installer. To check the file you downloaded is the file that was published:

```powershell
Get-FileHash .\opal-downloader-setup.exe -Algorithm SHA256
```

Compare the printed hash against the sidecar's contents (case-insensitive).
A mismatch means a corrupted or truncated download — delete it and download
again. Note the limit of what this proves: it guards against transport damage
and naive tampering, **not** against a malicious release published by whoever
controls this repo. That is a code-signing problem with a different trust root
— see [`docs/update-mechanism-plan.md`](docs/update-mechanism-plan.md) Section 4.

### About the Windows security warning

The installer isn't digitally signed (see
[`docs/installer-plan.md`](docs/installer-plan.md) Section 6 for the
cost/benefit), so Windows shows a blue **"Windows protected your PC"**
SmartScreen screen the first time you run it. **This is expected, not a sign of
malware** — it only means Microsoft's reputation system hasn't seen this
particular file often enough to vouch for it. To continue: click
**"More info"**, then the **"Run anyway"** button that appears. The installer
proceeds normally after that.

## Build from source (contributors)

Only needed if you want to modify the code or run the tests — end users should
use the installer above.

Prerequisites: Go 1.23+ (Chromium is fetched by the Playwright step below).

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

### Building `opal-downloader-setup.exe` locally

End users should download the installer from [Releases](https://github.com/alu-developer/Opal_downloader/releases/latest)
(see above) rather than building it. To build one yourself (e.g. to test an
installer change before pushing a release tag), run:

```powershell
.\scripts\build-installer.ps1
```

from a repo checkout, with [Inno Setup 6](https://jrsoftware.org/isinfo.php)
installed and a populated `%USERPROFILE%\.opal-downloader\ms-playwright`
(run `opal-downloader.exe setup` once first if that's empty). It builds the
binary, stages the local Chromium cache, and compiles
`installer\output\opal-downloader-setup.exe`. This is the same one-command
path `.github/workflows/release.yml` uses to build every published release.

## Quick Start (Web UI)

Once built (see "Build from source" above), just run the binary with no arguments:

```bash
./opal-downloader
```

This starts a local web server bound to `127.0.0.1` and prints the URL to
open in your browser (default `http://127.0.0.1:<port>/`, an available port
is picked automatically unless `--port` is given) - equivalent to running
`./opal-downloader gui` explicitly. From there:

1. **Settings** - a form covering every `config.yaml` field described below,
   including an add/remove row editor for `course_folders`. If no
   `config.yaml` exists yet, the form is pre-filled with sensible defaults
   instead of erroring, so saving it here is all you need to bootstrap a
   fresh setup - no need to run `opal-downloader init` first. Saving
   validates the form (e.g. rejects an empty download path or a malformed
   glob pattern) and shows errors inline instead of writing a broken config;
   a valid save writes `config.yaml` directly and keeps the previous version
   as `config.yaml.bak`.
2. **Sync / List / Dump links** - run the same operations as the CLI
   subcommands below, from the browser. If no valid session is saved yet,
   this opens a separate, visible browser window to complete OPAL login
   (TU-Fast/2FA supported) automatically before syncing - there's no
   separate manual login step to run first.

The web UI is the primary, recommended way to use opal-downloader. Everything
it does is backed by the same `config.yaml` and the same underlying code as
the CLI subcommands below, so you can freely mix the two - e.g. configure via
the browser, then run `sync` from a script or cron job.

### Fast path: `setup`

Instead of running the Playwright install manually (see "Build from source" above),
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

# Optional but recommended: install TU-Fast once for automatic 2FA on every
# future login - see docs/browser-profile-strategy.md, or the GUI's
# Settings -> "Set up TU-Fast" (/tufast-setup) page. Skipping this is fine
# too; login then just needs manual 2FA each time.

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
| `use_section_subfolders` | Optional, default `false`. When `true`, files are placed in a subfolder per OPAL section (e.g. `<course>/<section>/<file>`) instead of flat `<course>/<file>`. Off by default; output is unchanged unless this is enabled. Changing this (or any other folder-layout setting) after you have already synced changes where every file belongs: the next sync detects the old layout in `.opal-sync.manifest.json`, moves already-downloaded files to their new locations instead of re-downloading them, and prints a warning listing anything it could not match so you can move or delete it yourself. Nothing is ever deleted automatically. |
| `section_folder_names` | Optional mapping of OPAL section-name patterns to a custom subfolder name (e.g. rename `"Exercises"` to `"Übungen"`). Only used when `use_section_subfolders` is `true`. Unmatched sections fall back to OPAL's own (sanitized) section name. |
| `subfolder_destinations` | Optional mapping of `"<course pattern>/<subfolder pattern>": "<destination path>"` to redirect a specific course's specific section to an arbitrary destination path, which may be outside `download_path` entirely. Both halves are matched with the same pattern rules as `course_folders`. Only used when `use_section_subfolders` is `true`. |
| `courses` | List of exact course names to sync (case-insensitive); use `"*"` to match every course. Partial/glob patterns are not matched. |
| `sync` | Keep for compatibility (`true` by default) |
| `opal_url` | Optional OPAL base URL override |
| `session_state_file` | Optional path for persisted browser session state |

There is no `browser_executable`/`browser_user_data_dir`/`browser_profile_directory`
config anymore - login/sync always use Playwright's bundled Chromium against a single
dedicated profile, see "TU-Fast Setup" below.

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
```

### TU-Fast Setup

Login and sync always use Playwright's bundled Chromium against a single, hardcoded
dedicated profile at `~/.opal-downloader/login-profile` - there is nothing to configure
for this in `config.yaml`, and opal-downloader never launches a real installed
Brave/Chrome executable. Full rationale is in
[`docs/browser-profile-strategy.md`](docs/browser-profile-strategy.md).

You can just log in manually (credentials + 2FA by hand) every time, or install TU-Fast
(TU Dresden's Shibboleth/2FA auto-login extension) once into that same dedicated profile
so future logins auto-complete.

**Fastest path: the GUI.** Settings → "Set up TU-Fast" (`/tufast-setup`) creates the
profile folder for you and opens a Chromium window already at TU-Fast's Chrome Web Store
listing - you only click "Add to Chrome" and log into OPAL/Shibboleth once. If TU-Fast is
already installed and logged in in another browser profile on this same computer (e.g.
your everyday Brave/Chrome), the same page can copy just its stored login/2FA data into
the dedicated profile instead, skipping that login step entirely (same-machine only - see
[`docs/browser-profile-strategy.md`](docs/browser-profile-strategy.md)'s "Transplanting
TU-Fast login data" section).

**Manual path (no GUI):** run `opal-downloader login` once - it opens Chromium against
`~/.opal-downloader/login-profile` directly. In that window, install TU-Fast from the
Chrome Web Store and log into OPAL/Shibboleth once to complete 2FA/device registration.
Nothing else to configure; the same profile is reused automatically on every future
login/sync.

Run `opal-downloader status` any time to check the dedicated login profile is healthy
(directory exists, looks like a real profile, TU-Fast detected) without launching a
browser.

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
