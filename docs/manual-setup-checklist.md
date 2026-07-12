# Manual Setup Checklist (login / sync tier)

This checklist covers the part of the fresh-install flow that **cannot** be
automated because it requires a real TU Dresden OPAL account, real
credentials, and (usually) real 2FA. It's the companion to:

- [`scripts/test-fresh-install.ps1`](../scripts/test-fresh-install.ps1) - covers
  everything before this (clone, Playwright install, build, `init`, config
  validation) without needing credentials.
- [`docs/setup-friction.md`](setup-friction.md) - friction points and
  suggestions found while doing the automatable part of this dry run.

**Do this only as yourself, with your own real OPAL credentials.** Never share
credentials or session-state files with anyone, including AI assistants -
don't paste them into a chat, screenshot them, or commit them.

## Prerequisites before starting this checklist

- [ ] You've already run through the automatable tier successfully - either by
      running `scripts/test-fresh-install.ps1` yourself, or by having an
      assistant run it and confirm it passed.
- [ ] You have a real, working copy of the repo built (`opal-downloader.exe`
      on Windows), with a `config.yaml` you've edited to match your actual
      setup (real `download_path`, real `courses` list or `"*"`).
- [ ] You know your TU Dresden / Bildungsportal Sachsen login and have your
      2FA device (TU-Fast / authenticator) ready.

Record start time here so you can note total setup time at the end: `___________`

## Step 0: browser profile setup (one-time)

Before running `login` for the first time, decide which browser-profile
strategy you're using - see `docs/browser-profile-strategy.md` for the full
write-up and rationale.

- **Option A (recommended for new users, fewer clicks via the GUI):** set up
  a dedicated, never-used-for-anything-else browser profile just for
  opal-downloader, so your everyday browser is never locked or closed.
  - **Via the GUI (fastest):** open the app, go to Settings, click "Set up a
    dedicated TU-Fast browser profile" (`/tufast-setup`), then "Create
    folder & open TU-Fast in the Chrome Web Store". This creates the
    directory for you and opens a browser window already pointed at TU-Fast's
    Web Store listing - you only need to click "Add to Chrome"/"Add to
    Brave" and then log into OPAL/Shibboleth once to complete 2FA/device
    registration (both are consent/identity actions and stay manual). If
    TU-Fast is *already* installed and logged in in another browser profile
    on this same computer (e.g. your everyday Brave/Chrome), the same page
    also offers "Copy TU-Fast login data" - a local, offline copy of just
    TU-Fast's stored login/2FA data (never its extension install, never
    anything else in that profile) that skips the second 2FA login entirely.
    See docs/browser-profile-strategy.md's "Transplanting TU-Fast login
    data" section for what this does and doesn't cover (same-machine only).
  - **Manually (no GUI):**
    1. Create an empty directory, e.g. `~/.opal-downloader/login-profile`.
    2. Launch Brave against it once:
       `brave.exe --user-data-dir="<path>"` (opens a completely fresh, empty
       profile - separate from your everyday one).
    3. In that window, install TU-Fast from the Chrome Web Store and log into
       OPAL/Shibboleth once to complete 2FA/device registration for TU-Fast in
       this profile.
    4. Close that window, then point `config.yaml`'s `browser_user_data_dir` at
       the same path (and `browser_profile_directory: "Default"`) - no other
       config or code changes required.
- **Option B (if you already use TU-Fast in your everyday browser):** just
  set `browser_user_data_dir` / `browser_profile_directory` to your real
  profile's paths. Note that you'll need to fully close your browser before
  running `login`, or whenever a saved session expires and `login` falls
  back to interactive.

