<div align="center">

<h1><img src="../../assets/logo.png" width="360" alt="LeetTest"></h1>

**gRPC-Lasttests, die nicht lügen.**

[Русский](../ru/) · [English](../en/) · [Deutsch](../de/) · [简体中文](../zh-CN/)
[![CI](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/yhgrwav/leettest.svg)](https://pkg.go.dev/github.com/yhgrwav/leettest)
[![Go version](https://img.shields.io/github/go-mod/go-version/yhgrwav/leettest)](../../go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](../../LICENSE)

</div>

---

> Übersetzt aus [README.md](../../README.md) bei 724bf40, 2026-10-05. Bei Abweichungen gilt die
> russische Fassung. Dazu ein Verzeichnis der deutschen Dokumentation, das im Original fehlt.

> **Frühes Stadium.** Funktioniert: unäre Last auf einen echten Dienst, mehrere Methoden mit eigener
> RPS in einem Lauf, Anfrage-Bodies aus der Konfiguration, ein Bericht in der Konsole und JSON für
> Skripte. Noch nicht: Hochfahren, Pass/Fail-Schwellen für CI, Metrik-Export —
> **[was als Nächstes kommt →](roadmap.md)**. Fehlt Ihnen etwas —
> [eröffnen Sie ein Issue](https://github.com/yhgrwav/leettest/issues/new/choose). Alles Folgende
> beschreibt, was bereits funktioniert.

Das Werkzeug beantwortet die Frage, mit der man zu einem Lasttest kommt: **ab welcher Last hält der
Dienst nicht mehr mit, und wo liegt der Engpass.** Es erzeugt Last wie in der Produktion — mehrere
Methoden gleichzeitig, jede mit eigener RPS — und misst so, dass die Zahlen nicht lügen, während der
Dienst degradiert. Kein `.proto`, kein Codegen, keine Skripte.

Prioritäten, der Reihe nach: Die Zahlen lügen nicht; das Werkzeug ist angenehm zu benutzen — eine
unklare Fehlermeldung ist ebenso ein Defekt wie eine falsche Zahl; der Generator wird nie zum
Engpass.

## Installation

```console
$ go install github.com/yhgrwav/leettest/cmd/leettest@latest
```

Benötigt Go 1.26 oder neuer. Fertige Binärdateien liegen auf der Release-Seite.

**Wo man es ausführt.** Unter Linux neben dem Ziel: im selben Netz, im selben Cluster, auf einem
CI-Runner. Alles zwischen Generator und Ziel landet in der Latenz und sieht aus wie die Zeit des
Ziels: auf unserem Stand bei 1000 RPS lag p99 über die Portweiterleitung von Docker Desktop bei
38 ms, innerhalb des Docker-Netzes bei 2 ms. Den exakten Aufrufplan gibt es nur unter Linux; in einem
Container mit einer CPU-Quote unter zwei Kernen sind seltene Startverzögerungen bis zur Quotenperiode
(meist 100 ms) möglich, und der Bericht zeigt sie. Hinter einem L4-Balancer wird ein Backend
belastet — [siehe unten](#den-bericht-lesen). Unter Windows springt die Uhr in Schritten von etwa
0,5 ms, und ein Lauf gegen einen schnellen Dienst wird für ungültig erklärt.

**[Schnellstart →](quickstart.md)** — jede Fähigkeit am Referenz-Stand: eine Konfiguration mit
allen Feldern, den Bericht lesen, andere Verhaltensweisen des Ziels, CI. Arbeiten Sie mit einem
KI-Agenten — geben Sie ihm [AGENTS.md](../../AGENTS.md).

## Ausführen

```yaml
app:
  target:
    ip: localhost
    port: 50051
  tls: false

load:
  warmup: 5s
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m

    - method: wallet.v1.WalletService/Transfer
      rps: 50
      duration: 1m
```

```console
$ leettest -c leettest.yaml
```

Der Methodenname ist der volle, `package.Service/Method`. Das Werkzeug holt das Schema der Methode
über gRPC Server Reflection vom Dienst, deshalb ist kein `.proto` nötig. Konfiguration, Adresse,
Methoden und Anfrage-Body werden vor dem Start geprüft: Jeder Fehler heißt Exit-Code 1 und kein
einziger Aufruf ans Ziel. Die ersten `warmup` Sekunden zählen nicht zur Statistik. Jeder Aufruf einer
Methode geht mit demselben Body raus: Bei einem Schreibvorgang mit Idempotenzschlüssel wird der Weg
der Wiederholung gemessen, nicht das Anlegen des Datensatzes. Alle Felder, TLS, Header, der
Anfrage-Body und die Flags stehen in der **[Referenz](reference.md)**.

Im Terminal läuft der Lauf im Vollbild, `q` zum Anhalten. Ohne Terminal (CI, umgeleitete Ausgabe) —
eine Fortschrittszeile pro Sekunde:

```
32.0s  sent 25600  rps 800  in-flight 47  failed 51  not-sent 0  p99 43ms
```

Der Bericht geht nach stdout, Fortschritt und Fehler nach stderr.

## Den Bericht lesen

Am Ende — ein Bericht je Methode: gesendet, fehlgeschlagen, `sent/s`, p50/p90/p95/p99.

- Ein Aufruf zählt ab seinem **geplanten** Zeitpunkt: Hat ihn das Ziel oder der Generator
  verzögert, steckt das in der Latenz.
- `sent/s` sind die gesendeten Aufrufe geteilt durch die Sendezeit: Ein Ziel, das am Ende des Laufs
  hängt, senkt diese Zahl nicht.
- Ein Perzentil, dessen Platz vom Timeout abgeschnittene Aufrufe einnehmen könnten, wird als
  **Untergrenze** ausgegeben: `>2.0s`. Kein Wert, sondern „mindestens".
- Die Last läuft über **eine Verbindung** und landet auf **einem Backend**: hinter einem
  L4-Balancer (Kubernetes ClusterIP, NLB) und wenn DNS mehrere Adressen liefert. „Hält X nicht"
  betrifft dieses Backend, nicht den Dienst, und der Bericht zeigt das nicht. Ein L7-Balancer, der
  einzelne Anfragen verteilt (Envoy, ein gRPC-Ingress), verteilt auch eine Verbindung. Der Bericht
  gibt aus, wie oft die Verbindung neu aufgebaut wurde und welches Limit gleichzeitiger Streams das
  Ziel angekündigt hat.

**Kategorien.** Antworten außer einem Erfolg werden in Zeilen aufgeteilt, jede mit eigenen
Perzentilen. Der Status kann von einem Proxy vor dem Ziel stammen statt vom Ziel: nginx ohne
lebendes Backend antwortet `UNAVAILABLE`, und der Client kann das eine nicht vom anderen
unterscheiden.

- `request error`: Der Aufruf würde bei jeder Rate scheitern — eine falsche Anfrage oder eine, die
  das Ziel oder ein Proxy zu groß fand.
- `overload`: Der Status sagt überlastet oder nicht verfügbar, `RESOURCE_EXHAUSTED` oder
  `UNAVAILABLE`. Das sind die Worte des Status, keine Diagnose des Ziels.
- `failure`: Der Status sagt, der Aufruf ist kaputtgegangen: `INTERNAL`, `UNKNOWN`, `DATA_LOSS`,
  `CANCELLED` von der Gegenseite, `ABORTED` (ein Konflikt gleichzeitiger Änderungen, kein
  Kapazitätsmangel).
- `bad response`: Eine Antwort kam, und der Client nahm sie nicht an: größer als
  `app.max_response_size` oder mit einer Kodierung komprimiert, die der Client nicht angekündigt hat.
- `client error`: Der Client selbst verweigerte das Senden: Die Anfrage ließ sich nicht kodieren,
  oder ein Codec oder Interceptor schlug fehl.

Aufrufe, die das Ziel nie erreicht haben, zählen als Fehler, bleiben aber aus den Perzentilen. Eine
Anfrage, die rausging, aber keinen Status bekam — die Gegenseite hat den Stream zurückgesetzt oder
die Verbindung getrennt —, wird gesondert gezählt, als `cut off`: Das Ziel oder ein Proxy könnte sie
verarbeitet haben, was bei einem Schreibvorgang ein Grund ist, auf Duplikate zu prüfen. Ein Aufruf,
den unser eigenes Anhalten abgeschnitten hat (Ctrl+C, SIGTERM), ist `aborted`, unabhängig vom Code:
keine Ablehnung durch das Ziel.

| Code | Status kam über die Leitung | Code vom Client gesetzt, Anfrage ging raus | Code vom Client gesetzt, Anfrage ging nicht raus |
|---|---|---|---|
| `INVALID_ARGUMENT`, `NOT_FOUND`, `ALREADY_EXISTS`, `PERMISSION_DENIED`, `UNAUTHENTICATED`, `FAILED_PRECONDITION`, `OUT_OF_RANGE`, `UNIMPLEMENTED` | `request error` | `cut off` | `client error` |
| `RESOURCE_EXHAUSTED` | `overload`; „larger than max" von grpc-go ist ein `request error` | `cut off`; eine Antwort über unserem Limit ist eine `bad response` | `client error` |
| `UNAVAILABLE` | `overload` | `cut off`; ein vom Ziel vor der Verarbeitung abgelehnter Stream ist `overload` | `unreachable`; dasselbe |
| `CANCELLED`, `UNKNOWN`, `INTERNAL`, `DATA_LOSS`, `ABORTED` | `failure` | `cut off`; eine Antwort, die sich nicht entpacken ließ, ist eine `bad response` | `client error` |
| `DEADLINE_EXCEEDED` | Timeout | Timeout | Timeout, nicht gesendet |

Eine zu große Anfrage wird am Fehlertext von grpc-go erkannt. Ein Ziel mit anderer Implementierung
(Envoy, Java) formuliert es anders, und seine Ablehnung landet bei `overload` statt bei
`request error`.

`UNAVAILABLE` bei einem abgelehnten Stream ist die Übersetzung von grpc-go für das RST_STREAM
REFUSED_STREAM des Ziels: Den Code hat der Client gesetzt, die Ablehnung kam aber vom Ziel, deshalb
steht er in der Zeile „sent by the target". Das Ziel hat einen solchen Stream nicht verarbeitet
(RFC 9113 §8.7).

Unter dem Bericht werden fehlgeschlagene Aufrufe nach gRPC-Code auf zwei Zeilen aufgeschlüsselt.
„sent by the target" — der Status kam über die Leitung, vom Ziel oder einem Proxy. „set by the
client" — der Client hat den Code selbst gesetzt: Niemand antwortete, der Stream wurde
zurückgesetzt, unsere Deadline lief ab, oder der Client lehnte die Antwort ab. `DEADLINE_EXCEEDED`
vom Ziel ist meist unsere eigene Deadline: Sie geht im Header `grpc-timeout` ans Ziel. Die Kategorie
sagt, wessen Fehler es ist, der Code, wonach man in den Logs des Ziels suchen soll.

**Wartezeiten auf Client-Seite.** Die Latenz enthält alles, worauf ein Aufruf auf unserer Seite
gewartet hat: einen verspäteten Generator, das Warten auf die Verbindung, das Warten auf einen
freien Stream. Sinkt ohne sie das p99 mindestens einer Methode um 10 % oder mehr, oder sind manche
Aufrufe nie rausgegangen, gibt der Bericht ein Urteil, nennt die im p99-Ende häufigste Ursache und
gibt p99 ohne die Wartezeiten aus — die Zeit, die der Aufruf beim Ziel verbracht hat. Liegt die
Ursache darin, dass die Streams ausgingen, ist die Kapazität des Ziels über „Limit × Verbindungen"
nicht gemessen. Hat ein Aufruf zuerst auf den Resolver gewartet, landen die Interceptoren des
Aufrufers ebenfalls in der Verbindungswartezeit, sodass p99 ohne Wartezeiten etwas zu niedrig
ausfallen kann. Nie rausgegangene Aufrufe werden nach Grund aufgeteilt. Eine Verschiebung unter
10 % ist kein Urteil, sondern ein Hinweis mit der Zahl; ist p99 eine Untergrenze, ist die
Verschiebung unbekannt und es gibt keinen Hinweis.

**Uhr.** Das Werkzeug misst den Uhrenschritt des Hosts vor und nach dem Lauf. Ein merklicher
Schritt gibt eine Zeile aus: `clock step 502us on this host: every latency and wait is +/- 502us`.
Unter Linux beträgt der Schritt einige zehn Nanosekunden, und es gibt keine Zeile.

## Wann ein Lauf ungültig ist

Es gibt einen Bericht, aber seine Zahlen handeln nicht von der Last auf das Ziel (Exit 2), wenn:

- **die Grenze `-max-in-flight` erreicht wurde.** Timeouts und Grenze werden vor dem Start so
  geprüft, dass ein hängendes Ziel sie nicht erreicht; wird sie erreicht, fehlte dem Generator CPU.
- **jeder gemessene Aufruf einer Methode ein `request error`, `client error` oder eine
  `bad response` ist.** Der Hinweis nennt Methode, Ursache und was zu tun ist.
- **die Uhr des Hosts gröber ist als ein Viertel des p50** einer Methode, oder der Schritt während
  des Laufs auf 1 µs oder mehr wuchs (`clock_step`). Messen Sie von Linux aus.

## Exit-Codes

Schwellen „hält / hält nicht" gibt es noch nicht, deshalb gibt ein Lauf, der sein Ende erreicht,
`0` zurück, egal wie das Ziel antwortete: **`0` heißt nicht „der Dienst ist gesund"**. Die Codes
haben einen Rang: Ein ungültiger Lauf (`2`) steht über einem unvollständigen (`3`); ein Anhalten,
das einen Bericht ausgab, ist immer `3`.

| Code | Was passiert ist |
|---|---|
| `0` | Der Plan lief durch, der Bericht ist vollständig |
| `1` | Der Lauf fand nicht statt: Flags, Konfiguration, Verbindung. Kein Bericht |
| `2` | Der Lauf ist ungültig (siehe oben). Es gibt einen Bericht, aber seine Zahlen handeln nicht von der Last |
| `3` | Der Lauf hielt vor seinem Plan an. Es gibt einen Bericht, und er deckt nur ab, was durchging |
| `130` | Ohne Bericht abgebrochen nach Ctrl+C |
| `143` | Ohne Bericht abgebrochen nach SIGTERM |

Code `4` ist für Schwellen reserviert. Wie das Anhalten funktioniert, steht in der
[Referenz](reference.md#anhalten).

## JSON für Skripte und CI

Mit `-output json` bekommt stdout genau ein JSON-Objekt und einen Zeilenumbruch; alles andere geht
nach stderr, sodass stdout direkt in `jq` gehen kann. Das Objekt wird nur für einen Lauf ausgegeben,
der stattfand (Codes `0`, `2`, `3`); bei `1`, `130` und `143` ist stdout leer. Das Feld `outcome`
(`complete`, `invalid` oder `incomplete`) passt immer zum Exit-Code.

Das Schema ist über `schema_version` versioniert, derzeit `1`, und es ist ein Vertrag; der
Bildschirmtext ist es nicht: Werten Sie das JSON aus, nicht den Bildschirm. Die Regeln:

- ein neues Feld kommt ohne Versionswechsel hinzu; ein Feld umzubenennen, zu entfernen oder seinen
  Typ zu ändern, erhöht `schema_version`;
- die Werte einer Aufzählung (`outcome`, `unchecked[].reason`, die Codes in `failure_codes`) können
  ohne Versionswechsel wachsen;
- ein Verbraucher muss unbekannte Felder überspringen und einen unbekannten Aufzählungswert
  verarbeiten, ohne zu scheitern.

Die Einheit steht im Feldnamen: `_us` sind ganze Mikrosekunden, `_s` ganze Sekunden; die Rate `rps`
ist die einzige gebrochene Zahl. Ein Wert, den der Lauf nicht hervorgebracht hat, ist `null`, nicht
`0`. Ein Perzentil ist ein Objekt `{"us": 1234, "lower_bound": false}`: Mit `lower_bound: true` ist
es eine Untergrenze, kein Wert. Latenzen sind die Werte des Histogramms auf 3 signifikante Stellen
(ein Fehler bis 0,1 %), Zählungen sind exakt. Zeiten zählen ab `started_at` (RFC 3339, UTC), dem
Beginn des Plans, Aufwärmen eingeschlossen; auch `duration_us` schließt das Aufwärmen ein.
`in_flight` einer Sekunde ist, wie viele Aufrufe an ihrem Ende unterwegs waren. Die Codes in
`failure_codes` sind kanonische Namen (`UNAVAILABLE`). Zum Abgleich: Die Summen des Laufs sind die
Summe der Methoden, und über die Sekunden einer Methode ist `Σ begun` plus `outside_timeline` jeder
Aufruf der Methode, Aufwärmen eingeschlossen.

Entscheidungen sind Felder, kein Text: `invalid_reasons` (`in_flight_cap`, `nothing_measured`,
`clock_step`), `methods[].invalid_reason` (`request_error`, `client_error`, `bad_response`, `mixed`
oder `null`), `tail_wait_cause` (`generator`, `stream`, `connection` oder `null`) und die Zahlen je
Ursache in `client_waits`. `notes` und `unchecked[].error` sind Text für Menschen und werden frei
umformuliert: Werten Sie sie nicht aus.

Das Schweigen des Ziels: `methods[].silent_from_s` ist die Sekunde, ab der das Ziel keinen der
gesendeten Aufrufe beantwortet hat, `methods[].silent_sent_rps`, wie viele Aufrufe in der Sekunde
davor rausgingen, `methods[].silent_planned_rps_low` und `_high` die geplante Rate der Stufen in
dieser Sekunde. Ohne Schweigen sind alle `null`; die geplante Rate ist auch `null`, wenn in dieser
Sekunde keine Stufe lief. `planned_rps_low` und `_high` sind die Rate des ganzen Plans.

Der Textbericht wird nur in ASCII ausgegeben. Zeichen außerhalb von ASCII, in einem Methodennamen
oder im Fehlertext des Ziels, werden als `\uXXXX` ausgegeben (jenseits von U+FFFF als
Surrogatpaar, wie in JSON). `notes` im JSON tragen denselben maskierten Text.

## Noch nicht

Hochfahren von null auf die Ziel-RPS, Pass/Fail-Schwellen, Export nach Prometheus. Was kommt und in
welcher Reihenfolge — **[der Plan](roadmap.md)**; was Ihnen fehlt — [ein Issue](https://github.com/yhgrwav/leettest/issues/new/choose).

## Mehr erfahren

**[Referenz →](reference.md)** — jedes Konfigurationsfeld und Flag.

| Frage | |
|---|---|
| Welches Problem löst LeetTest? | [Lesen](problem.md) |
| Warum dieses Werkzeug? | [Lesen](why.md) |
| Welche Probleme von Lasttests behebt es? | [Lesen](pitfalls.md) |
| Was ist Verkettung von Aufrufen, und wozu? | [Lesen](chaining.md) |
| Wie sieht die kommerzielle Nutzung aus? | [Lesen](commercial.md) |
| Wo stelle ich Fragen oder gebe Feedback? | [Lesen](feedback.md) |
| Was kommt als Nächstes? | [Lesen](roadmap.md) |

**[Vergleich mit ghz auf einem Stand →](compare-ghz.md)** — Tabellen und der Befehl zum Nachstellen.

## Mitwirken

Issues und Diskussionen sind willkommen. Pull Requests werden nach Unterzeichnung des
[CLA](../../CLA.md) angenommen — eine Zeile als Kommentar im PR, automatisch geprüft. Wie man
anfängt — in [CONTRIBUTING.md](../../CONTRIBUTING.md): ein Weg für Menschen und einer mit einem
KI-Agenten, und für den Agenten [AGENTS.md](../../AGENTS.md). Das CLA ist eine Lizenz, keine
Übertragung von Rechten: Das Urheberrecht bleibt bei Ihnen.

## Lizenz

Apache License 2.0 — siehe [LICENSE](../../LICENSE).
