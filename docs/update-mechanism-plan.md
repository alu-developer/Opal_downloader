# In-App Update Mechanism — Design Plan (v2+)

Status: planning only. No code, no `internal/updater` package, no GUI changes
exist yet. This is the follow-up design for `docs/installer-plan.md` Section
9, item 7 ("In-app update checker... Later, optional... not designed further
here"). It assumes the Section 7 v1 decision — manual re-run of `setup.exe`
from GitHub Releases, relying on Inno Setup's upgrade-in-place behavior — is
final and does not revisit it.

This document does **not** re-litigate:
- The v1 manual-re-run decision itself (installer-plan.md Section 7).
- The unsigned-vs-signed cert tradeoff (installer-plan.md Section 6) — that
  decision (ship unsigned) is taken as given here; this plan only asks
  "given unsigned binaries, how much of the update flow can still be
  automated safely."
- The contributor/source build flow (`git pull` + `go build`) — out of
  scope, that flow is fine as-is.

## 0. Where this sits relative to the installer

No installer exists yet — `installer-plan.md` is itself still planning-only.
It was originally also gated on `gui-primary-entrypoint` landing before any
release; that has since happened (PR #31, commit `5fcccd0`), so the
remaining gap is purely "no installer has been built yet," not a GUI
dependency. That matters here in one concrete way: **a fully automatic
"download the new setup.exe and run it" flow has no artifact to update
yet**, and more importantly, no established *release process* to consume. Today's `.github/workflows/ci.yml`
only runs tests/vet/build on push/PR — there is no job that tags a release,
builds `opal-downloader-setup.exe`, or uploads it as a GitHub Release asset.
Any update checker that parses release names/assets is implicitly depending
on that process existing and being stable (consistent naming, consistent
`AppId`/tag format) — which is a real prerequisite, not a detail to wave
away. This plan flags it in Section 6 rather than designing it (that's the
installer-plan's/release-automation's job, not this one's).

## 1. The options, and where the effort/friction tradeoff actually lands

Four tiers, ordered by both effort and how much manual action they remove.
"Friction removed" is measured against today's v1 baseline: notice a new
release exists (how?), open GitHub, download `setup.exe`, run it, click
through the wizard again.

| Tier | Mechanism | Build effort | User steps remaining | Manual action removed |
|---|---|---|---|---|
| **T0 — v1 baseline** | User has to *know* a release happened (watching GitHub, a Discord ping, whatever) and does everything by hand. | None (already true) | Notice → open GitHub → download → run → click through wizard | None |
| **T1 — "Check for updates" button** | GUI has a button/menu item that opens `https://github.com/<repo>/releases/latest` in the user's browser. No version comparison, no API call needed even. | Trivial (a link) | Still: notice the button exists → click it → download → run → click through wizard | Removes "how do I find the releases page" but nothing else |
| **T2 — Prompted, one-click apply** *(recommended v2 default, see Section 3)* | App polls the GitHub Releases API (on GUI startup), compares the embedded version to the latest tag, and if newer, shows a banner: "vX.Y.Z is available — click to download and install." Clicking downloads the asset, verifies it, and either launches the downloaded `setup.exe` (handing off to the existing Inno Setup upgrade-in-place flow) or at minimum opens it in Explorer/a save dialog ready to run. | Medium | One click; Inno Setup's own wizard still runs (Next/Next/Finish) since v1 didn't design a silent installer switch | Removes "notice + find + download" (3 of 5 steps); keeps the wizard as a deliberate final human checkpoint (see Section 4) |
| **T3 — Fully silent self-update** | Background check + silent download + silent `/VERYSILENT` Inno Setup install + auto-relaunch, no prompt at all. | High (silent install flags, relaunch-without-losing-state, handling "app is currently running" self-replacement, rollback on failure) | Zero | Removes everything — but see Section 4 for why this is the wrong call here specifically because there's no code signing |

**Recommendation: T2.** The full case is in Section 3; the short version is
that T2 captures nearly all the friction reduction of T3 (3 of 5 manual
steps gone, and the remaining step is one click, not "go find the file") at
a fraction of the effort and — critically — without removing the one human
checkpoint that matters when there's no code signature to trust instead
(Section 4).

## 2. Technical design sketch

### 2.1 Version embedding (prerequisite — currently missing)

Checked `cmd/opal-downloader/root.go`: the `--version`/`-v`/`version`
subcommand exists today but is a **hardcoded string literal**:

```go
case "--version", "-v", "version":
    fmt.Println("opal-downloader 0.1.0")
```

There is no `go build -ldflags "-X ...Version=..."` injection anywhere in
the repo (confirmed — no `ldflags` usage in any `.go` file or in
`ci.yml`), and no use of `debug.ReadBuildInfo()`. This means:

- Today, "0.1.0" has to be hand-edited in source for every release and
  nothing currently enforces it's kept in sync with the git tag.
- **This is a real prerequisite for the update checker**, not optional
  polish: T2 needs a reliable "what version am I currently running"
  comparison value, and a string that's only updated when someone
  remembers to isn't reliable enough for an automated check (it would
  either nag forever if forgotten, or never fire if stale-but-equal).

Recommended fix (small, self-contained, could even land ahead of/independent
of the rest of this plan):

```go
// var set via: go build -ldflags "-X github.com/alu-developer/opal-downloader/cmd/opal-downloader.buildVersion=v0.2.0"
var buildVersion = "dev" // overridden at release-build time
```

Falling back to `"dev"` (or `debug.ReadBuildInfo().Main.Version` for
`go install`-built binaries, which report the module pseudo-version) when
unset, so contributor builds don't show a misleading fixed version. The
release-build step (wherever it ends up living — see Section 6, this is
tangled up with "no release workflow exists yet") passes the git tag via
`-ldflags` at build time. This is a standard, low-risk Go pattern with no
new dependencies.

### 2.2 Where the check lives

New package: **`internal/updater`**, parallel to `internal/config` /
`internal/syncer` (matches the existing package-per-concern layout described
in `CLAUDE.md`). Responsibilities:

- `CheckLatest(ctx) (Release, error)` — one unauthenticated `GET
  https://api.github.com/repos/alu-developer/Opal_downloader/releases/latest`
  using the stdlib `net/http` client (default `http.Client`, TLS
  verification on, no custom transport — see Section 4 on why this matters).
  Public repo, public endpoint: **no token, no auth, no server-side
  component needed at all** (Section 5).
- Parses the JSON response's `tag_name` (e.g. `"v0.2.0"`), strips a leading
  `v`, and does a simple semver-ish comparison against `buildVersion` from
  2.1. A minimal hand-rolled comparison (split on `.`, compare ints) is
  sufficient — this project has no other semver dependency and pulling one
  in just for this is not justified.
- Picks the release asset matching the expected name pattern (e.g.
  `opal-downloader-setup.exe`) from the response's `assets[]` array, and
  exposes its `browser_download_url` and `size`.
- `Download(ctx, url, destPath) error` — streams the asset to a temp file.
- Optional `VerifyChecksum(path, expectedSHA256 string) error` if the
  release publishes one (Section 4).

This is a small, easily unit-testable package (the GitHub API response is
just JSON over HTTP — trivially fakeable with `httptest.Server` in tests, no
Playwright/browser dependency at all, unlike most of this codebase).

Rate limit: GitHub's unauthenticated REST API allows 60 requests/hour **per
calling IP** — since each user's own machine calls it directly (not a shared
server), and this plan recommends checking at most once per GUI process
start (Section 2.3), no realistic usage pattern gets near that limit. No
auth token, no server-side proxy, no infrastructure needed — confirms the
task's suggested "zero-infrastructure" answer holds with no caveats.

### 2.3 When the check runs

`internal/gui/gui.go`'s `Run()` already has the right shape for this: it
starts a listener, then blocks in a `select` on shutdown signals. Add one
more goroutine, launched from `Run()` right after the listener starts:

```go
go srv.checkForUpdateOnce(ctx)
```

Recommend **check once per GUI process start, not a recurring ticker.**
Rationale: this is a locally-run, short-lived desktop tool (per `job.go`'s
own documented model — "single-user local tool... no persistence across a
server restart"), not a long-running daemon. Most users start `gui`, do a
sync, and close it within minutes; a `time.Ticker`-based recurring check
would rarely fire a second time in practice and adds shutdown-goroutine
lifecycle complexity (needs to respect the same `context.WithTimeout`
shutdown path `Run()` already uses) for no realistic benefit. A manual
"Check again" link on the landing page covers the rare case of someone
leaving the GUI open for days.

Result is cached on the `server` struct (same pattern as `loginActive` in
`gui.go`) — a new field like:

```go
type server struct {
    configPath string
    loginMu     sync.Mutex
    loginActive bool

    updateMu     sync.Mutex
    updateResult *updater.Release // nil until checked; nil result also means "no update" once checked
    updateChecked bool
}
```

No persistence to disk needed — same reasoning as job state in `job.go`
("no persistence/resume... acceptable for a single-user local tool").

### 2.4 GUI surface

`internal/gui/gui.go`'s `landingTemplate` already has a `.status` block
pattern (used today for the "logged in / not logged in" banner). Add a
second status block, conditionally rendered when an update is available:

```html
{{if .UpdateAvailable}}
<div class="status warn">
    A new version is available: <strong>{{.LatestVersion}}</strong>
    (you have {{.CurrentVersion}}).
    <form method="post" action="/update/start" style="display:inline">
        <button type="submit">Download &amp; install</button>
    </form>
</div>
{{end}}
```

New routes, mirroring the existing `/login` + `/login/start` pattern:
- `GET /update` (optional, or fold into landing) — shows current vs. latest
  version, changelog link (GitHub release notes URL), a "Download & Install"
  button.
- `POST /update/start` — downloads the asset (2.2), verifies it (Section
  4), then **opens the downloaded `setup.exe` via `exec.Command` /
  `os.StartProcess` and exits the GUI process** so Inno Setup's
  upgrade-in-place installer isn't fighting a running instance of the app
  it's trying to replace, then re-renders a "starting installer, this app
  will close now" page before that happens. (This is the one genuinely
  new piece of process-lifecycle handling this plan introduces — worth
  flagging as the highest-uncertainty part of the implementation estimate
  in Section 6, since "does Windows let you `exec` an installer and then
  exit the process that launched it, cleanly" needs a live spike, not just
  a design.)

This reuses the existing HTML-template-based routing style already
established in `gui.go`/`settings.go` — no new UI framework/JS needed,
consistent with the rest of the GUI package.

**CLI footnote (not required, cheap once 2.1–2.2 exist):** once
`internal/updater` exists, `sync`/`list`/`login` in `root.go` could each
print one extra line at the end of a run — "A newer version (vX.Y.Z) is
available: <release URL>" — using the same cached-once-per-process check.
Zero new UI surface, no interactivity, purely informational. Not required
by this task's acceptance criteria (which asks specifically about the GUI
surface) but worth noting as a near-free extension once the package exists,
since many users may run CLI subcommands directly rather than the GUI.

