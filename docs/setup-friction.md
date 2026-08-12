# Setup Friction Report

This document records what it actually felt like to install and set up
`opal-downloader` from scratch, following only the Installation / Quick Start
steps in `README.md`. It was produced by simulating a brand-new user in a
disposable temp directory (fresh `git clone`, no reused build artifacts) and
is the companion to:

- [`scripts/test-fresh-install.ps1`](../scripts/test-fresh-install.ps1) - automated
  re-run of everything below that doesn't need real OPAL credentials.
- [`docs/manual-setup-checklist.md`](manual-setup-checklist.md) - the human-only
  tier (`login`, `list`, `sync`, incremental re-sync) that needs real TU Dresden
  credentials/2FA.

Findings are ordered roughly by how early in the flow a new user would hit them,
with the most impactful ones first.

## 1. README clone URL is wrong (repo name casing mismatch)

The README's Installation section says:

```bash
git clone https://github.com/alu-developer/opal-downloader.git
```

The actual GitHub repository is `alu-developer/Opal_downloader` (capital `O`,
underscore, no hyphen). Copy-pasting the README's exact command fails
immediately:

```
Cloning into 'opal-downloader'...
remote: Repository not found.
fatal: repository 'https://github.com/alu-developer/opal-downloader.git/' not found
```

This is the very first command in the README and it does not work as written.
For a brand-new user this is an immediate, confusing dead end (nothing in the
error suggests "wrong casing" - it reads like the whole project might not
exist or is private).

**Suggestion:** fix the clone URL in the README to match the real repo name
(`Opal_downloader`), or rename the GitHub repo to match the module path /
binary name (`opal-downloader`) for consistency across module path, binary
name, and repo name. The module path (`github.com/alu-developer/opal-downloader`)
and built binary name (`opal-downloader`) already use the hyphenated lowercase
form, so aligning the repo name to match is probably the least surprising fix.

## 2. On Windows, the exact README build command produces a binary that PowerShell's call operator won't reliably run

The README says:

```bash
go build -o opal-downloader .
```

