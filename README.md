# opal-downloader

Download and sync course files from [OPAL](https://tu-dresden.de/opal) (Bildungsportal Sachsen) to your local machine via web scraping.

Built for TU Dresden students, but works with any Bildungsportal Sachsen OPAL instance.

## Features

- Download files from enrolled courses and groups
- Configurable course selection via glob patterns in `config.yaml`
- Incremental sync — re-runs skip unchanged files
- Requires manual login (supports 2FA via TU-Fast / SSO)

## Prerequisites

1. **Python 3.10+**
2. **A web browser** — for manual login during sync/list operations

## Installation

```bash
git clone https://github.com/alu-developer/opal-downloader.git
cd opal-downloader
python -m venv .venv

# Windows
.venv\Scripts\activate

# Linux / macOS
source .venv/bin/activate

pip install -e .
```

## Quick start

```bash
# Create config.yaml and secrets.yaml from examples
opal-downloader init

# (Optional) Edit secrets.yaml if you use a non-standard OPAL URL
# Edit config.yaml with your download path and course patterns

# List available courses (opens browser for manual login)
opal-downloader list

# Download / sync files (opens browser for manual login)
opal-downloader sync
```

## How it works

1. **First run:** When you run `sync` or `list`, Playwright opens a web browser.
2. **Login:** You log in manually to OPAL (including 2FA if enabled). TU-Fast is fully supported.
3. **Scraping:** Once logged in, the tool automatically scrapes your courses and downloads files.
4. **Sync:** On subsequent runs, only new or changed files are downloaded.

## Configuration

### `config.yaml`

| Key | Description |
|-----|-------------|
| `download_path` | Where files are saved locally |
| `courses` | List of fnmatch patterns matched against course names (e.g., `"*Analysis*"`, `"*SS2026*"`) |
| `sync` | Enable incremental sync (default: `true`) |

Example — filter specific courses:

```yaml
download_path: "D:/Uni/OPAL"
courses:
  - "*Lineare Algebra*"
  - "*Programmierung*"
sync: true
```

### `secrets.yaml`

```yaml
# Optional: Override OPAL URL (defaults to TU Dresden)
opal_url: "https://bildungsportal.sachsen.de/opal/"
```

## Commands

| Command | Description |
|---------|-------------|
| `opal-downloader init` | Create `config.yaml` and `secrets.yaml` from examples |
| `opal-downloader list` | List available courses (manual login required) |
| `opal-downloader sync` | Download new/changed files (manual login required) |
| `opal-downloader sync --force` | Re-download all matched files |

## Limitations

- **Manual login required each time** — The tool doesn't store session tokens due to 2FA.
- **Limited to scraped content** — Only files visible in OPAL's web interface are downloaded (same as WebDAV previously).
- **Course detection** — Courses are detected from the logged-in user's dashboard and must match the patterns in `config.yaml`.

## Troubleshooting

- **Browser doesn't open?** Check that Playwright has permissions to launch chromium. On Linux, install: `playwright install-deps`
- **Login fails?** If the browser opens but you can't log in, wait a moment and refresh the page.
- **No files found?** Check your course patterns in `config.yaml` — they use fnmatch syntax (case-insensitive).

| `opal-downloader sync --force` | Re-download everything |

Sync state is stored in `<download_path>/.opal-sync.manifest.json`.

## Project structure

```
opal-downloader/
├── opal_downloader/     # Python package
├── config.example.yaml
├── secrets.example.yaml
├── requirements.txt
└── README.md
```

## Disclaimer

This tool is unofficial and not affiliated with TU Dresden or Bildungsportal Sachsen. Use it responsibly and in accordance with your university's terms of use. Only download materials you are authorized to access.

## License

MIT
