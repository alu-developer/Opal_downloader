# Why students have no usable WebDAV on OPAL — and what to ask BPS

**Research, 2026-08-04.** Written to support a possible letter to the platform
operator asking whether WebDAV (or any read interface) could be opened to
students. No code changes.

This supersedes the interpretation in
[`webdav-propfind-research.md`](webdav-propfind-research.md) — see
[the correction](#correction-to-the-earlier-research) at the end.

---

## The short version

1. **Students do have WebDAV.** A student account can set a WebDAV password,
   authenticates fine, and gets a mounted drive. What they get is **empty**,
   because OPAL only exposes folders you *own* — not folders you have access to
   as a participant. This is documented behaviour, not a bug.
2. **The reason is a fork that stopped in 2011.** OpenOlat — the same codebase
   OPAL was forked from — added exactly this feature (course folders over
   WebDAV for participants and coaches) in **November 2014**, and turned it
   **on by default in January 2015**. OPAL forked from OLAT 7.1 in 2011 and
   never got it.
3. **So the ask is a feature request, not a config toggle** — and it is a
   feature request for something that already exists, working and shipped, in
   the upstream project, including the permission handling that would be the
   obvious objection.
4. **Nothing in OPAL's terms forbids what opal-downloader does.** There is no
   clause about automated access, scripts, or bots.
5. **Realistic answer to expect:** load/performance, and the fact that WebDAV
   needs a second password outside the SSO/2FA login. Both are answerable, and
   the second one points at the better ask — a token-based read API.

---

## 1. What the operator actually says

BPS Bildungsportal Sachsen GmbH publishes the OPAL user handbook. The WebDAV
page (last modified **2026-07-27**, so current) says, verbatim:

> **Wer kann WebDAV nutzen?**
> In der Lernplattform können nur Nutzer mit folgenden Rollen die Funktion
> verwenden:
> * Autoren
> * Lernressourcenmanager
> * Administratoren

and, for the folders you get after connecting:

> **coursefolders** — Ablageordner aller Kurse, die Sie **besitzen**. Dies
> betrifft in der Regel nur Benutzer mit Autorenrechten. **Alle anderen
> Benutzer finden hier ein leeres Verzeichnis vor.**
>
> **resourcefolders** — Alle Lernressourcen des Typs Ressourcenordner, die Sie
> besitzen. […] Alle anderen Benutzer finden hier ein leeres Verzeichnis vor.
>
> **groupfolders** — Gruppen des Typs Arbeitsgruppe, Lerngruppe und
> Rechtegruppe, in denen Sie eingetragen sind und auf deren Gruppenwerkzeug
> vom Typ Ordner Sie Zugriff haben.
>
> **home** — Ihren persönlichen Ordner (private).

Source: <https://help.bps-system.de/wiki/bin/view/LMS/Benutzerhandbuch%20OPAL/Meine%20Lernplattform/Meine%20Einstellungen/WebDAV/>

Read that carefully — the restriction is **ownership**, not enrolment. The two
places a student *can* get content are `groupfolders` (only if a lecturer set
up a group with a folder tool and put files there — rare) and their own `home`.
The place course material actually lives — the folder course elements
(*Kursbausteine "Ordner"*) — is not mapped into WebDAV at all, for anybody. Even
an author sees only the course's *Ablageordner*, i.e. the raw storage folder of
courses they own.

So "no WebDAV for students" is really **"no WebDAV mapping for course content
you only have access to, ever."**

### Two more data points from the operator's own release notes

- **OPAL 13.4.1 (2021-12-15):** "WebDAV: Schreibender Zugriff ab sofort nur noch
  für Autor\*innen, Lernressourcenverwalter\*innen und Sys-Admins möglich."
  → they tightened **write** access in 2021. The role list in the handbook
  probably dates from this change. Note it says *schreibend* — the restriction
  that was deliberately introduced is about writing, not reading.
- **OPAL 2025.12.1 (Dec 2025):** "WebDAV Dateizugriff: Fehlerbehebung für das
  Kopieren von Ordnern über WebDAV."
  → WebDAV is **alive and actively maintained**, one bugfix ago. It is not a
  legacy corner they have quietly abandoned. OPAL currently ships monthly
  (latest release page: **OPAL 2026.08.1**).

---

## 2. Why it is like this: the fork

| | |
|---|---|
| 1999 | OLAT developed at Uni Zürich |
| 2011 | BPS forks **OLAT 7.1** → "OLAT CE" for Saxony |
| 2014-11-11 | **OpenOlat** commit `c458fb7a`, *OO-1245: make the courses accessible via WebDAV to students* |
| 2015-01-14 | OpenOlat commit `d09fc733`, *no-jira: enable per default WebDAV for learners* |
| 2016 | OLAT CE renamed **OPAL** |
| 2016-03-04 | OpenOlat `b9d7cfb9`, *OO-1921: allow coach to see courses with WebDAV with the same options as for participants* |
| 2024-08-15 | OpenOlat `6ae28f0e`, *OO-7949: remove support of Basic authentication in WebDAV* |

The feature the user wants landed upstream **three years after the fork**. That
is the whole explanation. There is no evidence anywhere that BPS considered and
rejected student WebDAV — it simply is not in their branch.

Sources: git history of
`src/main/java/org/olat/core/commons/services/webdav/WebDAVModule.java` in
<https://github.com/OpenOLAT/OpenOLAT>; <https://de.wikipedia.org/wiki/OPAL_(Lernplattform)>

### What upstream actually does today

In current OpenOlat this is two admin switches, both **default `true`**:

```java
@Value("${webdav.learners.participatingCourses.enabled:true}")   // courses you're enrolled in
@Value("${webdav.learners.bookmarks.enabled:true}")              // courses in your favourites
```

Admin UI labels: *"Zugriff für Studenten / Betreuer Kurse"* and *"Zugriff für
Studenten / Betreuer Favoriten"*.

**The important part — the permission handling is not hand-waved.** For a
participant, OpenOlat builds the WebDAV tree through the *same*
`CourseTreeModelBuilder` + `AccessibleFilter` the web UI uses, then applies the
node's own `canDownload()` / `canUpload()`, falling back to a `ReadOnlyCallback`:

```java
// MergedCourseElementDataContainer.java — participant path
CourseTreeNode treeRoot = nodeAccessService.getCourseTreeModelBuilder(userCourseEnv)
        .withFilter(AccessibleFilter.create())
        .build().getRootNode();
...
boolean canDownload = bcNode.canDownload(nodeEval);
if (canDownload && rootFolder != null) {
    if (courseReadOnly)                              rootFolder.setLocalSecurityCallback(new ReadOnlyCallback());
    else if (bcNode.canUpload(userCourseEnv, nodeEval)) { /* inherit */ }
    else                                             rootFolder.setLocalSecurityCallback(new ReadOnlyCallback());
}
```

So a date-gated, group-gated or unpublished folder is invisible over WebDAV for
exactly the same reason it is invisible in the browser. **"The filesystem view
can't express our access rules" is not an available objection** — upstream
solved it by not reimplementing the rules at all.

Sources: `org/olat/course/CoursefolderWebDAVMergeSource.java`,
`org/olat/course/MergedCourseContainer.java`,
`org/olat/course/folder/MergedCourseElementDataContainer.java`,
<https://docs.openolat.org/de/manual_admin/administration/WebDAV/>

---

## 3. Reasons they might give, and what's true about each

**"Performance / load."** The most likely real answer, and it has precedent:
TH OWL switched ILIAS's WebDAV off entirely in 2016 — *"Aus Performanzgründen
wurde am Donnerstag, den 28. April 2016 die WebDAV-Schnittstelle der
Lernplattform eCampus (ILIAS) bis auf weiteres deaktiviert."* The concern is
real: OS file managers `PROPFIND` aggressively and recursively, and OPAL would
have to assemble a virtual tree across every course a student is enrolled in.
Counter: upstream ships it default-on for the entire OpenOlat installed base;
and a read-only, rate-limited variant is strictly lighter than the browser
traffic the same student generates by hand.
Source: <https://www.th-owl.de/en/skim/news/web-portals/article/detail/webdav-zugang-der-lernplattform-ecampus-ilias-deaktiviert/>

**"WebDAV means a second password outside SSO."** True, and the strongest
argument *against WebDAV specifically*. OPAL's WebDAV is HTTP Basic auth
(`WWW-Authenticate: Basic realm="OPAL WebDAV"`, verified live 2026-08-04)
against a password stored separately from the Shibboleth/2FA login. Upstream
took this seriously enough to **remove Basic auth support in August 2024**
(OO-7949). Rolling that out to ~100 000 accounts is a genuine security
question, not an excuse.
→ This is why the letter should not ask only for WebDAV. Ask for *a read
interface*, and name WebDAV as one acceptable shape.

**"Copyright / licensed material."** §5 of the terms already assigns
responsibility to the user; and every file is already downloadable one click at
a time, plus "selected files as ZIP" per folder. A read interface changes the
ergonomics, not the legal position.

**"Nobody asked."** Plausibly true — which is the point of writing.

---

## 4. Did anyone find a way around it?

Searched German and English, GitHub, forums, university IT pages. Findings:

- **No public workaround exists for student WebDAV.** No forum thread, no
  script, no "trick". The empty directory is documented, so there is nothing to
  bypass — the content is not mapped into that namespace at all.
- **"Just get author rights" does not work.** Authors see the *Ablageordner of
  courses they own*. It gives you nothing for a course you attend. (At TU
  Dresden author rights go to staff automatically, and students would have to
  request them by mail — for no benefit here.)
- **The REST API exists but is closed.** BPS documents an OPAL REST API whose
  docs are password-protected behind a mail to `support@bps-system.de`, and
  `/opal/restapi/` answers a bare **403 with no auth challenge** (verified
  2026-08-04, matching this repo's earlier probes). It is positioned as a
  system-integration interface for institutions.
- **Other Saxon tooling exists and is tolerated.**
  [`spyfly/videocampus-sachsen-downloader`](https://github.com/spyfly/videocampus-sachsen-downloader)
  downloads videos from videocampus.sachsen.de including those embedded in
  OPAL. Public on GitHub for years, no takedown.
- **The comparison worth naming:** Moodle has an official Web Services API and
  a mobile API; the student tool [`Moodle-DL`](https://github.com/C0D3D3V/Moodle-DL)
  (629★) does exactly what opal-downloader does, using that official API with a
  token instead of scraping HTML. That is the "it's normal, and it's better for
  the operator" precedent.

### One thing that *does* exist for students and is machine-readable

OPAL generates a **personal RSS news feed with a token in the URL** ("Neuigkeiten
→ RSS-News-Feed → RSS-Link generieren") covering subscribed folders, forums and
messages. It carries change notifications, not files — but it proves OPAL
already hands students a token-authenticated machine endpoint. Useful both as
an argument (*this is not a new category of risk*) and possibly for this project
as a cheap change-detector.
Source: <https://help.bps-system.de/wiki/bin/view/LMS/Benutzerhandbuch%20OPAL/Meine%20Lernplattform/Abonnements%20und%20Benachrichtigungen/RSS%20Feed/>

---

## 5. Is scraping against the rules?

Read the full OPAL *Nutzungsbedingungen*
(`https://bildungsportal.sachsen.de/opal/raw/<date>/doc/legal/disclaimer_de.html`)
and `robots.txt` on 2026-08-04. Not legal advice, but factually:

- **No clause about automated access, scripts, crawlers, rate limits or bots.**
  §6 forbids illegal content, commercial advertising and storing encrypted data;
  §5 is ordinary copyright; §3 is "don't share your credentials".
- `robots.txt` disallows only `/preview/` and `/opal/auth/search` for everyone,
  plus three commercial crawlers. Course pages are not excluded.
- §7 does let the operator restrict use at any time — so this is *not
  forbidden*, rather than *permitted forever*.

Practical read: a personal tool that logs in as you and downloads material you
are entitled to is not covered by any prohibition. Hammering the server would
be, under §4/§7.

### And the copyright objection, examined

"Bulk download is a copyright problem" is the objection most likely to be
raised informally by lecturers. As a *copyright* argument it does not hold:

- **German copyright has no effort threshold.** It asks what is copied and what
  you do with it, never how tedious the copying was. A student's copy of course
  material they are enrolled in, for their own study, is ordinary private/own
  use (§ 53 UrhG). Automating the clicks changes nothing about that.
- **OPAL ships a bulk-download button itself.** Every folder course element
  offers "select all → download selected files as ZIP" (BPS handbook, Kursordner
  / TU Dresden handbook 3.3). BPS has already decided bulk download per folder
  is fine; the tool only removes the per-folder and per-course repetition.

What *is* legitimate, and should be named honestly rather than dressed up as a
technical reason:

- **Much material sits in OPAL under § 60a UrhG** (up to 15% of a published
  work for teaching, *limited to the participants of the course*). That limit is
  the condition of the permission. A private copy is covered; passing it on is
  not.
- **Friction is a de-facto access control.** A complete, tidy local mirror is
  one drag-and-drop away from being re-shared. That is a behavioural argument,
  not a legal one about any individual copy — but it is the real concern.
- **Course folders can contain other people's personal data** (submissions,
  seminar papers with names). Mirroring everything sweeps that up too.

None of this makes a personal downloader unlawful. It does mean the operator's
caution is not stupid — it is simply about aggregate behaviour, not about one
enrolled student's copy.

---

## 6. Who to write to

BPS builds and runs OPAL; the universities are its customers. Feature requests
realistically travel **institution → BPS**, not student → BPS.

- `support@bps-system.de` — BPS support, the address in OPAL's own footer and
  the one their docs name for API access. Right place for "does this exist, and
  why not".
- Your university's e-learning team (TU Dresden: `elearning@tu-dresden.de`) —
  the ones who can actually put it on BPS's list. Worth copying in.
- **OPAL User Day** — BPS's annual user meeting, explicitly framed around
  feedback and future development. If they answer "bring it to the User Day",
  that is a real answer, not a brush-off.

**Recommendation: write to both at once**, short, and ask a question they can
answer in three lines rather than making a demand.

---

## 7. Draft letter

Send-ready, not sent. Adjust the personal bits.

Sharpened 2026-08-04: **one request instead of three questions.** The earlier
draft asked separately about WebDAV and about "some other token-based access",
which set up a contradiction that does not exist — WebDAV is just HTTP, so the
credential in front of it is a free choice. It also now names the two-tier model
(strong login for humans, scoped token for machines) as a concrete proposal,
because "you would be undermining 2FA" is the most likely objection and this
answers it before it is raised.

> **Betreff:** Lesender Zugriff auf die eigenen Kursmaterialien für
> Studierende — WebDAV mit widerrufbarem Token?
>
> Sehr geehrte Damen und Herren,
>
> ich bin Student an der TU Dresden und nutze OPAL täglich. Um meine
> Kursmaterialien lokal zu haben, lade ich sie regelmäßig herunter — bei
> mehreren Kursen pro Semester ist das viel Handarbeit, und ich merke nur
> zufällig, wenn eine Datei ersetzt wurde.
>
> Laut Ihrem Handbuch steht WebDAV nur Autoren, Lernressourcenmanagern und
> Administratoren zur Verfügung; als Teilnehmer sehe ich unter `coursefolders`
> erwartungsgemäß ein leeres Verzeichnis. Das ist mir technisch klar — OPAL
> bildet dort Ordner ab, die man *besitzt*, nicht solche, auf die man als
> Teilnehmer Zugriff hat.
>
> **Meine Bitte in einem Satz:** lesender WebDAV-Zugriff auf die
> Ordner-Kursbausteine der Kurse, in denen ich eingeschrieben bin — mit einem
> vom System erzeugten, widerrufbaren Token statt eines selbstgewählten
> Passworts.
>
> Warum ich glaube, dass das kleiner ist, als es klingt:
>
> - In OpenOlat, das auf derselben Codebasis beruht, gibt es genau diese
>   Funktion seit November 2014 und sie ist seit Januar 2015 standardmäßig
>   aktiv ("Zugriff für Studenten / Betreuer Kurse"). Die Sichtbarkeitsregeln
>   werden dort nicht neu implementiert, sondern über denselben Kursbaum-Filter
>   wie in der Weboberfläche ausgewertet — nicht freigegebene oder
>   zeitgesteuerte Bausteine bleiben also auch über WebDAV unsichtbar.
> - Dieselbe Codebasis hat im August 2024 die Unterstützung für Basic
>   Authentication im WebDAV entfernt; der Anmeldeteil verarbeitet heute
>   `Digest` und `Bearer`. Ein Token-Verfahren ist dort also bereits vorgesehen.
> - OPAL stellt Studierenden bereits einen tokenauthentifizierten
>   maschinenlesbaren Zugang bereit — den persönlichen RSS-Feed. Ein lesender
>   Dateizugriff wäre insofern keine neue Kategorie.
>
> Falls die Sorge ist, dass ein solcher Zugang die Zwei-Faktor-Anmeldung
> aushebelt: das ließe sich zweistufig lösen, wie es etwa Nextcloud mit
> App-Passwörtern macht — die Anmeldung als Mensch bleibt unverändert stark,
> und *nach* dieser Anmeldung lässt sich ein Token erzeugen, das ausdrücklich
> weniger darf: nur lesen, nur Dateien, kein Ändern von Passwort oder
> Zwei-Faktor-Einstellungen, jederzeit einzeln widerrufbar, gern mit
> Ablaufdatum. Ein solches Token ist damit schwächer als mein normaler Zugang,
> nicht stärker.
>
> Mir ist bewusst, dass Last ein Thema ist — andere Hochschulen haben WebDAV
> deshalb abgeschaltet. Ein rein lesender, gern gedrosselter oder auf Antrag
> freizuschaltender Zugang wäre für meinen Zweck völlig ausreichend.
>
> Und falls das alles nicht in Frage kommt, wären mir schon zwei kurze
> Auskünfte sehr viel wert:
>
> 1. Gibt es einen inhaltlichen Grund gegen lesenden Teilnehmerzugriff, oder
>    ist er in OPAL schlicht nie umgesetzt worden?
> 2. Über welchen Weg bringt man so einen Wunsch bei Ihnen ein — über die
>    Hochschule, den OPAL User Day, oder direkt?
>
> Ich frage aus echtem Interesse und nicht, um Druck zu machen: auch
> "technisch möglich, aber nicht geplant" ist eine hilfreiche Antwort.
>
> Vielen Dank für Ihre Zeit und viele Grüße
> …

**Deliberately left out** of the letter, though both are true: that OPAL already
ships a per-folder "download selected files as ZIP" button (so bulk download is
already sanctioned), and that nothing in the Nutzungsbedingungen forbids
automated access. Both read as *arguing a case* rather than asking a question,
and neither is needed unless BPS raises the copyright objection first — at which
point they are the answer to it.

---

## Correction to the earlier research

[`webdav-propfind-research.md`](webdav-propfind-research.md) Finding 6
(2026-07-09) observed that an authenticated student account got an identical
blank `200` for every path and method, and concluded the best-fitting
explanation was *"the backend WebDAV servlet has been decommissioned or
disconnected behind a still-live auth gate"*.

**That conclusion is wrong.** Two pieces of evidence found here:

1. BPS documents the empty result as expected behaviour — *"Alle anderen
   Benutzer finden hier ein leeres Verzeichnis vor."*
2. BPS shipped a WebDAV **bugfix in OPAL 2025.12.1**, five months after that
   test. A decommissioned servlet does not get bugfixes.

So the correct reading is the one that document had listed as Finding 2 and then
demoted: **role/ownership gating**. Its own counter-argument — "a permissions
wall would 401/403, not return a blank 200 for a nonexistent path" — assumed
OPAL rejects unauthorised paths. It doesn't; it mounts an empty namespace, in
which every path is equally absent. The identical blank `200` for a real folder
and for `this-path-does-not-exist-xyz123` is exactly what an empty mount looks
like.

Everything else in that document stands: credential format is correct, User-Agent
filtering is ruled out, and DOM scraping remains the right approach — but for the
reason "OPAL never mapped participant course content into WebDAV", not "OPAL's
WebDAV is broken". Which matters, because it is the difference between reporting
a bug and asking for a feature.

---

## Sources

- BPS OPAL handbook, WebDAV — <https://help.bps-system.de/wiki/bin/view/LMS/Benutzerhandbuch%20OPAL/Meine%20Lernplattform/Meine%20Einstellungen/WebDAV/>
- BPS OPAL handbook, RSS feed — <https://help.bps-system.de/wiki/bin/view/LMS/Benutzerhandbuch%20OPAL/Meine%20Lernplattform/Abonnements%20und%20Benachrichtigungen/RSS%20Feed/>
- BPS OPAL REST API doc (password-protected) — <https://help.bps-system.de/wiki/bin/view/LMS/Benutzerhandbuch%20OPAL/Administration/Technische%20Dokumentationen/Dokumentation%20REST%20API/>
- OPAL release notes 13.4.1 (2021-12-15) and 2025.12.1 — <https://help.bps-system.de/wiki/bin/view/Releasenotes/Releases%20OPAL/>
- OpenOlat WebDAV administration — <https://docs.openolat.org/de/manual_admin/administration/WebDAV/>
- OpenOlat user manual, Using WebDAV — <https://docs.openolat.org/manual_user/basic_concepts/Using_WebDAV/>
- OpenOlat source — <https://github.com/OpenOLAT/OpenOLAT>
- TU Dresden OPAL handbook 5.4, WebDAV as network drive — <http://elearning.tu-dresden.de/opalhandbuch/5_weitere_funktionen/54_opal_als_netzlaufwerk/index_ger.html>
- OPAL (Lernplattform), Wikipedia — <https://de.wikipedia.org/wiki/OPAL_(Lernplattform)>
- TH OWL, ILIAS WebDAV deactivated for performance — <https://www.th-owl.de/en/skim/news/web-portals/article/detail/webdav-zugang-der-lernplattform-ecampus-ilias-deaktiviert/>
- Moodle-DL — <https://github.com/C0D3D3V/Moodle-DL>
- videocampus-sachsen-downloader — <https://github.com/spyfly/videocampus-sachsen-downloader>
- OPAL User Day — <https://bildungsportal.sachsen.de/portal/veranstaltungskalender/opal-user-day-2025/>
