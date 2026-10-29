<div align="center">

<h1><img src="../../assets/logo.png" width="360" alt="LeetTest"></h1>

**Find where your gRPC service breaks — with numbers you can trust.**

[Русский](../ru/) · [English](../en/) · [Deutsch](../de/) · [简体中文](../zh-CN/)

[![CI](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/yhgrwav/leettest.svg)](https://pkg.go.dev/github.com/yhgrwav/leettest)
[![Go version](https://img.shields.io/github/go-mod/go-version/yhgrwav/leettest)](../../go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](../../LICENSE)

</div>

---

> Translated from [README.md](../../README.md) at ebf39ff, 2026-10-13. If they differ, the Russian
> one is right. Plus an index of English docs, not in the Russian source.

The tool answers the question people bring to a load test: **at what load does the service stop
coping, and where is the bottleneck.** It applies load that looks like production — several
methods at once, each at its own RPS — and measures so that the numbers don't lie while the
service degrades. No `.proto`, no codegen, no scripts.

Priorities, in order: the numbers don't lie; the tool is pleasant to use — an unclear error
message is as much a defect as a wrong number; the generator never becomes the bottleneck.

The project is growing: what works today is below, what comes next is in the [roadmap](roadmap.md).

This is what a run looks like: mid-load the service freezes for three seconds — the screen shows
the calls in flight piling up, and the report counts the ones left without an answer.

<img src="../../assets/demo.gif" width="100%" alt="A LeetTest run in a terminal: the load screen, the target freezing for three seconds, the report showing the unanswered calls and a p99 above 2 seconds">

What questions LeetTest answers — [as a list](questions.md).

## Install

Linux and macOS (amd64 and arm64) — one command:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
```

The script installs the latest release into `~/.local/bin`, verifies the checksum and, if that
directory is not on your `PATH`, prints the line for your shell profile. On Windows, take the
archive from the [releases page](https://github.com/yhgrwav/leettest/releases/latest). Or with Go 1.26+:

```sh
go install github.com/yhgrwav/leettest/cmd/leettest@latest
```

Check: `leettest -version`. If the command is not found after `go install` —
`export PATH="$PATH:$(go env GOPATH)/bin"`.

**Where to run:** on Linux next to the target — in the same network, cluster or on a CI runner.
Everything between the generator and the target goes into the latency. More in the
[reference](reference.md#where-to-run).

**[Quickstart →](quickstart.md)** — every capability on the reference stand: a config with every
field, reading the report, other target behaviours, CI. Working through an AI agent — give it
[AGENTS.md](../../AGENTS.md).

## Run

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

```sh
leettest -c leettest.yaml
```

The method name is the full one, `package.Service/Method`. The tool takes the method's schema from
the service through gRPC server reflection, so no `.proto` is needed. A service without reflection
(since v0.3): the schema comes from `.proto` files or a protoset —
[`examples/proto.yaml`](../../examples/proto.yaml), the `app.proto` / `app.protoset` field in the
[reference](reference.md). The config, the address, the
methods and the request body are checked before the start: any error means exit 1 and not a single
call to the target. To check the config before the load, run
`leettest -c leettest.yaml -dry-run`: one request per call, the request and the
reply on screen, no report. The first `warmup` seconds stay out of the stats. A ramp from zero to `rps` is
`load.ramp: 30s` ([`examples/ramp.yaml`](../../examples/ramp.yaml)): a run lasts `ramp` + `duration`,
the ramp's calls stay out of the stats too (the `pass` limits count the hold), and `warmup`, unlike
the ramp, counts toward `duration`. Every call of a method goes
out with the same body: for a write with an idempotency key the repeat path is measured, not
creating the record. A different request for each call: a file in `dataset`, one
JSON per line, in order, round and round (`dataset_order: random` — in a random one, the seed in
the report repeats the order). Every field, TLS, headers, the request body and flags
are in the **[reference](reference.md)**.

In a terminal the run goes full-screen, `q` to stop. Without a terminal (CI, redirected output) — a
progress line once a second:

```
32.0s  sent 25600  rps 800  in-flight 47  failed 51  not-sent 0  p99 43ms
```

The report goes to stdout, progress and errors to stderr. No live screen (e.g. over ssh): `-plain`.

## With your AI agent

If you would rather not write the config yourself, give the link to your agent: it installs
LeetTest, asks about your service and runs the load.

```text
Read https://raw.githubusercontent.com/yhgrwav/leettest/main/AGENTS.md and load-test my gRPC service
```

The guide it works from is [AGENTS.md](../../AGENTS.md).

## Reading the report

At the end — a report per method: sent, failed, `sent/s`, p50/p90/p95/p99.

- A call counts from its **planned** moment: if the target or the generator delayed it, that is in
  the latency.
- `sent/s` is the calls sent divided by the sending time: a target hanging at the end of the run
  does not lower this number.
- A percentile whose place calls cut off by the timeout could take is printed as a **lower
  bound**: `>2.0s`. Not a value, but "at least".
- Under the table a method gets an `over time` block (not released): the measured part of the run,
  from the end of the warm-up, in at most 12 rows; JSON holds up to 60 windows. A call belongs to
  the row of its planned moment, whenever it finished. A row shows `sent/s`, the share failed, p50
  and p99 to two significant digits, so a row's p50 may differ from the table's p50 in the last
  digit. A `no calls` row means no call went out in that window. If the rate is below one call
  per window, that is the plan, not a failure: compare with the method's `plan:`. With a `ramp`
  the first rows are the ramp: a low `sent/s` there is the plan, not a failure; look for the row
  where p99 or the failed share climbs before the rate has reached its plateau.
- Under a method with a `dataset` stands a `data:` line: how many requests of the
  file were used and how many times the most used one went out. It does not mean the target saw
  that many distinct requests.
- By default the load goes over **one connection**, and it lands on **one backend**: behind an L4
  balancer (Kubernetes ClusterIP, NLB) and when DNS returns several addresses. "Does not hold X" is
  about that backend, not the service, and the report will not show it. To load several, set
  `app.connections`: the report's `Connections:` block shows a row per connection —
  address, calls, share failed and p99 — so a lagging backend is visible. An L7 balancer that
  spreads individual requests (Envoy, a gRPC ingress) spreads even one connection. The report
  prints how many times the connection reconnected and which concurrent stream limit the target
  announced.

**Categories.** Answers other than a success are split into rows, each with its own percentiles.
The status may have come from a proxy in front of the target rather than from the target: nginx
without a live backend answers `UNAVAILABLE`, and the client cannot tell one from the other.

- `request error`: the call would fail at any rate — a wrong request, or one the target or a proxy
  found too large.
- `overload`: the status says overloaded or unavailable, `RESOURCE_EXHAUSTED` or `UNAVAILABLE`.
  These are the status's words, not a diagnosis of the target.
- `failure`: the status says the call broke: `INTERNAL`, `UNKNOWN`, `DATA_LOSS`, `CANCELLED` from
  the other end, `ABORTED` (a conflict between concurrent changes, not a lack of capacity).
- `bad response`: a reply came and the client did not accept it: over `app.max_response_size`, or
  compressed with an encoding the client did not announce, or it breaks the one-reply shape: an OK
  with no message, or an extra message.
- `client error`: the client itself refused to send: the request did not encode, or a codec or an
  interceptor failed.

Calls that never reached the target count as failures but stay out of the percentiles. A request
that went out but got no status — the other end reset the stream or dropped the connection — is
counted separately, as `cut off`: the target or a proxy may have processed it, which for a write is
a reason to check for duplicates. A call cut off by our own stop (Ctrl+C, SIGTERM) is `aborted`
whatever its code: not the target's refusal.

| Code | Status came over the wire | Code set by the client, request went out | Code set by the client, request did not go out |
|---|---|---|---|
| `INVALID_ARGUMENT`, `NOT_FOUND`, `ALREADY_EXISTS`, `PERMISSION_DENIED`, `UNAUTHENTICATED`, `FAILED_PRECONDITION`, `OUT_OF_RANGE`, `UNIMPLEMENTED` | `request error` | `cut off` | `client error` |
| `RESOURCE_EXHAUSTED` | `overload`; grpc-go's "larger than max" is a `request error` | `cut off`; a reply over our limit is a `bad response` | `client error` |
| `UNAVAILABLE` | `overload` | `cut off`; a stream the target refused before processing it is `overload` | `unreachable`; the same |
| `CANCELLED`, `UNKNOWN`, `INTERNAL`, `DATA_LOSS`, `ABORTED` | `failure`; an OK with no reply, as grpc-go sees it, is a `bad response` | `cut off`; a reply that did not decompress is a `bad response` | `client error` |
| `DEADLINE_EXCEEDED` | timeout | timeout | timeout, not sent |

A request too large is recognised by grpc-go's error text. A target on another implementation
(Envoy, Java) words it otherwise, and its refusal lands on `overload` instead of `request error`.

`UNAVAILABLE` on a refused stream is grpc-go's translation of the target's RST_STREAM
REFUSED_STREAM: the client set the code, the target sent the refusal, so it is in the "sent by the
target" line. The target did not process such a stream (RFC 9113 §8.7).

Below the report, failed calls are broken down by gRPC code on two lines. "sent by the target" —
the status came over the wire, from the target or a proxy. "set by the client" — the client set the
code itself: nobody answered, the stream was reset, our deadline ran out, or the client refused the
reply. `DEADLINE_EXCEEDED` from the target is usually our own deadline: it goes to the target in the
`grpc-timeout` header. The category says whose fault it is, the code says what to look for in the
target's logs.

**Client-side waits.** The latency includes everything a call waited on our side: the generator
running late, waiting for the connection, waiting for a free stream. If without them the p99 of at
least one method drops by 10% or more, or some calls never went out, the report gives a verdict,
names the cause most common in the p99 tail, and prints p99 without the waits — the time the call
spent at the target. If the cause is that streams ran out, the target's capacity above "limit ×
connections" is not measured. If a call waited for the resolver first, the caller's interceptors
land in the connection wait too, so p99 without waits may come out slightly low. Calls that never
went out are split by reason. A shift under 10% is not a verdict but a note with the number; when
p99 is a lower bound the shift is unknown and there is no note.

**Clock.** The tool measures the host's clock step before and after the run. A noticeable step
prints a line `clock step 502us on this host: every latency and wait is +/- 502us`. On Linux the
step is tens of nanoseconds, and there is no line.

### Run passport

The report opens with a passport: which machine and which config the run was made with, so that two
reports can be compared without guessing what differs. A run and a breaking-point search both have
it.

```text
machine: linux/amd64, 8 CPU, GOMAXPROCS 8, quota 4, go1.27.1; config sha256:3f9a1c0d2b7e
labels: commit=abc123, env=staging
```

- `quota` is the container's CPU limit (cgroup) as set; without a limit there is no such part.
- The hash is the SHA-256 of the config file as it lies on disk, the same as `sha256sum load.yaml`;
  the text shows the first 12 digits, the JSON all of them. Any edit of the file, a comment
  included, changes it. It is taken before `${ENV}` is substituted: put secrets in `metadata`
  through `${ENV}`, not as values in the file.
- Labels are `labels:` in the config and `-label key=value` on the command line (repeatable; the
  flag wins over the same key in the config):

```sh
leettest -c load.yaml -label env=staging -label commit=abc123
```

A key is Latin letters, digits and `_.-`, up to 64 characters; a value is a string of up to 256
bytes, and in YAML a number goes in quotes (`build: "1.10"`). A violation is exit `1` before the
start. In the JSON the passport is the `passport` field, and a dataset has `sha256` next to `file`;
every field is in the [reference](reference.md#json-fields).

## When a run is invalid

There is a report, but its numbers are not about the load on the target (exit 2), if:

- **it hit the `-max-in-flight` cap.** Timeouts and the cap are checked before the start so that a
  hung target does not hit it; hitting it means the generator ran short of CPU.
- **every measured call of a method is a `request error`, a `client error` or a `bad response`.**
  The note names the method, the cause and what to do.
- **the host clock is coarser than a quarter of the p50** of some method, or the step grew during
  the run to 1 µs or more (`clock_step`). Measure from Linux.
- **a `pass` limit cannot be decided** (`thresholds_undecided`): too few observations, the
  generator was late, calls waited for a free stream, a tail of calls with no answer. The
  `Thresholds:` block names the limit and the reason.

## Pass limits: a run as a check

Without `pass`, a run that reaches its end returns `0` however the target answered. To have the run
say "holds / does not hold" itself, set limits under the call:

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
      pass:
        p99: 200ms
        errors: 1%
```

```sh
leettest -c pass.yaml
echo $?
```

The report ends with a block: the worst verdict and a row per limit. For a healthy service:

```
Thresholds: passed
  wallet.v1.WalletService/GetBalance  p99     <= 200ms  pass  21.2ms
  wallet.v1.WalletService/GetBalance  errors  <= 1%     pass  0.0% (0 of 44000)
```

The keys are `p50`, `p90`, `p95`, `p99`, `p999` and `errors`; a latency limit needs `errors` next
to it, and `errors: 100%` switches the failure check off, which makes the latency limits worth
nothing. A breaking-point search's limit is `breakpoint.pass.held_rps`. A missed limit gives exit
code `4`; a limit that cannot be decided (too few observations, a late generator, a tail of calls
with no answer) gives `2`. All keys, verdicts and reasons are in the
[reference](reference.md#pass-limits); an example with every key is
[`examples/pass.yaml`](../../examples/pass.yaml).

## Exit codes

Without `pass`, a run that reaches its end returns `0` however the target answered: **`0` does not
mean "the service is healthy"**. With `pass`, `0` means every limit was checked and held. The codes
are ranked: an invalid run (`2`) outranks an incomplete one (`3`); a stop that did print a report
is always `3`; a finished run that missed a limit is `4`, and limits never turn an invalid or
stopped run into `4`.

| Code | What happened |
|---|---|
| `0` | The plan ran, the report is complete; with `pass`, every limit held |
| `1` | The report you asked for was not produced: the run did not happen (flags, config, connection) or the report could not be written to `-json-file`. stderr says why |
| `2` | The run is invalid (see above), or a `pass` limit cannot be decided. There is a report, but its numbers are not about the load or do not answer the limit's question |
| `3` | The run stopped before its plan. There is a report, and it covers only what got through |
| `4` | A finished run missed a `pass` limit. There is a report, and `outcome` in the JSON is `complete` |
| `130` | Aborted without a report after Ctrl+C |
| `143` | Aborted without a report after SIGTERM |

How stopping works is in the [reference](reference.md#stopping).

## JSON for scripts and CI

With `-output json`, stdout gets exactly one JSON object and a newline; everything else goes to
stderr, so stdout can go straight into `jq`. The object is printed only for a run that happened
(codes `0`, `2`, `3`, `4`); on `1`, `130` and `143` stdout is empty. The `outcome` field
(`complete`, `invalid` or `incomplete`) matches exit code `0`, `2`, `3`; code `4` is `complete`, and
the `thresholds` field says which limit was missed. The one exception: if the report could not be
written to the file, the code is `1` and stdout still holds the run's object.

For the text in the log and the JSON for the next step from one run, use `-json-file`:

```sh
leettest -c load.yaml -json-file report.json
```

Stdout is whatever `-output` says (text by default); `report.json` holds the same object
`-output json` prints. The file is created before the start and never overwritten: if it exists
or its directory does not, the code is `1` and the target gets no call. A run with no report (for
example `1` on the connection, or `130`) leaves no file. If the write after the run fails, stderr
names the path and the error, and the code is `1`.

The schema is versioned by `schema_version`, now `1`, and it is a contract; the screen text is not:
parse the JSON, not the screen. The rules:

- a new field is added without a version change; renaming or removing a field, or changing its
  type, raises `schema_version`;
- the values of an enum (`outcome`, `unchecked[].reason`, the codes in `failure_codes`) may grow
  without a version change;
- a consumer must skip unknown fields and handle an unknown enum value without failing.

The unit is in the field's name: `_us` is whole microseconds, `_s` whole seconds; the rate `rps` is
the only fractional number. A value the run did not produce is `null`, not `0`. A percentile is an
object `{"us": 1234, "ns": 1234567, "lower_bound": false}`: `ns` is the histogram's value in whole
nanoseconds and `us` the same value truncated to microseconds; `pass:` and `leettest compare` judge by `ns`,
and for a report written before that field takes `us` × 1000. With `lower_bound: true` it is a
lower bound, not a value. Latencies are the histogram's values at 3 significant figures (an error of up to 0.1%),
counts are exact. Times count from `started_at` (RFC 3339, UTC), the start of the schedule, warm-up
included; `duration_us` includes warm-up too. `in_flight` in a second is how many calls were in
flight at its end. The codes in `failure_codes` are canonical names (`UNAVAILABLE`). To reconcile: the run's
totals are the sum of the methods', and over a method's seconds `Σ begun` plus `outside_timeline`
is every call of the method, warm-up included.

Decisions are fields, not text: `invalid_reasons` (`in_flight_cap`, `nothing_measured`,
`clock_step`), `methods[].invalid_reason` (`request_error`, `client_error`, `bad_response`, `mixed`
or `null`), `tail_wait_cause` (`generator`, `stream`, `connection` or `null`) and the numbers per
cause in `client_waits`. `notes` and `unchecked[].error` are text for people, reworded freely: do
not parse them.

The target's silence: `methods[].silent_from_s` is the second from which the target answered none
of the calls sent, `methods[].silent_sent_rps` how many calls went out in the second before it,
`methods[].silent_planned_rps_low` and `_high` the planned rate of the stages in that second.
Without a silence all are `null`; the planned rate is `null` also when no stage ran in that second.
`planned_rps_low` and `_high` are the whole plan's rate.

The text report is printed in ASCII only. Characters from outside ASCII, in a method name or in
the target's error text, are printed as `\uXXXX` (past U+FFFF as a surrogate pair, as in JSON).
`notes` in JSON carry the same escaped text.

### Breaking-point search

A `load.breakpoint` section instead of the call's `rps` and `duration`: LeetTest raises the load in
steps and names the step the target held and the step it broke at. Exactly one call, without `rps`,
`duration` or `load.warmup`.

```yaml
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      timeout: 500ms
  breakpoint:
    from: 100         # first step, rps
    to: 2000          # never above it
    factor: 1.25      # next step = last × factor; or step: 100 — that many rps more
    settle: 5s        # the start of a step, out of its verdict; under half the hold
    hold: 30s         # a step's length
    p99_limit: 200ms  # optional; without it a step breaks at p99 over 3× the best held one
    errors_limit: 1%  # optional; a step breaks when more than this share of calls failed
```

A step breaks when more than `errors_limit` of its calls failed (1% by default; exactly that much
is still held, `100%` means errors do not break) or its p99 crossed the line. A call not sent for
the connection breaks the step at any `errors_limit`. Failures on the client side (`request_error`,
`client_error`, `bad_response`) do not prove the target broke: when the limit is crossed only with
them, the search stops as `run_limit` with the cause `client_errors` instead of naming a breaking
point; `held_rps` stays. A broken step is
repeated after a pause and, except the first, after probes at the first step: the target must come
back to its old p99. A step counts as held only if the generator sent what was planned — at least
99.9% of the measured window's calls. Otherwise it is the run's limit, not the target's. Before the
first step stderr has `breakpoint: up to N steps, at most T`: how many steps and how long the search
takes at worst. One connection for the whole search; if the target drops it, the step is broken,
with no reconnect.

The result (`outcome` in JSON) is a closed list: `broke` (held X, broke at Y), `broke_at_first`
(broke at the first step — start lower), `held_all` (held every step up to `to`), `run_limit` (the
generator, a connection's limit or `-max-in-flight` ran out: nothing is known about the target
above), `stopped` (Ctrl+C), `invalid` (a step was an invalid run). The cause (`why`) is a closed
list too: `errors`, `p99_limit`, `p99_vs_base`, `connection`, `no_recovery`, `generator`,
`in_flight_cap`, `stream_limit`, `stream_wait`, `client_errors`, `clock_step`, `request_errors` or
`null`. The error limit in force is in `breakpoint.errors_limit`, a string (`"1%"`).
`held_rps` and `broke_rps` are `null` where there is none. In search mode the JSON is another
object: `mode: "breakpoint"`, none of a plain run's top-level fields, each run with its full report
in `breakpoint.runs` (`kind`: `step`, `repeat`, `probe`; `planned_rps` and `sent_rps`). A plain run
writes `mode: "run"`.

Exit codes: `0` for `broke`, `broke_at_first`, `held_all`, `run_limit` — they are findings; `2` for
`invalid`; `3` for a stop by one Ctrl+C; `4` when the search held less than `breakpoint.pass.held_rps`.

### Compare two searches

Two search reports written with `-json-file`, from before a change and after it.
`leettest compare` says how far the capacity (`held_rps`) fell and, with
`-max-capacity-drop`, judges it. On the reference stand, from the repository root: a search against a
stand of 270 rps capacity (in the stand's terminal), then, in another terminal, the search and the
"before" report:

```sh
go run ./test/stand/cmd/stand -capacity 270 -delay 20ms
```

```sh
leettest -c examples/breakpoint-step.yaml -json-file before.json
```

Stop the stand (Ctrl+C), start it with 200 rps capacity and take the "after" report:

```sh
go run ./test/stand/cmd/stand -capacity 200 -delay 20ms
```

```sh
leettest -c examples/breakpoint-step.yaml -json-file after.json
```

Compare (the stand can be stopped):

```sh
leettest compare -max-capacity-drop 10% before.json after.json
```

```
base: before.json  Health  2026-10-19T19:25:28.941479Z
new:  after.json  Health  2026-10-19T19:26:46.1806268Z
capacity: 250 rps -> 200 rps, drop 20.0%, limit 10%: FAIL
```

The capacity fell from 250 to 200 rps, 20% against a limit of 10%, exit code `4`. Your times and
stand differ; the numbers follow the stand's capacity.

Reports are comparable only if the target, method, LeetTest version, run passport (except labels)
and dataset match; otherwise `differs:` lines name the field and the verdict is `unknown`. When a
search ended before it broke, its capacity is a lower bound (`>= N rps`); when the target broke at
the first step, an upper bound (`below N rps`). The drop is then printed as a bound
(`drop <= 33.4%`), and the limit is decided only if the bound decides it. Without
`-max-capacity-drop` nothing is judged. Exit codes: `0` within the limit or no limit, `1` a report
that could not be read, `2` reports that cannot be compared or a limit that cannot be decided, `4`
the capacity fell past the limit.

### Compare two runs

The same command reads plain-run reports (`-json-file`): `-max-p99-rise` judges the
rise of each method's p99 in percent of the "before" p99, `-max-errors-rise` the rise of the share of
failed calls in percentage points. Without a limit the line is printed but not judged. A search
report and a run report in one command is exit `1`. On the reference stand, from the repository root:
a run at 20 ms delay, then at 30 ms:

```sh
go run ./test/stand/cmd/stand -delay 20ms -life 60s
```

```sh
leettest -c examples/stand.yaml -json-file before.json
```

The stand exits by itself after a minute; start it with a 30 ms delay and take the "after" report:

```sh
go run ./test/stand/cmd/stand -delay 30ms -life 60s
```

```sh
leettest -c examples/stand.yaml -json-file after.json
```

Compare (the stand can be stopped):

```sh
leettest compare -max-p99-rise 20% before.json after.json
```

```
base: before.json  Health  2026-10-20T12:40:09.5222467Z
new:  after.json  Health  2026-10-20T12:40:42.469917Z
grpc.health.v1.Health/Check: p99 21.2ms -> 31.3ms, rise 47.3%, limit 20%: FAIL
grpc.health.v1.Health/Check: errors 0.0% -> 0.0%, no change
```

The p99 rose from 21.2 to 31.3 ms, 47.3% against a limit of 20%, exit code `4`. Your times and
delays differ.

A check can be `unknown` (exit `2`) when the reports cannot be compared (as above), a run is invalid
or incomplete, there are too few observations, a p99 ran into the timeout (so it is larger than
printed, `>=`), or the client's own errors leave it open whether the service is at fault. Methods
that are not in both reports are not compared. Exit codes are the same as for searches.

For a script or CI the same result comes as one JSON object: `-output json` prints
only it to stdout, the exit codes and stderr are those of the text; on exit code `1` stdout is empty.

```sh
leettest compare -max-p99-rise 20% -output json before.json after.json
```

In the object `verdict` is the worst verdict of the checks that have a limit (`null` when there is
none), and `checks` holds every check with the numbers of both reports; for an `unknown` the cause
is in `reason`, a value from a closed list:

```json
{"schema_version":1,"mode":"compare","compared_mode":"run","comparable":true,"differences":[],"verdict":"fail","checks":[{"method":"/grpc.health.v1.Health/Check","key":"p99","base_p99":{"us":21381,"ns":21381472,"lower_bound":false},"new_p99":{"us":31358,"ns":31358109,"lower_bound":false},"limit_percent":"20%","verdict":"fail","reason":null,"why":null}]}
```

Not every field is shown: the ones empty for this check (`base_capacity` and the other `null`s) and
the description of the reports (`base`, `new`) are left out. The object has no percentage of rise —
a script computes it from `us`. Every field is described in the
[reference, «Compare JSON»](reference.md#compare-json).

## Going deeper

We have a roadmap for the product — **[roadmap](roadmap.md)**. Missing something —
[open an issue](https://github.com/yhgrwav/leettest/issues/new/choose).

**[Reference →](reference.md)** — every config field and flag.

| Question | |
|---|---|
| What questions does LeetTest answer? | [Read](questions.md) |
| What problem does LeetTest solve? | [Read](problem.md) |
| Why this tool? | [Read](why.md) |
| Which load-testing problems does it fix? | [Read](pitfalls.md) |
| What is call chaining and why does it matter? | [Read](chaining.md) |
| What does commercial use look like? | [Read](commercial.md) |
| Where do I ask a question or leave feedback? | [Read](feedback.md) |
| What comes next? | [Read](roadmap.md) |

**[Comparison with ghz on one stand →](compare-ghz.md)** — tables and the command to reproduce.

## Contributing

Issues and discussions are welcome. Pull requests are accepted after signing the [CLA](../../CLA.md)
— one line as a comment in the PR, checked automatically. How to start — in
[CONTRIBUTING.md](../../CONTRIBUTING.md): a path for people and a path with an AI agent, and for
the agent, [AGENTS.md](../../AGENTS.md). The CLA is a license, not a transfer of rights: the
copyright stays with you.

## License

Apache License 2.0 — see [LICENSE](../../LICENSE).
