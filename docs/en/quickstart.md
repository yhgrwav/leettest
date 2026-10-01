> Translated from [quickstart.md](../ru/quickstart.md), 2026-10-01. If they differ, the Russian
> one is right.

# Quickstart

A full pass over what LeetTest does, in half an hour: from installing it to a report you can hand
to the service's developers, and a run in CI. Every field and flag is in the
[README](README.md); this is how they work together.

Working through an AI agent? Give it [AGENTS.md](../../AGENTS.md): the same ground, in the shape
an agent needs — a run without a terminal, JSON, exit codes.

## Where to run it

LeetTest is built for **Linux and macOS**: the generator goes next to the target — a server in
the same network, a container, a CI runner. The closer it is, the less foreign network is in the
numbers.

Windows is supported but measures worse: its clock steps about 0.5 ms, and a run against a fast
service will honestly be declared invalid (`clock_step`, exit code 2) — the clock step is as big as
the latency itself. Measure on Linux; Windows is fine for trying the tool out.

## 1. Install

```console
$ go install github.com/yhgrwav/leettest/cmd/leettest@latest
$ leettest -version
```

Go 1.26 or newer. Prebuilt binaries for Linux, macOS and Windows (amd64 and arm64) are on the
releases page.

## 2. A target: the reference stand

You need no service of your own for the tour: the repository has a reference stand, a gRPC
service whose behaviour you set. It answers `grpc.health.v1.Health/Check` and serves reflection,
as a real service does.

```console
$ git clone https://github.com/yhgrwav/leettest && cd leettest
$ go run ./test/stand/cmd/stand -delay 20ms -hang-from 25s -life 60s
```

The stand answers in 20 ms, and 25 seconds after the first call it stops answering at all — the
way a service that fell over under load looks. The right answer is known up front, so you can see
whether the report tells the truth.

## 3. The config

[`examples/tour.yaml`](../../examples/tour.yaml) uses every field a local stand can check:

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

load:
  warmup: 3s
  calls:
    - method: grpc.health.v1.Health/Check
      rps: 300
      duration: 40s
      timeout: 500ms
      data:
        service: wallet.v1.WalletService
```

- **`name`** — the run's name in the header.
- **`tls: false`** — the stand has no encryption. TLS is on by default; `ca`, `cert`, `key`,
  `server_name` for TLS and mTLS are in the README's «Access to the service».
- **`metadata`** — headers on every call. `${LEETTEST_TOKEN}` comes from the environment: the
  token is in neither the config nor the shell history, and LeetTest prints header values nowhere.
  An unset variable is an error before the start, not a run with an empty token.
- **`max_response_size`** — a reply over the limit is a `bad response`, not a success.
- **`warmup`** — for the first 3 seconds calls go to the target but stay out of the percentiles.
- **`rps`, `duration`** — each method has its own. Open model: a call goes out on schedule even if
  the earlier ones have not answered, and latency counts from the planned moment. A slow service
  neither slows the load nor hides its slowness.
- **`timeout`** — how long to wait for an answer (2 s by default).
- **`data`** — the request body as plain YAML. The schema comes from the service through
  reflection, no `.proto`. A mistake in the body shows before the start.

Several methods are several calls, each at its own rate. One method, one call: the report is per
method.

```yaml
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 5m
    - method: wallet.v1.WalletService/Transfer
      rps: 50
      duration: 5m
```

## 4. Run

```console
$ LEETTEST_TOKEN=demo leettest -c examples/tour.yaml
```

Before the start LeetTest connects to the target, checks each method through reflection, builds
the request body, and checks that the in-flight cap (`-max-in-flight`) holds a target that hangs.
Whatever can be known before the run is an error before the first call.

In a terminal you get the live view: RPS, calls in flight, errors and percentiles by the second,
a tab per method, `?` for help. **Stopping:** the first `q` (or Ctrl+C) sends no new calls and
lets the sent ones wait out their timeout; the second cuts them off and still prints the report;
the third exits without one.

Without a terminal (CI, output to a file) you get a progress line a second on stderr instead.

## 5. The report

The report goes to stdout, ASCII only — save it to a file or `grep` it. The tour above ends so:

```
run finished: 127.0.0.1:50051 in 40.5s
sent 11100, failed 4500

method                                           sent   failed    sent/s       p50       p90       p95       p99
grpc.health.v1.Health/Check                     11100     4500       300    20.9ms    >500ms    >500ms    >500ms

