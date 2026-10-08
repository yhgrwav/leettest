# What comes next

[← Back to docs](README.md)

> Translated from [docs/ru/roadmap.md](../ru/roadmap.md) at 67f8732, 2026-10-08. If they differ, the Russian
> one is right.

> **Missing something? [Open an issue](https://github.com/yhgrwav/leettest/issues/new/choose)** —
> describe the problem you are solving. Questions without a concrete task go to
> [Discussions](https://github.com/yhgrwav/leettest/discussions). The order below changes with what
> users ask for.

These are plans, not promised dates. What works today is in the [README](README.md).

## Next version (v0.2)

- **Finding the breaking point.** The tool raises the load in steps by itself and names the RPS at
  which the service stopped coping, by a "broke" criterion set in advance.
- **Different data in every call.** The request body changes from call to call instead of one per
  method, so the load does not hit a single cache key or a single database row.
- **Several connections.** Today the generator holds one connection, and behind a load balancer
  one backend gets the load. `connections: N` is coming.

## Later

- **`.proto` and protoset** for services without server reflection.
- **Ramp-up** from zero to the target RPS.
- **Pass/fail thresholds for CI**: the run fails if p99 or the error share is above a limit.
- **[Call chaining](chaining.md)**: a field from one method's reply goes into another's request.
- **Token refresh** during a long run.
- **Metrics export** to Prometheus.
