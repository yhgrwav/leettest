# Why this tool?

[← Back to docs](README.md)

> Translated from [docs/ru/why.md](../ru/why.md) at b7a685c, 2026-10-12. If they differ, the
> Russian one is right.

LeetTest is built on one principle: a load test result is worth something only when it can
be trusted without caveats. Everything else in the tool follows from that.

The priorities are ordered: **measurement correctness**, then **usability**, then **speed**. On
conflict the first wins — an optimisation that hurts accuracy is rejected.

## Comparison with ghz, k6 and JMeter

🟢 has it · 🔴 does not · 🟡 partly or with extra work (the word in the cell says which). Numbers in
square brackets are [sources](#sources) at the bottom of the page. JMeter is version 5.6.3 with a
gRPC plugin: JMeter itself does not do gRPC [20][21].

**What you can run**

| What we check | ghz | k6 | JMeter | LeetTest |
|---|---|---|---|---|
| gRPC without a plugin | 🟢 [1] | 🟢 [13] | 🟡 plugin [20][21] | 🟢 |
| A method without `.proto`, via server reflection | 🟢 [3] | 🟢 [14] | 🔴 needs `.proto` [21] | 🟢 |
| A service without reflection: schema from `.proto` or protoset | 🟢 [3] | 🟢 [15] | 🟢 [21] | 🔴 [roadmap](roadmap.md#next-version-v03) |
| Several methods, each at its own RPS, in one run | 🔴 one method [1] | 🟡 scenarios in a script [10] | 🟡 thread groups [22] | 🟢 |
| Load described without code of your own | 🟢 flags, JSON, TOML [2] | 🔴 JavaScript [10] | 🟢 `.jmx` plan [28] | 🟢 YAML |
| Ramp-up to a target RPS, then steady load | 🟢 [7] | 🟢 [16] | 🟡 experimental [23] | 🔴 [roadmap](roadmap.md#later) |
| Thresholds and an exit code for CI | 🔴 [8] | 🟢 [17] | 🔴 [20][26] | 🔴 [roadmap](roadmap.md#later) |
| A response field goes into the next request | 🔴 [9] | 🟡 script [18] | 🟢 post-processors [27] | 🔴 [roadmap](roadmap.md#later) |
| Finding the load at which the target breaks | 🔴 [8] | 🟡 script [19] | 🔴 [20] | 🟢 steps |

**How it measures**

| What we check | ghz | k6 | JMeter | LeetTest |
|---|---|---|---|---|
| Load does not drop when the target slows down (open model) | 🟡 `--async`, not the default [4] | 🟡 its own executor, not the default [11] | 🟡 experimental, not the default [23] | 🟢 the only model |
| The generator's own lag is counted in latency | 🔴 no: from the send [5] | 🔴 no: from the send, unsent ones in `dropped_iterations` [12][13] | 🔴 no: from the send [24] | 🟢 |
| Failed and timed-out calls are in the percentiles | 🟡 `--count-errors`, not the default [6] | 🟢 [13] | 🟢 [25] | 🟢 failed ones in their own per-category percentiles, timed-out ones as a lower bound |

The open model by default, counting the generator's lag and breakpoint search are the rows
where none of ghz, k6 and JMeter has a 🟢. LeetTest does not yet have ramp-up to a target RPS, CI thresholds, call
chaining or running without reflection; the order is in the [roadmap](roadmap.md).

## Load that mirrors production

The tool describes load the way it exists in reality: several methods at once, each with its own
rate and duration, all in one run and one report. It is on the mix of calls that contention for
the connection pool, database locks and cache fights show up — effects you never see while
methods are measured one at a time.

## Measurement that resists distortion

Latency is counted from the moment the request was scheduled for, not from the moment it could be
sent. That captures the whole delay, including time spent queued on the generator side. When the
target hangs for one second, the tool reports `1005ms` — the delay a real user would have seen —
instead of `5ms`, which only reflects how fast replies came after recovery.

Load is applied on a schedule: the requested RPS holds whether or not the service keeps up.

A request cut off by the timeout is not replaced with a number. All we know is that it took
longer than the timeout, and if such requests could occupy a percentile's place, the report
prints a lower bound instead of a number: `p99 >2.0s`.

## No preparation

A service address and a method name are enough to start. The tool gets the method description
from the service itself through gRPC server reflection: no `.proto` files, no codegen, no build
steps. The request body is written in the config as plain YAML and built from the service's schema
before the start.

## Declarative description

Load is defined by a config file, not a program. The file lives next to the service code, goes
through code review and stays readable six months later. It needs no debugging and cannot itself
become a source of measurement error.

## Care for the person using it

Error messages name the place and the reason. An unknown config key or a zero rate is an error,
not silently accepted: a run that sent no requests must not look successful. The run's state is
visible live — current rate, requests in flight, error share, percentiles.

Interface quality is treated on a par with measurement correctness: an unclear error message is a
defect.

## Sources

Versions: ghz v0.121.0 (latest release), k6 v2.3.0, JMeter 5.6.3 with the jmeter-grpc-request
plugin v2.0.2. Checked 2026-10-12. Source code is cited at the version's tag, k6 documentation at
commit [`b5fdef3`](https://github.com/grafana/k6-docs/tree/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x).

**ghz**

1. One method per run: `--call` takes a single name, the run is `runner.Run(cfg.Call, …)` —
   [`cmd/ghz/main.go` L40](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L40),
   [L334](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L334).
2. Load is set by flags or a JSON/TOML file —
   [`cmd/ghz/main.go` L28](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L28).
3. The method description comes from `.proto`, protoset, or, without them, reflection —
   [`runner/requester.go` L79–L113](https://github.com/bojand/ghz/blob/v0.121.0/runner/requester.go#L79-L113).
4. The synchronous worker waits for the call; with `--async` it starts a goroutine per call —
   [`runner/worker.go` L47–L70](https://github.com/bojand/ghz/blob/v0.121.0/runner/worker.go#L47-L70);
   the flag — [`cmd/ghz/main.go` L78](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L78).
5. A call's duration is `EndTime − BeginTime` from gRPC stats events —
   [`runner/stats_handler.go` L44](https://github.com/bojand/ghz/blob/v0.121.0/runner/stats_handler.go#L44).
6. Only calls without an error go into the percentiles and the histogram unless `--count-errors`
   is set —
   [`runner/reporter.go` L259–L264](https://github.com/bojand/ghz/blob/v0.121.0/runner/reporter.go#L259-L264),
   [`cmd/ghz/main.go` L218](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L218).
   The maintainer's answer: "the stats are only based on successful / OK responses" —
   [issue 248](https://github.com/bojand/ghz/issues/248).
7. `--rps` and the `--load-schedule` profiles const/step/line —
   [`cmd/ghz/main.go` L82–L109](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L82-L109).
8. A non-zero exit code only when the run could not start or the report could not be written;
   there are no threshold flags. The full flag list —
   [`cmd/ghz/main.go` L28–L279](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L28-L279),
   the exit — [L334–L342, L385–L392](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L385-L392).
9. The call template data (`CallData`) has no field for a previous call's response —
   [`runner/calldata.go` L32–L46](https://github.com/bojand/ghz/blob/v0.121.0/runner/calldata.go#L32-L46).

**k6**

10. Several scenarios in one script, each with its own executor and its own JavaScript function
    (`exec`) —
    [Scenarios](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/_index.md#L13-L18),
    [options table, `exec`](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/_index.md#L90).
11. The open model exists only in the arrival-rate executors; `vus` with `duration` is the
    constant-vus executor, the closed model —
    [Open and closed models](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/concepts/open-vs-closed.md#L9-L13),
    [Options, `duration`](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/k6-options/reference.md#L329).
12. With no free VU an iteration is dropped and counted in `dropped_iterations` —
    [Dropped iterations](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/concepts/dropped-iterations.md#L10-L25).
13. `grpc_req_duration` is written on every call's End event as `EndTime − BeginTime`, the status
    as a tag —
    [`internal/lib/netext/grpcext/conn.go` L282–L295](https://github.com/grafana/k6/blob/v2.3.0/internal/lib/netext/grpcext/conn.go#L282-L295).
14. `reflect: true` in the `connect` params —
    [Client.connect](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-connect-connect-address-params.md#L26).
15. `Client.load` reads `.proto` files —
    [Client.load](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-load.md#L11-L18).
16. The `ramping-arrival-rate` executor changes the rate by stages —
    [Ramping arrival rate](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/executors/ramping-arrival-rate.md#L8-L16).
17. A failed threshold gives a non-zero exit code, 99 —
    [Thresholds](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/thresholds.md#L70-L80),
    [`errext/exitcodes/codes.go` L18](https://github.com/grafana/k6/blob/v2.3.0/errext/exitcodes/codes.go#L18).
18. `Client.invoke` returns the response object to the script —
    [Client.invoke](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-invoke.md#L24-L28).
19. A breakpoint test is `ramping-arrival-rate` plus a threshold with `abortOnFail` —
    [Breakpoint testing](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/testing-guides/test-types/breakpoint-testing.md#L66-L75),
    [L102–L106](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/testing-guides/test-types/breakpoint-testing.md#L102-L106).

**JMeter**

20. The JMeter 5.6.3 component reference has no gRPC sampler (the word gRPC does not occur in it),
    no percentile thresholds and no breakpoint search —
    [`xdocs/usermanual/component_reference.xml`](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml).
21. The jmeter-grpc-request plugin v2.0.2, a continuation of the archived zalopay-oss one; JMeter
    Plugins Manager installs it: unary calls only, the input is a folder of `.proto` files —
    [README L19, L28](https://github.com/bakthava/jmeter-grpc-request/blob/v2.0.2/README.md#L19),
    [Plugins Manager](https://github.com/undera/jmeter-plugins/blob/cdaa5ac7d27f081787f57d69133e8c34b14b133f/site/dat/repo/various.json#L1752-L1826).
22. One plan can hold several thread groups; the Constant Throughput Timer and the Open Model
    Thread Group each set a rate per group —
    [`component_reference.xml` L5153](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5153),
    [L5228](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5228),
    [L6517–L6521](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6517-L6521).
23. The Open Model Thread Group "is experimental"; the schedule `rate(…) random_arrivals(…)
    rate(…)` changes the rate linearly; the ordinary Thread Group is a pool of threads with an
    iteration count —
    [`component_reference.xml` L6519–L6526](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6519-L6526),
    [L6619–L6621](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6619-L6621).
24. Elapsed time is measured "from just before sending the request to just after the last
    response has been received" —
    [`xdocs/usermanual/glossary.xml` L34–L35](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/glossary.xml#L34-L35).
25. In the Aggregate Report and in the HTML report every call's time is counted, failed calls
    too —
    [`SamplingStatCalculator.java` L185](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/visualizers/SamplingStatCalculator.java#L185),
    [`StatisticsSummaryConsumer.java` L79–L88](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/report/processor/StatisticsSummaryConsumer.java#L79-L88).
26. In the launcher `System.exit(1)` happens only on a startup error, plus a property that is off
    by default for failing to stop threads; the Duration Assertion marks a single response as
    failed, not the run —
    [`JMeter.java` L519](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/JMeter.java#L519),
    [`get-started.xml` L476–L478](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/get-started.xml#L476-L478),
    [`component_reference.xml` L4695–L4698](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L4695-L4698).
27. The JSON Extractor and Regular Expression Extractor post-processors take values from a
    sampler's response and store them in a variable —
    [`component_reference.xml` L6334](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6334),
    [L5797](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5797).
28. The plan lives in a `.jmx` file and runs with `jmeter -n -t plan.jmx` —
    [`xdocs/usermanual/get-started.xml` L474](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/get-started.xml#L474).