## 3. The case for T2 over T1 and T3

**Why not stop at T1 (button that just opens the releases page)?** It's
almost free to build, but it only solves "where do I click" — it does
nothing about "how would I know to click it in the first place." A user
who isn't already in the habit of checking GitHub will simply never open
the GUI's update page unless something tells them to. T2's actual value-add
over T1 is the **unprompted-but-visible banner** — the check happens
automatically and surfaces itself, so discovery isn't left to the user's
initiative. That's the single biggest lever in this whole design: the gap
between "a mechanism exists if you go looking" and "the app tells you"
matters far more than the difference between "opens a browser tab" and
"downloads the file for you." Given that, once you're building the
automatic-check plumbing anyway (2.2, 2.3), doing the one-click
download-and-launch on top (rather than stopping at "click here to open the
browser") is a comparatively small additional increment — most of the
Section 2 design (the version check, the GitHub API call, the banner) is
shared between a hypothetical "auto-checking T1" and full T2; only the
download+verify+launch step (2.4's `/update/start`) is T2-exclusive, and
it's a bounded, well-understood piece of work (stream a file, hash it,
`exec.Command` it).

**Why not go all the way to T3 (fully silent)?** This is where the
unsigned-binary trade-off (installer-plan.md Section 6) actually bites, and
it's worth stating plainly rather than leaving it implicit:

- With no code signature, there is **no cryptographic proof to the OS (or
  the user) that a given `setup.exe` actually came from this project's
  maintainer** rather than, say, a compromised GitHub account, a compromised
  CI/build machine, or a MITM'd download (mitigated by HTTPS, see Section 4,
  but a compromised release asset itself isn't caught by HTTPS at all).
- Today's manual flow has an accidental but real safety property: a human
  has to *notice* a new release, go find it, and choose to run it — which
  means a compromised release sits there for the window between publish and
  when a suspicious user might notice something's off (wrong file size,
  unexpected release timing, a GitHub security advisory, whatever) before
  most users touch it.
