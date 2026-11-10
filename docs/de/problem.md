# Welches Problem löst LeetTest?

[← Zur Übersicht](README.md)

> Übersetzt aus [docs/ru/problem.md](../ru/problem.md) bei feb2d1d, 2026-11-10. Bei Abweichungen gilt die
> russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/problem.md).

Ein Lasttest soll eine Frage beantworten: **ab welcher Last hält der Dienst nicht mehr mit, und wo
liegt der Engpass.** Mit dieser Antwort geht man zu den Entwicklern — reparieren, optimieren,
entscheiden, ob das Release rausgeht.

Damit die Antwort stimmt, braucht es zwei Dinge.

**Last aus mehreren Methoden.** Ein Dienst im Betrieb bekommt Aufrufe verschiedener Methoden
gleichzeitig: zum Beispiel viele Saldoabfragen, weniger Überweisungen, ganz wenige Registrierungen.
Die Methoden können sich Verbindungspool, Datenbanksperren und Cache teilen, daher kann die Last
jeder Methode für sich ein anderes Ergebnis liefern als die Mischung (das Beispiel ist erfunden, keine
Messung). Ein Lauf von `ghz` belastet eine Methode [(Quelle)](why.md#quellen); LeetTest beschreibt
die ganze Last: eine Liste von Methoden, jede mit eigener RPS, ein Lauf, ein Bericht.

**Zahlen mit sichtbaren Grenzen.** Ein Lastwerkzeug liefert eine Zahl, auf der Entscheidungen
beruhen. Eine Zahl mit einem Messfehler sieht genauso aus wie eine richtige, daher zeigt der Bericht
die angeforderte Last neben der gelieferten und nennt die Wartezeiten auf der Clientseite. Welche
Messfehler es gibt und wie LeetTest mit ihnen umgeht, steht auf [einer eigenen Seite](pitfalls.md).

Was das Werkzeug nicht tut: Es sieht den Dienst nur von außen — Latenz, Durchsatz, Fehler. CPU und
Speicher des Dienstes sieht es nicht und errät sie nicht aus eigenen Daten. Die kommen aus dem
Monitoring des Dienstes und werden zeitlich mit der Last abgeglichen.
