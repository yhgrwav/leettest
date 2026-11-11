# Welche Fragen beantwortet LeetTest?

[← Zur Übersicht](README.md)

> Übersetzt aus [docs/ru/questions.md](../ru/questions.md) bei feb2d1d, 2026-11-10. Bei Abweichungen gilt die
> russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/questions.md).

Die Fragen, die der Bericht schon jetzt beantwortet.

| Frage | Was der Bericht zeigt | Wo |
|---|---|---|
| Bei welcher Last kommt der Dienst nicht mehr zurecht? | Suche nach dem Bruchpunkt: auf welcher Stufe das Ziel durchhielt, auf welcher es brach und warum — Fehler, p99, die Verbindung, keine Rückkehr zum früheren p99 | [Suche nach dem Bruchpunkt](../en/report.md#breaking-point-search) |
| Wie antwortet jede Methode unter einer Last, die der echten ähnelt? | Mehrere Methoden gleichzeitig, jede mit eigener RPS; p50, p90, p95, p99 für jede | [Last](../en/guide/load.md) |
| Was sieht ein Nutzer, wenn der Dienst stockt oder langsamer wird? | Die Latenz wird ab dem geplanten Zeitpunkt des Aufrufs gezählt: Ein Stillstand des Ziels landet im p99 | [Den Bericht lesen](../en/report.md#reading-the-report) |
| Welche Fehler, und von wem? | Kategorien: falsche Anfrage, Überlastung, Ausfall, Abbruch, Zeitüberschreitung; der Status kam vom Ziel (oder einem Proxy) oder wurde vom Client gesetzt | [Den Bericht lesen](../en/report.md#reading-the-report) |
| Wie viele Aufrufe blieben ohne Antwort, und warum? | Nicht gesendet, abgebrochen oder Zeitüberschreitung — getrennt; die Sekunde, ab der das Ziel nicht mehr antwortete | [Den Bericht lesen](../en/report.md#reading-the-report) |
| Sind alle Backends hinter dem Balancer belastet? | `app.connections: N`: eine Zeile je Verbindung — Adresse, Aufrufe, Anteil der Fehlschläge, p99 | [Den Bericht lesen](../en/report.md#reading-the-report) |
| Kann man der Messung selbst trauen? | Ein Urteil über Wartezeiten auf der Client-Seite und das p99 ohne sie; ein ungültiger Lauf (Exit-Code 2), wenn die Zahlen nicht das Ziel beschreiben | [Wann ein Lauf ungültig ist](../en/report.md#when-a-run-is-invalid) |
| Wie verhält sich der Dienst bei unterschiedlichen Daten? | `dataset`: eine eigene Anfrage für jeden Aufruf, ein JSON je Zeile der Datei | [Referenz](reference.md#bei-jedem-aufruf-eine-andere-anfrage-dataset) |
| Lässt es sich in CI ausführen? | Ein JSON-Objekt mit stabilem Schema und Exit-Codes; `pass`-Grenzen entscheiden „hält / hält nicht“ mit Exit-Code `4` | [JSON für Skripte und CI](../en/report.md#json-for-scripts-and-ci) |

LeetTest sieht nur die Client-Seite: Wie viele Ressourcen das Ziel hat, weiß es nicht und errät es
nicht. Was als Nächstes kommt, steht in der [Roadmap](roadmap.md).
