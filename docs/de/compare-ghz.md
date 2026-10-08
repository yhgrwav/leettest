# LeetTest und ghz auf demselben Stand

> Übersetzt aus [docs/ru/compare-ghz.md](../ru/compare-ghz.md) bei 67f8732, 2026-10-08. Bei
> Abweichungen gilt die russische Fassung.

Diese Seite vergleicht LeetTest mit [ghz](https://ghz.sh) auf einem Testserver mit bekanntem
Verhalten. Die Frage ist eng: Wenn der Server stockt oder langsamer wird, zeigt der Bericht das?

Kurz:

- **Wenn sich der Server normal verhält**, melden beide Werkzeuge dieselben Latenzen (Modus A).
- **Wenn der Server 2 Sekunden einfriert** (Modus B1), meldet ghz im synchronen Modus (Standard)
  ein p99 von 10.8ms mit `-c 10` und 12.0ms mit dem Standard `-c 50`. LeetTest meldet 1.71s. ghz
  mit `--async` meldet ebenfalls 1.71s.
- **Wenn der Server langsamer ist, als die Last verlangt** (Modus B2), sendet ghz im synchronen
  Modus (Standard) mit `-c 10` 2980 der 6000 angeforderten Aufrufe. Seine Latenzen stimmen für die
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

Die ghz-Flags sind:

```
ghz --insecure --call grpc.health.v1.Health/Check -d '{}' --rps 200 -z 30s -t 5s \
    --duration-stop wait --connections 1 -c 10 [--async]
```

Synchrones ghz läuft mit `-c 10` und mit dem ghz-Standard `-c 50`. `--duration-stop wait` lässt
Aufrufe, die bei 30s noch unterwegs sind, zu Ende laufen und zählen.

Jedes Werkzeug lief 5 Mal in jedem Modus, und jeder Lauf bekam einen frischen Stand. Die Reihenfolge
rotiert, sodass kein Werkzeug immer zuerst läuft. Die Tabellen zeigen den Median der 5 Läufe, mit
Minimum und Maximum in Klammern.

Umgebung:

- LeetTest bei Commit `4be4115`;
- ghz v0.121.0 (grpc-go v1.56.3);
- Go 1.26.1;
- Windows 11 (10.0.26100), AMD Ryzen 9 5900X, 32 GB RAM;
- Werkzeuge und Stand auf derselben Maschine, über Loopback.

## Ergebnisse

### B1: ein 2-Sekunden-Stillstand

| Werkzeug | gesendet | fehlgeschlagen | p50 | p90 | p95 | p99 | Aufrufe über 1s |
|---|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 10.6ms [10.5–10.6] | 11.2ms [11.1–11.2] | 514ms [510–516] | 1.71s [1.71–1.72] | nicht ausgegeben |
| ghz, sync (`-c 10`) | 5999 [5999–6002] | 0 | 10.2ms [10.1–10.2] | 10.5ms [10.4–10.6] | 10.6ms [10.6–10.7] | 10.8ms [10.8–10.9] | 10 [10–10] |
| ghz, sync (`-c 50`, Standard) | 5999 [5999–6000] | 0 | 10.2ms [10.2–10.3] | 10.8ms [10.7–10.8] | 11.1ms [10.9–11.1] | 12.0ms [11.7–12.5] | 50 [50–50] |
| ghz, `--async` | 5999 [5999–6000] | 0 | 10.2ms [10.2–10.3] | 10.7ms [10.7–10.7] | 514ms [510–515] | 1.71s [1.71–1.72] | 203 [202–203] |

In 2 Sekunden bei 200 RPS waren etwa 400 Aufrufe fällig. Mit `--async` zählt ghz 203 Aufrufe über
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

| Werkzeug | gesendet | fehlgeschlagen | p50 | p90 | p95 | p99 |
|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 101ms [101–101] | 101ms [101–101] | 101ms [101–101] | 102ms [102–102] |
| ghz, sync (`-c 10`) | 2980 [2980–2980] | 0 | 100.6ms [100.5–100.6] | 101.1ms [101.1–101.2] | 101.3ms [101.2–101.5] | 102ms [101.8–102.8] |
| ghz, sync (`-c 50`, Standard) | 6000 [5999–6003] | 0 | 100.3ms [100.3–100.3] | 100.7ms [100.7–100.7] | 101ms [101–101] | 101.5ms [101.5–101.5] |

Synchrones ghz sendet höchstens c / Latenz Aufrufe pro Sekunde, wobei c die Zahl der Worker ist.
Mit `-c 10` und 100ms sind das 100 Aufrufe pro Sekunde, die Hälfte der angeforderten 200. Die
Latenzen stimmen, die Last nicht. Wer nur die Latenzspalten liest, sieht einen Server, der 200 RPS
bei 101ms schafft. Der Server schaffte 100 RPS.

Mit dem Standard `-c 50` liegt die Decke bei 500 Aufrufen pro Sekunde, und die volle Last geht
raus. Dieselbe Decke gilt für jedes c, sobald der Server langsam genug wird: Bei `-c 50` wird sie
oberhalb von 250ms erreicht.

### A: Kontrolle

| Werkzeug | gesendet | fehlgeschlagen | p50 | p90 | p95 | p99 |
|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 10.6ms [10.6–10.6] | 10.9ms [10.9–11.0] | 11.1ms [11.0–11.1] | 11.3ms [11.2–11.5] |
| ghz, sync (`-c 10`) | 6000 [5999–6001] | 0 | 10.2ms [10.2–10.3] | 10.5ms [10.5–10.5] | 10.5ms [10.5–10.5] | 10.7ms [10.7–10.8] |
| ghz, `--async` | 6001 [5999–6002] | 0 | 10.2ms [10.2–10.2] | 10.5ms [10.5–10.5] | 10.5ms [10.5–10.6] | 10.7ms [10.7–10.8] |

Alle drei stimmen auf 0.6ms überein. LeetTest liegt 0.4–0.6ms höher; die Ursache wird hier nicht
gemessen. LeetTest misst ab dem Zeitpunkt, für den ein Aufruf geplant war, ghz ab dem Beginn des
Aufrufs [5], also enthält LeetTest die eigene Verzögerung des Generators vor dem Senden. LeetTest
zählt sie absichtlich: Fällt der Generator zurück, zeigt die Latenz das, statt es zu verbergen. Die
Startverspätung wird zudem gesondert ausgegeben.

## Nachstellen

Installieren Sie ghz und führen Sie aus dem Wurzelverzeichnis des Repositorys aus:

```
go run ./test/compare-ghz -ghz /path/to/ghz
```

Das Skript baut den Stand und LeetTest und führt jeden Modus 5 Mal aus, was etwa 25 Minuten dauert.
Es gibt die Tabellen dieser Seite aus und schreibt sie, mit der Rohausgabe jedes Laufs, nach
`test/compare-ghz/out/`. Nützliche Flags:

- `-runs N` legt die Zahl der Läufe fest;
- `-modes B1` führt nur die genannten Modi aus;
- `-variants ghz-sync50` führt nur die genannten Werkzeuge aus.

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
