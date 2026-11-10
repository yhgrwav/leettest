# Verbindung

[← Zur Dokumentation](../README.md)

> Übersetzt aus [docs/ru/guide/connection.md](../../ru/guide/connection.md) bei feb2d1d, 2026-11-10. Bei
> Abweichungen gilt die russische Fassung.
> **Kann hinterherhinken:** Die deutsche Fassung wird vor jedem Release nachgezogen. Aktuell ist
> die [englische](../../en/guide/connection.md).

Wie LeetTest Ihren Dienst findet und sich bei ihm anmeldet: Adresse, Verschlüsselung, Header mit
Token und das Schema der Methoden. Konfiguration, Zertifikate, Umgebungsvariablen und das Schema der
Methoden werden vor dem Start geprüft: Bei einem Fehler ist der Exit-Code 1, und es geht keine
einzige Anfrage an den Dienst.

## Die minimale Konfiguration

```yaml
app:
  target:
    ip: 10.0.3.17
    port: 443
  ca: certs/ca.pem            # Pfade — relativ zur Konfigurationsdatei
  cert: certs/client.pem
  key: certs/client.key
  server_name: payments.internal
  metadata:
    authorization: Bearer ${PAYMENTS_TOKEN}
    x-api-key: ${PAYMENTS_KEY}

load:
  calls:
    - method: payments.v1.PaymentService/GetPayment
      rps: 100
      duration: 1m
```

- `target` — die Adresse des Dienstes: `ip` (Name oder IP) und `port`.
- `tls` — nicht angegeben: Verschlüsselung ist an. `tls: false` — Verbindung ohne Verschlüsselung.
- `ca` — eine PEM-Datei (Textformat für Zertifikate, beginnt mit der Zeile `-----BEGIN`) mit den
  Zertifikaten, gegen die der Dienst statt der Systemzertifikate geprüft wird. Braucht TLS.
- `cert` und `key` — das Client-Zertifikat und sein Schlüssel im PEM-Format, für einen Dienst mit
  mTLS (gegenseitiges TLS: Der Dienst prüft auch das Zertifikat des Clients). Nur zusammen.
- `server_name` — der Name, gegen den das Zertifikat des Dienstes geprüft wird, wenn es die Adresse
  aus `target` nicht nennt.
- `metadata` — Header jeder Anfrage: `authorization`, `x-api-key` und andere.

Jedes Feld steht in der [Referenz](../../en/reference.md#config) (englisch).

## Token und Geheimnisse

`${NAME}` wird aus einer Umgebungsvariablen eingesetzt, auch innerhalb eines Wertes: `Bearer ${TOKEN}`.

```sh
export PAYMENTS_TOKEN=token-wert
export PAYMENTS_KEY=schluessel-wert
leettest -c leettest.yaml
```

- Ist die Variable nicht gesetzt oder leer, kommt vor dem Start ein Fehler mit ihrem Namen. Sonst
  ginge `Bearer ` ohne Token raus, und der Lauf zeigte 100 % Fehlschläge als Schuld des Dienstes.
- Die Ersetzung funktioniert nur in `metadata`. Ein Flag für Header gibt es nicht, deshalb landet das
  Token weder in `ps` noch in der Shell-Historie.
- Die Werte der Header gelangen weder in den Bericht noch in die Ausgabe des Laufs noch in
  Fehlermeldungen.
- Um `${` wörtlich zu schreiben, verdoppeln Sie das Dollarzeichen: `$${`.
- Die Namen der Header werden in Kleinbuchstaben umgesetzt. Ein Konfigurationsfehler sind: ein
  Schlüssel mit `grpc-`, Pseudo-Header wie `:path`, ein Schlüssel mit `-bin`, `content-type`, `te`,
  `connection`, `keep-alive`, `proxy-connection`, `transfer-encoding`, `upgrade`, ein Wert außerhalb
  des druckbaren ASCII.

## Was im Bericht erscheint

- Ein Flag, das die Prüfung des Zertifikats des Dienstes abschaltet, gibt es nicht. `ca` ersetzt die
  Systemwurzeln, es kommt nicht zu ihnen hinzu. `server_name` ändert nur den Namen, mit dem das
  Zertifikat abgeglichen wird; ein mit Passwort verschlüsselter Schlüssel wird nicht unterstützt —
  entschlüsseln Sie ihn vorher.
- Der Dienst antwortete bei der Prüfung mit `Unauthenticated`, und Header sind gesetzt: Der Lauf
  beginnt nicht, Fehler „target rejected credentials“. `PermissionDenied` stoppt den Lauf nicht: Das
  Token ist angenommen, aber die Methoden werden als ungeprüft markiert. `Unauthenticated` ohne
  Header stoppt ebenfalls nicht, der Hinweis lautet: „target requires credentials; app.metadata is
  not set“.
- Mehrere Verbindungen: Standardmäßig trifft die eine Verbindung einen Server, und hinter einem
  L4-Balancer (Kubernetes ClusterIP, NLB) oder bei mehreren Adressen im DNS wird nur er belastet,
  und der Bericht zeigt das nicht. `app.connections: N` öffnet N Verbindungen, und der Bericht druckt
  den Block `Connections:`: Adresse, Anfragen, Anteil der Fehlschläge und p99 je Verbindung. Einzelheiten
  — in der [Referenz](../../en/reference.md#access-to-the-service) (englisch).

## Das Schema der Methoden: Reflection oder .proto

Standardmäßig holt LeetTest das Schema über gRPC Server Reflection vom Dienst (der Dienst gibt die
Liste seiner Methoden und Nachrichten selbst heraus), ein `.proto` ist nicht nötig. Ist Reflection
abgeschaltet, startet eine Methode mit `data` nicht: Der Fehler sagt das ausdrücklich. Zwei Auswege:

- `data` entfernen: Es geht eine leere Nachricht raus, die Methode ist nicht geprüft, dazu wird eine
  Warnung gedruckt;
- das Schema als Datei geben (seit v0.3): `app.proto` (Dateien und `import_paths`, wo ihre Importe
  gesucht werden) oder `app.protoset` (ein Satz von Deskriptoren, gebaut mit
  `protoc --include_imports`) — [`examples/proto.yaml`](../../../examples/proto.yaml),
  [`examples/protoset.yaml`](../../../examples/protoset.yaml).

## Häufige Fehler

- **TLS ist an, der Dienst aber ohne Verschlüsselung** (oder umgekehrt): Fehler vor dem Start. Setzen
  oder entfernen Sie `tls: false`.
- **Das Zertifikat des Dienstes nennt die Adresse nicht.** Der Fehler weist auf `server_name` hin.
- **Der Dienst hat die Verbindung gleich nach dem Handshake geschlossen.** Er hat das
  Client-Zertifikat nicht akzeptiert: Prüfen Sie `cert` und `key`.
- **Die Zertifikatsdatei fehlt oder ist kein PEM, das Zertifikat passt nicht zum Schlüssel, das
  Zertifikat ist abgelaufen.** Alle diese Fehler kommen vor der ersten Anfrage.
- **Die Adresse ist `localhost`, der Dienst hört aber nur auf `127.0.0.1`.** Der Name kann zu `::1`
  aufgelöst werden; geben Sie die IP an.
- **Das Token steht als Wert in der Konfiguration.** Der Hash der Konfiguration im Bericht zeigt jede
  Änderung der Datei, und das Token selbst landet im Repository: Schreiben Sie `${NAME}`.