warm-up 900 sent, excluded from stats

grpc.health.v1.Health/Check: at 300 rps, 4500 of 11100 calls (40.5%) got no answer within 500ms,
and nothing after the call sent at 25.0s of the run got one.

failed calls by gRPC code:
grpc.health.v1.Health/Check codes sent by the target: DeadlineExceeded 67
grpc.health.v1.Health/Check codes set by the client: DeadlineExceeded 4433
...
start lag, how late calls began against their schedule: p99 923us, max 1.52ms.
...
4500 requests were abandoned before answering. A percentile shown as "> value"
is a lower bound: the real tail lies above it. Raise the timeout to see it.

clock step 525us on this host: every latency and wait is +/- 525us; ...
```

What matters here:

- **At what load the service stopped coping.** "at 300 rps … nothing after the call sent at
  25.0s" — the target went silent at second 25, exactly when the stand was told to. The moment is
  when the call went out, not when it was due: a lagging generator is not passed off as the
  target's silence.
- **`>500ms`** is not a number but a lower bound: some calls got no answer, and their real latency
  is above the timeout. LeetTest does not put the timeout in place of an unknown value.
- **Codes on two lines.** "sent by the target" — the status came from the target (or a proxy in
  front of it); "set by the client" — our client set it, no answer came. Two different stories for
  the developers.
- **`start lag`** — how far the generator fell behind its schedule. If it is large, the bottleneck
  is the generator's machine, not the target; the report says so itself when the latency tail is
  waiting on our side.
- **`clock step`** — how precise this machine's clock is. Coarser than a quarter of the p50, and
  the run is invalid: numbers below the clock step mean nothing.

The failure categories (`overload`, `failure`, `request error`, `timed out`, `cut off`,
`unreachable` and others) and what they mean are in the README, «Run».

## 6. Other target behaviours

The same config against the stand in another mode — what the report shows:

```console
$ go run ./test/stand/cmd/stand -delay 20ms -freeze-at 10s -freeze-for 2s -life 60s
```
A 2-second freeze with a 500 ms timeout: p50 and p90 stay near 21 ms, and the calls that arrived
during the pause got no answer — about 4% `got no answer within 500ms`, p99 printed as `>500ms`.
Percentiles are not averaged: a short freeze shows in the tail, not smeared over a mean.

```console
$ go run ./test/stand/cmd/stand -delay 20ms -fail-every 10 -life 60s
```
Every tenth call is `RESOURCE_EXHAUSTED`: category `overload`, its code on the "sent by the
target" line.

```console
$ go run ./test/stand/cmd/stand -delay 150ms -max-streams 1 -life 60s
```
The target allows one stream per connection and holds each answer 150 ms: through one connection
it can answer no more than 6–7 calls a second. The report says where they got stuck:
`connections: 1; target stream limit 1` and `not sent N: waited for a stream N` — calls waited
for a stream on our side and did not reach the target before their timeout. That is the
connection's limit, not a slow target.

## 7. Scripts and CI

```console
$ leettest -c load.yaml -output json > report.json
$ echo $?
```

The JSON is versioned (`schema_version`); decisions are fields, not text: `invalid_reasons`,
`methods[].invalid_reason`, `tail_wait_cause`, `methods[].silent_from_s`. They are described in
the README, «Run».

Exit codes:

| Code | Meaning |
|---|---|
| `0` | The plan ran, the report is complete |
| `1` | No run: flags, config, connection. No report |
| `2` | Invalid run: the in-flight cap, every call of a method a request error, or the host clock coarser than a quarter of the p50. A report exists; its numbers are not about the load |
| `3` | Stopped before the plan ended. The report covers what ran |
| `130`, `143` | Killed by Ctrl+C or SIGTERM, no report |

There are no pass/fail thresholds yet: in CI check the exit code and the JSON fields.

## 8. No service at all

```console
$ leettest -c examples/leettest.yaml -fake -fake-delay 30ms -fake-jitter 10ms
```

`-fake` loads a built-in fake target instead of a service — to look at the screen and the report
without one. The report is marked `fake target`.

## Next

- [README](README.md) — every field, flag and report line.
- [Pitfalls](pitfalls.md) — what breaks measurements.
- [CONTRIBUTING](../../CONTRIBUTING.md) — proposing a change, as a person or with an AI agent.
