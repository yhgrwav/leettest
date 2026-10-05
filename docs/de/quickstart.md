> Übersetzt aus [docs/ru/quickstart.md](../ru/quickstart.md) bei 9a08876, 2026-10-05. Bei
> Abweichungen gilt die russische Fassung.

# Schnellstart

Ein vollständiger Durchgang durch LeetTest in einer halben Stunde: von der Installation bis zu einem
Bericht, den man den Entwicklern des Dienstes geben kann, und einem Lauf in CI. Jedes Feld und Flag
steht in der [Referenz](reference.md), das Lesen des Berichts im [README](README.md); hier sieht
man, wie sie zusammenspielen.

Arbeiten Sie mit einem KI-Agenten? Geben Sie ihm [AGENTS.md](../../AGENTS.md): dasselbe, in der
Form, die ein Agent braucht — ein Lauf ohne Terminal, JSON, Exit-Codes.

## Wo man es ausführt

LeetTest ist für **Linux und macOS** gebaut: Der Generator steht neben dem Ziel — ein Server im
selben Netz, ein Container, ein CI-Runner. Je näher, desto weniger fremdes Netz steckt in den
Zahlen.

Windows wird unterstützt, misst aber schlechter: Die Uhr springt in Schritten von etwa 0,5 ms, und
ein Lauf gegen einen schnellen Dienst wird ehrlich für ungültig erklärt (`clock_step`, Exit-Code 2)
— der Uhrenschritt ist so groß wie die Latenz selbst. Messen Sie unter Linux; zum Ausprobieren
reicht Windows.

## 1. Installation

```console
$ go install github.com/yhgrwav/leettest/cmd/leettest@latest
$ leettest -version
```

Go 1.26 oder neuer. Fertige Binärdateien für Linux, macOS und Windows (amd64 und arm64) liegen auf
der Release-Seite.

## 2. Ein Ziel: der Referenz-Stand

Für den Rundgang brauchen Sie keinen eigenen Dienst: Das Repository enthält einen Referenz-Stand,
einen gRPC-Dienst, dessen Verhalten Sie festlegen. Er beantwortet `grpc.health.v1.Health/Check` und
`wallet.v1.WalletService` und bietet Reflection an, wie ein echter Dienst.

```console
$ git clone https://github.com/yhgrwav/leettest && cd leettest
$ go run ./test/stand/cmd/stand -delay 20ms -hang-from 25s -life 60s
```

Der Stand antwortet in 20 ms, und 25 Sekunden nach dem ersten Aufruf antwortet er gar nicht mehr —
so sieht ein Dienst aus, der unter Last umgefallen ist. Die richtige Antwort ist vorher bekannt,
also sieht man, ob der Bericht die Wahrheit sagt.

## 3. Die Konfiguration

[`examples/tour.yaml`](../../examples/tour.yaml) nutzt jedes Feld, das ein lokaler Stand prüfen
kann:

```yaml
name: tour

app:
  target:
    ip: 127.0.0.1
    port: 50051
  tls: false
  metadata:
    authorization: Bearer ${LEETTEST_TOKEN}
    x-request-source: leettest-tour
  max_response_size: 1MiB

load:
  warmup: 3s
  calls:
    - method: grpc.health.v1.Health/Check
      rps: 300
      duration: 40s
      timeout: 500ms
      data:
        service: wallet.v1.WalletService
    - method: wallet.v1.WalletService/GetBalance
      rps: 50
      duration: 40s
      timeout: 500ms
      data:
        wallet_id: w-42
```

