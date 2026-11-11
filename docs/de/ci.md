# LeetTest in CI

[← Zur Dokumentation](README.md)

> Übersetzt aus [docs/ru/ci.md](../ru/ci.md) bei dc7fd95, 2026-11-10. Bei Abweichungen gilt die
> russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/ci.md).

Diese Seite zeigt, wie man bei jeder Änderung an einem Dienst einen Lasttest startet und den Build
scheitern lässt, wenn der Dienst die gesetzten Grenzen nicht mehr hält. Das Beispiel ist ein
GitHub-Actions-Workflow; jedes CI, das Shell-Befehle ausführt, tut dieselben vier Dinge:

1. LeetTest installieren.
2. Den zu prüfenden Dienst starten.
3. Die Last starten. Der Exit-Code ist das Urteil.
4. Den JSON-Bericht aufbewahren.

## Die Konfiguration

`load.yaml` im Repository des Dienstes. Die Grenzen in `pass` machen den Exit-Code zum Urteil:
Ohne sie gibt ein Lauf, der bis zum Ende kommt, `0` zurück, egal wie der Dienst antwortet
([`pass`-Grenzen](../en/report.md#pass-limits-a-run-as-a-check), englisch). `app.target` ist der
Dienst, den der Job gestartet hat; in dieser Datei ist es der Referenzstand aus unserem
Repository, das Ziel unseres eigenen CI.

```yaml
# Ein Lasttest für CI. app.target ist der Dienst, den der Job gestartet hat; hier der
# Referenzstand, aus dem Wurzelverzeichnis des Repositorys:
#   go run ./test/stand/cmd/stand -delay 20ms
#   leettest -c examples/ci.yaml
# Die pass-Grenzen entscheiden den Exit-Code: Auf dem Stand halten beide, also ist er 0;
# eine verfehlte Grenze ergibt 4.

app:
  target:
    ip: 127.0.0.1
    port: 50051
  tls: false

load:
  warmup: 2s
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 100
      duration: 10s
      pass:
        p99: 200ms
        errors: 1%
```

## Der Workflow

`.github/workflows/load-test.yml`:

```yaml
name: Load test

on:
  pull_request:

permissions:
  contents: read

jobs:
  load:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - name: Install LeetTest
        run: |
          curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
      - name: Start the service under test
        run: docker compose up -d --wait
      # example workflow: start
      - name: Run the load
        run: |
          rc=0
          leettest -c load.yaml -json-file report.json || rc=$?
          case "$rc" in
            0) ;;
            2) echo "::error::invalid run: the numbers are not about the service's load"; exit 1 ;;
            3) echo "::error::the run stopped before its plan"; exit 1 ;;
            4) echo "::error::a pass limit was missed"; exit 1 ;;
            *) echo "::error::leettest exited with $rc and no report"; exit 1 ;;
          esac
      - name: Read the report
        if: always() && hashFiles('report.json') != ''
        run: |
          echo "p99: $(jq '.methods[0].latency.p99.us' report.json) us" >> "$GITHUB_STEP_SUMMARY"
          jq -e '.thresholds != null and all(.thresholds[]; .verdict == "pass")' report.json
      - uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        if: always()
        with:
          name: leettest-report
          path: report.json
          if-no-files-found: ignore
      # example workflow: end
```

Die Namen der Schritte und die Fehlertexte im YAML sind englisch: Dieser Text stimmt Wort für Wort
mit der englischen Seite und mit der Aufgabe unseres CI überein.

Schritt für Schritt:

- **Installation.** `install.sh` legt das neueste Release in `~/.local/bin` ab und prüft dessen
  Prüfsumme ([Installation](../en/guide/first-run.md#install), englisch). Ein Build, der sich nicht
  unter den Füßen ändern soll, legt ein Release fest: `LEETTEST_VERSION=<Tag>` vor `sh`, den Tag
  nimmt man von der [Release-Seite](https://github.com/yhgrwav/leettest/releases).
- **Den Dienst starten.** Ersetzen Sie den Schritt durch das, womit Ihr Dienst startet, und richten
  Sie `app.target` in `load.yaml` darauf. Die Last beginnt, sobald LeetTest startet: Warten Sie bis
  zum Ende dieses Schritts, bis der Dienst antwortet (bei Docker Compose tut das `--wait`).
- **Die Last.** `-json-file report.json` schreibt den JSON-Bericht in eine Datei, der Textbericht
  geht ins Log des Jobs. Das `case` macht aus jedem Exit-Code eine Fehlerzeile; jeder Code
  ungleich null lässt den Build scheitern. Die Datei wird vor dem Start angelegt und nie
  überschrieben, deshalb braucht sie einen sauberen Runner.
- **Den Bericht lesen.** `jq` holt ein Feld aus dem JSON: hier das p99 der ersten Methode, in die
  Zusammenfassung des Jobs. Die zweite Zeile prüft, dass `pass`-Grenzen gesetzt sind und jede
  eingehalten ist: Der Exit-Code `0` einer Konfiguration ohne Grenzen sagt nichts über den Dienst.
  Der Schritt läuft auch nach einer gescheiterten Last, wenn die Datei da ist.
- **Den Bericht aufbewahren.** `if: always()` lädt ihn auch für einen gescheiterten Lauf hoch. Ein
  Lauf, der nicht gestartet ist (Exit-Code `1`), hinterlässt keine Datei, daher
  `if-no-files-found: ignore`.

Die zwei Kommentarzeilen `# example workflow` markieren den Teil, den unser eigenes CI so
ausführt, wie er geschrieben steht.

## Exit-Codes im Build

| Code | Was geschah | Build |
|---|---|---|
| `0` | Der Plan ist durchgelaufen, der Bericht vollständig; mit `pass` — alle Grenzen eingehalten | läuft durch |
| `1` | Der gewünschte Bericht liegt nicht vor: Der Lauf fand nicht statt oder der Bericht wurde nicht geschrieben | scheitert; der Grund steht auf stderr |
| `2` | Der Lauf ist ungültig oder eine `pass`-Grenze lässt sich nicht entscheiden | scheitert; das ist kein Bestehen |
| `3` | Der Lauf wurde vor dem Plan gestoppt | scheitert; der Bericht handelt nur von dem, was lief |
| `4` | Ein beendeter Lauf hat eine `pass`-Grenze verfehlt | scheitert; welche, steht in `thresholds` im JSON |
| `130` | Abbruch ohne Bericht nach Strg+C | scheitert; es gibt keinen Bericht |
| `143` | Abbruch ohne Bericht nach SIGTERM | scheitert; es gibt keinen Bericht |

`2` ist weder Bestehen noch Verfehlen: Die Zahlen eines solchen Berichts handeln nicht von der Last
auf dem Dienst, deshalb können sie nicht zeigen, dass eine Grenze eingehalten ist. Der Bericht nennt
den Grund (`invalid_reasons` im JSON): Dem Generator fehlte Prozessor, die Uhr des Hosts ist zu
grob, eine Grenze lässt sich nicht entscheiden. Repariert werden muss die Umgebung; eine
gelockerte Grenze hilft nicht. Die Codes ändern sich innerhalb von 1.x nicht:
[was gleich bleibt](stability.md#exit-codes).

## Das JSON lesen

Der Text des Berichts ist für Menschen, JSON und Exit-Code sind für Skripte. Ein Skript darf sich
auf die Felder und Werte der Seite [was gleich bleibt](stability.md) verlassen; alle Felder stehen
in der [Referenz](../en/reference.md#json-fields) (englisch). Einige Felder, die ein Build
gewöhnlich liest:

| Feld | Was darin steht |
|---|---|
| `outcome` | `complete`, `invalid` oder `incomplete`; dieselbe Tatsache wie die Exit-Codes `0`/`4`, `2`, `3` |
| `thresholds[].verdict` | `pass`, `fail` oder `unknown` für jede `pass`-Grenze |
| `invalid_reasons` | warum der Lauf ungültig ist; bei einem gültigen leer |
| `methods[].latency.p99.us` | p99 der Methode in ganzen Mikrosekunden |

Ein Skript überspringt Felder, die es nicht kennt, und stürzt bei einem neuen Wert einer Aufzählung
nicht ab.

## Wo der Runner zählt

Die Last geht vom Runner aus, deshalb steckt alles zwischen Runner und Dienst in der Latenz, und
ein Runner, dem Prozessor fehlt, hält den Zeitplan nicht: Dann sagt der Bericht `generator_late`
und der Exit-Code ist `2`. Starten Sie den Job neben dem Dienst, im selben Netz oder im selben Job,
und halten Sie die Rate innerhalb dessen, was der Runner sendet. Mehr — [wo man startet](../en/reference.md#where-to-run)
(englisch) und [was die Zahlen nicht abdecken](limitations.md).

## Das Beispiel wird geprüft

Der Teil des Workflows zwischen den zwei Markierungskommentaren ist der Job `example workflow` aus
[`.github/workflows/ci.yml`](../../.github/workflows/ci.yml), Wort für Wort;
[`examples/ci_page_test.go`](../../examples/ci_page_test.go) schlägt fehl, wenn sie auseinanderlaufen.
Der Job läuft bei jedem Pull Request, gegen den Referenzstand und den Build dieses Commits. Vom
Workflow oben unterscheidet er sich nur in den Schritten vor dem markierten Teil: Die Seite
installiert ein Release und startet Ihren Dienst, der Job baut den Commit und startet den Stand.
