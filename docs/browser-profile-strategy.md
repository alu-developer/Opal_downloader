# Browser-profile strategy for new-user onboarding

Status: **implemented**. The dedicated-second-profile default, the
`status`/`checkBrowserProfileHealth` pre-flight check, and (as of task
`ease-second-profile-tufast-setup`, 2026-07-12) a GUI page at
`/tufast-setup` that both cuts Step 0's manual clicks and offers an optional
TU-Fast login-data copy shortcut are all live in `master`. What follows below
was originally written as a planning document before implementation; it's
kept as the design rationale/trade-off record rather than rewritten, but
treat anything phrased as a future recommendation as already shipped unless
a later section (search for "2026-07-1" dates) says otherwise.

## Recommendation, up front

Make the **dedicated second profile** (`~/.opal-downloader/login-profile`,
live-verified in `investigate-independent-second-profile-for-login.md`) the
**documented default for new users**, not the user's everyday browser
profile. Keep launching directly against the real profile
(`session.go`'s current behavior) fully supported as a documented
**opt-out**, clearly presented as "use this instead if you already have
TU-Fast working day-to-day and don't mind closing your browser
occasionally."

Do **not** make this an interactive prompt during `init`/`setup`. Present it
as a clearly-labelled fork in the docs (README Quick Start,
`manual-setup-checklist.md`) with a strong default recommendation, plus a
config-file comment explaining the trade-off. Reasons for "docs fork, not
interactive prompt" are in "Should this be a user choice or a hard default?"
below.

Pair this default with a **pre-flight health check extension to `status`**
(detailed below) so the dedicated profile's "one-time" setup failing
silently — the user's stated worry — turns into a fast, actionable error
instead of an opaque hang or a mysterious TU-Fast-less browser window.

## Why this isn't a rubber stamp: working through the trade-off

### The naive case for Strategy 1 ("reuse what's already there") doesn't hold for a brand-new user

Strategy 1's real advantage is zero *extra* setup for someone who **already**
has TU-Fast installed and working in their everyday Brave/Chrome. But
`opal-downloader`'s actual first-time user is a TU Dresden student setting
this tool up for the first time — there's no reason to assume they already
have TU-Fast installed at all. Confirmed via search: TU-Fast (`TUfast TU
Dresden`) is a normal Chrome Web Store extension (also published for
Firefox), not something bundled with Brave or tied to a specific browser —
so a genuinely new user installs it fresh regardless of which strategy is
chosen. For that user, "reuse the profile you already have" and "set up a
brand-new dedicated profile" cost the **same one-time effort**: install a
Chromium browser (if not already present), install TU-Fast from the store,
log into OPAL/Shibboleth once to complete 2FA/device registration.

The difference only shows up **after** that one-time setup:

- Strategy 1 (everyday profile): every future interactive login or
  session-expiry fallback requires the user to **fully close their browser**
  first (`isUserDataDirLocked` pre-flight in `internal/scraper/profile.go`
  returns a clear error otherwise). Reading `session.go`'s `launchBrowser`
  branch (`s.browserUserDataDir != "" && !headless && !useSavedState`)
  confirms this only fires for interactive login / expired-session fallback
  — a normal `sync`/`list` with a still-valid saved session goes through the
  separate headless bundled-browser-plus-stored-cookies path
  (`useSavedState` branch) and never touches the real profile or requires
  closing anything. So the "close your browser" tax is not paid on *every*
  sync, but it recurs indefinitely at every login/re-login, for the life of
  the tool.
- Strategy 2 (dedicated profile): the same one-time setup cost, but the
  "close your browser" tax disappears **permanently** afterward — the
  dedicated profile isn't the user's daily-driver browser, so there's never
  a reason it would be open and locked when opal-downloader wants it.

Given CLAUDE.md's explicit priority — "friction reduction, both first-install
**and long-term/maintenance friction**, outranks almost everything else" —
a recurring, indefinite friction (Strategy 1) is a worse fit than a one-time
setup cost that's identical to the alternative's one-time cost (Strategy 2),
even before weighing anything else.

### Where Strategy 1 still legitimately wins

For a user who **already has TU-Fast working** in their everyday browser
today (a real, non-trivial slice of the target audience — this codebase's
own maintainer was exactly this case per `fix-login-crash-and-missing-tu-fast.md`),
migrating to Strategy 2 means redoing the TU-Fast install + OPAL login in a
new profile, because Shibboleth/TU-Fast's trust and any 2FA device
registration is tied to the specific browser profile it was completed in —
it does not transfer by pointing config at a different directory. For this
user, Strategy 1 costs zero extra setup; Strategy 2 costs one redundant
setup pass to get the "never lock my browser" benefit. That's a legitimate
reason for this user to deliberately opt out of the new default and keep
using Strategy 1 — which is exactly why Strategy 1 must remain a fully
documented, first-class option, not deprecated or hidden.

### Conclusion from the trade-off

- Brand-new user, nothing installed yet: Strategy 2 strictly dominates
  (same setup cost, strictly less recurring friction).
- Existing TU-Fast user migrating in: Strategy 1 strictly dominates for
  *setup* cost, Strategy 2 dominates for *ongoing* friction. Genuine
  judgment call for that specific user — hence "opt-out, not removed."

Because new-user onboarding is the explicit optimization target in this
task, and the "already has TU-Fast" case is well served by an equally
well-documented opt-out, the recommendation is Strategy 2 as the default
new-user path.

## The central risk: is the dedicated profile's "one-time" setup actually durable?

This is the direct answer to the user's stated worry ("nicht jedes Mal
TU-Fast komplett neu einrichten müssen") and the part of this plan that
determines whether Strategy 2 is safe to recommend at all. Going through
each vector named in the task:

1. **Browser auto-updates rotating profile format / extension signing
   state.** Low risk, and not specific to Strategy 2. Chrome/Brave update
   themselves in-place constantly for *every* profile, including a user's
   main daily-driver one, without requiring extensions to be reinstalled or
   re-authenticated — this is a fundamental compatibility guarantee those
   browsers already maintain for any long-lived profile. The dedicated
   profile is not exposed to anything the user's main profile isn't already
   exposed to today. No mitigation needed beyond what already exists (none
   specific).

2. **TU-Fast's own extension updates requiring re-auth.** Also not specific
   to Strategy 2 — this risk exists identically whether TU-Fast lives in the
   user's main profile or a dedicated one, since Web Store auto-update
   applies per-profile regardless of which profile it is. If TU-Fast ever
   ships an update that invalidates its stored local login data, both
   strategies are equally affected and equally recoverable (redo the TU-Fast
   login inside whichever profile is configured — no opal-downloader code
   involved either way).

3. **The dedicated profile directory being deleted/moved/corrupted.** This
   *is* the one genuinely elevated risk specific to Strategy 2. A user's
   main browser profile is extremely unlikely to be casually deleted — it's
   their daily driver, high-visibility, and every browser warns heavily
   before letting you nuke it. A dedicated profile living quietly under
   `~/.opal-downloader/login-profile` has no such protection: a user doing
   disk cleanup, or resetting `~/.opal-downloader/` because they assume
   everything under that directory is disposable cache (it currently also
   holds the session-state file and manifest-adjacent data), could delete it
   without realizing it holds live authenticated browser state that took a
   real 2FA/device-registration flow to establish. **This is the risk the
   health check below exists to catch fast**, and it's also worth an
   explicit doc callout (a comment where the directory is referenced, plus a
   README note) that this specific subdirectory is not disposable cache.

4. **`init` resetting this directory.** Checked directly:
   `cmd/opal-downloader/root.go`'s `runInit`/`runSetup` only ever touch
   `config.yaml` (copy `config.example.yaml` over it if missing, or skip if
   present) — neither function reads, writes, or deletes anything under
   `browser_user_data_dir`. Confirmed no code path today resets or touches
   this directory on re-init. This should be treated as an **invariant to
   preserve** going forward (worth a one-line test or at least a code
   comment on `runInit`/`runSetup` warning future editors not to touch
   `browser_user_data_dir` paths), not a currently-live risk.

**Net assessment:** three of the four named risks are low/shared-with-either-strategy;
the fourth (accidental deletion of a low-visibility directory) is real but
fully addressable with a fast, cheap, offline pre-flight check — which
`opal-downloader` already has the scaffolding for (`status`, see
`docs/setup-friction.md` #3). This tips the balance decisively toward
"Strategy 2 is safe to recommend, provided the health check ships alongside
it" rather than "durability is unproven, don't default to it yet."

## Health-check design (extends `docs/setup-friction.md` #3 and the existing `status` command)

`cmd/opal-downloader/root.go`'s `runStatus` (added since `docs/setup-friction.md`
#3 was written) currently checks: config parses, and whether
`session_state_file` exists and is non-empty. It does **not** yet look at
`browser_user_data_dir` at all. Extend it — this directly answers the task's
"decide whether that check should also validate the dedicated profile's
TU-Fast state" question:

- **Applies whenever `browser_user_data_dir` is configured, regardless of
  which strategy the user picked** — a real-profile user benefits from this
  exact check just as much as a dedicated-profile user (e.g. their real
  profile's TU-Fast could equally get uninstalled or corrupted). Don't
  special-case it to only the dedicated-profile path.
- Checks to add, all filesystem-only (no browser launch, stays fast and
  offline per the existing `status` design goal):
  1. Does `browser_user_data_dir` exist at all? If configured but missing →
     hard, actionable message: `"browser_user_data_dir is set to <path> but
     that directory doesn't exist. If you were following the dedicated
     browser-profile setup, re-run the one-time setup steps in
     docs/browser-profile-strategy.md."`
  2. Is it non-empty / does it look like a real Chromium profile (e.g. does
     `<dir>/<profile-directory-or-"Default">/Preferences` exist)? If not →
     same class of hard message.
  3. Does the TU-Fast extension folder look present —
     `<dir>/<profile-directory-or-"Default">/Extensions/aheogihliekaafikeepfjngfegbnimbk`
     (the extension ID confirmed live in `fix-login-crash-and-missing-tu-fast.md`)?
     If the profile directory itself is fine but this is missing, this is a
     **soft warning, not a hard failure** — "manual login without TU-Fast"
     is still a legitimate, supported (if slower, human-attended) path, so
     don't block on it. Print something like: `"Note: TU-Fast extension not
     detected in this browser profile. Logins will need manual 2FA each
     time. If you expected TU-Fast to be set up here, see
     docs/browser-profile-strategy.md."`
- Keep the hardcoded extension ID check honest about being a heuristic in
  a code comment — it's specific to the current TU-Fast build and this
  OPAL/Shibboleth flow; if TU-Fast's ID ever changes or another
  Bildungsportal Sachsen-instance-specific extension is used instead, this
  check degrades to "soft warning always fires," not a false hard failure,
  which is an acceptable failure mode (it only makes the check less useful,
  never wrong in a blocking way).
- This slots into `runStatus` right after the existing session-state-file
  check, before it prints "Logged in" — i.e. `status` becomes: config OK →
  browser-profile health (if configured) → session-state presence →
  overall verdict. All still zero-browser-launch, matching the "fast,
  offline" design goal from `setup-friction.md` #3.

## Concrete onboarding-flow changes

Referencing exact current locations, in the order a new user would hit them:

1. **`config.example.yaml`** (repo root): the `browser_user_data_dir` /
   `browser_profile_directory` comment block is currently **stale** — it
   describes the copy-into-`~/.opal-downloader/browser-profile` behavior
   that PR #20 removed (see "Also found while researching this" below; this
   needs fixing regardless of which strategy is chosen as default). Once
   fixed, rewrite it to present the dedicated-profile path as the
   recommended default, with the real-profile path as the documented
   alternative, e.g.:
   - Recommended: point `browser_user_data_dir` at a dedicated profile such
     as `~/.opal-downloader/login-profile` that you set up once per
     `docs/browser-profile-strategy.md` — your everyday browser is never
     locked or closed.
   - Alternative: point it at your real Brave/Chrome profile directly if
     you already have TU-Fast working there — note that `login` and any
     session-expiry fallback will then require your browser to be fully
     closed.
   Leave the field itself defaulting to `""` (Playwright's bundled browser,
   no TU-Fast) — don't silently point new users' example config at a path
   that doesn't exist on their machine yet; the comment guides them to
   *create* one of the two options deliberately.

2. **`cmd/opal-downloader/root.go`, `runInit`'s "Next steps"** (lines
   86–89) and **`runSetup`'s "Next steps"** (lines 133–136): both currently
   read (after the already-known reordering fix from `setup-friction.md` #6):
   ```
   1. Edit config.yaml with your download path and course patterns
   2. Run: opal-downloader login
   3. Run: opal-downloader sync
   ```
   Insert a step between 1 and 2 that names the fork explicitly without
   forcing an interactive decision:
   ```
   1. Edit config.yaml with your download path and course patterns
   2. Set up browser login: for TU-Fast auto-login without ever locking your
      everyday browser, see docs/browser-profile-strategy.md (recommended);
      or leave browser_user_data_dir empty / point it at your everyday
      profile if you'd rather skip that setup and log in manually or reuse
      an existing TU-Fast install
   3. Run: opal-downloader login
   4. Run: opal-downloader sync
   ```

3. **`docs/manual-setup-checklist.md`**: insert a new "Step 0: browser
   profile setup (one-time)" before the current "Step 1: `login`", giving
   the user the fork explicitly:
   - Option A (recommended for new users): follow the dedicated-profile
     steps already written and live-verified in
     `investigate-independent-second-profile-for-login.md`'s "One-time
     manual setup" section (create `~/.opal-downloader/login-profile`,
     launch the browser against it once, install TU-Fast from the Web
     Store, log into OPAL/Shibboleth once) — reuse that exact text, it's
     already accurate and tested.
   - Option B (if you already use TU-Fast in your everyday browser): just
     set `browser_user_data_dir`/`browser_profile_directory` to your real
     profile's paths; note you'll need to close your browser before
     `login` or whenever a session expires.
   - Add a checklist line after either option: run `opal-downloader status`
     and confirm it reports the browser profile as healthy (once the health
     check above ships) before proceeding to Step 1.

4. **README.md**:
   - **Quick Start** section: add a step between `init` and `login` mirroring
     the `root.go` "Next steps" change above, linking to
     `docs/browser-profile-strategy.md`.
   - **"TU-Fast / Brave Setup" section** (current lines ~174–200): this is
     the most out-of-date part of the README — it fully describes the
     copy-to-`~/.opal-downloader/browser-profile` behavior that PR #20
     deleted (`prepareBrowserProfile`, the "one-time snapshot" language, "the
     copy is a one-time snapshot, delete the folder to force a refresh").
     None of this matches current code (`internal/scraper/session.go`
     launches directly against `browser_user_data_dir`, no copy exists at
     all anymore). This section needs a full rewrite independent of this
     plan's recommendation — see "Also found while researching this" below.
     Once rewritten, it should present both strategies with the dedicated
     profile as the lead/recommended path and a link to
     `docs/browser-profile-strategy.md` for the full write-up, keeping the
     README section itself short.

5. **`config.yaml` default in `config.example.yaml`**: leave
   `browser_user_data_dir: ""` as the shipped default (Playwright's bundled
   browser, no extension, works out of the box with manual 2FA each login)
   — don't ship it pre-pointed at either strategy's path, since neither
   exists on a fresh clone. The comment does the steering; the value stays
   inert until the user deliberately sets it up.

## Should this be a user choice or a hard default?

**A documented choice with a strong default, not an interactive prompt, and
not a silent hard default either.**

- A **silent hard default** (e.g. `init` auto-creating and pointing at
  `~/.opal-downloader/login-profile` without explanation) is wrong because
  the dedicated-profile setup requires consent-gated manual steps (install
  an extension from a store, complete a 2FA/device-registration login) that
  cannot be automated or silently assumed — a user who runs `init` and then
  `login` without reading anything would hit a confusing "browser opened but
  TU-Fast isn't there yet" experience with the dedicated path pointed at
  by default, which is worse than today's "bundled browser, manual login"
  fallback.
- An **interactive prompt during `init`** (e.g. "which profile strategy do
  you want? [1/2]") is also the wrong shape here: this decision requires
  information the user doesn't have yet at `init` time (whether they
  already have TU-Fast working somewhere, whether they're willing to do a
  one-time extra setup pass) and `init`/`setup` are explicitly designed to
  be scriptable/non-interactive (README already documents both a manual
  hand-editing path and a GUI path specifically so automation isn't forced
  through a prompt). Forcing a synchronous decision at the exact moment
  someone is just trying to get `config.yaml` created adds friction rather
  than removing it.
- The right shape is what's described above: `init`/`setup`'s printed next
  steps **name the fork and link to the doc**, the doc **leads with a
  recommendation** (dedicated profile) while fully documenting the
  alternative, and the actual mechanism for choosing is what it already is
  today — setting (or not setting) `browser_user_data_dir` in `config.yaml`,
  either by hand or via the existing GUI Settings page. No new config field
  or interactive step is needed; this is purely a documentation and
  default-guidance change, consistent with the task's non-goal of not
  building new setup automation.

## Also found while researching this (worth a separate, small fix)

Both `README.md`'s "TU-Fast / Brave Setup" section and the
`browser_user_data_dir` comment block in `config.example.yaml` still
describe the profile-**copy** approach (copying into
`~/.opal-downloader/browser-profile`, "the copy is a one-time snapshot,"
etc.) that PR #20 removed in favor of direct-launch-against-the-real-profile.
This is stale documentation independent of which strategy this plan
recommends — it actively misdescribes current behavior today (a new user
reading the README's current TU-Fast section would expect their everyday
Brave to stay open during `login`, which is false as of PR #20; they'd only
discover the real "close Brave first" requirement when `isUserDataDirLocked`
rejects them). This should be fixed regardless of the Strategy 1 vs. 2
decision above, and is small enough to be its own short follow-up rather
than blocking on the broader plan.

## Transplanting TU-Fast login data into a fresh dedicated profile (2026-07-12 finding)

Task `ease-second-profile-tufast-setup` investigated a narrower question than
the copy-based-approach re-litigations above: **not** "can a whole copied
profile work" (already conclusively ruled out — see "Non-goals reaffirmed"
below), but "if a user already has TU-Fast logged in somewhere on this same
computer, can just TU-Fast's own stored login/2FA data be copied into a
*freshly and properly initialized* dedicated profile (empty dir, TU-Fast
installed fresh via the Web Store — so `Secure Preferences` stays
self-consistent for that profile) to skip redoing the OPAL/Shibboleth 2FA
setup?"

**Yes — this works, live-verified, and now ships as an optional GUI action**
(Settings → "Set up a dedicated TU-Fast browser profile" → "Copy TU-Fast
login data", `internal/scraper.TransplantTUFastLoginData`).

What was found, reading TU-Fast's own bundled extension source
(`modules/credentials.js`, `modules/otp.js`) from a real installed copy at
extension ID `aheogihliekaafikeepfjngfegbnimbk`:

- TU-Fast's "device registration" for 2FA is **not** a server-side token or
  certificate at all — it's a plain client-side TOTP (RFC 6238) implementation.
  TU-Fast stores the OPAL/Shibboleth username, password, and the TOTP shared
  secret entirely in `chrome.storage.local` (persisted by Chromium as a
  leveldb directory at `<profile>/Local Extension Settings/<extension-id>`)
  and computes 6-digit codes itself, offline, whenever it auto-fills the
  Shibboleth IdP login form.
- That stored data is AES-CBC encrypted, but the key is derived via
  `SHA256(JSON(chrome.system.cpu.getInfo() minus volatile fields) +
  JSON(chrome.runtime.getPlatformInfo()))` — both are pure OS/hardware facts
  (CPU model, architecture, etc.), **identical across every Chromium/Brave
  profile on the same physical machine**. The derivation never references the
  profile directory path, a Chromium-generated device ID, or anything else
  that would differ between two profiles on one PC. This is exactly why the
  encrypted blob is portable across profile directories on the same
  machine — it isn't incidental, it's how the encryption key is constructed.
- Live round-trip test (same machine, `~/.opal-downloader/login-profile`,
  which already had a real, working TU-Fast install from the original
  second-profile investigation): backed up
  `Default/Local Extension Settings/aheogihliekaafikeepfjngfegbnimbk`, deleted
  it (leaving `Preferences`/`Secure Preferences`/`Local State`/`Extensions`
  completely untouched), relaunched Brave against that profile — TU-Fast
  loaded fine, no HMAC/corruption warning, but showed a fresh/logged-out
  state (AutoLogin toggle off, stats reset to 0). Restored the backed-up
  folder, relaunched again — TU-Fast immediately showed AutoLogin on, prior
  usage stats, and course shortcuts again, with **no re-login, no 2FA, no
  reinstall**. This confirms extension-storage data is not part of the
  `Secure Preferences` HMAC chain at all (only copying `Preferences`/`Secure
  Preferences`/`Local State`/`Extensions` themselves breaks that check, per
  the existing PR #20/#41 finding above) — copying *just* TU-Fast's own data
  folder is a completely different, safe operation.

**Scope/caveat this finding does not cover:** the round-trip test above was
same-directory (delete + restore in place), not a literal second, brand-new
directory with TU-Fast installed via a live Web Store click — installing an
extension is a consent action this investigation deliberately did not
script (see "Non-goals reaffirmed" below). The AES-key analysis is what
extends the same-directory result to "this should also work copying into a
*different* directory on the same machine" — nothing in TU-Fast's key
derivation is directory-specific, so there's no mechanical reason the outcome
would differ. **This does not extend to a different physical machine** —
moving to different hardware changes `chrome.system.cpu.getInfo()`'s output,
which changes the derived key, which will fail to decrypt. The shipped
"Copy TU-Fast login data" GUI action is documented as same-machine-only for
this reason, and never touches the network, matching CLAUDE.md's
"credentials/session data never leave the machine unscrubbed" principle.

## Non-goals reaffirmed

This plan does not re-open whether the copy-based approach (PR #6) could be
revived, and does not attempt to automate the dedicated profile's one-time
TU-Fast install + OPAL login (both are consent/identity actions on the
user's own account that cannot and should not be scripted). The TU-Fast
login-*data* transplant above is a different thing: it still requires the
target profile to have TU-Fast genuinely, manually installed via the Web
Store first (a real "install" event so `Secure Preferences` stays
self-consistent) — it only skips redoing the *2FA/device-registration login*
after that, never the install itself.

The copy-based approach *was* re-litigated once more after this plan merged
(task `revisit-copy-based-browser-profile-approach`, 2026-07-09): the
maintainer asked whether the HMAC/`Secure Preferences` block could be worked
around (e.g. by recomputing the MAC after copying) rather than accepted as
final. Live re-testing on the real machine reconfirmed PR #20's result —
copying `Default/Secure Preferences` + `Extensions` + `Local State` into a
fresh directory and launching Brave against the copy still strips TU-Fast's
permissions the instant Chromium loads it. Research also turned up that the
HMAC seed and device-ID inputs are, in principle, forgeable (published
academic/red-team work has reverse-engineered both), but both are
undocumented Chromium/Brave binary internals that can silently change on any
browser update — not a foundation this project should build its login path
on. See `docs/HISTORY.md`'s "Browser profile handling" section for the full
write-up. Treat "conclusively ruled out" as reaffirmed, not just asserted.
