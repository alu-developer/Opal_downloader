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

**Entscheidung des Maintainers, 2026-08-03:** *"es soll immer noch an
schneller discovery gearbeitet werden, aber der rest ist auch gut."* Die
Kampagne wird also **nicht** auf die Verdeckungs-Klasse umgeschwenkt —
schnellere Discovery bleibt die Hauptlinie und behält die Priorität in der
Fragenliste. Hintergrundlauf/Teilergebnisse sind damit aber ausdrücklich
zulässige Arbeit statt einer Ausweichbewegung: sie dürfen aufgegriffen
werden, wenn die Discovery-Linie gerade auf eine Messung wartet oder eine
Frage dort erschöpft ist. Diese Frage bleibt deshalb offen und wandert
nicht nach oben.

### 6. Warum bleibt 1 von 12 Sektionen über Läufe hinweg instabil?
Der Rest wurde auf Wicket-Bookkeeping zurückgeführt. Dieser eine nicht.
Möglicherweise dieselbe Ursache wie Frage 17 — dort ist der instabile Knoten
zum ersten Mal namentlich bekannt und als paginiert identifiziert.

### 17. Warum verliert eine paginierte Sektion unter Kontention ihre zweite Seite? (neu aus Frage 16, 2026-08-03)
Frage 16 hat einen reproduzierbaren Verlust gefunden, der **nicht** am
Settle-Budget hängt: derselbe Kursbaustein (`CourseNode/1775615795226691003`,
6 Dateien) fehlte in 2 von 4 Läufen, je einmal unter der unveränderten
500ms/6000ms-Konfiguration und einmal unter 150ms/4000ms. Der Knoten ist
paginiert (`offered a "show all" control` im Lauf-Log), und genau dieser
Wicket-Klickpfad trägt die Verlustgeschichte der Kampagne.

Offen ist der Mechanismus, und er ist mit einem billigen Lauf eingrenzbar,
weil die Frage binär ist:
- **Kandidat A: der „show all"-Klick wird unter Kontention gar nicht
  ausgelöst** — der Control ist zum Zeitpunkt der Prüfung noch nicht da, die
  Sektion gilt als einseitig und wird ohne zweite Seite abgehakt.
- **Kandidat B: der Klick läuft, aber sein Ergebnis wird nicht abgewartet** —
  die AJAX-Antwort trifft nach dem Stability-Poll ein.
- **Kandidat C:** kein Kontentions-Effekt, sondern serverseitige Varianz an
  diesem Baustein. Widerlegbar durch Läufe bei `concurrency=1`.

**Nächster Schritt, entschieden:** zuerst C ausschließen — dasselbe Kurspaar
zweimal bei `course_concurrency=1`, gleiche Probe. Bleiben beide Läufe bei
248 Dateien, ist Kontention die Ursache und das Settle-Budget vollständig
entlastet; taucht der Verlust auch dort auf, ist es kein Concurrency-Thema
und Frage 17 wird zu einem Bug-Report über den Paginierungspfad. A gegen B
trennt danach ein Log am Klick selbst, nicht noch ein Timing-Lauf.

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

**Frage:** (16, neu aus Frage 15) — hält die 150ms-`mutationObserverDebounceMs`
auch unter echter `course_concurrency>1`-Kontention (mehrere Kurs-Tabs
rendern gleichzeitig, konkurrieren um CPU/Event-Loop)? Frage 15 hat das
bewusst nicht getestet (siehe deren "Referenzpunkt": `OPAL_DEBOUNCE_MS_OVERRIDE`
kurzschließt die `effectiveCourseConcurrency() > 1`-Verzweigung in
`contentSettleWaitBudget()` komplett, ein Solo-Kurs-Lauf mit
`SetCourseConcurrency(2)` erzeugt also gar keine echte Konkurrenz). Genau
unter Kontention passierten historisch alle echten Datenverluste dieser
Kampagne (`docs/sync-speed-campaign.md`; `course_concurrency=2` verlor 9
Dateien am 2026-07-26). Das Projekt hält selbst schon 500ms/6000ms (statt
300ms/4000ms) für nötig, sobald Kontention vorliegt — die offene Frage ist,
ob eine gesenkte Serial-Debounce-Zeit diese Marge unterläuft, wenn der
Override beide Werte gleich setzt statt nur den seriellen.

**Referenzpunkt (gelesen vor der Vorhersage):** die Override senkt unter
Kontention **zwei** Werte gleichzeitig, nicht einen. `contentSettleWaitBudget()`
(`navigation.go` Zeile 397-405) gibt bei gesetzter Override
`(ms, mutationObserverHardCapMs)` zurück — also den **seriellen** Hard Cap
(4000ms), nicht den konkurrenten (6000ms). Ein Lauf mit
`OPAL_DEBOUNCE_MS_OVERRIDE=150` unter `course_concurrency=2` fährt damit
150ms/4000ms gegen die Vergleichsbasis 500ms/6000ms: Debounce auf 30%, Hard
Cap auf 67%. Das ist ein schärferer Test als Frage 14/15 (dort nur
150ms/4000ms gegen 300ms/4000ms, Hard Cap unverändert) und die Ursache muss
bei einem Fehlschlag zwischen beiden Größen getrennt werden.

**Vorhersage:** Alle vier Läufe (2× Override 150ms/4000ms unter echter
Kontention, 2× unverändertes 500ms/6000ms) finden dieselbe Dateimenge — kein
Selbst-Diff, kein Cross-Diff. Mechanismus: der Debounce misst *Ruhe nach der
letzten Mutation*, nicht absolute Zeit. Kontention verzögert die Mutationen
selbst, verschiebt also das Fenster nach hinten, statt es zu verkürzen; sie
erzeugt nur dann einen Verlust, wenn sie *Lücken innerhalb* des Renderns über
150ms aufreißt (Renderer bekommt die CPU für >150ms nicht, obwohl er noch
nicht fertig ist). Frage 9 (Baum-Fragment strukturell auf offene Knoten
begrenzt) sagt zusätzlich, dass die pro Sektion zu rendernde Menge unter
Kontention nicht wächst. Zeitersparnis erwartet **unter** den 28,7% von Frage
15 — unter Kontention sitzt mehr Zeit im tatsächlichen Rendern und im Hard
Cap, wo die Override nichts spart.

**Gescheitert ab:** Jede Datei-/Byte-Abweichung, egal in welcher Richtung —
Selbst-Diff zwischen zwei Läufen derselben Bedingung oder Cross-Diff zwischen
Override und Basis. Genau unter Kontention passierten alle echten
Datenverluste dieser Kampagne (`course_concurrency=2` verlor am 2026-07-26
9 Dateien), also ist hier eine einzelne fehlende Datei ein Nein, keine
Messungenauigkeit. Bei Fehlschlag ist die nächste Frage nicht "150ms zu
kurz?", sondern welcher der beiden gesenkten Werte es war: ein Wiederholungs-
lauf mit 150ms-Debounce **und** explizit 6000ms Hard Cap trennt das.

