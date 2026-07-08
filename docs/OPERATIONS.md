# Operations Guide

This project is browser-automation heavy and depends on external website structure.

## Long-term checklist

- Keep Go and module dependencies updated regularly.
- Run CI checks on every pull request.
- Reinstall Playwright browser binaries after major updates.
- Keep `config.yaml` and session-state files out of version control.
- Keep selectors in scraper code reviewed when OPAL UI changes.
- Re-run login when the saved OPAL session expires.

## Suggested maintenance cadence

- Weekly: `scripts/dev.ps1 all`
- Monthly: dependency updates (`go get -u ./...`) and smoke sync run
- Semester start: validate course discovery and download selectors
- Periodically (or after README/config changes): `scripts/test-fresh-install.ps1`
  to re-validate the new-user setup flow (clone through `init`, no OPAL
  credentials needed). See [docs/setup-friction.md](setup-friction.md) for
  known friction points and [docs/manual-setup-checklist.md](manual-setup-checklist.md)
  for the credential-requiring login/sync tier.

## Per-subfolder download destinations

By default every file in a course downloads flat: `<download_path>/<course>/<file>`.
Three optional `config.yaml` settings (added in PR #19) let you split a
course's downloads into a subfolder per OPAL section (e.g. "Vorlesung",
"Übungen") and even redirect one specific section to an arbitrary path
outside `download_path` entirely - useful if, say, lecture slides for one
course should land directly in a folder you already sync elsewhere (Dropbox,
OneDrive, a shared drive).

These are editable both directly in `config.yaml` and, since this feature,
in the GUI Settings page (`opal-downloader gui` -> Settings -> "Subfolder
organization").

- **`use_section_subfolders`** (bool, default `false`) - the master switch.
  When `false` (the default), the other two settings below are parsed but
  have **no effect** - both the CLI (on every `status`/`list`/`sync`) and
  the GUI Settings page print/show a warning if they're set while this is
  off, so a misconfiguration doesn't fail silently.
- **`section_folder_names`** - maps an OPAL section-name pattern (same
  glob/substring matching as `course_folders`) to the local folder name to
  use instead of OPAL's own section wording. Sections that don't match any
  pattern keep OPAL's own (sanitized) name.
- **`subfolder_destinations`** - maps `"<course pattern>/<subfolder
  pattern>"` to an arbitrary destination path (can be outside
  `download_path`). Both halves of the key are matched independently, using
  the same pattern rules as `course_folders`.

### Worked example

Given this course structure in OPAL - course "Analysis I" with sections
"Vorlesung" and "Übungen" - and this `config.yaml`:

```yaml
use_section_subfolders: true

section_folder_names:
  "Übungen": "Exercises"

subfolder_destinations:
  "*Analysis*/*Vorlesung*": "D:/Elsewhere/AnalysisSlides"
```

- Files from the "Vorlesung" section of any course matching `*Analysis*` go
  to `D:/Elsewhere/AnalysisSlides/<file>` (the `subfolder_destinations`
  override wins, bypassing `download_path` and `section_folder_names`
  entirely for that one section).
- Files from "Übungen" go to `<download_path>/Analysis I/Exercises/<file>`
  (renamed via `section_folder_names`, still under the normal course
  folder).
- A section not covered by either map, in a course not covered by
  `subfolder_destinations`, falls back to
  `<download_path>/<course>/<OPAL section name>/<file>`.

If `use_section_subfolders` were left at its default `false` here, both
`section_folder_names` and `subfolder_destinations` would be ignored and
every file would land flat under `<download_path>/Analysis I/<file>` - this
is exactly the misconfiguration `opal-downloader status`/`list`/`sync` and
the GUI Settings page now warn about.

See `config.example.yaml` for the same fields with inline comments, and
`internal/config/config.go` (`ResolveSectionFolderName`,
`ResolveSubfolderDestination`, `Warnings`) for the resolution/validation
logic.

## Incident playbook

If sync suddenly returns too few files:

1. Run `opal-downloader list` and compare expected course count.
2. Re-authenticate with `opal-downloader login`.
3. Check OPAL page changes and update selectors in `internal/scraper/scraper.go`.
4. Run one forced sync: `opal-downloader sync --force`.