- [ ] Run `opal-downloader status` and confirm it reports the browser
      profile as healthy (no "doesn't exist" / "doesn't look like a real
      browser profile" error) before proceeding to Step 1.

## Step 1: `login`

Run:

```powershell
./opal-downloader.exe login
```

- [ ] **Pass/Fail:** A browser window opened automatically.
- [ ] **Pass/Fail:** The OPAL login page loaded (not a blank page / error).
- [ ] **Pass/Fail:** You were able to complete TU-Fast / 2FA login without the
      tool interfering (closing the window, timing out too early, etc).
- [ ] **Pass/Fail:** After login, the terminal printed something like
      `Login successful! Session state saved.` and a session state file path.
- [ ] Note the **time taken** from running the command to seeing "Login
      successful": `___________`
- [ ] Note any **friction**: confusing prompts, unclear waiting states, browser
      window losing focus, unexpected extra login steps, timeout too short/long, etc.
      `___________________________________________________`

If this step fails, capture the exact error text before retrying - it's useful
feedback even if a retry then succeeds.

## Step 2: `list`

Run:

```powershell
./opal-downloader.exe list
```

- [ ] **Pass/Fail:** Command reused the saved session (no browser window /
      no re-login prompt).
- [ ] **Pass/Fail:** Real courses you're enrolled in actually showed up.
- [ ] **Pass/Fail:** The course names look correct (not truncated, garbled, or
      obviously missing courses you expected).
- [ ] Note **how many courses** were listed vs. how many you expected:
      `___________________________________________________`
- [ ] Note the **time taken**: `___________`
- [ ] Note any **friction**: unexpected courses, missing courses, unclear
      output formatting, etc. `___________________________________________________`

## Step 3: `sync` (first real download)

Edit `config.yaml` first if needed so `courses:` / `course_folders:` reflect
what you actually want downloaded (not just placeholder values), then run:

```powershell
./opal-downloader.exe sync
```

- [ ] **Pass/Fail:** Command reused the saved session (no re-login needed).
- [ ] **Pass/Fail:** Files actually downloaded to the configured
      `download_path`.
- [ ] **Pass/Fail:** Files landed in the expected folder structure (matching
      `course_folders` rules / `default_course_folder` / course-name folders).
      If you're using per-section subfolders or destination overrides
      (`use_section_subfolders` / `section_folder_names` /
      `subfolder_destinations`, editable via `config.yaml` or the GUI
      Settings page), see
      [docs/OPERATIONS.md](OPERATIONS.md#per-subfolder-download-destinations)
      for a worked example and check files landed under the expected
      per-section paths too.
- [ ] **Pass/Fail:** The final summary line (`Done. downloaded=N skipped=N
      errors=N`) matches what you expected (errors=0, downloaded roughly
      matches file count in your courses).
- [ ] Note **how long the sync took** and roughly how much data: `___________`
- [ ] Note any **friction**: unexpected files skipped/downloaded, wrong
      folder placement, unclear progress output, slow performance, etc.
      `___________________________________________________`

If `errors > 0`, check the printed error lines - do they mention which
file/course failed clearly enough to act on? `___________________________`

## Step 4: re-run `sync` a second time (incremental behavior)

Without changing anything, run the exact same command again:

```powershell
./opal-downloader.exe sync
```

- [ ] **Pass/Fail:** Second run reports `downloaded=0` (or very close to 0 -
      only genuinely new/changed files) and a `skipped=N` count matching the
      number of files from the first run.
- [ ] **Pass/Fail:** No files were re-downloaded unnecessarily (check
      timestamps on a few files in `download_path`, or watch for "Done."
      completing much faster than the first run).
- [ ] **Pass/Fail:** The manifest file `.opal-sync.manifest.json` exists
      inside your configured `download_path` and its `updated_at` / file
      entries look sane.
- [ ] Note the **time taken** for this second run vs. the first (should be
      much faster): `___________`
- [ ] Note any **friction**: files re-downloaded that shouldn't have been,
      manifest in an unexpected location, unclear "skipped" reporting, etc.
      `___________________________________________________`

## Optional: `sync --force` (re-download everything)

Only do this if you want to verify the force-download path; it will re-fetch
everything and take as long as the first `sync`.

```powershell
./opal-downloader.exe sync --force
```

- [ ] **Pass/Fail:** All previously-downloaded files were re-downloaded
      (`downloaded=N` matches total file count, `skipped=0`).

## Wrap-up

- [ ] Total time from first `login` to a fully synced course set:
      `___________`
- [ ] Biggest single friction point encountered: `___________________________`
- [ ] Anything that felt genuinely broken (not just rough around the edges):
      `___________________________________________________`
- [ ] Anything you had to figure out that wasn't documented anywhere:
      `___________________________________________________`

Report back (to whoever asked you to run this, or as a GitHub issue/PR
comment) with: this filled-in checklist, plus the overall pass/fail verdict
for the login -> list -> sync -> re-sync flow.