**Kosten:** Vier Läufe à zwei gleichzeitig gecrawlte Kurse (klein + groß, wie
Frage 15). Kein Default ändert sich — die Override ist test-only und
standardmäßig aus.

**Ergebnis (2026-08-03): Vorhersage in beiden Teilen widerlegt — aber nicht
dort, wo sie angegriffen wurde. Frage 16 ist so, wie sie gestellt war, nicht
beantwortbar.** Rohdaten: `tmp/debounce-contention-probe.txt`.

| Lauf | Dateien | settle+stable |
|---|---|---|
| baseline-1 (500ms/6000ms) | **248** | 130362ms |
| baseline-2 (500ms/6000ms) | **242** | 132070ms |
| override-1 (150ms/4000ms) | **242** | 63769ms |
| override-2 (150ms/4000ms) | **248** | 67055ms |

Die Dateimengen weichen ab — aber **die Basis weicht von sich selbst ab**.
`baseline-1` gegen `baseline-2` unterscheiden sich um exakt dieselben 6
Dateien wie jeder andere Vergleich, und jede Bedingung lieferte einmal 248
und einmal 242. Es gibt hier keine Bedingung, die stabil ist, und damit
nichts, wogegen sich 150ms messen ließe: **die unveränderte, heute geltende
Konfiguration verliert unter Kontention genauso.** Das entlastet die Override
nicht, es entzieht dem Experiment die Vergleichsbasis. Ein Nachlauf mit
150ms-Debounce und wiederhergestelltem 6000ms-Cap (der oben geplante
Trennungsschritt) wäre jetzt sinnlos — er würde gegen dieselbe instabile Basis
messen.

Die 6 Dateien sind immer dieselben, aus **einem** Kursbaustein
(`CourseNode/1775615795226691003`, `Vorlesung_7`/`7p`/`8`/`8p`/`9_10`/`9_10p`)
— und der Lauf-Log sagt über genau diesen Knoten `offered a "show all"
control`, er ist also **paginiert**. Damit zeigt der Verlust nicht auf das
Settle-Budget, sondern auf den Wicket-„show all"-Klickpfad (`crawl.go`), der
die Verlustgeschichte dieser Kampagne ohnehin schon trägt: unter Kontention
wird entweder der Klick nicht ausgeführt oder sein Ergebnis nicht gelesen,
bevor die Sektion als fertig gilt. Ein Settle-Debounce, der Ruhe nach
Mutationen misst, kann eine zweite Seite, die nie angefordert wurde, gar
nicht abwarten.

Der Zeitteil der Vorhersage ("Ersparnis unter 28,7%") war ebenfalls falsch,
und aus einem uninteressanten Grund: gemessen wurden **50,1%**, weil die
Basis hier 500ms ist und nicht die 300ms von Frage 15 — 150ms ist gegen 500ms
ein viel größerer relativer Schnitt. Das war vor dem Lauf ableitbar und wurde
beim Schreiben der Vorhersage übersehen. Wall clock spart deutlich weniger
(169,1s → 151,4s), weil ein wachsender Teil der Laufzeit unter Kontention
nicht im Settle-Wait sitzt.

**Nicht betroffen sind heutige Nutzer:** `DefaultCourseConcurrency = 1`
(`internal/config/config.go` Zeile 343), und bei `concurrency=1` fanden
Frage 14 und 15 über vier bzw. vier Läufe identische Dateimengen. Der Befund
schließt aber `course_concurrency>1` als Geschwindigkeitshebel weiter aus —
und liefert erstmals einen benannten Mechanismus statt der bisherigen
Beobachtung "course=2 verlor am 2026-07-26 9 Dateien".

---

## Vorheriges Experiment (Frage 15, abgeschlossen 2026-08-02)

**Frage:** (15, neu aus Frage 14) — Frage 14 bestätigte die gesenkte
`mutationObserverDebounceMs` (150ms) nur auf dem **kleinen** Kurs (6
Sektionen, 38 Dateien, `course_concurrency=1`, eine bereits vorher bekannte
paginierte Sektion verhielt sich in allen 4 Läufen identisch). Die
historischen Datenverlust-Vorfälle dieser Kampagne (Wicket-AJAX-Race,
`docs/sync-speed-campaign.md`; `sectionContentRequiredStableReads`s eigene
Historie, `crawl.go` Zeile 920ff.) passierten alle unter **Kontention**:
großer Kurs mit vielen paginierten Sektionen, oft zusätzlich unter
`course_concurrency>1`, wo der Renderer die Maschine nicht mehr für sich
allein hat. Frage 14s Testfall deckt genau das nicht ab. Hält die gesenkte
Debounce-Zeit auch auf dem großen Kurs (Softwaretechnologie, 164 Sektionen)
und/oder unter `course_concurrency>1` — oder zeigt sich dort genau die
Art von Verlust, die die bisherigen Vorsichtsmaßnahmen (separates,
breiteres `mutationObserverConcurrentDebounceMs`-Budget) schon andeuten?

**Referenzpunkt (gelesen vor der Vorhersage):** `mutationObserverConcurrentDebounceMs`
(`navigation.go` Zeile 127) steht bei 500ms gegen 300ms seriell — das Projekt
hält für Kontention selbst schon 67% mehr Sicherheitsspanne für nötig. Aber
`contentSettleWaitBudget()` (Zeile 397-401) prüft `OPAL_DEBOUNCE_MS_OVERRIDE`
**vor** der `effectiveCourseConcurrency() > 1`-Verzweigung — ist die Override
gesetzt, kommt sie unabhängig von Concurrency zurück, die 500/300-Marge
existiert für den Override-Pfad also gar nicht. Damit lässt sich mit dem
bestehenden Probe (`debounceoverride_probe_test.go`, nur `sc.collectCourseFiles`
auf einen Einzelkurs) echte Concurrency-Kontention (konkurrierende Tabs, die
sich CPU/Event-Loop teilen) gar nicht testen — `SetCourseConcurrency(2)` auf
einen Solo-Kurs-Lauf würde nur den ungenutzten Zweig anders belegen, ohne dass
je ein zweiter Tab tatsächlich mitrendert. Diese Runde testet deshalb bewusst
nur die Kursgröße (großer Kurs, `course_concurrency=1`); echte
Mehr-Kurs-Konkurrenz bleibt Frage 16.

