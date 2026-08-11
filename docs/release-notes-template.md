# GitHub Release notes template (for `setup.exe` releases)

**Status: automated. This is the reference copy, not a manual step.**

The header here said "no CI release workflow exists yet (see
`docs/installer-plan.md` Section 9, task 4)" until 2026-08-11, and told a
maintainer to paste the block below by hand. Both parts were wrong by then:
`.github/workflows/release.yml` has existed since PR #44, and it published
`v0.1.0` on 2026-07-14 — with `--generate-notes` alone, so that release's page
is a bare list of merged PRs and carries neither of the two notes below. The
gap was not that the template was missing; it was that nothing ever applied it.

Since 2026-08-11 the workflow's "Create GitHub Release and upload assets" step
builds these notes itself and passes them via `--notes-file`, with
`--generate-notes` appending the auto changelog underneath. **Keep this file and
that step in sync** — the workflow is what actually ships, this file is the
readable copy and the record of why the content is what it is.

Rationale for including this on every release, rather than linking the README
once:

- **The SmartScreen note.** An unsigned installer triggers a Windows
  SmartScreen warning (see `docs/installer-plan.md` Section 6). Without an
  explanation right next to the download link, that warning reads as "this is
  malware, don't run it" to a non-technical user — exactly the audience the
  installer exists to serve.
- **The checksum note.** The `.sha256` sidecar is uploaded on every release and
  was, until 2026-08-11, documented nowhere a downloader would look. A
  verification file nobody is told how to use is not a verification story.

Both belong at the point where the user is about to double-click, not one
click away.

---

## Template

```markdown
### Download

Download **`opal-downloader-setup.exe`** below and run it. Chromium is
bundled, so there is nothing else to install. It installs per-user, so
Windows will not ask for administrator rights.

### Windows SmartScreen warning - this is expected

This installer isn't digitally signed ([why not](https://github.com/alu-developer/Opal_downloader/blob/master/docs/installer-plan.md#6-code-signing--smartscreen)),
so Windows shows a blue **"Windows protected your PC"** screen the
first time you run it.

**This does not mean the file is malware** - it means Microsoft's
SmartScreen reputation system hasn't seen this file often enough to
vouch for it. To continue: click **"More info"**, then **"Run anyway"**.

### Verifying the download (optional)

`opal-downloader-setup.exe.sha256` below holds the SHA-256 of the
installer as this workflow built it. To compare:

```powershell
Get-FileHash .\opal-downloader-setup.exe -Algorithm SHA256
```

A mismatch means a corrupted or truncated download. This guards
against transport damage and naive tampering, not against a malicious
release published by whoever controls this repo - that is a
code-signing problem, see
[docs/update-mechanism-plan.md](https://github.com/alu-developer/Opal_downloader/blob/master/docs/update-mechanism-plan.md)
Section 4.

---
```
