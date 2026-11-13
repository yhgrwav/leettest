# Bericht, Exit-Codes, JSON, Vergleich

[← Zur Dokumentation](README.md)

> Übersetzt aus [docs/ru/report.md](../ru/report.md) bei 4cba437, 2026-11-10. Bei Abweichungen gilt die
> russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/report.md).

Hier stehen die Abschnitte, die früher in der README standen: wie man den Bericht liest, Grenzen,
Exit-Codes, JSON, die Suche nach dem Bruchpunkt, der Vergleich von Läufen. Jedes Feld und jedes
Flag steht in der [Referenz](../en/reference.md) (englisch).

## Den Bericht lesen

Am Ende steht der Bericht je Methode: gesendet, Fehlschläge, `sent/s`, p50/p90/p95/p99.

- Ein Aufruf wird ab dem **geplanten** Zeitpunkt gerechnet: Haben ihn das Ziel oder der Generator
  verzögert, steckt das in der Latenz.
- `sent/s` — gesendete Aufrufe, geteilt durch die Sendezeit: Ein Hängen des Ziels am Ende des Laufs
  drückt diese Zahl nicht.
- Ein Perzentil, an dessen Stelle durch Timeout abgebrochene Aufrufe hätten stehen können, wird als
  **untere Grenze** gedruckt: `>2.0s`. Das ist kein Wert, sondern „nicht weniger als“.
- Unter der Tabelle der Methode steht der Block `over time`: der gemessene Teil des Laufs, ab dem
  Ende des Warm-ups, in höchstens 12 Zeilen; im JSON — bis zu 60 Fenster. Ein Aufruf gehört zu der
  Zeile seines geplanten Zeitpunkts, wann immer er endete. In der Zeile: `sent/s`, Fehleranteil, p50
  und p99 mit zwei signifikanten Stellen, daher kann das p50 der Zeile vom p50 der Tabelle in der
  letzten Stelle abweichen. Die Zeile `no calls` heißt, dass in diesem Fenster kein einziger Aufruf
  rausging. Liegt das Tempo unter einem Aufruf je Fenster, ist das vom Plan so gewollt und nicht
  kaputt: Gleichen Sie mit `plan:` der Methode ab. Bei `ramp` sind die ersten Zeilen der Anlauf: Ein
  niedriges `sent/s` ist dort der Plan, kein Ausfall; suchen Sie die Zeile, in der p99 oder der
  Fehleranteil wachsen, bevor das Tempo das Plateau erreicht hat.
- Bei `ramp` hat der Bericht für jede Methode außerdem den Block `ramp by second`: die Sekunden des ausgeschlossenen Fensters, in jeder — das Plantempo, die
  rausgegangenen Aufrufe, die auf sie geplant waren, und der Anteil der fehlgeschlagenen unter
  ihnen. Nur ganze Sekunden, höchstens 20 Zeilen. Der Fehleranteil ist eine untere Grenze: Aufrufe,
  die am Ende des Laufs noch unterwegs waren, zählen nicht als fehlgeschlagen. Die Suche nach dem
  Bruchpunkt zeigt diesen Block für ihre erste Stufe.
- Die Zeile `traffic:` im Kopf des Laufs — wie viele Bit pro Sekunde durch die Bodys der Nachrichten
  gingen: gesendet und empfangen. Das ist eine untere Grenze: ohne HTTP/2-Frames, Header und TLS;
  der Empfang wird nach der Größe vor dem Entpacken gerechnet. Unter der Methode steht
  `response: N B per call` — die mittlere Größe der Antwort erfolgreicher Aufrufe, und beim Warm-up
  `warm-up: N calls, p50, p99` — seine eigenen Zahlen, getrennt vom Lauf.
- Unter einer Methode mit `dataset` steht die Zeile `data:`: wie viele Anfragen der Datei benutzt
  wurden und wie oft die häufigste rausging. Das heißt nicht, dass das Ziel so viele verschiedene
  Anfragen gesehen hat.
- Standardmäßig geht die Last über **eine Verbindung**, und die landet auf **einem Backend**: hinter
  einem L4-Balancer (Kubernetes ClusterIP, NLB) und wenn der DNS mehrere Adressen zurückgibt. Der
  Schluss „hält X nicht“ betrifft dieses Backend, nicht den Dienst, und der Bericht zeigt das nicht.
  Um mehrere zu belasten, setzen Sie `app.connections`: Der Block `Connections:` im Bericht zeigt
  eine Zeile je Verbindung — Adresse, Aufrufe, Fehleranteil und p99, sodass ein zurückbleibendes
  Backend sichtbar wird. Ein L7-Balancer, der einzelne Anfragen verteilt (Envoy, gRPC-Ingress),
  verteilt auch eine einzelne Verbindung. Der Bericht druckt, wie oft sich die Verbindung neu
  verbunden hat und welches Limit gleichzeitiger Streams das Ziel angegeben hat.