- **`name`** — der Name des Laufs in der Kopfzeile.
- **`tls: false`** — der Stand hat keine Verschlüsselung. TLS ist standardmäßig an; `ca`, `cert`,
  `key`, `server_name` stehen in [`examples/tour-tls.yaml`](../../examples/tour-tls.yaml) (unten,
  „TLS").
- **`metadata`** — Header bei jedem Aufruf. `${LEETTEST_TOKEN}` kommt aus der Umgebung: Der Token
  steht weder in der Konfiguration noch in der Shell-Historie, und LeetTest gibt Header-Werte
  nirgends aus. Eine nicht gesetzte Variable ist ein Fehler vor dem Start, kein Lauf mit leerem
  Token.
- **`max_response_size`** — eine Antwort über dem Limit ist eine `bad response`, kein Erfolg.
- **`warmup`** — in den ersten 3 Sekunden gehen Aufrufe ans Ziel, bleiben aber aus den Perzentilen.
- **`rps`, `duration`** — jede Methode hat ihre eigenen. Open Model: Ein Aufruf geht nach Plan raus,
  auch wenn frühere noch nicht geantwortet haben, und die Latenz zählt ab dem geplanten Zeitpunkt.
  Ein langsamer Dienst bremst weder die Last, noch versteckt er seine Langsamkeit.
- **`timeout`** — wie lange auf eine Antwort gewartet wird (standardmäßig 2 s).
- **`data`** — der Anfrage-Body als einfaches YAML. Das Schema kommt über Reflection vom Dienst,
  kein `.proto`. Ein Fehler im Body zeigt sich vor dem Start.

Mehrere Methoden sind mehrere Aufrufe, jeder mit eigener Rate. Eine Methode, ein Aufruf: Der Bericht
ist je Methode.

## 4. Ausführen

```console
$ export LEETTEST_TOKEN=demo
$ leettest -c examples/tour.yaml
```

Vor dem Start verbindet sich LeetTest mit dem Ziel, prüft jede Methode über Reflection, baut den
Anfrage-Body und prüft, dass die In-Flight-Grenze (`-max-in-flight`) ein hängendes Ziel aushält.
Alles, was sich vor dem Lauf wissen lässt, ist ein Fehler vor dem ersten Aufruf.

Im Terminal bekommen Sie die Live-Ansicht: RPS, laufende Aufrufe, Fehler und Perzentile pro Sekunde,
ein Reiter je Methode, `?` für die Hilfe. **Anhalten:** Das erste `q` (oder Ctrl+C) sendet keine
neuen Aufrufe und lässt die gesendeten ihr Timeout abwarten; das zweite schneidet sie ab und gibt
trotzdem den Bericht aus; das dritte beendet ohne Bericht.

Ohne Terminal (CI, Ausgabe in eine Datei) gibt es stattdessen eine Fortschrittszeile pro Sekunde auf
stderr.

**TLS.** Mit `-mtls` schreibt der Stand beim Start ein CA, sein Zertifikat und ein Client-Zertifikat
nach `test/stand/certs` und verlangt das Client-Zertifikat;
[`examples/tour-tls.yaml`](../../examples/tour-tls.yaml) zeigt auf diese Dateien:

```console
$ go run ./test/stand/cmd/stand -mtls -delay 20ms -life 30s
$ leettest -c examples/tour-tls.yaml
```

## 5. Der Bericht

Der Bericht geht nach stdout, nur ASCII — speichern Sie ihn in eine Datei oder durchsuchen Sie ihn
mit `grep`. Der Rundgang oben endet so:

```
run finished: 127.0.0.1:50051 in 40.5s
sent 12950, failed 5248

method                                           sent   failed    sent/s       p50       p90       p95       p99
grpc.health.v1.Health/Check                     11100     4499       300    20.5ms    >500ms    >500ms    >500ms
wallet.v1.WalletService/GetBalance               1850      749        50    20.5ms    >500ms    >500ms    >500ms

warm-up 1050 sent, excluded from stats

grpc.health.v1.Health/Check: at 300 rps, 4499 of 11100 calls (40.5%) got no answer within 500ms,
and nothing after the call sent at 25.0s of the run got one.
...
failed calls by gRPC code:
grpc.health.v1.Health/Check codes sent by the target: DeadlineExceeded 2
grpc.health.v1.Health/Check codes set by the client: DeadlineExceeded 4497
...
start lag, how late calls began against their schedule: p99 109us, max 225us.
...
5248 requests were abandoned before answering. A percentile shown as "> value"
is a lower bound: the real tail lies above it. Raise the timeout to see it.
```

(Linux, Docker, 9a08876.)

Worauf es ankommt:

- **Bei welcher Last der Dienst nicht mehr mithielt.** „at 300 rps … nothing after the call sent at
  25.0s" — das Ziel verstummte in Sekunde 25, genau wann es dem Stand befohlen war. Der Zeitpunkt
  ist, wann der Aufruf rausging, nicht wann er fällig war: Ein nachhinkender Generator wird nicht
  als Schweigen des Ziels ausgegeben.
- **`>500ms`** ist keine Zahl, sondern eine Untergrenze: Manche Aufrufe bekamen keine Antwort, und
  ihre wirkliche Latenz liegt über dem Timeout. LeetTest setzt nicht das Timeout an die Stelle
  eines unbekannten Werts.
- **Codes auf zwei Zeilen.** „sent by the target" — der Status kam vom Ziel (oder einem Proxy davor);
  „set by the client" — unser Client hat ihn gesetzt, es kam keine Antwort. Zwei verschiedene
  Geschichten für die Entwickler.
- **`start lag`** — wie weit der Generator hinter seinem Plan zurücklag. Ist er groß, ist der Engpass
  die Maschine des Generators, nicht das Ziel; der Bericht sagt das selbst, wenn das Ende der
  Latenzverteilung auf unserer Seite wartet.
- **`clock step`** — wie genau die Uhr der Maschine ist; unter Linux sind es Nanosekunden, und die
  Zeile fehlt. Unter Windows (etwa 0,5 ms) erscheint sie, und ist sie gröber als ein Viertel des
  p50, ist der Lauf ungültig: Zahlen unter dem Uhrenschritt bedeuten nichts.

Die Fehlerkategorien (`overload`, `failure`, `request error`, `timed out`, `cut off`, `unreachable`
und andere) und ihre Bedeutung stehen im
[README, „Den Bericht lesen"](README.md#den-bericht-lesen).

## 6. Andere Verhaltensweisen des Ziels

Dieselbe Konfiguration gegen den Stand in einem anderen Modus — was der Bericht zeigt:

```console
$ go run ./test/stand/cmd/stand -delay 20ms -freeze-at 10s -freeze-for 2s -life 60s
```
Ein 2-Sekunden-Stillstand bei 500 ms Timeout: p50 und p90 bleiben um 21 ms, und die Aufrufe, die
während der Pause ankamen, bekamen keine Antwort — etwa 4 % `got no answer within 500ms`, p99 als
`>500ms` ausgegeben. Perzentile werden nicht gemittelt: Ein kurzer Stillstand zeigt sich im Ende der
Verteilung, nicht verschmiert in einem Mittelwert.

```console
$ go run ./test/stand/cmd/stand -delay 20ms -fail-every 10 -life 60s
```
Jeder zehnte Aufruf ist `RESOURCE_EXHAUSTED`: Kategorie `overload`, sein Code auf der Zeile „sent
by the target".

```console
$ go run ./test/stand/cmd/stand -delay 150ms -max-streams 1 -life 60s
```
Das Ziel erlaubt einen Stream je Verbindung und hält jede Antwort 150 ms: Über eine Verbindung kann
es höchstens 6–7 Aufrufe pro Sekunde beantworten. Der Bericht sagt, wo sie hängen blieben:
`connections: 1; target stream limit 1` und `not sent N: waited for a stream N` — Aufrufe warteten
auf unserer Seite auf einen Stream und erreichten das Ziel nicht vor ihrem Timeout. Das ist die
Grenze der Verbindung, kein langsames Ziel.

## 7. Skripte und CI

```console
$ leettest -c load.yaml -output json > report.json
$ echo $?
```

Das JSON ist versioniert (`schema_version`); Entscheidungen sind Felder, kein Text:
`invalid_reasons`, `methods[].invalid_reason`, `tail_wait_cause`, `methods[].silent_from_s`.
Beschrieben sind sie im [README, „JSON für Skripte und CI"](README.md#json-für-skripte-und-ci).

Exit-Codes:

| Code | Bedeutung |
|---|---|
| `0` | Der Plan lief durch, der Bericht ist vollständig |
| `1` | Kein Lauf: Flags, Konfiguration, Verbindung. Kein Bericht |
| `2` | Ungültiger Lauf: die In-Flight-Grenze, jeder Aufruf einer Methode ein Anfragefehler oder eine Host-Uhr gröber als ein Viertel des p50. Es gibt einen Bericht; seine Zahlen handeln nicht von der Last |
| `3` | Vor dem Ende des Plans angehalten. Der Bericht deckt ab, was lief |
| `130`, `143` | Durch Ctrl+C oder SIGTERM beendet, kein Bericht |

Pass/Fail-Schwellen gibt es noch nicht: Prüfen Sie in CI den Exit-Code und die JSON-Felder.

## 8. Ganz ohne Dienst

```console
$ leettest -c examples/leettest.yaml -fake -fake-delay 30ms -fake-jitter 10ms
```

`-fake` lädt statt eines Dienstes ein eingebautes Scheinziel — um Bildschirm und Bericht ohne Dienst
anzusehen. Der Bericht ist als `fake target` markiert.

## Weiter

- [Referenz](reference.md) — jedes Feld und Flag; [README](README.md) — den Bericht lesen.
- [Fallstricke](pitfalls.md) — was Messungen kaputt macht.
- [CONTRIBUTING](../../CONTRIBUTING.md) — eine Änderung vorschlagen, als Mensch oder mit einem
  KI-Agenten.
