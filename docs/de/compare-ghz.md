# Ein Lasttest kann Sie belügen: LeetTest und ghz auf demselben Stand

> Übersetzt aus [docs/ru/compare-ghz.md](../ru/compare-ghz.md) bei feb2d1d, 2026-11-10. Bei
> Abweichungen gilt die russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/compare-ghz.md).

Diese Seite vergleicht LeetTest mit [ghz](https://ghz.sh) auf einem Testserver mit bekanntem
Verhalten. Die Frage ist eng: Wenn der Server stockt oder langsamer wird, zeigt der Bericht das?
Das Lastmodell eines Werkzeugs (geschlossen oder offen) ändert die Antwort, deshalb ist jeder
ghz-Modus eine eigene Zeile. Jeder Versuch unten nennt die Frage, die genauen Flags, die
angeforderte und die gelieferte Last und die Grenzen. Ein Funktionsvergleich mit ghz, k6 und JMeter steht in der [Tabelle](why.md#vergleich-mit-ghz-k6-und-jmeter).

Kurz:

- **Wenn sich der Server normal verhält**, melden beide Werkzeuge Latenzen innerhalb von 1 ms (Modus A).
- **Wenn der Server 2 Sekunden einfriert** (Modus B1), meldet ghz im synchronen Modus (Standard)
  ein p99 von 11.4ms mit `-c 10` und 11.6ms mit dem Standard `-c 50`. LeetTest meldet 1.71s.
  ghz mit `--async` meldet ebenfalls 1.71s.
- **Wenn der Server langsamer ist, als die Last verlangt** (Modus B2), sendet ghz im synchronen
  Modus (Standard) mit `-c 10` 2970 der 6000 angeforderten Aufrufe. Seine Latenzen stimmen für die
  gesendeten Aufrufe, aber das ist die halbe angeforderte Last. Mit dem Standard `-c 50` sendet es
  alle 6000.

Das ist kein Fehler in ghz. Der synchrone Modus betreibt eine feste Zahl von Workern (`-c`,
standardmäßig 50 [1]). Jeder Worker wartet, bis sein Aufruf fertig ist, bevor er den nächsten nimmt
[2]: ein Closed Model, so gewollt. Stockt der Server, warten auch die Worker, und Aufrufe, die
während des Stockens hätten rausgehen sollen, gehen nicht rechtzeitig raus. Ihre Verzögerung wird
also nie gemessen. Dieser Effekt heißt *Coordinated Omission*. Mit `--async` wartet ghz nicht auf
das Ende eines Aufrufs vor dem nächsten [2][3]: ein Open Model. Das ergibt dieselben Zahlen wie
LeetTest. LeetTest hat nur das Open Model.

Alles, was hier über ghz gesagt wird, wurde gegen v0.121.0 geprüft — Quellen am Ende der Seite.

## Aufbau

Der Stand ist `test/stand` in diesem Repository: ein gRPC-Server, der `grpc.health.v1.Health/Check`
nach einer festgelegten Verzögerung beantwortet. Er kann für eine festgelegte Zeit einfrieren oder
langsamer antworten.

| Modus | Stand | was er nachbildet |
|---|---|---|
| A | `-delay 10ms` | Kontrolle: ein Server, der mithält |
| B1 | `-delay 10ms -freeze-at 10s -freeze-for 2s` | ein 2-Sekunden-Stillstand 10s nach Beginn (eine GC-Pause, eine Sperre) |
| B2 | `-delay 100ms` | ein Server, der langsamer ist, als die Last verlangt |

Last in jedem Lauf:

- 200 Aufrufe pro Sekunde für 30 Sekunden, also 6000 Aufrufe;
- ein Timeout von 5s;
- eine Verbindung.

Die ghz-Flags im synchronen Modus (ein geschlossenes Modell):

```
ghz --insecure --call grpc.health.v1.Health/Check -d '{}' --rps 200 -z 30s -t 5s \
    --duration-stop wait --connections 1 -c 10
```

Die ghz-Flags mit `--async` (ein offenes Modell):

```
ghz --insecure --call grpc.health.v1.Health/Check -d '{}' --rps 200 -z 30s -t 5s \
    --duration-stop wait --connections 1 -c 10 --async
```

Synchrones ghz läuft mit `-c 10` und mit dem ghz-Standard `-c 50`. `--duration-stop wait` lässt
Aufrufe, die bei 30s noch unterwegs sind, zu Ende laufen und zählen.

Jedes Werkzeug lief 5 Mal in jedem Modus, und jeder Lauf bekam einen frischen Stand. Die Reihenfolge
rotiert, sodass kein Werkzeug immer zuerst läuft. Die Tabellen zeigen den Median der 5 Läufe, mit
Minimum und Maximum in Klammern.

Umgebung:

- LeetTest bei Commit `fb8c7b7`;
- ghz v0.121.0 (grpc-go v1.56.3);
- Go 1.27.1;
- Linux (Kernel 6.18) in einem Container `golang:1.27.1` mit `--cpus=4`; Docker Desktop unter
  Windows 11, also Linux in einer WSL2-virtuellen Maschine auf einem Windows-Host;
- Host-Prozessor AMD Ryzen 9 5900X (12 Kerne, 24 Threads), 16 GB RAM in der virtuellen Maschine;
- Werkzeuge und Stand im selben Container, über Loopback.

## Ergebnisse

### B1: ein 2-Sekunden-Stillstand

**Frage:** zeigt der Bericht einen Server, der in der 10. Sekunde des Laufs 2 Sekunden stillsteht?
**Stand:** `-delay 10ms -freeze-at 10s -freeze-for 2s`.
**Werkzeuge und Modell:** LeetTest (`rps: 200`, `duration: 30s`, `timeout: 5s`; offen); ghz ohne
`--async` mit `-c 10` und mit dem Standard `-c 50` (geschlossen); ghz mit `--async` (offen). Die
Flags stehen im Abschnitt «Aufbau».
**Angefordert / geliefert:** 200 Aufrufe pro Sekunde, 6000 pro Lauf; gesendet wurden in allen Zeilen
5999–6000, also etwa 200 pro Sekunde.

**Grenzen des Versuchs:** ein Host, Loopback, ein Container mit 4 Kernen, eine ghz-Version (v0.121.0),
eine Art von Ausfall (ein Stillstand von 2 Sekunden). Der Versuch zeigt nicht, wie sich andere
ghz-Versionen, andere Stillstandsdauern und ein Server über das Netz verhalten.

| Werkzeug | gesendet | fehlgeschlagen | p50 | p90 | p95 | p99 | Aufrufe über 1s |
|---|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 10.4ms [10.3–10.4] | 10.6ms [10.5–10.6] | 509ms [508–509] | 1.71s [1.71–1.71] | nicht ausgegeben |
| ghz, sync (`-c 10`) | 5999 [5999–6004] | 0 | 10.7ms [10.7–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.3–11.4] | 11.4ms [11.4–11.5] | 10 [10–10] |
| ghz, sync (`-c 50`, Standard) | 5999 [5999–6000] | 0 | 10.7ms [10.7–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.4–11.4] | 11.6ms [11.5–12.3] | 50 [50–50] |
| ghz, `--async` | 6000 [5999–6000] | 0 | 10.7ms [10.7–10.7] | 11.4ms [11.4–11.4] | 512ms [506–518] | 1.71s [1.71–1.72] | 202 [202–203] |

In 2 Sekunden bei 200 RPS waren etwa 400 Aufrufe fällig. Mit `--async` zählt ghz 202 Aufrufe über
1s: Die in der ersten Hälfte des Stillstands fälligen warten länger als eine Sekunde. Im synchronen
Modus liegen 10 Aufrufe über 1s mit `-c 10` und 50 mit `-c 50`, einer pro Worker. Das sind 0.17 %
und 0.83 % der Aufrufe, zu wenig, um p99 zu erreichen. Die Worker standen während des Stillstands
still. Als er endete, schickte ghz die verpassten Aufrufe auf einmal: Hinter dem Plan wartet sein
Taktgeber nicht vor dem nächsten Aufruf [4]. Diese Aufrufe trafen auf einen Server, der schon wieder
schnell war. Ihre Wartezeit vor dem Senden gehört nicht zur Latenz, die ghz meldet: Sie zählt ab dem
Beginn des Aufrufs selbst [5].

Die Zahl der gesendeten Aufrufe ist in allen Zeilen gleich, die Zählung verrät den Stillstand also
nicht.

### B2: ein Server, langsamer als die Last verlangt

**Frage:** welche Last kam wirklich beim Server an, der in 100 ms antwortet, bei angeforderten 200
Aufrufen pro Sekunde?
**Stand:** `-delay 100ms`.
**Werkzeuge und Modell:** LeetTest (offen); ghz ohne `--async` mit `-c 10` und mit dem Standard
`-c 50` (geschlossen). ghz mit `--async` wurde in diesem Modus nicht gestartet.
**Angefordert / geliefert:** 200 pro Sekunde, 6000 pro Lauf. Gesendet: LeetTest 6000, ghz `-c 10`
2970 (etwa 99 pro Sekunde), ghz `-c 50` 6000 (Median; 5999–6001).

**Grenzen des Versuchs:** ein Host, Loopback, eine ghz-Version, eine Serverlatenz (100 ms). Das
Ergebnis von ghz hängt von `-c` ab: Bei `-c 50` liegt die Decke über der angeforderten Last, und die
gesamte Last geht raus. Der Versuch zeigt nicht, welches `-c` ghz-Nutzer in der Praxis wählen würden.

| Werkzeug | gesendet | fehlgeschlagen | p50 | p90 | p95 | p99 |
|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 100ms [100–100] | 101ms [101–101] | 101ms [101–101] | 102ms [101–102] |
| ghz, sync (`-c 10`) | 2970 [2970–2970] | 0 | 100.9ms [100.9–101] | 101.3ms [101.3–101.4] | 101.4ms [101.4–101.5] | 101.6ms [101.6–101.7] |
| ghz, sync (`-c 50`, Standard) | 6000 [5999–6001] | 0 | 100.5ms [100.5–100.6] | 101.3ms [101.3–101.4] | 101.4ms [101.3–101.5] | 101.5ms [101.4–101.7] |

Synchrones ghz sendet höchstens c / Latenz Aufrufe pro Sekunde, wobei c die Zahl der Worker ist.
Mit `-c 10` und 100ms sind das 100 Aufrufe pro Sekunde, die Hälfte der angeforderten 200. Die
Latenzen der gesendeten Aufrufe sind echt, aber es ging nur die Hälfte der angeforderten Last raus.
Wer nur die Latenzspalten liest, sieht einen Server, der 200 RPS bei 101ms schafft; beim Server
kamen 100 RPS an.

Mit dem Standard `-c 50` liegt die Decke bei 500 Aufrufen pro Sekunde, und die volle Last geht
raus. Dieselbe Decke gilt für jedes c, sobald der Server langsam genug wird: Bei `-c 50` wird sie
oberhalb von 250ms erreicht.

### A: Kontrolle

**Frage:** stimmen die Werkzeuge überein, wenn mit dem Server alles in Ordnung ist?
**Stand:** `-delay 10ms`.
**Werkzeuge und Modell:** LeetTest (offen); ghz ohne `--async` mit `-c 10` (geschlossen); ghz mit
`--async` (offen).
**Angefordert / geliefert:** 200 pro Sekunde, 6000 pro Lauf; gesendet 6000–6001 (Median).

**Grenzen des Versuchs:** ein Host, Loopback, eine ghz-Version, ein Server ohne Ausfälle. Den
Unterschied von 0.3–0.9 ms zwischen ghz und LeetTest haben wir nicht untersucht; der Versuch zeigt
seine Ursache nicht.

| Werkzeug | gesendet | fehlgeschlagen | p50 | p90 | p95 | p99 |
|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 10.3ms [10.3–10.4] | 10.5ms [10.5–10.6] | 10.5ms [10.5–10.6] | 10.6ms [10.6–10.8] |
| ghz, sync (`-c 10`) | 6000 [6000–6008] | 0 | 10.6ms [10.6–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.3–11.4] | 11.4ms [11.4–11.5] |
| ghz, `--async` | 6001 [5996–6002] | 0 | 10.7ms [10.7–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.4–11.4] | 11.4ms [11.4–11.5] |

Alle drei stimmen auf 1ms überein. In dieser Umgebung zeigt ghz 0.3–0.9ms mehr als LeetTest; die
Ursache haben wir nicht gemessen. Welches der beiden Werkzeuge der Latenz «auf dem Draht» näher
kommt, prüft dieser Versuch nicht.

## Nachstellen

Installieren Sie ghz und führen Sie aus dem Wurzelverzeichnis des Repositorys aus:

```sh
go install github.com/bojand/ghz/cmd/ghz@v0.121.0
go run ./test/compare-ghz -ghz "$(go env GOPATH)/bin/ghz"
```

Das Skript baut den Stand und LeetTest und führt jeden Modus 5 Mal aus, was etwa 25 Minuten dauert.
Es gibt die Tabellen dieser Seite aus und schreibt sie, mit der Rohausgabe jedes Laufs, nach
`test/compare-ghz/out/`.

Ein kurzer Lauf, einmal und nur Modus B1, dauert etwa 2 Minuten:

```sh
go run ./test/compare-ghz -ghz "$(go env GOPATH)/bin/ghz" -runs 1 -modes B1
```

Die Modi sind `A`, `B1`, `B2` (`-modes`), die Werkzeuge `leettest`, `ghz-sync`, `ghz-sync50`,
`ghz-async` (`-variants`); die übrigen Flags stehen in `go run ./test/compare-ghz -h`.

Auf einer anderen Maschine werden die Zahlen abweichen. Das Muster nicht: In B1 verpasst ghz im
synchronen Modus den Stillstand, während LeetTest und ghz mit `--async` ihn beide zeigen.

## Quellen zu ghz (v0.121.0, geprüft am 2026-10-02)

1. `-c` ist standardmäßig 50 — [`runner/options.go`, `NewConfig`](https://github.com/bojand/ghz/blob/v0.121.0/runner/options.go).
2. Ein synchroner Worker ruft `makeRequest` auf und wartet darauf; mit `--async` startet er je
   Aufruf eine Goroutine — [`runner/worker.go`, `runWorker`](https://github.com/bojand/ghz/blob/v0.121.0/runner/worker.go).
3. `--async`, `--duration-stop`, `--connections` — [Dokumentation der Optionen](https://ghz.sh/docs/options).
4. Hinter dem Plan gibt der Taktgeber eine Wartezeit von null zurück — [`load/pacer.go`, `ConstantPacer.Pace`](https://github.com/bojand/ghz/blob/v0.121.0/load/pacer.go);
   die Takte erreichen die Worker über einen ungepufferten Kanal — [`runner/requester.go`, `runWorkers`](https://github.com/bojand/ghz/blob/v0.121.0/runner/requester.go).
5. Die Dauer eines Aufrufs ist `EndTime − BeginTime` aus den gRPC-Stats-Ereignissen — [`runner/stats_handler.go`, `HandleRPC`](https://github.com/bojand/ghz/blob/v0.121.0/runner/stats_handler.go).
