# Sync speed: das Ursachenmodell

**Diese Datei treibt die Arbeit. `docs/sync-speed-campaign.md` ist ab jetzt
das Archiv** — Messwerte und Friedhof, zum Nachschlagen, nicht zum Ableiten
des nächsten Schritts.

Der Unterschied ist der Punkt: eine Liste von *Ansätzen* geht aus, und wenn
sie ausgeht, sieht das aus wie "es geht nicht". Eine Liste offener *Fragen*
geht nicht aus, weil jedes Experiment neue erzeugt. Angeordnet ist sie
danach, wie sehr die Antwort alles ändern würde — nicht danach, wie leicht
sie zu beantworten ist.

Eingeführt am 2026-07-31, nachdem der Maintainer die Arbeitsweise als das
eigentliche Problem benannt hat: *"Idee haben, Idee probieren, klappt nicht,
Ansatz verwerfen"* — ohne den Schritt dazwischen, in dem man versteht, warum.

## Die drei Regeln

1. **Jedes Experiment schreibt vorher auf: erwartete Zahl, vermuteter
   Mechanismus, und ab welcher Zahl es gescheitert ist.** Danach ist ein
   schlechtes Ergebnis kein Urteil, sondern eine Lücke zwischen Vorhersage
   und Wirklichkeit — und die muss erklärt werden.
