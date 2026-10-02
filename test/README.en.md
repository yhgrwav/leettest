# <img src="../assets/icon.png" width="24" alt="" align="top"> Reference stand and measurement correctness tests

> Translated from [README.md](README.md) at d7f498c, 2026-10-02. If they differ, the Russian
> one is right.

What lives here is not a check of features but a check that the tool does not lie. The product is
a measuring instrument, and there is one question to ask of it: do the numbers in the report match
what actually happened?

## What is here

**`stand/`** — a reference gRPC service with controlled behaviour: a constant delay, a hang, a
refusal with a given code on a schedule, a delay that changes mid-run. The behaviour is set by
construction, not asserted, so the right answer is known in advance: a stand told to hold its
answer for 200 ms has no opinion on the right number — it makes it.

The stand does not depend on the engine's code — only on the `grpc.health.v1.Health/Check`
contract and server reflection — so another tool can be pointed at it too.

**`stand/cmd/stand`** — the same stand as a separate process over TCP: point the CLI or another
tool at it. Behaviour is set by a flag, one at a time; on exit the stand prints what it saw
itself: how many calls arrived each second, how many it answered and how long it held them.

```console
$ go run ./test/stand/cmd/stand -delay 20ms -freeze-at 5s -freeze-for 1s -life 20s
$ go run ./test/stand/cmd/stand -delay 20ms -hang-from 8s
$ go run ./test/stand/cmd/stand -delay 20ms -fail-every 10
$ go run ./test/stand/cmd/stand -delay 150ms -max-streams 1
$ go run ./test/stand/cmd/stand -delay 20ms -capacity 270
```

The method is `grpc.health.v1.Health/Check`, the default address `127.0.0.1:50051`, no TLS. Times
in flags count from the first call to arrive. Without `-life` the stand runs until Ctrl+C.
`-max-streams n` is the limit of concurrent streams per connection the stand announces to the
client; it combines with any behaviour. `0` announces no limit at all. `-capacity n` is a stand of
known capacity: it serves at most `n` calls a second, first come first served; above that calls
wait in a queue.

**`measure/`** — substitution tests: the full path (scheduler → worker pool → gRPC sender →
metrics) against the stand, the report's numbers checked against what the stand did.

| What the stand is set to | What the report must show |
| --- | --- |
| answer in 200 ms, target 100 RPS | every percentile near 200 ms, not milliseconds |
| answer in 400 ms, target 100 RPS | the stand sees calls the whole second: the generator does not wait for answers |
| never answers, timeout 200 ms | every call fails, censored exactly at the timeout |
| every third call `RESOURCE_EXHAUSTED` | exactly a third fail, under that code; they carry the time to the refusal, the rest the serving time |
| first 40 calls 50 ms, then 400 ms | p50 in the fast part, p90 in the slow one, not the average |
| instant answer, target 200 RPS | calls spread evenly over the second, no burst at the start |
| **a 1 s hang mid-run**, target 100 RPS | p99 near a second, not milliseconds — the stage 0a criterion |
| the same with a 2-stream quota on the stand | p99 near a second and p90 at least 0.5 s: calls that waited for a stream inside the generator count from their scheduled moment (coordinated omission) |
| a 1-stream quota, answer in 150 ms, timeout 230 ms, 10 RPS | nearly every sent call waited for the stream (wait p99 near 130 ms), the limit 1 named as announced by the target, p99 without the wait printed apart |
| no limit announced, 300 calls in flight | no call waited for a stream: the grpc-go client has no limit of its own |
| the stand drops the connection mid-run and refuses a new one for 500 ms | unsent calls recorded as "waited for the connection", not "waited for a stream" |
| one call held longer than every other slow one | max is that call, not p99 |
| 1500 calls, 250 of them warm-up | 1250 sent plus the warm-up line — together every call the stand got |
| the stand never answers from the first call | silent from the first second, failures both live and in the report |
| slots held past the in-flight budget's allowance | the run hits the in-flight cap and says so |

Each check's tolerance is smaller than the distance to the nearest plausible wrong answer: the mean
instead of a percentile, a wait counted twice, latency from the moment of sending. The margin is
only upward; lower bounds are strict, because a tool suffering from coordinated omission lies low.

The report's numbers are compared not with the plan but with what **the stand recorded**: each
call's arrival time is an observation from outside the generator.

## Not there yet

- a breaking-point search: the verdict "the target does not hold X RPS" is not done yet;
- the stand changes its delay by call number, not by time;
- a cross-check against `k6` on the same stand (against `ghz` — [docs/en/compare-ghz.md](../docs/en/compare-ghz.md));
- rate accuracy over a long run: 800 RPS for a minute — about 48,000 calls with no bursts.

## How we work

The stand does not depend on the engine's code — only on the agreed contract. So work here can
start before the engine is ready.

A mismatch between expected and actual numbers is filed through the "Measurement correctness"
issue template: it asks for the stand's description, the expected value's calculation and the full
report.
