> Translated from [docs/ru/quickstart.md](../ru/quickstart.md) at 1bd45f9, 2026-10-12. If they
> differ, the Russian one is right.

# Quickstart

A full pass over what LeetTest does, in half an hour: from installing it to a report you can hand
to the service's developers, and a run in CI. Every field and flag is in the
[reference](reference.md), reading the report in the [README](README.md); this is how they work
together.

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

Two paths, take either. The commands work as they are in a Linux or macOS terminal.

**1. A prebuilt binary** (Linux and macOS; amd64 and arm64) — no Go needed, only `curl` and `tar`:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
```

The script installs the latest release into `~/.local/bin` and verifies the checksum. If that
directory is not on your `PATH`, it prints an `export PATH=...` line: run it in this terminal, and
to keep it put it in your shell profile. `LEETTEST_INSTALL_DIR` picks another directory,
`LEETTEST_VERSION` another version; the variable goes before `sh`, not before `curl`:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | LEETTEST_VERSION=v0.2.0 sh
```

Windows has a `.zip` in the [release](https://github.com/yhgrwav/leettest/releases/latest).

**2. Through Go**: Go 1.26 or newer ([install](https://go.dev/doc/install); an older Go with
`GOTOOLCHAIN=auto` downloads the right one itself):

```sh
go install github.com/yhgrwav/leettest/cmd/leettest@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

Check, for both paths:

```sh
leettest -version
```

## 2. A target: the reference stand

You need no service of your own for the tour: the repository has a reference stand, a gRPC
service whose behaviour you set. It answers `grpc.health.v1.Health/Check` and
`wallet.v1.WalletService` and serves reflection, as a real service does.

The tour goes on in the same terminal and builds from a clone: it needs `git` and Go 1.26+, and
the stand and `leettest` from one clone match its configs exactly.

```sh
git clone --depth 1 https://github.com/yhgrwav/leettest && cd leettest
go build -o bin/ ./cmd/leettest ./test/stand/cmd/stand
bin/stand -delay 20ms -hang-from 25s -life 3m > stand.log 2>&1 &
sleep 1
```

The stand runs in the background (`sleep 1` gives it a moment to start listening) and stops by itself after 3 minutes (`-life 3m`: the warm-up, 40
seconds of the run and some slack). It answers in 20 ms, and 25 seconds after the first call it
stops answering at all — the way a service that fell over under load looks. The right answer is
known up front, so you can see whether the report tells the truth.

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

- **`name`** — the run's name in the header and in the JSON report (`name`).
- **`tls: false`** — the stand has no encryption. TLS is on by default; `ca`, `cert`, `key`,
  `server_name` are in [`examples/tour-tls.yaml`](../../examples/tour-tls.yaml) (below, "TLS").
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
- **`dataset`** — a file of request bodies instead of `data`, one JSON per line
  ([`examples/wallets.jsonl`](../../examples/wallets.jsonl)): the first call goes out with the
  first line, the second with the second, and after the last line it starts over. Beside the
  method's row the report says how much of the file was used. `dataset_order: random` shuffles
  the file on each pass; the report prints the seed, and `dataset_seed` repeats the order.
- **`connections`** — how many connections carry the load (1 by default). Behind an L4
  balancer one connection loads one backend; with N ≥ 2 the report adds a `Connections:` block, a
  row per connection.

Several methods are several calls, each at its own rate. One method, one call: the report is per
method.

## 4. Run

```sh
export LEETTEST_TOKEN=demo
bin/leettest -c examples/tour.yaml
```

`demo` — the shared token we give the first users.

The first launch in a terminal asks for a colour theme: arrows and Enter, asked once (`-plain`
skips both the question and the live view).

Before the start LeetTest connects to the target, checks each method through reflection, builds
the request body, and checks that the in-flight cap (`-max-in-flight`) holds a target that hangs.
Whatever can be known before the run is an error before the first call.

In a terminal you get the live view: RPS, calls in flight, errors and percentiles by the second,
a tab per method, `?` for help. **Stopping:** the first `q` (or Ctrl+C) sends no new calls and
lets the sent ones wait out their timeout; the second cuts them off and still prints the report;
the third exits without one.

Without a terminal (CI, output to a file) you get a progress line a second on stderr instead.

**TLS.** With `-mtls` the stand writes a CA, its certificate and a client one into
`test/stand/certs` at start and requires the client certificate;
[`examples/tour-tls.yaml`](../../examples/tour-tls.yaml) points at those files:

The stand from §2 holds the same port, so it is stopped before another: `kill %%` ends the last
background command of this terminal (if the stand has already stopped by itself, `2>/dev/null`
hides the error):

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -mtls -delay 20ms -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour-tls.yaml
```

## 5. The report

The report goes to stdout, ASCII only — save it to a file or `grep` it. The tour above ends so:

```
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

(Linux, Docker, 9a08876.)

What matters here:

- **Which run it is.** `name:` and `settings:` repeat the run's name and what it was started with,
  and `plan:` under a method its configured rate and duration, so a report pasted into a ticket
  can be compared with another.
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
- **`clock step`** — how precise the machine's clock is; on Linux it is nanoseconds and the line is
  absent. On Windows (about 0.5 ms) it shows, and coarser than a quarter of the p50,
  the run is invalid: numbers below the clock step mean nothing.

The failure categories (`overload`, `failure`, `request error`, `timed out`, `cut off`,
`unreachable` and others) and what they mean are in the
[README, «Reading the report»](README.md#reading-the-report).

Exit code `0` does not mean "the service is healthy": read the load limit and the causes
([README, «Exit codes»](README.md#exit-codes)).

## 6. Other target behaviours

The same config against the stand in another mode — what the report shows. Each block first stops
the previous stand (as in §4), then starts a new one and the run; `LEETTEST_TOKEN` from §4 is
already set.

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -freeze-at 10s -freeze-for 2s -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```
A 2-second freeze with a 500 ms timeout: p50 and p90 stay near 21 ms, and the calls that arrived
during the pause got no answer — about 4% `got no answer within 500ms`, p99 printed as `>500ms`.
Percentiles are not averaged: a short freeze shows in the tail, not smeared over a mean.

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -fail-every 10 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```
Every tenth call is `RESOURCE_EXHAUSTED`: category `overload`, its code on the "sent by the
target" line.

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 150ms -max-streams 1 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```
The target allows one stream per connection and holds each answer 150 ms: through one connection
it can answer no more than 6–7 calls a second. The report says where they got stuck:
`connections: 1; target stream limit 1` and `not sent N: waited for a stream N` — calls waited
for a stream on our side and did not reach the target before their timeout. That is the
connection's limit, not a slow target.

## 7. Scripts and CI

```sh
leettest -c load.yaml -output json > report.json
echo $?
```

The JSON is versioned (`schema_version`); decisions are fields, not text: `invalid_reasons`,
`methods[].invalid_reason`, `tail_wait_cause`, `methods[].silent_from_s`. They are described in
the [README, «JSON for scripts and CI»](README.md#json-for-scripts-and-ci).

Exit codes:

| Code | Meaning |
|---|---|
| `0` | The plan ran, the report is complete |
| `1` | The report you asked for was not produced: the run did not happen (flags, config, connection) or the report could not be written to `-json-file`. stderr says why |
| `2` | Invalid run: the in-flight cap, every call of a method a request error, or the host clock coarser than a quarter of the p50. A report exists; its numbers are not about the load |
| `3` | Stopped before the plan ended. The report covers what ran |
| `4` | A finished run missed a `pass` limit. The report exists; `thresholds` says which |
| `130`, `143` | Killed by Ctrl+C or SIGTERM, no report |

For a pass/fail gate in CI set `pass` limits under the call
([`examples/pass.yaml`](../../examples/pass.yaml), [README, «Pass limits»](README.md#pass-limits-a-run-as-a-check))
and fail the build on a non-zero exit code; the `thresholds` JSON field says which
limit was missed or could not be decided.

## 8. No service at all

```sh
bin/leettest -c examples/leettest.yaml -fake -fake-delay 30ms -fake-jitter 10ms
```

`-fake` loads a built-in fake target instead of a service — to look at the screen and the report
without one. The config comes from the clone (`examples/leettest.yaml`) and its `target` is
ignored. The report is marked `fake target`.

## Next

- [Reference](reference.md) — every field and flag; [README](README.md) — reading the report.
- [Pitfalls](pitfalls.md) — what breaks measurements.
- [CONTRIBUTING](../../CONTRIBUTING.md) — proposing a change, as a person or with an AI agent.