2. **Ein Ansatz darf erst geschlossen werden, wenn die Erklärung so scharf
   ist, dass sie das Scheitern vorher vorhergesagt hätte.** Ist da noch ein
   Loch, bleibt er offen. ("HTTP verliert Kurse" ist eine Beschreibung;
   "OpenOLAT-Bausteintyp X rendert client-seitig und ist in der Antwort an
   Y erkennbar" wäre eine Ursache.)
3. **Jedes Experiment muss mindestens eine neue offene Frage hinterlassen.**
   Tut es das nicht, ist genau das die Meldung.

## Wenn die Ideen ausgehen

Kein Grund aufzuhören — ein lösbarer Zustand. Feste Züge, der Reihe nach:

- **Die Gegenseite lesen.** OPAL läuft auf OpenOLAT, das ist Open Source.
  Bisher wurden nur die Handbücher gelesen und der Live-Server abgetastet.
- **Nachsehen, wie andere dasselbe gelöst haben** (andere OPAL-/OpenOLAT-/
  LMS-Downloader).
- **Die Obergrenze ausrechnen, bevor gebaut wird.** Hat hier schon einmal
  einen ganzen Build gespart (HTTP-Ceiling ~93s).
- **Die Frage wechseln.** Das Ziel ist *"fühlt sich wie ein Klick an"*, nicht
  "Discovery ist schnell". Vorabholen, Hintergrundlauf, Teilergebnisse sind
  eine eigene Lösungsklasse und standen nie auf der Liste.
- **Fragen, welche Randbedingung verhandelbar ist** — als Optionen an den
  Maintainer, nicht als offene Frage.
- **Messen statt argumentieren.**

---

## Was wir wissen (nur Zahlen, alles aus echten Läufen)

| | |
|---|---|
| Ziel | **30s** für einen No-op-Sync |
| Heutiger verlustfreier Boden | **~207s** (reiner Browser-Crawl, 282 Sektionen) |
| Settle-Wait | 338ms/Sektion, **64%** der Zeit in Sektionen |
| Stability-Poll | 172ms/Sektion, **32%** |
| Echte Arbeit (Extraktion, Navigation) | 14ms/Sektion, **2%** |
| Rate-Limiter | 0s gehalten — bremst nichts |

**96% der Zeit wartet das Werkzeug auf eigene Timer, und jedes Warten trägt.**
Settle-Wait weglassen: 51% *langsamer*. Verdikt zusätzlich behaupten: immer
noch 40% langsamer. Die Zeit braucht die Seite wirklich; der MutationObserver
ist nur die billigste Art, sie abzusitzen.

Weitere harte Befunde: kein positives Render-fertig-Signal im DOM gefunden.
Kein AJAX beim initialen Sektionsaufbau (Netzwerk-Trace, 2 Kurse, 0
unerklärt). `ctx.Route` kostet ~30% eines Laufs allein durch seine Existenz.
HTTP-Fetch 315ms/Sektion, 91 KiB, ~93s seriell projiziert — parallel
korrumpiert (OPAL serialisiert die Session serverseitig). Hybrid `mode=1`:
254s gegen 207s, also langsamer, weil HTTP erst laufen kann, nachdem der
Browser die URLs geliefert hat. Section-Hash-Cache: 3,9% Trefferquote, 13%
langsamer. Inhalt wächst beim Rendern nur (278 Sektionen: nie leer, nie
größer als am Ende).

## Was wir nicht wissen (sortiert nach Hebelwirkung)

### 1. Was rendert OPAL da eigentlich? — jetzt nachgelesen, siehe unten
~~OpenOLAT ist Open Source. Diese Kampagne hat zehn Tage lang am lebenden
Server geraten, was er tut.~~ Beantwortet 2026-07-31, siehe "Nächstes
Experiment" unten für die Belege. Kurzfassung: es gibt **keinen** Marker, weil
es **nichts client-seitig Gerendertes gibt, das fertig werden müsste** —
Baum und Dateitabelle sind reines Server-HTML. Das öffnet Frage 7.

### 2. Warum war HTTP auf 2 von 6 Kursen leer?
"Manche Bausteine rendern server-, manche client-seitig" ist die
Beschreibung, nicht die Ursache. Welcher Bausteintyp, und woran ist er in
der Antwort erkennbar? Vermutlich beantwortet durch (1). Dieser Ansatz war
der schnellste, den es je gab (22s) — er wurde an Tag 1 verworfen, ohne dass
je jemand die Ursache diagnostiziert hat.

### 3. ~~Warum kostet `ctx.Route` 30%?~~ Beantwortet 2026-08-01, siehe Bericht unten
Playwright installiert bei jeder Route, egal welches Pattern übergeben wird,
CDP-seitig `Fetch.enable` mit `patterns: [{ urlPattern: "*", requestStage:
"Request" }]` — jede einzelne Anfrage im Browser pausiert und braucht einen
Roundtrip zum Driver-Prozess, bevor sie weiterläuft. Das aufrufer-seitige
Pattern (z. B. `**/FolderResource/**`) wird erst danach, im Driver-Prozess,
geprüft — zu spät, um den Pause/Resume-Roundtrip zu vermeiden. Das erklärt
exakt den beobachteten Befund "Pattern, das nichts matcht, kostet trotzdem
~30%" — der ist mit CDP-seitigem `"*"` unvermeidbar, keine Pattern-Wahl
rettet ihn. Zusätzlich schaltet dieselbe Codestelle `Network.setCacheDisabled
(true)` scharf, solange irgendeine Route aktiv ist — der komplette
HTTP-Cache der Session ist aus, solange interceptiert wird, unabhängig vom
Pattern.

### 4. _(aufgegangen in Frage 7 — siehe unten)_

### 5. Ist "30s" überhaupt an Discovery gebunden?
Das Ziel ist *"fühlt sich wie ein Klick an"*. Nie geprüft: Hintergrundlauf
vor dem Klick, Teilergebnisse während des Laufs, geänderte Kurse zuerst.
Diese Klasse braucht keine schnellere Discovery, sondern eine, die nicht
vor dem Nutzer steht.

### 6. Warum bleibt 1 von 12 Sektionen über Läufe hinweg instabil?
Der Rest wurde auf Wicket-Bookkeeping zurückgeführt. Dieser eine nicht.

### 7. Wenn nichts client-seitig rendert — was füllt dann die 336ms? (ersetzt die alte Frage 4)
Das Kampagnen-Fazit vom 2026-07-31 spät ("der Content-Tree ist auf jeder
Ebene JS-gerendert") und der heutige Quellcode-Befund ("alles ist
Server-HTML, kein Client-Rendering") widersprechen sich direkt — beide
stützen sich auf echte Belege (Live-DOM-Probe vs. Java-Quellcode +
OpenOLAT-eigene Doku), keiner ist bloße Behauptung. Das muss aufgelöst
werden, nicht stillschweigend überschrieben:
- **Kandidat A:** Settle-Zeit ist Netzwerk-/Transferzeit einer großen
  Server-Antwort, keine JS-Bauzeit. Plausibel, weil jede Coursenode-Seite den
  kompletten `o_tree` des Kurses mitliefert — bei 282 Sektionen potenziell ein
  großes HTML-Dokument pro Request. **Live gemessen 2026-08-01 (siehe
  "Nächstes Experiment" unten): widerlegt in der geprüften Form.** Bytes
  wachsen bei 27x mehr Sektionen nur um 1,4x, Netzwerkanteil bleibt bei
  25–31% — Minderheit, nicht Erklärung. Offen, warum die Bytes nicht
  skalieren (→ Frage 9).
- **Kandidat B:** Die Probe hat etwas anderes gemessen als Baum/Tabelle
  selbst — z. B. wächst die Trefferzahl von `looksLikeSectionFolderLink`
  einfach, weil der Browser ein großes statisches HTML-Dokument noch
  parst/layoutet, nicht weil JS etwas nachbaut.
- **Kandidat C:** Ein schmal begrenztes JS-Widget auf der Seite (nicht Baum
  oder Tabelle selbst) ist verantwortlich — ungeprüft, welches.

### 8. Welcher der beiden `ctx.Route`-Kosten dominiert — Cache-Aus oder Pause/Resume?
Frage 3 fand zwei getrennte Mechanismen hinter derselben Zahl:
`Network.setCacheDisabled(true)` (kein Browser-Cache mehr für die ganze
Session) und der CDP-`Fetch`-Pause/Resume-Roundtrip pro Anfrage (immer `"*"`,
unabhängig vom Pattern). Playwright koppelt beide fest — ein Aufrufer kann sie
in dieser Driver-Version (1.61.1) nicht einzeln abschalten. Ungeprüft: wie
viel der ~30% ist reines Cache-Aus (viele kleine statische Wicket-Assets pro
Sektionsseite, die sonst aus dem Cache kämen) gegenüber reinem
Pause/Resume-Overhead (ein CDP-Roundtrip pro Anfrage, unabhängig von der
Antwortgröße)? Lokal reproduzierbar ohne Account — z. B. via `page.route` auf
eine Seite mit vielen kleinen statischen Assets, einmal mit und einmal ohne
zusätzlichen `CDPSession.send("Network.setCacheDisabled", {cacheDisabled:
false})`-Override direkt nach `Fetch.enable`, falls das die Kopplung
umgehen lässt. Wichtig für "previews.go ohne `ctx.Route` ausliefern"
(Zeile 70ff. dort): wenn Cache-Aus der Haupttäter ist, reicht ein
`page.on('request')`-Listener ohne Interception nicht als Ersatz, weil der
nichts am Cache-Verhalten ändert — dann bräuchte es tatsächlich einen
browser-seitigen Blockmechanismus ohne CDP-`Fetch`-Domain.

### 9. ~~Warum wächst die Sektionsseiten-Response kaum mit der Kursgröße?~~ Beantwortet 2026-08-01, siehe Bericht unten
**Kandidat (a) bestätigt, mit Beleg — Regel 2 erfüllt.**
`MenuTreeRenderer.isRenderChildren()` (OpenOLAT-Quelle, Methode ab Zeile 660)
rekursiert nur in einen Kindknoten, wenn dessen `ident` in `openNodeIds`
steht oder er auf dem Pfad zum aktuell selektierten Knoten liegt (`curSel ==
curRoot`); sonst liefert die Methode `false` und `renderLevel()` (Zeile 232:
`if (renderChildren) { renderChildren(...); }`) ruft für diesen Teilbaum
gar nicht erst rekursiv auf. Der Baum-Fragment ist also strukturell auf
"offene Knoten + Selektionspfad" begrenzt, nicht auf "alle Sektionen" — exakt
der Mechanismus, der die gemessene 1,4x/27,3x-Diskrepanz vorhergesagt hätte.
Kandidat (b) (Caching) ist damit nicht mehr nötig, um den Befund zu
erklären, und wurde nicht separat geprüft.

Was das für Kandidat A (Frage 7) bedeutet: **endgültig tot**, jetzt mit
Mechanismus statt nur mit Gegenbeweis. Was offen bleibt: Wenn weder das
Baum-Fragment noch die Übertragungszeit mit der Kursgröße skalieren, aber
settle+stable trotzdem 511–525ms/Sektion braucht (Kandidat B/C, 69–75%
unerklärt) — skaliert diese Zeit vielleicht mit etwas anderem als
Kursgröße, z. B. der Dateizahl *innerhalb* der gerade besuchten Sektion,
statt mit der Gesamtsektionszahl des Kurses? Ungeprüft, und das ist jetzt
die konkrete nächste Frage vor dem in Frage 7 bereits angekündigten
Browser-Profiling.

---

## Nächstes Experiment

**Frage:** (11, neu aus Frage 10) — was füllt die settle+stable-Zeit, wenn
weder Netzwerktransfer (24%) noch Sektions-Dateizahl (r=0,40, ~16%
Varianz) die Mehrheit erklären? Kandidat: fixer Overhead pro
Sektionsseite (Layout/Paint/CPU, nicht Netzwerk oder Inhaltsmenge).

**Vorhersage:** Noch nicht geschrieben — dieses Experiment braucht zuerst
eine Entscheidung, welches Profiling-Werkzeug (Playwright Tracing API,
CDP `Performance`/`Tracing` Domain, oder `page.evaluate` mit
`performance.getEntriesByType`) die CPU-/Layout-Zeit einer Sektionsseite
separat von Netzwerkzeit sichtbar macht, ohne selbst signifikanten
Overhead in die Messung einzuschleusen. Das ist der erste inhaltliche
Schritt des nächsten Zyklus, nicht mehr reines Lesen oder Aufbauen auf der
bestehenden Probe.

**Kosten:** Unbekannt, vermutlich der teuerste Schritt bisher in dieser
Kampagne — echtes Browser-Profiling braucht neues Instrument, nicht nur
eine Erweiterung von `network_timing_probe_test.go`.

---

## Vorheriges Experiment (Frage 10, abgeschlossen 2026-08-01)

**Frage:** (10, neu aus Frage 9) — skaliert settle+stable pro Sektion mit
der Dateizahl *in der gerade besuchten Sektion*, statt mit der
Gesamtsektionszahl des Kurses?

**Vorhersage:** Innerhalb desselben großen Kurses (Softwaretechnologie, 164
Sektionen) korreliert die pro-Sektion settle+stable-Zeit mit der Dateizahl
dieser einen Sektion — Sektionen mit vielen Dateien brauchen spürbar länger
als leere/dateiarme Sektionen.

**Gescheitert ab:** Wenn settle+stable pro Sektion auch bei stark
unterschiedlicher Dateizahl (z. B. 0 vs. 20+ Dateien) im selben Kurs flach
bleibt, ist Dateizahl nicht die erklärende Variable — dann ist die
verbleibende Zeit ein fixer Overhead pro Sektionsseite (Navigation,
Wicket-Bookkeeping, Layout/Parsing einer im Wesentlichen konstant großen
Seite), und das braucht laut Modell (Frage 7) echtes Browser-Profiling,
nicht mehr Quellcode-Lesen oder Netzwerk-Tracing.

**Kosten:** Erweiterung der bestehenden Probe
(`network_timing_probe_test.go`) um Dateizahl pro Sektion neben
`sectionTiming`, ein Live-Lauf gegen den echten Account (nur der bereits
gecrawlte große Kurs), kein Diff gegen Ground-Truth nötig.

**Ergebnis (2026-08-01, `opal-downloader-sync-speed`, dieser Zyklus):
weder bestätigt noch sauber widerlegt — schwacher, nicht dominanter
Zusammenhang.** `OpalScraper.sectionProbe` (neuer Hook, nil in Produktion,
`internal/scraper/scraper.go`/`crawl.go`) misst pro Sektion settle+stable
gegen die Kandidatenzahl (`candidateStabilityPoll`-Trefferzahl, Proxy für
Dateizahl). Live-Lauf, nur der große Kurs (164 Sektionen, Kandidatenzahl
21–72): **Pearson r = 0,40** zwischen Kandidatenzahl und settle+stable-Zeit
pro Sektion. Das ist real (nicht 0, also nicht "flach" im Sinne des
Scheitern-Kriteriums), aber schwach — r²≈16% der Varianz erklärt, weit
entfernt von "spürbar länger bei vielen Dateien" als Haupterklärung.

Zusammen mit Frage 7 (Netzwerk erklärt 24% dieses Laufs) bleibt der
Großteil der settle+stable-Zeit unerklärt durch beide bisher geprüften
Kandidaten (Netzwerkbytes, Sektions-Dateizahl). Das deckt sich mit der im
Modell schon vor diesem Zyklus benannten Konsequenz: die verbleibende Zeit
sieht nach einem weitgehend **fixen Overhead pro Sektionsseite** aus, nicht
nach etwas, das mit Inhaltsmenge (Baum oder Datei-Tabelle) skaliert — egal
ob gemessen über Kursgröße (Frage 9) oder Sektions-Dateizahl (hier).
Reines Quellcode-Lesen und Netzwerk-Tracing sind damit als Werkzeuge für
diese Frage ausgereizt; der nächste Schritt braucht echtes
Browser-Profiling (CPU/Layout/Paint), wie Frage 7 das schon vorhergesagt
hatte.

---

## Vorheriges Experiment (Frage 9, abgeschlossen 2026-08-01, reines Quellcode-Lesen)

**Frage:** (9) — serialisiert `MenuTreeRenderer` den ganzen Kursbaum auf
jeder Sektionsseite, oder nur den sichtbaren/aufgeklappten Teilbaum?

**Vorhersage:** Der Quellcode zeigt bedingte Rekursion (z. B. ein
`if (node.isOpen())`-artiger Check vor dem rekursiven Aufruf für
Kindknoten), die erklärt, warum die gemessene Response-Größe nicht mit der
Gesamtsektionszahl skaliert.

**Gescheitert ab:** Wenn der Code unbedingt über alle Kindknoten
rekursiert (kein Open/Closed-Gate erkennbar), ist (a) widerlegt und es
bleibt nur (b) (Caching) oder eine dritte, noch nicht benannte Erklärung —
dann zurück zu reinem Messen: Response-Bytes zweier verschiedener
Sektionsseiten *innerhalb desselben großen Kurses* vergleichen (variieren
sie mit der angeklickten Sektion, oder sind sie auch dort flach?).

**Kosten:** Quellcode-Lesen (`gh search code --repo OpenOLAT/OpenOLAT`),
kein Build, kein Live-Lauf nötig.

**Ergebnis (2026-08-01, `opal-downloader-sync-speed`, dieser Zyklus):
Vorhersage bestätigt, mit Beleg — Kandidat (a) erwiesen.**
`isRenderChildren()` (`MenuTreeRenderer.java`, ab Zeile 660) gibt `true`
nur zurück, wenn der Knoten in `openNodeIds` steht oder auf dem
Selektionspfad liegt (`curSel == curRoot`); sonst `false`, und
`renderLevel()` (Zeile 232) ruft `renderChildren(...)` dann gar nicht erst
auf — der rekursive Abstieg endet strukturell an jedem nicht offenen,
nicht selektierten Knoten. Das ist die scharfe Erklärung, die Regel 2
verlangt: der Baum-Fragment-Anteil der Response ist auf offene Knoten +
Selektionspfad begrenzt, nicht auf die Gesamtsektionszahl — exakt das
Muster, das die 1,4x/27,3x-Diskrepanz aus dem vorherigen Experiment
vorhersagt. Kandidat A (Frage 7) ist damit nicht nur widerlegt, sondern mit
Mechanismus geschlossen. Neue Frage (Regel 3): Frage 10 oben.

---

## Vorheriges Experiment (Frage 7, abgeschlossen 2026-08-01)

**Frage:** (7) — erklärt Netzwerk-/Transferzeit einer großen
Server-HTML-Antwort die 336ms Settle-Wait, statt Client-JS?

**Vorhersage:** Response-Größe (Content-Length) und Time-to-last-byte einer
Coursenode-Seite korrelieren mit der Kursgröße (Sektionsanzahl im Baum), und
"Netzwerk fertig" fällt zeitlich nah mit "Kandidatenzahl stabil" zusammen.

**Gescheitert ab:** Wenn die Antwortkörper klein sind (wenige KB,
Transfer < 50ms) während Settle/Stability weiter 300ms+ braucht, ist Transfer
nicht die Erklärung — dann bleibt offen, was in der Zeit läuft (zurück zu
Kandidat B/C), und das braucht ein echtes Browser-Profiling, kein Quellcode-
Lesen mehr.

**Kosten:** Ein Live-Lauf gegen den echten Account (Netzwerk-Timing pro
Sektion mitschneiden), kein Build-Risiko — rein lesende Instrumentierung,
kein Diff gegen den Ground-Truth-Sync nötig, weil nichts am Sync-Verhalten
geändert wird.

**Ergebnis (2026-08-01, `opal-downloader-sync-speed`, dieser Zyklus):
Live-Lauf durchgeführt, Vorhersage widerlegt — Kandidat A (Baumgröße treibt
Response-Größe) ist tot, aber mit einer neuen, engeren Frage statt
geschlossen (Regel 2 noch nicht erfüllt, siehe unten).**

Ergebnis (`tmp/settle-timing-network-trace.txt`):

| | Algorithmen u. Datenstrukturen (6 Sektionen) | Softwaretechnologie (164 Sektionen) |
|---|---|---|
| avg. Dokument-Response | 5604 Bytes / 79ms | 7789 Bytes / 65ms |
| settle+stable pro Sektion | 511ms | 525ms |
| Netzwerkanteil an settle+stable | 31% | 25% |

Byteverhältnis (größer/kleiner) **1,4x** bei einem Sektionsverhältnis von
**27,3x**. Die Vorhersage verlangte, dass Response-Größe mit der Kursgröße
mitwächst (Begründung: `MenuTreeRenderer` liefert den kompletten `o_tree`
auf jeder Sektionsseite mit). Das ist nicht eingetreten — die Bytes bleiben
über einen 27-fachen Größenunterschied praktisch flach, die Transferdauer
sinkt sogar leicht. Deckt sich mit dem Scheitern-Kriterium: kleine Bodies
(5,6–7,8 KB), Transfer im Bereich 65–130ms/Sektion (2 Dokument-Requests je
Sektion), während settle+stable bei 511–525ms/Sektion bleibt — Netzwerk
erklärt höchstens 25–31%, nie die Mehrheit.

Nebenbefund, nicht Teil der Vorhersage, aber informativ: settle+stable
pro Sektion ist zwischen den beiden Kursen fast identisch (511 vs. 525ms) —
deckt sich mit dem bereits bekannten Aggregat aus der Tabelle oben
(338+172=510ms/Sektion). Das Timing selbst skaliert also so oder so nicht
mit der Kursgröße; nur die Vorhersage, *warum* es das nicht tut (Bytes
skalieren auch nicht), ist neu.

**Warum Kandidat A trotzdem offen bleibt (Regel 2):** Widerlegt ist nur die
Vorhersage "Response wächst mit Kursgröße", nicht der Mechanismus dahinter.
Es gibt zwei unbestätigte Erklärungen, warum `MenuTreeRenderer` trotz
27x mehr Sektionen kaum mehr Bytes schickt:
- **(a)** Der Renderer serialisiert nicht den ganzen Baum, sondern nur den
  sichtbaren/aufgeklappten Teilbaum (Tiefe/offene Knoten statt
  Gesamtsektionszahl) — dann wäre Kursgröße die falsche unabhängige
  Variable, nicht die Baumgröße als solche widerlegt.
- **(b)** Der Baum wird serverseitig irgendwo gecached/wiederverwendet und
  nur ein Diff oder Verweis geschickt.
Keine davon ist geprüft. Ungeprüft, auch: die 25–31% Netzwerkanteil sind
real, aber Minderheit — was füllt die restlichen 69–75%, wenn nicht
Client-JS (Frage 1, beantwortet) und nicht Netzwerktransfer (jetzt
größtenteils widerlegt)? Das war schon vor diesem Lauf Kandidat B/C und
bleibt es.

Die Probe (`internal/scraper/network_timing_probe_test.go`,
`OPAL_SETTLE_TIMING_TRACE=1`) crawlt die kleinste und die größte
Content-Kurs im Account nacheinander und stellt pro Kurs Bytes/Dauer der
Section-Seiten-Dokumentresponses (`Request.Sizes()`/`Request.Timing()`)
neben `sectionTiming` (settle+stable, diese Datei kannte den Mechanismus
schon, siehe oben).

Vor dem ersten Live-Lauf ein Blick in den bereits archivierten Trace vom
2026-07-27 (`tmp/network-trace-Softwaretechnologie (SoSe 26).txt`, aus einer
anderen Probe): 324 Hauptframe-Dokumentresponses, **0 bytes
content-length-Summe**. Ein Java/Wicket-Servlet, das eine Seite dynamisch
baut, puffert sie nicht komplett, um vorher `Content-Length` auszurechnen —
es schickt Chunked Transfer-Encoding, das den Header ganz weglässt. Die
erste Fassung dieser Probe hätte also für jede Section-Seite in beiden
Kursen "0 bytes" gemeldet und Kandidat A fälschlich für widerlegt gehalten —
nicht weil die Antwort klein war, sondern weil das Instrument blind war.
Umgestellt auf `Request.Sizes()` (echte transferierte Bytes, nicht der
Header) — das braucht einen Round-Trip in den Browser-Prozess, deshalb
absichtlich *nicht* im `OnResponse`/`OnRequestFinished`-Handler aufgerufen
(derselbe Deadlock, den `network_trace_probe_test.go` am 2026-07-27 schon
55 Minuten lang live hatte), sondern danach, wenn die Dispatch-Loop wieder
frei ist.

**Live-Lauf blockiert:** gespeicherte Session war abgelaufen
(`ensureSession: timed out after 300000ms waiting for the OPAL course list
after login`). Die damalige Begründung — "Login braucht 2FA im geöffneten
Browserfenster, unbeaufsichtigt nicht überbrückbar" — **war falsch** und wird
hier stehen gelassen, weil sie einen ganzen Zyklus gekostet hat. TU-Fast im
dedizierten Profil erledigt Login und 2FA selbst; eine abgelaufene Session ist
kein Blocker. Nachgemessen 2026-08-01: `list` mit abgelaufenem State →
Auto-Login → 8 Kurse in 3,7 s, ohne Klick. Der 300-s-Timeout am 2026-07-31 war
also ein echter Fehlschlag mit unbekannter Ursache, keine strukturelle Grenze —
beim nächsten Auftreten die Fehlermeldung untersuchen, nicht die Person rufen.
Browser/Profil wurden sauber geschlossen (`sc.Close()` lief über den
regulären `defer`, `rate ceiling: 2 navigation(s), 0 delayed` bestätigt
es), nichts blieb hängen.

Frage 7 bleibt offen — keine neue Live-Messung diesmal. Aber die Probe ist
jetzt lauffähig und der Bytes-Messweg schon gegen einen echten Bug
verifiziert; der nächste Zyklus mit valider Session kann direkt messen statt
erst zu bauen.

---

## Berichte

Alle 5 Zyklen, jeder endet mit einer Empfehlung: weitermachen oder aufhören,
und warum. Die Stopp-Entscheidung trifft der Maintainer, nicht ein Zähler —
kein Deckel auf die Kampagne, das Kill-Kriterium sitzt pro Experiment
(Entscheidung vom 2026-07-31, Gegenargumente in derselben Sitzung notiert:
jede Abbruchbedingung, die dieses Repo je hatte, wurde zu dem, woran die
Arbeit aufhörte).

### 2026-07-31 (autopilot): Frage 1 gelesen, nicht gemessen

**Quellen (primär, `gh search code --repo OpenOLAT/OpenOLAT`):**
- `src/main/java/org/olat/core/gui/components/tree/MenuTreeRenderer.java` —
  baut `.o_tree`/`.o_tree_l{n}` als Java-`StringBuilder`-HTML, synchron,
  serverseitig. Kein JS-Templating.
- `src/main/java/org/olat/core/gui/components/table/TableRenderer.java` +
  die FlexiTable-Renderer (`.o_table_wrapper`, `.o_table_flexi`) — dasselbe
  Muster: Java-Renderer erzeugt komplettes Tabellen-HTML inkl. Paging-Links.
- Die alte `Table`-Klasse (`org.olat.core.gui.components.table.Table`,
  vermutlich NICHT die für Kursordner verwendete — das ist FlexiTable) kennt
  einen URL-Parameter `COMMAND_PAGEACTION_SHOWALL="a"`. Nicht live geprüft,
  ob die Kursordner-Dateiliste diesen Pfad überhaupt nutzt — der bestehende
  Wicket-AJAX-Klick in `wicket.go` ist bereits live vermessen (0 Fehler,
  byte-identische Parität) und wurde hierdurch **nicht** ersetzt.
- REST-API (`/repo/courses/{id}/elements/folder/{nodeId}/files`,
  `VFSWebservice`) existiert im Quellcode — aber am Reverse-Proxy bereits mit
  403 gemessen (`docs/sync-speed-campaign.md` Zeile 899), unabhängig
  bestätigt tot. WebDAV ebenso bereits mit blankem 200 gemessen (dead
  backend) — `docs/webdav-propfind-research.md`. Beides nicht neu getestet,
  nur der Quellcode-Fund gegen die schon vorliegenden Messungen abgeglichen.
- Sekundär, zur Bestätigung: OpenOLATs eigene `.claude/openolat-frontend-
  knowledge.md` im selben Repo sagt wörtlich "No client-side framework (no
  React/Angular/Vue). All state lives on the server."

**Ergebnis: Vorhersage (60% Tabellen-Zustandsklasse) widerlegt, aber mit
Erklärung, die das Fehlen vorhersagt (Regel 2 erfüllt) — kein Marker,
weil nichts client-seitig aufgebaut wird, das einen Marker bräuchte.**
Deckt sich mit dem bereits bekannten "Dateizeilen sind in der initialen
Antwort server-gerendert, null Wicket-AJAX" aus `wicket.go`. Frage (1c,
Pager-Parameter) bleibt unklar, ändert aber nichts am bestehenden,
funktionierenden Wicket-Signal-Ansatz. Frage (1b, alternative View) bleibt
verneint — beide bekannten Alternativen sind bereits unabhängig als tot
gemessen.

**Neue offene Frage (Regel 3):** Frage 7 oben — der Quellcode-Befund
widerspricht der Live-DOM-Probe vom 2026-07-31, die zum Verwerfen des
Nav-Walk-Hebels führte. Nicht aufgelöst, nur präzise benannt.

**Nicht ausgeführt in diesem Zyklus:** das oben definierte nächste
Experiment (Netzwerk-Timing live messen) — das ist ein Lauf gegen den echten
Account, nicht mehr Lesen, und gehört in einen `opal-downloader-sync-speed`-
Zyklus mit dessen eigener Berichts-Kadenz statt in diesen allgemeinen
Autopilot-Lauf.
