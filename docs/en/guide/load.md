# Load

[← Back to docs](../README.md)

> Translated from [docs/ru/guide/load.md](../../ru/guide/load.md) at feb2d1d, 2026-11-10. If they
> differ, the Russian one is right.

Which methods to load, at what rate and for how long. The rate is set in requests per second: a
request goes out on schedule without waiting for the answers to the earlier ones, and the latency
is counted from the planned moment.

## Minimal config

```yaml
app:
  target:
    ip: localhost
    port: 50051
  tls: false

load:
  warmup: 5s
  ramp: 30s
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      timeout: 500ms
      data:
        wallet_id: "w-123"

    - method: wallet.v1.WalletService/Transfer
      rps: 50
      duration: 1m
```

```sh
leettest -c leettest.yaml
```

- `method` — the full name: `package.Service/Method`. One method is one call in `calls`; several
  methods at different rates are several calls.
- `rps` — a whole number of requests per second. `10.5` is rejected, not rounded silently.
- `duration` — how long to load the method: `30s`, `5m`, `1h`.
- `timeout` — how long to wait for an answer. Not set — `2s`. Zero does not turn the timeout off, it
  is an error.
- `warmup` — the first seconds go to the service but stay out of the statistics, because cold caches
  spoil them. The warm-up is part of `duration` and must be shorter than every call. The report
  shows it on a separate line, `warm-up N sent`.
- `ramp` — the ramp-up: each method rises from zero to its `rps` linearly over this time, and then
  its `duration` begins. The run lasts `ramp` + `duration` (`warmup`, if set, is part of
  `duration`). The ramp's requests stay out of the statistics; the `pass` limits and the share of
  errors are counted over the hold only. An example is
  [`examples/ramp.yaml`](../../../examples/ramp.yaml).
- `data` — the request body as plain YAML in the shape of the message. No `data` — an empty message
  goes out.

All requests of a method go out with the same body. For a write with an idempotency key this means
that only the first request creates the record and the rest go down the repeat path: that is what
is measured. A different request for each call is a `dataset` file (one JSON per line) or
`templates: true` with `{{uuid}}` and `{{seq}}`; see the
[reference](../reference.md#request-body).

## What will appear in the report

- A row per method: `sent`, `failed`, `sent/s` and the percentiles. Under it `plan:` — the rate and
  the duration you set.
- `sent/s` is the requests sent divided by the sending time: a service hanging at the end of the run
  does not lower this number.
- If fewer requests went out than in `plan:`, the report says why: `not sent` (requests that were
  due to go out by the plan but did not) with the reason.
- The `over time` block under a method's table shows how the rate, the share of failures, p50 and
  p99 changed over time windows. With `ramp` the first rows are the ramp-up: a low `sent/s` there is
  planned, not broken. Look for the row where p99 or the share of failures climbs before the rate
  has reached its plateau.
- When the service has hung, the line under the table names the second after which it did not
  answer a single request.

How to read each part — [Reading the report](../report.md#reading-the-report).

## Before the start

LeetTest checks the config before the first request: an unknown field, a wrong value, a method the
service does not have, a mistake in the request body — exit code 1 and not a single request to the
service. The cap on requests without an answer is checked too (in-flight: sent, no answer yet): a
service that hangs piles up `rps × timeout` such requests, and the sum over the methods must not
exceed `-max-in-flight` (5000 by default). The error names both ways out: which timeout would fit
and which cap is needed.

## Common mistakes

- **`warmup` is longer than or equal to `duration`.** Not a single measured request would be left
  of the call: a config error.
- **A "growth" of load is wanted, but `rps` is set at once.** Without `ramp` a method starts at its
  full rate. For growth set `ramp`; to find out at what load the service stops coping — the
  [breaking-point search](../report.md#breaking-point-search).
- **The timeout is too large for the cap.** An error before the start, see above.
- **The timeout is too short.** Requests that had no time to answer are cut off by the timeout and
  look like failures of the service; their percentile is printed as `>500ms` — "at least 500 ms".
- **Every request is the same, but different ones are needed.** See above on `dataset` and
  `templates`.
- **A run over ssh without a terminal.** Add `-plain`: a progress line once a second instead of the
  live screen.

## Next

- [Connection](connection.md) — TLS, headers, tokens.
- [Reference](../reference.md) — every field and flag.