- **T3 removes that window and that human checkpoint entirely.** A fully
  silent updater would apply a malicious release to every installed user
  automatically, with nobody in the loop to notice anything wrong before it
  runs with the same privileges the app already has. That's a strictly worse
  security posture than today, not a neutral change — automating the
  *download* is a friction win with no real security cost (the user still
  didn't inspect the binary manually before either), but automating the
  *execution with no prompt* is what actually removes the checkpoint.
- T2 keeps exactly one click as the remaining human gate — "install
  vX.Y.Z now?" — which costs the user almost nothing (it's not "go find
  the download page," it's "click the button that's already in front of
  you") but preserves a moment where an attentive user (or just a
  legitimately-busy one who defers it) isn't auto-updated into a
  same-day-compromised release. Given the cert situation isn't changing
  (Section 6 of installer-plan.md stands), this is the most defensible
  place to draw the line between "friction removed" and "risk accepted."

This is also why Section 2.4 recommends **still running Inno Setup's normal
(non-silent) wizard** after the user clicks "Download & install," rather
than shelling out `/VERYSILENT` — it's a second, cheap checkpoint (the
installer UI itself, with its own "Next"/"Finish" clicks) layered on top of
the first one, at zero extra engineering cost since it's just *not* passing
a silent-install flag. A future T3-style silent flow is not being ruled out
forever — if the project later buys a code-signing cert (Section 6 revisit
trigger in installer-plan.md), the calculus changes meaningfully (a valid
signature is a real trust root a silent updater could depend on, closer to
how Chrome/VS Code auto-update) and T3 could be revisited then.

