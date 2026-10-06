# <img src="../../assets/icon.png" width="24" alt="" align="top"> Referenz

[Русский](../ru/reference.md) · [English](../en/reference.md) · [Deutsch](reference.md) · [简体中文](../zh-CN/reference.md)
[← Startseite](README.md)

> Übersetzt aus [docs/ru/reference.md](../ru/reference.md) bei 9a08876, 2026-10-05. Bei
> Abweichungen gilt die russische Fassung.

Jedes Konfigurationsfeld, jedes Flag und was das Werkzeug vor dem Start prüft. Wie man den Bericht
liest, steht im [README](README.md#den-bericht-lesen).

## Konfiguration

```yaml
name: wallet                     # optional
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
      timeout: 2s
```

| Feld | Was es tut |
|---|---|
| `name` | Optional. Wie der Lauf in der Kopfzeile heißt. Weggelassen — der Dienstname, wenn alle Methoden zu einem Dienst gehören, sonst der Name der Konfigurationsdatei |
| `app.target` | Die Adresse des Dienstes: `ip` und `port` |
| `app.tls` | TLS. Weggelassen — an. `false` — eine unverschlüsselte Verbindung |
| `app.ca` | Eine PEM-Datei mit Zertifikaten, mit denen der Dienst statt mit den System-Zertifikaten geprüft wird. Braucht TLS |
| `app.cert`, `app.key` | Ein Client-Zertifikat und sein Schlüssel in PEM, für einen Dienst mit mTLS. Nur zusammen, braucht TLS |
| `app.server_name` | Der Name, gegen den das Zertifikat des Dienstes geprüft wird, wenn es die Adresse aus `target` nicht nennt. Braucht TLS |
| `app.metadata` | Header jedes Aufrufs: `authorization`, `x-api-key` usw. `${NAME}` kommt aus einer Umgebungsvariable |
| `app.max_response_size` | Die größte Antwort, die ein Aufruf annimmt: `16MiB`, `512KB`. Die Einheit ist Pflicht (`MB` = 10⁶ Bytes, `MiB` = 2²⁰), unter 2 GiB. Weggelassen — 4 MiB, wie in gRPC. Eine größere Antwort ist eine `bad response`. Ein laufender Aufruf kann bis zum Doppelten des Limits puffern: standardmäßig bis zu 8 MiB je Aufruf |
| `load.warmup` | Die ersten N Sekunden bleiben aus den Perzentilen und aus `sent`: kalte Caches verderben sie. Aufwärm-Aufrufe erreichen das Ziel durchaus; der Bericht gibt sie auf einer Zeile `warm-up N sent (M failed), excluded from stats` aus — `sent` plus diese Zeile ist jeder Aufruf, den der Generator versucht hat. Das Ziel bekam sie alle bis auf die als unreachable oder client error gezählten; `cut off` und abgelaufene haben es vielleicht nicht vollständig erreicht: Ein Ziel, das sein HTTP/2-Fenster nicht öffnet (Flusskontrolle), bekommt nur die Header, und seine Zähler sehen den Aufruf womöglich nicht. Zählt zu `duration`, kürzer als jeder Aufruf |
| `load.calls[].method` | Der volle Methodenname: `package.Service/Method` |
| `load.calls[].rps` | Anfragen pro Sekunde für diese Methode |
| `load.calls[].duration` | Wie lange sie belastet wird: `30s`, `5m`, `1h` |
| `load.calls[].timeout` | Wie lange auf eine Antwort gewartet wird. Weggelassen — `2s`. Null schaltet es nicht ab, es ist ein Fehler |
| `load.calls[].data` | Der Anfrage-Body, siehe [unten](#anfrage-body). Weggelassen — eine leere Nachricht |

Die Konfiguration wird streng gelesen: Ein Tippfehler in einem Feldnamen ist ein Fehler mit der
Zeilennummer, ein falscher Wert ein Fehler mit Nummer und Methode des Aufrufs, kein Lauf mit leerer
Last. `rps` ist eine ganze Zahl: `10.5` wird abgelehnt, nicht still auf zehn gerundet. `warmup` muss
kürzer sein als jeder Aufruf, sonst bliebe von diesem Aufruf keine einzige gemessene Anfrage übrig.

**Timeout und die In-Flight-Grenze.** Hängt der Dienst, hält jede Methode `rps × timeout` Anfragen
in Flug, bis das Timeout greift, plus Reserve: eine Anfrage am Rand des Fensters und `rps × 100 ms`
dafür, dass der Generator einen Platz etwas nach der Deadline freigibt. Die Summe über die Methoden
darf `-max-in-flight` (standardmäßig 5000) nicht überschreiten. Das wird vor dem Start geprüft, und
der Fehler nennt beide Auswege: welches Timeout passen würde und welche Grenze nötig ist. So erreicht
ein hängender Dienst die Grenze nicht: Der Lauf erreicht sein Ende, und der Bericht sagt, wie viele
Aufrufe innerhalb des Timeouts keine Antwort bekamen und nach welchem gesendeten Aufruf das Ziel nicht
mehr antwortete. Ist die Grenze doch erschöpft, wurden Plätze mehr als 100 ms über ihre Deadline
gehalten — das ist der Generator (zu wenig CPU) oder der Sender, und der Bericht erklärt den Lauf für
ungültig. Daneben gibt der Bericht aus, wie viele Plätze in diesem Moment über ihre Deadline gehalten
wurden.

## Zugang zum Dienst

```yaml
app:
  target:
    ip: 10.0.3.17
    port: 443
  ca: certs/ca.pem            # Pfade sind relativ zur Konfigurationsdatei
  cert: certs/client.pem
  key: certs/client.key
  server_name: payments.internal
  metadata:
    authorization: Bearer ${PAYMENTS_TOKEN}
    x-api-key: ${PAYMENTS_KEY}
```

**Geheimnisse.** `${NAME}` wird aus einer Umgebungsvariable gefüllt, auch innerhalb eines Werts:
`Bearer ${TOKEN}`. Eine nicht gesetzte oder leer gesetzte Variable ist ein Fehler vor dem Start, der
sie nennt. Sonst ginge `Bearer ` ohne Token raus, und der Lauf zeigte 100 % Fehler als Schuld des
Ziels. Um `${` wörtlich zu schreiben, verdoppeln Sie das Dollarzeichen: `$${`. Ein einzelnes `$`
bleibt, wie es ist. Die Ersetzung wirkt nur in `app.metadata`: In `data`, `target` und jedem anderen
Feld geht `${NAME}` so raus, wie es geschrieben ist. Es gibt kein Flag für Header, sodass der Token
nie in `ps` oder der Shell-Historie landet. Das Werkzeug gibt Header-Werte nirgends aus: nicht im
Bericht, nicht während des Laufs, nicht in Fehlern.

**Header.** Namen werden kleingeschrieben, wie HTTP/2 sie ohnehin trägt. Ein Konfigurationsfehler
vor dem Start: ein Schlüssel, der mit `grpc-` beginnt (von gRPC reserviert), Pseudo-Header wie
`:path`, ein Schlüssel, der auf `-bin` endet (binäre Header werden noch nicht unterstützt), ein Wert
außerhalb von druckbarem ASCII. Dieselben Header und dasselbe Zertifikat gehen in die Methodenprüfung
vor dem Start. Hat das Ziel sie bei gesetzten Headern mit `Unauthenticated` beantwortet, startet der
Lauf nicht: „target rejected credentials". `PermissionDenied` hält den Lauf nicht an: Der Token wurde
angenommen und kann für Aufrufe gelten, ohne in die Reflection zu lassen; die Methoden werden als
ungeprüft markiert. `Unauthenticated` ohne Header hält ihn auch nicht an, und ein Hinweis sagt:
„target requires credentials; app.metadata is not set".

**Zertifikate.** `ca` ersetzt die System-Wurzeln, statt sie zu ergänzen. Die Prüfung des
Ziel-Zertifikats lässt sich durch nichts abschalten. `server_name` ändert nur den Namen, gegen den
das Zertifikat geprüft wird und der in SNI geht; `:authority` bleibt die Adresse aus `target`. Ein
passwortgeschützter Schlüssel wird nicht unterstützt: Entschlüsseln Sie ihn vorher.

**Eine Verbindung.** Der Generator hält eine Verbindung zu einer Adresse. Liefert DNS mehrere
Adressen oder steht das Ziel hinter einem L4-Balancer, wird nur ein Backend belastet, und der Bericht
zeigt das nicht. Aufrufe werden nicht wiederholt.
Die Service Config des Dienstes wird ignoriert: Wiederholungen und Balancing-Strategie daraus werden
nicht angewandt. Ein produktiver Client, der sie anwendet, sieht andere Kategorien und ein anderes
p99.

**Fehler vor dem Start.** Alles, was sich vor dem Lauf wissen lässt, heißt Exit-Code 1 und kein
einziger Aufruf ans Ziel: eine Datei nicht gefunden oder kein PEM, ein Zertifikat, das nicht zu
seinem Schlüssel passt, das Zertifikat des Ziels abgelaufen oder ohne die Adresse (Hinweis:
`server_name`), das Ziel, das die Verbindung gleich nach dem Handshake schließt (es hat das
Client-Zertifikat nicht angenommen), TLS an, während das Ziel keines hat, oder umgekehrt.

## Anfrage-Body

```yaml
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      data:
        wallet_id: "w-123"
        currency: USD                         # Enum — per Name
        filter:
          since: "2026-01-01T00:00:00Z"       # google.protobuf.Timestamp
```

`data` ist einfaches YAML in der Form der Nachricht. Das Werkzeug holt das Schema über Server
Reflection vom Dienst; kein `.proto` nötig. Der Body wird einmal vor dem Start gebaut, und jeder
Fehler darin — ein unbekanntes Feld, ein falscher Typ, eine Methode, die es nicht gibt — zeigt sich
sofort, mit Methoden- und Feldnamen, vor der ersten Anfrage. Kein `data` — es geht eine leere
Nachricht raus, und eine solche Methode braucht keine Reflection.

**Ein Body je Methode.** Jeder Aufruf einer Methode geht mit demselben `data` raus. Bei einem
Schreibvorgang mit Idempotenzschlüssel heißt das: Der erste Aufruf legt den Datensatz an, und jeder
spätere nimmt den Weg der Wiederholung: Das Ziel gibt die bereits gespeicherte Antwort zurück.
Gemessen wird dieser Weg, nicht das Anlegen des Datensatzes.

Die Regeln für Werte sind die Standard-JSON-Regeln für Protobuf (`protojson`):

- ein Feldname wie im `.proto` (`wallet_id`) oder in seiner JSON-Form (`walletId`);
- ein Enum per Name; `Timestamp`, `Duration` als String in ihrem Format;
- `int64` und `uint64` kommen exakt an, auch über 2^53 (Snowflake-IDs, Beträge in kleinen
  Einheiten): Die Zahl geht nie durch eine Gleitkommazahl;
- eine Zahl mit führender Null (`0123`, `007`) ist ein Konfigurationsfehler: YAML würde sie oktal
  lesen. Brauchen Sie die Null — schreiben Sie einen String, `"0123"`;
- **`bytes` als Base64-String.** `signature: abcd` sind nicht die vier Bytes `abcd`, sondern drei
  andere Bytes: `abcd` ist selbst gültiges Base64, und es gibt keinen Fehler. Die vier Bytes `abcd`
  schreibt man als `signature: YWJjZA==`.

**Methodenprüfung.** Jede Methode der Konfiguration wird vor dem Start gegen den Dienst geprüft: Ein
Tippfehler im Namen oder eine Methode, die der Dienst nicht hat, ist ein Fehler vor der ersten
Anfrage, kein Lauf, in dem jeder Aufruf mit `Unimplemented` scheitert. Ist Reflection beim Dienst
aus, startet eine Methode mit `data` nicht — der Fehler sagt das deutlich. Eine Methode ohne `data`
funktioniert auch ohne Reflection: Es gibt nichts, womit man sie prüfen könnte, und eine Warnung
sagt das — vor dem Lauf auf stderr und noch einmal als Zeile im Bericht. Die Warnung sagt, warum die
Prüfung nicht möglich war: Reflection ist aus, sie hat abgelehnt oder gar nicht geantwortet.

## Flags

| Flag | Was es tut |
|---|---|
| `-c` | Pfad zur Konfiguration |
| `-output` | Berichtsformat auf stdout: `text` (Standard) oder `json` für Skripte und CI |
| `-connect-timeout` | Wie lange auf einen Dienst gewartet wird, der die Verbindung angenommen hat, aber schweigt. Standard `10s`. Auf eine abgelehnte Verbindung und eine falsche Adresse wird nicht gewartet |
| `-max-in-flight` | Grenze für Anfragen, die auf eine Antwort warten. Standard `5000` |
| `-fake` | Statt des Dienstes aus der Konfiguration einen eingebauten Platzhalter belasten — um das Werkzeug ohne Dienst anzusehen. Der Bericht ist als `fake target` markiert |
| `-fake-delay`, `-fake-jitter`, `-fake-fail-ratio` | Das Verhalten des Platzhalters. Nur zusammen mit `-fake` |
| `-version` | Version ausgeben und beenden. Ein Build aus dem Quellcode gibt den Commit aus |

## Anhalten

`q` in der Vollbildansicht, Ctrl+C ohne sie:

- der erste Druck — keine neuen Anfragen gehen raus, Anfragen in Flug laufen bis zu ihrem Timeout
  und landen wie üblich im Bericht;
- der zweite — Anfragen in Flug werden abgeschnitten und gesondert gezählt (`aborted`): kein Fehler
  des Dienstes, sondern eine Untergrenze der Zeit; der Bericht wird ausgegeben;
- der dritte — sofortiges Beenden, ohne Bericht.

In der Vollbildansicht führt die oberste Zeile durch diese Schritte.

SIGTERM (`docker stop`, Kubernetes, ein abgebrochener CI-Job) schneidet die Anfragen in Flug sofort
ab und gibt den Bericht aus, ohne das sanfte Anhalten: Der Orchestrator beendet den Prozess einige
Sekunden später, und der Bericht muss rauskommen. Läuft bereits ein Abschneiden, unterbricht SIGTERM
es nicht.

Ein angehaltener Lauf wird im Bericht als unvollständig markiert (Exit 3): Seine Zahlen sind ehrlich,
decken aber weniger ab als geplant.

## JSON-Felder

Die Regeln des Vertrags — Schemaversion, Einheiten, `null` statt `0`, unbekannte Felder — stehen im
[README](README.md#json-für-skripte-und-ci). Hier ist jedes Feld von `schema_version` 1. Ein Typ mit
`?` kann `null` sein. Aufwärm-Zähler stecken in keinem anderen Zähler; `sent` und alle seine Anteile
sind nur gemessene Aufrufe. Die erste Spalte ist der volle Pfad des Felds.

**Perzentil** — ein Objekt; `null` — keine einzige Beobachtung. `<percentile>` ist jedes Feld, das
unten den Typ „percentile?" hat.

| Feld | Typ | Bedeutung |
|---|---|---|
| `<percentile>.us` | int | Der Wert, ganze Mikrosekunden |
| `<percentile>.lower_bound` | bool | `true` — eine Untergrenze, kein Wert ([README](README.md#den-bericht-lesen)) |

### Lauf

| Feld | Typ | Bedeutung |
|---|---|---|
| `schema_version` | int | Schemaversion, derzeit `1` |
| `leettest_version` | string | Die Version des Werkzeugs; ein Build aus dem Quellcode gibt den Commit an |
| `target` | string | Die Zieladresse aus der Konfiguration, `fake target` mit `-fake` |
| `outcome` | string | `complete`, `invalid`, `incomplete` — entspricht Exit-Code `0`, `2`, `3` |
| `invalid_reasons` | []string | Warum der Lauf ungültig ist: `in_flight_cap`, `nothing_measured`, `clock_step`. Leer — gültig |
| `tail_wait_cause` | string? | Die Wartezeit auf Client-Seite, die das Ende der Verteilung bestimmt hat: `generator`, `stream`, `connection`; `null` — kein Urteil |
| `clock_step_ns` | int | Der Uhrenschritt des Hosts, ns: Jede Latenz und Wartezeit ist ± dieser Wert |
| `started_at` | string | Beginn des Plans, RFC 3339 UTC, Aufwärmen eingeschlossen. Jedes `_us` und `_s` zählt ab hier |
| `duration_us` | int | Wie lange der Lauf ging, Aufwärmen eingeschlossen |
| `planned_us` | int | Wie lange er gehen sollte |
| `warmup_us` | int | Länge des Aufwärmens |
| `sent` | int | Gemessene Aufrufe, außer `not_sent`. `unreachable` und `aborted` sind enthalten |
| `failed` | int | Aus `sent` — jeder Aufruf, der nicht erfolgreich war, außer `aborted` |
| `aborted` | int | Durch unser eigenes Anhalten abgeschnitten |
| `not_sent` | int | Das Timeout lief vor dem Senden ab: weder in `sent` noch in `failed` |
| `not_sent_generator`, `not_sent_stream`, `not_sent_connection` | int | `not_sent` nach Ursache: Der Generator war spät, kein freier Stream, keine bereite Verbindung. Zusammen ergeben sie `not_sent` |
| `warmup_sent`, `warmup_failed`, `warmup_not_sent` | int | Dasselbe für Aufrufe, die im Aufwärmen geplant waren |
| `cap_hit` | object? | Die Grenze `-max-in-flight` wurde erreicht; `null` — wurde sie nicht |
| `cap_hit.at_us` | int | Wann |
| `cap_hit.unsent` | int | Aufrufe, die die Grenze abgewiesen hat |
| `cap_hit.over_deadline` | int | Aufrufe in Flug, die in diesem Moment ihren Platz über ihre Deadline hinaus hielten |
| `start_lag` | object | Wie spät Aufrufe gegenüber dem Plan starteten |
| `start_lag.p99` | percentile? | p99 der Verspätung |
| `start_lag.max` | percentile? | Das Maximum, immer exakt; `null` ohne Aufrufe |
| `connections` | object? | Verbindungen; `null`, wenn der Sender sie nicht meldet |
| `connections.open` | int | Wie viele Verbindungen gleichzeitig Aufrufe trugen |
| `connections.reconnects` | int | Erfolgreiche Handshakes nach dem ersten |
| `connections.first_limit`, `connections.last_limit` | int? | `MAX_CONCURRENT_STREAMS` beim ersten und letzten Handshake; `null` — nicht angekündigt (`0` — null angekündigt) |
| `connections.limit_changes` | int | Handshakes, die ein anderes Limit als das vorige ankündigten |
| `client_waits` | object | Aufrufe, die auf Client-Seite über der Schwelle warteten, nach Ursache ([README](README.md#den-bericht-lesen)) |
| `client_waits.generator_calls`, `client_waits.stream_calls`, `client_waits.connection_calls` | int | Alle solchen Aufrufe, gesendet oder nicht. Ein gesendeter Aufruf kann für mehrere Ursachen zählen |
| `client_waits.generator_tail_calls`, `client_waits.stream_tail_calls`, `client_waits.connection_tail_calls` | int | Nur im p99-Ende und unter den ungesendeten: Diese bestimmen `tail_wait_cause` |
| `methods` | []object | Methoden, siehe unten |
| `unchecked` | []object | Methoden, die über Reflection nicht geprüft werden konnten |
| `unchecked[].method` | string | Die Methode |
| `unchecked[].reason` | string | `reflection_off` oder `reflection_failed` |
| `unchecked[].error` | string | Text für Menschen, ändert sich mit grpc-go |
| `notes` | []string | Die Hinweise des Berichts, Text für Menschen in ASCII; nicht auswerten |

### Methode: `methods[]`

`<category>` ist jede der vier Kategorien, die im JSON ein Objekt mit Antwortzeiten haben:
`request_error`, `overload`, `failure`, `bad_response` ([Kategorien](README.md#den-bericht-lesen)).
Die übrigen sind einfache Zähler.

| Feld | Typ | Bedeutung |
|---|---|---|
| `methods[].method` | string | Der volle Methodenname |
| `methods[].invalid_reason` | string? | Die Methode hat nichts über Last gemessen: `request_error`, `client_error`, `bad_response`, `mixed`; `null` — sie hat |
| `methods[].sent`, `methods[].failed`, `methods[].aborted`, `methods[].not_sent`, `methods[].not_sent_generator`, `methods[].not_sent_stream`, `methods[].not_sent_connection`, `methods[].warmup_sent`, `methods[].warmup_failed`, `methods[].warmup_not_sent` | int | Der Anteil der Methode an den gleichnamigen Feldern des Laufs |
| `methods[].rps` | float? | `sent/s`: gesendete Aufrufe geteilt durch die Sendezeit; `null` — keine Aufrufe |
| `methods[].timeout_us` | int | Das Timeout der Methode |
| `methods[].planned_rps_low`, `methods[].planned_rps_high` | int | Niedrigste und höchste geplante Rate des ganzen Plans |
| `methods[].latency` | object | Latenz erfolgreicher Aufrufe, mit Timeouts und abgebrochenen Aufrufen als Untergrenzen |
| `methods[].latency.min`, `methods[].latency.p50`, `methods[].latency.p90`, `methods[].latency.p95`, `methods[].latency.p99`, `methods[].latency.max` | percentile? | Minimum, Perzentile, Maximum |
| `methods[].p99_without_client_waits` | percentile? | p99 derselben Aufrufe ohne Wartezeiten auf Client-Seite; kann etwas zu niedrig ausfallen |
| `methods[].observations` | int | Beobachtungen hinter den Perzentilen von `latency` |
| `methods[].censored` | int | Davon ist nur eine Untergrenze bekannt |
| `methods[].invalid_latencies` | int | Als unmöglich verworfen (eine negative Latenz) — ein Zeichen für einen Fehler im Code, nicht im Ziel |
| `methods[].timed_out` | int | Ging raus und bekam innerhalb des Timeouts keine Antwort |
| `methods[].timed_out_after_wait` | int | Aus `timed_out` — ging raus, als weniger als die Hälfte des Timeouts übrig war |
| `methods[].cut_off` | int | Ging raus und bekam keinen Status |
| `methods[].unreachable` | int | Hat das Ziel nie erreicht |
| `methods[].unclassified` | int | Der Sender gab keine Kategorie an — ein Defekt des Senders, keine Beobachtung des Ziels |
| `methods[].outside_timeline` | int | Nicht in `seconds`: Ein Zeitpunkt des Aufrufs passte nicht in die Zeitleiste |
| `methods[].<category>` | object | Antworten dieser Kategorie |
| `methods[].<category>.count` | int | Aufrufe darin |
| `methods[].<category>.p50`, `methods[].<category>.p90`, `methods[].<category>.p95`, `methods[].<category>.p99`, `methods[].<category>.max` | percentile? | Ihre Antwortzeit |
| `methods[].client_error` | int | Der Client verweigerte das Senden |
| `methods[].failure_codes` | []object | Fehlgeschlagene Aufrufe nach gRPC-Code |
| `methods[].failure_codes[].code` | string | Der kanonische Codename (`UNAVAILABLE`); die Liste kann wachsen |
| `methods[].failure_codes[].count` | int | Wie viele |
| `methods[].failure_codes[].from_target` | bool | `true` — das Ziel hat die Ablehnung geschickt: ein Status über die Leitung oder REFUSED_STREAM, `false` — der Client hat den Code gesetzt |
| `methods[].silent_from_s` | int? | Die Sekunde, ab der das Ziel keinen der gesendeten Aufrufe beantwortet hat; `null` — kein Schweigen |
| `methods[].silent_sent_rps` | int? | In der Sekunde davor gesendete Aufrufe (in der ersten, wenn das Schweigen dort beginnt) |
| `methods[].silent_planned_rps_low`, `methods[].silent_planned_rps_high` | int? | Die geplante Rate der Stufen in dieser Sekunde; `null` auch, wenn darin keine Stufe lief |
| `methods[].last_answer_at_us` | int? | Wann der letzte Aufruf rausging, den das Ziel beantwortet hat; `null` — es hat nie geantwortet |
| `methods[].seconds` | []object | Die Zeitleiste pro Sekunde, siehe unten |

### Sekunde: `methods[].seconds[]`

Die Zeitleiste deckt den ganzen Lauf ab, Aufwärmen eingeschlossen, bis zur letzten Sekunde, in der
etwas geschah. Anders als die Summen enthält sie jeden Aufruf. Zum Abgleich: `Σ begun` +
`outside_timeline` ist jeder Aufruf der Methode.

| Feld | Typ | Bedeutung |
|---|---|---|
| `methods[].seconds[].warmup` | bool | Die Sekunde liegt im Aufwärmen |
| `methods[].seconds[].begun` | int | In dieser Sekunde begonnene Aufrufe |
| `methods[].seconds[].succeeded`, `methods[].seconds[].overload`, `methods[].seconds[].failure`, `methods[].seconds[].client_error`, `methods[].seconds[].bad_response`, `methods[].seconds[].request_error`, `methods[].seconds[].timed_out`, `methods[].seconds[].unreachable`, `methods[].seconds[].cut_off`, `methods[].seconds[].aborted`, `methods[].seconds[].unclassified` | int | In dieser Sekunde beendete Aufrufe, nach Kategorie |
| `methods[].seconds[].not_sent_generator`, `methods[].seconds[].not_sent_stream`, `methods[].seconds[].not_sent_connection` | int | Timeouts vor dem Senden, die in dieser Sekunde endeten, nach Ursache |
| `methods[].seconds[].in_flight` | int | Aufrufe in Flug am Ende der Sekunde |
| `methods[].seconds[].lag_calls`, `methods[].seconds[].lag_sum_us`, `methods[].seconds[].lag_max_us` | int | Über die in dieser Sekunde geplanten Aufrufe: Anzahl, Summe und Maximum der Startverspätung |
| `methods[].seconds[].observed_calls` | int | Davon die erfolgreichen |
| `methods[].seconds[].observed_lag_sum_us`, `methods[].seconds[].transport_wait_sum_us`, `methods[].seconds[].service_time_sum_us` | int | Über die erfolgreichen: Summe der Startverspätung, der Wartezeit vor dem Senden (Verbindung und Stream), der Zeit vom Senden bis zur Antwort. Zusammen — ihre Latenzen |
