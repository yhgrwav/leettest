# Was sich innerhalb der Version 1.x nicht ändert

[← Zur Dokumentation](README.md)

> Übersetzt aus [docs/ru/stability.md](../ru/stability.md) bei ecdd128, 2026-11-10. Bei Abweichungen gilt die
> russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/stability.md).

**Gilt ab v1.0.0; davor siehe den Abschnitt „Upgrading“ in den [Release Notes](https://github.com/yhgrwav/leettest/releases).** Vor 1.0 kann eine Minor-Version (`0.7`, `0.8`) brechen, was vorher
funktionierte, und jeder solche Bruch ist unter „Upgrading“ benannt.

Die Version wird als `major.minor.patch` geschrieben. Ab v1.0.0 laufen ein Skript, eine Konfiguration
oder ein Dashboard, die auf `1.0` laufen, auf jeder `1.y` weiter. Was genau dazugehört, steht unten:
Die Werte sind dem Code und der Referenz entnommen, so wie sie jetzt sind.

Unten stehen zwei Teile: was sich nur in einer neuen Major-Version (`2.0`) ändert und was in einer
Minor-Version geändert werden darf. Der Text des Berichts für Menschen gehört nicht zum Versprechen:
Skripte lesen das JSON und den Exit-Code.

## Ändert sich nicht bis `2.0`

### Exit-Codes

| Code | Was geschah |
|---|---|
| `0` | Der Plan ist durchgelaufen, der Bericht vollständig; mit `pass` — alle Grenzen eingehalten |
| `1` | Der gewünschte Bericht liegt nicht vor: Der Lauf fand nicht statt oder der Bericht wurde nicht geschrieben |
| `2` | Der Lauf ist ungültig oder eine `pass`-Grenze lässt sich nicht entscheiden |
| `3` | Der Lauf wurde vor dem Plan gestoppt |
| `4` | Ein beendeter Lauf hat eine `pass`-Grenze verfehlt |
| `130` | Abbruch ohne Bericht nach Strg+C |
| `143` | Abbruch ohne Bericht nach SIGTERM |

Die vollständige Beschreibung — [Exit-Codes](../en/report.md#exit-codes) (englisch).

### Bericht als JSON: `-output json`, `schema_version` 1

Jedes vorhandene Feld behält Namen, Typ und Bedeutung, und `schema_version` wächst nur mit der
Major-Version. Felder der obersten Ebene:

- Lauf (`mode: "run"`): `schema_version`, `mode`, `leettest_version`, `target`, `name`,
  `methods_source`, `outcome`, `invalid_reasons`, `tail_wait_cause`,
  `generator_limit`, `stop_reason`, `stop`, `seq_overflow`, `clock_step_ns`, `started_at`, `duration_us`, `planned_us`, `warmup_us`,
  `settings`, `passport`, `sent`, `failed`, `aborted`, `not_sent`, `not_sent_generator`,
  `not_sent_stream`, `not_sent_connection`, `warmup_sent`, `warmup_failed`, `warmup_not_sent`,
  `cap_hit`, `start_lag`, `schedule`, `connections`, `client_waits`, `generator_cpu`, `traffic`,
  `methods`, `unchecked`, `thresholds`, `notes`.
- Suche nach dem Bruchpunkt (`mode: "breakpoint"`): `schema_version`, `mode`, `leettest_version`,
  `target`, `name`, `methods_source`, `method`, `started_at`, `settings`, `passport`, `breakpoint`,
  `thresholds`.

Verschachtelte Felder, ihre Typen und ihre Bedeutung stehen in den [JSON-Feldern](../en/reference.md#json-fields)
(englisch). Die Regeln des Schemas (Einheiten, `null` statt `0`, was mit unbekannten Feldern zu tun
ist) stehen in [JSON für Skripte und CI](../en/report.md#json-for-scripts-and-ci) (englisch). `notes`
und `unchecked[].error` sind Text für Menschen, ihre Formulierungen ändern sich frei, man muss sie
nicht auswerten. Die Werte der Aufzählungsfelder (`outcome`, `invalid_reasons`, `stop_reason`,
`unchecked[].reason`, die Codes in `failure_codes`) können wachsen: Ein Skript darf bei einem
unbekannten Wert nicht abstürzen.

### 11 Namen der Aufruf-Kategorien

`success`, `request_error`, `overload`, `failure`, `timed_out`, `cut_off`, `unreachable`,
`client_error`, `bad_response`, `aborted`, `unclassified`. Sie sind auch die Feldnamen im JSON und
die Werte des Labels `category` in Prometheus. Was jede bedeutet, steht in
[Den Bericht lesen](../en/report.md#reading-the-report) (englisch).

### Konfigurationsschlüssel und Flags

Jeder Konfigurationsschlüssel und jedes Flag, die es in 1.0 gibt: Name, Bedeutung und zulässige
Werte. Die vollständige Liste — [Konfiguration](../en/reference.md#config), [Flags](../en/reference.md#flags) und
[Berichte vergleichen](../en/reference.md#comparing-reports-leettest-compare) mit ihren eigenen Flags
(alle englisch).

### Prometheus-Metriken

Die Namen der Serien und der Labels und ihre Bedeutung. Derzeit gibt es diese Serien:

`leettest_planned_rps`, `leettest_calls_total`, `leettest_latency_seconds`,
`leettest_censored_total`, `leettest_excluded_total`, `leettest_not_sent_total`,
`leettest_in_flight`, `leettest_start_lag_seconds`, `leettest_warmup_sent_total`,
`leettest_warmup`.

Labels: `method`, `category`, `cause`, `le`. Histogramme haben die üblichen `_bucket`, `_sum`,
`_count`. Welche Serie was zählt, welche Labels und welchen Typ sie hat, steht in den
[Prometheus-Metriken](../en/reference.md#prometheus-metrics) (englisch).

## In einer Minor-Version änderbar

- JSON-Felder, Flags, Konfigurationsschlüssel, Serien und Labels hinzufügen.
- Den Text des Berichts für Menschen ändern.
- Den Teststand und die Werkzeuge für CI ändern.
- Die Prüfung der Konfiguration verschärfen: Abgelehnt wird nur, was auch vorher falsch oder
  undefiniert war.
- Eine falsche Zahl korrigieren: Eine Zahl, die log, ist ein Fehler, keine Änderung des Versprechens.

## Veralten

Ein veraltetes Flag, ein veralteter Schlüssel oder ein veraltetes Feld werden unter „Upgrading“ der
Release Notes angekündigt, geben bei Benutzung eine Warnung auf stderr aus, funktionieren bis zum
Ende der Major-Version weiter und entfallen in der nächsten. Derzeit ist nichts als veraltet
angekündigt.

## Wohin als Nächstes

- [Referenz](../en/reference.md) (englisch) — jeder Schlüssel, jedes Flag, jede Serie und jedes
  JSON-Feld.
- [Bericht, Exit-Codes, JSON](../en/report.md) (englisch) — wie man den Bericht liest und was man
  darin per Skript auswerten kann.
- [Was LeetTest nicht tut](limitations.md) — die Grenzen, an denen die Zahlen des Berichts nicht
  mehr vom Ziel handeln.
