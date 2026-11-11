# Last

[← Zur Dokumentation](../README.md)

> Übersetzt aus [docs/ru/guide/load.md](../../ru/guide/load.md) bei feb2d1d, 2026-11-10. Bei Abweichungen gilt die
> russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../../en/guide/load.md).

Welche Methoden mit welcher Rate und wie lange belastet werden. Das Tempo wird in Anfragen pro
Sekunde angegeben: Eine Anfrage geht nach Plan raus, ohne auf die Antworten der vorigen zu warten,
und die Latenz wird vom geplanten Zeitpunkt an gerechnet.

## Die minimale Konfiguration

```yaml
app:
  target:
    ip: localhost
    port: 50051
  tls: false

load:
  warmup: 5s
  ramp: 30s
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      timeout: 500ms
      data:
        wallet_id: "w-123"

    - method: wallet.v1.WalletService/Transfer
      rps: 50
      duration: 1m
```

```sh
leettest -c leettest.yaml
```

- `method` — der volle Name: `paket.Dienst/Methode`. Eine Methode — ein Eintrag in `calls`; mehrere
  Methoden mit unterschiedlichem Tempo — mehrere Einträge.
- `rps` — eine ganze Zahl von Anfragen pro Sekunde. `10.5` wird abgelehnt, nicht stillschweigend
  gerundet.
- `duration` — wie lange die Methode belastet wird: `30s`, `5m`, `1h`.
- `timeout` — wie lange auf die Antwort gewartet wird. Nicht angegeben — `2s`. Null schaltet das
  Timeout nicht ab, sondern ergibt einen Fehler.
- `warmup` — die ersten Sekunden gehen an den Dienst, zählen aber nicht in die Statistik, weil kalte
  Caches sie verderben. Das Warm-up ist Teil von `duration` und muss kürzer sein als jeder Aufruf.
  Im Bericht steht es in einer eigenen Zeile `warm-up N sent`.
- `ramp` — der Anlauf: Jede Methode steigt in dieser Zeit linear von null auf ihr `rps`, danach
  beginnt ihre `duration`. Der Lauf dauert `ramp` + `duration` (`warmup`, falls gesetzt, ist Teil von
  `duration`). Die Anfragen des Anlaufs zählen nicht in die Statistik; die `pass`-Grenzen und der
  Fehleranteil werden nur über die Haltephase gerechnet. Beispiel —
  [`examples/ramp.yaml`](../../../examples/ramp.yaml).
- `data` — der Body der Anfrage als gewöhnliches YAML in Form der Nachricht. Ohne `data` geht eine
  leere Nachricht raus.

Alle Anfragen einer Methode gehen mit demselben Body raus. Bei einem Eintrag mit Idempotenzschlüssel
heißt das: Nur die erste Anfrage legt den Eintrag an, die übrigen gehen den Weg der Wiederholung —
und gemessen wird er. Eine andere Anfrage bei jedem Aufruf — eine Datei `dataset` (ein JSON je
Zeile) oder `templates: true` mit `{{uuid}}` und `{{seq}}`; siehe
[Referenz](../../en/reference.md#request-body) (englisch).

## Was im Bericht erscheint

- Eine Zeile je Methode: `sent`, `failed`, `sent/s` und Perzentile. Darunter `plan:` — Tempo und
  Dauer, die Sie gesetzt haben.
- `sent/s` — gesendete Anfragen, geteilt durch die Sendezeit: Ein Hängen des Dienstes am Ende des
  Laufs drückt diese Zahl nicht.
- Gingen weniger Anfragen raus als in `plan:`, sagt der Bericht, warum: `not sent` (Anfragen, die
  nach Plan hätten rausgehen sollen, aber nicht rausgingen) mit der Ursache.
- Der Block `over time` unter der Tabelle der Methode zeigt, wie sich Tempo, Fehleranteil, p50 und
  p99 nach Zeitfenstern änderten. Mit `ramp` sind die ersten Zeilen der Anlauf: Ein niedriges
  `sent/s` ist dort geplant, nicht kaputt. Achten Sie auf die Zeile, in der p99 oder der
  Fehleranteil wachsen, bevor das Tempo das Plateau erreicht hat.
- Hing der Dienst, nennt die Zeile unter der Tabelle die Sekunde, nach der er auf keine einzige
  Anfrage mehr antwortete.

Wie man jeden Teil liest — [Den Bericht lesen](../../en/report.md#reading-the-report) (englisch).

## Vor dem Start

LeetTest prüft die Konfiguration vor der ersten Anfrage: ein unbekanntes Feld, ein falscher Wert,
eine Methode, die der Dienst nicht hat, ein Fehler im Body der Anfrage — Exit-Code 1 und keine
einzige Anfrage an den Dienst. Geprüft wird auch die Obergrenze der Anfragen ohne Antwort
(in-flight: gesendet, Antwort steht aus): Ein Dienst, der hängt, sammelt `rps × timeout` solcher
Anfragen an, und die Summe über die Methoden darf `-max-in-flight` nicht überschreiten (Standard
5000). Der Fehler nennt beide Auswege: welches Timeout passt und welche Obergrenze nötig ist.

## Häufige Fehler

- **`warmup` ist länger als `duration` oder gleich lang.** Vom Aufruf bliebe keine einzige gemessene
  Anfrage: Konfigurationsfehler.
- **Gewünscht ist ein „Anstieg“ der Last, gesetzt ist aber gleich `rps`.** Ohne `ramp` startet die
  Methode mit vollem Tempo. Für einen Anstieg setzen Sie `ramp`; um zu erfahren, bei welcher Last der
  Dienst nicht mehr mithält — [Suche nach dem Bruchpunkt](../../en/report.md#breaking-point-search)
  (englisch).
- **Das Timeout ist zu groß für die Obergrenze.** Fehler vor dem Start, siehe oben.
- **Das Timeout ist zu kurz.** Anfragen, die nicht rechtzeitig antworten, brechen per Timeout ab und
  erscheinen als Fehlschläge des Dienstes; ihr Perzentil wird als `>500ms` gedruckt — „nicht weniger
  als 500 ms“.
- **Jede Anfrage ist gleich, gebraucht werden verschiedene.** Siehe oben zu `dataset` und
  `templates`.
- **Start über ssh ohne Terminal.** Fügen Sie `-plain` hinzu: eine Fortschrittszeile pro Sekunde
  statt des Live-Bildschirms.

## Weiter

- [Verbindung](connection.md) — TLS, Header, Token.
- [Referenz](../../en/reference.md) (englisch) — jedes Feld und jedes Flag.
