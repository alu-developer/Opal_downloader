# Operations Guide

This project is browser-automation heavy and depends on external website structure.

## Long-term checklist

- Keep Go and module dependencies updated regularly.
- Run CI checks on every pull request.
- Reinstall Playwright browser binaries after major updates.
- Keep `secrets.yaml` and session-state files out of version control.
- Keep selectors in scraper code reviewed when OPAL UI changes.
- Re-run login when the saved OPAL session expires.

## Suggested maintenance cadence

- Weekly: `scripts/dev.ps1 all`
- Monthly: dependency updates (`go get -u ./...`) and smoke sync run
- Semester start: validate course discovery and download selectors

## Incident playbook

If sync suddenly returns too few files:

1. Run `opal-downloader list` and compare expected course count.
2. Re-authenticate with `opal-downloader login`.
3. Check OPAL page changes and update selectors in `internal/scraper/scraper.go`.
4. Run one forced sync: `opal-downloader sync --force`.