**Vorhersage:** Zwei Läufe des bestehenden Probes gegen den großen Kurs
(`Softwaretechnologie (SoSe 26)`, 164 Sektionen) bei 150ms-Override finden
dieselbe Dateimenge in beiden Läufen (Selbstkonsistenz) — der in Frage 9
gefundene Mechanismus (Baum-Fragment strukturell auf offene Knoten begrenzt,
nicht auf Kursgröße) sagt voraus, dass der Debounce pro Sektion unabhängig
von der Gesamtsektionszahl wirkt, also überträgt sich Frage 14s
Korrektheits-Befund vom kleinen auf den großen Kurs. Ein frischer
300ms-Baseline-Lauf auf diesem Kurs wird bewusst NICHT wiederholt: der
2026-07-16-Livetest (`navigation.go` Zeile 91-100) hat 300ms auf exakt
diesem Kurs bereits gegen eine 344/344-Ground-Truth bestätigt — ein neuer
Baseline-Lauf heute wäre eine dritte Bestätigung derselben, längst
etablierten Zahl, nicht neue Evidenz. Verglichen wird stattdessen gegen
die historische 198-Datei-Zahl dieses Kurses.

**Gescheitert ab:** Jede Datei-/Byte-Abweichung zwischen den beiden
150ms-Läufen, oder gegen die historische 198-Datei-Zahl (gleiches Kriterium
wie Frage 14: ein einzelner sauberer Lauf reicht laut eigener Historie nicht,
zwei sind das Minimum). Zusätzlich: wenn die Ersparnis auf dem großen Kurs
deutlich unter den ~29,6% liegt, die Frage 14 auf dem kleinen Kurs gemessen
hat (z. B. <15%), widerlegt das nicht die Korrektheit, aber die Annahme
"Mechanismus ist kursgrößen-unabhängig".

**Kosten:** Zwei volle Crawls des großen Kurses (164 Sektionen) statt der
vollen Vier-Läufe-Probe (Baseline entfällt aus obigem Grund) — bei ~230s/Lauf
bei 300ms historisch, bei 150ms schneller, geschätzt ~5-6 Minuten Gesamtlauf,
passt in ein einzelnes Zeitfenster. Kein Produktionscode-Änderung nötig
(`OPAL_DEBOUNCE_MS_OVERRIDE` existiert schon). `course_concurrency>1` bleibt
unbeantwortet (siehe Referenzpunkt oben) — das ist die neue Frage 16, und sie
braucht andere Werkzeuge (echter Zwei-Kurse-Parallel-Crawl), nicht nur diesen
Probe mit einem anderen Flag.

**Erster Versuch (2026-08-02, `opal-downloader-autopilot`): kein Ergebnis,
Kollision mit einem zweiten gleichzeitig laufenden Routine-Run, nicht der
getestete Mechanismus.** `docs/BACKLOG.md`'s Noticed-Eintrag hat die Details.
Zwei Prozesse griffen im selben Realzeit-Fenster auf `login-profile` zu; der
andere Lauf scheiterte mit einem rohen Playwright-Launch-Timeout, dieser Lauf
hing 22 Minuten, bis der eigene `go test -timeout 20m` ihn tötete — beides
ohne das saubere `ErrProfileLocked`, das `acquireSessionLock` eigentlich
liefern soll. Kein Datei-Befund, keine Regression gemessen — die Frage ist
offen geblieben, nicht beantwortet.

**Zweiter Versuch (2026-08-02, direkt danach, verifiziert kein anderer
opal-downloader-Prozess lief): wieder kein Ergebnis, diesmal die
2026-07-31-Altlast des 300s-Course-List-Timeouts, ohne erkennbare Kollision.**
`ensureSession: timed out after 300000ms waiting for the OPAL course list
after login` nach 305,98s — TU-Fast öffnete das Loginfenster, aber die
Kursliste erschien nie. Kein Debug-Flag war an, also nichts Konkretes
mitgeschnitten. Behoben für das nächste Mal: `waitForLoggedInCourseLink`
(`session.go`) faltet die Seiten-URL beim Timeout jetzt direkt in den
zurückgegebenen Fehler, unbedingt, nicht hinter `--debug-clicks` versteckt.

**Dritter Versuch (2026-08-02): lief mechanisch durch, aber mit einer
Diskrepanz, die eine weitere Runde nötig machte.** `OPAL_DEBOUNCE_OVERRIDE_SKIP_BASELINE`
(kein frischer 300ms-Lauf, nur gegen die historische 198-Datei-Zahl vom
2026-07-16 verglichen): 210 Dateien, selbstkonsistent über beide
150ms-Läufe — aber 210 ≠ 198. Zwei Erklärungen wären damit vereinbar
gewesen: (a) der Kurs ist ein aktiver SoSe-26-Kurs und hat in 2,5 Wochen
echt 12 Dateien dazubekommen, oder (b) die Override tut etwas Falsches. Das
Skip-Baseline-Design konnte die beiden nicht unterscheiden — genau die
Lücke, die ein frischer Vergleich am selben Tag schließt.

**Vierter Versuch (2026-08-02), vollständiger Probe (2 Baseline + 2
Override, kein Skip): Vorhersage bestätigt, Diskrepanz aufgelöst.**

| Lauf | Dateien | settle+stable |
|---|---:|---:|
| baseline-1 (300ms) | 210 | 86670ms |
| baseline-2 (300ms) | 210 | 86376ms |
| override-1 (150ms) | 210 | 61583ms |
| override-2 (150ms) | 210 | 61837ms |

Der frische Baseline-Lauf findet selbst 210 Dateien, nicht 198 — die
Diskrepanz aus dem dritten Versuch war Kursinhalt-Drift (Erklärung a),
keine Override-Nebenwirkung. Alle drei Vergleiche (Baseline-Selbstkonsistenz,
Override-Selbstkonsistenz, Baseline-vs-Override) sind **exakt identisch**,
210 Dateien in allen 4 Läufen. Ersparnis: Ø-settle+stable sinkt von 86523ms
auf 61710ms, **28,7%** — praktisch identisch zur 29,6% des kleinen Kurses
(Frage 14), trotz eines 35x größeren Kurses (210 vs. 6 Sektionen betroffene
Größenordnung nach Dateizahl). Das ist die Signatur, die Regel 2 verlangt:
wäre die Ersparnis kursgrößenabhängig gewesen, hätten die beiden Kurse nicht
auf 1 Prozentpunkt genau übereingestimmt.

**Frage 15 damit beantwortet, mit Einschränkung:** Für `course_concurrency=1`
hält die 150ms-Debounce-Korrektheit auf beiden geprüften Kursen (klein: 6
Sektionen/38 Dateien, groß: 164 Sektionen/210 Dateien), mit praktisch
identischer Ersparnis (29,6%/28,7%) — der Mechanismus (300ms ist die
bindende Konstante, nicht kursgrößenabhängige Render-Arbeit, siehe Frage 9
und Frage 13) sagt genau dieses Ergebnis vorher und wird durch beide Läufe
bestätigt, nicht nur durch Zufallstreffer. **Ausdrücklich nicht geprüft:**
`course_concurrency>1` — siehe "Referenzpunkt" oben, wieso der bestehende
Probe das gar nicht messen kann. Genau dort lagen historisch alle echten
Datenverluste dieser Kampagne, also ist die Korrektheit unter Kontention die
Voraussetzung für einen echten Default-Wechsel, nicht diese Runde — Frage 16.