## 4. Unsigned-binary risk — explicit treatment and mitigation

Direct answers to the task's specific questions:

**Does auto-downloading and running a new unsigned `setup.exe` make the
SmartScreen/trust problem worse?** Partially, and in a specific way, not a
blanket "yes." SmartScreen's reputation-based warning fires the same way
whether the user double-clicked a manually-downloaded file or the app
launched it on their behalf — automating the download doesn't change
*SmartScreen's* behavior at all (there's no way for the calling app to
suppress or pre-clear that warning without a signature, full stop). What
automation *does* change is the human-attention model described in Section
3: it shifts from "user actively chooses to download and run something
they went looking for" to "user clicks one button the app is already
showing them" — a smaller, but real, reduction in the moments where a
suspicious user might pause. T2's one-click-not-zero-click design is the
direct mitigation for that shift (Section 3).

**Mitigation: checksum verification.** Recommended, as defense-in-depth,
with an honest scope statement:

- **Implemented (2026-07-10, task `add-release-checksum-publishing`):**
  `.github/workflows/release.yml`'s "Build installer" step computes the
  SHA-256 of `opal-downloader-setup.exe` via `Get-FileHash` and writes it to
  `opal-downloader-setup.exe.sha256` in the canonical `sha256sum` line
  format (`<lowercase-hex-hash>  <filename>`, two spaces), uploaded as a
  second `gh release create` asset alongside the installer. This is a plain
  public release asset — fetching it needs no GitHub API scope beyond the
  same unauthenticated, public-repo access `internal/updater`'s
  `CheckLatest`/`Download` already use (Section 5), and its fixed two-token
  line format is trivially parsed (split on whitespace, take the first
  token) without a dedicated parser dependency.
