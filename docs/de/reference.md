# <img src="../../assets/icon.png" width="24" alt="" align="top"> Referenz

[Русский](../ru/reference.md) · [English](../en/reference.md) · [Deutsch](reference.md) · [简体中文](../zh-CN/reference.md)
[← Startseite](README.md)

> Übersetzt aus [docs/ru/reference.md](../ru/reference.md) bei e4663f1, 2026-10-27. Bei
> Abweichungen gilt die russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/reference.md).

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
| `labels` | Optional. Paare `key: "value"`, die der Pass des Berichts druckt, um Läufe zu unterscheiden (`env: staging`, `commit: abc123`). Ein Schlüssel hat 1 bis 64 Zeichen aus `A-Za-z0-9_.-`; ein Wert ist ein YAML-String von bis zu 256 Bytes, eine Zahl also in Anführungszeichen (`"1.10"`). `-label` fügt ein Paar hinzu oder ersetzt das der Konfiguration |
| `app.target` | Die Adresse des Dienstes: `ip` und `port` |
| `app.tls` | TLS. Weggelassen — an. `false` — eine unverschlüsselte Verbindung |
| `app.ca` | Eine PEM-Datei mit Zertifikaten, mit denen der Dienst statt mit den System-Zertifikaten geprüft wird. Braucht TLS |
| `app.cert`, `app.key` | Ein Client-Zertifikat und sein Schlüssel in PEM, für einen Dienst mit mTLS. Nur zusammen, braucht TLS |
| `app.server_name` | Der Name, gegen den das Zertifikat des Dienstes geprüft wird, wenn es die Adresse aus `target` nicht nennt. Braucht TLS |
| `app.metadata` | Header jedes Aufrufs: `authorization`, `x-api-key` usw. `${NAME}` kommt aus einer Umgebungsvariable |
| `app.max_response_size` | Die größte Antwort, die ein Aufruf annimmt: `16MiB`, `512KB`. Die Einheit ist Pflicht (`MB` = 10⁶ Bytes, `MiB` = 2²⁰), unter 2 GiB. Weggelassen — 4 MiB, wie in gRPC. Eine größere Antwort ist eine `bad response`. Ein laufender Aufruf kann bis zum Doppelten des Limits puffern: standardmäßig bis zu 8 MiB je Aufruf |
| `app.connections` | Wie viele Verbindungen zum Ziel geöffnet werden, eine ganze Zahl von 1 bis 256. Weggelassen — 1. Die Aufrufe nehmen die Verbindungen reihum, der Bericht druckt einen Block `Connections:`, eine Zeile je Verbindung |
| `app.compression` | Anfragekörper komprimieren: `gzip`. Weggelassen — keine. Der Bericht druckt unter der Methode `bytes: <raw> -> <wire> per call` |
| `app.mode` | `local` oder `real`, genau so geschrieben (`Real` ist ein Fehler, ebenso ein Schlüssel ohne Wert). Weggelassen — `local`. `real` kennzeichnet einen Lauf gegen ein System, das Menschen nutzen, und verlangt vor dem ersten Aufruf `load.ramp` (einen Anlauf von 0 auf `rps`), sonst Exit 1; mit `load.breakpoint` ist kein Anlauf nötig, weil die Suche keinen annimmt. Auch die Suche ist kein Anlauf: ihr erster Schritt geht sofort auf `breakpoint.from`. Der Bericht druckt den Modus unter `name`, auch bei `local`. Ein Flag dafür gibt es nicht. Verlangt auch `load.stop_on_errors`, außer bei einer Suche. (nicht veröffentlicht) |
| `app.proto.files` | `.proto`-Dateien, aus denen die Dienste genommen werden, für einen Dienst ohne Reflection. Pfade gelten ab dem Verzeichnis der Konfigurationsdatei. Jede Datei muss in einem der `import_paths` liegen |
| `app.proto.import_paths` | Wo die Dateien und ihre Imports gesucht werden, wie bei `protoc -I`. Pfade gelten ab dem Verzeichnis der Konfigurationsdatei. Weggelassen — dieses Verzeichnis. Für Standardtypen (`google/protobuf/…`) braucht es keinen Pfad |
| `app.protoset` | Eine Deskriptorsatz-Datei statt `.proto`-Dateien, gebaut mit `protoc --include_imports --descriptor_set_out=FILE`. Der Pfad gilt ab dem Verzeichnis der Konfigurationsdatei. Nicht zusammen mit `app.proto`; ist keines von beiden gesetzt, werden die Methoden über Reflection geholt |
| `load.warmup` | Die ersten N Sekunden bleiben aus den Perzentilen und aus `sent`: kalte Caches verderben sie. Aufwärm-Aufrufe erreichen das Ziel durchaus; der Bericht gibt sie auf einer Zeile `warm-up N sent (M failed), excluded from stats` aus — `sent` plus diese Zeile ist jeder Aufruf, den der Generator versucht hat. Das Ziel bekam sie alle bis auf die als unreachable oder client error gezählten; `cut off` und abgelaufene haben es vielleicht nicht vollständig erreicht: Ein Ziel, das sein HTTP/2-Fenster nicht öffnet (Flusskontrolle), bekommt nur die Header, und seine Zähler sehen den Aufruf womöglich nicht. Zählt zu `duration`, kürzer als jeder Aufruf |
| `load.ramp` | Jeden Aufruf in dieser Zeit linear von 0 auf sein `rps` anheben, bevor sein `duration` beginnt: der Lauf dauert `ramp` + `duration`, und `duration` bedeutet dasselbe wie vorher (die Zeit auf `rps`). Fehlt der Schlüssel oder `0s` — kein Anlauf. Die Aufrufe des Anlaufs bleiben aus Perzentilen und `sent`, wie die des Aufwärmens, das erst danach beginnt, wenn der Anlauf endet; der Bericht gibt beide in einer Zeile aus, `ramp and warm-up N sent, excluded from stats`. Die `pass`-Grenzen und der Fehleranteil zählen nur die Haltephase: Ein Ziel, das im Anlauf fiel und sich erholte, besteht, und das ist nur an dieser Zeile zu sehen. Der Anlauf beginnt mit einem Aufruf im Moment 0. Nicht zusammen mit `load.breakpoint`. |
| `load.stop_on_errors` | Ein Prozentwert wie `5%` oder `0.5%`, bis zu drei Stellen nach dem Punkt, höchstens `100%`, aber über null (`0%` ist ein Fehler). Der Lauf stoppt, wenn der Anteil fehlgeschlagener Aufrufe einer Methode in den letzten 5 ganzen Sekunden strikt über der Grenze liegt und mindestens zwei Aufrufe fehlschlugen; jede Methode wird für sich beurteilt, die Stopp-Zeile nennt sie. Fehlgeschlagen ist jede Kategorie außer `success` und `aborted`; Aufrufe, die nicht hinausgingen, zählen nicht. Das Fenster richtet sich nach der Sekunde, in der ein Aufruf endete, und wird einmal pro Sekunde geprüft; die laufende Sekunde gehört nicht dazu, Anlauf und Aufwärmen schon. Ein hängendes Ziel fällt frühestens beim ersten Aufruf auf, der mit Timeout endete, etwa nach `timeout` plus dem 5-s-Fenster. Der Stopp ist weich wie Strg-C: Exit 3, `incomplete`; er geht vor `pass`, das dann `unknown/incomplete` lautet, während `pass` und `stop_on_errors` voneinander unabhängig sind. `100%` bedeutet aus: Der Bericht druckt `stop_on_errors: off (100%)`, der Schlüssel besteht die Prüfung und stoppt nichts. Fehlt der Schlüssel, wird nichts beobachtet. Für `app.mode: real` Pflicht (außer bei einer Suche); zusammen mit `load.breakpoint` ein Fehler, die Suche hat ihr eigenes `breakpoint.errors_limit`. (nicht veröffentlicht) |
| `load.calls[].method` | Der volle Methodenname: `package.Service/Method` |
| `load.calls[].rps` | Anfragen pro Sekunde für diese Methode |
| `load.calls[].duration` | Wie lange sie belastet wird: `30s`, `5m`, `1h` |
| `load.calls[].timeout` | Wie lange auf eine Antwort gewartet wird. Weggelassen — `2s`. Null schaltet es nicht ab, es ist ein Fehler |
| `load.calls[].data` | Der Anfrage-Body, siehe [unten](#anfrage-body). Weggelassen — eine leere Nachricht |
| `load.calls[].dataset` | Eine Datei mit Anfrage-Bodies statt `data`, siehe [unten](#bei-jedem-aufruf-eine-andere-anfrage-dataset). Zusammen mit `data` nicht erlaubt |
| `load.calls[].dataset_order` | `file` (weggelassen) oder `random`: die Reihenfolge der Anfragen aus der Datei. Braucht `dataset` |
| `load.calls[].dataset_seed` | Eine ganze Zahl von 0 bis 9007199254740991, legt eine zufällige Reihenfolge fest. Braucht `dataset_order: random`. Weggelassen — wird selbst gewählt und im Bericht ausgegeben |
| `load.calls[].dataset_reuse` | `true` (weggelassen) oder `false`: bei `false` geht kein Datensatz im ganzen Prozess zweimal raus. Braucht `dataset` |
| `load.calls[].pass` | Die Grenzen, die ein Lauf einhalten muss, um zu bestehen: `p50`, `p90`, `p95`, `p99`, `p999`, `errors`, siehe [unten](#grenzen-pass). Weggelassen — keine Grenzen |
| `load.breakpoint.pass` | Die Grenze einer Bruchpunkt-Suche: `held_rps`, siehe [unten](#grenzen-pass). Weggelassen — keine Grenze |
| `load.breakpoint.errors_limit` | Ein Prozentwert wie `1%` oder `0.5%`, bis zu drei Stellen nach dem Punkt, höchstens `100%`. Eine Stufe der Suche bricht, wenn mehr als dieser Anteil der Aufrufe fehlschlug; genau so viel wird gehalten. Weggelassen — `1%` |

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

## Grenzen: pass

`pass` unter einem Aufruf sagt, bei welchen Werten der Lauf als bestanden gilt:

```yaml
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      pass:
        p99: 200ms
        errors: 1%
```

Alle Schlüssel stehen in [`examples/pass.yaml`](../../examples/pass.yaml), die Grenze einer Suche in
[`examples/breakpoint.yaml`](../../examples/breakpoint.yaml).

| Schlüssel | Grenze |
|---|---|
| `p50`, `p90`, `p95`, `p99`, `p999` | Die Latenz ist höchstens die Grenze: eine Dauer mit Einheit (`200ms`), ganze Mikrosekunden, über null, unter dem `timeout` des Aufrufs und unter einer Stunde. Braucht `errors` daneben: Ein Ziel, das Aufrufe sofort ablehnt, bestünde sonst eine Latenzgrenze. `p999` braucht 1000 gemessene Aufrufe: `rps` × (`duration` − `warmup`) |
| `errors` | Der Anteil fehlgeschlagener Aufrufe ist höchstens die Grenze: ein Prozentwert mit `%` (`1%`, `0.5%`), bis zu drei Stellen nach dem Punkt, höchstens `100%`. Eine Zahl ohne `%` wird nicht erraten. `errors: 100%` schaltet die Fehlerprüfung ab, und dann sind die Latenzgrenzen nichts wert |
| `breakpoint.pass.held_rps` | Die Suche muss mindestens so viele Aufrufe pro Sekunde halten. Eine ganze Zahl, eine der Stufen des Plans der Suche. Ein `pass` unter einem Aufruf zusammen mit `load.breakpoint` wird abgelehnt |

Eine Konfiguration mit falschem `pass` ist ein Fehler vor dem Start (Exit-Code 1), kein Lauf ohne
die Prüfung. Auch ein leeres `pass: {}` ist ein Fehler.

**Jede Grenze bekommt ein Urteil**: `pass`, `fail` oder `unknown`. Der Bericht gibt einen Block
`Thresholds:` mit dem schlechtesten Urteil und eine Zeile je Grenze aus; das JSON hat das Feld
`thresholds`.

| Ergebnis | Exit-Code |
|---|---|
| Jede Grenze ist `pass` | `0` |
| Ein beendeter Lauf hat eine Grenze verfehlt (`fail`) | `4`, `outcome` bleibt `complete` |
| Kein `fail`, aber eine Grenze lässt sich nicht entscheiden (`unknown`) | `2`, `invalid_reasons` enthält `thresholds_undecided` |
| Der Lauf ist ungültig oder angehalten | `2` oder `3`, wie ohne Grenzen: Grenzen ändern daran nichts |

Eine Verfehlung geht vor einer unentschiedenen Grenze: Bei `fail` und `unknown` zusammen ist der
Code `4`.

Eine Grenze ist `unknown`, wenn man der Zahl nicht trauen kann oder sie fehlt; der Grund stammt aus
einer geschlossenen Liste: `incomplete` (der Lauf wurde angehalten), `invalid` (der Lauf ist
ungültig), `few_observations` (das Perzentil wurde nicht gemessen), `run_limit` (die Suche stieß an
die eigene Grenze des Laufs), `generator` (der Generator war zu spät), `stream` (Aufrufe warteten auf
einen freien Stream), `client_errors` (die eigenen Fehler des Clients entscheiden, nicht die des
Ziels), `nothing_sent` (kein einziger Aufruf wurde gesendet), `censored` (am Ende der Verteilung
stehen Aufrufe ohne Antwort innerhalb des Timeouts: Der Wert ist nur eine Untergrenze, nicht über
der Grenze).

- Die Latenz wird in Nanosekunden verglichen, wie das JSON sie trägt (`value.ns`): Ein Wert, der eine
  Nanosekunde über der Grenze liegt, ist ein `fail`, obwohl `value.us` (auf Mikrosekunden
  abgeschnitten) gleich `limit_us` sein kann. Eine Untergrenze über der Grenze ist ein `fail`: Der
  wahre Wert ist noch größer.
- Der Anteil zählt jeden fehlgeschlagenen Aufruf und jeden Aufruf, der mangels Verbindung nicht
  gesendet wurde, an allen gezählten. Für ein `fail` zählen nur die Fehler, die das Ziel
  entschieden hat: Vom Client abgelehnte Anfragen, Client-Fehler und fehlerhafte Antworten sagen
  nichts über das Ziel.
- Die Zeile einer Grenze widerspricht nie ihrem Urteil: Läge der auf drei signifikante Stellen
  gerundete Wert auf der falschen Seite der ausgegebenen Grenze, wird der genaue Wert ausgegeben.
  Neben einem FAIL ist es der erste der drei, der über der Grenze liegt: drei signifikante Stellen,
  der Wert in ganzen Mikrosekunden (zum nächsten; eine Untergrenze nach unten), der Wert auf die
  Nanosekunde (`100.4us` bei einer Grenze von `100us`). Der Anteil neben einem FAIL wird zum
  nächsten gerundet, nicht nach oben: Genommen wird die erste von 1, 2, 3 Stellen nach dem Punkt, bei
  der die Zahl über der Grenze liegt (`1.004%` bei einer Grenze von `1%`); reichen auch drei nicht,
  wird `> ` und die Grenze, wie sie geschrieben steht, ausgegeben (`> 1%`).

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
`:path`, ein Schlüssel, der auf `-bin` endet (binäre Header werden noch nicht unterstützt), `content-type` und `te` (die schreibt der Transport
selbst), `connection`, `keep-alive`, `proxy-connection`, `transfer-encoding`, `upgrade` (in HTTP/2
gibt es sie nicht), ein Wert außerhalb von druckbarem ASCII. `user-agent` ist erlaubt: Das Ziel sieht
einen Header, Ihren Wert und danach ` grpc-go/<Version>`. Dieselben Header und dasselbe Zertifikat gehen in die Methodenprüfung
vor dem Start. Hat das Ziel sie bei gesetzten Headern mit `Unauthenticated` beantwortet, startet der
Lauf nicht: „target rejected credentials". `PermissionDenied` hält den Lauf nicht an: Der Token wurde
angenommen und kann für Aufrufe gelten, ohne in die Reflection zu lassen; die Methoden werden als
ungeprüft markiert. `Unauthenticated` ohne Header hält ihn auch nicht an, und ein Hinweis sagt:
„target requires credentials; app.metadata is not set".

**Zertifikate.** `ca` ersetzt die System-Wurzeln, statt sie zu ergänzen. Die Prüfung des
Ziel-Zertifikats lässt sich durch nichts abschalten. `server_name` ändert nur den Namen, gegen den
das Zertifikat geprüft wird und der in SNI geht; `:authority` bleibt die Adresse aus `target`. Ein
passwortgeschützter Schlüssel wird nicht unterstützt: Entschlüsseln Sie ihn vorher.

**Verbindungen.** Standardmäßig hält der Generator eine Verbindung zu einer Adresse. Liefert DNS
mehrere Adressen oder steht das Ziel hinter einem L4-Balancer, wird nur ein Backend belastet, und der
Bericht zeigt das nicht. `app.connections: N` öffnet N Verbindungen. Ein Name,
den DNS zu M Adressen auflöst, wird einmal vor dem Start aufgelöst, und Verbindung i geht an Adresse
i mod M: drei Verbindungen auf zwei Adressen sind zwei und eine. Eine Adresse, die das Ziel nicht
bedient (`localhost` zu `::1`, während der Server nur auf `127.0.0.1` lauscht), stoppt den Start und
steht im Fehler: nehmen Sie ihre IP. Der Block im Bericht zeigt je Verbindung Adresse, Aufrufe,
Fehleranteil und p99, damit ein totes oder langsames Backend hinter einem Namen nicht im Mittel
untergeht. Aufrufe werden nicht wiederholt.
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

## Bei jedem Aufruf eine andere Anfrage: dataset

```yaml
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      dataset: wallets.jsonl
```

```
{"wallet_id": "w-1", "currency": "USD"}
{"wallet_id": "w-2", "currency": "EUR"}
```

`dataset` ist eine Datei mit Anfrage-Bodies, ein JSON je Zeile, in derselben Form wie bei `data`. Ein
relativer Pfad gilt ab dem Verzeichnis der Konfigurationsdatei. Die erste Anfrage geht mit der ersten
Zeile raus, die zweite mit der zweiten, nach der letzten Zeile fängt es von vorn an. Leere Zeilen
werden übersprungen. Die Datei wird gelesen, und jede Zeile wird vor dem Start gegen die Nachricht
der Methode geprüft — nach den Regeln von `data` und über Reflection; ein Fehler nennt die Zeile,
`wallets.jsonl:3`, und gibt nie aus, was in ihr steht. Eine Datei ohne Anfragen ist ein Fehler.

Unter der Methodenzeile sagt der Bericht, wie viel der Datei benutzt wurde:
`data: 3 of 3 requests from wallets.jsonl, each used up to 5 times`. Das ist, wie oft eine Anfrage
dem Sender übergeben wurde, nicht, was das Ziel gesehen hat: Eine Anfrage, die zweimal in der Datei
steht, geht zweimal raus, und ein Aufruf, der das Ziel nie erreicht hat, wird auch gezählt. In einer
Suche nach dem Bruchpunkt macht jeder Schritt dort weiter, wo der vorige endete, und die Zeile zählt
die ganze Suche.

`dataset_order: random` gibt die Anfragen in zufälliger Reihenfolge aus: Jeder Durchgang durch die
Datei ist eine neue Permutation, in der jede Anfrage einmal rausgeht, deshalb bleiben die Zahlen
oben dieselben. Die Zeile endet dann auf `, random order, seed 42`. Um den Lauf zu wiederholen,
tragen Sie diese Zahl in `dataset_seed` ein (eine ganze Zahl von 0 bis 9007199254740991): Derselbe
Seed, dieselbe Datei und dieselbe Anzahl Anfragen ergeben dieselbe Reihenfolge. Weggelassen — der
Seed wird beim Start gewählt, je Aufruf ein eigener.

`dataset_reuse: false` sendet jeden Datensatz höchstens einmal, je Aufruf und im ganzen Prozess:
Warm-up, jeder Schritt der Suche und jede Verbindung nehmen die Datensätze aus einem Zähler (zwei
Aufrufe mit derselben Datei — zwei Zähler). Die Zeile lautet dann:
`data: 3 of 3 requests from wallets.jsonl, each at most once`. Ein Lauf, den die Datei nicht abdeckt, wird vor dem Senden
abgelehnt, mit beiden Zahlen. Eine Suche, die einen Datensatz braucht, während die Datei keinen mehr
hat, hält an wie bei Ctrl+C (Exit-Code 3): Der laufende Lauf wird nicht beurteilt, der Bericht nennt
die Datei und die Rate. Nur ein Prozess: Die Datensätze werden nicht zwischen Maschinen geteilt, und
ein Anhalten lässt Datensätze ungesendet.

## Flags

| Flag | Was es tut |
|---|---|
| `-c` | Pfad zur Konfiguration |
| `-output` | Berichtsformat auf stdout: `text` (Standard) oder `json` für Skripte und CI |
| `-json-file` | Pfad einer neuen Datei: der JSON-Bericht (dasselbe Objekt wie bei `-output json`) wird hineingeschrieben, stdout bleibt, wie `-output` es sagt. Eine vorhandene Datei oder ein fehlendes Verzeichnis ist Exit 1 vor dem Start. Ein Lauf ohne Bericht hinterlässt keine Datei. Ein fehlgeschlagenes Schreiben ist Exit 1 |
| `-label` | Ein Label `key=value` für den Pass des Berichts; mehrfach angebbar. Es gewinnt gegen denselben Schlüssel in `labels` der Konfiguration. Ein doppelt angegebener Schlüssel oder einer, der die Regeln für Schlüssel und Wert verletzt, ist Exit 1 vor dem Start |
| `-connect-timeout` | Wie lange auf einen Dienst gewartet wird, der die Verbindung angenommen hat, aber schweigt. Standard `10s`. Auf eine abgelehnte Verbindung und eine falsche Adresse wird nicht gewartet |
| `-max-in-flight` | Grenze für Anfragen, die auf eine Antwort warten. Standard `5000` |
| `-plain` | Ohne Live-Bildschirm: eine Fortschrittszeile pro Sekunde auf stderr, wie ohne Terminal, und ohne den Dialog beim ersten Start. Der Bericht und die Exit-Codes sind dieselben |
| `-dry-run` | Die Konfiguration ohne Last prüfen: verbinden, die Methoden prüfen, pro Aufruf eine Anfrage senden und die Anfrage, den Status und die Antwort ausgeben. Keine Statistik, kein Bericht. Einzelheiten im Abschnitt „Prüfen vor der Last“ unten. Nicht zusammen mit `-fake`, `-output`, `-json-file`, `-label`, `-plain` (Exit-Code 1). |
| `-yes` | Wird angenommen und vorerst ignoriert; reserviert für die Bestätigung eines Laufs mit `app.mode: real`. Mit `app.mode: local` ändert es nichts. (nicht veröffentlicht) |
| `-fake` | Statt des Dienstes aus der Konfiguration einen eingebauten Platzhalter belasten — um das Werkzeug ohne Dienst anzusehen. Der Bericht ist als `fake target` markiert |
| `-fake-delay`, `-fake-jitter`, `-fake-fail-ratio` | Das Verhalten des Platzhalters. Nur zusammen mit `-fake` |
| `-version` | Version ausgeben und beenden. Ein Build aus dem Quellcode gibt den Commit aus |

## Prüfen vor der Last

`leettest -c x.yaml -dry-run` verbindet sich und prüft die Methoden wie ein
Lauf, sendet dann pro Eintrag von `load.calls` eine Anfrage, nacheinander in der Reihenfolge der
Konfiguration, und gibt für jeden die Anfrage so, wie sie hinausging, den Status und die Antwort
als JSON aus. Keine Last, keine Statistik, kein Bericht: in einer Sekunde zeigt sich, ob die
Konfiguration mit diesen Daten und dieser Autorisierung den Dienst erreicht, bevor eine Last einen
Bericht mit einem hunderttausendfach wiederholten Fehler füllt.

```
dry run: 2 calls, one request each, nothing measured
1/2 grpc.health.v1.Health/Check: success, OK, 1.20ms
  request: {"service":"wallet"}
  response: {"status":"SERVING"}
2/2 wallet.Wallet/Transfer: failure, InvalidArgument sent by the target, 800us
  request (record 1 of 500): {"from":"a-1","amount":"-5"}
  message: amount must be positive
metadata: authorization: Bearer ${TOKEN}, x-tenant: acme
```

- **Die erste Zeile eines Aufrufs:** der Name der Kategorie wie im Bericht, der Code, wer ihn
  gesetzt hat (`sent by the target` oder `set by the client`; bei Erfolg nichts) und die Zeit vom
  Senden bis zur Antwort. Ein Aufruf, zu dem kein Status kam (`unreachable`, `cut_off`), hat keine
  Zeit.
- **`request:` und `response:`** sind die Bytes, mit dem Schema der Methode gelesen, in den
  Feldnamen der `.proto`, in einer Zeile. Die Anfrage ist das, was über die Leitung ging, ein Feld
  mit seinem Standardwert fehlt also. Ein fehlgeschlagener Aufruf hat statt `response:` die Zeile
  `message:` (die Statusmeldung, nur wenn sie nicht leer ist). Eine Antwort, die sich nicht lesen
  lässt, steht als `response: N bytes, not decoded: <Fehler>`, die Kategorie des Aufrufs bleibt.
  Eine Methode, die die Reflection nicht kannte, geht als leere Nachricht hinaus, mit einer Warnung
  auf stderr wie im Lauf, und gibt `request: {}` aus.
- **Ein Body oder eine Meldung über 4096 Bytes** wird dort abgeschnitten und endet mit
  `… (N bytes in all)`, N ist die Länge, nachdem die Geheimnisse verborgen sind. Steuerzeichen
  einer Meldung werden als `\xNN` geschrieben.
- **Daten.** Ein Aufruf mit `dataset` sendet den ersten Datensatz der Datei, was auch
  `dataset_order`, `dataset_seed` und `dataset_reuse` sagen; der Block nennt `record 1 of N`.
- **Metadaten** sind eine Zeile nach den Blöcken: Schlüssel klein geschrieben, wie sie hinausgehen,
  sortiert, Werte so, wie die Konfiguration sie schreibt, also bleibt `${ENV}` unaufgelöst. Keine
  Metadaten — keine Zeile.
- **Geheimnisse.** Der Wert jeder `${NAME}`, die in die Metadaten eingesetzt wurde, wird in
  `message:` und in `response:` durch `${NAME}` ersetzt: Ein Dienst, der den Header zurückspiegelt,
  bringt das Geheimnis weder auf den Bildschirm noch ins CI-Log, und eine Antwort, die es trägt,
  liest sich `{"tenant":"${TENANT}"}` statt `acme`. Nicht erfasst: ein Geheimnis in einem
  `bytes`-Feld als Base64. Ein Geheimnis, das im YAML selbst steht, wird so gedruckt, wie es steht,
  auch in der Metadatenzeile: Um es vom Bildschirm fernzuhalten, nehmen Sie es mit `${ENV}` aus der
  Umgebung.
- **Prüfungen wie beim Lauf.** Was ein Lauf vor dem Netz ablehnt, lehnt ein Dry Run mit demselben
  Text und Exit-Code 1 ab: eine Datei, die für den Plan zu kurz ist, ein Aufruf über der Grenze der
  Anfragen im Flug, eine Methode doppelt. `-max-in-flight` gilt, ebenso `-c` und
  `-connect-timeout`. `-fake`, `-fake-delay`, `-fake-jitter`, `-fake-fail-ratio`, `-output`,
  `-json-file`, `-label` und `-plain` ändern im Dry Run nichts und werden abgelehnt:
  `-<Name> with -dry-run: …`.
- **Was die Konfiguration über die Last sagt** — `rps`, `duration`, `load.ramp`, `load.warmup`,
  `pass`, `load.breakpoint` — wird angenommen und nicht verwendet: Eine Konfiguration mit
  `load.breakpoint` bekommt ihre eine Anfrage pro Aufruf und keine Suche. Für die Anfrage jedes
  Aufrufs gilt sein `timeout`.
- **Exit-Code.** `0`, wenn jeder Aufruf `success` ist. Sonst `1` und auf stderr `dry run: 1 of 2
  calls did not succeed`; dieselbe `1` gilt für alles, was einen Lauf vor dem Start stoppt
  (Konfiguration, Dateien, ein nicht erreichbarer Dienst). Ctrl+C oder SIGTERM brechen den Aufruf
  im Flug ab: Die schon gedruckten Blöcke bleiben, nichts wird mehr gesendet, auf stderr
  `dry run: stopped after K of N calls`, Exit `1`. Ein CI-Schritt liest `1` als „die Last nicht
  starten“.

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

## Wo man es ausführt

Unter Linux neben dem Ziel: im selben Netz, im selben Cluster, auf einem CI-Runner. Alles zwischen
Generator und Ziel landet in der Latenz und sieht aus wie die Zeit des Ziels: auf unserem Stand bei
1000 RPS lag p99 über die Portweiterleitung von Docker Desktop bei 38 ms, innerhalb des Docker-Netzes
bei 2 ms.

- Den exakten Aufrufplan gibt es nur unter Linux. In einem Container mit einer CPU-Quote unter zwei
  Kernen sind seltene Startverzögerungen bis zur Quotenperiode (meist 100 ms) möglich; der Bericht
  zeigt sie.
- Hinter einem L4-Balancer wird ein Backend belastet: eine Verbindung — ein Backend. Siehe
  [den Bericht lesen](README.md#den-bericht-lesen).
- Unter Windows springt die Uhr in Schritten von etwa 0,5 ms, und ein Lauf gegen einen schnellen
  Dienst wird für ungültig erklärt (`clock_step`, Exit-Code 2).

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
| `<percentile>.ns` | int | Der Wert des Histogramms in ganzen Nanosekunden, den der Textbericht mit drei signifikanten Stellen ausgibt; `us` ist derselbe Wert, auf Mikrosekunden abgeschnitten; `pass:` und `leettest compare` urteilen nach `ns`: neben einem `fail` kann `us` gleich `limit_us` sein |
| `<percentile>.lower_bound` | bool | `true` — eine Untergrenze, kein Wert ([README](README.md#den-bericht-lesen)) |

### Lauf

| Feld | Typ | Bedeutung |
|---|---|---|
| `schema_version` | int | Schemaversion, derzeit `1` |
| `mode` | string | `run` bei einem gewöhnlichen Lauf, `breakpoint` bei der Suche nach dem Bruchpunkt |
| `leettest_version` | string | Die Version des Werkzeugs; ein Build aus dem Quellcode gibt den Commit an |
| `target` | string | Die Zieladresse aus der Konfiguration, `fake target` mit `-fake` |
| `name` | string | Der Name des Laufs: `name` aus der Konfiguration, sonst der Dienstname, sonst der Name der Konfigurationsdatei; immer ein String |
| `methods_source` | string? | Woher die Methoden stammen: `reflection`, `proto` (die Dateien aus `app.proto`) oder `protoset`; `null` mit `-fake` |
| `outcome` | string | `complete`, `invalid`, `incomplete` — entspricht Exit-Code `0`, `2`, `3`; ein beendeter Lauf, der eine `pass`-Grenze verfehlt hat, ist `complete` mit Code `4`; bei `-json-file` und fehlgeschlagenem Schreiben ist der Code `1`, und `outcome` bleibt der des Laufs |
| `invalid_reasons` | []string | Warum der Lauf ungültig ist: `in_flight_cap`, `nothing_measured`, `clock_step`, `thresholds_undecided` (eine `pass`-Grenze lässt sich nicht entscheiden, und nicht, weil der Lauf angehalten oder schon ungültig war). Leer — gültig |
| `tail_wait_cause` | string? | Die Wartezeit auf Client-Seite, die das Ende der Verteilung bestimmt hat: `generator`, `stream`, `connection`; `null` — kein Urteil |
| `clock_step_ns` | int | Der Uhrenschritt des Hosts, ns: Jede Latenz und Wartezeit ist ± dieser Wert |
| `stop_reason` | string? | `error_spike`, wenn `load.stop_on_errors` den Lauf gestoppt hat; sonst `null`. (nicht veröffentlicht) |
| `stop` | object? | Wo und auf welchen Zahlen `load.stop_on_errors` den Lauf gestoppt hat; `null`, wenn nicht. (nicht veröffentlicht) |
| `stop.method` | string | Die Methode, deren Anteil fehlgeschlagener Aufrufe die Grenze überschritten hat, wie die Konfiguration sie nennt, ohne führendes `/`. (nicht veröffentlicht) |
| `stop.failed` | int | Ihre fehlgeschlagenen Aufrufe in den letzten 5 Sekunden. (nicht veröffentlicht) |
| `stop.total` | int | Ihre Aufrufe in denselben Sekunden (fehlgeschlagene und erfolgreiche). (nicht veröffentlicht) |
| `stop.limit` | string | Die Grenze, wie in der Konfiguration geschrieben: `"5%"`. (nicht veröffentlicht) |
| `stop.at_us` | int | Die Zeit vom Start des Laufs bis zum Stopp, in ganzen Mikrosekunden. (nicht veröffentlicht) |
| `stop.in_flight` | int | Die Aufrufe im Flug in diesem Moment (begonnen, nicht beendet): bei einem hängenden Ziel das, was das Fenster nicht zeigt. (nicht veröffentlicht) |
| `started_at` | string | Beginn des Plans, RFC 3339 UTC, Aufwärmen eingeschlossen. Jedes `_us` und `_s` zählt ab hier |
| `duration_us` | int | Wie lange der Lauf ging, Aufwärmen eingeschlossen |
| `planned_us` | int | Wie lange er gehen sollte, der Anlauf eingeschlossen |
| `warmup_us` | int | Das Fenster, das die Statistik nicht zählt: Anlauf und Aufwärmen. Ohne Anlauf — die Länge des Aufwärmens; für das Aufwärmen allein nehmen Sie `warmup_us` − `settings.ramp_us` |
| `settings` | object | Womit der Lauf gestartet wurde, wie konfiguriert, nicht wie gemessen |
| `settings.mode` | string | Das `app.mode` der Konfiguration: `"local"` oder `"real"`. Nicht das oberste `mode`, das ist die Art des Berichts. (nicht veröffentlicht) |
| `settings.max_in_flight` | int; Suche: `int?` | Die Obergrenze für Anfragen im Flug: der Wert von `-max-in-flight`, sonst der Standard. Suche: `null` ohne die Option (jeder Lauf nimmt die kleinste Grenze, mit der er läuft); in ihren Läufen die Grenze dieses Laufs |
| `settings.connections` | int | `app.connections` wie konfiguriert, standardmäßig 1; `connections.open` ist, was sich geöffnet hat |
| `settings.warmup_us` | int | Das Aufwärmen wie konfiguriert. Suche: die Einschwingzeit `settle` |
| `settings.ramp_us` | int | `load.ramp` wie konfiguriert, in ganzen Mikrosekunden; 0 ohne Anlauf. |
| `settings.stop_on_errors` | string? | `load.stop_on_errors` wie geschrieben: `"5%"`, `"100%"` bleibt `"100%"`; `null` — weggelassen. (nicht veröffentlicht) |
| `settings.compression` | string? | `app.compression` wie konfiguriert: `"gzip"`, `null` — weggelassen |
| `passport` | object? | Die Maschine und die Konfiguration, mit denen der Lauf gemacht wurde, um Läufe zu unterscheiden. Eine Suche hat ihn einmal auf oberster Ebene, ihre Läufe in `breakpoint.runs` nicht |
| `passport.config_sha256` | string | SHA-256 der Bytes der Konfigurationsdatei, wie gelesen, 64 Hexziffern |
| `passport.os`, `passport.arch` | string | `GOOS` und `GOARCH` des Generators, etwa `linux` und `amd64` |
| `passport.num_cpu` | int | Logische CPUs, die der Prozess des Generators sieht |
| `passport.gomaxprocs` | int | `GOMAXPROCS` beim Start |
| `passport.cpu_quota` | float? | Die CPU-Quote der cgroup in CPUs, wie gesetzt (`4`, `0.5`), nicht durch `gomaxprocs` gekappt; `null` — keine Quote oder nicht Linux |
| `passport.go_version` | string | Die Go-Version, mit der das Binary gebaut wurde |
| `passport.labels` | object | Die Labels, Schlüssel auf String; `{}` ohne welche |
| `sent` | int | Gemessene Aufrufe, außer `not_sent`. `unreachable` und `aborted` sind enthalten |
| `failed` | int | Aus `sent` — jeder Aufruf, der nicht erfolgreich war, außer `aborted` |
| `aborted` | int | Durch unser eigenes Anhalten abgeschnitten |
| `not_sent` | int | Das Timeout lief vor dem Senden ab: weder in `sent` noch in `failed` |
| `not_sent_generator`, `not_sent_stream`, `not_sent_connection` | int | `not_sent` nach Ursache: Der Generator war spät, kein freier Stream, keine bereite Verbindung. Zusammen ergeben sie `not_sent` |
| `warmup_sent`, `warmup_failed`, `warmup_not_sent` | int | Dasselbe für Aufrufe, die im ausgeschlossenen Fenster geplant waren (Anlauf und Aufwärmen) |
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
| `connections.first_limit`, `connections.last_limit` | int? | `MAX_CONCURRENT_STREAMS` beim ersten und letzten Handshake; `null` — nicht angekündigt (`0` — null angekündigt). Bei mehreren Verbindungen immer `null`: jede hat eigene Limits |
| `connections.limit_changes` | int | Handshakes, die ein anderes Limit als das vorige ankündigten |
| `connections.resolved` | []string? | Die Adressen, für die das Ziel steht, in der Reihenfolge des Resolvers: alle, auch bei weniger Verbindungen. Eine IP-Adresse steht für sich; `null` bei einer Verbindung |
| `connections.in_flight_limit` | int? | Wie viele Aufrufe das Ziel gleichzeitig in Flug erlaubt: das Limit der einen Verbindung oder die Summe aller Limits; `null`, solange eine Verbindung keines angekündigt hat |
| `connections.per_connection` | []object? | Ein Objekt je Verbindung, in der Nummerierung des Berichtsblocks; `null` bei einer Verbindung |
| `connections.per_connection[].address` | string | Die Adresse der Verbindung |
| `connections.per_connection[].calls` | int | Der Verbindung zugewiesene Aufrufe, gesendet oder nicht, ohne Warm-up |
| `connections.per_connection[].failed` | int | Die Fehler der Verbindung: die Regel von `failed` des Laufs plus die Aufrufe, die nicht hinausgingen, weil die Verbindung nicht bereit war |
| `connections.per_connection[].stream_waited`, `connections.per_connection[].not_sent_stream` | int | Aufrufe der Verbindung, die auf einen freien Stream warteten: die nach dem Warten hinausgingen und die beim Warten ihr Zeitlimit erreichten |
| `connections.per_connection[].p99` | percentile? | p99 der Aufrufe der Verbindung; `null` ohne einen Aufruf mit Latenz |
| `connections.per_connection[].first_limit`, `connections.per_connection[].last_limit` | int? | Das Stream-Limit der Verbindung beim ersten und letzten Handshake; `null` — nicht angekündigt |
| `connections.per_connection[].limit_changes` | int | Handshakes der Verbindung, die ein anderes Limit als das vorige ankündigten |
| `client_waits` | object | Aufrufe, die auf Client-Seite über der Schwelle warteten, nach Ursache ([README](README.md#den-bericht-lesen)) |
| `client_waits.generator_calls`, `client_waits.stream_calls`, `client_waits.connection_calls` | int | Alle solchen Aufrufe, gesendet oder nicht. Ein gesendeter Aufruf kann für mehrere Ursachen zählen |
| `client_waits.generator_tail_calls`, `client_waits.stream_tail_calls`, `client_waits.connection_tail_calls` | int | Nur im p99-Ende und unter den ungesendeten: Diese bestimmen `tail_wait_cause` |
| `generator_cpu` | object? | Wie viel der für LeetTest selbst verfügbaren CPU der Lauf belegte, sekundenweise; `null`, wenn die CPU-Zeit des Prozesses nicht gelesen werden konnte. Der Bericht druckt eine Zeile, wenn eine Sekunde ausgelastet ist |
| `generator_cpu.available_cores` | float | Wie viel CPU der Prozess nutzen darf: `GOMAXPROCS` oder das cgroup-Kontingent, wenn es kleiner ist |
| `generator_cpu.share` | float | CPU-Zeit geteilt durch Wanduhrzeit und `available_cores`, auf 2 Stellen gerundet, über die geplanten Sekunden nach dem Warm-up. Kann über 1 liegen: Zeit in Systemaufrufen läuft außerhalb der Ps |
| `generator_cpu.busy_seconds` | int | Wie viele dieser Sekunden einen `share` von 0.90 oder mehr haben (verglichen wird der gerundete Wert). `0`, wenn sie zusammen unter einer Sekunde liegen |
| `generator_cpu.first_busy_s` | int? | Die erste solche Sekunde ab Laufbeginn, das Warm-up zählt mit; `null`, wenn es keine gibt |
| `generator_cpu.seconds` | []float | Der Anteil jeder Sekunde ab Laufbeginn, Warm-up und Timeout-Ende eingeschlossen: so viele Einträge wie das längste `methods[].seconds`. `null` für eine Sekunde, die keine Messung abdeckt, und für eine letzte Sekunde unter einer halben, die mit der vorigen zusammen gezählt wird |
| `methods` | []object | Methoden, siehe unten |
| `unchecked` | []object | Methoden, die über Reflection nicht geprüft werden konnten |
| `unchecked[].method` | string | Die Methode |
| `unchecked[].reason` | string | `reflection_off` oder `reflection_failed` |
| `unchecked[].error` | string | Text für Menschen, ändert sich mit grpc-go |
| `thresholds` | []object? | Urteile über die `pass`-Grenzen, ein Objekt je Grenze, alle mit denselben Schlüsseln; die, die nicht zu einer Grenze gehören, sind `null`. `null` — keine Grenzen gesetzt (keine leere Liste, die sich als „alles bestanden“ läse). Eine Suche schreibt es auf oberster Ebene |
| `thresholds[].method` | string? | Die Methode mit führendem Schrägstrich, wie der Bericht sie nennt; `null` — die Grenze einer Suche |
| `thresholds[].key` | string | `p50`, `p90`, `p95`, `p99`, `p999`, `errors`, `held_rps` |
| `thresholds[].verdict` | string | `pass`, `fail`, `unknown` |
| `thresholds[].reason` | string? | Warum `unknown`, aus der Liste: `incomplete`, `invalid`, `few_observations`, `run_limit`, `generator`, `stream`, `client_errors`, `nothing_sent`, `censored`; `null` bei `pass` und `fail` |
| `thresholds[].limit_us` | int? | Eine Latenzgrenze, ganze Mikrosekunden; `null` bei den anderen Schlüsseln |
| `thresholds[].value` | percentile? | Das gemessene Perzentil, mit dem verglichen wurde; `null` — nicht gemessen, oder der Schlüssel betrifft nicht die Latenz |
| `thresholds[].limit_percent` | string? | Eine `errors`-Grenze, wie in der Konfiguration geschrieben, etwa `"0.5%"`; `null` bei den anderen Schlüsseln |
| `thresholds[].bad`, `thresholds[].bad_client`, `thresholds[].total` | int? | Der `errors`-Anteil: fehlgeschlagene Aufrufe (mit den mangels Verbindung nicht gesendeten), davon die, die nicht das Ziel entschieden hat, alle gezählten. `null` bei den anderen Schlüsseln und wenn kein einziger Aufruf gezählt wurde |
| `thresholds[].limit_rps`, `thresholds[].held_rps` | int? | Die Grenze einer Suche und was sie gehalten hat; `null` bei den anderen Schlüsseln |
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
| `methods[].planned_rps_low`, `methods[].planned_rps_high` | int | Niedrigste und höchste geplante Rate des gemessenen Teils des Plans, nach dem ausgeschlossenen Fenster: Ein Aufruf mit Anlauf gibt sein `rps` zweimal |
| `methods[].planned_duration_us` | int | Wie lange die Methode gehen sollte, wie konfiguriert, der Anlauf eingeschlossen |
| `methods[].request_bytes` | object? | Der Anfragekörper, Mittel über die gemessenen Aufrufe, die hinausgingen; `null` ohne Kompression |
| `methods[].request_bytes.raw`, `methods[].request_bytes.wire` | float | Bytes vor der Kompression und auf der Leitung (ohne den 5-Byte-Rahmen) |
| `methods[].latency` | object | Latenz erfolgreicher Aufrufe, mit Timeouts und abgebrochenen Aufrufen als Untergrenzen |
| `methods[].latency.min`, `methods[].latency.p50`, `methods[].latency.p90`, `methods[].latency.p95`, `methods[].latency.p99`, `methods[].latency.max` | percentile? | Minimum, Perzentile, Maximum |
| `methods[].latency.p999` | percentile? | p99.9 nach derselben Regel; null, solange die Methode weniger als 1000 Beobachtungen (`observations`) hat, dann wäre p99.9 das Maximum |
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
| `methods[].dataset` | object? | Die Anfragedatei des Aufrufs; `null` — er hat keine |
| `methods[].dataset.file` | string | Der Pfad, wie ihn die Konfiguration schreibt |
| `methods[].dataset.sha256` | string | SHA-256 der Datei, wie sie auf der Platte liegt, eine Byte-Order-Mark eingeschlossen, 64 Hexziffern |
| `methods[].dataset.records` | int | Anfragen in der Datei |
| `methods[].dataset.used` | int | Wie viele davon mindestens einmal rausgingen |
| `methods[].dataset.used_max` | int | Wie oft die am häufigsten genutzte rausging; in einer Suche — vom ersten Schritt bis zum Ende dieses Laufs |
| `methods[].dataset.order` | string | `file` oder `random`: in welcher Reihenfolge die Anfragen rausgingen |
| `methods[].dataset.seed` | int? | Seed einer zufälligen Reihenfolge, 0 bis 9007199254740991; `null` bei der Reihenfolge der Datei |
| `methods[].dataset.reuse` | bool | `false` — der Aufruf sendet jeden Datensatz höchstens einmal (`dataset_reuse: false`); sonst `true` |
| `methods[].seconds` | []object | Die Zeitleiste pro Sekunde, siehe unten |

### Sekunde: `methods[].seconds[]`

Die Zeitleiste deckt den ganzen Lauf ab, Aufwärmen eingeschlossen, bis zur letzten Sekunde, in der
etwas geschah. Anders als die Summen enthält sie jeden Aufruf. Zum Abgleich: `Σ begun` +
`outside_timeline` ist jeder Aufruf der Methode.

| Feld | Typ | Bedeutung |
|---|---|---|
| `methods[].seconds[].warmup` | bool | Die Sekunde liegt im ausgeschlossenen Fenster (Anlauf und Aufwärmen) |
| `methods[].seconds[].begun` | int | In dieser Sekunde begonnene Aufrufe |
| `methods[].seconds[].succeeded`, `methods[].seconds[].overload`, `methods[].seconds[].failure`, `methods[].seconds[].client_error`, `methods[].seconds[].bad_response`, `methods[].seconds[].request_error`, `methods[].seconds[].timed_out`, `methods[].seconds[].unreachable`, `methods[].seconds[].cut_off`, `methods[].seconds[].aborted`, `methods[].seconds[].unclassified` | int | In dieser Sekunde beendete Aufrufe, nach Kategorie |
| `methods[].seconds[].not_sent_generator`, `methods[].seconds[].not_sent_stream`, `methods[].seconds[].not_sent_connection` | int | Timeouts vor dem Senden, die in dieser Sekunde endeten, nach Ursache |
| `methods[].seconds[].in_flight` | int | Aufrufe in Flug am Ende der Sekunde |
| `methods[].seconds[].lag_calls`, `methods[].seconds[].lag_sum_us`, `methods[].seconds[].lag_max_us` | int | Über die in dieser Sekunde geplanten Aufrufe: Anzahl, Summe und Maximum der Startverspätung |
| `methods[].seconds[].observed_calls` | int | Davon die erfolgreichen |
| `methods[].seconds[].observed_lag_sum_us`, `methods[].seconds[].transport_wait_sum_us`, `methods[].seconds[].service_time_sum_us` | int | Über die erfolgreichen: Summe der Startverspätung, der Wartezeit vor dem Senden (Verbindung und Stream), der Zeit vom Senden bis zur Antwort. Zusammen — ihre Latenzen |

### Suche nach dem Bruchpunkt: `mode: "breakpoint"`

Die Suche schreibt ein anderes Objekt: `schema_version`, `leettest_version`, `target` und
`started_at` wie beim Lauf oben, die übrigen Felder eines gewöhnlichen Laufs stehen nicht auf der
obersten Ebene. Jeder Lauf liegt mit seinem vollständigen Bericht in `breakpoint.runs` ([die Suche](README.md#suche-nach-dem-bruchpunkt)).

| Feld | Typ | Bedeutung |
|---|---|---|
| `method` | string | Die Methode, die die Suche belastet hat |
| `breakpoint` | object | Ergebnis der Suche |
| `breakpoint.outcome` | string | Geschlossene Liste: `broke`, `broke_at_first`, `held_all`, `run_limit`, `stopped`, `invalid` |
| `breakpoint.held_rps` | int? | Die höchste Rate, die das Ziel gehalten hat; `null` — keine gehalten |
| `breakpoint.broke_rps` | int? | Die niedrigste Rate, bei der das Ziel brach oder ein Lauflimit die Suche stoppte; `null` — es brach nicht |
| `breakpoint.errors_limit` | string | Die Fehlergrenze der Suche als Prozent: standardmäßig `"1%"`. Eine Stufe brach darüber; `100%` — Fehler brechen keine Stufe |
| `breakpoint.why` | string? | Ursache des Laufs, der die Suche beendet hat, aus einer geschlossenen Liste: `errors`, `p99_limit`, `p99_vs_base`, `connection`, `no_recovery`, `generator`, `in_flight_cap`, `stream_limit`, `stream_wait`, `client_errors`, `clock_step`, `request_errors`; `null` — keine Ursache. `client_errors`: die Grenze wurde nur durch Fehler auf der Seite des Clients überschritten, die eigenen Fehler des Ziels liegen innerhalb davon; das Ergebnis ist `run_limit` |
| `breakpoint.stop_reason` | string? | Wer die Suche beendet hat: `dataset_exhausted` (eine Datei mit `dataset_reuse: false` war aufgebraucht), `requested` (vom Nutzer gestoppt); `null` — die Suche endete von selbst |
| `breakpoint.notes` | []string | Hinweise der Suche, Text für Menschen in ASCII; nicht auswerten |
| `breakpoint.runs` | []object | Alle Läufe der Reihe nach |
| `breakpoint.runs[].kind` | string | `step`, `repeat` oder `probe` |
| `breakpoint.runs[].planned_rps` | int | Geplante Rate des Laufs |
| `breakpoint.runs[].sent_rps` | int | Rate, mit der die Aufrufe tatsächlich hinausgingen: gehalten gilt nur, was gesendet wurde |
| `breakpoint.runs[].broken` | bool | `true` — der Lauf hat das Ziel gebrochen |
| `breakpoint.runs[].recovered` | bool? | Bei einem `probe`: das Ziel ist innerhalb des 1,5-Fachen des p99 der Basis zurück; bei `step` und `repeat` `null` |
| `breakpoint.runs[].why` | string? | Ursache des Laufs, dieselbe geschlossene Liste wie bei `breakpoint.why`; `null` — keine Ursache |
| `breakpoint.runs[].report` | object | Vollständiger Bericht dieses Laufs, dasselbe Objekt wie das JSON eines gewöhnlichen Laufs (Felder oben) |

## Berichte vergleichen: `leettest compare`

`leettest compare [Flags] vorher.json nachher.json` liest zwei `-json-file`-Berichte derselben Art —
zwei Suchen oder zwei Läufe — und gibt aus, was sich geändert hat. Die Flags stehen vor den Dateien.

| Flag | Was es tut |
|---|---|
| `-max-capacity-drop` | Zwei Suchen: die Grenze für den Rückgang der Kapazität (`held_rps`) in Prozent der Kapazität „vorher“, zum Beispiel `10%`. Ohne sie wird die Kapazität ausgegeben, aber nicht beurteilt |
| `-max-p99-rise` | Zwei Läufe: die Grenze für den Anstieg des p99 jeder Methode in Prozent des p99 „vorher“, zum Beispiel `20%` |
| `-max-errors-rise` | Zwei Läufe: die Grenze für den Anstieg des Anteils fehlgeschlagener Aufrufe in Prozentpunkten, zum Beispiel `1%` |
| `-output` | Format des Berichts auf stdout: `text` (weggelassen) oder `json` für Skripte und CI. Jeder andere Wert — Code 1, die Dateien werden nicht gelesen |

Die Exit-Codes sind für beide Formate dieselben: `0` — innerhalb der Grenze oder keine Grenzen, `1` —
ein Bericht ließ sich nicht lesen oder die Flags passen nicht zusammen, `2` — die Berichte sind nicht
vergleichbar oder eine Grenze lässt sich nicht entscheiden, `4` — eine Grenze ist verletzt (auch wenn
daneben eine Prüfung steht, die sich nicht entscheiden lässt).

## JSON des Vergleichs

`leettest compare -output json` schreibt auf stdout ein Objekt und einen Zeilenumbruch, sonst nichts;
bei Exit-Code 1 ist stdout leer. Exit-Codes und stderr sind dieselben wie beim Text. Im Objekt stehen
die eigenen Zahlen der Berichte und die Urteile; einen Änderungsprozentsatz enthält es nicht — das
Skript rechnet ihn selbst aus den Werten. Der Grund eines `unknown` ist ein Wert aus einer
geschlossenen Liste in `checks[].reason`, und `why` sind dieselben Worte, die der Text ausgibt.

Methodennamen sind auf zwei Arten geschrieben. In `differences` und im Text steht die Methode ohne
führendes `/` (`pkg.Svc/Do`). In `checks[].method` steht der Name wie im Bericht: bei einem Lauf mit
führendem `/` (`/pkg.Svc/Do`), bei einer Suche ohne.

Ein Feld, das es für die Art der Prüfung nicht gibt, ist `null`: bei `capacity` sind `*_p99` und
`*_errors` leer, bei `p99` die übrigen. Ein Urteil gibt es nur, wenn die Prüfung eine Grenze hat.

| Feld | Typ | Bedeutung |
|---|---|---|
| `schema_version` | int | Version des Schemas, derzeit `1` |
| `mode` | string | Immer `"compare"` |
| `compared_mode` | string | Art der verglichenen Berichte: `"run"` oder `"breakpoint"` |
| `leettest_version` | string | Version von LeetTest, das verglichen hat (nicht die der Berichte) |
| `base`, `new` | object | Der Bericht „vorher“ und der Bericht „nachher“ |
| `base.path`, `new.path` | string | Pfad zur Datei, wie auf der Kommandozeile angegeben |
| `base.name`, `new.name` | string | Name des Berichts; ein leerer bleibt `""` |
| `base.started_at`, `new.started_at` | string | Beginn, wie im Bericht |
| `base.leettest_version`, `new.leettest_version` | string | Version von LeetTest, das den Bericht geschrieben hat |
| `base.labels`, `new.labels` | map | Labels des Passes; `{}` ohne Pass oder ohne Labels |
| `comparable` | bool | `true` — die Berichte unterscheiden sich in nichts, was sie unvergleichbar macht, und die Läufe haben eine gemeinsame Methode |
| `differences` | []object | Was die Berichte unvergleichbar macht, in der Reihenfolge des Vergleichs; `[]`, wenn nichts |
| `differences[].field` | string | Name des Feldes wie in der Zeile `differs:` (`target`, `method`, `leettest_version`, `passport.config_sha256`, `methods`, `dataset <Methode>`). Fehlt der Pass — `passport` |
| `differences[].base` | string | Wert „vorher“; `none` — kein `dataset` oder kein Pass |
| `differences[].new` | string | Wert „nachher“; bei `passport` — `present` dort, wo ein Pass da ist |
| `verdict` | string? | Das schlechteste Urteil der Prüfungen mit Grenze: `fail`, sonst `unknown`, sonst `pass`; `null` — keine Grenze gesetzt |
| `checks` | []object | Prüfungen in der Reihenfolge des Textes: bei einer Suche eine (`capacity`), bei einem Lauf für jede gemeinsame Methode in der Reihenfolge „vorher“ erst `p99`, dann `errors`; `[]`, wenn es nichts zu vergleichen gibt |
| `checks[].method` | string | Methode der Prüfung, die Namen siehe oben |
| `checks[].key` | string | `capacity`, `p99` oder `errors` |
| `checks[].base_capacity`, `checks[].new_capacity` | object? | Kapazität der Suche; `null` — nicht gemessen oder eine andere Art der Prüfung |
| `checks[].base_capacity.rps`, `checks[].new_capacity.rps` | int | Kapazität in rps |
| `checks[].base_capacity.bound`, `checks[].new_capacity.bound` | string | `exact` — die Suche brach, `rps` ist die höchste gehaltene Rate; `lower` — endete vor dem Bruch, die Kapazität ist mindestens `rps`; `upper` — brach auf der ersten Stufe, die Kapazität liegt unter `rps` |
| `checks[].base_outcome`, `checks[].new_outcome` | string? | `breakpoint.outcome` der Suche; nur bei `capacity` |
| `checks[].base_p99`, `checks[].new_p99` | object? | p99 der Methode — dasselbe Objekt wie `latency.p99` des Laufberichts; `null` — zu wenige Messungen oder eine andere Art der Prüfung |
| `checks[].base_p99.us`, `checks[].new_p99.us` | int | p99 in ganzen Mikrosekunden |
| `checks[].base_p99.ns`, `checks[].new_p99.ns` | int | p99 in ganzen Nanosekunden; der Anstieg wird danach berechnet. Bei einem Bericht ohne `ns` — `us` × 1000 |
| `checks[].base_p99.lower_bound`, `checks[].new_p99.lower_bound` | bool | `true` — das Ende der Verteilung stieß ans Timeout, der wahre p99 ist größer |
| `checks[].base_errors`, `checks[].new_errors` | object? | Anteil fehlgeschlagener Aufrufe der Methode in Teilen; `null` — eine andere Art der Prüfung |
| `checks[].base_errors.bad`, `checks[].new_errors.bad` | int | Alle fehlgeschlagenen Aufrufe und die von der Verbindung verlorenen |
| `checks[].base_errors.bad_client`, `checks[].new_errors.bad_client` | int | Der Teil von `bad`, der nichts über das Ziel sagt: abgelehnte Anfragen, Client-Fehler, unsere `bad_response` |
| `checks[].base_errors.total`, `checks[].new_errors.total` | int | Gesendete und von der Verbindung verlorene Aufrufe; `0` bleibt `0` |
| `checks[].limit_percent` | string? | Grenze der Prüfung, wie im Flag geschrieben (`"20%"`); `null` — keine Grenze gesetzt |
| `checks[].verdict` | string? | `pass`, `fail` oder `unknown`; `null` — keine Grenze |
| `checks[].reason` | string? | Nur bei `unknown`, ein Wert aus der Liste unten; sonst `null` |
| `checks[].why` | string? | Nur bei `unknown`: dieselben Worte, die der Text nach `unknown, ` ausgibt; nicht auswerten |

Die Gründe für `unknown` sind eine geschlossene Liste; von den ersten sechs gewinnt der erste, der
passt:

- `not_comparable` — die Berichte sind nicht vergleichbar, die Zahlen werden nicht gegenübergestellt;
- `method_invalid` — die Methode einer Seite ist ungültig;
- `invalid` — der Lauf einer Seite ist ungültig;
- `incomplete` — der Lauf einer Seite ist nicht zu Ende gekommen;
- `generator` — eine Seite wurde vom Generator selbst gebremst;
- `stream` — eine Seite wurde von der Stream-Grenze gebremst;
- `few_observations` — zu wenige Messungen für den p99;
- `base_zero` — die Basis ist null (p99 0 µs oder Kapazität 0 rps), einen Anteil davon gibt es nicht;
- `censored` — der p99 stieß ans Timeout und entscheidet die Grenze nicht;
- `nothing_sent` — auf einer Seite wurde nichts gesendet;
- `client_errors` — die Fehler des Clients selbst entscheiden;
- `not_measured` — die Kapazität einer Seite ist nicht gemessen;
- `broke_at_first` — die Suche brach auf der ersten Stufe, ein Rückgang ist nicht zu sehen;
- `lower_bound` — die Suche endete vor dem Bruch, die Kapazität ist eine Untergrenze.
