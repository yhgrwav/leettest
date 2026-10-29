<div align="center">

<h1><img src="../../assets/logo.png" width="360" alt="LeetTest"></h1>

**Finden Sie heraus, wo Ihr gRPC-Dienst bricht — mit Zahlen, denen Sie trauen können.**

[Русский](../ru/) · [English](../en/) · [Deutsch](../de/) · [简体中文](../zh-CN/)

[![CI](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/yhgrwav/leettest.svg)](https://pkg.go.dev/github.com/yhgrwav/leettest)
[![Go version](https://img.shields.io/github/go-mod/go-version/yhgrwav/leettest)](../../go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](../../LICENSE)

</div>

---

> Übersetzt aus [README.md](../../README.md) bei e4663f1, 2026-10-27. Bei Abweichungen gilt die
> russische Fassung. Dazu ein Verzeichnis der deutschen Dokumentation, das im Original fehlt.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/README.md).

Das Werkzeug beantwortet die Frage, mit der man zu einem Lasttest kommt: **ab welcher Last hält der
Dienst nicht mehr mit, und wo liegt der Engpass.** Es erzeugt Last wie in der Produktion — mehrere
Methoden gleichzeitig, jede mit eigener RPS — und misst so, dass die Zahlen nicht lügen, während der
Dienst degradiert. Kein `.proto`, kein Codegen, keine Skripte.

Prioritäten, der Reihe nach: Die Zahlen lügen nicht; das Werkzeug ist angenehm zu benutzen — eine
unklare Fehlermeldung ist ebenso ein Defekt wie eine falsche Zahl; der Generator wird nie zum
Engpass.

Das Projekt entwickelt sich: Was heute funktioniert, steht unten, was als Nächstes kommt, im [Plan](roadmap.md).

So sieht ein Lauf aus: Mitten in der Last friert der Dienst drei Sekunden lang ein — auf dem
Bildschirm sammeln sich die laufenden Aufrufe, und der Bericht zählt die, die ohne Antwort blieben.

<img src="../../assets/demo.gif" width="100%" alt="Ein LeetTest-Lauf im Terminal: der Lastbildschirm, das Ziel friert drei Sekunden ein, der Bericht zeigt die Aufrufe ohne Antwort und ein p99 über 2 Sekunden">

Welche Fragen LeetTest beantwortet — [als Liste](questions.md).

## Installation

Linux und macOS (amd64 und arm64) — mit einem Befehl:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
```

Das Skript installiert das neueste Release nach `~/.local/bin`, prüft die Prüfsumme und gibt, wenn
dieses Verzeichnis nicht im `PATH` liegt, die Zeile für das Shell-Profil aus. Für Windows — das
Archiv von der [Release-Seite](https://github.com/yhgrwav/leettest/releases/latest). Oder mit Go 1.26+:

```sh
go install github.com/yhgrwav/leettest/cmd/leettest@latest
```

Prüfen: `leettest -version`. Wird der Befehl nach `go install` nicht gefunden —
`export PATH="$PATH:$(go env GOPATH)/bin"`.

**Wo man es ausführt:** unter Linux neben dem Ziel — im selben Netz, im selben Cluster oder auf einem
CI-Runner. Alles zwischen Generator und Ziel landet in der Latenz. Mehr in der
[Referenz](reference.md#wo-man-es-ausführt).

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

```sh
leettest -c leettest.yaml
```

Der Methodenname ist der volle, `package.Service/Method`. Das Werkzeug holt das Schema der Methode
über gRPC Server Reflection vom Dienst, deshalb ist kein `.proto` nötig. Ein Dienst ohne Reflection
(ab v0.3): Das Schema kommt aus `.proto`-Dateien oder einem Protoset —
[`examples/proto.yaml`](../../examples/proto.yaml), das Feld `app.proto` / `app.protoset` in der
[Referenz](reference.md). Konfiguration, Adresse,
Methoden und Anfrage-Body werden vor dem Start geprüft: Jeder Fehler heißt Exit-Code 1 und kein
einziger Aufruf ans Ziel. Die Konfiguration vor der Last prüfen:
`leettest -c leettest.yaml -dry-run` — eine Anfrage pro Aufruf, Anfrage und
Antwort auf dem Bildschirm, kein Bericht. Die ersten `warmup` Sekunden zählen nicht zur Statistik.
Das Hochfahren von null auf `rps` ist `load.ramp: 30s`
([`examples/ramp.yaml`](../../examples/ramp.yaml)): Ein Lauf dauert `ramp` + `duration`, auch die
Aufrufe des Anlaufs bleiben aus der Statistik (die `pass`-Grenzen zählen die Haltephase), und
`warmup` zählt, anders als der Anlauf, zu `duration`. Jeder Aufruf einer
Methode geht mit demselben Body raus: Bei einem Schreibvorgang mit Idempotenzschlüssel wird der Weg
der Wiederholung gemessen, nicht das Anlegen des Datensatzes. Eine andere Anfrage bei jedem Aufruf —
eine Datei in `dataset`: ein JSON je Zeile, der Reihe nach und im Kreis (`dataset_order: random` —
in zufälliger Reihenfolge, der Seed im Bericht wiederholt sie). Alle
Felder, TLS, Header, der Anfrage-Body und die Flags stehen in der **[Referenz](reference.md)**.

Im Terminal läuft der Lauf im Vollbild, `q` zum Anhalten. Ohne Terminal (CI, umgeleitete Ausgabe) —
eine Fortschrittszeile pro Sekunde:

```
32.0s  sent 25600  rps 800  in-flight 47  failed 51  not-sent 0  p99 43ms
```

Der Bericht geht nach stdout, Fortschritt und Fehler nach stderr. Ohne Bildschirm (zum Beispiel über
ssh): `-plain`.

## Mit Ihrem KI-Agenten

Wenn Sie die Konfiguration nicht selbst schreiben wollen, geben Sie Ihrem Agenten den Link: Er
installiert LeetTest, fragt nach Ihrem Dienst und fährt die Last.

```text
Read https://raw.githubusercontent.com/yhgrwav/leettest/main/AGENTS.md and load-test my gRPC service
```

Die Anleitung, nach der er arbeitet, ist [AGENTS.md](../../AGENTS.md).

## Den Bericht lesen

Am Ende — ein Bericht je Methode: gesendet, fehlgeschlagen, `sent/s`, p50/p90/p95/p99.

- Ein Aufruf zählt ab seinem **geplanten** Zeitpunkt: Hat ihn das Ziel oder der Generator
  verzögert, steckt das in der Latenz.
- `sent/s` sind die gesendeten Aufrufe geteilt durch die Sendezeit: Ein Ziel, das am Ende des Laufs
  hängt, senkt diese Zahl nicht.
- Ein Perzentil, dessen Platz vom Timeout abgeschnittene Aufrufe einnehmen könnten, wird als
  **Untergrenze** ausgegeben: `>2.0s`. Kein Wert, sondern „mindestens".
- Unter der Tabelle bekommt eine Methode einen Block `over time` (noch nicht veröffentlicht): der
  gemessene Teil des Laufs ab dem Ende des Warm-ups in höchstens 12 Zeilen; JSON hält bis zu 60
  Fenster. Ein Aufruf gehört zur Zeile seines geplanten Moments, egal wann er endete. Eine Zeile
  zeigt `sent/s`, den Anteil der Fehlschläge, p50 und p99 auf zwei signifikante Stellen, daher kann
  das p50 einer Zeile in der letzten Stelle vom p50 der Tabelle abweichen. Eine Zeile `no calls`
  heißt, dass in diesem Fenster kein Aufruf hinausging. Liegt die Rate unter einem Aufruf pro
  Fenster, ist das der Plan, kein Fehler: mit `plan:` der Methode vergleichen. Bei einem `ramp` sind
  die ersten Zeilen der Hochlauf: ein niedriges `sent/s` dort ist der Plan, kein Fehler; suchen Sie
  die Zeile, in der p99 oder der Fehleranteil steigt, bevor die Rate ihr Plateau erreicht hat.
- Unter einer Methode mit `dataset` steht eine Zeile `data:`: wie viele
  Anfragen der Datei benutzt wurden und wie oft die am häufigsten benutzte rausging. Das heißt nicht,
  dass das Ziel so viele verschiedene Anfragen gesehen hat.
- Standardmäßig läuft die Last über **eine Verbindung** und landet auf **einem Backend**: hinter
  einem L4-Balancer (Kubernetes ClusterIP, NLB) und wenn DNS mehrere Adressen liefert. „Hält X
  nicht" betrifft dieses Backend, nicht den Dienst, und der Bericht zeigt das nicht. Um mehrere zu
  belasten, setzen Sie `app.connections`: Der Block `Connections:` im Bericht
  zeigt je Verbindung eine Zeile — Adresse, Aufrufe, Fehleranteil und p99 —, sodass ein
  zurückbleibendes Backend sichtbar wird. Ein L7-Balancer, der einzelne Anfragen verteilt (Envoy,
  ein gRPC-Ingress), verteilt auch eine Verbindung. Der Bericht gibt aus, wie oft die Verbindung neu
  aufgebaut wurde und welches Limit gleichzeitiger Streams das Ziel angekündigt hat.

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
  `app.max_response_size`, mit einer Kodierung komprimiert, die der Client nicht angekündigt hat,
  oder sie verletzt die Einzelantwort-Form: ein OK ohne Nachricht oder eine zusätzliche Nachricht.
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
| `CANCELLED`, `UNKNOWN`, `INTERNAL`, `DATA_LOSS`, `ABORTED` | `failure`; ein OK ohne Antwort, wie grpc-go es sieht, ist eine `bad response` | `cut off`; eine Antwort, die sich nicht entpacken ließ, ist eine `bad response` | `client error` |
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

### Der Pass des Laufs

Über dem Bericht steht ein Pass: auf welcher Maschine und mit welcher Konfiguration der Lauf gemacht
wurde, damit sich zwei Berichte vergleichen lassen, ohne zu raten, worin sie sich unterscheiden. Ihn
haben der Lauf und die Suche nach dem Bruchpunkt.

```text
machine: linux/amd64, 8 CPU, GOMAXPROCS 8, quota 4, go1.27.1; config sha256:3f9a1c0d2b7e
labels: commit=abc123, env=staging
```

- `quota` ist das CPU-Limit des Containers (cgroup), wie gesetzt; ohne Limit fehlt dieser Teil.
- Der Hash ist das SHA-256 der Konfigurationsdatei, wie sie auf der Platte liegt; ihn zeigt auch
  `sha256sum load.yaml`; im Text stehen die ersten 12 Ziffern, im JSON alle. Jede Änderung der Datei,
  auch an einem Kommentar, ändert den Hash. Er wird vor dem Einsetzen von `${ENV}` gebildet:
  Geheimnisse in `metadata` schreiben Sie über `${ENV}`, nicht als Wert in die Datei.
- Labels sind `labels:` in der Konfiguration und `-label key=value` in der
  Kommandozeile (wiederholbar; das Flag überschreibt denselben Schlüssel aus der Konfiguration):

```sh
leettest -c load.yaml -label env=staging -label commit=abc123
```

Ein Schlüssel besteht aus lateinischen Buchstaben, Ziffern und `_.-`, bis zu 64 Zeichen; ein Wert ist
ein String bis zu 256 Bytes, in YAML setzen Sie eine Zahl in Anführungszeichen (`build: "1.10"`). Eine
Verletzung ist Exit-Code `1` vor dem Start. Im JSON ist der Pass das Feld `passport`,
und bei einem `dataset` steht neben `file` ein `sha256`; alle Felder stehen in der
[Referenz](reference.md#json-felder).

## Wann ein Lauf ungültig ist

Es gibt einen Bericht, aber seine Zahlen handeln nicht von der Last auf das Ziel (Exit 2), wenn:

- **die Grenze `-max-in-flight` erreicht wurde.** Timeouts und Grenze werden vor dem Start so
  geprüft, dass ein hängendes Ziel sie nicht erreicht; wird sie erreicht, fehlte dem Generator CPU.
- **jeder gemessene Aufruf einer Methode ein `request error`, `client error` oder eine
  `bad response` ist.** Der Hinweis nennt Methode, Ursache und was zu tun ist.
- **die Uhr des Hosts gröber ist als ein Viertel des p50** einer Methode, oder der Schritt während
  des Laufs auf 1 µs oder mehr wuchs (`clock_step`). Messen Sie von Linux aus.
- **sich eine `pass`-Grenze nicht entscheiden lässt** (`thresholds_undecided`): zu wenige
  Beobachtungen, der Generator war zu spät, Aufrufe warteten auf einen freien Stream, am Ende der
  Verteilung stehen Aufrufe ohne Antwort. Der Block `Thresholds:` nennt die Grenze und den Grund.

## Grenzen: ein Lauf als Prüfung

Ohne `pass` gibt ein Lauf, der sein Ende erreicht, `0` zurück, egal wie das Ziel antwortete. Damit
der Lauf selbst „hält / hält nicht" sagt, setzen Sie Grenzen unter den Aufruf:

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
      pass:
        p99: 200ms
        errors: 1%
```

```sh
leettest -c pass.yaml
echo $?
```

Am Ende des Berichts steht ein Block: das schlechteste Urteil und eine Zeile je Grenze. Für einen
gesunden Dienst:

```
Thresholds: passed
  wallet.v1.WalletService/GetBalance  p99     <= 200ms  pass  21.2ms
  wallet.v1.WalletService/GetBalance  errors  <= 1%     pass  0.0% (0 of 44000)
```

Die Schlüssel: `p50`, `p90`, `p95`, `p99`, `p999` und `errors`; eine Latenzgrenze braucht `errors`
daneben, und `errors: 100%` schaltet die Fehlerprüfung ab — dann sind auch die Latenzgrenzen nichts
wert. Die Grenze der Suche nach dem Bruchpunkt ist `breakpoint.pass.held_rps`. Eine verfehlte Grenze
gibt Code `4`, eine Grenze, die sich nicht entscheiden lässt (zu wenige Beobachtungen, ein
verspäteter Generator, am Ende der Verteilung Aufrufe ohne Antwort), Code `2`. Alle Schlüssel,
Urteile und Gründe stehen in der [Referenz](reference.md#grenzen-pass); ein Beispiel mit allen
Schlüsseln ist [`examples/pass.yaml`](../../examples/pass.yaml).

## Exit-Codes

Ohne `pass` gibt ein Lauf, der sein Ende erreicht, `0` zurück, egal wie das Ziel antwortete:
**`0` heißt nicht „der Dienst ist gesund"**. Mit `pass` heißt `0`, dass jede Grenze geprüft und
eingehalten wurde. Die Codes haben einen Rang: Ein ungültiger Lauf (`2`) steht über einem
unvollständigen (`3`); ein Anhalten, das einen Bericht ausgab, ist immer `3`; ein beendeter Lauf, der
eine Grenze verfehlt hat, ist `4`, und Grenzen machen aus einem ungültigen oder angehaltenen Lauf kein
`4`.

| Code | Was passiert ist |
|---|---|
| `0` | Der Plan lief durch, der Bericht ist vollständig; mit `pass` sind alle Grenzen eingehalten |
| `1` | Der gewünschte Bericht wurde nicht erzeugt: der Lauf fand nicht statt (Flags, Konfiguration, Verbindung) oder der Bericht konnte nicht in `-json-file` geschrieben werden. stderr nennt den Grund |
| `2` | Der Lauf ist ungültig (siehe oben) oder eine `pass`-Grenze lässt sich nicht entscheiden. Es gibt einen Bericht, aber seine Zahlen handeln nicht von der Last oder beantworten die Frage der Grenze nicht |
| `3` | Der Lauf hielt vor seinem Plan an. Es gibt einen Bericht, und er deckt nur ab, was durchging |
| `4` | Ein beendeter Lauf hat eine `pass`-Grenze verfehlt. Es gibt einen Bericht, `outcome` im JSON ist `complete` |
| `130` | Ohne Bericht abgebrochen nach Ctrl+C |
| `143` | Ohne Bericht abgebrochen nach SIGTERM |

Wie das Anhalten funktioniert, steht in der [Referenz](reference.md#anhalten).

## JSON für Skripte und CI

Mit `-output json` bekommt stdout genau ein JSON-Objekt und einen Zeilenumbruch; alles andere geht
nach stderr, sodass stdout direkt in `jq` gehen kann. Das Objekt wird nur für einen Lauf ausgegeben,
der stattfand (Codes `0`, `2`, `3`, `4`); bei `1`, `130` und `143` ist stdout leer. Das Feld
`outcome` (`complete`, `invalid` oder `incomplete`) passt zum Exit-Code `0`, `2`, `3`; Code `4` ist
`complete`, und welche Grenze verfehlt wurde, sagt das Feld `thresholds`. Die
einzige Abweichung: Konnte der Bericht nicht in die Datei geschrieben werden,
ist der Code `1`, und auf stdout steht weiter das Objekt des Laufs.

Den Text im Log und das JSON für den nächsten Schritt aus einem Lauf liefert
`-json-file`:

```sh
leettest -c load.yaml -json-file report.json
```

Auf stdout steht, was `-output` sagt (standardmäßig Text); `report.json` enthält dasselbe Objekt,
das `-output json` ausgibt. Die Datei wird vor dem Start angelegt und nie überschrieben: Existiert
sie oder fehlt ihr Verzeichnis, ist der Code `1` und das Ziel bekommt keinen Aufruf. Ein Lauf ohne
Bericht (etwa `1` bei der Verbindung oder `130`) hinterlässt keine Datei. Schlägt das Schreiben nach
dem Lauf fehl, nennt stderr den Pfad und den Fehler, und der Code ist `1`.

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
`0`. Ein Perzentil ist ein Objekt `{"us": 1234, "ns": 1234567, "lower_bound": false}`: `ns` ist der
Wert des Histogramms in ganzen Nanosekunden, `us` derselbe Wert auf Mikrosekunden abgeschnitten;
`pass:` und `leettest compare` urteilen nach `ns`, bei einem Bericht, der vor diesem Feld geschrieben
wurde, nehmen sie `us` × 1000. Mit `lower_bound: true` ist es eine Untergrenze, kein Wert. Latenzen
sind die Werte des Histogramms auf 3 signifikante Stellen (ein Fehler bis 0,1 %), Zählungen sind
exakt. Zeiten zählen ab `started_at` (RFC 3339, UTC), dem
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

### Suche nach dem Bruchpunkt

Ein Abschnitt `load.breakpoint` statt `rps` und `duration` des Aufrufs:
LeetTest erhöht die Last in Stufen und nennt die Stufe, die das Ziel gehalten hat, und die, bei der
es brach. Genau ein Aufruf, ohne `rps`, `duration` und `load.warmup`.

```yaml
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      timeout: 500ms
  breakpoint:
    from: 100         # erste Stufe, rps
    to: 2000          # höher geht es nicht
    factor: 1.25      # nächste Stufe = letzte × factor; oder step: 100 — so viel rps mehr
    settle: 5s        # Beginn einer Stufe, geht nicht ins Urteil ein; unter der Hälfte von hold
    hold: 30s         # Länge einer Stufe
    p99_limit: 200ms  # optional; ohne ihn bricht eine Stufe bei p99 über dem Dreifachen der besten gehaltenen
    errors_limit: 1%  # optional; eine Stufe bricht, wenn mehr als dieser Anteil der Aufrufe fehlschlug
```

Eine Stufe bricht, wenn mehr als `errors_limit` ihrer Aufrufe fehlschlugen (standardmäßig 1 %; genau
so viel wird noch gehalten, `100%` — Fehler brechen nicht) oder ihr p99 die Grenze überschritt. Ein
Aufruf, der wegen der Verbindung nicht gesendet wurde, bricht die Stufe bei jedem `errors_limit`.
Fehler auf der Client-Seite (`request_error`, `client_error`, `bad_response`) beweisen nicht, dass das
Ziel brach: Wird das Limit nur mit ihnen überschritten, hält die Suche als `run_limit` mit der
Ursache `client_errors` an, statt einen Bruchpunkt zu nennen; `held_rps` bleibt. Eine gebrochene Stufe wird nach einer Pause wiederholt und, außer der ersten, nach Proben auf der
ersten Stufe: Das Ziel muss zu seinem früheren p99 zurückkehren. Eine Stufe gilt nur dann als
gehalten, wenn der Generator das Geplante gesendet hat — mindestens 99,9 % der Aufrufe des
Messfensters. Sonst ist es die Grenze des Laufs, nicht des Ziels. Vor der ersten Stufe steht in
stderr eine Zeile `breakpoint: up to N steps, at most T`: wie viele Stufen und wie lange die Suche im
schlimmsten Fall dauert. Eine Verbindung für die ganze Suche; reißt das Ziel sie ab, ist die Stufe
gebrochen, einen Wiederaufbau gibt es nicht.

Das Ergebnis (`outcome` im JSON) ist eine geschlossene Liste: `broke` (hielt X, brach bei Y),
`broke_at_first` (brach schon bei der ersten Stufe — fangen Sie niedriger an), `held_all` (hielt alles
bis `to`), `run_limit` (der Generator, das Limit einer Verbindung oder `-max-in-flight` war zu Ende:
über das Ziel oberhalb ist nichts bekannt), `stopped` (Ctrl+C), `invalid` (eine Stufe war ein
ungültiger Lauf). Die Ursache (`why`) ist ebenfalls eine geschlossene Liste: `errors`, `p99_limit`,
`p99_vs_base`, `connection`, `no_recovery`, `generator`, `in_flight_cap`, `stream_limit`,
`stream_wait`, `client_errors`, `clock_step`, `request_errors` oder `null`. Das Fehlerlimit steht
als String in `breakpoint.errors_limit`. `held_rps` und `broke_rps` sind
`null`, wo es keinen gibt. Im Suchmodus ist das JSON ein anderes Objekt: `mode: "breakpoint"`, keines der
Felder eines gewöhnlichen Laufs auf der obersten Ebene, jeder Lauf mit seinem vollständigen Bericht
in `breakpoint.runs` (`kind`: `step`, `repeat`, `probe`; `planned_rps` und
`sent_rps`). Ein gewöhnlicher Lauf schreibt `mode: "run"`.

Exit-Codes: `0` für `broke`, `broke_at_first`, `held_all`, `run_limit` — das sind Befunde; `2` für
`invalid`; `3` für das Anhalten mit einem einzelnen Ctrl+C; `4` — die Suche hielt weniger als
`breakpoint.pass.held_rps`.

### Zwei Suchen vergleichen

Zwei Berichte der Suche, mit `-json-file` geschrieben: vor einer Änderung und
danach. `leettest compare` sagt, um wie viel die Kapazität (`held_rps`)
gesunken ist, und urteilt mit `-max-capacity-drop` darüber. Am Referenz-Stand, aus dem Repository-Root:
eine Suche bei einer Kapazität von 270 rps (im Terminal des Stands), dann in einem anderen Terminal
die Suche und der Bericht „vorher":

```sh
go run ./test/stand/cmd/stand -capacity 270 -delay 20ms
```

```sh
leettest -c examples/breakpoint-step.yaml -json-file before.json
```

Halten Sie den Stand an (Ctrl+C), starten Sie ihn mit einer Kapazität von 200 rps und nehmen Sie den
Bericht „nachher" auf:

```sh
go run ./test/stand/cmd/stand -capacity 200 -delay 20ms
```

```sh
leettest -c examples/breakpoint-step.yaml -json-file after.json
```

Vergleichen (der Stand darf aus sein):

```sh
leettest compare -max-capacity-drop 10% before.json after.json
```

```
base: before.json  Health  2026-10-19T19:25:28.941479Z
new:  after.json  Health  2026-10-19T19:26:46.1806268Z
capacity: 250 rps -> 200 rps, drop 20.0%, limit 10%: FAIL
```

Die Kapazität fiel von 250 auf 200 rps, das sind 20 % bei einer Grenze von 10 %, Exit-Code `4`.
Zeiten und Stand sind bei Ihnen andere, die Zahlen folgen der Kapazität des Stands.

Die Berichte sind nur vergleichbar, wenn Ziel, Methode, LeetTest-Version, Pass des Laufs (außer den
Labels) und Dataset übereinstimmen; sonst nennen die Zeilen `differs:` das Feld, und das Urteil ist
`unknown`. Endete die Suche vor dem Bruch, ist die Kapazität eine Untergrenze (`>= N rps`); brach das
Ziel schon auf der ersten Stufe, eine Obergrenze (`below N rps`). Der Rückgang wird dann als Schranke
ausgegeben (`drop <= 33.4%`), und die Grenze wird nur entschieden, wenn die Schranke sie entscheidet.
Ohne `-max-capacity-drop` wird nichts beurteilt. Exit-Codes: `0` — innerhalb der Grenze oder keine
Grenze, `1` — ein Bericht nicht gelesen, `2` — die Berichte sind nicht vergleichbar oder eine Grenze
lässt sich nicht entscheiden, `4` — die Kapazität fiel über die Grenze.

### Zwei Läufe vergleichen

Dieselben Befehle lesen auch Berichte eines gewöhnlichen Laufs (`-json-file`):
`-max-p99-rise` beurteilt den Anstieg des p99 jeder Methode in Prozent des p99 „vorher",
`-max-errors-rise` den Anstieg des Anteils fehlgeschlagener Aufrufe in Prozentpunkten. Ohne Grenze
wird die Zeile ausgegeben, aber nicht beurteilt. Ein Bericht der Suche und ein Bericht eines Laufs in
einem Befehl — Code `1`. Am Referenz-Stand, aus dem Repository-Root: ein Lauf bei 20 ms Verzögerung,
dann bei 30 ms:

```sh
go run ./test/stand/cmd/stand -delay 20ms -life 60s
```

```sh
leettest -c examples/stand.yaml -json-file before.json
```

Der Stand beendet sich nach einer Minute selbst; starten Sie ihn mit 30 ms Verzögerung und nehmen Sie
den Bericht „nachher" auf:

```sh
go run ./test/stand/cmd/stand -delay 30ms -life 60s
```

```sh
leettest -c examples/stand.yaml -json-file after.json
```

Vergleichen (der Stand darf aus sein):

```sh
leettest compare -max-p99-rise 20% before.json after.json
```

```
base: before.json  Health  2026-10-20T12:40:09.5222467Z
new:  after.json  Health  2026-10-20T12:40:42.469917Z
grpc.health.v1.Health/Check: p99 21.2ms -> 31.3ms, rise 47.3%, limit 20%: FAIL
grpc.health.v1.Health/Check: errors 0.0% -> 0.0%, no change
```

Der p99 stieg von 21.2 auf 31.3 ms, das sind 47.3 % bei einer Grenze von 20 %, Exit-Code `4`. Zeiten
und Verzögerungen sind bei Ihnen andere.

Eine Prüfung kann `unknown` sein (Code `2`), wenn die Berichte nicht vergleichbar sind (wie oben), ein
Lauf ungültig oder unvollständig ist, zu wenige Messungen vorliegen, der p99 am Timeout hängt (dann
ist er größer als ausgegeben, `>=`) oder die eigenen Fehler des Clients offenlassen, ob der Dienst
schuld ist. Methoden, die nicht in beiden Berichten stehen, werden nicht verglichen. Die Exit-Codes
sind dieselben wie bei den Suchen.

Für ein Skript und CI kommt dasselbe Ergebnis als ein JSON-Objekt: `-output json`
gibt nur es auf stdout aus, Exit-Codes und stderr sind die des Textes; bei Code `1` ist stdout
leer.

```sh
leettest compare -max-p99-rise 20% -output json before.json after.json
```

Im Objekt ist `verdict` das schlechteste Urteil der Prüfungen mit einer Grenze (`null`, wenn es keine
gibt), und `checks` enthält jede Prüfung mit den Zahlen beider Berichte; bei einem `unknown` steht der
Grund in `reason`, ein Wert aus einer geschlossenen Liste:

```json
{"schema_version":1,"mode":"compare","compared_mode":"run","comparable":true,"differences":[],"verdict":"fail","checks":[{"method":"/grpc.health.v1.Health/Check","key":"p99","base_p99":{"us":21381,"ns":21381472,"lower_bound":false},"new_p99":{"us":31358,"ns":31358109,"lower_bound":false},"limit_percent":"20%","verdict":"fail","reason":null,"why":null}]}
```

Nicht alle Felder sind gezeigt: Die bei dieser Prüfung leeren (`base_capacity` und die anderen
`null`) und die Beschreibung der Berichte (`base`, `new`) sind weggelassen. Einen Anstieg in Prozent
gibt es im Objekt nicht — ein Skript rechnet ihn aus `us`. Jedes Feld steht in der
[Referenz, „JSON des Vergleichs"](reference.md#json-des-vergleichs).

## Mehr erfahren

Für das Produkt gibt es einen Plan — **[roadmap](roadmap.md)**. Fehlt Ihnen etwas —
[eröffnen Sie ein Issue](https://github.com/yhgrwav/leettest/issues/new/choose).

**[Referenz →](reference.md)** — jedes Konfigurationsfeld und Flag.

| Frage | |
|---|---|
| Welche Fragen beantwortet LeetTest? | [Lesen](questions.md) |
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