- `internal/updater.VerifyChecksum` computes the downloaded file's SHA-256
  and compares it before allowing `/update/start` to launch the installer;
  mismatch aborts with a clear error shown in the GUI rather than silently
  proceeding.
- **What this actually protects against:** a corrupted or truncated
  download, and a MITM that tampers with the binary bytes *if* it can't
  also tamper with the checksum response (unlikely if both come from the
  same GitHub Releases API/CDN over HTTPS — see below).
- **What this does NOT protect against, and shouldn't be oversold as
  protecting against:** a genuinely malicious release published by whoever
  controls the repo/release process (they'd publish a matching checksum for
  their own malicious binary too). The checksum's trust root is still "you
  trust GitHub's release pipeline for this repo," identical to today's
  manual-download trust root — it is not a substitute for code signing, it
  only guards the transport/integrity layer, not the authenticity layer.
  Being explicit about this distinction matters so the checksum isn't
  mistaken for solving a problem it doesn't solve.
- **The other, arguably more load-bearing mitigation, which costs nothing
  extra:** use HTTPS end-to-end with Go's default `http.Client` (no custom
  `Transport`, no `InsecureSkipVerify`, no third-party "update server" — just
  `api.github.com` and `objects.githubusercontent.com`, both already
  TLS-certificate-validated by the stdlib). This is what actually rules out
  a DNS-spoofed or MITM'd fake "update server" redirecting users to an
  attacker's binary — a real risk if this plan invented any custom update
  endpoint, and a non-issue precisely because it doesn't (Section 5).

Net recommendation: ship checksum verification (small, cheap, real
defense-in-depth value against corrupted downloads and naive tampering) but
document its actual scope honestly in code comments/docs rather than
presenting it as a signature-equivalent trust mechanism — and lean on T2's
one-click human checkpoint (Section 3) as the actual mitigation for the
"malicious release" case that checksums can't cover.

## 5. Server-side component: not needed, polling GitHub directly is sufficient

No server-side component is needed, now or foreseeably:

- The GitHub Releases API (`GET
  /repos/{owner}/{repo}/releases/latest`) is public and requires no
  authentication for a public repo.
- Unauthenticated rate limit is 60 requests/hour **per source IP** — since
  every user's own machine calls it directly (not routed through a shared
  proxy), this project would need thousands of *concurrent* users hitting
  it within the same hour from behind the same IP (e.g. a NAT'd
  university network) before this became a real constraint, and even then
  only for that one check-once-per-launch call, not a sustained load.
- Checksums and release assets are served from the same GitHub-hosted
  infrastructure (`objects.githubusercontent.com`) — no separate hosting
  needed for those either.
- The only scenario that would ever require a server-side component is
  something this project doesn't need: per-user analytics/telemetry on
  update adoption, staged/canary rollouts, or serving different update
  channels (beta/stable) — none of which are goals here. **Prefer the
  zero-infrastructure answer; there's no concrete reason it won't hold.**

## 6. Effort estimate and what's prerequisite vs. this-task-specific

