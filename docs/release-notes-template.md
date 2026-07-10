# GitHub Release notes template (for `setup.exe` releases)

Status: no CI release workflow exists yet (see `docs/installer-plan.md`
Section 9, task 4 — "Wire installer build into a release process" is a
separate, still-pending task). This file is a reusable snippet for whoever
builds that workflow, or for a maintainer cutting a release by hand in the
meantime: paste the block below into the GitHub Release description for any
release that attaches `opal-downloader-setup.exe`, then fill in the
version/changelog placeholders.

Rationale for including this every time: an unsigned installer triggers a
Windows SmartScreen warning (see `docs/installer-plan.md` Section 6 for the
cost/benefit of not code-signing for v1). Without an explanation right next
to the download link, that warning reads as "this is malware, don't run it"
to a non-technical user — exactly the audience the installer exists to
serve. Repeating the note on every release (rather than only linking to the
README) keeps the guidance visible at the point where the user is actually
about to double-click the file.

---

## Template

```markdown
## opal-downloader vX.Y.Z

<!-- changelog / what's new goes here -->

### Download

Download **`opal-downloader-setup.exe`** below and run it.

### ⚠️ Windows SmartScreen warning — this is expected

This installer isn't digitally signed (code-signing certificates cost
money and aren't worth it yet for a small open-source project — see
[docs/installer-plan.md, Section 6](https://github.com/alu-developer/Opal_downloader/blob/master/docs/installer-plan.md#6-code-signing--smartscreen)
for the full reasoning). Because of that, Windows will likely show a blue
**"Windows protected your PC"** screen the first time you run it.

**This does not mean the file is malware** — it just means Microsoft's
SmartScreen reputation system hasn't seen this file enough times yet to
vouch for it. To continue:

1. Click **"More info"** on the blue warning screen.
2. Click the **"Run anyway"** button that appears.

The installer will then proceed normally.
```
