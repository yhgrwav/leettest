# First run

[← Back to docs](../README.md)

> Translated from [docs/ru/guide/first-run.md](../../ru/guide/first-run.md) at feb2d1d, 2026-11-10.
> If they differ, the Russian one is right.

This is the path from installing to the first report: install LeetTest, write a config for two
methods, run it and read the result. You need no service of your own: below is a test service that
ships with LeetTest. Working through an AI agent — give it [docs/agent.md](../../agent.md) (the
phrase for it is in the [README](../../../README.md#установка)): it will take the same steps itself.

## Where to run

Run on Linux or macOS next to the service: in the same network, in the same cluster or on a CI
runner. Everything between LeetTest and the service goes into the latency and looks like the
service's time: at 1000 requests per second through Docker Desktop's port forwarding the p99 was
38 ms, inside the Docker network 2 ms (our stand, one machine; in other conditions the numbers will
differ).

Windows is fine for trying the tool. On our Windows 11 machine the clock step was about 0.5 ms
(LeetTest measures it itself and prints it), and LeetTest may declare a run against a fast service
invalid (`clock_step`, exit code 2): the clock step is comparable to the latency itself. For
measurements take Linux. The rest on where to run is in the
[reference](../reference.md#where-to-run).

## Install

**Linux and macOS** (amd64 and arm64). You need `curl` and `tar`:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
```

The script installs the latest release into `~/.local/bin` and verifies the checksum. If that
directory is not on your `PATH`, it prints a line for your shell profile: run it in this terminal,
and to keep it add it to the profile. Another directory is the `LEETTEST_INSTALL_DIR` variable,
another version is `LEETTEST_VERSION`; the variable goes before `sh`, not before `curl`:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | LEETTEST_VERSION=v0.2.0 sh
```

**Windows.** A `.zip` archive from the
[releases page](https://github.com/yhgrwav/leettest/releases/latest): unpack `leettest.exe` into a
directory on your `PATH`.

**Any OS with Go 1.26 or newer** ([install Go](https://go.dev/doc/install); an older Go with
`GOTOOLCHAIN=auto` downloads the right one itself):

```sh
go install github.com/yhgrwav/leettest/cmd/leettest@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

Check, for any way:

```sh
leettest -version
```

## Run

Save this as `leettest.yaml`, putting in the address and methods of your service:

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

    - method: wallet.v1.WalletService/Transfer
      rps: 50
      duration: 1m
```

- `tls: false` — a service without encryption. Without this line TLS is on.
- `method` — the full name: `package.Service/Method`. No `.proto` is needed: LeetTest takes the
  schema from the service through gRPC server reflection (the service itself serves the list of its
  methods and messages). If reflection is off on the service, see [connection](connection.md).
- `rps` — requests per second for this method, `duration` — how long to load it.
- `warmup` — the first seconds go to the service but stay out of the statistics.

To check the config without load: LeetTest connects, checks the methods, sends one request per
method and shows the request and the reply. There is no report.

```sh
leettest -c leettest.yaml -dry-run
```

The run:

```sh
leettest -c leettest.yaml
```

Before the start LeetTest connects to the service and checks every method and request body. A
mistake in the config, an unknown method, a mistake in the request body, an `Unauthenticated`
refusal when headers are set — exit code 1 and not a single request to the service. A
`PermissionDenied` answer does not stop the run, details are in
[connection](connection.md#what-will-appear-in-the-report).

In a terminal a live screen runs: requests per second, requests without an answer, errors and
percentiles by the second, a tab per method, `?` for help. Stopping: the first `q` (or Ctrl+C) —
no new requests go out, the sent ones wait for their timeout; the second cuts them off, the report
is still printed; the third exits without a report. Without a terminal (CI, output to a file, ssh)
there is a progress line once a second on stderr instead of the screen; `-plain` turns it on in a
terminal too:

```text
32.0s  sent 25600  rps 800  in-flight 47  failed 51  not-sent 0  p99 43ms
```

`in-flight` — requests sent and with no answer yet; `not-sent` — requests that were due to go out by
the plan but did not (for example, they waited for a free connection).

The report goes to stdout, progress and errors to stderr. The first run in a terminal asks for a
colour theme (once; `-plain` skips the question).

## What the report contains

At the end — a row per method: how many were sent, how many failed, `sent/s` and the percentiles
p50, p90, p95, p99. The main points:

- The latency is counted from the request's **planned** moment. If the service or LeetTest itself
  delayed the request, that delay is in the measured one. A request goes out on schedule without
  waiting for the answers to the earlier ones.
- `>2.0s` is not a number but "at least 2 seconds": some requests were cut off by the timeout, and
  their real latency is higher. LeetTest does not put the timeout in place of an unknown value.
- Errors are split into categories (`overload`, `failure`, `timed out` and others), and under the
  report the gRPC codes stand on two lines: "sent by the target" — the status was sent by the
  service or a proxy in front of it, "set by the client" — the status was set by LeetTest, there
  was no answer.
- Exit code `0` does not mean "the service is healthy": it means the plan ran. For the run itself to
  give a "passed / failed" verdict, set [limits](../report.md#pass-limits-a-run-as-a-check).

Every number and category is in the section [Reading the report](../report.md#reading-the-report).

## A trial run on the test service

The test service is in the repository: a gRPC service whose behaviour you control. It answers
`grpc.health.v1.Health/Check` and `wallet.v1.WalletService` and serves reflection, as a real one
does. The right answer is known up front, so the report can be checked against what the service
actually did. You need `git` and Go 1.26+; the test service and `leettest` from one clone match its
configs by version.

```sh
git clone --depth 1 https://github.com/yhgrwav/leettest && cd leettest
go build -o bin/ ./cmd/leettest ./test/stand/cmd/stand
bin/stand -delay 20ms -hang-from 25s -life 3m > stand.log 2>&1 &
sleep 1
```

The service runs in the background (`sleep 1` is a second for it to start listening on the port) and
stops by itself after 3 minutes (`-life 3m`: the warm-up, 40 seconds of the run and some slack). It
answers in 20 ms, and 25 seconds after the first request it stops answering at all — that is how a
service that fell over under load looks.

The config [`examples/tour.yaml`](../../../examples/tour.yaml) uses every field that can be checked
on a local service:

```yaml
name: tour

app:
  target:
    ip: 127.0.0.1
    port: 50051
  tls: false
  metadata:
    authorization: Bearer ${LEETTEST_TOKEN}
    x-request-source: leettest-tour
  max_response_size: 1MiB
  connections: 1

load:
  warmup: 3s
  calls:
    - method: grpc.health.v1.Health/Check
      rps: 300
      duration: 40s
      timeout: 500ms
      data:
        service: wallet.v1.WalletService
    - method: wallet.v1.WalletService/GetBalance
      rps: 50
      duration: 40s
      timeout: 500ms
      dataset: wallets.jsonl
      dataset_order: random
      dataset_seed: 42
```

- `name` — what the run is called in the header and in the JSON report.
- `metadata` — headers of every request. `${LEETTEST_TOKEN}` is taken from the environment: the token
  gets neither into the config nor into the shell history; header values do not get into the report.
  A variable that is not set is an error before the start, not a run with an empty token.
- `max_response_size` — a reply over the limit counts as a `bad response`, not a success.
- `timeout` — how long to wait for an answer (2 s by default).
- `data` — the request body as plain YAML. A mistake in the body shows before the start.
- `dataset` — a file of request bodies instead of `data`, one JSON per line
  ([`examples/wallets.jsonl`](../../../examples/wallets.jsonl)): the first request goes out with the
  first line, the second with the second, after the last — the first again. `dataset_order: random`
  shuffles the file on each pass; the report prints the seed, and `dataset_seed` repeats the order.
  Beside the method's row the report says how much of the file was used.
- `connections` — how many connections carry the load (1 by default). Behind an L4 balancer one
  connection loads one server; with two or more the report gets a `Connections:` block — a row per
  connection.

Several methods are several calls in `calls`, each with its own rate. One method is one call.

```sh
export LEETTEST_TOKEN=demo
bin/leettest -c examples/tour.yaml
```

`demo` is the token for this tour. The run ends like this:

```text
run finished: 127.0.0.1:50051 in 40.5s
name: tour
settings: warm-up 3s, max in flight 5000, connections 1
sent 12950, failed 5248

method                                           sent   failed    sent/s       p50       p90       p95       p99
grpc.health.v1.Health/Check                     11100     4499       300    20.5ms    >500ms    >500ms    >500ms
  plan: 300 rps for 40s
wallet.v1.WalletService/GetBalance               1850      749        50    20.5ms    >500ms    >500ms    >500ms
  plan: 50 rps for 40s
  data: 4 of 4 requests from wallets.jsonl, each used up to 500 times, random order, seed 42

warm-up 1050 sent, excluded from stats

grpc.health.v1.Health/Check: at 300 rps, 4499 of 11100 calls (40.5%) got no answer within 500ms,
and nothing after the call sent at 25.0s of the run got one.
...
failed calls by gRPC code:
grpc.health.v1.Health/Check codes sent by the target: DeadlineExceeded 2
grpc.health.v1.Health/Check codes set by the client: DeadlineExceeded 4497
...
start lag, how late calls began against their schedule: p99 109us, max 225us.
...
5248 requests were abandoned before answering. A percentile shown as "> value"
is a lower bound: the real tail lies above it. Raise the timeout to see it.
```

(Linux, Docker, 9a08876.) What matters here:

- **Which run it is.** `name:` and `settings:` repeat the run's name and what it was started with,
  `plan:` under a method is the rate and duration set for it: a report pasted into a ticket can be
  compared with another.
- **At what load the service stopped coping.** "at 300 rps … nothing after the call sent at 25.0s" —
  the service went silent at second 25, exactly when it was told to. The moment is counted by when
  the request was sent, not by the schedule: LeetTest's own lag is not passed off as the service's
  silence.
- **`>500ms`** — "at least 500 ms": some requests did not wait for an answer.
- **`start lag`** — how far LeetTest fell behind its schedule. If it is large, the bottleneck is the
  machine LeetTest runs on, not the service; the report itself says when the latency tail is
  explained by delays inside LeetTest, not in the service.
- **`clock step`** — the precision of the machine's clock. On Linux it is in nanoseconds, and the
  line is absent. On Windows (about 0.5 ms for us) the line appears, and if the step is coarser than
  a quarter of the p50 the run is invalid: numbers below the clock step mean nothing.

The same config against the test service in another mode. Each block first stops the previous
service (`kill %%` ends the last background command of this terminal), then starts a new one and the
run; the `LEETTEST_TOKEN` token is already set.

The service freezes for 2 seconds with a timeout of 500 ms:

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -freeze-at 10s -freeze-for 2s -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```

p50 and p90 stay around 21 ms, and the requests that arrived during the pause did not get an answer:
about 4% `got no answer within 500ms`, p99 is printed as `>500ms`. Percentiles are not averaged: a
short freeze shows in the tail, not smeared over the mean.

Every tenth answer is a `RESOURCE_EXHAUSTED` error:

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -fail-every 10 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```

The category is `overload`, the code is on the "sent by the target" line.

The service allows one stream per connection, and every answer takes 150 ms:

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 150ms -max-streams 1 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```

Over one connection the service can answer no more than 6–7 requests a second. The report says where
they got stuck: `connections: 1; target stream limit 1` and `not sent N: waited for a stream N` —
the requests waited for a free stream inside LeetTest and did not reach the service before their
timeout. That is a limit of the connection, not a slow service.

With TLS: the test service with `-mtls` (mTLS is mutual TLS: the service checks not only its own
certificate for the client but the client's certificate too) writes certificates into
`test/stand/certs` at start and requires a client certificate;
[`examples/tour-tls.yaml`](../../../examples/tour-tls.yaml) points at these files.

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -mtls -delay 20ms -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour-tls.yaml
```

With no service at all: `-fake` loads a built-in stub instead of a service. The config comes from the
clone, and the `target` in it is ignored. The report is marked `fake target`.

```sh
bin/leettest -c examples/leettest.yaml -fake -fake-delay 30ms -fake-jitter 10ms
```

## Common mistakes

- **`0` taken for "the service is healthy".** Code `0` means "the plan ran". Health is checked by
  the `pass` limits and by reading the report.
- **A service without encryption, and no `tls: false`.** TLS is on by default, and a connection to a
  service without encryption fails before the start with code 1.
- **A method written incompletely.** It must be `package.Service/Method`; LeetTest finds a typo
  before the first request.
- **Reflection is off on the service.** A method with `data` will not start; the schema can be given
  as a `.proto` file or a protoset (a set of method descriptions in one file) —
  [connection](connection.md).
- **Code 2 and "clock step" on Windows.** Run from Linux next to the service.
- **A run through Docker Desktop's port forwarding.** The latencies will include that path.

## Next

- [Connection](connection.md) — TLS, headers, tokens.
- [Load](load.md) — methods, rate, ramp-up, request body.
- [Reference](../reference.md) — every field and flag.
- [Pitfalls](../pitfalls.md) — what breaks measurements.
