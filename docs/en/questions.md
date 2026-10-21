# What questions LeetTest answers

[← Back to docs](README.md)

> Translated from [docs/ru/questions.md](../ru/questions.md) at fe640ce, 2026-10-12. If they
> differ, the Russian one is right.

The questions the report answers today.

| Question | What the report shows | Where |
|---|---|---|
| At what load does the service stop coping? | Breaking-point search: the step the target held, the step it broke on, and why — errors, p99, the connection, no return to the earlier p99 | [Breaking-point search](README.md#breaking-point-search) |
| How does each method answer under load that looks like production? | Several methods at once, each at its own RPS; p50, p90, p95, p99 for each | [Run](README.md#run) |
| What does a user see when the service stalls or slows down? | Latency counts from the call's scheduled moment: a stall of the target does not hide, it lands in p99 | [Reading the report](README.md#reading-the-report) |
| Which errors, and from whom? | Categories: bad request, overload, failure, cut off, timeout; the status came from the target (or a proxy) or was set by the client | [Reading the report](README.md#reading-the-report) |
| How many calls were left without an answer, and why? | Never sent, cut off or timed out — separately; the second from which the target stopped answering | [Reading the report](README.md#reading-the-report) |
| Are all the backends behind a balancer loaded? | `app.connections: N`: a line per connection — address, calls, share of failures, p99 | [Reading the report](README.md#reading-the-report) |
| Is the measurement itself lying? | A verdict on client-side waits and the p99 without them; an invalid run (exit 2) when the numbers are not about the target | [When a run is invalid](README.md#when-a-run-is-invalid) |
| How does the service behave on different data? | `dataset`: its own request for every call, one JSON per file line | [Reference](reference.md#a-different-request-each-call-a-dataset) |
| Can it run in CI? | One JSON object with a stable schema and exit codes; `pass` limits decide "holds / does not hold" with exit code `4` | [JSON for scripts and CI](README.md#json-for-scripts-and-ci) |

LeetTest sees only the client side: it does not know the target's resources and does not guess
them. What comes next is in the [roadmap](roadmap.md).
