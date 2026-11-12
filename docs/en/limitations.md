# What LeetTest does not do and where its numbers stop holding

[← Back to docs](README.md)

> Translated from [docs/ru/limitations.md](../ru/limitations.md) at 3a9dbba, 2026-11-09. If they
> differ, the Russian one is right.

LeetTest sees only the client side: the machine and process that send the load (the generator,
below) and what comes back. It does not know the target's resources and does not guess them. This
page is the one place where the limits are collected: under which conditions the report's numbers
are not about the target, and what the tool simply does not do yet. The details of each point are
on the pages it links to.

## Where the numbers stop being about the target

When the generator or the path to the target spoils a measurement, the report either says so itself
or cannot. Here is which.

- **The generator's CPU.** The generator needs CPU to send calls on schedule. If it lacks it (few
  cores, `GOMAXPROCS`, the container's CPU quota), calls start later than planned and that time is
  in the latency. When the generator's lag decided the p99 tail, the run is invalid
  (`generator_late`, exit code 2), the report names the methods and next to it says to give the
  generator more CPU, lower the rate or split it over several machines. A method is judged from
  100 observations; with fewer, p99 is the maximum and the run stays valid, with a note. A p99
  shift under 10% is not a verdict but a note with the number. A breaking-point search does not get
  this verdict: its steps have their own `generator` cause. The run passport shows the CPU count,
  `GOMAXPROCS` and the quota. Not released yet: the report names the limit the generator hit
  (`GOMAXPROCS`, the CPU quota, the CPU itself or the schedule) or says it cannot name one, and the
  run is also invalid when the generator never sent 1% or more of a method's calls.
- **The schedule on Linux and on Windows.** The exact call schedule is Linux-only. In a container
  with a CPU quota under two cores, rare start lags up to the quota period (usually 100 ms) are
  possible; the report shows them. On Windows the clock steps about 0.5 ms, and a run against a
  fast service is declared invalid (`clock_step`, exit code 2): the clock is coarser than a quarter
  of the p50, or the step grew to 1 µs or more during the run. Measure from Linux next to the
  target.
- **The network between the generator and the target.** Everything between them goes into the
  latency and looks like the target's time. The tool does not know the network delay separately and
  gives no verdict on it. On our stand at 1000 RPS, through Docker Desktop port forwarding p99 was
  38 ms, inside the Docker network 2 ms.
- **An L4 balancer.** By default all the load goes over one connection, and it lands on one
  backend: behind an L4 balancer (a Kubernetes ClusterIP, an NLB) and when DNS returns several
  addresses. "Does not hold X" is about that backend, not the service, and the report will not show
  it. To load several, set `app.connections`: the report shows a row per connection. An L7 balancer
  that spreads individual requests (Envoy, a gRPC ingress) spreads even one connection.
- **The target's stream quota.** The target announces how many calls may be in flight on one
  connection (`MAX_CONCURRENT_STREAMS`). A call that finds no free stream waits on the client, and
  the wait is in the latency. If streams running out is the cause of the tail, the report says so
  and prints p99 without the waits, but the target's capacity above "limit × connections" is not
  measured.
- **The in-flight cap.** `-max-in-flight` (5000 by default) limits the calls waiting for a reply.
  The sum of `rps × timeout` over the methods plus a margin is checked against it before the start,
  so a hung target does not hit the cap. If it runs out anyway, the generator held the slots (it
  ran short of CPU), and the run is invalid.
- **What an invalid run names and what it does not.** It names the cause from a closed list and
  the method. It does not name the target's bottleneck, does not tell the target's CPU from its
  database and does not judge what the client cannot see.
- **Error categories.** A status may have come from a proxy in front of the target, not from the
  target: nginx without a live backend answers `UNAVAILABLE`, and the client cannot tell one from
  the other. A request too large is recognised by grpc-go's error text; a target on another
  implementation (Envoy, Java) words it otherwise, and its refusal lands in `overload` instead of
  `request error`.
- **A small sample.** With fewer than 100 observations p99 is the maximum. The generator verdict
  knows this, while `leettest compare` and a `pass` limit on p99 judge such a p99 without a caveat.
- **A fragile generator verdict.** With 200 observations the p99 tail rests on two calls: one call
  that started 4–35 ms later can make the run invalid. The number does not lie, but the verdict on
  such a sample is fragile.
- **The "over time" windows.** Their p50 and p99 have two significant digits, so they are not
  compared with the run's p50 by the last digit. The windows take memory: about 74 KB per
  method-window pair, about 250 MB at 50 methods. The warm-up histogram is about 600 KB per
  method; with many methods that have a warm-up the memory adds up.

## What it does not do yet

- **Unary calls only.** Streaming methods are rejected at load.
- **Call chaining.** A field from one method's reply cannot go into another's request — planned,
  see [call chaining](chaining.md) and the [roadmap](roadmap.md).
- **Token refresh** during a long run — planned, see the [roadmap](roadmap.md).
- **Every call of a method goes out with the same body** unless a `dataset` is set. With a
  `dataset` the requests differ, but that does not mean the target saw as many distinct requests:
  the `data:` line of the report shows how many requests of the file were used.
- **Binary headers** (`-bin`) are not supported.
- **A password-protected private key** is not supported: decrypt it beforehand.

## Where to look next

- [Reading the report](report.md#reading-the-report) — client-side waits, categories, the clock.
- [When a run is invalid](report.md#when-a-run-is-invalid) — the full list of causes.
- [Where to run](reference.md#where-to-run) — what disturbs a measurement and where to run from.
- [Which load-testing problems does it fix?](pitfalls.md) — what the tool does about each.
