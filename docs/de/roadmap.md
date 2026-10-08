# Was als Nächstes kommt

[← Zur Übersicht](README.md)

> Übersetzt aus [docs/ru/roadmap.md](../ru/roadmap.md) bei 67f8732, 2026-10-08. Bei Abweichungen gilt die
> russische Fassung.

> **Fehlt Ihnen etwas? [Eröffnen Sie ein Issue](https://github.com/yhgrwav/leettest/issues/new/choose)** —
> beschreiben Sie die Aufgabe, die Sie lösen. Fragen ohne konkrete Aufgabe gehören in die
> [Discussions](https://github.com/yhgrwav/leettest/discussions). Die Reihenfolge unten richtet sich
> danach, was Nutzer brauchen.

Das sind Pläne, keine zugesagten Termine. Was heute funktioniert, steht im [README](README.md).

## Nächste Version (v0.3)

- **`.proto` und Protoset** für Dienste ohne Server Reflection.

## Später

- **Hochfahren** von null auf die Ziel-RPS.
- **Pass/Fail-Schwellen für CI**: Der Lauf schlägt fehl, wenn p99 oder der Fehleranteil über einer
  Grenze liegt.
- **[Verkettung von Aufrufen](chaining.md)**: ein Feld aus der Antwort einer Methode in die Anfrage
  einer anderen.
- **Token-Erneuerung** während eines langen Laufs.
- **Metrik-Export** nach Prometheus.
