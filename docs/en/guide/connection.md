# Connection

[← Back to docs](../README.md)

> Translated from [docs/ru/guide/connection.md](../../ru/guide/connection.md) at feb2d1d, 2026-11-10.
> If they differ, the Russian one is right.

How LeetTest finds your service and gets into it: the address, encryption, headers with a token and
the methods' schema. The config, the certificates, the environment variables and the methods' schema
are checked before the start: on an error the exit code is 1 and the service gets not a single
request.

## Minimal config

```yaml
app:
  target:
    ip: 10.0.3.17
    port: 443
  ca: certs/ca.pem            # paths are relative to the config file
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

- `target` — the service's address: `ip` (a name or an IP) and `port`.
- `tls` — not set: encryption is on. `tls: false` — a connection without encryption.
- `ca` — a PEM file (the text format of certificates, starting with a `-----BEGIN` line) with the
  certificates to verify the service against instead of the system ones. Needs TLS.
- `cert` and `key` — the client certificate and its key in PEM, for a service with mTLS (mutual
  TLS: the service checks the client's certificate too). Only together.
- `server_name` — the name to verify the service's certificate against, if it does not name the
  address from `target`.
- `metadata` — headers of every request: `authorization`, `x-api-key` and others.

Every field is in the [reference](../reference.md#config).

## Tokens and secrets

`${NAME}` is substituted from an environment variable, inside a value too: `Bearer ${TOKEN}`.

```sh
export PAYMENTS_TOKEN=token-value
export PAYMENTS_KEY=key-value
leettest -c leettest.yaml
```

- A variable that is not set or is empty is an error before the start, naming it. Otherwise
  `Bearer ` would go out with no token, and the run would show 100% failures as the service's fault.
- Substitution works only in `metadata`. There is no flag for headers, so the token gets neither
  into `ps` nor into the shell history.
- Header values get neither into the report, nor into the run's progress output, nor into error
  messages.
- To write `${` literally, double the dollar: `$${`.
- Header names are lowercased. A config error: a key starting with `grpc-`, pseudo-headers such as
  `:path`, a key ending in `-bin`, `content-type`, `te`, `connection`, `keep-alive`,
  `proxy-connection`, `transfer-encoding`, `upgrade`, a value outside printable ASCII.

## What will appear in the report

- There is no flag that turns off the check of the service's certificate. `ca` replaces the system
  roots, it is not added to them. `server_name` changes only the name the certificate is checked
  against; a key encrypted with a password is not supported — decrypt it beforehand.
- The service answered `Unauthenticated` to the check and headers are set: the run will not start,
  the error is "target rejected credentials". `PermissionDenied` does not stop the run: the token is
  accepted, but the methods are marked unchecked. `Unauthenticated` with no headers does not stop it
  either, the note says: "target requires credentials; app.metadata is not set".
- Several connections: by default one connection lands on one server, and behind an L4 balancer
  (Kubernetes ClusterIP, NLB) or with several addresses in DNS only that one is loaded, and the
  report will not show it. `app.connections: N` opens N connections, and the report prints a
  `Connections:` block: the address, requests, the share of failures and p99 for each. Details are in
  the [reference](../reference.md#access-to-the-service).

## The methods' schema: reflection or .proto

By default LeetTest takes the schema from the service through gRPC server reflection (the service
itself serves the list of its methods and messages), and no `.proto` is needed. If reflection is
off, a method with `data` will not start: the error says so plainly. Two ways out:

- remove `data`: an empty message goes out, the method is not checked, a warning is printed;
- give the schema as a file (since v0.3): `app.proto` (files and `import_paths`, where to look for
  their imports) or `app.protoset` (a descriptor set built with `protoc --include_imports`) —
  [`examples/proto.yaml`](../../../examples/proto.yaml),
  [`examples/protoset.yaml`](../../../examples/protoset.yaml).

## Common mistakes

- **TLS is on, but the service has no encryption** (or the other way round): an error before the
  start. Set or remove `tls: false`.
- **The service's certificate does not name the address.** The error suggests `server_name`.
- **The service closed the connection right after the handshake.** It did not accept the client
  certificate: check `cert` and `key`.
- **The certificate file is not found or is not PEM, the certificate does not match the key, the
  certificate has expired.** All these errors arrive before the first request.
- **The address is `localhost`, but the service listens only on `127.0.0.1`.** The name may resolve
  to `::1`; give the IP.
- **The token is written as a value in the config.** The config's hash in the report will show any
  edit of the file, and the token itself would end up in the repository: write `${NAME}`.
