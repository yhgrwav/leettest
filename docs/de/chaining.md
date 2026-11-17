# Was ist Call-Verkettung und wozu?

[← Zur Übersicht](README.md)

> Übersetzt aus [docs/ru/chaining.md](../ru/chaining.md) bei 46c2f53, 2026-11-17. Bei Abweichungen gilt die
> russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/chaining.md).

> **Status: funktioniert ab v0.9 (`scenario`, `steps` und `extract`, siehe
> [Referenz](reference.md#szenario-schritte-nach-dem-aufruf)).** Die Pools, die hier früher
> beschrieben waren, sind dadurch ersetzt: Schritte nach dem Aufruf plus ein Antwortfeld im nächsten
> Schritt.

Die Hälfte der Methoden eines echten Dienstes lässt sich nicht mit statischen Daten aufrufen. Um
Überweisungen zu belasten, braucht man existierende Wallets: `Transfer` verlangt `from` und `to`,
und die gibt es nur aus Antworten von `CreateWallet`.

Der übliche Ausweg ist, Testdaten vorab vorzubereiten. Wo sich Daten vorbereiten lassen, ist das
der richtige Weg, und dafür gibt es die Einspeisung aus einer Datei: einen
[Datensatz](reference.md#bei-jedem-aufruf-eine-andere-anfrage-dataset) als CSV oder JSON, eine Zeile
pro Anfrage. Doch nicht alles lässt sich vorbereiten: Einmal-Tokens,
Sitzungen, Last auf den Erstellungsvorgang selbst. Und tausend Überweisungen zwischen denselben
zwei Wallets sind keine Last auf Überweisungen, sondern auf eine Datenbankzeile und ihre Sperre.

Der zweite Ausweg ist ein Skript, das ein Wallet erstellt und gleich davon überweist. Dann misst
man die Folge „Erstellen plus Überweisen“ und nicht eine einzelne Überweisung; den Beitrag jedes
Schritts zur Latenz bekommt man aus der Gesamtzahl nicht heraus, wenn das Skript die Schritte nicht
getrennt misst.

Wir lösen das mit einem Szenario. Ein Aufruf und die Schritte nach ihm sind eine Iteration: Sie läuft
der Reihe nach, und jeder Schritt bleibt eine eigene Zeile im Bericht mit eigenen Perzentilen. Ein
Aufruf oder Schritt benennt Felder seiner Antwort (`extract`), und der nächste Schritt setzt sie als
`{{extract.<name>}}` in seine Anfrage ein. Die Werte leben in einer Iteration: Jede Iteration hat ihr
eigenes Wallet.

```yaml
load:
  calls:
    - method: wallet.v1.WalletService/CreateWallet
      rps: 5
      duration: 1m
      templates: true
      scenario: create-and-pay
      extract:
        wallet: wallet_id
      steps:
        - method: wallet.v1.WalletService/Transfer
          templates: true
          data:
            from: "{{extract.wallet}}"
            to: "{{uuid}}"
            amount: 100
```

Fremde `.proto` muss man dafür nicht anfassen: Der Pfad zum Feld wird beim Start anhand der
Reflection-Daten (oder aus `app.proto` / `app.protoset`) geprüft — ein fehlendes Feld ist ein klarer
Fehler mit dem Pfad vor dem Lauf, keine Müllantworten mitten in der Last. Gegen das Schema wird auch
geprüft, dass der Wert zum Feld des nächsten Schritts passt: Eine Zahl von `int64` nach `int32` geht
nicht durch, und nichts wird stillschweigend abgeschnitten.

Was geschieht, wenn die Antwort den nötigen Wert nicht enthält. Stillschweigend einen leeren zu
senden ist die schlechteste Variante: Der Lauf entspräche nicht mehr dem gesetzten Plan, und das wäre
nicht zu sehen. Darum ist so eine Antwort eine `bad response`: Die Antwort kam, aber der Client kann
sie nicht verwenden. Die Iteration endet damit, der Schritt gilt als „hier gescheitert“, die folgenden
Schritte sind nicht erreicht, und der Code bleibt das `OK`, das das Ziel geschickt hat. Antwortet so
jeder Aufruf einer Methode, ist der Lauf ungültig, und der Bericht sagt, dass die Werte in den
Antworten fehlen.

Das ist nicht dasselbe wie ein gemeinsamer Pool von Werten zwischen verschiedenen Aufrufen: Die Werte
gehen nicht von einer Iteration in die andere über und sammeln sich nicht an. Für gemeinsame,
vorbereitete Daten gibt es den Datensatz.

Einzelheiten — Feldtypen, Text des Werts, Regeln der Ersetzung — stehen in der
[Referenz](reference.md#extract-ein-wert-aus-einer-früheren-antwort). Ein fertiges Beispiel für den
Referenz-Stand ist [`examples/ru/scenario-extract.yaml`](../../examples/ru/scenario-extract.yaml) (die
Kommentare sind russisch; die englische Fassung ist
[`examples/scenario-extract.yaml`](../../examples/scenario-extract.yaml)).
