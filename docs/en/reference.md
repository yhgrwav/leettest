# <img src="../../assets/icon.png" width="24" alt="" align="top"> Reference

[Русский](../ru/reference.md) · [English](reference.md) · [Deutsch](../de/reference.md) · [简体中文](../zh-CN/reference.md)
[← Home](README.md)

> Translated from [docs/ru/reference.md](../ru/reference.md) at b7a685c, 2026-10-12. If they differ, the
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
| `labels` | Optional. Pairs `key: "value"` the report's passport prints, for telling runs apart (`env: staging`, `commit: abc123`). A key is 1 to 64 characters of `A-Za-z0-9_.-`; a value is a YAML string of up to 256 bytes, so quote a number (`"1.10"`). `-label` adds a pair or replaces the config's |
| `app.target` | The service address: `ip` and `port` |
| `app.tls` | TLS. Left out — on. `false` — an unencrypted connection |
| `app.ca` | A PEM file of certificates to verify the service with instead of the system ones. Needs TLS |
| `app.cert`, `app.key` | A client certificate and its key in PEM, for a service with mTLS. Only together, needs TLS |
| `app.server_name` | The name to check the service's certificate against when it does not name the address in `target`. Needs TLS |
| `app.metadata` | Headers of every call: `authorization`, `x-api-key` and so on. `${NAME}` is taken from an environment variable |
| `app.max_response_size` | The largest reply a call accepts: `16MiB`, `512KB`. The unit is required (`MB` = 10⁶ bytes, `MiB` = 2²⁰), below 2 GiB. Left out — 4 MiB, as in gRPC. A larger reply is a `bad response`. A call in flight may buffer up to twice the limit: by default up to 8 MiB each |
| `app.connections` | How many connections to open to the target, a whole number from 1 to 256. Left out — 1. Calls take the connections in turn, and the report prints a `Connections:` block, a row per connection |
| `app.compression` | Compress request bodies: `gzip`. Left out — none. The report prints `bytes: <raw> -> <wire> per call` under the method |
| `app.proto.files` | The `.proto` files whose services to load, for a service without reflection. Paths are relative to the config file's directory. Each file must lie under one of `import_paths` |
| `app.proto.import_paths` | Where to look for the files and their imports, as `protoc -I`. Paths are relative to the config file's directory. Left out — that directory. The well-known types (`google/protobuf/…`) need no path |
| `app.protoset` | A descriptor set file instead of `.proto` files, built with `protoc --include_imports --descriptor_set_out=FILE`. The path is relative to the config file's directory. Not together with `app.proto`; neither of them — methods come from reflection |
| `load.warmup` | The first N seconds stay out of the percentiles and of `sent`: cold caches spoil them. Warm-up calls do reach the target; the report prints them on a line `warm-up N sent (M failed), excluded from stats` — `sent` plus that line is every call the generator attempted. The target got all of them except those counted as unreachable or client error; `cut off` and timed-out ones may not have fully reached it: a target that does not open its HTTP/2 window (flow control) gets the headers only, and its counters may not see the call. Counts toward `duration`, shorter than any call |
| `load.ramp` | Raise every call from 0 to its `rps`, linearly, over this long before its `duration` starts: the run lasts `ramp` + `duration`, and `duration` keeps its meaning (time at `rps`). Left out or `0s` — none. The ramp's calls stay out of the percentiles and of `sent`, like the warm-up's, which starts when the ramp ends; the report prints both on one line, `ramp and warm-up N sent, excluded from stats`. The limits in `pass` and the error share count the hold only: a target that fails during the ramp and recovers passes, and only that line shows it. The ramp begins with one call at moment 0. Not together with `load.breakpoint`. (not released) |
| `load.calls[].method` | The full method name: `package.Service/Method` |
| `load.calls[].rps` | Requests per second for this method |
| `load.calls[].duration` | How long to load it: `30s`, `5m`, `1h` |
| `load.calls[].timeout` | How long to wait for a reply. Left out — `2s`. Zero does not turn it off, it is an error |
| `load.calls[].data` | The request body, see [below](#request-body). Left out — an empty message |
| `load.calls[].dataset` | A file of request bodies instead of `data`, see [below](#a-different-request-each-call-a-dataset). Not together with `data` |
| `load.calls[].dataset_order` | `file` (left out) or `random`: the order of the dataset's requests. Needs `dataset` |
| `load.calls[].dataset_seed` | A whole number from 0 to 9007199254740991 that fixes a random order. Needs `dataset_order: random`. Left out — one is drawn and the report prints it |
| `load.calls[].dataset_reuse` | `true` (left out) or `false`: with `false` no record goes out twice, in the whole process. Needs `dataset` |
| `load.calls[].pass` | The limits a run must meet to pass: `p50`, `p90`, `p95`, `p99`, `p999`, `errors`, see [below](#pass-limits). Left out — no limits |
| `load.breakpoint.pass` | A breaking-point search's limit: `held_rps`, see [below](#pass-limits). Left out — no limit |
| `load.breakpoint.errors_limit` | A percent such as `1%` or `0.5%`, up to three digits after the point, at most `100%`. A search step breaks when more than this share of its calls failed; exactly that much holds. Left out — `1%` |

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

## Pass limits

`pass` under a call says at what values the run counts as passed:

```yaml
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      pass:
        p99: 200ms
        errors: 1%
```

Every key is in [`examples/pass.yaml`](../../examples/pass.yaml), a search's limit in
[`examples/breakpoint.yaml`](../../examples/breakpoint.yaml).

| Key | Limit |
|---|---|
| `p50`, `p90`, `p95`, `p99`, `p999` | The latency is at most the limit: a duration with a unit (`200ms`), whole microseconds, above zero, below the call's `timeout` and below an hour. Needs `errors` next to it: a target that rejects calls at once would otherwise pass a latency limit. `p999` needs 1000 measured calls: `rps` × (`duration` − `warmup`) |
| `errors` | The share of failed calls is at most the limit: a percent with `%` (`1%`, `0.5%`), up to three digits after the point, at most `100%`. A number without `%` is not guessed. `errors: 100%` switches the failure check off, and then the latency limits are worth nothing |
| `breakpoint.pass.held_rps` | The search must hold at least this many calls a second. A whole number, one of the steps of the search's plan. A call's `pass` together with `load.breakpoint` is refused |

A config with a wrong `pass` is an error before the start (exit code 1), not a run without the
check. An empty `pass: {}` is an error too.

**Each limit gets a verdict**: `pass`, `fail` or `unknown`. The report prints a `Thresholds:` block
with the worst of them and a row per limit; the JSON has the `thresholds` field.

| Result | Exit code |
|---|---|
| Every limit is `pass` | `0` |
| A finished run missed a limit (`fail`) | `4`, `outcome` stays `complete` |
| No `fail`, but a limit cannot be decided (`unknown`) | `2`, `invalid_reasons` has `thresholds_undecided` |
| The run is invalid or stopped | `2` or `3`, as without limits: limits do not change them |

A miss outranks an undecided limit: with a `fail` and an `unknown` together the code is `4`.

A limit is `unknown` when the number cannot be trusted or is missing; the reason is from a closed
list: `incomplete` (the run was stopped), `invalid` (the run is invalid), `few_observations` (the
percentile was not measured), `run_limit` (the search hit the run's own limit), `generator` (the
generator was late), `stream` (calls waited for a free stream), `client_errors` (the client's own
failures decide it, not the target's), `nothing_sent` (not a single call was sent), `censored` (the
tail holds calls with no answer within the timeout: the value is only a lower bound, not above the
limit).

- Latency is compared in nanoseconds, as the JSON carries it (`value.ns`): a value one nanosecond
  over the limit is a `fail`, though `value.us` (cut at the microsecond) may equal `limit_us`. A
  lower bound over the limit is a `fail`: the real value is larger still.
- The share counts every failed call and every call not sent for lack of a connection, over all
  counted. For a `fail` only the failures the target decided count: requests the client rejected,
  client errors and bad responses say nothing of the target.
- A limit's row never contradicts its verdict: if the value rounded to three significant figures
  would land on the wrong side of the printed limit, the exact one is printed. Beside a FAIL it
  is the first of three that is over the limit: the three figures, the value at whole microseconds
  (the nearest; a lower bound rounded down), the value to the nanosecond (`100.4us` against
  `100us`). A share beside a
  FAIL is rounded to the nearest, never up: the first of 1, 2, 3 digits after the point that is
  over the limit (`1.004%` against `1%`); if three are not enough, `> ` and the limit as written
  is printed (`> 1%`).

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
ending in `-bin` (binary headers are not supported yet), `content-type` and `te` (the transport
writes them itself), `connection`, `keep-alive`, `proxy-connection`, `transfer-encoding`,
`upgrade` (not allowed in HTTP/2), a value outside printable ASCII. `user-agent` is allowed: the
target sees one header, your value followed by ` grpc-go/<version>`. The same
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

**Connections.** By default the generator holds one connection to one address. If DNS returns
several addresses or the target is behind an L4 balancer, only one backend is loaded, and the
report will not show it. `app.connections: N` opens N connections. A name that DNS
resolves to M addresses is resolved once before the start, and connection i goes to address i mod M:
three connections over two addresses are two and one. An address that does not serve the target
(`localhost` to `::1` while the server listens on `127.0.0.1` only) stops the start and is named in
the error: use its IP. The report's block shows each connection's address, calls, share failed and
p99, so a dead or slow backend behind a name is not averaged away. Calls are not retried.
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

## A different request each call: a dataset

```yaml
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      dataset: wallets.jsonl
```

```
{"wallet_id": "w-1", "currency": "USD"}
{"wallet_id": "w-2", "currency": "EUR"}
```

`dataset` is a file of request bodies, one JSON per line, in the form `data` takes. A relative
path is read from the config file's directory. Request one goes out with line one, request two
with line two, and after the last line it starts over. Blank lines are skipped. The file is read
and every line is checked against the method's message before the start, with `data`'s rules and
reflection; an error names the line, `wallets.jsonl:3`, and never prints what the line holds. A
file with no requests is an error.

Under the method's row the report says how much of the file was used:
`data: 3 of 3 requests from wallets.jsonl, each used up to 5 times`. That is how often a request
was handed to the sender, not what the target saw: a request that stands twice in the file goes
out twice, and a call that never reached the target is counted too. In a breaking-point search
every step goes on where the one before ended, and the line counts the whole search.

`dataset_order: random` hands the requests out in a random order: every pass through the file is
a new shuffle in which each request goes out once, so the numbers above stay as they are. The
line then ends with `, random order, seed 42`. To repeat a run, put that number in
`dataset_seed` (a whole number from 0 to 9007199254740991): the same seed, file and number of
requests give the same order. Left out, a seed is drawn at the start, one per call.

`dataset_reuse: false` sends each record at most once, per call, in the whole process: warm-up,
every step of a search and every connection draw from one count (two calls on one file are two
counts). The line then reads `data: 3 of 3 requests from wallets.jsonl, each at most once`. A run
the file cannot cover is refused before anything is sent, with both numbers. A search that needs
a record the file no longer has stops like a Ctrl+C (exit code 3): the run in progress is not
judged and the report says which file ran out and at which rate. One process only: records are
not split between machines, and a stop leaves records unsent.

## Flags

| Flag | What it does |
|---|---|
| `-c` | Path to the config |
| `-output` | Report format on stdout: `text` (the default) or `json` for scripts and CI |
| `-json-file` | Path of a new file: the JSON report (the same object `-output json` prints) is written to it, and stdout stays as `-output` says. An existing file or a missing directory is exit 1 before the start. A run with no report leaves no file. A failed write is exit 1 |
| `-label` | A `key=value` label for the report's passport; repeat it for several. Wins over the same key in the config's `labels`. A key given twice, or one that breaks the key and value rules, is exit 1 before the start |
| `-connect-timeout` | How long to wait for a service that accepted the connection but stays silent. Default `10s`. A refused connection and a wrong address are not waited for |
| `-max-in-flight` | Cap on requests waiting for a reply. Default `5000` |
| `-plain` | No live screen: a progress line once a second on stderr, as without a terminal, and no first-run dialog. The report and exit codes are the same |
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

## Where to run

On Linux next to the target: in the same network, the same cluster, a CI runner. Everything between
the generator and the target goes into the latency and looks like the target's time: on our stand at
1000 RPS, through Docker Desktop port forwarding p99 was 38 ms, inside the Docker network 2 ms.

- The exact call schedule is Linux-only. In a container with a CPU quota under two cores, rare start
  lags up to the quota period (usually 100 ms) are possible; the report shows them.
- Behind an L4 balancer one backend is loaded: one connection, one backend. See
  [reading the report](README.md#reading-the-report).
- On Windows the clock steps about 0.5 ms, and a run against a fast service will be declared invalid
  (`clock_step`, exit code 2).

## JSON fields

The contract's rules — schema version, units, `null` rather than `0`, unknown fields — are in the
[README](README.md#json-for-scripts-and-ci). Here is every field of `schema_version` 1. A type with
`?` may be `null`. Warm-up counters are in no other counter; `sent` and all its shares are
measured calls only. The first column is the field's full path.

**Percentile** — an object; `null` — not a single observation. `<percentile>` is any field typed
"percentile?" below.

| Field | Type | Meaning |
|---|---|---|
| `<percentile>.us` | int | The value, whole microseconds |
| `<percentile>.ns` | int | The histogram's value in whole nanoseconds, which the text report prints at three significant figures; `us` is the same value truncated to microseconds; `pass:` and `leettest compare` judge by `ns`, so beside a `fail` `us` can equal `limit_us` |
| `<percentile>.lower_bound` | bool | `true` — a lower bound, not a value ([README](README.md#reading-the-report)) |

### Run

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | int | Schema version, now `1` |
| `mode` | string | `run` for a plain run, `breakpoint` for a breaking-point search |
| `leettest_version` | string | The tool's version; a build from source gives the commit |
| `target` | string | The target address from the config, `fake target` with `-fake` |
| `name` | string | The run's name: the config's `name`, else the service name, else the config file name; always a string |
| `methods_source` | string? | Where the methods came from: `reflection`, `proto` (the `app.proto` files) or `protoset`; `null` with `-fake` |
| `outcome` | string | `complete`, `invalid`, `incomplete` — matches exit code `0`, `2`, `3`; a finished run that missed a `pass` limit is `complete` with code `4`; with `-json-file` and a failed write the code is `1` and `outcome` stays the run's |
| `invalid_reasons` | []string | Why the run is invalid: `in_flight_cap`, `nothing_measured`, `clock_step`, `thresholds_undecided` (a `pass` limit cannot be decided, and not because the run was stopped or already invalid). Empty — valid |
| `tail_wait_cause` | string? | The client-side wait that set the tail: `generator`, `stream`, `connection`; `null` — no verdict |
| `clock_step_ns` | int | The host clock step, ns: every latency and wait is ± it |
| `started_at` | string | Start of the schedule, RFC 3339 UTC, warm-up included. Every `_us` and `_s` counts from it |
| `duration_us` | int | How long the run went, warm-up included |
| `planned_us` | int | How long it was meant to go, the ramp included |
| `warmup_us` | int | The window the statistics leave out: the ramp and the warm-up. Without a ramp, the warm-up length; for the warm-up alone take `warmup_us` − `settings.ramp_us` |
| `settings` | object | What the run was started with, as configured, not as measured |
| `settings.max_in_flight` | int; search: `int?` | The in-flight cap: the `-max-in-flight` value, else the default. A search: `null` without the flag (each run takes the smallest cap it can run with); in its runs, that run's cap |
| `settings.connections` | int | `app.connections` as configured, 1 by default; `connections.open` is what opened |
| `settings.warmup_us` | int | The warm-up as configured. A search: the settle |
| `settings.ramp_us` | int | `load.ramp` as configured, in whole microseconds; 0 without one. (not released) |
| `settings.compression` | string? | `app.compression` as configured: `"gzip"`, `null` — left out |
| `passport` | object? | The machine and the config the run was made with, for telling runs apart. A search has it once at the top level, its runs in `breakpoint.runs` do not |
| `passport.config_sha256` | string | SHA-256 of the config file's bytes as read, 64 hex digits |
| `passport.os`, `passport.arch` | string | `GOOS` and `GOARCH` of the generator, such as `linux` and `amd64` |
| `passport.num_cpu` | int | Logical CPUs the generator's process sees |
| `passport.gomaxprocs` | int | `GOMAXPROCS` at the start |
| `passport.cpu_quota` | float? | The cgroup CPU quota in CPUs as set (`4`, `0.5`), not capped by `gomaxprocs`; `null` — no quota, or not Linux |
| `passport.go_version` | string | The Go version the binary was built with |
| `passport.labels` | object | The labels, key to string; `{}` without any |
| `sent` | int | Measured calls, except `not_sent`. `unreachable` and `aborted` are in it |
| `failed` | int | Of `sent` — every call that did not succeed, except `aborted` |
| `aborted` | int | Cut off by our own stop |
| `not_sent` | int | The timeout ran out before sending: in neither `sent` nor `failed` |
| `not_sent_generator`, `not_sent_stream`, `not_sent_connection` | int | `not_sent` by cause: the generator was late, no free stream, no ready connection. They add up to `not_sent` |
| `warmup_sent`, `warmup_failed`, `warmup_not_sent` | int | The same for calls scheduled in the excluded window (the ramp and the warm-up) |
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
| `connections.first_limit`, `connections.last_limit` | int? | `MAX_CONCURRENT_STREAMS` at the first and last handshake; `null` — not announced (`0` — announced zero). Always `null` with several connections: each has its own limits |
| `connections.limit_changes` | int | Handshakes that announced a limit different from the one before |
| `connections.resolved` | []string? | The addresses the target stands for, in the resolver's order: all of them, even with fewer connections. An IP address stands for itself; `null` with one connection |
| `connections.in_flight_limit` | int? | How many calls the target lets the run have in flight: the one connection's limit, or the sum of all the limits; `null` while any connection announced none |
| `connections.per_connection` | []object? | One object per connection, in the report block's numbering; `null` with one connection |
| `connections.per_connection[].address` | string | The connection's address |
| `connections.per_connection[].calls` | int | Calls assigned to the connection, sent or not, warm-up left out |
| `connections.per_connection[].failed` | int | The connection's failures: the run's `failed` rule plus the calls that never went out because the connection was not ready |
| `connections.per_connection[].stream_waited`, `connections.per_connection[].not_sent_stream` | int | The connection's calls that waited for a free stream: those that went out after waiting and those that expired waiting |
| `connections.per_connection[].p99` | percentile? | p99 of the connection's calls; `null` without a call that has a latency |
| `connections.per_connection[].first_limit`, `connections.per_connection[].last_limit` | int? | The connection's stream limit at its first and last handshake; `null` — not announced |
| `connections.per_connection[].limit_changes` | int | Handshakes of the connection that announced a limit different from the one before |
| `client_waits` | object | Calls that waited on the client side over the floor, by cause ([README](README.md#reading-the-report)) |
| `client_waits.generator_calls`, `client_waits.stream_calls`, `client_waits.connection_calls` | int | All such calls, sent or not. A sent call can count for several causes |
| `client_waits.generator_tail_calls`, `client_waits.stream_tail_calls`, `client_waits.connection_tail_calls` | int | Only among the p99 tail and the unsent: these pick `tail_wait_cause` |
| `generator_cpu` | object? | How much of the CPU available to LeetTest itself the run used, second by second; `null` when the process's CPU time could not be read. The report prints a line when some second is busy |
| `generator_cpu.available_cores` | float | The CPU the process may use: `GOMAXPROCS`, or the cgroup quota when that is smaller |
| `generator_cpu.share` | float | CPU time over wall time and `available_cores`, rounded to 2 decimals, over the planned seconds after the warm-up. Can pass 1: time in system calls runs outside the Ps |
| `generator_cpu.busy_seconds` | int | How many of those seconds have a `share` of 0.90 or more (compared as rounded). `0` when they total under a second |
| `generator_cpu.first_busy_s` | int? | The first such second, counted from the start of the run, warm-up included; `null` without one |
| `generator_cpu.seconds` | []float | The share of each second from the start of the run, warm-up and the timeout tail included: as many entries as the longest `methods[].seconds`. `null` for a second no reading covers, and for a last second under half a second that is counted with the one before |
| `methods` | []object | Methods, see below |
| `unchecked` | []object | Methods that could not be checked through reflection |
| `unchecked[].method` | string | The method |
| `unchecked[].reason` | string | `reflection_off` or `reflection_failed` |
| `unchecked[].error` | string | Text for people, changes with grpc-go |
| `thresholds` | []object? | Verdicts on the `pass` limits, an object per limit, all with the same keys; the ones that do not belong to a limit are `null`. `null` — no limits set (not an empty list, which would read as "all passed"). A search writes it at the top level |
| `thresholds[].method` | string? | The method with its leading slash, as the report names it; `null` — a search's limit |
| `thresholds[].key` | string | `p50`, `p90`, `p95`, `p99`, `p999`, `errors`, `held_rps` |
| `thresholds[].verdict` | string | `pass`, `fail`, `unknown` |
| `thresholds[].reason` | string? | Why `unknown`, from the list: `incomplete`, `invalid`, `few_observations`, `run_limit`, `generator`, `stream`, `client_errors`, `nothing_sent`, `censored`; `null` for `pass` and `fail` |
| `thresholds[].limit_us` | int? | A latency limit, whole microseconds; `null` for the other keys |
| `thresholds[].value` | percentile? | The measured percentile it was compared with; `null` — not measured, or the key is not about latency |
| `thresholds[].limit_percent` | string? | An `errors` limit as written in the config, such as `"0.5%"`; `null` for the other keys |
| `thresholds[].bad`, `thresholds[].bad_client`, `thresholds[].total` | int? | The `errors` share: failed calls (with those not sent for lack of a connection), of them the ones the target did not decide, all counted. `null` for the other keys and when not a single call was counted |
| `thresholds[].limit_rps`, `thresholds[].held_rps` | int? | A search's limit and what it held; `null` for the other keys |
| `notes` | []string | The report's notes, text for people in ASCII; do not parse |

### Method: `methods[]`

`<category>` is each of the four categories that have an object with answer times in JSON:
`request_error`, `overload`, `failure`, `bad_response` ([categories](README.md#reading-the-report)).
The rest are plain counters.

| Field | Type | Meaning |
|---|---|---|
| `methods[].method` | string | The full method name |
| `methods[].invalid_reason` | string? | The method measured nothing about load: `request_error`, `client_error`, `bad_response`, `mixed`; `null` — it did |
| `methods[].sent`, `methods[].failed`, `methods[].aborted`, `methods[].not_sent`, `methods[].not_sent_generator`, `methods[].not_sent_stream`, `methods[].not_sent_connection`, `methods[].warmup_sent`, `methods[].warmup_failed`, `methods[].warmup_not_sent` | int | The method's share of the run's fields of the same name |
| `methods[].rps` | float? | `sent/s`: calls sent divided by the sending time; `null` — no calls |
| `methods[].timeout_us` | int | The method's timeout |
| `methods[].planned_rps_low`, `methods[].planned_rps_high` | int | Lowest and highest planned rate of the measured part of the plan, after the excluded window: a ramped call gives its `rps` twice |
| `methods[].planned_duration_us` | int | How long the method was meant to go, as configured, the ramp included |
| `methods[].request_bytes` | object? | The request body, mean over the measured calls that went out; `null` without compression |
| `methods[].request_bytes.raw`, `methods[].request_bytes.wire` | float | Bytes before compression and on the wire (without the 5-byte frame) |
| `methods[].latency` | object | Latency of successful calls, with timeouts and aborted calls as lower bounds |
| `methods[].latency.min`, `methods[].latency.p50`, `methods[].latency.p90`, `methods[].latency.p95`, `methods[].latency.p99`, `methods[].latency.max` | percentile? | Minimum, percentiles, maximum |
| `methods[].latency.p999` | percentile? | p99.9 by the same rule; null while the method has fewer than 1000 observations (`observations`), where p99.9 would be the max |
| `methods[].p99_without_client_waits` | percentile? | p99 of the same calls without client-side waits; may come out slightly low |
| `methods[].observations` | int | Observations behind the `latency` percentiles |
| `methods[].censored` | int | Of them, only a lower bound is known |
| `methods[].invalid_latencies` | int | Rejected as impossible (a negative latency) — a sign of a bug in the code, not in the target |
| `methods[].timed_out` | int | Went out and got no answer within the timeout |
| `methods[].timed_out_after_wait` | int | Of `timed_out` — went out with less than half the timeout left |
| `methods[].cut_off` | int | Went out and got no status |
| `methods[].unreachable` | int | Never reached the target |
| `methods[].unclassified` | int | The sender gave no category — a sender defect, not an observation of the target |
| `methods[].outside_timeline` | int | Left off `seconds`: a moment of the call did not fit the timeline |
| `methods[].<category>` | object | Answers of this category |
| `methods[].<category>.count` | int | Calls in it |
| `methods[].<category>.p50`, `methods[].<category>.p90`, `methods[].<category>.p95`, `methods[].<category>.p99`, `methods[].<category>.max` | percentile? | Its answer time |
| `methods[].client_error` | int | The client refused to send |
| `methods[].failure_codes` | []object | Failed calls by gRPC code |
| `methods[].failure_codes[].code` | string | The canonical code name (`UNAVAILABLE`); the list may grow |
| `methods[].failure_codes[].count` | int | How many |
| `methods[].failure_codes[].from_target` | bool | `true` — the target sent the refusal: a status over the wire or REFUSED_STREAM, `false` — the client set the code |
| `methods[].silent_from_s` | int? | The second from which the target answered none of the calls sent; `null` — no silence |
| `methods[].silent_sent_rps` | int? | Calls sent in the second before it (in the first, if the silence starts there) |
| `methods[].silent_planned_rps_low`, `methods[].silent_planned_rps_high` | int? | The planned rate of the stages in that second; `null` also when no stage ran in it |
| `methods[].last_answer_at_us` | int? | When the last call the target answered went out; `null` — it never answered |
| `methods[].dataset` | object? | The call's dataset file; `null` — it has none |
| `methods[].dataset.file` | string | The path as the config wrote it |
| `methods[].dataset.sha256` | string | SHA-256 of the file as it lies on disk, a byte-order mark included, 64 hex digits |
| `methods[].dataset.records` | int | Requests in the file |
| `methods[].dataset.used` | int | How many of them were handed to the sender at least once |
| `methods[].dataset.used_max` | int | How many times the most used one was handed to the sender; in a search, counted from its first step to the end of this run |
| `methods[].dataset.order` | string | `file` or `random`: the order the requests went out in |
| `methods[].dataset.seed` | int? | The seed of a random order, 0 to 9007199254740991; `null` for file order |
| `methods[].dataset.reuse` | bool | `false` — the call sends each record at most once (`dataset_reuse: false`); otherwise `true` |
| `methods[].seconds` | []object | The per-second timeline, see below |

### Second: `methods[].seconds[]`

The timeline covers the whole run, warm-up included, up to the last second anything happened in.
Unlike the totals it keeps every call. To reconcile: `Σ begun` + `outside_timeline` is every call
of the method.

| Field | Type | Meaning |
|---|---|---|
| `methods[].seconds[].warmup` | bool | The second is in the excluded window (the ramp and the warm-up) |
| `methods[].seconds[].begun` | int | Calls begun in this second |
| `methods[].seconds[].succeeded`, `methods[].seconds[].overload`, `methods[].seconds[].failure`, `methods[].seconds[].client_error`, `methods[].seconds[].bad_response`, `methods[].seconds[].request_error`, `methods[].seconds[].timed_out`, `methods[].seconds[].unreachable`, `methods[].seconds[].cut_off`, `methods[].seconds[].aborted`, `methods[].seconds[].unclassified` | int | Calls that finished in this second, by category |
| `methods[].seconds[].not_sent_generator`, `methods[].seconds[].not_sent_stream`, `methods[].seconds[].not_sent_connection` | int | Timeouts before sending that finished in this second, by cause |
| `methods[].seconds[].in_flight` | int | Calls in flight at the end of the second |
| `methods[].seconds[].lag_calls`, `methods[].seconds[].lag_sum_us`, `methods[].seconds[].lag_max_us` | int | Over calls scheduled in this second: how many, sum and maximum of the start lag |
| `methods[].seconds[].observed_calls` | int | Of them, the successful ones |
| `methods[].seconds[].observed_lag_sum_us`, `methods[].seconds[].transport_wait_sum_us`, `methods[].seconds[].service_time_sum_us` | int | Over the successful ones: sum of start lag, of the wait before sending (connection and stream), of the time from sending to the answer. Together — their latencies |

### Breaking-point search: `mode: "breakpoint"`

A search writes another object: `schema_version`, `leettest_version`, `target` and `started_at` are
as in the run above, a plain run's other fields are not at the top level. Every run is in
`breakpoint.runs` with its own full report ([the search](../../README.md#breaking-point-search)).

| Field | Type | What it means |
|---|---|---|
| `method` | string | The method the search loaded |
| `breakpoint` | object | The search's result |
| `breakpoint.outcome` | string | Closed list: `broke`, `broke_at_first`, `held_all`, `run_limit`, `stopped`, `invalid` |
| `breakpoint.held_rps` | int? | The highest rate the target held; `null` — none held |
| `breakpoint.broke_rps` | int? | The lowest rate the target broke at or a run limit stopped the search at; `null` — it did not break |
| `breakpoint.errors_limit` | string | The error limit the search used, as a percent: `"1%"` by default. A step broke over it; `100%` means errors do not break a step |
| `breakpoint.why` | string? | The cause of the run that ended the search, from a closed list: `errors`, `p99_limit`, `p99_vs_base`, `connection`, `no_recovery`, `generator`, `in_flight_cap`, `stream_limit`, `stream_wait`, `client_errors`, `clock_step`, `request_errors`; `null` — no cause. `client_errors`: the limit is crossed only with failures on the client's side, the target's own are within it; the outcome is `run_limit` |
| `breakpoint.stop_reason` | string? | Who ended the search: `dataset_exhausted` (a `dataset_reuse: false` file ran out), `requested` (stopped by the user); `null` — the search ended itself |
| `breakpoint.notes` | []string | The search's notes, text for people in ASCII; do not parse |
| `breakpoint.runs` | []object | Every run in order |
| `breakpoint.runs[].kind` | string | `step`, `repeat` or `probe` |
| `breakpoint.runs[].planned_rps` | int | The run's planned rate |
| `breakpoint.runs[].sent_rps` | int | The rate the calls actually went out at: only what was sent counts as held |
| `breakpoint.runs[].broken` | bool | `true` — the run broke the target |
| `breakpoint.runs[].recovered` | bool? | For a `probe`: the target is back within 1.5× of the baseline p99; `null` for a `step` and a `repeat` |
| `breakpoint.runs[].why` | string? | The run's cause, the same closed list as `breakpoint.why`; `null` — no cause |
| `breakpoint.runs[].report` | object | This run's full report, the same object as a plain run's JSON (fields above) |

## Comparing reports: `leettest compare`

`leettest compare [flags] before.json after.json` reads two `-json-file` reports of one kind — two
searches or two runs — and prints what changed. Flags come before the files.

| Flag | What it does |
|---|---|
| `-max-capacity-drop` | Two searches: the limit of the capacity (`held_rps`) fall, in percent of the "before" capacity, such as `10%`. Without it the capacity is printed but not judged |
| `-max-p99-rise` | Two runs: the limit of each method's p99 rise, in percent of the "before" p99, such as `20%` |
| `-max-errors-rise` | Two runs: the limit of the rise of the share of failed calls, in percentage points, such as `1%` |
| `-output` | Report format on stdout: `text` (the default) or `json` for scripts and CI. Any other value is exit 1, no file is read |

The exit codes are the same for both formats: `0` within the limits or none given, `1` a report that
could not be read or flags that do not fit, `2` reports that cannot be compared or a limit that
cannot be decided, `4` a limit missed (also beside a check that cannot be decided).

## Compare JSON

`leettest compare -output json` writes one object and a newline to stdout, nothing else; on exit
code 1 stdout is empty. Exit codes and stderr are those of text. The object holds the reports' own
numbers and the verdicts; it has no percentage of change — a script computes it from the values.
The cause of an `unknown` is a value from a closed list in `checks[].reason`, and `why` is the same
words the text prints.

Method names are written two ways. In `differences` and in the text a method has no leading `/`
(`pkg.Svc/Do`). In `checks[].method` the name is as the report has it: a run report with the leading
`/` (`/pkg.Svc/Do`), a search report without.

A field that a check's key does not use is `null`: a `capacity` check has no `*_p99` and `*_errors`,
a `p99` check has no others. A verdict exists only when the check has a limit.

| Field | Type | What it means |
|---|---|---|
| `schema_version` | int | Schema version, now `1` |
| `mode` | string | Always `"compare"` |
| `compared_mode` | string | The kind of the compared reports: `"run"` or `"breakpoint"` |
| `leettest_version` | string | The version of the LeetTest that compared (not of the reports) |
| `base`, `new` | object | The "before" report and the "after" report |
| `base.path`, `new.path` | string | The file path as given on the command line |
| `base.name`, `new.name` | string | The report's name; empty stays `""` |
| `base.started_at`, `new.started_at` | string | The start, as the report has it |
| `base.leettest_version`, `new.leettest_version` | string | The version of the LeetTest that wrote the report |
| `base.labels`, `new.labels` | map | The passport's labels; `{}` without a passport or labels |
| `comparable` | bool | `true` — nothing makes the reports incomparable and, for runs, there is a method in common |
| `differences` | []object | What makes the reports incomparable, in the order compared; `[]` if nothing |
| `differences[].field` | string | The field's name as in the `differs:` line (`target`, `method`, `leettest_version`, `passport.config_sha256`, `methods`, `dataset <method>`). A missing passport is `passport` |
| `differences[].base` | string | The "before" value; `none` — no dataset or no passport |
| `differences[].new` | string | The "after" value; for `passport`, `present` on the side that has one |
| `verdict` | string? | The worst verdict of the checks that have a limit: `fail`, else `unknown`, else `pass`; `null` — no limit given |
| `checks` | []object | The checks in the text's order: a search has one (`capacity`), a run has, per method in common in the "before" order, `p99` then `errors`; `[]` if there is nothing to compare |
| `checks[].method` | string | The check's method; names as above |
| `checks[].key` | string | `capacity`, `p99` or `errors` |
| `checks[].base_capacity`, `checks[].new_capacity` | object? | The search's capacity; `null` — not measured, or another kind of check |
| `checks[].base_capacity.rps`, `checks[].new_capacity.rps` | int | The capacity in rps |
| `checks[].base_capacity.bound`, `checks[].new_capacity.bound` | string | `exact` — the search broke, `rps` is the highest rate that held; `lower` — it ended before it broke, the capacity is at least `rps`; `upper` — it broke at the first step, the capacity is below `rps` |
| `checks[].base_outcome`, `checks[].new_outcome` | string? | The search's `breakpoint.outcome`; only for `capacity` |
| `checks[].base_p99`, `checks[].new_p99` | object? | The method's p99 — the same object as `latency.p99` of the run report; `null` — too few observations, or another kind of check |
| `checks[].base_p99.us`, `checks[].new_p99.us` | int | The p99 in whole microseconds |
| `checks[].base_p99.ns`, `checks[].new_p99.ns` | int | The p99 in whole nanoseconds; the rise is computed from it. For a report written without `ns`: `us` × 1000 |
| `checks[].base_p99.lower_bound`, `checks[].new_p99.lower_bound` | bool | `true` — the tail ran past the timeout, the true p99 is larger |
| `checks[].base_errors`, `checks[].new_errors` | object? | The method's share of failed calls in parts; `null` — another kind of check |
| `checks[].base_errors.bad`, `checks[].new_errors.bad` | int | Every failed call and every call lost to the connection |
| `checks[].base_errors.bad_client`, `checks[].new_errors.bad_client` | int | The part of `bad` that says nothing of the target: rejected requests, client errors, our `bad_response` |
| `checks[].base_errors.total`, `checks[].new_errors.total` | int | The calls sent and those lost to the connection; `0` stays `0` |
| `checks[].limit_percent` | string? | The check's limit as the flag has it (`"20%"`); `null` — no limit given |
| `checks[].verdict` | string? | `pass`, `fail` or `unknown`; `null` — no limit |
| `checks[].reason` | string? | Only for `unknown`, a value from the list below; otherwise `null` |
| `checks[].why` | string? | Only for `unknown`: the words the text prints after `unknown, `; do not parse |

The reasons of an `unknown` are a closed list; of the first six the first that applies wins:

- `not_comparable` — the reports are not comparable, the numbers are not put side by side;
- `method_invalid` — the method of one side is invalid;
- `invalid` — the run of one side is invalid;
- `incomplete` — the run of one side did not finish;
- `generator` — the generator itself held one side back;
- `stream` — the stream limit held one side back;
- `few_observations` — too few observations for a p99;
- `base_zero` — the base is zero (a p99 of 0 us or a capacity of 0 rps), there is no share of it;
- `censored` — the p99 ran into the timeout and does not decide the limit;
- `nothing_sent` — one side sent nothing;
- `client_errors` — the client's own errors decide it;
- `not_measured` — one side's capacity was not measured;
- `broke_at_first` — the search broke at its first step, no fall can be seen;
- `lower_bound` — the search ended before it broke, the capacity is a lower bound.
