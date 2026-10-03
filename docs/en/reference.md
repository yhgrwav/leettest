# <img src="../../assets/icon.png" width="24" alt="" align="top"> Reference

[Русский](../ru/reference.md) · [English](reference.md)
[← Home](README.md)

> Translated from [docs/ru/reference.md](../ru/reference.md) at ae9a826, 2026-10-03. If they differ, the
> Russian one is right.

Every config field, every flag, and what the tool checks before the start. How to read the report
is in the [README](README.md#reading-the-report).

## Config

```yaml
name: wallet                     # optional
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
      timeout: 2s
```

| Field | What it does |
|---|---|
| `name` | Optional. What to call the run in the header. Left out — the service name if every method belongs to one service, otherwise the config file name |
| `app.target` | The service address: `ip` and `port` |
| `app.tls` | TLS. Left out — on. `false` — an unencrypted connection |
| `app.ca` | A PEM file of certificates to verify the service with instead of the system ones. Needs TLS |
| `app.cert`, `app.key` | A client certificate and its key in PEM, for a service with mTLS. Only together, needs TLS |
| `app.server_name` | The name to check the service's certificate against when it does not name the address in `target`. Needs TLS |
| `app.metadata` | Headers of every call: `authorization`, `x-api-key` and so on. `${NAME}` is taken from an environment variable |
| `app.max_response_size` | The largest reply a call accepts: `16MiB`, `512KB`. The unit is required (`MB` = 10⁶ bytes, `MiB` = 2²⁰), below 2 GiB. Left out — 4 MiB, as in gRPC. A larger reply is a `bad response`. A call in flight may buffer up to twice the limit: by default up to 8 MiB each |
| `load.warmup` | The first N seconds stay out of the percentiles and of `sent`: cold caches spoil them. Warm-up calls do reach the target; the report prints them on a line `warm-up N sent (M failed), excluded from stats` — `sent` plus that line is every call the generator attempted. The target got all of them except those counted as unreachable or client error; `cut off` and timed-out ones may not have fully reached it: a target that does not open its HTTP/2 window (flow control) gets the headers only, and its counters may not see the call. Counts toward `duration`, shorter than any call |
| `load.calls[].method` | The full method name: `package.Service/Method` |
| `load.calls[].rps` | Requests per second for this method |
| `load.calls[].duration` | How long to load it: `30s`, `5m`, `1h` |
| `load.calls[].timeout` | How long to wait for a reply. Left out — `2s`. Zero does not turn it off, it is an error |
| `load.calls[].data` | The request body, see [below](#request-body). Left out — an empty message |

The config is read strictly: a typo in a field name is an error with the line number, a wrong
value is an error with the call's number and method, not a run with an empty load. `rps` is a
whole number: `10.5` is refused, not silently rounded to ten. `warmup` must be shorter than every
call, or not a single measured request would be left of that call.

**Timeout and the in-flight cap.** If the service hangs, each method holds `rps × timeout`
requests in flight until the timeout fires, plus a margin: one request at the window's edge and
`rps × 100 ms` for the generator freeing a slot a little after the deadline. The sum over methods
must not exceed `-max-in-flight` (5000 by default). This is checked before the start, and the
error names both ways out: which timeout would fit and which cap is needed. So a hung service does
not hit the cap: the run reaches its end, and the report says how many calls got no reply within
the timeout and after which sent call the target answered no more. If the cap does run out, slots
were held past their deadline by more than 100 ms — that is the generator (not enough CPU) or the
sender, and the report declares the run invalid. Next to it the report prints how many slots were
held past their deadline at that moment.

## Access to the service

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
```

**Secrets.** `${NAME}` is filled from an environment variable, inside a value too:
`Bearer ${TOKEN}`. A variable that is not set or set empty is an error before the start, naming
it. Otherwise `Bearer ` would go out without a token, and the run would show 100% failures as the
target's fault. To write `${` literally, double the dollar: `$${`. A single `$` stays as written.
Substitution works only in `app.metadata`: in `data`, `target` and every other field `${NAME}`
goes out as written. There is no flag for headers, so the token never lands in `ps` or in the
shell history. The tool prints header values nowhere: not in the report, not during the run, not
in errors.

**Headers.** Names are lowercased, as HTTP/2 carries them anyway. A config error before the
start: a key starting with `grpc-` (reserved by gRPC), pseudo-headers such as `:path`, a key
ending in `-bin` (binary headers are not supported yet), a value outside printable ASCII. The same
headers and certificate go into the method check before the start. If the target answered it
with `Unauthenticated` while headers are set, the run does not start: "target rejected
credentials". `PermissionDenied` does not stop the run: the token was accepted and may work for
calls while not letting it into reflection; the methods are marked unchecked. `Unauthenticated`
without headers does not stop it either, and a note says: "target requires credentials;
app.metadata is not set".

**Certificates.** `ca` replaces the system roots rather than adding to them. Checking the
target's certificate cannot be turned off by anything. `server_name` changes only the name the
certificate is checked against and that goes into SNI; `:authority` stays the address from
`target`. A password-protected key is not supported: decrypt it beforehand.

**One connection.** The generator holds one connection to one address. If DNS returns several
addresses, only one backend is loaded, and the report will not show it. Calls are not retried.
The service's service config is ignored: retries and the balancing policy from it are not
applied. A production client that applies them will see other categories and p99.

**Errors before the start.** Everything that can be known before the run means exit code 1 and not
a single call to the target: a file not found or not PEM, a certificate that does not match its
key, the target's certificate expired or not naming the address (hint: `server_name`), the target
closing the connection right after the handshake (it did not accept the client certificate), TLS
on while the target has none, or the other way round.

## Request body

```yaml
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      data:
        wallet_id: "w-123"
        currency: USD                         # enum — by name
        filter:
          since: "2026-01-01T00:00:00Z"       # google.protobuf.Timestamp
```

`data` is plain YAML shaped like the message. The tool takes the schema from the service through
server reflection; no `.proto` is needed. The body is built once before the start, and any error
in it — an unknown field, a wrong type, a method that does not exist — shows at once, with the
method and field names, before the first request. No `data` — an empty message goes out, and such
a method needs no reflection.

**One body per method.** Every call of a method goes out with the same `data`. For a write with an
idempotency key this means the first call creates the record and every later one takes the repeat
path: the target returns the answer it already stored. That path is what gets measured, not
creating the record.

Value rules are the standard JSON rules for protobuf (`protojson`):

- a field name as in the `.proto` (`wallet_id`) or in its JSON form (`walletId`);
- an enum by name; `Timestamp`, `Duration` as a string in their format;
- `int64` and `uint64` arrive exactly, above 2^53 too (snowflake IDs, amounts in minor units): the
  number never passes through a float;
- a number with a leading zero (`0123`, `007`) is a config error: YAML would read it as octal.
  Need the zero — write it as a string, `"0123"`;
- **`bytes` as a base64 string.** `signature: abcd` is not the four bytes `abcd` but three other
  bytes: `abcd` is itself valid base64, and there will be no error. The four bytes `abcd` are
  written as `signature: YWJjZA==`.

**Method check.** Every method in the config is checked against the service before the start: a
typo in the name or a method the service does not have is an error before the first request, not
a run in which every call fails with `Unimplemented`. If reflection is off on the service, a method
with `data` will not start — the error says so plainly. A method without `data` works without
reflection too: there is nothing to check it with, and a warning says so — before the run on
stderr and once more as a line in the report. The warning says why the check was not possible:
reflection is off, it refused, or it did not answer at all.

## Flags

| Flag | What it does |
|---|---|
| `-c` | Path to the config |
| `-output` | Report format on stdout: `text` (the default) or `json` for scripts and CI |
| `-connect-timeout` | How long to wait for a service that accepted the connection but stays silent. Default `10s`. A refused connection and a wrong address are not waited for |
| `-max-in-flight` | Cap on requests waiting for a reply. Default `5000` |
| `-fake` | Load a built-in stub instead of the service from the config — to look at the tool without a service. The report is marked `fake target` |
| `-fake-delay`, `-fake-jitter`, `-fake-fail-ratio` | The stub's behavior. Only together with `-fake` |
| `-version` | Print the version and exit. A build from source prints the commit |

## Stopping

`q` in the full-screen view, Ctrl+C without it:

- the first press — no new requests go out, requests in flight run to their timeout and land in
  the report as usual;
- the second — requests in flight are cut off and counted separately (`aborted`): not a service
  failure but a lower bound of the time; the report is printed;
- the third — exit at once, without a report.

In the full-screen view the top line walks through these steps.

SIGTERM (`docker stop`, Kubernetes, a cancelled CI job) cuts off the requests in flight at once
and prints the report, skipping the gentle stop: the orchestrator kills the process a few seconds
later, and the report must make it out. If a cut-off is already under way, SIGTERM does not
interrupt it.

A stopped run is marked incomplete in the report (exit 3): its numbers are honest but cover less
than was planned.

## JSON fields

The contract's rules — schema version, units, `null` rather than `0`, unknown fields — are in the
[README](README.md#json-for-scripts-and-ci). Here is every field of `schema_version` 1. A type with
`?` may be `null`. Warm-up counters are in no other counter; `sent` and all its shares are
measured calls only.

**Percentile** — an object `{"us", "lower_bound"}`: `us` is whole microseconds, `lower_bound: true`
means a lower bound, not a value ([README](README.md#reading-the-report)). `null` — not a single
observation.

### Run

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | int | Schema version, now `1` |
| `leettest_version` | string | The tool's version; a build from source gives the commit |
| `target` | string | The target address from the config, `fake target` with `-fake` |
| `outcome` | string | `complete`, `invalid`, `incomplete` — matches exit code `0`, `2`, `3` |
| `invalid_reasons` | []string | Why the run is invalid: `in_flight_cap`, `nothing_measured`, `clock_step`. Empty — valid |
| `tail_wait_cause` | string? | The client-side wait that set the tail: `generator`, `stream`, `connection`; `null` — no verdict |
| `clock_step_ns` | int | The host clock step, ns: every latency and wait is ± it |
| `started_at` | string | Start of the schedule, RFC 3339 UTC, warm-up included. Every `_us` and `_s` counts from it |
| `duration_us` | int | How long the run went, warm-up included |
| `planned_us` | int | How long it was meant to go |
| `warmup_us` | int | Warm-up length |
| `sent` | int | Measured calls, except `not_sent`. `unreachable` and `aborted` are in it |
| `failed` | int | Of `sent` — every call that did not succeed, except `aborted` |
| `aborted` | int | Cut off by our own stop |
| `not_sent` | int | The timeout ran out before sending: in neither `sent` nor `failed` |
| `not_sent_generator`, `not_sent_stream`, `not_sent_connection` | int | `not_sent` by cause: the generator was late, no free stream, no ready connection. They add up to `not_sent` |
| `warmup_sent`, `warmup_failed`, `warmup_not_sent` | int | The same for calls scheduled in the warm-up |
| `cap_hit` | object? | The `-max-in-flight` cap was hit; `null` — it was not |
| `cap_hit.at_us` | int | When |
| `cap_hit.unsent` | int | Calls the cap refused |
| `cap_hit.over_deadline` | int | Calls in flight at that moment holding their slot past their deadline |
| `start_lag` | object | How late calls started against the schedule |
| `start_lag.p99` | percentile? | p99 of the lag |
| `start_lag.max` | percentile? | The maximum, always exact; `null` without calls |
| `connections` | object? | Connections; `null` when the sender does not report them |
| `connections.open` | int | How many connections carried calls at once |
| `connections.reconnects` | int | Successful handshakes after the first |
| `connections.first_limit`, `connections.last_limit` | int? | `MAX_CONCURRENT_STREAMS` at the first and last handshake; `null` — not announced (`0` — announced zero) |
| `connections.limit_changes` | int | Handshakes that announced a limit different from the one before |
| `client_waits` | object | Calls that waited on the client side over the floor, by cause ([README](README.md#reading-the-report)) |
| `client_waits.generator_calls`, `stream_calls`, `connection_calls` | int | All such calls, sent or not. A sent call can count for several causes |
| `client_waits.generator_tail_calls`, `stream_tail_calls`, `connection_tail_calls` | int | Only among the p99 tail and the unsent: these pick `tail_wait_cause` |
| `methods` | []object | Methods, see below |
| `unchecked` | []object | Methods that could not be checked through reflection |
| `unchecked[].method` | string | The method |
| `unchecked[].reason` | string | `reflection_off` or `reflection_failed` |
| `unchecked[].error` | string | Text for people, changes with grpc-go |
| `notes` | []string | The report's notes, text for people in ASCII; do not parse |

### Method: `methods[]`

| Field | Type | Meaning |
|---|---|---|
| `method` | string | The full method name |
| `invalid_reason` | string? | The method measured nothing about load: `request_error`, `client_error`, `bad_response`, `mixed`; `null` — it did |
| `sent`, `failed`, `aborted`, `not_sent`, `not_sent_generator`, `not_sent_stream`, `not_sent_connection`, `warmup_sent`, `warmup_failed`, `warmup_not_sent` | int | The method's share of the run's fields of the same name |
| `rps` | float? | `sent/s`: calls sent divided by the sending time; `null` — no calls |
| `timeout_us` | int | The method's timeout |
| `planned_rps_low`, `planned_rps_high` | int | Lowest and highest planned rate of the whole plan |
| `latency` | object | `min`, `p50`, `p90`, `p95`, `p99`, `max` — percentiles? of successful calls, with timeouts and aborted calls as lower bounds |
| `p99_without_client_waits` | percentile? | p99 of the same calls without client-side waits; may come out slightly low |
| `observations` | int | Observations behind the `latency` percentiles |
| `censored` | int | Of them, only a lower bound is known |
| `invalid_latencies` | int | Rejected as impossible (a negative latency) — a sign of a bug in the code, not in the target |
| `timed_out` | int | Went out and got no answer within the timeout |
| `timed_out_after_wait` | int | Of `timed_out` — went out with less than half the timeout left |
| `cut_off` | int | Went out and got no status |
| `unreachable` | int | Never reached the target |
| `unclassified` | int | The sender gave no category — a sender defect, not an observation of the target |
| `outside_timeline` | int | Left off `seconds`: a moment of the call did not fit the timeline |
| `request_error`, `overload`, `failure`, `bad_response` | object | The category ([README](README.md#reading-the-report)): `count` and percentiles? `p50`, `p90`, `p95`, `p99`, `max` of its answer time |
| `client_error` | int | The client refused to send |
| `failure_codes` | []object | Failed calls by gRPC code |
| `failure_codes[].code` | string | The canonical code name (`UNAVAILABLE`); the list may grow |
| `failure_codes[].count` | int | How many |
| `failure_codes[].from_target` | bool | `true` — the status came over the wire, `false` — the client set the code |
| `silent_from_s` | int? | The second from which the target answered none of the calls sent; `null` — no silence |
| `silent_sent_rps` | int? | Calls sent in the second before it (in the first, if the silence starts there) |
| `silent_planned_rps_low`, `silent_planned_rps_high` | int? | The planned rate of the stages in that second; `null` also when no stage ran in it |
| `last_answer_at_us` | int? | When the last call the target answered went out; `null` — it never answered |
| `seconds` | []object | The per-second timeline, see below |

### Second: `methods[].seconds[]`

The timeline covers the whole run, warm-up included, up to the last second anything happened in.
Unlike the totals it keeps every call. To reconcile: `Σ begun` + `outside_timeline` is every call
of the method.

| Field | Type | Meaning |
|---|---|---|
| `warmup` | bool | The second is in the warm-up |
| `begun` | int | Calls begun in this second |
| `succeeded`, `overload`, `failure`, `client_error`, `bad_response`, `request_error`, `timed_out`, `unreachable`, `cut_off`, `aborted`, `unclassified` | int | Calls that finished in this second, by category |
| `not_sent_generator`, `not_sent_stream`, `not_sent_connection` | int | Timeouts before sending that finished in this second, by cause |
| `in_flight` | int | Calls in flight at the end of the second |
| `lag_calls`, `lag_sum_us`, `lag_max_us` | int | Over calls scheduled in this second: how many, sum and maximum of the start lag |
| `observed_calls` | int | Of them, the successful ones |
| `observed_lag_sum_us`, `transport_wait_sum_us`, `service_time_sum_us` | int | Over the successful ones: sum of start lag, of the wait before sending (connection and stream), of the time from sending to the answer. Together — their latencies |
