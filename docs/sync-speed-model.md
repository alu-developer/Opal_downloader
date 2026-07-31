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

### 1. Was rendert OPAL da eigentlich? — der Quellcode wurde nie gelesen
OpenOLAT ist Open Source. Diese Kampagne hat zehn Tage lang am lebenden
Server geraten, was er tut. Dort direkt beantwortbar:
- Gibt es einen **Fertig-Marker** beim Aufbau der Dateitabelle (CSS-Klasse,
  Attribut, JS-Callback, Wicket-Komponente)? Die Kampagne hat "es gibt
  keinen" aus Abwesenheit im DOM geschlossen — das ist nicht dasselbe wie
  nachgelesen.
- Gibt es eine **andere View auf dieselbe Dateiliste** ohne gestaffeltes
  Client-Rendering (Druck-, Export-, Feed-, WebDAV-, Mobil-Ansicht)?
- Wie funktioniert der **Pager serverseitig** — gibt es einen Parameter
  statt des Klicks?

### 2. Warum war HTTP auf 2 von 6 Kursen leer?
"Manche Bausteine rendern server-, manche client-seitig" ist die
Beschreibung, nicht die Ursache. Welcher Bausteintyp, und woran ist er in
der Antwort erkennbar? Vermutlich beantwortet durch (1). Dieser Ansatz war
der schnellste, den es je gab (22s) — er wurde an Tag 1 verworfen, ohne dass
je jemand die Ursache diagnostiziert hat.

### 3. Warum kostet `ctx.Route` 30%?
Ungeklärt — und es entwertet jede Zahl, die mit installierter Route gemessen
wurde. Playwright-seitig, nicht OPAL-seitig; also lokal reproduzierbar ohne
Account.

### 4. Was passiert in den 336ms wirklich?
Bisher als Blackbox behandelt: gewartet, bis Ruhe ist. Nie im Browser
profiliert, was in dieser Zeit läuft. Wenn es z.B. Layout-Thrashing oder ein
Skript ist, das auf einen Timer wartet, ist das eine ganz andere Ursache als
"die Seite braucht das".

### 5. Ist "30s" überhaupt an Discovery gebunden?
Das Ziel ist *"fühlt sich wie ein Klick an"*. Nie geprüft: Hintergrundlauf
vor dem Klick, Teilergebnisse während des Laufs, geänderte Kurse zuerst.
Diese Klasse braucht keine schnellere Discovery, sondern eine, die nicht
vor dem Nutzer steht.

### 6. Warum bleibt 1 von 12 Sektionen über Läufe hinweg instabil?
Der Rest wurde auf Wicket-Bookkeeping zurückgeführt. Dieser eine nicht.

---

## Nächstes Experiment

**Frage:** (1) — gibt es im OpenOLAT-Quellcode einen Fertig-Marker oder eine
alternative Dateilisten-View?

**Vorhersage:** In OpenOLAT existiert für die Dateitabelle
(`VFSItemTable`/FolderRunController) eine identifizierbare fertige
DOM-Struktur; wahrscheinlich (60%) gibt es eine Tabellen-Zustandsklasse oder
ein Wicket-Behavior, das erst nach vollständigem Aufbau gesetzt wird.

**Gescheitert ab:** Wenn nach Lesen der einschlägigen Klassen und Templates
kein Marker und keine alternative View benennbar ist, die sich am Live-System
prüfen lässt. Dann ist "es gibt kein positives Signal" zum ersten Mal
*nachgelesen* statt geschlossen — und Frage (4) rückt auf.

**Kosten:** Lesen, kein Account, kein Lauf. Billigstes Experiment der Liste
bei größtem Hebel.

**Ergebnis:** _(offen)_

---

## Berichte

Alle 5 Zyklen, jeder endet mit einer Empfehlung: weitermachen oder aufhören,
und warum. Die Stopp-Entscheidung trifft der Maintainer, nicht ein Zähler —
kein Deckel auf die Kampagne, das Kill-Kriterium sitzt pro Experiment
(Entscheidung vom 2026-07-31, Gegenargumente in derselben Sitzung notiert:
jede Abbruchbedingung, die dieses Repo je hatte, wurde zu dem, woran die
Arbeit aufhörte).

_(noch keiner)_
