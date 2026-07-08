# Research: why did WebDAV/PROPFIND against OPAL fail?

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
WebDAV retry is not recommended as the next thing to build**, but for
reasons different from what the terse commit message implies:

- This research found no evidence that WebDAV was removed or is
  unsupported by the platform (Finding 1) — so if it's retried, it isn't
  fighting a shrinking/removed feature.
- The much more likely explanation for the original failure is **not** a
  protocol bug at all: it's that a student/participant OPAL account simply
  isn't granted WebDAV access to `coursefolders` unless Bildungsportal
  Sachsen has opted in to participant-level access (Finding 2), which nothing
  in this research can confirm or rule out without a live WebDAV credential
  test. If that's the cause, no amount of client-library fixing would help —
  it's a permissions wall, not a bug.
- Even if permissions turned out fine, WebDAV would still need: the correct
  institution-suffixed username (Finding 3), tolerance for a
  cookie/token-based auth layer some stateless clients may not handle well
  (Finding 4), and the possibility of User-Agent filtering (Finding 5) —
  each an independent thing that would need to be verified with a real
  WebDAV credential before it's worth writing code against.
- Meanwhile DOM scraping already works end-to-end today (see
  `internal/scraper/`, verified per `CLAUDE.md`) using the same SSO session
  the user already has, with no separate WebDAV credential/setup step and
  no dependency on an institution enabling a participant-access flag that
  may or may not be on. It also naturally handles anything WebDAV wouldn't
  expose the same way (show-all/paginated file lists, per-subfolder
  destinations — see `docs/OPERATIONS.md`).

**If someone wants to actually re-open this**, the productive next step
isn't more code — it's five minutes with a real WebDAV credential and
`curl`/`curl -v --digest` doing a manual `PROPFIND` against
`https://bildungsportal.sachsen.de/opal/webdav/coursefolders/` with
`Depth: 1`, to see the actual status code and response body. That single
data point would confirm or eliminate Findings 2–5 immediately, which nothing
short of a live test can do. Absent that, DOM scraping remains the only
approach this project has actually verified works.

## Evidence-confidence summary

| Finding | Confidence |
|---|---|
| WebDAV not removed/deprecated by OPAL/OLAT | Well-evidenced (current official docs, no removal notices found) — but absence-of-evidence, not a positive removal-never-happened statement |
| Role-gating (student accounts lack default coursefolders access) | Well-evidenced from official OpenOLAT admin/user docs; **not confirmed** as the actual historical cause (no live test against this project's account) |
| Institution-suffixed username requirement | Well-evidenced as a real requirement; whether it caused *this* failure is speculative |
| Fragile/legacy WebDAV auth stack (cookie/token layering) | Documented general characteristic of OLAT's WebDAV; link to this specific failure is speculative |
| User-Agent client blocking | Documented as an available admin feature; whether BPS uses it is unknown/speculative |
