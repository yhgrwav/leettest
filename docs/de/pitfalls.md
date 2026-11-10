# Welche Lasttest-Probleme behebt es?

[← Zur Übersicht](README.md)

> Übersetzt aus [docs/ru/pitfalls.md](../ru/pitfalls.md) bei 0e32d00, 2026-11-10. Bei
> Abweichungen gilt die russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/pitfalls.md).

Eine Liste bekannter Messfehler und Unannehmlichkeiten beim Lasttest und dessen, was LeetTest damit
tut. Welches Werkzeug welche Eigenheit hat, steht mit Quellen im
[Vergleich](why.md#vergleich-mit-ghz-k6-und-jmeter); hier sind keine Werkzeuge genannt.

Status: `fertig` — funktioniert; `geplant` — die Entscheidung steht, der Code kommt noch.

| Problem | Status |
|---|---|
| Coordinated Omission: Stillstände des Dienstes verschwinden aus dem Bericht | fertig |
| Eine Methode pro Lauf: gemischte Last lässt sich nicht ausdrücken | fertig |
| Closed Model senkt die Last genau dann, wenn der Dienst degradiert | fertig |
| Ein Timeout wird als Zahl erfasst und verdeckt das Ende der Verteilung | fertig |
| Eine Verbindungsablehnung in Bruchteilen einer Millisekunde „verbessert" die Latenz | fertig |
| Perzentile werden gemittelt und verlieren ihren Sinn | fertig |
| Metriken bremsen den Generator selbst | fertig |
| `.proto`-Dateien und Codegen nötig | fertig |
| Ein Tippfehler in der Konfiguration ergibt einen leeren grünen Lauf | fertig |
| Kaltstart verzerrt Perzentile | fertig |
| Der Generator stirbt vor dem Ziel an hängenden Anfragen | fertig |
| Der Generator stößt an seine Grenze, und der Bericht beschuldigt den Dienst | fertig |
| Rate-Limits des Ziels zählen als Dienstfehler | fertig |
| Aufrufe lassen sich nicht über Daten verketten | geplant |
| Unklar, was während des Laufs passiert | fertig |

## Coordinated Omission

Ein Fehler, bei dem ein Stillstand des Dienstes aus dem Bericht verschwindet. Ziel sind 1000 RPS,
eine Anfrage pro Millisekunde. Der Dienst hängt eine Sekunde. In dieser Sekunde hätten 1000
Anfragen rausgehen müssen.

Zählt man die Latenz ab dem tatsächlichen Senden, gehen alle nach der Erholung raus, brauchen je
5 ms, und der Bericht zeigt `p99 = 5ms`. Der einsekündige Stillstand ist im Bericht nicht zu sehen
(das Beispiel ist errechnet, keine Messung; eine Messung auf dem Stand steht im
[Vergleich mit ghz](compare-ghz.md)).

Wir geben jeder Anfrage beim Planen ihren geplanten Zeitpunkt und zählen die Latenz ab diesem. Eine
für `t=0` geplante Anfrage ging bei `t=1000ms` raus, die Antwort kam bei `t=1005ms` — Latenz
`1005ms`. Der geplante Zeitpunkt und das Zählen ab ihm gehören zum Kern von LeetTest.

## Eine Methode pro Lauf

Produktion ist nicht eine Methode. Ein Dienst, der getrennt 800 RPS Lesen und 50 RPS Schreiben
hält, kann an ihrer Summe kippen — gemeinsamer Verbindungspool, Sperren, Cache-Konkurrenz.
Getrennte Läufe zeigen das nie. Bei uns eine Liste von Methoden mit eigener RPS — ein Lauf, ein
Bericht.

## Closed Model

„N virtuelle Nutzer, jeder wartet auf eine Antwort vor der nächsten Anfrage" wirkt natürlich, hat
aber eine Eigenschaft: Wird der Dienst langsam, warten die Nutzer länger, und die tatsächliche Last
sinkt von selbst. So passt das Modell zur Frage „wie viele Nutzer bedient der Dienst", aber nicht zur
Frage „was passiert bei einer vorgegebenen Anfragerate".

Wir arbeiten mit einem Open Model: Die Ziel-RPS ist ein Zeitplan, Anfragen gehen nach der Uhr raus,
ob die vorigen zurück sind oder nicht.

## Timeouts und Verbindungsfehler

Eine nach zwei Sekunden abgebrochene Anfrage sagt eines: Sie dauerte **mehr** als zwei Sekunden.
Genau zwei zu erfassen unterschätzt das Ende; sie wegzuwerfen tut so, als hätte es sie nie
gegeben. Wir setzen keine Zahl ein: Könnten abgebrochene Anfragen den Platz eines Perzentils
einnehmen, druckt der Bericht eine untere Schranke, `p99 >2.0s`. Um das Ende zu sehen, den Timeout
erhöhen.

Eine abgelehnte Verbindung kommt in Bruchteilen einer Millisekunde zurück. Als Messung erfasst,
würde sie die Latenz nach unten ziehen — der Dienst liegt, und der Bericht zeigt eine
Beschleunigung. Solche Anfragen zählen als Fehler, bleiben aber aus den Perzentilen.

## Perzentile und Metriken

Der Mittelwert zweier p99 ist kein p99 — sie dürfen nicht gemittelt werden. Addieren lassen sich
nur Verteilungen, daher wird die Zusammenfassung über Methoden aus zusammengeführten Histogrammen
berechnet, nicht aus fertigen Perzentilen.

Dazu die Kosten der Metrikerfassung. Ein Mutex um ein gemeinsames Latenz-Slice macht den Generator
bei hoher RPS zu seinem eigenen Engpass. Wir schreiben in HDR-Histogramme: ein Schreibvorgang
kostet einige zehn Nanosekunden (eine Schätzung; die Benchmarks des Pakets `pkg/metrics` liegen im
Repository, eine eigene Auswertung für 5000 RPS gibt es nicht).

## Vorbereitung und Tippfehler

Kein `.proto`, kein Codegen: Methodenbeschreibungen kommen per Reflection vom Dienst.

Die Konfiguration wird im strikten Modus gelesen. `rsp: 800` statt `rps: 800` ist ein Fehler mit
Ortsangabe, kein stiller Lauf mit null Last und grünem Bericht. Auch `rps` gleich null ist ein
Fehler: Ein leerer erfolgreicher Lauf sieht wie Erfolg aus, obwohl er nichts geprüft hat.

## Aufwärmen

Die ersten Sekunden eines Laufs sind immer langsam: leere Caches, Verbindungsaufbau, JIT-Aufwärmen.
Sie verzerren die Perzentile des ganzen Laufs, daher hält `warmup` sie aus den Perzentilen heraus.

## Tod des Generators

Die Kehrseite des Open Model: Steht das Ziel, stauen sich hängende Anfragen, und der Generator
stirbt vor dem getesteten Dienst. Abhilfe ist eine explizite Obergrenze gleichzeitiger Anfragen.
Die Last an der Grenze still zu senken ist nicht erlaubt: Ein „1000 RPS"-Lauf, der still zu
„so viel es eben ging" wurde, verbirgt, dass die Last nicht die richtige war. Deshalb wird die Grenze vor dem
Start geprüft: Sie fasst alles, was ein hängender Dienst bis zum Timeout festhalten kann, mit
Reserve. Ein hängender Dienst läuft bis zum Ende des Laufs, und der Bericht stellt das Gemessene
fest: `at 100 rps, 150 of 150 calls (100.0%) got no answer within 300ms, and the target answered
nothing at all`. Ist die Grenze trotzdem erschöpft, hat der Generator die Plätze gehalten, nicht
der Dienst: Der Lauf hält mit Bericht an und wird als ungültig markiert.

## Der Generator stößt an seine Grenze

Schafft die Maschine die angeforderte RPS nicht, gehen Anfragen verspätet raus, die Latenz steigt
— und der Bericht beschuldigt den Dienst, obwohl der Generator schuld ist. Die Verspätung des
Generators messen wir bereits getrennt von der Antwortzeit des Dienstes und heben sie zu einem
Urteil: Ein Lauf, in dem die Verspätung des Generators das p99-Ende bestimmt hat, wird für ungültig
erklärt (`generator_late`, Exit-Code 2), und der Bericht nennt die Methoden und was zu ändern ist.
Noch nicht veröffentlicht: Der Bericht nennt die Grenze, an die der Generator stieß (GOMAXPROCS,
CPU-Quote, die CPU selbst oder der Zeitplan), oder sagt, dass er sie nicht nennen kann; der Lauf ist
auch dann ungültig, wenn der Generator 1 % oder mehr der Aufrufe einer Methode nicht gesendet hat,
selbst wenn die Ursache des Endes die Verbindung ist. Solange der Lauf geht, warnt eine Zeile auf
stderr, dass der Generator hinter seinem Plan zurückbleibt. Einzelheiten stehen in der
[Referenz](reference.md#wo-man-es-ausführt).

## Fehler nach Kategorien

Ein `RESOURCE_EXHAUSTED` vom Rate-Limiter des Ziels ist nicht dasselbe wie ein Timeout oder ein
Absturz des Dienstes. Alles in „Fehler: 12%" zu werfen, verliert den Sinn. Der Bericht teilt die
Aufrufe nach Kategorien auf, je Methode und je Sekunde: eine Ablehnung „überlastet" (`overload`)
getrennt von einem Timeout (`timed_out`) und einem Fehler des Ziels (`failure`) —
[Kategorien](../en/report.md#reading-the-report).

## Sichtbarkeit

Ein Lauf darf keine zehnminütige Blackbox sein. Während er läuft, sieht man aktuelle RPS, laufende
Anfragen, Fehleranteil und Perzentile — genug, um einen offensichtlich sinnlosen Lauf in der
zweiten Minute abzubrechen statt in der zehnten.