---

## Vorheriges Experiment (Frage 14, abgeschlossen 2026-08-02)

**Frage:** (14, neu aus Frage 13) — `mutationObserverDebounceMs` (300ms,
`navigation.go` Zeile 99) und `sectionContentPollIntervalMs` (150ms,
`crawl.go` Zeile 982) sind feste Konstanten. Frage 13 fand, dass die
gemessene settle-Zeit (Ø 326ms/Sektion) fast exakt der 300ms-Konstante
entspricht und die stable-Zeit (Ø 193ms) nahe an einem einzelnen
150ms-Poll-Intervall liegt — mit CPU-Arbeit (selbst großzügig über
`TaskDuration` gerechnet) als Erklärung für höchstens ~24 % der Zeit. Kann
`mutationObserverDebounceMs` sicher gesenkt werden, ohne Dateiverlust zu
riskieren?

**Vorrecherche (2026-08-02, Quellcode-Lesen, kein Live-Lauf):**
`sectionContentPollIntervalMs` selbst wurde bereits einmal genau in diese
Richtung verändert — 400→150ms am 2026-07-21 (`crawl.go` Zeile 965ff.),
explizit als *Sampling-Rate-Senkung, keine Geduld-Kürzung* (Gesamtbudget
`sectionContentMaxPolls` gleichzeitig angehoben, damit die Gesamtwartezeit
gleich bleibt). Live gemessen: 322/322 Dateien bei 150ms, kein Regression.
Aber derselbe Kommentar trägt eine explizite, bis heute unaufgelöste
Warnung: *"1 of 3 runs at the OLD 400ms setting silently lost 15 files
(...). That intermittent loss is NOT proven fixed by this change; three
clean runs are not enough to prove absence."* `mutationObserverDebounceMs`
selbst wurde dagegen nie in diese Richtung getestet — der einzige
dokumentierte Live-Test (2026-07-16, `navigation.go` Zeile 89ff.) validierte
300ms als korrekt gegen die alte fixe 1100ms-Wartezeit, probierte aber nie
einen niedrigeren Wert.

**Vorhersage:** Eine Senkung von `mutationObserverDebounceMs` auf 150ms
(gleiches Sampling-Muster wie die bereits bewährte Poll-Interval-Senkung,
`mutationObserverHardCapMs` unverändert lassen, damit die Gesamt-Geduld für
langsame Sektionen gleich bleibt) verliert bei wiederholten Läufen gegen den
345-Datei-Ground-Truth keine Dateien und spart durchschnittlich ~150ms/
Sektion (≈46 % der aktuellen Ø-326ms-settle-Zeit, ≈29 % von settle+stable).

**Gescheitert ab:** Jede Datei-/Byte-Abweichung gegen den Ground-Truth bei
**mindestens 2–3 wiederholten** Läufen (ein einzelner sauberer Lauf ist laut
der eigenen Historie oben *keine* ausreichende Evidenz für Verlustfreiheit).
Auch ein Lauf, der zwar alle Dateien findet, aber die Ø-Ersparnis unter
~50ms/Sektion bleibt, widerlegt den behaupteten Mechanismus (dann wäre
300ms nicht die bindende Grenze, MutationObserver würde real länger
brauchen als die Konstante vorgibt, und die Ø-326ms wären Zufall, kein
Beleg).

