# LeetTest, ghz, k6 und JMeter: ein Vergleich

[← Zur Übersicht](README.md)

> Übersetzt aus [docs/ru/why.md](../ru/why.md) bei feb2d1d, 2026-11-10. Bei Abweichungen gilt die
> russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/why.md).

LeetTest ist ein Werkzeug für gRPC. Die Hauptfrage, die es beantwortet: Bei welcher Last kommt der
Dienst nicht mehr zurecht. Diese Seite vergleicht es Punkt für Punkt mit ghz, k6 und JMeter; zu jedem
Punkt steht eine Quelle. Die Prioritäten des Projekts: zuerst die Korrektheit der Messung, dann die
Bedienbarkeit, dann die Geschwindigkeit.

## Vergleich mit ghz, k6 und JMeter

🟢 vorhanden · 🔴 nicht vorhanden · 🟡 teilweise oder mit Zusatzaufwand (das Wort in der Zelle sagt,
womit). Die Zahlen in eckigen Klammern verweisen auf die [Quellen](#quellen) am Ende der Seite.
JMeter ist Version 5.6.3 mit einem gRPC-Plugin: JMeter selbst kann kein gRPC [20][21].

**Was sich ausführen lässt**

| Was wir prüfen | ghz | k6 | JMeter | LeetTest |
|---|---|---|---|---|
| gRPC ohne Plugin | 🟢 [1] | 🟢 [13] | 🟡 Plugin [20][21] | 🟢 |
| Eine Methode ohne `.proto`, per Server Reflection | 🟢 [3] | 🟢 [14] | 🔴 braucht `.proto` [21] | 🟢 |
| Ein Dienst ohne Reflection: Schema aus `.proto` oder Protoset | 🟢 [3] | 🟢 [15] | 🟢 [21] | 🟢 |
| Mehrere Methoden, jede mit eigener RPS, in einem Lauf | 🔴 eine Methode [1] | 🟡 Szenarien im Skript [10] | 🟡 Thread-Gruppen [22] | 🟢 |
| Last ohne eigenen Code beschrieben | 🟢 Flags, JSON, TOML [2] | 🔴 JavaScript [10] | 🟢 `.jmx`-Plan [28] | 🟢 YAML |
| Hochfahren auf eine Ziel-RPS, danach gleichmäßige Last | 🟢 [7] | 🟢 [16] | 🟡 experimentell [23] | 🟢 |
| Schwellen und ein Exit-Code für CI | 🔴 [8] | 🟢 [17] | 🔴 [20][26] | 🟢 |
| Ein Antwortfeld geht in die nächste Anfrage | 🔴 [9] | 🟡 Skript [18] | 🟢 Postprozessoren [27] | 🔴 [roadmap](roadmap.md#später) |
| Suche nach der Last, bei der das Ziel zusammenbricht | 🔴 [8] | 🟡 Skript [19] | 🔴 [20] | 🟢 Stufen |

**Wie gemessen wird**

| Was wir prüfen | ghz | k6 | JMeter | LeetTest |
|---|---|---|---|---|
| Die Last bricht nicht ein, wenn das Ziel langsamer wird (Open Model) | 🟡 `--async`, nicht der Standard [4] | 🟡 eigener Executor, nicht der Standard [11] | 🟡 experimentell, nicht der Standard [23] | 🟢 nur das Open Model |
| Die eigene Verspätung des Generators fließt in die Latenz ein | 🔴 nein: ab dem Senden [5] | 🔴 nein: ab dem Senden, ungesendete in `dropped_iterations` [12][13] | 🔴 nein: ab dem Senden [24] | 🟢 |
| Fehlgeschlagene und durch Timeout abgebrochene Aufrufe gehen in die Perzentile ein | 🟡 `--count-errors`, nicht der Standard [6] | 🟢 [13] | 🟢 [25] | 🟢 fehlgeschlagene mit eigenen Perzentilen je Kategorie, abgebrochene als untere Schranke |

**Wie viel sie aushält und wie gleichmäßig**

| Was wir messen | ghz | k6 | LeetTest |
|---|---|---|---|
| Anfragen pro Sekunde, die ein Kern hält; das Ziel antwortet sofort (mehr ist besser) | 9.300 | 4.750 | 35.550 |
| Gleichmäßigkeit des Zeitplans bei 1000 Anfragen pro Sekunde: Abstand zwischen benachbarten Anfragen am Ziel, Median / p99 / p99,9 / Maximum; ideal ist 1 ms (Median: näher an 1 ms ist besser; p99 / p99,9 / Maximum: weniger ist besser) ² | 1,1 / 1,75 / 3,5 / 9,3 ms | 0,59 / 1,8 / 2,05 / 14,7 ms | 1,5 / 1,68 / 1,88 / 3,1 ms |
| Was das Werkzeug zur Latenz hinzufügt; das Ziel antwortet in 20 ms, 500 Anfragen pro Sekunde; p50 / p99 (weniger ist besser) | 0,33 / 0,31 ms | 0,33 / 0,49 ms | 0,98 / 1,3 ms ¹ |

Diese drei Zeilen messen verschiedene Dinge, eine Gesamtwertung gibt es nicht. Der Durchsatz je Kern
ist bei LeetTest höher; der Median des Abstands liegt bei ghz näher an 1 ms, das p99 und das Ende
des Abstands sind bei LeetTest besser; der Zuschlag zur Latenz ist bei ghz und k6 kleiner. JMeter
wurde nicht gemessen. ¹ LeetTest zählt die Latenz ab dem Moment, zu dem der Aufruf fällig war, ghz
und k6 ab dem Senden (die Zeile „Die eigene Verspätung des Generators fließt in die Latenz ein“
oben); bei `GOMAXPROCS=1` startete LeetTest in diesem Lauf Aufrufe verspätet (p99 etwa 1,1 ms,
einzelne bis 2,5 ms), und diese Verspätung landet nur in seiner Zahl. Die Quelle dieser Schätzung ist
der unten beschriebene Lauf `compare-perf`; eine eigene Auswertung dafür gibt es im Repository nicht.
² Im Lauf sendeten die Werkzeuge in jeder stabilen Sekunde 999–1001 Anfragen, der mittlere Abstand
beträgt also 1 ms; ein Median abseits von 1 ms bedeutet, dass die Anfragen ungleichmäßig kommen,
nicht dass die Rate eine andere ist.

So wurde gemessen. Jedes Werkzeug läuft in einem eigenen Docker-Container mit einem Kern
(`--cpus=1`, `GOMAXPROCS=1`), der Referenz-Stand in einem anderen mit vier. Die Zahlen berechnet der
Stand daraus, wann die Aufrufe bei ihm eintrafen, nicht aus den Berichten der Werkzeuge; in der
dritten Zeile sind es p50 und p99 des Werkzeugs selbst minus die Zeit, die der Stand die Antwort
am selben Perzentil zurückhielt. ghz v0.121.0 mit `--async`; k6 v2.3.0 mit konstanter Ankunftsrate
und 100 vorab angelegten virtuellen Nutzern (es öffnet für jeden eine Verbindung); LeetTest mit
einer Verbindung. Drei Läufe zu je 20 Sekunden, in der zweiten und dritten Zeile steht der Median.
Eine Rate in der ersten Zeile gilt als gehalten, wenn, ohne die ersten zwei Sekunden und die
letzte, mindestens 99,9 % der geplanten Aufrufe beim Stand ankamen und keine Sekunde unter 99 % der
Rate fiel; ein Punkt zählt, wenn mindestens zwei von drei Läufen ihn hielten. LeetTest mit vier
Kernen hielt 108.400 Anfragen pro Sekunde, der Stand war in der ersten Zeile also nicht die
Grenze. Eine Maschine: 24 logische Kerne, Linux in WSL2 unter Docker Desktop, 12.–13. Oktober 2026.

Zum Nachmessen (Docker und Go werden gebraucht; der Befehl läuft im Wurzelverzeichnis des
Repositorys):

```
go run ./test/compare-perf -runs 3 -duration 20s
```

Am Ende gibt der Befehl die Tabellen aus und speichert sie in `test/compare-perf/out/summary.md`;
ein voller Lauf dauert bis zu drei Stunden. `-plan` gibt alle Einstellungen und Befehle aus, ohne
etwas zu starten.

In der Tabelle hat nur LeetTest ein 🟢 bei: Open Model als einziges Modell, Erfassung der Verspätung
des Generators, Suche nach dem Ausfallpunkt. Wo LeetTest ein 🔴 hat, ist die Verkettung von
Aufrufen (ein Antwortfeld geht in die nächste Anfrage): k6 und JMeter haben sie, LeetTest noch nicht;
die Reihenfolge steht in der [roadmap](roadmap.md). Das Hochfahren auf eine Ziel-RPS (`load.ramp`),
Schwellen für CI (`pass`) und der Betrieb ohne Reflection (`.proto`, Protoset) hat LeetTest.

## Mehrere Methoden in einem Lauf

Die Last wird als mehrere gleichzeitige Methoden beschrieben: jede mit eigener Rate und Dauer, alles
in einem Lauf und einem Bericht. ghz startet pro Lauf eine Methode [1]; k6 und JMeter beschreiben
mehrere Methoden mit Szenarien oder Thread-Gruppen [10][22].

## Latenz ab dem Zeitplan

Die Latenz zählt ab dem Zeitpunkt, für den die Anfrage geplant war, nicht ab dem, zu dem sie
gesendet werden konnte, und enthält die Wartezeit auf der Seite des Generators. Ein Beispiel: Das
Ziel hängt eine Sekunde, eine Anfrage ging laut Plan zu Beginn der Pause raus, die Antwort kam nach
1005 ms. Das Zählen ab dem Plan ergibt `1005ms`; das Zählen ab dem Senden (ghz, k6, JMeter
[5][13][24]) ergibt bei einer Anfrage, die sich beim Generator in die Warteschlange stellte, eine
kleinere Zahl. Der Versuch auf dem Stand steht auf der Seite [LeetTest und ghz](compare-ghz.md).

Die Last folgt einem Zeitplan: Die angeforderte RPS hängt nicht davon ab, ob der Dienst mitkommt.

Eine durch den Timeout abgebrochene Anfrage wird nicht durch eine Zahl ersetzt. Bekannt ist nur,
dass sie länger als der Timeout dauerte, und könnten solche Anfragen den Platz eines Perzentils
einnehmen, druckt der Bericht statt einer Zahl eine untere Schranke: `p99 >2.0s`.

## Ohne Vorbereitung

Adresse des Dienstes und Methodenname genügen zum Start. Die Beschreibung der Methode holt das
Werkzeug per gRPC Server Reflection vom Dienst selbst: keine `.proto`-Dateien, kein Codegen, keine
Build-Schritte. Der Request-Body steht als gewöhnliches YAML in der Konfiguration und wird vor dem
Start nach dem Schema des Dienstes gebaut.

## Deklarative Beschreibung

Die Last wird durch eine Konfigurationsdatei festgelegt, nicht durch ein Programm. Die Datei liegt
beim Code des Dienstes und durchläuft das Code-Review. Ein Skriptprogramm gibt es nicht, also auch
keine Fehler darin; Fehler in der Datei selbst (falsche Grenzen, die falsche Methode) bleiben
möglich.

## Fehlermeldungen und Laufverlauf

Fehlermeldungen nennen Ort und Ursache. Ein unbekannter Schlüssel in der Konfiguration oder eine
Rate von null sind Fehler und werden nicht stillschweigend hingenommen: Ein Lauf, der keine einzige
Anfrage gesendet hat, sieht nicht erfolgreich aus. Der Zustand des Laufs ist live sichtbar: aktuelle
Rate, laufende Anfragen, Fehleranteil, Perzentile.

## Quellen

Versionen: ghz v0.121.0 (letztes Release), k6 v2.3.0, JMeter 5.6.3 mit dem Plugin
jmeter-grpc-request v2.0.2. Geprüft am 2026-10-12. Quellcode — am Versions-Tag, die k6-Dokumentation
— am Commit
[`b5fdef3`](https://github.com/grafana/k6-docs/tree/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x).

**ghz**

1. Eine Methode pro Lauf: `--call` nimmt einen Namen, der Start ist `runner.Run(cfg.Call, …)` —
   [`cmd/ghz/main.go` L40](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L40),
   [L334](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L334).
2. Die Last wird per Flags oder per JSON/TOML-Datei festgelegt —
   [`cmd/ghz/main.go` L28](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L28).
3. Die Beschreibung der Methode kommt aus `.proto` oder Protoset, ohne beides per Reflection —
   [`runner/requester.go` L79–L113](https://github.com/bojand/ghz/blob/v0.121.0/runner/requester.go#L79-L113).
4. Der synchrone Worker wartet auf den Aufruf, mit `--async` startet er pro Aufruf eine Goroutine —
   [`runner/worker.go` L47–L70](https://github.com/bojand/ghz/blob/v0.121.0/runner/worker.go#L47-L70);
   das Flag — [`cmd/ghz/main.go` L78](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L78).
5. Die Dauer eines Aufrufs ist `EndTime − BeginTime` aus den gRPC-Stats-Ereignissen —
   [`runner/stats_handler.go` L44](https://github.com/bojand/ghz/blob/v0.121.0/runner/stats_handler.go#L44).
6. In Perzentile und Histogramm gehen nur Aufrufe ohne Fehler, solange `--count-errors` nicht
   gesetzt ist —
   [`runner/reporter.go` L259–L264](https://github.com/bojand/ghz/blob/v0.121.0/runner/reporter.go#L259-L264),
   [`cmd/ghz/main.go` L218](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L218).
   Antwort des Maintainers: «the stats are only based on successful / OK responses» —
   [issue 248](https://github.com/bojand/ghz/issues/248).
7. `--rps` und die Profile `--load-schedule` const/step/line —
   [`cmd/ghz/main.go` L82–L109](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L82-L109).
8. Ein Exit-Code ungleich null nur, wenn der Lauf nicht gestartet oder der Bericht nicht
   geschrieben werden konnte; Flags für Schwellen gibt es nicht. Die gesamte Flag-Liste —
   [`cmd/ghz/main.go` L28–L279](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L28-L279),
   der Ausstieg — [L334–L342](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L334-L342), [L385–L392](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L385-L392).
9. Die Daten der Aufrufvorlage (`CallData`) enthalten nicht die Antwort des vorherigen Aufrufs —
   [`runner/calldata.go` L32–L46](https://github.com/bojand/ghz/blob/v0.121.0/runner/calldata.go#L32-L46).

**k6**

10. Mehrere Szenarien in einem Skript, jedes mit eigenem Executor und eigener JavaScript-Funktion
    (`exec`) —
    [Scenarios](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/_index.md#L13-L18),
    [Optionentabelle, `exec`](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/_index.md#L90).
11. Das Open Model gibt es nur in den Arrival-Rate-Executoren; `vus` mit `duration` ist der
    Executor constant-vus, also ein Closed Model —
    [Open and closed models](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/concepts/open-vs-closed.md#L9-L13),
    [Options, `duration`](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/k6-options/reference.md#L329).
12. Ohne freien VU wird die Iteration verworfen und im Zähler `dropped_iterations` gezählt —
    [Dropped iterations](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/concepts/dropped-iterations.md#L10-L25).
13. `grpc_req_duration` wird beim End-Ereignis jedes Aufrufs als `EndTime − BeginTime`
    geschrieben, der Status als Tag —
    [`internal/lib/netext/grpcext/conn.go` L282–L295](https://github.com/grafana/k6/blob/v2.3.0/internal/lib/netext/grpcext/conn.go#L282-L295).
14. `reflect: true` in den Parametern von `connect` —
    [Client.connect](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-connect-connect-address-params.md#L26).
15. `Client.load` liest `.proto` —
    [Client.load](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-load.md#L11-L18).
16. Der Executor `ramping-arrival-rate` ändert die Rate stufenweise —
    [Ramping arrival rate](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/executors/ramping-arrival-rate.md#L8-L16).
17. Eine verletzte Schwelle ergibt einen Exit-Code ungleich null, 99 —
    [Thresholds](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/thresholds.md#L70-L80),
    [`errext/exitcodes/codes.go` L18](https://github.com/grafana/k6/blob/v2.3.0/errext/exitcodes/codes.go#L18).
18. `Client.invoke` gibt das Antwortobjekt an das Skript zurück —
    [Client.invoke](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-invoke.md#L24-L28).
19. Breakpoint-Test — `ramping-arrival-rate` und eine Schwelle mit `abortOnFail` —
    [Breakpoint testing](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/testing-guides/test-types/breakpoint-testing.md#L66-L75),
    [L102–L106](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/testing-guides/test-types/breakpoint-testing.md#L102-L106).

**JMeter**

20. Die Komponentenreferenz von JMeter 5.6.3 enthält keinen gRPC-Sampler (das Wort gRPC kommt
    darin nicht vor), keine Schwellen auf Perzentile und keine Suche nach dem Ausfallpunkt —
    [`xdocs/usermanual/component_reference.xml`](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml).
21. Das Plugin jmeter-grpc-request v2.0.2 setzt das archivierte zalopay-oss fort, der JMeter
    Plugins Manager installiert es: nur Unary-Aufrufe, Eingabe ist ein Ordner mit `.proto` —
    [README L19, L28](https://github.com/bakthava/jmeter-grpc-request/blob/v2.0.2/README.md#L19),
    [Plugins Manager](https://github.com/undera/jmeter-plugins/blob/cdaa5ac7d27f081787f57d69133e8c34b14b133f/site/dat/repo/various.json#L1752-L1826).
22. Ein Plan — mehrere Thread-Gruppen; Constant Throughput Timer und Open Model Thread Group haben
    ihre eigene Rate je Gruppe —
    [`component_reference.xml` L5153](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5153),
    [L5228](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5228),
    [L6517–L6521](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6517-L6521).
23. Die Open Model Thread Group «is experimental»; der Zeitplan `rate(…) random_arrivals(…)
    rate(…)` ändert die Rate linear; die gewöhnliche Thread Group ist ein Thread-Pool mit einer
    Anzahl von Iterationen —
    [`component_reference.xml` L6519–L6526](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6519-L6526),
    [L6619–L6621](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6619-L6621).
24. Elapsed time — «vom Moment vor dem Senden der Anfrage bis zum Empfang der letzten Antwort» —
    [`xdocs/usermanual/glossary.xml` L34–L35](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/glossary.xml#L34-L35).
25. Im Aggregate Report und im HTML-Bericht geht die Zeit jedes Aufrufs auch bei einem Fehler ein —
    [`SamplingStatCalculator.java` L185](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/visualizers/SamplingStatCalculator.java#L185),
    [`StatisticsSummaryConsumer.java` L79–L88](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/report/processor/StatisticsSummaryConsumer.java#L79-L88).
26. Im Startcode steht `System.exit(1)` nur bei einem Startfehler, dazu eine standardmäßig
    ausgeschaltete Eigenschaft zum Stoppen der Threads; die Duration Assertion markiert eine
    einzelne Antwort als fehlgeschlagen, nicht den Lauf —
    [`JMeter.java` L519](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/JMeter.java#L519),
    [`get-started.xml` L476–L478](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/get-started.xml#L476-L478),
    [`component_reference.xml` L4695–L4698](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L4695-L4698).
27. Die Postprozessoren JSON Extractor und Regular Expression Extractor nehmen Werte aus der
    Antwort des Samplers und speichern sie in einer Variablen —
    [`component_reference.xml` L6334](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6334),
    [L5797](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5797).
28. Der Plan liegt in einer `.jmx`-Datei und wird mit `jmeter -n -t plan.jmx` gestartet —
    [`xdocs/usermanual/get-started.xml` L474](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/get-started.xml#L474).