On Windows this produces a literal, extensionless file named `opal-downloader`
(Go does not append `.exe` when an explicit `-o` name without an extension is
given). That extensionless file **is** a valid, runnable Windows executable
(`Get-Command` resolves it as `CommandType: Application`, and it runs
correctly via `Start-Process` or from Git Bash's `./opal-downloader`) - but
invoking it directly with PowerShell's call operator, e.g.:

```powershell
./opal-downloader init
```

did not reliably execute during this dry run: it returned with no output and
no exit code, as if it silently did nothing. Renaming the exact same file to
`opal-downloader.exe` and re-running the identical command worked immediately.

Since PowerShell is the default shell on Windows (and the README doesn't
specify git-bash/WSL), a Windows user following the README literally is likely
to hit this. It's a quiet, confusing failure mode - no error, no crash, just
nothing happening - which is worse than a loud one.

**Suggestion:** document `go build -o opal-downloader.exe .` (with the
explicit `.exe` suffix) as the Windows-specific build command, or provide
separate copy-paste blocks per OS. This also sidesteps the ambiguity of
`./opal-downloader` meaning different things depending on shell.

## 3. No fast, safe, offline way to check "is my config + auth good?"

`list` and `sync` both call the same session logic: if no session-state file
exists yet (i.e. before the user has ever run `login`), they don't fail fast
with something like "not logged in, run `opal-downloader login` first."
Instead they silently proceed to open a real, visible browser window, navigate
to the live OPAL URL, and wait **up to 5 minutes** for an interactive login to
complete (`internal/scraper/session.go`, `ensureSession`/`WaitForSelector`
with a 300000ms timeout).

This is invisible from the README - nothing warns a new user that running
`list` too early opens a GUI browser and blocks for minutes. It also means
this exact step could not be safely automated in `scripts/test-fresh-install.ps1`
without either genuinely attempting a live login (out of scope / against the
task's constraints) or working around it with an intentionally-unroutable
`opal_url` to force a fast failure in the browser layer instead (which is what
the script does, and it's the reason the script's `list` check only proves
"config parses fine," not "auth fails cleanly").

**Suggestion:** add an explicit, fast pre-check - e.g. `opal-downloader
status` or `--check` - that: (a) verifies config.yaml parses, (b) checks
whether a session-state file exists and looks non-empty, and (c) if not,
prints something like `Not logged in yet. Run: opal-downloader login` and
exits immediately instead of opening a browser. This also makes the "fails
with a clear auth error, not a config-parsing error" expectation from a
fresh setup actually true today it either hangs waiting for interactive login,
or (with an unreachable/misconfigured URL) surfaces a raw Playwright error,
neither of which is a clean auth message.

## 4. Raw Playwright/Chromium errors leak through to the user

When `opal_url` is unreachable (tested here via an intentionally-invalid
`127.0.0.1:1` address, but the same would happen with a typo'd URL, no
network, or a corporate proxy), the error surfaced is:

```
Error: Frame.Goto http://127.0.0.1:1/opal/: playwright: net::ERR_UNSAFE_PORT at http://127.0.0.1:1/opal/
Call log:
  - navigating to "http://127.0.0.1:1/opal/", waiting until "domcontentloaded"
```

This is a raw Playwright/Chromium network-stack error, not a message written
for this project's users. Someone with zero context on Playwright internals
would not know whether this means "bad URL," "no internet," "blocked port,"
or "bug in the tool."

**Suggestion:** catch navigation errors in `ensureSession`/`isAuthenticated`
and wrap them with a one-line, project-specific hint, e.g. "Could not reach
OPAL at <url> - check your internet connection and `opal_url` in config.yaml"
before printing the underlying Playwright error (which can still be shown for
debugging, just not as the only line).

## 5. Playwright browser install is completely silent on success

```bash
go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6100.0 install
```

produced **zero stdout output** and exit code 0, even the first time browsers
are installed (confirmed separately: this machine's `%LOCALAPPDATA%\ms-playwright`
cache already existed, so on a truly first-ever run there would likely be some
download progress output from Playwright itself - but there's no
project-level confirmation either way). A brand-new user has no way to tell
"it worked" from "it silently did nothing" just by looking at the terminal
after this step.

**Suggestion:** after this step succeeds, have `init` or a wrapper command
print something like `Playwright browsers ready.` A `./opal-downloader setup`
meta-command (see finding 7) could run this and print a clear confirmation.

## 6. `init`'s "Next steps" say to run `login` before editing `config.yaml`

`init`'s printed next steps are:

```
Next steps:
  1. Run: opal-downloader login
  2. Edit config.yaml with your download path and course patterns
  3. Run: opal-downloader sync
```

This orders `login` before editing `config.yaml`, but `login` itself reads
`opal_url` / `browser_executable` / `browser_user_data_dir` from config.yaml
(via `config.LoadCredentials`), and the default `config.example.yaml` has a
placeholder `download_path` (`D:/Uni/OPAL`) that only makes sense on the
original author's machine. A new user who follows steps literally in order
would run `login` against a config they haven't looked at yet.

**Suggestion:** swap the order - "1. Edit config.yaml, 2. Run login, 3. Run
sync" - or explicitly note that step 2 is optional before login if defaults
are acceptable (they mostly are, aside from `download_path`).

## 7. No single meta-command ties the setup flow together

Setup requires four separate manual steps (clone, playwright install, build,
init) before the user even gets to `login`. Nothing enforces or checks
ordering, and there's no single "make sure everything is ready" entry point.

**Suggestion:** consider a `./opal-downloader setup` (or a documented
`scripts/setup.ps1`, matching the existing `scripts/dev.ps1` convention) that:
runs the Playwright install, confirms the Go build is current, runs `init` if
`config.yaml` doesn't exist, and prints a final checklist of what's left
(edit config.yaml, run login). This wouldn't replace the documented manual
steps for transparency, but would give new users (and this test script) a
single, well-tested path to validate.

## 8. `dump-links` is undocumented in the README

`--help` and the command switch statement both support a `dump-links`
subcommand, but it is not mentioned anywhere in the README's Commands table.
Not a blocker, but a minor documentation gap a new user might stumble on via
`--help` and wonder what it's for / whether it's supported.

## 9. Things that worked well (worth preserving)

To be fair to the current state, several things about the fresh-install
experience were genuinely good and worth calling out so they aren't
accidentally regressed:

- `init` is idempotent and clearly says `skip (exists): <path>` on re-run.
- Missing-config and malformed-YAML errors are both clear, actionable, and
  include the offending file path (and line number for YAML errors) -
  `config file not found: <path>` and `invalid yaml in <path>: yaml: line N: ...`.
  These are exactly the kind of error message a brand-new user can act on
  without any project-specific context.
- `go build -o opal-downloader .` itself (once you know to add `.exe` on
  Windows) is fast and dependency resolution "just works" via `go.mod`/`go.sum`
  with no extra steps.
- `--help` output is accurate and lists every real subcommand and flag
  (aside from the `dump-links` README gap above).

## Summary of concrete suggestions — all eight shipped

**Audited against the code on 2026-07-30, item by item. Everything in this table
is done.** It had been left reading as an open to-do list, which is how a later
session ends up either redoing finished work or believing the tool still has
traps it does not. The prose above the table is the *original* dry-run report and
is deliberately preserved as written — read it as a record of what the first-run
experience was like then, not as a description of the tool now.

| # | Suggestion | Status, and how it was checked |
|---|---|---|
| 1 | Fix README clone URL casing (`Opal_downloader`) | **Done.** `README.md:47`, and `scripts/test-fresh-install.ps1` clones from it successfully every run. |
| 2 | Document `go build -o opal-downloader.exe .` for Windows | **Done.** `README.md:55-63` gives separate Linux/macOS and Windows build blocks plus a note naming the extensionless-file trap. |
| 3 | Fast, offline `status` that reports auth state without opening a browser | **Done.** `runStatus` (`cmd/opal-downloader/root.go:321`) reads `os.Stat` on the session-state file and prints "Not logged in yet. Run: opal-downloader login". No Playwright, no network. |
| 4 | Wrap raw Playwright navigation errors with a friendlier hint | **Done.** A live run against an unreachable URL prints `could not reach OPAL at <url> - check your internet connection and opal_url in config.yaml` first; the raw Playwright text still follows it as detail. |
| 5 | Print a confirmation line after Playwright install succeeds | **Done** for the path users are pointed at: `setup` prints "Installing Playwright browser binaries..." then "Playwright browsers ready." The silence in the table above belongs to upstream's `go run ... playwright install` CLI, which `setup` no longer shells out to (it calls `playwright.Install` directly). |
| 6 | Reorder `init`'s "Next steps" so editing config.yaml comes first | **Done.** Live `init` output starts at "1. Edit config.yaml with your download path and course patterns". |
| 7 | Add a `setup` meta-command | **Done.** `case "setup"` → `runSetup`, which installs browsers and creates the config in one step. |
| 8 | Document `dump-links` in the README Commands table | **Done.** `README.md:149`. |

**Update 2026-08-12 (autopilot, source reading, no live run): the closing
paragraph below is now itself out of date, three defaults later.** With a valid
`opal_url` and no saved session, `list`/`sync` no longer open a browser and wait
for interactive login by default: `ensureSession` (`internal/scraper/session.go`)
runs an offline reachability pre-check (`netcheck.Describe`) before anything
else, so an unreachable/misconfigured `opal_url` fails in well under a second
with a written-for-humans sentence, not a raw Playwright dump - and discovery
itself is HTTP-first by default (`OPAL_HTTP_DISCOVERY=2`), so a *reachable* OPAL
with no saved session is the only remaining path that opens a browser at all
(confirmed unaffected by setting `OPAL_HTTP_DISCOVERY=0`, since that only
changes discovery after a session already exists). `status` still answers the
"am I logged in" question offline, but it's no longer the only thing standing
between a new user and a raw Playwright error - that gap is closed. The
original paragraph is kept below, struck through in spirit but not in text, for
the same "record of what it was like then" reason findings 3/4's prose is kept
above.

~~**What is still genuinely rough is not in this table**, because it needs
credentials to see: with a valid `opal_url` and no saved session, `list`/`sync`
still open a real browser and wait for an interactive login rather than failing
fast — `status` answers the question offline now, but only if the user thinks to
ask it.~~ `docs/manual-setup-checklist.md` covers the credentialed tier this
document couldn't automate.
