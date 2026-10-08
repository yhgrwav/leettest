<div align="center">

<h1><img src="../../assets/logo.png" width="360" alt="LeetTest"></h1>

**不说谎的 gRPC 压测。**

[Русский](../ru/) · [English](../en/) · [Deutsch](../de/) · [简体中文](../zh-CN/)
[![CI](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/yhgrwav/leettest.svg)](https://pkg.go.dev/github.com/yhgrwav/leettest)
[![Go version](https://img.shields.io/github/go-mod/go-version/yhgrwav/leettest)](../../go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](../../LICENSE)

</div>

---

> 这是 `main` 分支：这里有尚未发布的内容，标记为“未发布”。已发布版本的文档见[最新发布](https://github.com/yhgrwav/leettest/releases/latest)。

> 译自 [README.md](../../README.md)，对应 67f8732，2026-10-08。如有出入，以俄文版为准。
> 另附中文文档目录，俄文原文中没有。

> **早期阶段。** 已可用：对真实服务施加 unary 负载，一次运行中多个方法各自设定 RPS，请求体来自
> 配置，控制台报告和供脚本使用的 JSON。尚未提供：爬坡、CI 的通过/失败阈值、指标导出——
> **[接下来会有什么 →](roadmap.md)**。缺少某项功能——
> [提交 issue](https://github.com/yhgrwav/leettest/issues/new/choose)。下文只描述已经可用的功能。

本工具回答人们做压测时要问的问题：**服务在多大负载下开始扛不住，瓶颈在哪里。** 它施加接近生产的
负载——多个方法同时进行，每个方法有自己的 RPS——并且在服务退化时也保证测出的数字不说谎。不需要
`.proto`，不需要代码生成，不需要脚本。

优先级依次为：数字不说谎；工具好用——含糊的错误提示与错误的数字同样是缺陷；压测机本身绝不成为瓶颈。

## 安装

```console
$ go install github.com/yhgrwav/leettest/cmd/leettest@latest
```

需要 Go 1.26 或更高版本。预编译的二进制文件在 Releases 页面。

**在哪里运行。** 在 Linux 上、紧挨着目标：同一网络、同一集群、CI runner。压测机与目标之间的一切
都会计入延迟，看起来像目标的耗时：在我们的测试台上以 1000 RPS 压测，经 Docker Desktop 端口转发
p99 为 38 ms，在 Docker 网络内为 2 ms。精确的调用调度只在 Linux 上提供；在 CPU 配额低于两个核的
容器里，偶尔会出现最长为配额周期（通常 100 ms）的启动延迟，报告会显示出来。在 L4 负载均衡器后面
只有一个后端承受负载——[见下文](#阅读报告)。在 Windows 上时钟步长约 0.5 ms，针对快速服务的运行
会被判为无效。

**[快速上手 →](quickstart.md)**——在参考测试台上演示全部能力：包含所有字段的配置、阅读报告、
目标的其他行为、CI。通过 AI 智能体工作——把 [AGENTS.md](../../AGENTS.md) 交给它。

## 运行

```yaml
app:
  target:
    ip: localhost
    port: 50051
  tls: false

load:
  warmup: 5s
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m

    - method: wallet.v1.WalletService/Transfer
      rps: 50
      duration: 1m
```

```console
$ leettest -c leettest.yaml
```

方法名使用全名 `package.Service/Method`。工具通过 gRPC server reflection 从服务获取方法的结构，
因此不需要 `.proto`。配置、地址、方法和请求体都在启动前检查：任何错误都意味着退出码 1，且不会向
目标发出任何调用。前 `warmup` 秒不计入统计。一个方法的每次调用都使用同一个请求体：对于带幂等键的
写操作，测到的是重复请求的路径，而不是创建记录。每次调用使用不同的请求：用 `dataset` 指定文件
（未发布），每行一个 JSON，按顺序循环使用。所有字段、TLS、请求头、请求体和命令行参数都在
**[参考手册](reference.md)**中。

在终端中运行时全屏显示，按 `q` 停止。没有终端时（CI、重定向输出）——每秒一行进度：

```
32.0s  sent 25600  rps 800  in-flight 47  failed 51  not-sent 0  p99 43ms
```

报告输出到 stdout，进度和错误输出到 stderr。没有屏幕时（例如通过 ssh）：`-plain`（未发布）。

## 阅读报告

结束时——按方法给出报告：已发送、失败、`sent/s`、p50/p90/p95/p99。

- 调用从其**计划**时刻开始计时：如果目标或压测机让它延迟了，这段时间会计入延迟。
- `sent/s` 是已发送调用数除以发送时间：运行末尾挂起的目标不会拉低这个数字。
- 如果被超时截断的调用可能占据某个百分位的位置，该百分位会以**下界**形式输出：`>2.0s`。这不是
  数值，而是“至少”。
- 带 `dataset`（未发布）的方法下面有一行 `data:`：文件中的请求用了多少个，最常用的那个发出了
  多少次。这并不表示目标看到了这么多不同的请求。
- 默认情况下负载通过**一条连接**发出，并落在**一个后端**上：在 L4 负载均衡器（Kubernetes
  ClusterIP、NLB）后面，以及 DNS 返回多个地址时都是如此。“扛不住 X”说的是那个后端而不是整个
  服务，报告不会显示这一点。要给多个后端施压，请设置 `app.connections`（未发布）：报告中的
  `Connections:` 块每条连接一行——地址、调用数、失败占比和 p99——落后的后端一目了然。按单个
  请求分发的 L7 负载均衡器（Envoy、gRPC ingress）即使只有一条连接也会分发。报告会输出连接重建的
  次数，以及目标声明的并发流上限。

**类别。** 除成功之外的应答按行拆分，每行有自己的百分位。状态可能来自目标前面的代理而不是目标
本身：没有可用后端的 nginx 会返回 `UNAVAILABLE`，客户端无法区分两者。

- `request error`：无论多大速率调用都会失败——请求有误，或目标、代理认为请求过大。
- `overload`：状态表示过载或不可用，即 `RESOURCE_EXHAUSTED` 或 `UNAVAILABLE`。这是状态本身的
  说法，不是对目标的诊断。
- `failure`：状态表示调用出错：`INTERNAL`、`UNKNOWN`、`DATA_LOSS`、对端发来的 `CANCELLED`、
  `ABORTED`（并发修改冲突，而不是容量不足）。
- `bad response`：收到了应答，但客户端不接受：超过 `app.max_response_size`，或使用了客户端
  未声明的编码压缩。
- `client error`：客户端自己拒绝发送：请求无法编码，或编解码器、拦截器出错。

从未到达目标的调用计为失败，但不进入百分位。已经发出却没有收到状态的请求——对端重置了流或断开了
连接——单独计为 `cut off`：目标或代理可能已经处理了它，对写操作来说这是检查重复的理由。被我们
自己停止（Ctrl+C、SIGTERM）截断的调用记为 `aborted`，与代码无关：这不是目标的拒绝。

| 代码 | 状态经网络传回 | 代码由客户端设置，请求已发出 | 代码由客户端设置，请求未发出 |
|---|---|---|---|
| `INVALID_ARGUMENT`、`NOT_FOUND`、`ALREADY_EXISTS`、`PERMISSION_DENIED`、`UNAUTHENTICATED`、`FAILED_PRECONDITION`、`OUT_OF_RANGE`、`UNIMPLEMENTED` | `request error` | `cut off` | `client error` |
| `RESOURCE_EXHAUSTED` | `overload`；grpc-go 的 “larger than max” 是 `request error` | `cut off`；超过我们上限的应答是 `bad response` | `client error` |
| `UNAVAILABLE` | `overload` | `cut off`；目标在处理前拒绝的流是 `overload` | `unreachable`；同样 |
| `CANCELLED`、`UNKNOWN`、`INTERNAL`、`DATA_LOSS`、`ABORTED` | `failure` | `cut off`；无法解压的应答是 `bad response` | `client error` |
| `DEADLINE_EXCEEDED` | 超时 | 超时 | 超时，未发送 |

请求过大是根据 grpc-go 的错误文本识别的。基于其他实现的目标（Envoy、Java）措辞不同，它的拒绝
会落入 `overload` 而不是 `request error`。

被拒绝的流上的 `UNAVAILABLE` 是 grpc-go 对目标发来的 RST_STREAM REFUSED_STREAM 的翻译：代码由
客户端设置，但拒绝来自目标，所以它在 “sent by the target” 一行。目标没有处理这样的流（RFC 9113
§8.7）。

报告下方，失败的调用按 gRPC 代码分两行列出。“sent by the target”——状态经网络传回，来自目标或
代理。“set by the client”——代码由客户端自己设置：没人应答、流被重置、我们的截止时间到了，或客户端
拒绝了应答。来自目标的 `DEADLINE_EXCEEDED` 通常就是我们自己的截止时间：它通过 `grpc-timeout`
请求头传给目标。类别说明是谁的问题，代码说明在目标日志里找什么。

**客户端侧的等待。** 延迟包含调用在我们这一侧等待的一切：压测机迟到、等待连接、等待空闲的流。
如果去掉这些等待后至少一个方法的 p99 下降 10% 或更多，或有调用根本没发出，报告会给出结论，指出
p99 尾部最常见的原因，并输出去掉等待后的 p99——即调用在目标处花费的时间。如果原因是流用尽，则
目标在“上限 × 连接数”之上的容量没有被测到。如果调用先等待了解析器，调用方的拦截器也会计入连接
等待，因此去掉等待后的 p99 可能略微偏低。从未发出的调用按原因拆分。低于 10% 的偏移不是结论，而是
附带数字的提示；当 p99 是下界时，偏移未知，也就没有提示。

**时钟。** 工具在运行前后测量主机时钟的步长。步长明显时输出一行
`clock step 502us on this host: every latency and wait is +/- 502us`。在 Linux 上步长为几十纳秒，
不会有这一行。

## 运行何时无效

有报告，但其中的数字与目标所受的负载无关（退出码 2），如果：

- **触及了 `-max-in-flight` 上限。** 启动前会检查超时和上限，确保挂起的目标不会触及它；触及即
  说明压测机 CPU 不足。
- **某个方法所有被测量的调用都是 `request error`、`client error` 或 `bad response`。** 提示会
  指出方法、原因和应对办法。
- **主机时钟比某个方法 p50 的四分之一还粗**，或运行期间步长增长到 1 µs 及以上（`clock_step`）。
  请从 Linux 上测量。

## 退出码

目前还没有“扛得住 / 扛不住”的阈值，因此运行到结束就返回 `0`，无论目标如何应答：**`0` 并不表示
“服务健康”**。退出码有优先级：无效运行（`2`）高于未完成运行（`3`）；输出了报告的停止总是 `3`。

| 代码 | 发生了什么 |
|---|---|
| `0` | 计划执行完毕，报告完整 |
| `1` | 运行没有发生：命令行参数、配置、连接。没有报告 |
| `2` | 运行无效（见上文）。有报告，但数字与负载无关 |
| `3` | 运行在计划结束前停止。有报告，只涵盖已完成的部分 |
| `130` | Ctrl+C 后中止，没有报告 |
| `143` | SIGTERM 后中止，没有报告 |

代码 `4` 预留给阈值。停止如何工作见[参考手册](reference.md#停止)。

## 供脚本和 CI 使用的 JSON

使用 `-output json` 时，stdout 只输出一个 JSON 对象和一个换行；其他内容都输出到 stderr，因此
stdout 可以直接交给 `jq`。只有运行确实发生时（代码 `0`、`2`、`3`）才输出该对象；代码 `1`、`130`
和 `143` 时 stdout 为空。字段 `outcome`（`complete`、`invalid` 或 `incomplete`）始终与退出码一致。

结构按 `schema_version` 版本化，目前为 `1`，它是一份契约；屏幕文本不是：请解析 JSON，而不是
屏幕。规则：

- 新增字段不改变版本；重命名、删除字段或改变其类型会提高 `schema_version`；
- 枚举的取值（`outcome`、`unchecked[].reason`、`failure_codes` 中的代码）可以增加而不改变版本；
- 使用方必须跳过未知字段，并在遇到未知枚举值时不出错。

单位写在字段名里：`_us` 是整数微秒，`_s` 是整数秒；速率 `rps` 是唯一的小数。运行未产生的值是
`null`，不是 `0`。百分位是一个对象 `{"us": 1234, "lower_bound": false}`：`lower_bound: true`
时它是下界，而不是数值。延迟是直方图在 3 位有效数字下的取值（误差不超过 0.1%），计数是精确的。
时间从 `started_at`（RFC 3339，UTC）即计划开始时刻算起，包括预热；`duration_us` 也包括预热。
某一秒的 `in_flight` 是该秒结束时在途的调用数。`failure_codes` 中的代码是规范名称
（`UNAVAILABLE`）。核对方式：运行的总数是各方法之和，一个方法各秒的 `Σ begun` 加上
`outside_timeline` 就是该方法的全部调用，包括预热。

结论以字段而不是文本给出：`invalid_reasons`（`in_flight_cap`、`nothing_measured`、
`clock_step`）、`methods[].invalid_reason`（`request_error`、`client_error`、`bad_response`、
`mixed` 或 `null`）、`tail_wait_cause`（`generator`、`stream`、`connection` 或 `null`），以及
`client_waits` 中按原因给出的数字。`notes` 和 `unchecked[].error` 是给人看的文本，措辞可以随意
修改：不要解析它们。

目标的沉默：`methods[].silent_from_s` 是目标从哪一秒起不再应答任何已发出的调用，
`methods[].silent_sent_rps` 是在那之前一秒发出了多少调用，`methods[].silent_planned_rps_low`
和 `_high` 是那一秒各阶段的计划速率。没有沉默时全部为 `null`；那一秒没有阶段在运行时，计划速率
也为 `null`。`planned_rps_low` 和 `_high` 是整个计划的速率。

文本报告只以 ASCII 输出。方法名或目标错误文本中 ASCII 以外的字符输出为 `\uXXXX`（超过 U+FFFF
时为代理对，与 JSON 相同）。JSON 中的 `notes` 携带同样转义过的文本。

### 崩溃点搜索

调用中用 `load.breakpoint` 段（未发布）代替 `rps` 和 `duration`：LeetTest 分台阶提高负载，并指出
目标撑住了哪一级、在哪一级崩溃。只能有一个调用，且不能有 `rps`、`duration` 和 `load.warmup`。

```yaml
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      timeout: 500ms
  breakpoint:
    from: 100         # 第一级，rps
    to: 2000          # 不会超过这个值
    factor: 1.25      # 下一级 = 上一级 × factor；或 step: 100 —— 每级多这么多 rps
    settle: 5s        # 一级的开头部分，不计入判定；小于 hold 的一半
    hold: 30s         # 每级的时长
    p99_limit: 200ms  # 可选；不设时，p99 超过最好的已撑住台阶的 3 倍即判该级崩溃
```

一级在至少 1% 的调用失败或 p99 越过界线时判为崩溃。崩溃的台阶会在停顿后重复，并且除第一级外，
还会先在第一级上做探测：目标必须回到原来的 p99。一级只有在压测机发出了计划的调用时才算撑住——
至少是测量窗口内调用数的 99.9%。否则这是本次运行的上限，而不是目标的上限。在第一级之前，stderr
会有一行 `breakpoint: up to N steps, at most T`：共有多少级，以及搜索最坏情况下要多久。整个搜索
只用一条连接；目标断开它时，该级判为崩溃，不会重连。

结果（JSON 中的 `outcome`）是封闭列表：`broke`（撑住 X，在 Y 崩溃）、`broke_at_first`（在第一级就
崩溃——请从更低处开始）、`held_all`（一直撑到 `to`）、`run_limit`（压测机、某条连接的上限或
`-max-in-flight` 用尽：更高负载下目标的情况未知）、`stopped`（Ctrl+C）、`invalid`（某一级是无效
运行）。原因（`why`）也是封闭列表：`errors`、`p99_limit`、`p99_vs_base`、`connection`、
`no_recovery`、`generator`、`in_flight_cap`、`stream_limit`、`stream_wait`、`clock_step`、
`request_errors` 或 `null`。没有对应值时，`held_rps` 和 `broke_rps` 为 `null`。在搜索模式下 JSON
是另一种对象：`mode: "breakpoint"`，普通运行的字段都不在顶层，每次运行连同其完整报告放在
`breakpoint.runs` 中（未发布；`kind`：`step`、`repeat`、`probe`；`planned_rps` 和 `sent_rps`）。
普通运行写 `mode: "run"`。

退出码：`broke`、`broke_at_first`、`held_all`、`run_limit` 为 `0`——它们是发现；`invalid` 为 `2`；
单次 Ctrl+C 停止为 `3`。

## 尚未提供

从零爬坡到目标 RPS、通过/失败阈值、导出到 Prometheus。会有什么、按什么顺序——
**[计划](roadmap.md)**；你缺少什么——[issue](https://github.com/yhgrwav/leettest/issues/new/choose)。

## 深入了解

**[参考手册 →](reference.md)**——每个配置字段和命令行参数。

| 问题 | |
|---|---|
| LeetTest 解决什么问题？ | [阅读](problem.md) |
| 为什么选这个工具？ | [阅读](why.md) |
| 它解决了压测中的哪些问题？ | [阅读](pitfalls.md) |
| 什么是调用串联，为什么重要？ | [阅读](chaining.md) |
| 商业使用是什么样的？ | [阅读](commercial.md) |
| 在哪里提问或反馈？ | [阅读](feedback.md) |
| 接下来会有什么？ | [阅读](roadmap.md) |

**[在同一测试台上与 ghz 对比 →](compare-ghz.md)**——表格和复现命令。

## 参与贡献

欢迎提交 issue 和参与讨论。签署 [CLA](../../CLA.md) 后接受 pull request——在 PR 中留一行评论
即可，会自动检查。如何开始——见 [CONTRIBUTING.md](../../CONTRIBUTING.md)：一条面向人的路径和一条
借助 AI 智能体的路径，给智能体的是 [AGENTS.md](../../AGENTS.md)。CLA 是许可，不是权利转让：
著作权仍归你所有。

## 许可证

Apache License 2.0——见 [LICENSE](../../LICENSE)。
