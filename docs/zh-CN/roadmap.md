# 接下来会有什么

[← 返回文档](README.md)

> 译自 [docs/ru/roadmap.md](../ru/roadmap.md)，对应 67f8732，2026-10-08。如有出入，以俄文版为准。

> **缺少某项功能？[提交 issue](https://github.com/yhgrwav/leettest/issues/new/choose)**——
> 描述你要解决的问题。没有具体任务的疑问请发到
> [Discussions](https://github.com/yhgrwav/leettest/discussions)。下面的顺序会随用户的需求调整。

这些是计划，不是承诺的日期。目前已经可用的功能见 [README](README.md)。

## 下一个版本（v0.3）

- **`.proto` 与 protoset**：用于没有 server reflection 的服务。

## 之后

- **爬坡**：从零逐步升到目标 RPS。
- **CI 的通过/失败阈值**：p99 或错误占比超过上限时，运行判为失败。
- **[调用串联](chaining.md)**：把一个方法响应中的字段填进另一个方法的请求。
- **长时间运行中刷新 token。**
- **导出指标**到 Prometheus。