| Task | Effort | Depends on |
|---|---|---|
| 1. Fix hardcoded version string — add `-ldflags` injection (Section 2.1) | Small | None; also useful independent of this plan (accurate `--version` output today is arguably already a small bug) |
| 2. Establish an actual release-build process that produces predictably-named GitHub Release assets | Medium | Installer existing (installer-plan.md tasks 1–4); this plan's checker is only as reliable as this process's asset-naming consistency — **done**, see below |
| 3. `internal/updater` package: GitHub API client, version comparison, download, checksum verify (Section 2.2) | Medium | Task 1 |
| 4. GUI integration: banner on landing page, `/update` + `/update/start` routes, process-handoff-then-exit spike (Section 2.4) | Medium (the process-handoff piece is the main uncertainty) | Task 3 |
| 5. Checksum publishing as part of the release process (Section 4) | Small | Task 2 |
| 6. (Optional, near-free once Task 3 exists) CLI-side "update available" footer line on `sync`/`list`/`login` | Trivial | Task 3 |

**Overall estimate: small-to-medium**, same rough shape as the installer
itself — a few focused days once there's an actual release process to
target. The two real unknowns worth flagging honestly: (a) task 2 doesn't
exist at all yet and this plan is implicitly depending on it, and (b) the
Windows process-handoff behavior in task 4 ("launch installer, exit self
cleanly") needs a short live spike before committing to an effort number,
not just this design.

**Task 2 update (2026-07-10, task `add-release-build-workflow`):** the
release-build process this whole plan was implicitly depending on now
exists — `.github/workflows/release.yml`, triggered on `v*` tag pushes.
It builds `opal-downloader.exe` with `-ldflags` version injection, fetches
a matching Chromium via the pinned `playwright-go` driver, invokes
`scripts/build-installer.ps1` to produce `opal-downloader-setup.exe`, and
publishes it (plus a `.sha256` sidecar) as a GitHub Release asset via
`gh release create`. Concretely, this confirms the two things
Sections 2.2/2.3 above were written against as assumptions:

- **Tag format is `vX.Y.Z`** (e.g. `v0.2.0`) — `tag_name` in the
  `/releases/latest` response will always have the leading `v` this
  section's version-comparison code needs to strip.
- **Asset name is the unversioned `opal-downloader-setup.exe`** — exactly
  the name Section 2.2 already used as its example, so `CheckLatest`'s
  asset-picking logic can hardcode this string rather than pattern-match
  a version into the filename.

See `docs/installer-plan.md` Section 9's Task 4 note and that task's PR for
the full write-up and live-verification status.

**Task 5 update (2026-07-10, task `add-release-checksum-publishing`):**
already fully done as a side effect of Task 2's own predictable-asset-shape
goal — `release.yml` was writing the `.sha256` sidecar from the start, this
task just confirmed it meets both of this table row's acceptance criteria
(public unauthenticated fetch, no new API scope; canonical
easily-parsed line format) and added the explicit threat-model note above
in Section 4. No workflow changes were needed.

## 7. Should any of this be pulled into v1?

**No — installer-plan.md's existing v1 scope (manual re-run) should stand
as-is.** Three independent reasons, any one of which would be sufficient on
its own:

1. **There's no artifact to update yet.** No installer exists; there has
   never been a first release, let alone a second one to check against. An
   update checker is meaningless before that.
2. **The version-embedding prerequisite (Section 2.1) and the
   release-process prerequisite (Section 6, task 2) are both real, and
   neither is "free" — pulling this in now would delay the actual v1
   installer ship (installer-plan.md's own stated priority: "small-to-medium...
   should not block other in-flight work") for a feature that has nothing
   to check yet.**
3. **The checker's reliability depends on a release process that doesn't
   exist and hasn't been exercised even once.** Designing the checker's
   asset-name matching, tag-format assumptions, etc. against a hypothetical
   process rather than one or two real releases risks building it against
   assumptions that turn out wrong the first time a real release happens —
   better to let the release process stabilize through 1–2 manual
   `setup.exe` releases first, then build the checker against how releases
   *actually* look, not how this plan guesses they'll look.

Recommended sequencing: ship the v1 installer per installer-plan.md
unchanged → cut the first 1–2 releases manually (re-run per v1's existing
story) → once the release process (task 2 above) has been exercised for
real and its shape is stable, pick up this plan's Sections 2–4 as a
standalone follow-up implementation task in the local queue. This plan
itself is the reusable design for that later task — nothing here needs to
be re-derived when that time comes.
