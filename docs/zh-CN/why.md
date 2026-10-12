# 为什么选择它？

[← 返回文档](README.md)

> 译自 [docs/ru/why.md](../ru/why.md)，对应 0ca9b60，2026-10-12。如有出入，以俄文版为准。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../en/why.md)。

LeetTest 建立在一条原则上：压测结果只有在可以毫无保留地信任时才有价值。
工具中的其他一切都由此而来。

优先级顺序为：**测量的准确性**，其次**易用性**，再次**速度**。发生冲突时第一条胜出——
损害准确性的优化会被否决。

## 与 ghz、k6 和 JMeter 的对比

🟢 有 · 🔴 没有 · 🟡 部分支持或需要额外工作（单元格中的词说明具体是哪种）。方括号中的数字指向
页面底部的[来源](#来源)。JMeter 为 5.6.3 版加 gRPC 插件：JMeter 本身不支持 gRPC [20][21]。

**能运行什么**

| 检查项 | ghz | k6 | JMeter | LeetTest |
|---|---|---|---|---|
| 无需插件的 gRPC | 🟢 [1] | 🟢 [13] | 🟡 插件 [20][21] | 🟢 |
| 不用 `.proto`，通过 server reflection 调用方法 | 🟢 [3] | 🟢 [14] | 🔴 需要 `.proto` [21] | 🟢 |
| 没有 reflection 的服务：schema 来自 `.proto` 或 protoset | 🟢 [3] | 🟢 [15] | 🟢 [21] | 🔴 [roadmap](roadmap.md#下一个版本v03) |
| 一次运行中多个方法，各有自己的 RPS | 🔴 一个方法 [1] | 🟡 脚本中的场景 [10] | 🟡 线程组 [22] | 🟢 |
| 无需自己写代码即可描述负载 | 🟢 标志、JSON、TOML [2] | 🔴 JavaScript [10] | 🟢 `.jmx` 计划 [28] | 🟢 YAML |
| 加压到目标 RPS，然后保持平稳负载 | 🟢 [7] | 🟢 [16] | 🟡 实验性 [23] | 🔴 [roadmap](roadmap.md#之后) |
| 用于 CI 的阈值和退出码 | 🔴 [8] | 🟢 [17] | 🔴 [20][26] | 🔴 [roadmap](roadmap.md#之后) |
| 响应字段进入下一个请求 | 🔴 [9] | 🟡 脚本 [18] | 🟢 后置处理器 [27] | 🔴 [roadmap](roadmap.md#之后) |
| 寻找目标崩溃时的负载 | 🔴 [8] | 🟡 脚本 [19] | 🔴 [20] | 🟢 分阶梯 |

**如何测量**

| 检查项 | ghz | k6 | JMeter | LeetTest |
|---|---|---|---|---|
| 目标变慢时负载不会下降（open model） | 🟡 `--async`，非默认 [4] | 🟡 专用 executor，非默认 [11] | 🟡 实验性，非默认 [23] | 🟢 唯一的模型 |
| 生成器自身的延迟计入延迟 | 🔴 否：从发送起算 [5] | 🔴 否：从发送起算，未发送的计入 `dropped_iterations` [12][13] | 🔴 否：从发送起算 [24] | 🟢 |
| 失败和被超时中断的调用计入百分位 | 🟡 `--count-errors`，非默认 [6] | 🟢 [13] | 🟢 [25] | 🟢 失败的按类别各有自己的百分位，被中断的作为下界 |

默认的 open model、计入生成器延迟、寻找失效点，这几行是 ghz、k6、JMeter 都没有 🟢 的地方。
LeetTest 目前还没有：加压到目标 RPS、用于 CI 的阈值、调用串联、无 reflection 运行；
顺序见 [roadmap](roadmap.md)。

## 复现生产的负载

工具按现实中的样子描述负载：多个方法同时运行，每个方法有自己的速率和时长，
全部在一次运行、一份报告中。连接池争用、数据库锁、缓存争夺正是在调用混合时才显现——
逐个方法测量时永远看不到这些效应。

## 抗失真的测量

延迟从请求被计划发送的时刻算起，而不是从它实际能发出的时刻算起。这样能捕获完整的延迟，
包括在压测机一侧排队的时间。目标卡住一秒时，工具报告 `1005ms`——真实用户会看到的延迟——
而不是 `5ms`，后者只反映恢复后响应有多快。

负载按时间表施加：无论服务是否跟得上，请求的 RPS 都会保持。

被超时中断的请求不会用一个数字代替。我们只知道它耗时超过了超时时间；如果这类请求
可能占据某个百分位的位置，报告打印的不是数字而是下界：`p99 >2.0s`。

## 无需准备

有服务地址和方法名就能启动。工具通过 gRPC server reflection 从服务本身获取方法描述：
没有 `.proto` 文件，没有代码生成，没有构建步骤。请求体在配置中用普通 YAML 书写，
启动前按服务的 schema 构建。

## 声明式描述

负载由配置文件定义，而不是由程序定义。文件与服务代码放在一起，经过代码评审，
半年后依然易读。它不需要调试，自身也不会成为测量误差的来源。

## 对使用者的用心

错误信息会指出位置和原因。配置中的未知键和零速率都视为错误，而不是被默默接受：
一个一个请求都没发出的运行不应看起来是成功的。运行状态实时可见——当前速率、在途请求、
错误占比、百分位。

界面质量与测量准确性同等重要：含义不清的错误提示被视为缺陷。

## 来源

版本：ghz v0.121.0（最新发布），k6 v2.3.0，JMeter 5.6.3 加插件 jmeter-grpc-request v2.0.2。
核对于 2026-10-12。源码取自版本标签，k6 文档取自提交
[`b5fdef3`](https://github.com/grafana/k6-docs/tree/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x)。

**ghz**

1. 每次运行一个方法：`--call` 接受一个名称，启动是 `runner.Run(cfg.Call, …)` ——
   [`cmd/ghz/main.go` L40](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L40)，
   [L334](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L334)。
2. 负载通过标志或 JSON/TOML 文件设定 ——
   [`cmd/ghz/main.go` L28](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L28)。
3. 方法描述取自 `.proto`、protoset，没有它们时通过 reflection ——
   [`runner/requester.go` L79–L113](https://github.com/bojand/ghz/blob/v0.121.0/runner/requester.go#L79-L113)。
4. 同步 worker 等待调用完成，加 `--async` 则每次调用启动一个 goroutine ——
   [`runner/worker.go` L47–L70](https://github.com/bojand/ghz/blob/v0.121.0/runner/worker.go#L47-L70)；
   该标志 —— [`cmd/ghz/main.go` L78](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L78)。
5. 调用时长是 gRPC stats 事件中的 `EndTime − BeginTime` ——
   [`runner/stats_handler.go` L44](https://github.com/bojand/ghz/blob/v0.121.0/runner/stats_handler.go#L44)。
6. 在未设置 `--count-errors` 时，只有无错误的调用进入百分位和直方图 ——
   [`runner/reporter.go` L259–L264](https://github.com/bojand/ghz/blob/v0.121.0/runner/reporter.go#L259-L264)，
   [`cmd/ghz/main.go` L218](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L218)。
   维护者的回答：“the stats are only based on successful / OK responses” ——
   [issue 248](https://github.com/bojand/ghz/issues/248)。
7. `--rps` 以及 `--load-schedule` 的 const/step/line 模式 ——
   [`cmd/ghz/main.go` L82–L109](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L82-L109)。
8. 仅当无法启动运行或无法写入报告时才返回非零退出码；没有阈值标志。完整的标志列表 ——
   [`cmd/ghz/main.go` L28–L279](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L28-L279)，
   退出 —— [L334–L342](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L334-L342)，[L385–L392](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L385-L392)。
9. 调用模板的数据（`CallData`）不包含上一次调用的响应 ——
   [`runner/calldata.go` L32–L46](https://github.com/bojand/ghz/blob/v0.121.0/runner/calldata.go#L32-L46)。

**k6**

10. 一个脚本中有多个场景，每个有自己的 executor 和自己的 JavaScript 函数（`exec`）——
    [Scenarios](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/_index.md#L13-L18)，
    [选项表，`exec`](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/_index.md#L90)。
11. open model 只存在于 arrival-rate 类 executor 中；`vus` 加 `duration` 是 constant-vus
    executor，即 closed model ——
    [Open and closed models](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/concepts/open-vs-closed.md#L9-L13)，
    [Options, `duration`](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/k6-options/reference.md#L329)。
12. 没有空闲 VU 时，该次迭代被丢弃并计入计数器 `dropped_iterations` ——
    [Dropped iterations](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/concepts/dropped-iterations.md#L10-L25)。
13. `grpc_req_duration` 在每次调用的 End 事件上记录为 `EndTime − BeginTime`，状态作为标签 ——
    [`internal/lib/netext/grpcext/conn.go` L282–L295](https://github.com/grafana/k6/blob/v2.3.0/internal/lib/netext/grpcext/conn.go#L282-L295)。
14. `connect` 参数中的 `reflect: true` ——
    [Client.connect](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-connect-connect-address-params.md#L26)。
15. `Client.load` 读取 `.proto` ——
    [Client.load](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-load.md#L11-L18)。
16. executor `ramping-arrival-rate` 按阶段改变速率 ——
    [Ramping arrival rate](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/executors/ramping-arrival-rate.md#L8-L16)。
17. 阈值未通过时返回非零退出码 99 ——
    [Thresholds](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/thresholds.md#L70-L80)，
    [`errext/exitcodes/codes.go` L18](https://github.com/grafana/k6/blob/v2.3.0/errext/exitcodes/codes.go#L18)。
18. `Client.invoke` 把响应对象返回给脚本 ——
    [Client.invoke](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-invoke.md#L24-L28)。
19. 断点测试 —— `ramping-arrival-rate` 加带 `abortOnFail` 的阈值 ——
    [Breakpoint testing](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/testing-guides/test-types/breakpoint-testing.md#L66-L75)，
    [L102–L106](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/testing-guides/test-types/breakpoint-testing.md#L102-L106)。

**JMeter**

20. JMeter 5.6.3 的组件参考中没有 gRPC sampler（其中没有 gRPC 一词），没有按百分位的阈值，
    也没有寻找失效点的功能 ——
    [`xdocs/usermanual/component_reference.xml`](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml)。
21. 插件 jmeter-grpc-request v2.0.2 是已归档的 zalopay-oss 的延续，由 JMeter Plugins Manager
    安装：仅支持 unary 调用，输入是含 `.proto` 的文件夹 ——
    [README L19, L28](https://github.com/bakthava/jmeter-grpc-request/blob/v2.0.2/README.md#L19)，
    [Plugins Manager](https://github.com/undera/jmeter-plugins/blob/cdaa5ac7d27f081787f57d69133e8c34b14b133f/site/dat/repo/various.json#L1752-L1826)。
22. 一个计划中有多个线程组；Constant Throughput Timer 和 Open Model Thread Group
    为每个组设定各自的速率 ——
    [`component_reference.xml` L5153](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5153)，
    [L5228](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5228)，
    [L6517–L6521](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6517-L6521)。
23. Open Model Thread Group “is experimental”；时间表 `rate(…) random_arrivals(…) rate(…)`
    线性地改变速率；普通 Thread Group 是带迭代次数的线程池 ——
    [`component_reference.xml` L6519–L6526](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6519-L6526)，
    [L6619–L6621](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6619-L6621)。
24. Elapsed time —— “从发送请求之前的时刻到收到最后一个响应” ——
    [`xdocs/usermanual/glossary.xml` L34–L35](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/glossary.xml#L34-L35)。
25. 在 Aggregate Report 和 HTML 报告中，每次调用的时间即使出错也会计入 ——
    [`SamplingStatCalculator.java` L185](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/visualizers/SamplingStatCalculator.java#L185)，
    [`StatisticsSummaryConsumer.java` L79–L88](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/report/processor/StatisticsSummaryConsumer.java#L79-L88)。
26. 启动代码中的 `System.exit(1)` 仅用于启动错误，另有一个默认关闭、用于停止线程的属性；
    Duration Assertion 把单个响应标记为失败，而不是整个运行 ——
    [`JMeter.java` L519](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/JMeter.java#L519)，
    [`get-started.xml` L476–L478](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/get-started.xml#L476-L478)，
    [`component_reference.xml` L4695–L4698](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L4695-L4698)。
27. 后置处理器 JSON Extractor 和 Regular Expression Extractor 从 sampler 的响应中取值并保存到变量 ——
    [`component_reference.xml` L6334](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6334)，
    [L5797](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5797)。
28. 计划保存在 `.jmx` 文件中，用 `jmeter -n -t plan.jmx` 运行 ——
    [`xdocs/usermanual/get-started.xml` L474](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/get-started.xml#L474)。