**Kategorien.** Antworten außer den erfolgreichen sind nach Zeilen getrennt, jede mit eigenen
Perzentilen. Den Status konnte nicht das Ziel geschickt haben, sondern ein Proxy davor: nginx ohne
lebendes Backend antwortet `UNAVAILABLE`, und der Client unterscheidet das eine nicht vom anderen.

- `request error` — der Aufruf wäre bei jedem Tempo gescheitert: eine falsche Anfrage oder eine
  Anfrage, die Ziel oder Proxy für zu groß hielten.
- `overload` — der Status sagt „überlastet oder nicht erreichbar“: `RESOURCE_EXHAUSTED`,
  `UNAVAILABLE`. Das sind Worte des Status, keine Diagnose des Ziels.
- `failure` — der Status sagt, dass der Aufruf kaputtging: `INTERNAL`, `UNKNOWN`, `DATA_LOSS`,
  `CANCELLED` von der anderen Seite, `ABORTED` (ein Konflikt gleichzeitiger Änderungen, kein Mangel
  an Kapazität).
- `bad response` — die Antwort kam, aber der Client nahm sie nicht an: größer als
  `app.max_response_size` oder mit einer Kodierung komprimiert, die der Client nicht angekündigt
  hatte, oder sie verletzt die Unarität: OK ohne Nachricht oder eine überzählige Nachricht.
- `client error` — der Client weigerte sich selbst zu senden: Die Anfrage ließ sich nicht kodieren,
  der Codec oder ein Interceptor fiel aus.

Aufrufe, die das Ziel nicht erreichten, zählen als Fehlschläge, gehen aber nicht in die Perzentile
ein. Eine Anfrage, die rausging, aber keinen Status bekam — die andere Seite setzte den Stream
zurück oder brach die Verbindung ab —, wird gesondert als `cut off` gezählt: Ziel oder Proxy
konnten sie verarbeitet haben, bei einer Schreiboperation ist das ein Anlass, auf Dubletten zu
prüfen. Ein Aufruf, den unser Stopp unterbrach (Strg+C, SIGTERM), ist `aborted` bei jedem Code: Das
ist kein Fehlschlag des Ziels.

| Code | Status kam übers Netz | Code vom Client gesetzt, Anfrage ging raus | Code vom Client gesetzt, Anfrage ging nicht raus |
|---|---|---|---|
| `INVALID_ARGUMENT`, `NOT_FOUND`, `ALREADY_EXISTS`, `PERMISSION_DENIED`, `UNAUTHENTICATED`, `FAILED_PRECONDITION`, `OUT_OF_RANGE`, `UNIMPLEMENTED` | `request error` | `cut off` | `client error` |
| `RESOURCE_EXHAUSTED` | `overload`; „larger than max“ von grpc-go — `request error` | `cut off`; eine Antwort über unserem Limit — `bad response` | `client error` |
| `UNAVAILABLE` | `overload` | `cut off`; ein vom Ziel vor der Verarbeitung abgelehnter Stream — `overload` | `unreachable`; ebenso |
| `CANCELLED`, `UNKNOWN`, `INTERNAL`, `DATA_LOSS`, `ABORTED` | `failure`; OK ohne Antwort von grpc-go — `bad response` | `cut off`; die Antwort ließ sich nicht entpacken — `bad response` | `client error` |
| `DEADLINE_EXCEEDED` | Timeout | Timeout | Timeout, nicht gesendet |

Eine zu große Anfrage erkennt man am Fehlertext von grpc-go. Ein Ziel auf anderer Implementierung
(Envoy, Java) schreibt ihn anders, und eine solche Ablehnung landet in `overload`, nicht in
`request error`.

`UNAVAILABLE` bei einem abgelehnten Stream ist die Übersetzung von grpc-go für RST_STREAM
REFUSED_STREAM vom Ziel: Den Code setzte der Client, die Ablehnung schickte aber das Ziel, daher
steht er in der Zeile „sent by the target“. Einen solchen Stream hat das Ziel nicht verarbeitet
(RFC 9113 §8.7).

