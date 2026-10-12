# LeetTest and ghz on the same stand

> Translated from [docs/ru/compare-ghz.md](../ru/compare-ghz.md) at b7a685c, 2026-10-12. If they
> differ, the Russian one is right.

This page compares LeetTest with [ghz](https://ghz.sh) on a test server with known behavior. The
question is narrow: when the server stalls or slows down, does the report show it?

In short:

- **When the server behaves**, both tools report the same latencies (mode A).
- **When the server freezes for 2 seconds** (mode B1), ghz in synchronous mode (the default)
  reports p99 of 11.4ms with `-c 10` and 11.6ms with the default `-c 50`. LeetTest reports 1.71s.
  ghz with `--async` also reports 1.71s.
- **When the server is slower than the load needs** (mode B2), ghz in synchronous mode (the
  default) with `-c 10` sends 2970 of the 6000 requested calls. Its latencies are correct for the
  calls it sent, but that is half the requested load. With the default `-c 50` it sends all 6000.

This is not a bug in ghz. The synchronous mode runs a fixed number of workers (`-c`, 50 by default
[1]). Each worker waits for its call to finish before taking the next one [2]: a closed model, by
design. When the server stalls, the workers wait too, and calls that should have gone out during
the stall do not go out on time. So their delay is never measured. This effect is known as
*coordinated omission*. With `--async`, ghz does not wait for a call to finish before the next
one [2][3]: an open model. That gives the same numbers as LeetTest. LeetTest has only the open
model.

Everything said here about ghz was checked against v0.121.0 — sources at the end of the page.

## Setup

The stand is `test/stand` in this repository: a gRPC server answering `grpc.health.v1.Health/Check`
after a set delay. It can freeze for a set time or answer more slowly.

| mode | stand | what it models |
|---|---|---|
| A | `-delay 10ms` | control: a server that keeps up |
| B1 | `-delay 10ms -freeze-at 10s -freeze-for 2s` | a 2-second stall 10s into the run (a GC pause, a lock) |
| B2 | `-delay 100ms` | a server slower than the load needs |

Load in every run:

- 200 calls per second for 30 seconds, which is 6000 calls;
- a 5s timeout;
- one connection.

The ghz flags are:

```
ghz --insecure --call grpc.health.v1.Health/Check -d '{}' --rps 200 -z 30s -t 5s \
    --duration-stop wait --connections 1 -c 10 [--async]
```

Synchronous ghz runs with `-c 10` and with ghz's default `-c 50`. `--duration-stop wait` lets calls
still in flight at 30s finish and be counted.

Every tool ran 5 times in every mode, and each run got a fresh stand. The run order rotates, so
no tool always runs first. The tables show the median of the 5 runs, with the minimum and maximum
in brackets.

Environment:

- LeetTest at commit `fb8c7b7`;
- ghz v0.121.0 (grpc-go v1.56.3);
- Go 1.27.1;
- Linux (kernel 6.18) in a `golang:1.27.1` container with `--cpus=4`; Docker Desktop on Windows 11,
  so Linux in a WSL2 virtual machine on a Windows host;
- host CPU AMD Ryzen 9 5900X (12 cores, 24 threads), 16 GB RAM in the virtual machine;
- tools and stand in the same container, over loopback.

## Results

### B1: a 2-second freeze

| tool | sent | failed | p50 | p90 | p95 | p99 | calls over 1s |
|---|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 10.4ms [10.3–10.4] | 10.6ms [10.5–10.6] | 509ms [508–509] | 1.71s [1.71–1.71] | not reported |
| ghz, sync (`-c 10`) | 5999 [5999–6004] | 0 | 10.7ms [10.7–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.3–11.4] | 11.4ms [11.4–11.5] | 10 [10–10] |
| ghz, sync (`-c 50`, default) | 5999 [5999–6000] | 0 | 10.7ms [10.7–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.4–11.4] | 11.6ms [11.5–12.3] | 50 [50–50] |
| ghz, `--async` | 6000 [5999–6000] | 0 | 10.7ms [10.7–10.7] | 11.4ms [11.4–11.4] | 512ms [506–518] | 1.71s [1.71–1.72] | 202 [202–203] |

Over 2 seconds at 200 RPS, about 400 calls were due. With `--async`, ghz counts 202 calls over 1s:
the ones due in the first half of the freeze wait longer than a second. In synchronous mode, 10
calls are over 1s with `-c 10` and 50 with `-c 50`, one per worker. That is 0.17% and 0.83%
of the calls, too few to reach p99. The workers stood still during the freeze. When it ended, ghz
sent the missed calls at once: behind schedule, its pacer does not wait before the next call [4].
Those calls met a server that was already fast again. Their wait before sending is not part of
the latency ghz reports: it counts from the start of the call itself [5].

The number of calls sent is the same in all rows, so the count does not reveal the stall.

### B2: a server slower than the load needs

| tool | sent | failed | p50 | p90 | p95 | p99 |
|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 100ms [100–100] | 101ms [101–101] | 101ms [101–101] | 102ms [101–102] |
| ghz, sync (`-c 10`) | 2970 [2970–2970] | 0 | 100.9ms [100.9–101] | 101.3ms [101.3–101.4] | 101.4ms [101.4–101.5] | 101.6ms [101.6–101.7] |
| ghz, sync (`-c 50`, default) | 6000 [5999–6001] | 0 | 100.5ms [100.5–100.6] | 101.3ms [101.3–101.4] | 101.4ms [101.3–101.5] | 101.5ms [101.4–101.7] |

Synchronous ghz sends at most c / latency calls per second, where c is the number of workers.
With `-c 10` and 100ms that is 100 calls per second, half of the 200 requested. The latencies
are right; the load is not. Reading only the latency columns, this run looks like a server
that handles 200 RPS at 101ms. The server handled 100 RPS.

With the default `-c 50` the ceiling is 500 calls per second, and the full load goes out. The
same ceiling applies to any c once the server slows down enough: at `-c 50` it is reached above
250ms.

### A: control

| tool | sent | failed | p50 | p90 | p95 | p99 |
|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 10.3ms [10.3–10.4] | 10.5ms [10.5–10.6] | 10.5ms [10.5–10.6] | 10.6ms [10.6–10.8] |
| ghz, sync (`-c 10`) | 6000 [6000–6008] | 0 | 10.6ms [10.6–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.3–11.4] | 11.4ms [11.4–11.5] |
| ghz, `--async` | 6001 [5996–6002] | 0 | 10.7ms [10.7–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.4–11.4] | 11.4ms [11.4–11.5] |

All three agree within 1ms. In this environment ghz reads 0.3–0.9ms higher than LeetTest; we did
not measure the cause.

## Reproduce

Install ghz, then run from the repository root:

```sh
go install github.com/bojand/ghz/cmd/ghz@v0.121.0
go run ./test/compare-ghz -ghz "$(go env GOPATH)/bin/ghz"
```

The script builds the stand and LeetTest and runs every mode 5 times, which takes about 25
minutes. It prints this page's tables and writes them, with every run's raw output, to
`test/compare-ghz/out/`.

A short run, once and only mode B1, takes about 2 minutes:

```sh
go run ./test/compare-ghz -ghz "$(go env GOPATH)/bin/ghz" -runs 1 -modes B1
```

The modes are `A`, `B1`, `B2` (`-modes`), the tools are `leettest`, `ghz-sync`, `ghz-sync50`,
`ghz-async` (`-variants`); the other flags are in `go run ./test/compare-ghz -h`.

Numbers will differ on another machine. The pattern should not: in B1, ghz in synchronous mode
misses the freeze, while LeetTest and ghz with `--async` both show it.

## Sources on ghz (v0.121.0, checked 2026-10-02)

1. `-c` defaults to 50 — [`runner/options.go`, `NewConfig`](https://github.com/bojand/ghz/blob/v0.121.0/runner/options.go).
2. A synchronous worker calls `makeRequest` and waits for it; with `--async` it starts a goroutine
   per call — [`runner/worker.go`, `runWorker`](https://github.com/bojand/ghz/blob/v0.121.0/runner/worker.go).
3. `--async`, `--duration-stop`, `--connections` — [options documentation](https://ghz.sh/docs/options).
4. Behind schedule, the pacer returns a zero wait — [`load/pacer.go`, `ConstantPacer.Pace`](https://github.com/bojand/ghz/blob/v0.121.0/load/pacer.go);
   ticks reach the workers over an unbuffered channel — [`runner/requester.go`, `runWorkers`](https://github.com/bojand/ghz/blob/v0.121.0/runner/requester.go).
5. A call's duration is `EndTime − BeginTime` from gRPC stats events — [`runner/stats_handler.go`, `HandleRPC`](https://github.com/bojand/ghz/blob/v0.121.0/runner/stats_handler.go).
