# Was LeetTest nicht tut und wo seine Zahlen nicht mehr tragen

[← Zur Dokumentation](README.md)

> Übersetzt aus [docs/ru/limitations.md](../ru/limitations.md) bei 3a9dbba, 2026-11-10. Bei Abweichungen gilt die
> russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../en/limitations.md).

LeetTest sieht nur die Clientseite: die Maschine und den Prozess, die die Last senden (im Folgenden
der Generator), und das, was zurückkommt. Die Ressourcen des Ziels kennt und errät es nicht. Diese
Seite ist der eine Ort, an dem die Grenzen gesammelt sind: unter welchen Bedingungen die Zahlen des
Berichts nicht vom Ziel handeln und was das Werkzeug schlicht noch nicht kann. Einzelheiten zu jedem
Punkt stehen auf den Seiten, auf die er verweist.

## Wo die Zahlen nicht mehr vom Ziel handeln

Wenn der Generator oder der Weg zum Ziel die Messung verfälscht, sagt der Bericht das entweder
selbst oder kann es nicht. Hier steht, was genau.

- **Die CPU des Generators.** Der Generator braucht CPU, damit die Aufrufe nach Plan rausgehen.
  Reicht sie nicht (wenige Kerne, `GOMAXPROCS`, CPU-Quota des Containers), starten die Aufrufe
  später als geplant, und diese Zeit steckt in der Latenz. Hat die Verspätung des Generators den
  p99-Schwanz entschieden, ist der Lauf ungültig (`generator_late`, Exit-Code 2); der Bericht nennt
  die Methoden und rät daneben, dem Generator mehr CPU zu geben, die Rate zu senken oder sie auf
  mehrere Maschinen zu verteilen. Eine Methode wird ab 100 Beobachtungen beurteilt; bei weniger ist
  p99 das Maximum, der Lauf bleibt gültig, mit Hinweis. Eine p99-Verschiebung unter 10 % ist kein
  Urteil, sondern ein Hinweis mit der Zahl. Die Suche nach dem Bruchpunkt bekommt dieses Urteil
  nicht: ihre Stufen haben die eigene Ursache `generator`. Der Laufpass zeigt CPU-Anzahl,
  `GOMAXPROCS` und die Quota. Der Bericht nennt die Grenze, an die der
  Generator gestoßen ist (`GOMAXPROCS`, CPU-Quota, die CPU selbst oder der Zeitplan), oder sagt, dass
  er keine nennen kann; außerdem ist der Lauf ungültig, wenn der Generator 1 % oder mehr der Aufrufe
  einer Methode nicht gesendet hat.
- **Der Zeitplan unter Linux und unter Windows.** Der exakte Aufrufplan gilt nur unter Linux. In
  einem Container mit einer CPU-Quota unter zwei Kernen sind seltene Startverspätungen bis zur
  Länge der Quota-Periode (meist 100 ms) möglich; der Bericht zeigt sie. Unter Windows tickt die
  Uhr in Schritten von etwa 0,5 ms, und ein Lauf gegen einen schnellen Dienst wird für ungültig
  erklärt (`clock_step`, Exit-Code 2): Die Uhr ist gröber als ein Viertel des p50, oder der Schritt
  ist im Lauf auf 1 µs oder mehr gewachsen. Messen Sie von Linux aus, neben dem Ziel.
- **Das Netz zwischen Generator und Ziel.** Alles dazwischen geht in die Latenz ein und sieht aus
  wie die Zeit des Ziels. Die Netzverzögerung kennt das Werkzeug nicht gesondert und fällt darüber
  kein Urteil. Auf unserem Teststand war p99 bei 1000 RPS über die Portweiterleitung von Docker
  Desktop 38 ms, innerhalb des Docker-Netzes 2 ms.
- **Ein L4-Balancer.** Standardmäßig läuft die ganze Last über eine Verbindung, und die landet auf
  einem Backend: hinter einem L4-Balancer (ClusterIP in Kubernetes, NLB) und auch dann, wenn der DNS
  mehrere Adressen zurückgibt. „Hält X nicht“ betrifft dann dieses Backend, nicht den Dienst, und
  der Bericht zeigt das nicht. Um mehrere zu belasten, setzen Sie `app.connections`: Der Bericht
  zeigt dann eine Zeile je Verbindung. Ein L7-Balancer, der einzelne Anfragen verteilt (Envoy,
  gRPC-Ingress), verteilt auch eine einzelne Verbindung.
