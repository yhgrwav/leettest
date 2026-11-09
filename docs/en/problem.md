# What problem does LeetTest solve?

[← Back to docs](README.md)

> Translated from [docs/ru/problem.md](../ru/problem.md) at b7a685c, 2026-10-12. If they differ,
> the Russian one is right.

A load test exists to answer one question: **at what load does the service stop coping, and
where is the bottleneck.** That answer goes to the developers — to fix, to optimise, to decide
whether the release ships.

For the answer to be true, two things are needed.

**Load of several methods.** A service in production receives calls to different methods at the
same time: for example many balance requests, fewer transfers, very few sign-ups. Methods can
share a connection pool, database locks and a cache, so load on each method alone can give a
different result than the mix (the example is invented, not a measurement). One `ghz` run loads one
method ([source](why.md#sources)); LeetTest describes the whole load: a list of methods, each at its
own RPS, one run, one report.

**Numbers with visible limits.** A load tool produces a number that decisions are made on. A number
with a measurement error looks the same as a correct one, so the report shows the requested load
next to the achieved load and names the waits on the client side. The kinds of measurement error
and how LeetTest handles them are on [a separate page](pitfalls.md).

What the tool does not do: it sees the service only from the outside — latency, throughput,
errors. It does not see the service's CPU or memory and does not guess them from its own data.
Those come from the service's monitoring and are lined up with the load over time.
