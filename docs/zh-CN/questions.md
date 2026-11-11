# LeetTest 回答哪些问题

[← 返回文档](README.md)

> 译自 [docs/ru/questions.md](../ru/questions.md)，对应 feb2d1d，2026-11-10。如有出入，以俄文版为准。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../en/questions.md)。

报告目前已经能回答的问题。

| 问题 | 报告会显示什么 | 位置 |
|---|---|---|
| 负载多大时服务开始撑不住？ | 崩溃点搜索：目标在哪一档撑住、在哪一档垮掉以及原因——错误、p99、连接、p99 没有回到之前的水平 | [崩溃点搜索](../en/report.md#breaking-point-search) |
| 在接近真实的负载下，每个方法的表现如何？ | 同时压多个方法，各有自己的 RPS；每个方法的 p50、p90、p95、p99 | [负载](../en/guide/load.md) |
| 服务卡住或变慢时，用户看到什么？ | 延迟从调用的计划时刻算起：目标卡住会进入 p99 | [阅读报告](../en/report.md#reading-the-report) |
| 有哪些错误，来自谁？ | 类别：请求错误、过载、故障、被切断、超时；状态由目标（或代理）返回，或由客户端设定 | [阅读报告](../en/report.md#reading-the-report) |
| 有多少调用没有得到应答，为什么？ | 未发出、被切断、超时——分别统计；目标从哪一秒起不再应答 | [阅读报告](../en/report.md#reading-the-report) |
| 负载均衡器后面的所有后端都被压到了吗？ | `app.connections: N`：每个连接一行——地址、调用数、失败占比、p99 | [阅读报告](../en/report.md#reading-the-report) |
| 测量本身可信吗？ | 关于客户端侧等待的结论，以及去掉等待后的 p99；数字与目标无关时，本次运行无效（退出码 2） | [运行何时无效](../en/report.md#when-a-run-is-invalid) |
| 服务在不同数据下表现如何？ | `dataset`：每次调用使用各自的请求，文件的每一行一个 JSON | [参考](reference.md#每次调用不同的请求dataset) |
| 能在 CI 中运行吗？ | 一个结构稳定的 JSON 对象和退出码；`pass` 限值以退出码 `4` 判定“撑住 / 撑不住” | [供脚本和 CI 使用的 JSON](../en/report.md#json-for-scripts-and-ci) |

LeetTest 只看到客户端一侧：目标有多少资源，它不知道，也不去猜。后续计划见[路线图](roadmap.md)。