Unter dem Bericht sind die gescheiterten Aufrufe nach gRPC-Codes in zwei Zeilen aufgeteilt. „sent by
the target“ — der Status kam übers Netz, vom Ziel oder von einem Proxy. „set by the client“ — den
Code setzte der Client selbst: Niemand antwortete, der Stream wurde zurückgesetzt, unsere Deadline
lief ab oder der Client nahm die Antwort nicht an. `DEADLINE_EXCEEDED` vom Ziel ist meist unsere
eigene Deadline: Sie geht im Header `grpc-timeout` ans Ziel. Die Kategorie sagt, wessen Schuld es
ist, der Code — was in den Logs des Ziels zu suchen ist.

**Wartezeiten auf der Clientseite.** In die Latenz geht alles ein, worauf der Aufruf bei uns
gewartet hat: die Verspätung des Generators, das Warten auf die Verbindung, das Warten auf einen
freien Stream. Fällt ohne sie p99 mindestens einer Methode um 10 % oder mehr oder ging ein Teil der
Aufrufe nie raus, fällt der Bericht ein Urteil und nennt die Ursache, die im p99-Schwanz am
häufigsten vorkommt, und druckt p99 ohne Wartezeiten — die Zeit, die der Aufruf beim Ziel
verbrachte. Ist die Ursache, dass die Streams ausgingen, ist eine Kapazität des Ziels über „Limit ×
Verbindungen“ nicht gemessen. Wurde vor dem Aufruf auf den Resolver gewartet, gehen in das Warten
auf die Verbindung auch die Interceptoren des Aufrufers ein, daher kann p99 ohne Wartezeiten etwas
zu niedrig ausfallen. Nicht gesendete Aufrufe sind nach Ursachen aufgeteilt. Eine Verschiebung unter
10 % ist kein Urteil, sondern ein Hinweis mit der Zahl; ist p99 eine untere Grenze, ist die
Verschiebung unbekannt, und einen Hinweis gibt es nicht.

**Uhr.** Das Werkzeug misst den Schritt der Uhr des Hosts vor und nach dem Lauf. Ist der Schritt
spürbar, wird die Zeile `clock step 502us on this host: every latency and wait is +/- 502us`
gedruckt. Unter Linux beträgt der Schritt Dutzende Nanosekunden, die Zeile fehlt.

### Der Laufpass

Über dem Bericht steht der Pass: auf welcher Maschine und mit welcher Konfiguration der Lauf
gemacht wurde, damit man zwei Berichte vergleichen kann, ohne zu raten, worin sie sich
unterscheiden. Ihn haben der Lauf und die Suche nach dem Bruchpunkt.

```text
machine: linux/amd64, 8 CPU, GOMAXPROCS 8, quota 4, go1.27.1; config sha256:3f9a1c0d2b7e
labels: commit=abc123, env=staging
```

- `quota` — das CPU-Limit des Containers (cgroup), wie es gesetzt ist; ohne Limit fehlt die Zeile.
- Der Hash — SHA-256 der Konfigurationsdatei, wie sie auf der Platte liegt; ihn zeigt auch
  `sha256sum load.yaml`; im Text die ersten 12 Ziffern, im JSON alle. Jede Änderung der Datei, auch
  eines Kommentars, ändert den Hash. Der Hash wird vor dem Einsetzen von `${ENV}` genommen:
  Geheimnisse in `metadata` schreiben Sie über `${ENV}`, nicht als Wert in der Datei.
- Labels — `labels:` in der Konfiguration und `-label schlüssel=wert` in der Kommandozeile (lässt
  sich wiederholen; das Flag überschreibt denselben Schlüssel aus der Konfiguration):

```sh
leettest -c load.yaml -label env=staging -label commit=abc123
```

