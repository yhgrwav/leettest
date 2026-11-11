# Der erste Lauf

[← Zur Dokumentation](../README.md)

> Übersetzt aus [docs/ru/guide/first-run.md](../../ru/guide/first-run.md) bei feb2d1d, 2026-11-10. Bei
> Abweichungen gilt die russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../../en/guide/first-run.md).

Hier steht der Weg von der Installation bis zum ersten Bericht: LeetTest installieren, eine
Konfiguration für zwei Methoden schreiben, starten und das Ergebnis lesen. Ein eigener Dienst ist
dafür nicht nötig: Unten steht ein Testdienst, der zu LeetTest gehört. Wenn Sie mit einem KI-Agenten
arbeiten, geben Sie ihm [docs/agent.md](../../agent.md) (der Satz für ihn steht in der
[README](../README.md#mit-ihrem-ki-agenten)): Er macht dieselben Schritte selbst.

## Wo man startet

Starten Sie unter Linux oder macOS neben dem Dienst: im selben Netz, im selben Cluster oder auf dem
CI-Runner. Alles, was zwischen LeetTest und dem Dienst liegt, geht in die Latenz ein und sieht aus
wie die Zeit des Dienstes: Bei 1000 Anfragen pro Sekunde war p99 über die Portweiterleitung von
Docker Desktop 38 ms, innerhalb des Docker-Netzes 2 ms (unser Teststand, eine Maschine; unter
anderen Bedingungen sind die Zahlen anders).

Windows genügt zum Ausprobieren. Auf unserer Windows-11-Maschine war der Uhrenschritt etwa 0,5 ms
(LeetTest misst ihn selbst und druckt ihn), und einen Lauf gegen einen schnellen Dienst kann
LeetTest für ungültig erklären (`clock_step`, Exit-Code 2): Der Uhrenschritt ist mit der Latenz
selbst vergleichbar. Für Messungen nehmen Sie Linux. Mehr zum Ort des Starts — in der
[Referenz](../../en/reference.md#where-to-run) (englisch).

## Installation

**Linux und macOS** (amd64 und arm64). Nötig sind `curl` und `tar`:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
```

Das Skript installiert das neueste Release nach `~/.local/bin` und gleicht die Prüfsumme ab. Liegt
dieses Verzeichnis nicht im `PATH`, druckt es die Zeile für das Shell-Profil: Führen Sie sie in
diesem Terminal aus, und um sie zu behalten, tragen Sie sie ins Profil ein. Ein anderes Verzeichnis —
die Variable `LEETTEST_INSTALL_DIR`, eine andere Version — `LEETTEST_VERSION`; die Variable steht
vor `sh`, nicht vor `curl`:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | LEETTEST_VERSION=v0.2.0 sh
```

**Windows.** Das `.zip`-Archiv von der [Release-Seite](https://github.com/yhgrwav/leettest/releases/latest):
Entpacken Sie `leettest.exe` in ein Verzeichnis aus dem `PATH`.

**Jedes Betriebssystem mit Go 1.26 oder neuer** ([Go installieren](https://go.dev/doc/install); ein
älteres Go mit `GOTOOLCHAIN=auto` lädt das nötige selbst herunter):

```sh
go install github.com/yhgrwav/leettest/cmd/leettest@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

Die Prüfung für jeden Weg:

```sh
leettest -version
```

## Der Start

Speichern Sie in `leettest.yaml`, mit Adresse und Methoden Ihres Dienstes:

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

- `tls: false` — ein Dienst ohne Verschlüsselung. Ohne diese Zeile ist TLS an.
- `method` — der volle Name: `paket.Dienst/Methode`. Ein `.proto` ist nicht nötig: Das Schema holt
  LeetTest über gRPC Server Reflection vom Dienst (der Dienst gibt die Liste seiner Methoden und
  Nachrichten selbst heraus). Ist Reflection am Dienst abgeschaltet, siehe [Verbindung](connection.md).
- `rps` — Anfragen pro Sekunde für diese Methode, `duration` — wie lange sie belastet wird.
- `warmup` — die ersten Sekunden gehen an den Dienst, zählen aber nicht in die Statistik.

Die Konfiguration ohne Last prüfen: LeetTest verbindet sich, prüft die Methoden, schickt je Methode
eine Anfrage und zeigt Anfrage und Antwort. Einen Bericht gibt es nicht.

```sh
leettest -c leettest.yaml -dry-run
```

Der Start:

```sh
leettest -c leettest.yaml
```

Vor dem Start verbindet sich LeetTest mit dem Dienst und prüft jede Methode und jeden Body der
Anfrage. Ein Fehler in der Konfiguration, eine unbekannte Methode, ein Fehler im Body der Anfrage,
ein `Unauthenticated` bei gesetzten Headern — Exit-Code 1 und keine einzige Anfrage an den Dienst.
Die Antwort `PermissionDenied` stoppt den Lauf nicht, Einzelheiten stehen in
[Verbindung](connection.md#was-im-bericht-erscheint).

Im Terminal läuft ein Live-Bildschirm: Anfragen pro Sekunde, Anfragen ohne Antwort, Fehler und
Perzentile nach Sekunden, ein Reiter je Methode, `?` — die Hilfe. Das Beenden: das erste `q` (oder
Strg+C) — neue Anfragen gehen nicht mehr raus, die gesendeten warten auf ihr Timeout; das zweite —
bricht sie ab, der Bericht wird trotzdem gedruckt; das dritte — Ausstieg ohne Bericht. Ohne Terminal
(CI, Ausgabe in eine Datei, ssh) gibt es statt des Bildschirms eine Fortschrittszeile pro Sekunde
auf stderr; `-plain` schaltet sie auch im Terminal ein:

```text
32.0s  sent 25600  rps 800  in-flight 47  failed 51  not-sent 0  p99 43ms
```

`in-flight` — Anfragen, die gesendet wurden und noch keine Antwort haben; `not-sent` — Anfragen, die
nach Plan hätten rausgehen sollen, aber nicht rausgingen (zum Beispiel weil sie auf eine freie
Verbindung warteten).

Der Bericht geht nach stdout, Fortschritt und Fehler nach stderr. Der erste Start im Terminal fragt
nach dem Farbschema (einmal; `-plain` überspringt die Frage).

## Was im Bericht steht

Am Ende steht eine Zeile je Methode: wie viel gesendet wurde, wie viele Fehlschläge, `sent/s` und
die Perzentile p50, p90, p95, p99. Das Wichtigste:

- Die Latenz wird vom **geplanten** Zeitpunkt der Anfrage an gerechnet. Hat der Dienst oder
  LeetTest selbst die Anfrage verzögert, geht diese Verzögerung in die gemessene ein. Die Anfrage
  geht nach Plan raus, ohne auf die Antworten der vorigen zu warten.
- `>2.0s` ist keine Zahl, sondern „nicht weniger als 2 Sekunden“: Ein Teil der Anfragen brach per
  Timeout ab, und ihre echte Latenz liegt höher. LeetTest setzt das Timeout nicht anstelle des
  unbekannten Wertes ein.
- Die Fehler sind nach Kategorien aufgeteilt (`overload`, `failure`, `timed out` und andere), und
  unter dem Bericht stehen die gRPC-Codes in zwei Zeilen: „sent by the target“ — den Status schickte
  der Dienst oder ein Proxy davor, „set by the client“ — den Status setzte LeetTest, eine Antwort
  gab es nicht.
- Der Exit-Code `0` bedeutet nicht „der Dienst ist gesund“: Er bedeutet, dass der Plan durchgelaufen
  ist. Damit der Lauf selbst ein Urteil „bestanden / nicht bestanden“ fällt, setzen Sie
  [Grenzen](../../en/report.md#pass-limits-a-run-as-a-check) (englisch).

Jede Zahl und jede Kategorie steht im Abschnitt
[Den Bericht lesen](../../en/report.md#reading-the-report) (englisch).

## Ein Probelauf gegen den Testdienst

Der Testdienst liegt im Repository: ein gRPC-Dienst mit steuerbarem Verhalten. Er antwortet auf
`grpc.health.v1.Health/Check` und `wallet.v1.WalletService` und gibt Reflection heraus wie ein
echter. Die richtige Antwort steht vorher fest, deshalb lässt sich der Bericht mit dem abgleichen,
was der Dienst tatsächlich getan hat. Nötig sind `git` und Go 1.26+; Testdienst und `leettest` aus
demselben Klon passen in der Version zu seinen Konfigurationen.

```sh
git clone --depth 1 https://github.com/yhgrwav/leettest && cd leettest
go build -o bin/ ./cmd/leettest ./test/stand/cmd/stand
bin/stand -delay 20ms -hang-from 25s -life 3m > stand.log 2>&1 &
sleep 1
```

Der Dienst läuft im Hintergrund (`sleep 1` — eine Sekunde, damit er auf dem Port zu lauschen
beginnt) und hält nach 3 Minuten von selbst an (`-life 3m`: Warm-up, 40 Sekunden Lauf und Reserve).
Er antwortet in 20 ms, und 25 Sekunden nach der ersten Anfrage hört er ganz auf zu antworten — so
sieht ein Dienst aus, der unter der Last gefallen ist.

Die Konfiguration [`examples/tour.yaml`](../../../examples/tour.yaml) verwendet alle Felder, die sich
gegen einen lokalen Dienst prüfen lassen:

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
  connections: 1

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
      dataset: wallets.jsonl
      dataset_order: random
      dataset_seed: 42
```

- `name` — wie der Lauf im Kopf und im JSON-Bericht heißt.
- `metadata` — Header jeder Anfrage. `${LEETTEST_TOKEN}` wird aus der Umgebung genommen: Das Token
  landet weder in der Konfiguration noch in der Shell-Historie; die Werte der Header gelangen nicht
  in den Bericht. Ist die Variable nicht gesetzt — Fehler vor dem Start, kein Lauf mit leerem Token.
- `max_response_size` — eine Antwort über dem Limit gilt als `bad response`, nicht als Erfolg.
- `timeout` — wie lange auf die Antwort gewartet wird (Standard 2 s).
- `data` — der Body der Anfrage als gewöhnliches YAML. Ein Fehler im Body zeigt sich vor dem Start.
- `dataset` — eine Datei mit Bodys der Anfragen statt `data`, ein JSON je Zeile
  ([`examples/wallets.jsonl`](../../../examples/wallets.jsonl)): Die erste Anfrage geht mit der
  ersten Zeile raus, die zweite mit der zweiten, nach der letzten wieder mit der ersten.
  `dataset_order: random` mischt die Datei bei jedem Durchgang; der Bericht druckt den Seed, und
  `dataset_seed` wiederholt die Reihenfolge. Neben der Zeile der Methode sagt der Bericht, wie viel
  von der Datei benutzt wurde.
- `connections` — wie viele Verbindungen die Last tragen (Standard 1). Hinter einem L4-Balancer
  belastet eine Verbindung einen Server; bei zwei und mehr erscheint im Bericht der Block
  `Connections:` — eine Zeile je Verbindung.

Mehrere Methoden — mehrere Einträge in `calls`, jeder mit eigenem Tempo. Eine Methode — ein Eintrag.

```sh
export LEETTEST_TOKEN=demo
bin/leettest -c examples/tour.yaml
```

`demo` ist das Token für diese Tour. Der Lauf endet so:

```text
run finished: 127.0.0.1:50051 in 40.5s
name: tour
settings: warm-up 3s, max in flight 5000, connections 1
sent 12950, failed 5248

method                                           sent   failed    sent/s       p50       p90       p95       p99
grpc.health.v1.Health/Check                     11100     4499       300    20.5ms    >500ms    >500ms    >500ms
  plan: 300 rps for 40s
wallet.v1.WalletService/GetBalance               1850      749        50    20.5ms    >500ms    >500ms    >500ms
  plan: 50 rps for 40s
  data: 4 of 4 requests from wallets.jsonl, each used up to 500 times, random order, seed 42

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

(Linux, Docker, 9a08876.) Das Wichtigste daran:

- **Was für ein Lauf das ist.** `name:` und `settings:` wiederholen den Namen des Laufs und womit er
  gestartet wurde, `plan:` unter der Methode — das gesetzte Tempo und die Dauer: Ein Bericht, den
  man in ein Ticket einfügt, lässt sich mit einem anderen vergleichen.
- **Bei welcher Last der Dienst nicht mehr mithielt.** „at 300 rps … nothing after the call sent at
  25.0s“ — der Dienst verstummte in der 25. Sekunde, genau wie ihm befohlen. Der Zeitpunkt wird nach
  dem Senden der Anfrage gerechnet, nicht nach dem Plan: Die Verspätung von LeetTest wird nicht als
  Stille des Dienstes ausgegeben.
- **`>500ms`** — „nicht weniger als 500 ms“: Ein Teil der Anfragen wartete nicht auf die Antwort.
- **`start lag`** — wie sehr LeetTest hinter dem Plan zurücklag. Ist er groß, ist der Engpass die
  Maschine, auf der LeetTest läuft, nicht der Dienst; der Bericht sagt selbst, wann der Schwanz der
  Latenz durch Verzögerungen in LeetTest erklärt wird, nicht im Dienst.
- **`clock step`** — die Genauigkeit der Uhr der Maschine. Unter Linux liegt sie in Nanosekunden, und
  die Zeile fehlt. Unter Windows (bei uns etwa 0,5 ms) erscheint die Zeile, und ist der Schritt
  gröber als ein Viertel von p50, ist der Lauf ungültig: Zahlen unter dem Uhrenschritt bedeuten nichts.

Dieselbe Konfiguration gegen den Testdienst in einem anderen Modus. Jeder Block hält zuerst den
vorigen Dienst an (`kill %%` beendet den letzten Hintergrundbefehl dieses Terminals), dann startet
er den neuen und den Lauf; das Token `LEETTEST_TOKEN` ist schon gesetzt.

Der Dienst friert bei einem Timeout von 500 ms für 2 Sekunden ein:

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -freeze-at 10s -freeze-for 2s -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```

p50 und p90 bleiben bei etwa 21 ms, und die Anfragen, die in die Pause fielen, warteten nicht auf
die Antwort: etwa 4 % `got no answer within 500ms`, p99 wird als `>500ms` gedruckt. Die Perzentile
werden nicht gemittelt: Ein kurzes Einfrieren zeigt sich im Schwanz, es wird nicht über den
Mittelwert verschmiert.

Jede zehnte Antwort ist ein Fehler `RESOURCE_EXHAUSTED`:

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -fail-every 10 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```

Die Kategorie `overload`, der Code in der Zeile „sent by the target“.

Der Dienst erlaubt einen Stream je Verbindung, und jede Antwort dauert 150 ms:

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 150ms -max-streams 1 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```

Über eine Verbindung schafft der Dienst nicht mehr als 6–7 Anfragen pro Sekunde. Der Bericht sagt,
wo sie hängen blieben: `connections: 1; target stream limit 1` und `not sent N: waited for a
stream N` — die Anfragen warteten in LeetTest auf einen freien Stream und gingen vor ihrem Timeout
nicht zum Dienst. Das ist eine Grenze der Verbindung, kein langsamer Dienst.

Mit TLS: Der Testdienst mit `-mtls` (mTLS — gegenseitiges TLS: Der Dienst prüft nicht nur sein
Zertifikat für den Client, sondern auch das Zertifikat des Clients) schreibt beim Start Zertifikate
nach `test/stand/certs` und verlangt ein Client-Zertifikat;
[`examples/tour-tls.yaml`](../../../examples/tour-tls.yaml) verweist auf diese Dateien.

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -mtls -delay 20ms -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour-tls.yaml
```

Ganz ohne Dienst: `-fake` belastet eine eingebaute Attrappe statt des Dienstes. Die Konfiguration
kommt aus dem Klon, und `target` darin wird ignoriert. Der Bericht ist mit `fake target` markiert.

```sh
bin/leettest -c examples/leettest.yaml -fake -fake-delay 30ms -fake-jitter 10ms
```

## Häufige Fehler

- **`0` als „der Dienst ist gesund“ verstanden.** Der Code `0` heißt „der Plan ist durchgelaufen“.
  Die Gesundheit prüfen die `pass`-Grenzen und das Lesen des Berichts.
- **Ein Dienst ohne Verschlüsselung, aber kein `tls: false`.** TLS ist standardmäßig an, und die
  Verbindung zu einem Dienst ohne Verschlüsselung scheitert vor dem Start mit Code 1.
- **Die Methode ist nicht vollständig geschrieben.** Nötig ist `paket.Dienst/Methode`; einen
  Tippfehler findet LeetTest vor der ersten Anfrage.
- **Reflection am Dienst ist abgeschaltet.** Eine Methode mit `data` startet nicht; das Schema lässt
  sich als `.proto`-Datei oder Protoset (ein Satz von Beschreibungen der Methoden in einer Datei)
  geben — [Verbindung](connection.md).
- **Code 2 und „clock step“ unter Windows.** Starten Sie von Linux aus, neben dem Dienst.
- **Start über die Portweiterleitung von Docker Desktop.** Die Latenz enthält dann diesen Weg.

## Weiter

- [Verbindung](connection.md) — TLS, Header, Token.
- [Last](load.md) — Methoden, Tempo, Anlauf, Body der Anfrage.
- [Referenz](../../en/reference.md) (englisch) — jedes Feld und jedes Flag.
- [Stolpersteine](../pitfalls.md) — was Messungen verdirbt.