**Kosten:** höher als jedes bisherige Frage-13-und-früher-Experiment — dies
ist die erste Frage der Kampagne, die tatsächlich Scraper-Verhalten ändert,
nicht nur misst. Muss laut Aufgaben-Policy hinter einem Env-Flag liegen,
off by default (`docs/RESUME.md`/Scheduled-Task-Regeln: "Anything touching
discovery goes behind an env flag"). Braucht `scripts/compare-visit-runs.ps1`
und mehrere Live-Läufe gegen den echten Account — genau das
Wicket-AJAX-Race-Risiko, das diese Kampagne schon zweimal real getroffen hat
(`docs/sync-speed-campaign.md`, `sectionContentRequiredStableReads`s eigene
Historie oben).

**Umsetzung, abweichend von der Kostenschätzung:** `OPAL_DEBOUNCE_MS_OVERRIDE`
(neu, `navigation.go`s `contentSettleWaitBudget`, off by default — bei
gesetztem Wert ersetzt sie sowohl den seriellen als auch den
Concurrency>1-Debounce-Wert, `mutationObserverHardCapMs` bleibt in jedem
Fall unverändert, wie vorhergesagt). `scripts/compare-visit-runs.ps1`
wurde nicht gebraucht — ein neuer Probe-Test (`debounceoverride_probe_test.go`)
vergleicht Datei-URL-Mengen direkt in Go, ohne den Umweg über einen echten
`sync`/`list`-Lauf und dessen Visit-Log. Vier Läufe gegen den kleinen Kurs
(Baseline×2, 150ms×2 — bewusst nicht nur Baseline-vs-Override, siehe oben),
`course_concurrency=1` (Default).

**Ergebnis (2026-08-02, `opal-downloader-sync-speed`, dieser Zyklus,
Live-Lauf, `tmp/debounce-override-probe.txt`): Vorhersage bestätigt, auf
diesem Kurs, mit dieser Wiederholungszahl.**

| Lauf | Dateien | settle+stable |
|---|---:|---:|
| baseline-1 | 38 | 3094ms |
| baseline-2 | 38 | 3135ms |
| override-1 (150ms) | 38 | 2205ms |
| override-2 (150ms) | 38 | 2180ms |

Alle drei Vergleiche (Baseline-Selbstkonsistenz, Override-Selbstkonsistenz,
Baseline-vs-Override) sind **exakt identisch** — gleiche 38 Datei-URLs in
allen 4 Läufen, keine Abweichung. Eine bereits vorher bekannte, vom
Debounce unabhängige Paginierungs-Lücke (eine Sektion bleibt bei 17 von
tatsächlich mehr Zeilen hängen, `show all`-Klick ohne Wirkung) trat in
allen 4 Läufen identisch auf — kein neues Symptom, keine Verschlechterung
durch die Änderung.

Zeitersparnis: Ø-settle+stable sinkt von 3114ms auf 2192ms, **29,6 %** —
extrem nah an der vorab berechneten Schätzung (≈29 % von settle+stable,
≈150ms/Sektion: gemessen 922ms/6 Sektionen = 154ms/Sektion). Das ist die
Signatur, die Regel 2 verlangt: die Ersparnis trifft die Arithmetik-
Vorhersage fast exakt, was bestätigt, dass die 300ms-Konstante tatsächlich
die bindende Grenze war, kein Zufall in der Ø-326ms-Beobachtung aus Frage
13.

**Warum das trotzdem nicht "Frage 14 gelöst, Default ändern" bedeutet
(Regel 2, Umfang der Vorhersage ernst nehmen):** Die Vorhersage war
ausdrücklich auf Korrektheit *ohne Kontention* begrenzt — kleiner Kurs, 6
Sektionen, `course_concurrency=1`. Jeder historische Datenverlust dieser
Kampagne trat unter Kontention auf (großer Kurs, viele paginierte
Sektionen, teils `course_concurrency>1`) — genau der Fall, den dieser Lauf
nicht testet. Vier identische Läufe auf einem Kurs, an einem Tag, sind
außerdem wörtlich die Größenordnung, die der eigene 2026-07-21-Kommentar zu
`sectionContentPollIntervalMs` schon als unzureichend markiert hat ("three
clean runs are not enough to prove absence"). Frage 14 ist damit **für den
getesteten Fall (klein, seriell) mit Mechanismus beantwortet**, aber nicht
allgemein geschlossen — neue, schärfere Frage: Frage 15 oben (großer Kurs,
ggf. Concurrency).

---

## Vorheriges Experiment (Frage 13, abgeschlossen 2026-08-02)

**Frage:** (13, neu aus Frage 12) — wo sitzt die verbleibende, unerklärte
Settle-Zeit tatsächlich (CPU/Layout/Paint), wenn drei unabhängige Kandidaten
(Netzwerktransfer 24–31 %, Sektions-Dateizahl linear ~16–21 % Varianz,
Sektions-Dateizahl quadratisch ~29 % Varianz) alle nur Minderheiten
erklären? Dies ist der in Frage 7 und Frage 10 schon vorhergesagte
"nächste Schritt braucht echtes Browser-Profiling" — jetzt nicht mehr
optional, weil die einzige noch ungeprüfte Erklärungsklasse.

**Vorhersage (geschrieben vor dem Lauf):** `Performance.enable` +
`Performance.getMetrics()` (leichter als volles `Tracing.start`, ein
synchroner CDP-Call statt Stream-Auswertung) gemessen an der schon aus
Frage 11 bekannten langsamsten Sektion des kleinen Kurses ("Vorlesung",
44 Kandidaten) zeigt LayoutDuration+RecalcStyleDuration+ScriptDuration als
realen, aber nicht dominanten Anteil von settle+stable — geschätzt 20–40 %.

**Gescheitert ab / erfüllt ab:** >50 % = CPU dominant (Vorhersage falsch,
aber informativ); ~20–40 % = Vorhersage bestätigt, Debounce-Konstante wird
Hauptverdächtiger für den Rest; <10 % = starker Fall für "Zeit ist die
Konstante, nicht Arbeit".

**Ergebnis (2026-08-02, `opal-downloader-sync-speed`, dieser Zyklus,
Live-Lauf gegen den kleinen Kurs, 6 Sektionen,
`tmp/cdp-performance-metrics-probe.txt`): Vorhersage im wörtlichen Sinne
weder bestätigt noch klar widerlegt (11,4 % Aggregat, 14,5 % für die
langsamste Sektion — zwischen den beiden Schwellenwerten), aber eine
zusätzliche, ungeplante Auswertung derselben Daten liefert eine schärfere,
konvergente Erklärung.**

| Sektion | Kandidaten | settle+stable | Script+Layout+RecalcStyle | % davon | TaskDuration | % davon |
|---|---:|---:|---:|---:|---:|---:|
| Algorithmen (Root) | 12 | 482ms | 37.5ms | 7.8% | 83.6ms | 17.3% |
| Übungseinschreibung | 14 | 501ms | 90.3ms | 18.0% | 199.8ms | 39.9% |
| Materialien | 18 | 505ms | 63.8ms | 12.6% | 103.0ms | 20.4% |
| Probeklausur | 17 | 504ms | 35.1ms | 7.0% | 59.3ms | 11.8% |
| Übungsblätter | 27 | 532ms | 43.8ms | 8.2% | 88.5ms | 16.6% |
| Vorlesung | 44 | 588ms | 85.5ms | 14.5% | 226.8ms | 38.6% |
| **Summe/Aggregat** | | **3112ms** | **356.1ms** | **11.4%** | **761.0ms** | **24.4%** |

Die im Voraus benannte Metrik (Script+Layout+RecalcStyle) liegt bei 11,4 %
— knapp über der <10 %-Schwelle, klar unter dem vorhergesagten 20–40 %-Band.
`TaskDuration` (Chromes eigene, umfassendere Haupt-Thread-Beschäftigt-Zeit —
ein Superset, das Script/Layout/RecalcStyle **und** alles andere enthält,
was der Browser als "Task" auf dem Haupt-Thread zählt: GC, Parsing, Paint-
Vorbereitung, Compositing-Anmeldung) liegt bei 24,4 % — im vorhergesagten
Band, aber als **obere Schranke**, nicht als Bestätigung der spezifischen
Kandidat-C-Hypothese (quadratische `tr`-Mutation → Layout/Style-Recalc):
Script+Layout+RecalcStyle machen selbst von diesem großzügigsten Wert nur
weniger als die Hälfte aus (356 von 761ms) — der Rest von `TaskDuration` ist
unbenannte Haupt-Thread-Arbeit, kein bestätigter Mechanismus.

**Methodischer Vorbehalt, gegen die eigene Messung gerichtet:** das
CDP-Metrik-Fenster spannt sich über `visitSection` insgesamt (Navigation +
settle + stable), das `settleStable`-Nenner-Fenster nur über settle+stable.
Ein Teil der gemessenen CPU-Zeit fällt also vermutlich in die
Navigation/Initial-Parse-Phase, nicht in die settle+stable-Phase selbst —
die hier berichteten 11,4 %/24,4 % sind damit eher eine **Überschätzung**
des CPU-Anteils an settle+stable, nicht eine Unterschätzung. Das verschärft
den Befund, statt ihn zu relativieren.

**Was das für Regel 2 bedeutet — Mechanismus statt nur Zahl:** selbst mit
diesem Vorbehalt zugunsten von "mehr CPU" bleibt Browser-Arbeit (jede Form,
die CDP sehen kann) eine Minderheit. Zwei bereits im Code stehende
Konstanten erklären, wohin die Mehrheit sonst geht: `mutationObserverDebounceMs
= 300` (`navigation.go` Zeile 99) liegt fast exakt bei der gemessenen
durchschnittlichen settle-Zeit dieses Laufs (326ms, `section timing`-Log-
Zeile des Testlaufs) — 26ms Differenz, 8,7 % Abweichung. `sectionContentPollIntervalMs
= 150` (`crawl.go` Zeile 982) liegt in derselben Größenordnung wie die
gemessene durchschnittliche stable-Zeit (193ms, gut erklärt durch einen
einzelnen Poll-Zyklus plus etwas Overhead, wenn `initialStableReads`
niedrig ist). Das ist die Signatur, die Regel 2 verlangt: **die settle+stable-
Zeit sieht nicht nach variabler Render-Arbeit aus, sondern nach zwei festen
Warte-Konstanten, die zufällig fast die gesamte Zeit ausmachen** — CPU-Arbeit
ist real (11–24 %), aber sie sitzt *innerhalb* dieser Fenster, treibt sie
nicht.

**Kandidat B/C (Frage 7, Frage 11) damit endgültig als Nebenerklärung
eingeordnet, nicht mehr offen als "die noch fehlende Mehrheit":** vier
unabhängig geprüfte Erklärungen (Netzwerk 24–31 %, Dateizahl linear 16–21 %,
Dateizahl quadratisch 29 %, jetzt CPU 11–24 %) sind alle Minderheiten, aber
zwei bekannte, fest im Code stehende Wartekonstanten passen numerisch fast
exakt auf die verbleibende Zeit. Das ist ein Erklärungswechsel, kein
weiterer ausgeschlossener Kandidat: die Frage ist nicht mehr "was baut der
Browser da", sondern "sind 300ms/150ms die richtige Sicherheitsspanne, oder
mehr als nötig" — Frage 14 oben.

**Vorab geprüft (2026-08-02, Quellcode-Lesen, kein Live-Lauf, kein
Serverkontakt):** Playwright Go's eigene `Tracing`-API
(`playwright-go@v0.6100.0/tracing.go`) produziert nur den Playwright-
eigenen Trace Viewer (Screenshots/Snapshots/Netzwerk/Aktionen) — kein
CPU-/Layout-Profil im Chrome-DevTools-Sinn. Aber `BrowserContext.
NewCDPSession(page)` existiert bereits (`browser_context.go` Zeile 88,
`CDPSession.Send(method string, params map[string]any)
(any, error)`, `generated-interfaces.go` Zeile 630) — dieselbe rohe
CDP-Andockstelle, über die Frage 3 schon `Fetch.enable` gefunden hat, nur
diesmal selbst aufgerufen statt nur gelesen.

**Methode präzisiert (2026-08-02, vor dem Lauf):** volles `Tracing.start`
(Chrome-Trace-JSON, Stream-Events über `Tracing.dataCollected`/
`tracingComplete`, kein fertiger Parser im Projekt) ist mehr Werkzeug, als
die Frage braucht. `Performance.enable` + `Performance.getMetrics()` ist
ein leichteres Mitglied derselben CDP-Domain-Familie: ein einzelner
synchroner Call ohne Stream, liefert kumulative Sekundenzähler
(`ScriptDuration`, `LayoutDuration`, `RecalcStyleDuration`,
`TaskDuration` seit `enable`). Diff vor/nach einer Sektions-Navigation
beantwortet dieselbe qualitative Frage ("sitzt die Zeit in Skript, Layout,
Style-Recalc, oder in keinem davon") ohne Offline-Trace-Auswertung.

**Vorhersage:** Für die in Frage 11 bereits identifizierte langsamste
Sektion des *kleinen* Kurses ("Vorlesung", Algorithmen und Datenstrukturen,
44 Kandidaten, 533 Mutationen — kein dritter Crawl des großen Kurses an
einem Tag nötig) macht die Summe aus LayoutDuration + RecalcStyleDuration +
ScriptDuration einen realen, aber nicht dominanten Anteil der gemessenen
settle+stable-Zeit aus — geschätzt 20–40%, in derselben Größenordnung wie
die bereits gemessenen Kandidaten Netzwerk (24–31%, Frage 7) und
Sektions-Dateizahl (16–29%, Frage 10/12), nicht die fehlenden 70%+ allein
auffangend. Mechanismus: die in Frage 11 gefundene quadratische
`tr`-Mutation sollte, wenn sie echte Style-Recalcs/Layout-Passes auslöst,
in LayoutDuration/RecalcStyleDuration sichtbar sein — aber Browser-
Style-Recalc für ein paar Dutzend Attribut-Änderungen auf `tr`-Elementen
ist typischerweise Mikrosekunden- bis niedriger Millisekundenbereich,
nicht Hunderte ms.

**Scheitern-Kriterium (qualitativ, nicht nur ein Schwellenwert — Lehre aus
Frage 12s zu laschem Kriterium):**
- **>50%** der settle+stable-Zeit in Layout+RecalcStyle+Script: Vorhersage
  widerlegt, aber im guten Sinn — CPU-Arbeit ist der bisher übersehene
  dominante Treiber, Frage 13 schließt mit Mechanismus, neue Frage: welche
  der drei Metriken konkret und warum.
- **~20–40%** (Vorhersage bestätigt): eine reale, aber weitere Minderheits-
  Erklärung neben Netzwerk und Dateizahl — dann ist nach vier geprüften
  Kandidaten (Netzwerk, Dateizahl linear, Dateizahl quadratisch, jetzt CPU)
  keiner dominant, und die verbleibende Mehrheit der Zeit ist vermutlich
  **reine Wartezeit ohne messbare Browser-Arbeit** — der 300ms-Debounce in
  `waitForInteractiveLinks` selbst wird dann verdächtig, nicht mehr das,
  worauf er wartet.
- **<10%** (nahe Null): stärkster Fall für "die Zeit ist die
  Debounce-Konstante, nicht Arbeit" — nächste Frage wäre dann, ob die
  300ms-Konstante zu konservativ ist, nicht mehr, was in dieser Zeit läuft.

**Kosten:** niedriger als ursprünglich angenommen — kein Trace-Parsing,
nur eine CDP-Session pro Seite plus zwei `Performance.getMetrics()`-Calls
pro Sektion. Lauf gegen den bereits für Frage 11 benutzten kleinen Kurs
(6 Sektionen, keine neue Serverlast über das hinaus, was ein
Sektions-BFS-Walk ohnehin kostet), kein Diff gegen Ground-Truth nötig
(kein Sync-Verhalten geändert).

---

## Vorheriges Experiment (Frage 12, abgeschlossen 2026-08-02)

**Frage:** (12, neu aus Frage 11) — skaliert die settle+stable-**Wartezeit**
(nicht nur die Mutationszahl) mit der quadrierten Kandidatenzahl besser als
mit der linearen (Frage 10: r=0,40 linear) — und erklärt das den bisher
schwachen linearen Befund als ein quadratisches Verhältnis, das ein lineares
Modell unterschätzt?

**Vorhersage:** Ein erneuter Lauf der bereits bestehenden Frage-10-Probe
(`network_timing_probe_test.go`, `sectionProbe`-Hook, unverändert bis auf
eine zusätzliche Pearson-r-Berechnung gegen `candidates²`) gegen denselben
großen Kurs (Softwaretechnologie, 164 Sektionen) zeigt einen deutlich
höheren Pearson-r zwischen Kandidatenzahl² und settle+stable-Zeit als der
bereits gemessene r=0,40 für die lineare Kandidatenzahl.

**Gescheitert ab:** Wenn r für Kandidatenzahl² nicht spürbar über r=0,40
liegt (z. B. unter ~0,5 bleibt), erklärt der in Frage 11 gefundene
quadratische Mutations-Zusammenhang nicht die pro-Sektion-Wartezeit — dann
bleibt der Haupttreiber der Wartezeit weiterhin offen, und der nächste
Schritt ist das in Frage 7/10 schon vorhergesagte echte
Browser-Profiling (CPU/Layout/Paint), nicht mehr Zählen von Mutationen.

**Kosten:** Eine Zeile Ergänzung an der bestehenden Frage-10-Probe (Pearson-r
zusätzlich gegen `candidates²` statt nur `candidates`), ein Live-Lauf gegen
denselben großen Kurs wie Frage 10 — heute noch kein zweiter Crawl dieses
Kurses, also kein Grund, ihn zu vermeiden (`docs/server-load.md`: ein Crawl
pro Tag ist vernachlässigbar). Kein Diff gegen Ground-Truth nötig, da nichts
am Sync-Verhalten geändert wird.

**Ergebnis (2026-08-02, `opal-downloader-sync-speed`, dieser Zyklus,
direkt im Anschluss an Frage 11 mit derselben noch gültigen Session:
Vorhersage nicht bestätigt — nur eine geringe Verbesserung, kein
"deutlich höher".**

Live-Lauf gegen denselben Kurs wie Frage 10 (Softwaretechnologie, 164
Sektionen, `tmp/settle-timing-network-trace.txt`): linear r=0,46 (leicht
über dem archivierten 0,40 aus Frage 10 — Lauf-zu-Lauf-Streuung eines
realen Servers, keine Regression), quadratisch (Kandidatenzahl²) r=0,54.
r² (erklärte Varianz) steigt damit von 21 % auf 29 % — ein realer, aber
kleiner Unterschied, keiner, der die "schwache Korrelation" von Frage 10
erklärt.

**Ehrliche Einordnung des eigenen Scheitern-Kriteriums:** wörtlich genommen
("unter ~0,5 bleibt") ist das Kriterium mit r=0,54 knapp nicht erfüllt —
aber das Kriterium war zu lasch formuliert. Der Geist der Vorhersage war
"erklärt die schwache lineare Korrelation", nicht "liegt algebraisch über
0,5". Eine Verbesserung von 8 Prozentpunkten erklärter Varianz ist das
nicht. Lehre für künftige Kriterien in dieser Datei: ein Schwellenwert
allein reicht nicht, das Kriterium braucht auch eine qualitative Formulierung
("verdoppelt die erklärte Varianz" statt nur "über x").

**Was das für das Gesamtbild bedeutet:** drei unabhängig geprüfte Kandidaten
für die Settle-Zeit landen jetzt alle im selben Bereich "real, aber
Minderheit": Netzwerktransfer 24–31 % (Frage 7), Sektions-Dateizahl linear
r=0,40–0,46 / ~16–21 % Varianz (Frage 10, hier bestätigt), Sektions-
Dateizahl quadratisch r=0,54 / ~29 % Varianz (hier). Keiner davon ist
dominant. Der in Frage 7 und Frage 10 schon vorhergesagte nächste Schritt
ist damit nicht mehr optional, sondern die einzige noch ungeprüfte Klasse:
echtes Browser-Profiling (CPU/Layout/Paint) einer einzelnen Sektions-
Navigation, um zu sehen, wo die verbleibenden 70+ % der Zeit tatsächlich
sitzen.

---

## Vorheriges Experiment (Frage 11, abgeschlossen 2026-08-02)

**Frage:** (11, neu aus Frage 10) — was mutiert während des
~338ms-Settle-Fensters tatsächlich im DOM, wenn weder Netzwerktransfer
(24%) noch Sektions-Dateizahl (r=0,40, ~16% Varianz) die Mehrheit
erklären?

**Präzisiert (2026-08-01, nach Quellcode-Lesen statt Raten):** Der erste
Entwurf dieser Frage nannte "CPU-/Layout-Profiling" als nötiges nächstes
Werkzeug, ungeprüft teuer. `contentSettleWaitScript`
(`internal/scraper/navigation.go` Zeile ~452) zeigt den Mechanismus
direkt: ein `MutationObserver` auf dem Content-Root mit
`{childList, subtree, attributes, characterData}` — **jede** Mutation,
egal wie klein, setzt den Debounce-Timer zurück. Settle-Zeit misst also
nicht "wie lange bis der Inhalt fertig ist", sondern "wie lange bis
irgendwo im Root-Element gar nichts mehr passiert". Das ist direkt
beobachtbar, ohne CPU-Profiling: die Mutation-Records selbst mitschneiden
(Ziel-Element, Typ, `attributeName`) statt nur ihre Häufigkeit.

**Vorhersage:** Ein Live-Mitschnitt der Mutation-Records während echter
Sektions-Visits zeigt, dass die Mutationen auf wenige, eng begrenzte
Elemente konzentriert sind (z. B. ein wiederkehrendes Widget, ein
Attribut-Toggle, eine Live-Anzeige) — nicht breit verteilt über
Baum/Dateitabelle, die laut Frage 1/9 beim initialen Laden bereits
fertiges Server-HTML sind und keinen Grund zum Nachmutieren hätten.

**Gescheitert ab:** Wenn die Mutationen breit über viele verschiedene,
nicht wiederkehrende Elemente verteilt sind (kein klar abgrenzbarer
Verursacher erkennbar), ist die Hypothese eines engen Kandidat-C-Widgets
widerlegt — dann bleibt nur eine diffuse Erklärung, und erst dann wird
echtes CPU-/Layout-Profiling nötig, nicht vorher.

**Kosten:** Test-seitige Instrumentierung (Kopie von
`contentSettleWaitScript` mit Mutation-Logging statt nur Debounce), Live-
Lauf gegen wenige Sektionen des kleinen Kurses (Algorithmen, 6 Sektionen —
bewusst nicht wieder der große, dritter Live-Crawl desselben Kurses an
einem Tag wäre unnötige Serverlast, docs/server-load.md), keine
Produktionscode-Änderung nötig, kein neues Werkzeug.

**Ergebnis (2026-08-02, `opal-downloader-sync-speed`, dieser Zyklus):
Vorhersage teilweise bestätigt, aber mit einer Verschiebung, die Regel 2
noch nicht ganz erfüllt — Kandidat C in seiner engen Form widerlegt,
abgelöst durch eine schärfere Frage 12.**

`TestMutationConcentrationAcrossSections`
(`internal/scraper/mutationmarker_probe_test.go`) erweitert die bestehende
`mutationObserverInitScript`-Probe (bislang nur Root + eine Sektion, nur
letzte 8 Records von Hand gelesen) auf einen vollständigen BFS-Walk aller 6
Sektionen des kleinen Kurses, mit Aggregation aller Mutationen nach
Ziel-Element. Live-Lauf gegen den echten Account, `tmp/mutation-
concentration-probe.txt`:

| Sektion | Kandidaten | Mutationen | Mutationen/Kandidat |
|---|---:|---:|---:|
| Algorithmen u. Datenstrukturen (Root) | 12 | 43 | 3.58 |
| Übungseinschreibung | 14 | 52 | 3.71 |
| Probeklausur | 17 | 64 | 3.76 |
| Materialien | 18 | 84 | 4.67 |
| Übungsblätter | 27 | 164 | 6.07 |
| Vorlesung | 44 | 533 | 12.11 |

**Konzentration bestätigt:** über alle 6 Sektionen (940 Mutationen, 36
distinkte Element-Keys) tragen die Top-3-Keys 79,8 % bei — das erfüllt das
Scheitern-Kriterium nicht (keine diffuse Verteilung), die Vorhersage
"konzentriert auf wenige" hält.

**Aber die dominante Ursache widerspricht der Kandidat-C-Prämisse:** der
mit Abstand größte Key ist ein namenloses `tr` (70,2 % aller Mutationen,
Attribut-Mutationen direkt auf Datei-Tabellenzeilen ohne id/class) — nicht
ein "schmal begrenztes Widget außerhalb von Baum/Tabelle", wie Kandidat C
explizit forderte ("nicht Baum oder Tabelle selbst"). Die beiden 2026-07-30
per Hand vermuteten Kandidaten (`#veil`, Wicket-AJAX-Overlay; `#MathJax_
Message`) sind real, aber mit 0,9 % bzw. 1,3 % eine kleine Minderheit, nicht
die Erklärung — die damalige Vermutung entstand aus dem Lesen von nur 8
Tail-Records einer einzigen (noch dazu ungewöhnlich langsamen) Sektion, hier
widerlegt durch die vollständige Aggregation über 940 Records.

**Neuer, schärferer Befund (nicht Teil der ursprünglichen Vorhersage):** das
Verhältnis Mutationen/Kandidat ist nicht konstant, sondern wächst von 3,58
(12 Kandidaten) auf 12,11 (44 Kandidaten) — ein 3,4-facher Anstieg der
Rate bei nur 3,7-fach mehr Kandidaten. Regression über die 6 Punkte:
Kandidatenzahl vs. Mutationen linear r=0,976, Kandidatenzahl² vs.
Mutationen r=0,997, log-log-Exponent 1,96 (r=0,993) — die Daten passen
deutlich besser zu einer quadratischen als zu einer linearen Beziehung.
Das ist die konkrete, prüfbare Signatur, die Regel 2 verlangt: irgendetwas
touched Datei-Tabellenzeilen mit einem Gesamtaufwand, der eher mit
Zeilenzahl² als mit Zeilenzahl wächst — z. B. eine paarweise Zeilen-
Vergleichsoperation (Duplikat-/Sortier-/Highlight-Logik), nicht eine reine
Pro-Zeile-Initialisierung. Welches konkrete Attribut auf `tr` wechselt
(nur der Tag wurde aggregiert, nicht `attr`/`attrVal`) ist noch nicht
untersucht.

**Warum Frage 11 trotzdem nicht einfach geschlossen ist:** Kandidat C ist in
seiner ursprünglichen, engen Form ("Widget außerhalb von Baum/Tabelle")
widerlegt, mit Mechanismus (die Aggregation zeigt klar: es ist die Tabelle
selbst). Aber das ist noch keine vollständige Erklärung für die
Settle-Zeit — nur für die Mutations*zahl*. Ob der quadratische
Mutations-Befund auch die tatsächliche Wartezeit erklärt (die eigentliche
Zielgröße, nicht nur ein Proxy dafür), ist ungeprüft und genau die neue
Frage 12 oben.

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

### 2026-08-02 (opal-downloader-sync-speed): erster Bericht dieser Aufgabe, Frage 11 geschlossen mit Verschiebung

Der erste Bericht **dieser** geplanten Aufgabe (`opal-downloader-sync-speed`)
— frühere Zyklen (Frage 3, 7-Bau, 7-Live, 9, 10, 11) liefen ohne einen. Fällig
seit mehr als 5 Zyklen.

**Bekannt seit dem letzten (nicht existenten) Bericht, also seit Kampagnenstart
2026-07-31:** `ctx.Route` kostet ~30 % (CDP-Pause/Resume, unvermeidbar,
Frage 3 geschlossen). Der Baum-Fragment-Anteil einer Sektionsseite ist auf
offene Knoten + Selektionspfad begrenzt, nicht auf die Kursgröße
(`MenuTreeRenderer.isRenderChildren`, Frage 9 geschlossen). Netzwerktransfer
erklärt nur 24–31 % der Settle-Zeit (Frage 7). Sektions-Dateizahl erklärt sie
linear nur schwach (r=0,40, Frage 10). Heute (Frage 11): DOM-Mutationen
während der Settle-Zeit sind konzentriert (Top-3-Elemente = 79,8 %), aber die
Ursache ist die Datei-Tabelle selbst (`tr`, 70 %), nicht ein externes Widget
— und die Mutationszahl wächst mit dem Quadrat der Zeilenzahl (Exponent
≈1,96, r=0,997), nicht linear.

**Was das für den Zustand des Modells bedeutet:** vier von fünf geprüften
Erklärungen für die Settle-Zeit sind jetzt einzeln entweder geschlossen
(Frage 3, 9) oder als Nebenerklärung quantifiziert und verworfen (Frage 7:
24–31 %; Frage 10: r=0,40). Frage 11 liefert zum ersten Mal eine Erklärung,
die stark genug aussieht, um dominant zu sein (quadratisches Wachstum statt
schwacher linearer Korrelation) — aber sie ist bisher nur für die
Mutations*zahl* gezeigt, nicht für die tatsächliche Wartezeit. Das ist Frage
12, bereits als nächstes Experiment aufgesetzt.

**Noch offen:** Frage 12 (skaliert die Wartezeit selbst quadratisch mit der
Zeilenzahl?), Frage 8 (Cache-Aus vs. Pause/Resume-Anteil an den 30 % aus
Frage 3, lokal ohne Account reproduzierbar, noch nie angefasst), Frage 5 (ist
"30s" überhaupt an Discovery gebunden, oder löst Hintergrundlauf/
Teilergebnisse das eigentliche Ziel "fühlt sich wie ein Klick an" ohne
schnellere Discovery?), Frage 6 (1 von 12 Sektionen bleibt über Läufe hinweg
instabil, ungeklärt warum).

**Empfehlung: weitermachen.** Zum ersten Mal seit Kampagnenbeginn gibt es eine
Erklärung mit einer klaren quantitativen Signatur (quadratisch, nicht nur
"nicht Netzwerk, nicht Dateizahl linear") statt einer Liste ausgeschlossener
Kandidaten. Frage 12 entscheidet in einem einzigen billigen Lauf (keine neue
Instrumentierung, nur eine zusätzliche Korrelation auf bereits vorhandenem
Code), ob das die Wartezeit selbst erklärt oder nicht — in beiden Fällen ein
scharfes Ergebnis, kein weiterer Rateversuch.

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
