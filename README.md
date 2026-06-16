# opal-downloader

Download and sync course files from [OPAL](https://tu-dresden.de/opal) (Bildungsportal Sachsen) to your local machine via WebDAV.

Built for TU Dresden students, but works with any Bildungsportal Sachsen OPAL instance.

## Features

- Download files from enrolled courses, groups, and your personal OPAL folder
- Configurable course selection via glob patterns in `config.yaml`
- Incremental sync — re-runs skip unchanged files
- Credentials kept in a gitignored `secrets.yaml`

## Prerequisites

1. **Python 3.10+**
2. **WebDAV access on OPAL** — this uses a dedicated WebDAV password, separate from your ZIH/SSO login (including 2FA).

### Setting up WebDAV on OPAL

1. Log in to [OPAL](https://bildungsportal.sachsen.de/opal/) with your TU Dresden account.
2. Open **Administration** → **User profile** (or **Meine Einstellungen**).
3. Go to the **WebDAV access** tab.
4. Set a **WebDAV password** (this can differ from your normal login password).
5. Note your **WebDAV username** and the server URL shown on that page.

> **Note:** WebDAV exposes folder course elements and group folders. Some course content (e.g. embedded documents inside other course element types) may only be available through the OPAL web interface, not via WebDAV.

## Installation

```bash
git clone https://github.com/YOUR_USERNAME/opal-downloader.git
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

# Edit secrets.yaml with your WebDAV username and password
# Edit config.yaml with your download path and course patterns

# See which folders OPAL exposes via WebDAV
opal-downloader list

# Download / sync files
opal-downloader sync
```

## Configuration

### `config.yaml`

| Key | Description |
|-----|-------------|
| `download_path` | Where files are saved locally |
| `courses` | List of fnmatch patterns matched against folder names (e.g. `"*Analysis*"`, `"*SS2026*"`) |
| `roots` | WebDAV roots to scan: `coursefolders`, `groupfolders`, `home` |
| `sync` | Enable incremental sync (default: `true`) |
| `delete_removed` | Delete local files removed from OPAL (default: `false`) |

Example — only two courses:

```yaml
download_path: "D:/Uni/OPAL"
courses:
  - "*Lineare Algebra*"
  - "*Programmierung*"
roots:
  - coursefolders
  - groupfolders
```

### `secrets.yaml`

```yaml
webdav:
  url: "https://bildungsportal.sachsen.de/opal/webdav"
  username: "z1234567@tu-dresden.de"
  password: "your-webdav-password"
```

## Commands

| Command | Description |
|---------|-------------|
| `opal-downloader init` | Create `config.yaml` and `secrets.yaml` from examples |
| `opal-downloader list` | List top-level WebDAV folders (helps you pick course patterns) |
| `opal-downloader sync` | Download new/changed files |
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