- **Die Stream-Quota des Ziels.** Das Ziel gibt an, wie viele Aufrufe gleichzeitig auf einer
  Verbindung laufen dürfen (`MAX_CONCURRENT_STREAMS`). Ein Aufruf, für den kein Stream frei war,
  wartet beim Client, und diese Wartezeit geht in die Latenz ein. Ist der Grund für den Schwanz,
  dass die Streams ausgingen, sagt der Bericht das und druckt p99 ohne Wartezeiten, aber eine
  Kapazität des Ziels über „Limit × Verbindungen“ ist nicht gemessen.
- **Die Obergrenze für Aufrufe im Flug.** `-max-in-flight` (Standard 5000) begrenzt die Aufrufe, die
  auf eine Antwort warten. Die Summe `rps × timeout` über die Methoden plus Reserve wird vor dem
  Start damit abgeglichen, daher stößt ein hängendes Ziel nicht an die Grenze. Ist sie trotzdem
  erschöpft, haben den Slot der Generator gehalten (ihm fehlte Prozessor), und der Lauf ist
  ungültig.
- **Was ein ungültiger Lauf nennt und was nicht.** Er nennt die Ursache aus einer geschlossenen
  Liste und die Methode. Er nennt keinen Engpass des Ziels, unterscheidet nicht die CPU des Ziels
  von seiner Datenbank und urteilt nicht über das, was der Client nicht sehen kann.
- **Fehlerkategorien.** Der Status kann von einem Proxy vor dem Ziel kommen, nicht vom Ziel: nginx
  ohne lebendes Backend antwortet `UNAVAILABLE`, und der Client unterscheidet das nicht. Eine zu
  große Anfrage wird am Fehlertext von grpc-go erkannt; ein Ziel auf anderer Implementierung (Envoy,
  Java) schreibt ihn anders, und die Ablehnung landet in `overload` statt in `request error`.
- **Kleine Stichprobe.** Bei weniger als 100 Beobachtungen ist p99 das Maximum. Das Urteil über den
  Generator weiß das, doch `leettest compare` und die `pass`-Grenze auf p99 beurteilen ein solches
  p99 ohne Vorbehalt.
- **Ein brüchiges Urteil über den Generator.** Bei 200 Beobachtungen hängt der p99-Schwanz an zwei
  Aufrufen: Ein einziger Aufruf, der 4–35 ms später gestartet ist, kann den Lauf ungültig machen.
  Die Zahl lügt nicht, aber das Urteil auf einer solchen Stichprobe ist brüchig.
- **Die Fenster „over time“.** Ihr p50 und p99 sind mit zwei signifikanten Stellen angegeben, daher
  vergleicht man sie nicht bis zur letzten Stelle mit dem p50 des ganzen Laufs. Die Fenster
  brauchen Speicher: etwa 74 KB je Paar „Methode — Fenster“, bei 50 Methoden rund 250 MB. Das
  Histogramm des Warm-ups braucht etwa 600 KB je Methode; bei vielen Methoden mit Warm-up addiert
  sich der Speicher.

## Was es noch nicht kann

- **Nur Unary-Aufrufe.** Methoden mit Streaming werden beim Laden abgelehnt.
- **Verkettung von Aufrufen.** Ein Feld aus der Antwort einer Methode lässt sich nicht in die
  Anfrage einer anderen einsetzen — geplant, siehe [Verkettung von Aufrufen](chaining.md) und
  [Plan](roadmap.md).
- **Token-Erneuerung** in einem langen Lauf — geplant, siehe [Plan](roadmap.md).
- **Jeder Aufruf einer Methode geht mit demselben Body raus,** solange kein `dataset` gesetzt ist.
  Mit `dataset` sind die Anfragen verschieden, doch das heißt nicht, dass das Ziel ebenso viele
  verschiedene Anfragen gesehen hat: Die Zeile `data:` im Bericht zeigt, wie viele Anfragen der
  Datei benutzt wurden.
- **Header mit `-bin`** (binäre) werden nicht unterstützt.
- **Ein privater Schlüssel mit Passwort** wird nicht unterstützt: Entschlüsseln Sie ihn vorher.

## Wohin als Nächstes

- [Wie man den Bericht liest](../en/report.md#reading-the-report) (englisch) — Erwartungen auf
  der Clientseite, Kategorien, Uhren.
- [Wann ein Lauf ungültig ist](../en/report.md#when-a-run-is-invalid) (englisch) — die vollständige
  Liste der Ursachen.
- [Wo man startet](../en/reference.md#where-to-run) (englisch) — was die Messung stört und von wo
  man besser startet.
- [Welche Probleme des Lasttests es löst](pitfalls.md) — was das Werkzeug zu jedem tut.
