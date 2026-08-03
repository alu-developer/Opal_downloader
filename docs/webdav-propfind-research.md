# Research: why did WebDAV/PROPFIND against OPAL fail?

> **Superseded in part, 2026-08-04.** Finding 6's conclusion — "the backend
> WebDAV servlet has been decommissioned behind a live auth gate" — is wrong.
> BPS documents the empty result as expected for non-owners, and shipped a
> WebDAV bugfix in OPAL 2025.12.1. The cause is ownership/role gating (the
> demoted Finding 2). See
> [`opal-webdav-student-access.md`](opal-webdav-student-access.md).

**Status:** research only, no code changes. Written in response to a queued
task investigating commit `0cf0e07` ("Removed WebDAV, because it didn't
work. Propfind requests didn't work. Change to scraping with Playwright").

## What the removed code did

The deleted implementation (`git show 0cf0e07^:opal_downloader/webdav.py`)
was a thin wrapper around the Python `webdav3.client` library:

```python
Client({
    "webdav_hostname": url,       # https://bildungsportal.sachsen.de/opal/webdav
    "webdav_login": username,
    "webdav_password": password,
    "webdav_timeout": 120,
})
```

It called `.check("/")` (an authenticated `PROPFIND`/`HEAD` probe) and
`.list(path, get_info=True)` (a `PROPFIND` with `Depth: 1`) against three
roots: `coursefolders`, `groupfolders`, `home`. No response body, status
code, or exception text survives anywhere in the repo or commit
history — the commit message ("Propfind requests didn't work") is the only
record. Nothing in this research should be read as confirming *which* of
the causes below actually fired; it narrows the field based on how OPAL's
WebDAV is documented and implemented, not on a captured failure.

## Finding 1: WebDAV is still live and documented today — not dropped

Bildungsportal Sachsen / OPAL (built on OLAT/OpenOLAT) actively documents
WebDAV access as a supported feature, current as of this research (mid-2026):

- BPS user handbook, WebDAV tab under "Mein OPAL → Einstellungen":
  https://help.bps-system.de/wiki/bin/view/LMS/Benutzerhandbuch%20OPAL/Meine%20Lernplattform/Meine%20Einstellungen/WebDAV/
- TU Dresden E-Learning handbook, "OPAL als Netzlaufwerk einrichten (WebDAV)":
  http://elearning.tu-dresden.de/opalhandbuch/5_weitere_funktionen/54_opal_als_netzlaufwerk/index_ger.html
- OpenOLAT admin docs, WebDAV administration:
  https://docs.openolat.org/manual_admin/administration/WebDAV/
- OpenOLAT user docs, "Using WebDAV":
  https://docs.openolat.org/manual_user/basic_concepts/Using_WebDAV/

No OLAT/OpenOLAT release notes, BPS announcement, or forum/mailing-list
thread found in this research states that WebDAV support was removed,
disabled, or deprecated at any point (searched release-note pages, the
OpenOLAT release archive, and general German/English forum queries for
"OPAL webdav abgeschaltet/eingestellt/nicht mehr verfügbar"). **The "server
stopped serving WebDAV" hypothesis is not supported by any evidence found.**
This is a negative result, not a strong confirmation — it's possible a
relevant announcement exists that a web search doesn't surface — but it
weighs against "protocol dropped entirely" as the explanation.

## Finding 2: WebDAV access at OPAL is role-gated by default — the most likely cause

This is the strongest concrete lead found. Per OpenOLAT's own admin and
user documentation:

- Course-folder and resource-folder WebDAV access is normally granted in
  full only to **Authors, Learning Resource Managers, and Administrators**
  (BPS wiki, "WebDAV" page).
- For regular **students/participants**, WebDAV access to `coursefolders`
  is *not* on by default — the OpenOLAT admin panel has separate toggle
  settings ("Access for participants/coaches", "Access to favorite
  courses") that an institution must explicitly enable. Without that,
  "non-author users cannot access course folders through WebDAV, even if
  they're enrolled in courses" (docs.openolat.org, WebDAV administration
  page).
- Participants who *do* get some access are documented as read-only on
  resource folders and are otherwise steered toward `groupfolders` (where
  enrolled) and their personal `home` folder, not general course content.

opal-downloader's own use case is a regular student/participant account,
not an author account, and the removed code specifically walked
`coursefolders` (alongside `groupfolders`/`home`) — exactly the tree that
is author-gated by default. If Bildungsportal Sachsen has not opted to
enable participant-level course-folder access (unknown — not publicly
documented per-institution), a normal ZIH/ Shibboleth student account's
WebDAV credentials would legitimately get `401`/`403`/empty-listing
responses on `PROPFIND` against `coursefolders`, independent of any client
bug. This alone plausibly explains "requests didn't work" without needing
a protocol-level bug at all.

Sources:
- https://docs.openolat.org/manual_admin/administration/WebDAV/
- https://docs.openolat.org/manual_user/basic_concepts/Using_WebDAV/
- https://help.bps-system.de/wiki/bin/view/LMS/Benutzerhandbuch%20OPAL/Meine%20Lernplattform/Meine%20Einstellungen/WebDAV/

## Finding 3: separate WebDAV credential, with an institution-suffixed username

OPAL WebDAV explicitly uses **its own username/password pair**, distinct
from the ZIH/Shibboleth SSO login used for the current Playwright flow, and
the username must include an institution suffix appended to the OPAL
username, e.g. `<opal-username>@tu-dresden.de` (TU Dresden handbook). A
config mismatch here (missing suffix, wrong suffix for the institution,
stale/never-actually-set WebDAV password separate from the SSO password)
would also manifest as blanket `401 Unauthorized` on every `PROPFIND` —
consistent with "didn't work" but unrelated to the DOM-scraping
architecture question. This is plausible but speculative for this
specific historical run: there's no surviving evidence of what username
format the removed `webdav.py` was actually configured with.

## Finding 4: OLAT/OpenOLAT's WebDAV stack has a documented history of being fragile

A 2012 frentix project document ("Fixing the broken WebDAV implementation")
describes OLAT's original WebDAV servlet as implementing only Basic
Authentication while Windows clients required Digest Authentication,
prompting a rewrite onto the Milton WebDAV library
(https://www.openolat.com/fileadmin/documents/openolat/projects/20120718-WebDAV-Refactoring.pdf
— PDF text extraction failed in this research, so treat the title/abstract
as the only confirmed detail; contents beyond that are not verified here).
Current admin docs describe an auth-persistence mechanism layered on top of
Basic/Digest: session-cookie reuse across requests, with a fallback
`X-OLAT-TOKEN` header when cookies aren't available
(docs.openolat.org/manual_admin/administration/WebDAV/, "REST API" doc
which documents the same token/cookie pattern used platform-wide). A
stateless client that sends only an `Authorization: Basic` header per
request (as `webdav3.client` does) rather than first establishing a
session/cookie could hit edge cases in this custom auth layer that GUI
clients (WinSCP, native OS WebDAV, Cyberduck) don't, since those are the
clients OPAL's own documentation targets and presumably tests against.
This is circumstantial — no direct report of `webdav3.client` failing
against OLAT/OPAL specifically was found — but it is a documented general
soft spot in this LMS's WebDAV stack, not specific to OPAL.

## Finding 5: administrator-configurable client blocking exists

OpenOLAT's WebDAV admin panel includes a **"WebDAV Client exclusion"**
feature: a toggle plus a comma-separated User-Agent blocklist, intended to
block specific WebDAV clients platform-wide
(docs.openolat.org/manual_admin/administration/WebDAV/). Whether
Bildungsportal Sachsen has this enabled, and with what blocklist, is not
publicly documented and wasn't tested. `webdav3.client` sends a
recognizable non-browser User-Agent by default (based on the underlying
`requests`/`lxml` stack), which is exactly the kind of client such a
blocklist is designed to catch (as opposed to WinSCP/Explorer/Finder). This
is speculative — no evidence BPS actually has this configured — but it's a
real, documented mechanism that would produce a "PROPFIND doesn't work for
my script but works in WinSCP" symptom.

## Finding 6: live-tested, 2026-07-09 — auth succeeds, then every request/path/method returns a blank 200

This is a live test, not inference from documentation like Findings 1-5. A
real WebDAV credential was obtained from Mein OPAL → Profil und
Einstellungen → WebDAV-Zugang (confirms Finding 3's username format exactly:
`<opal-username>@tu-dresden.de`) and used with `curl` against
`https://bildungsportal.sachsen.de/opal/webdav/`:

- **Auth is genuinely checked and works.** No `Authorization` header → `401`
  with `WWW-Authenticate: Basic realm="OPAL WebDAV"`. Correct
  username/password → `200`. Deliberately wrong password → `401` again. This
  rules out Finding 3 (credential format) as a live blocker — the documented
  format is correct and a freshly-set WebDAV password authenticates fine.
- **But every authenticated request returns `HTTP/1.1 200` with
  `Content-Length: 0` — completely empty, regardless of:**
  - HTTP method: `GET`, `OPTIONS`, and `PROPFIND` (with `Depth: 1`, both with
    and without an explicit `<D:propfind><D:allprop/></D:propfind>` body) all
    behave identically.
  - Path: `coursefolders/`, `groupfolders/`, `home/`, the bare
    `/opal/webdav/` root, and — critically — a **nonexistent path that
    cannot possibly exist** (`/opal/webdav/this-path-does-not-exist-xyz123/`)
    all return the exact same blank `200`. A real WebDAV backend would 404 a
    garbage path; getting an identical response for a real folder and a
    made-up one means nothing behind the auth check is actually doing
    path/method dispatch.
  - User-Agent: tried default `curl/8.16.0`, a spoofed
    `Microsoft-WebDAV-MiniRedir/10.0.19041` (Windows' native WebDAV client
    UA), and a spoofed desktop-browser UA — no difference. This weighs
    against Finding 5 (User-Agent-based client blocking) as the explanation:
    a UA blocklist would be expected to produce a different status for a
    blocked vs. allowed UA (e.g. `403` for curl, real content for the
    Windows client string), not the same blank `200` for every UA tried.
  - `OPTIONS` returned no `Allow` or `DAV` header at all, which a working
    WebDAV endpoint is expected to advertise (e.g. `DAV: 1,2` and
    `Allow: OPTIONS, GET, PROPFIND, ...`).

**Interpretation:** this looks like Apache is still terminating Basic Auth
for `/opal/webdav/*` and validating real OPAL credentials against it (so the
endpoint is "live" in the narrow sense Finding 1 describes — it hasn't been
un-published from Apache's config), but nothing behind that auth check
implements WebDAV/HTTP method or path semantics anymore — every verb and
every path, real or fake, produces the identical empty `200`. That's
consistent with the backend WebDAV servlet having been decommissioned or
disconnected while the front-door auth gate was left in place, rather than
with a permissions wall (Finding 2 would produce `401`/`403` or a real-but-empty
multistatus body, not an identical response for a real folder and a garbage
path) or client filtering (Finding 5 would differ by User-Agent). This is the
single most concrete data point gathered on this question to date — it
directly rules out credential format and User-Agent filtering as the
*current* cause, and points at a broken/disconnected backend rather than a
permissions wall, though it doesn't fully prove that theory (an
institution-side proxy quirk that swallows all WebDAV responses identically
is a less likely but not impossible alternative reading of the same data).

The WebDAV password used for this test was supplied directly by the
maintainer for this one-off check and was not written to any file in this
repository.

## What wasn't found

- No captured HTTP status code, response body, or exception traceback from
  the original failure — everything above is inferred from how OPAL/OLAT's
  WebDAV is documented to behave, not from a diagnosed failure.
- No public bug reports, forum threads, or mailing-list posts from other
  OPAL/Bildungsportal Sachsen users hitting `PROPFIND` failures with
  third-party WebDAV clients or scripts were found. Searches for German and
  English phrasings ("OPAL webdav 401", "propfind fehler", "webdav
  funktioniert nicht") returned only setup documentation, not incident
  reports.
- The OpenOLAT/frentix JIRA tracker (`track.frentix.com`) could not be
  queried for WebDAV-tagged issues in this research — the request required
  interactive access this environment doesn't have. A maintainer with a
  tracker account could check the "Files, Folder, WebDAV" component for
  relevant open/closed bugs; this is a legitimate follow-up gap, not a
  finding.

## Recommendation

**DOM scraping via Playwright is correctly the pragmatic path forward; a
WebDAV retry is not recommended as the next thing to build.** This is now
backed by a live test (Finding 6, 2026-07-09), not just inference:

- Live-testing ruled out two of the five hypothesized causes outright:
  credential format (Finding 3) is correct and authenticates successfully,
  and User-Agent filtering (Finding 5) shows no difference across three
  tried UAs including a real Windows WebDAV client string.
- The live evidence points at something closer to **a decommissioned/
  disconnected backend behind a still-active auth gate** than at a
  permissions wall (Finding 2): a pure permissions problem would be expected
  to produce `401`/`403` or an empty-but-real multistatus response, not an
  identical blank `200` for a real course folder *and* a path that cannot
  possibly exist. That said, this is this project's account only — it
  doesn't rule out Finding 2 as *also* true, and it's not a certainty, just
  the better-fitting explanation for the specific response pattern observed.
- Practically, this closes the loop opened by commit `0cf0e07`: the original
  "Propfind requests didn't work" almost certainly wasn't a client-library
  bug (Finding 4 concerns are moot if nothing behind the auth gate responds
  to any method at all), and it's very unlikely a config fix on
  opal-downloader's side would change the outcome today.
- Meanwhile DOM scraping already works end-to-end today (see
  `internal/scraper/`, verified per `CLAUDE.md`) using the same SSO session
  the user already has, with no separate WebDAV credential/setup step. It
  also naturally handles anything WebDAV wouldn't expose the same way
  (show-all/paginated file lists, per-subfolder destinations — see
  `docs/OPERATIONS.md`).

**If someone wants to actually re-open this**, the productive next step is no
longer "get a live credential and test it" (done — see Finding 6). It would
be reporting the dead-backend-behind-live-auth-gate behavior to Bildungsportal
Sachsen/OPAL support, since from the outside this reads as a platform bug
(an advertised, documented feature that authenticates but serves nothing) —
not something fixable from opal-downloader's side.

## Evidence-confidence summary

| Finding | Confidence |
|---|---|
| WebDAV not removed/deprecated by OPAL/OLAT | Well-evidenced (current official docs, no removal notices found) — but absence-of-evidence, not a positive removal-never-happened statement |
| Role-gating (student accounts lack default coursefolders access) | Live test (Finding 6) doesn't fit this cleanly — a pure permissions wall would more likely 401/403 than return an identical blank 200 for a real vs. nonexistent path. Not ruled out, but no longer the best-fitting explanation |
| Institution-suffixed username requirement | **Confirmed correct and sufficient to authenticate** — live-tested 2026-07-09 (Finding 6) |
| Fragile/legacy WebDAV auth stack (cookie/token layering) | Documented general characteristic of OLAT's WebDAV; live test found no auth-layer failure (auth succeeds cleanly) — so this specifically isn't the blocker, though it says nothing about the empty-response behavior found instead |
| User-Agent client blocking | **Ruled out for this account** — live-tested 2026-07-09 with 3 different UAs (Finding 6), no difference in response |
| Backend WebDAV servlet decommissioned/disconnected behind a still-live auth gate | New, live-evidenced (Finding 6, 2026-07-09) — best-fitting explanation for the observed identical-blank-200-for-any-path/method pattern, though not independently confirmed against OPAL/BPS support |