Der Schlüssel — lateinische Buchstaben, Ziffern und `_.-`, bis zu 64 Zeichen; der Wert — eine
Zeichenkette bis zu 256 Byte, in YAML setzen Sie eine Zahl in Anführungszeichen (`build: "1.10"`).
Eine Verletzung — Code `1` vor dem Start. Im JSON ist der Pass das Feld `passport`, beim Dataset
steht neben `file` der `sha256`; alle Felder stehen in der
[Referenz](../en/reference.md#json-fields) (englisch).

## Wann ein Lauf ungültig ist

Ein Bericht ist da, aber die Zahlen handeln nicht von der Last auf dem Ziel (Code 2), wenn:

- **die Obergrenze `-max-in-flight` erreicht wurde.** Timeouts und Obergrenze werden vor dem Start so
  abgeglichen, dass ein hängendes Ziel sie nicht erreicht; ein Anstoßen heißt, dass dem Generator
  Prozessor fehlte.
- **alle gemessenen Aufrufe einer Methode `request error`, `client error` oder `bad response`
  sind.** Der Hinweis nennt die Methode, die Ursache und was zu tun ist.
- **die Uhr des Hosts gröber ist als ein Viertel von p50** irgendeiner Methode oder der Schritt im
  Lauf auf 1 µs und mehr gewachsen ist (`clock_step`). Messen Sie von Linux aus.
- **die Verspätung des Generators den p99-Schwanz entschied** (`generator_late`): Die Latenzen
  enthalten die Verspätung dieser Maschine und messen nicht das Ziel. Geben Sie dem Generator mehr
  CPU, senken Sie die Rate oder verteilen Sie sie auf Maschinen. Beurteilt wird eine Methode ab 100
  Beobachtungen. Der Bericht nennt die Methoden.
- **eine `pass`-Grenze sich nicht entscheiden lässt** (`thresholds_undecided`): zu wenige
  Beobachtungen, der Generator war verspätet, Aufrufe warteten auf einen freien Stream, ein Schwanz
  aus Aufrufen ohne Antwort. Der Block `Thresholds:` nennt die Grenze und die Ursache.

## Grenzen: ein Lauf als Prüfung

Ohne `pass` liefert ein Lauf, der bis zum Ende kommt, `0` zurück, egal wie das Ziel antwortet.
Damit der Lauf selbst sagt „hält / hält nicht“, setzen Sie Grenzen unter dem Aufruf:

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

Am Ende des Berichts erscheint ein Block: das schlechteste der Urteile und eine Zeile je Grenze.
Für einen gesunden Dienst:

```
Thresholds: passed
  wallet.v1.WalletService/GetBalance  p99     <= 200ms  pass  21.2ms
  wallet.v1.WalletService/GetBalance  errors  <= 1%     pass  0.0% (0 of 44000)
```

Schlüssel: `p50`, `p90`, `p95`, `p99`, `p999` und `errors`; eine Latenzgrenze verlangt `errors`
daneben, und `errors: 100%` hebt die Prüfung der Fehlschläge auf — dann ist auch die Latenz nichts
wert. Die Grenze der Suche nach dem Bruchpunkt ist `breakpoint.pass.held_rps`. Eine verfehlte Grenze
ergibt Code `4`, eine Grenze, die sich nicht entscheiden lässt (zu wenige Beobachtungen, der
Generator war verspätet, ein Schwanz aus Aufrufen ohne Antwort) — Code `2`. Alle Schlüssel, Urteile
und Ursachen stehen in der [Referenz](../en/reference.md#pass-limits) (englisch); ein Beispiel mit
allen Schlüsseln — [`examples/pass.yaml`](../../examples/pass.yaml).

## Exit-Codes

Ohne `pass` liefert ein Lauf, der bis zum Ende kommt, `0` zurück, wie auch immer das Ziel antwortet:
**`0` heißt nicht „der Dienst ist gesund“**. Mit `pass` heißt `0`, dass jede Grenze geprüft und
eingehalten wurde. Die Codes sind gerankt: Ein ungültiger Lauf (`2`) wiegt schwerer als ein
unvollständiger (`3`); ein Stopp, der den Bericht gedruckt hat, ist immer `3`; ein beendeter Lauf,
der eine Grenze verfehlte, ist `4`, und die Grenzen machen einen ungültigen oder gestoppten Lauf
nicht zu `4`.

| Code | Was geschah |
|---|---|
| `0` | Der Plan wurde erfüllt, der Bericht ist vollständig; mit `pass` — alle Grenzen eingehalten |
| `1` | Der verlangte Bericht liegt nicht vor: Der Lauf fand nicht statt (Flags, Konfiguration, Verbindung) oder der Bericht wurde nicht in `-json-file` geschrieben. Die Ursache steht in stderr |
| `2` | Der Lauf ist ungültig (siehe oben) oder eine `pass`-Grenze lässt sich nicht entscheiden. Ein Bericht ist da, aber die Zahlen handeln nicht von der Last oder beantworten die Frage der Grenze nicht |
| `3` | Der Lauf wurde vor dem Plan gestoppt. Ein Bericht ist da und deckt nur ab, was durchgelaufen ist |
| `4` | Ein beendeter Lauf verfehlte eine `pass`-Grenze. Ein Bericht ist da, `outcome` im JSON ist `complete` |
| `130` | Notausstieg ohne Bericht nach Strg+C |
| `143` | Notausstieg ohne Bericht nach SIGTERM |

Wie der Stopp funktioniert — in der [Referenz](../en/reference.md#stopping) (englisch).

## JSON für Skripte und CI

Mit `-output json` geht genau ein JSON-Objekt und ein Zeilenumbruch nach stdout; alles Übrige — nach
stderr, sodass man stdout gleich an `jq` weitergeben kann. Das Objekt wird nur für einen Lauf
gedruckt, der stattfand (Codes `0`, `2`, `3`, `4`); bei `1`, `130` und `143` ist stdout leer. Das
Feld `outcome` — `complete`, `invalid` oder `incomplete` — entspricht dem Exit-Code `0`, `2`, `3`;
Code `4` ist `complete`, und welche Grenze verfehlt wurde, sagt das Feld `thresholds`. Die einzige
Abweichung: Ließ sich der Bericht nicht in die Datei schreiben, ist der Code `1`, und in stdout
bleibt das Objekt des Laufs.

Text im Log und JSON für den nächsten Schritt aus einem Lauf — `-json-file`:

```sh
leettest -c load.yaml -json-file report.json
```

In stdout steht alles wie mit `-output` gesetzt (standardmäßig Text), in `report.json` — dasselbe
Objekt, das `-output json` druckt. Die Datei wird vor dem Start angelegt und nicht überschrieben:
Gibt es sie schon oder fehlt das Verzeichnis, ist der Code `1` und kein einziger Aufruf ans Ziel. Ein
Lauf ohne Bericht (zum Beispiel `1` bei der Verbindung oder `130`) hinterlässt keine Datei. Misslingt
das Schreiben nach dem Lauf, nennt stderr Pfad und Fehler, und der Code ist `1`.

Das Schema wird mit dem Feld `schema_version` versioniert, derzeit `1`, und das ist ein Vertrag; der
Text auf dem Bildschirm nicht — werten Sie das JSON aus, nicht den Bildschirm. Regeln:

- Ein neues Feld kommt ohne Versionswechsel dazu; Umbenennen, Entfernen oder ein Typwechsel eines
  Feldes erhöhen `schema_version`;
- die Menge der Werte von Aufzählungen (`outcome`, `unchecked[].reason`, Codes in `failure_codes`)
  kann ohne Versionswechsel wachsen;
- der Verbraucher muss unbekannte Felder überspringen und einen unbekannten Wert einer Aufzählung
  verarbeiten, ohne abzustürzen.

Die Einheit steht im Feldnamen: `_us` — ganze Mikrosekunden, `_s` — ganze Sekunden; die Frequenz
`rps` ist die einzige gebrochene Zahl. Ein Wert, den der Lauf nicht lieferte, ist `null`, nicht `0`.
Ein Perzentil ist ein Objekt `{"us": 1234, "ns": 1234567, "lower_bound": false}`: `ns` — der Wert des
Histogramms in ganzen Nanosekunden, `us` — derselbe, auf Mikrosekunden abgeschnitten; `pass:` urteilt
nach dem unteren Wert desselben Histogramm-Buckets (höchstens 0,1 % unter `ns`), `leettest compare` —
nach `ns`; in einem Bericht, der vor diesem Feld geschrieben wurde, gilt `us` × 1000. Bei
`lower_bound: true` ist es eine untere Grenze, kein Wert. Latenzen sind Werte des Histogramms mit
einer Genauigkeit von 3 signifikanten Stellen (Fehler bis 0,1 %), Zähler sind exakt. Die Zeit wird ab
`started_at` gerechnet (RFC 3339, UTC) — dem Beginn des Zeitplans, das Warm-up eingeschlossen;
`duration_us` schließt das Warm-up ebenfalls ein. `in_flight` in einer Sekunde — wie viele Aufrufe an
ihrem Ende unterwegs waren. Die Codes in `failure_codes` sind kanonische Namen (`UNAVAILABLE`).
Abgleich: Die Summen des Laufs sind die Summe der Summen der Methoden, und nach Sekunden einer
Methode sind `Σ begun` plus `outside_timeline` alle ihre Aufrufe, das Warm-up eingeschlossen.

Entscheidungen stehen in Feldern, nicht im Text: `invalid_reasons` (`in_flight_cap`,
`nothing_measured`, `clock_step`), `methods[].invalid_reason` (`request_error`, `client_error`,
`bad_response`, `mixed` oder `null`), `tail_wait_cause` (`generator`, `stream`, `connection` oder
`null`) und die Zahlen je Ursache in `client_waits`. `notes` und `unchecked[].error` sind Text für
Menschen, er ändert sich frei, werten Sie ihn nicht aus.

Stille des Ziels: `methods[].silent_from_s` — die Sekunde, ab der das Ziel auf keinen einzigen
gesendeten Aufruf mehr antwortete, `methods[].silent_sent_rps` — wie viele Aufrufe in der Sekunde
davor rausgingen, `methods[].silent_planned_rps_low` und `_high` — das Plantempo der Stufen in
derselben Sekunde. Ohne Stille sind alle `null`; das Plantempo ist auch `null`, wenn in jener Sekunde
keine Stufen mehr liefen. `planned_rps_low` und `_high` — das Tempo des gesamten Plans.

Der Textbericht wird nur in ASCII gedruckt. Zeichen außerhalb von ASCII — im Methodennamen, im
Fehlertext vom Ziel — werden als `\uXXXX` gedruckt (jenseits von U+FFFF als Surrogatpaar, wie im
JSON). `notes` im JSON tragen denselben escapten Text.

### Suche nach dem Bruchpunkt

Der Abschnitt `load.breakpoint` anstelle von `rps` und `duration` beim Aufruf: LeetTest erhöht die
Last in Stufen und nennt, auf welcher Stufe das Ziel hielt und auf welcher es zusammenbrach. Der
Aufruf ist genau einer, ohne `rps`, `duration` und `load.warmup`.

```yaml
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      timeout: 500ms
  breakpoint:
    from: 100         # erste Stufe, rps
    to: 2000          # höher gehen wir nicht
    factor: 1.25      # nächste Stufe = vorige × factor; oder step: 100 — so viele rps mehr
    settle: 5s        # Anfang der Stufe, der nicht ins Urteil geht; weniger als die Hälfte von hold
    hold: 30s         # Länge der Stufe
    p99_limit: 200ms  # optional; ohne ihn bricht die Stufe bei p99 über dem Dreifachen der besten gehaltenen
    errors_limit: 1%  # optional; die Stufe bricht, wenn mehr als dieser Anteil der Aufrufe scheiterte
```

Eine Stufe bricht, wenn mehr als `errors_limit` der Aufrufe scheiterten (standardmäßig 1 %; genau so
viel wird noch gehalten, `100%` — Fehler brechen nicht) oder p99 die Schwelle überschritt. Ein Aufruf,
der wegen der Verbindung nicht gesendet wurde, bricht die Stufe bei jedem `errors_limit`. Fehler der
Clientseite (`request_error`, `client_error`, `bad_response`) beweisen nicht, dass das Ziel
zusammenbrach: Ist das Limit nur mit ihnen überschritten, hält die Suche als `run_limit` mit der
Ursache `client_errors` an und erklärt keinen Bruchpunkt; `held_rps` bleibt. Eine gebrochene Stufe
wird nach einer Pause wiederholt und, außer der ersten, nach Proben auf der ersten Stufe: Das Ziel
muss zum früheren p99 zurückkehren. Eine Stufe gilt nur dann als gehalten, wenn der Generator das
Geplante gesendet hat — mindestens 99,9 % der Aufrufe des Messfensters. Sonst ist es die Grenze des
Laufs, nicht des Ziels. Vor der ersten Stufe steht in stderr die Zeile `breakpoint: up to N steps, at
most T`: wie viele Stufen und wie lange die Suche im schlimmsten Fall dauert. Die Verbindung ist eine
für die ganze Suche; reißt das Ziel sie ab, ist die Stufe gebrochen, ein Neuaufbau erfolgt nicht.

Die erste Stufe muss genug Aufrufe für p99 messen: `from × (hold − settle)`, der Nachkommaanteil wird
verworfen, mindestens 100. Eine Suche, deren erste Stufe weniger misst, wird vor jeder Last
abgelehnt: `invalid breakpoint: the first step measures 40 calls, a p99 needs 100: raise from, hold or
lower settle` — nach dem p99 der besten gehaltenen Stufe werden die nächsten Stufen und Proben
beurteilt. Eine Stufe, bei der p99 trotzdem nicht zustande kam (zu wenige Aufrufe mit gemessener
Latenz), setzt diesen Anker nicht; eine Probe ohne Anker gilt als erholt, und der Bericht sagt das in
einem Hinweis (`no baseline: the first step measured too few calls for a p99, the knee was not
checked`).

Das Ergebnis (`outcome` im JSON) ist eine geschlossene Liste: `broke` (hielt X, brach bei Y),
`broke_at_first` (brach schon auf der ersten Stufe — beginnen Sie tiefer), `held_all` (hielt alles bis
`to`), `run_limit` (der Generator, das Verbindungslimit oder `-max-in-flight` ging aus: über das Ziel
weiter oben ist nichts bekannt), `stopped` (Strg+C), `invalid` (die Stufe erwies sich als ungültiger
Lauf). Die Ursache (`why`) ist ebenfalls eine geschlossene Liste: `errors`, `p99_limit`,
`p99_vs_base`, `connection`, `no_recovery`, `generator`, `in_flight_cap`, `stream_limit`,
`stream_wait`, `client_errors`, `clock_step`, `request_errors` oder `null`. Das Fehlerlimit steht in
der Zeile `breakpoint.errors_limit`. `held_rps` und `broke_rps` sind `null`, wo es sie nicht gibt. Im
Suchmodus ist das JSON ein anderes Objekt: `mode: "breakpoint"`, die Felder eines gewöhnlichen Laufs
fehlen auf der obersten Ebene, jeder Lauf mit seinem vollständigen Bericht liegt in
`breakpoint.runs` (`kind`: `step`, `repeat`, `probe`; `planned_rps` und `sent_rps`). Ein gewöhnlicher
Lauf schreibt `mode: "run"`.

Exit-Codes: `0` für `broke`, `broke_at_first`, `held_all`, `run_limit` — das sind Funde; `2` für
`invalid`; `3` für einen Stopp mit einem Strg+C; `4` — die Suche hielt weniger als
`breakpoint.pass.held_rps`.

### Zwei Suchen vergleichen

Zwei Berichte der Suche, mit `-json-file` geschrieben — vor der Änderung und nach ihr. `leettest
compare` sagt, um wie viel die Kapazität (`held_rps`) fiel, und urteilt mit `-max-capacity-drop`
über sie. Am Teststand aus dem Repository-Wurzelverzeichnis: eine Suche bei einer Kapazität von 270
rps (im Terminal des Stands), dann in einem anderen Terminal — die Suche und der Bericht „vorher“:

```sh
go run ./test/stand/cmd/stand -capacity 270 -delay 20ms
```

```sh
leettest -c examples/breakpoint-step.yaml -json-file before.json
```

Stoppen Sie den Stand (Strg+C), starten Sie ihn mit einer Kapazität von 200 rps und nehmen Sie den
Bericht „nachher“ auf:

```sh
go run ./test/stand/cmd/stand -capacity 200 -delay 20ms
```

```sh
leettest -c examples/breakpoint-step.yaml -json-file after.json
```

Vergleichen Sie (der Stand kann gestoppt werden):

```sh
leettest compare -max-capacity-drop 10% before.json after.json
```

```
base: before.json  Health  2026-10-19T19:25:28.941479Z
new:  after.json  Health  2026-10-19T19:26:46.1806268Z
capacity: 250 rps -> 200 rps, drop 20.0%, limit 10%: FAIL
```

Die Kapazität fiel von 250 auf 200 rps, das sind 20 % bei einer Grenze von 10 %, Exit-Code `4`. Zeit
und Stand sind bei Ihnen eigene, die Zahlen — nach der Kapazität des Stands.

Die Berichte sind nur vergleichbar, wenn Ziel, Methode, LeetTest-Version, Laufpass (außer den
Labels) und Datensatz übereinstimmen; sonst nennen die Zeilen `differs:` das Feld, und das Urteil
ist `unknown`. Endete die Suche vor dem Zusammenbruch, ist die Kapazität eine untere Grenze
(`>= N rps`); brach das Ziel schon auf der ersten Stufe zusammen — eine obere (`below N rps`). Dann
wird der Abfall als Schranke gedruckt (`drop <= 33.4%`), und das Limit wird nur entschieden, wenn
diese Schranke es entscheidet. Ohne `-max-capacity-drop` wird nichts beurteilt. Exit-Codes: `0` — in der
Grenze oder keine Grenze, `1` — der Bericht wurde nicht gelesen, `2` — die Berichte sind nicht
vergleichbar oder die Grenze lässt sich nicht entscheiden, `4` — die Kapazität fiel stärker als die
Grenze.

### Zwei Läufe vergleichen

Dieselben Befehle lesen auch die Berichte eines gewöhnlichen Laufs (`-json-file`): `-max-p99-rise`
beurteilt den Anstieg von p99 jeder Methode in Prozent des p99 „vorher“, `-max-errors-rise` — den
Anstieg des Anteils fehlgeschlagener Aufrufe in Prozentpunkten. Ohne Grenze wird die Zeile gedruckt,
aber nicht beurteilt. Ein Bericht der Suche und ein Bericht eines Laufs in einem Befehl — Code `1`.
Am Teststand aus dem Repository-Wurzelverzeichnis: ein Lauf bei einer Verzögerung von 20 ms, dann bei
30 ms:

```sh
go run ./test/stand/cmd/stand -delay 20ms -life 60s
```

```sh
leettest -c examples/stand.yaml -json-file before.json
```

Der Stand beendet sich nach einer Minute selbst; starten Sie ihn mit einer Verzögerung von 30 ms und
nehmen Sie den Bericht „nachher“ auf:

```sh
go run ./test/stand/cmd/stand -delay 30ms -life 60s
```

```sh
leettest -c examples/stand.yaml -json-file after.json
```

Vergleichen Sie (der Stand kann gestoppt werden):

```sh
leettest compare -max-p99-rise 20% before.json after.json
```

```
base: before.json  Health  2026-10-20T12:40:09.5222467Z
new:  after.json  Health  2026-10-20T12:40:42.469917Z
grpc.health.v1.Health/Check: p99 21.2ms -> 31.3ms, rise 47.3%, limit 20%: FAIL
grpc.health.v1.Health/Check: errors 0.0% -> 0.0%, no change
```

p99 stieg von 21.2 auf 31.3 ms, das sind 47.3 % bei einer Grenze von 20 %, Exit-Code `4`. Zeit und
Verzögerungen sind bei Ihnen eigene.

Die Prüfung kann `unknown` sein (Code `2`), wenn die Berichte nicht vergleichbar sind (wie oben),
der Lauf ungültig oder unvollständig ist, zu wenige Messungen vorliegen, p99 am Timeout hängt (dann
ist er größer als gedruckt, `>=`) oder Fehler des Clients selbst nicht entscheiden lassen, ob der
Dienst schuld ist. Methoden, die nicht in beiden Berichten stehen, werden nicht verglichen. Die
Exit-Codes sind dieselben wie bei den Suchen.

Für Skript und CI dasselbe Ergebnis als ein JSON-Objekt: `-output json` druckt nur es nach stdout,
Exit-Codes und stderr sind dieselben wie beim Text; bei Code `1` ist stdout leer.

```sh
leettest compare -max-p99-rise 20% -output json before.json after.json
```

Im Objekt ist `verdict` das schlechteste Urteil der Prüfungen mit Grenze (`null`, wenn es keine
Grenzen gibt), und in `checks` steht jede Prüfung mit den Zahlen beider Berichte; bei `unknown` steht
die Ursache in `reason` — ein Wert aus einer geschlossenen Liste:

```json
{"schema_version":1,"mode":"compare","compared_mode":"run","comparable":true,"differences":[],"verdict":"fail","checks":[{"method":"/grpc.health.v1.Health/Check","key":"p99","base_p99":{"us":21381,"ns":21381472,"lower_bound":false},"new_p99":{"us":31358,"ns":31358109,"lower_bound":false},"limit_percent":"20%","verdict":"fail","reason":null,"why":null}]}
```

Nicht alle Felder sind gezeigt: Die leeren dieser Prüfung (`base_capacity` und andere `null`) und die
Beschreibung der Berichte (`base`, `new`) sind weggelassen. Einen Prozentwert des Anstiegs gibt es im
Objekt nicht — das Skript rechnet ihn aus `us`. Jedes Feld ist in der
[Referenz, „Compare JSON“](../en/reference.md#compare-json) (englisch) beschrieben.

## Weiter

[Referenz](../en/reference.md) (englisch) — jedes Feld der Konfiguration und jedes Flag. [Prometheus-Metriken](../en/reference.md#prometheus-metrics)
(englisch) — der Lauf liefert `/metrics`, solange die Last läuft; die Adresse ist standardmäßig nicht
gesetzt, und `linger` von 15 s verzögert den Ausstieg: Lesen Sie die Warnungen im Abschnitt. [Fahrplan](../en/roadmap.md)
(englisch). Fehlt Ihnen eine Funktion — [schreiben Sie ein Issue](https://github.com/yhgrwav/leettest/issues/new/choose).
