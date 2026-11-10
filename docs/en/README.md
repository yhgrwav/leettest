# <img src="../../assets/icon.png" width="24" alt="" align="top"> Documentation

[Русский](../ru/) · [English](../en/) · [Deutsch](../de/) · [简体中文](../zh-CN/)

[← Back to home](../../README.md)

> Translated from [docs/ru/README.md](../ru/README.md) at dc7fd95, 2026-11-10. If they differ, the
> Russian one is right.

LeetTest answers the question people bring to a load test: **at what load does the service stop
coping, and where is the bottleneck.** The report shows the requested load next to the achieved
load and names the waits on the client side, so the numbers can be judged for how much is the
service and how much is the generator. Beyond that: several methods in one run, each at its own
RPS; a simple CLI with no `.proto`, codegen or scripts. The project's priorities, in order:
measurement correctness, usability, speed. What works today is in the table below, what comes next
is in the [roadmap](roadmap.md).

| Question | |
|---|---|
| How do I make a first run? | [First run](guide/first-run.md) |
| How do I set the address, TLS, headers and a token? | [Connection](guide/connection.md) |
| How do I set the methods, the load and the duration? | [Load](guide/load.md) |
| How do I read the report, the exit codes and the JSON, and compare two runs? | [Report](report.md) |
| What does every config field and flag mean? | [Reference](reference.md) |
| What questions does LeetTest answer? | [Read](questions.md) |
| What problem does LeetTest solve? | [Read](problem.md) |
| How does it differ from ghz, k6 and JMeter? | [Read](why.md) |
| Which load-testing problems does it fix? | [Read](pitfalls.md) |
| What does it not do, and where do its numbers stop holding? | [Read](limitations.md) |
| What will not change within version 1.x? | [Read](stability.md) |
| How do I run LeetTest in CI and fail the build on a missed limit? | [Read](ci.md) |
| What is call chaining and why does it matter? | [Read](chaining.md) |
| Will there be a paid version? | [Read](commercial.md) |
| Where do I ask a question or leave feedback? | [Read](feedback.md) |
| What comes next? | [Read](roadmap.md) |

## Contributing

Issues and discussions are welcome. Pull requests are accepted after signing the [CLA](../../CLA.md)
— one line as a comment in the PR, checked automatically. How to start — in
[CONTRIBUTING.md](../../CONTRIBUTING.md): a path for people and a path with an AI agent (for an
agent that changes code, [AGENTS.md](../../AGENTS.md)). The CLA is a license, not a transfer of
rights: the copyright stays with you.

## Support the project

[Address and QR code](../support.md).

## License

Apache License 2.0 — see [LICENSE](../../LICENSE).
