<div align="center">

<h1><img src="../../assets/logo.png" width="360" alt="LeetTest"></h1>

**找出你的 gRPC 服务在哪里崩溃——用可以信赖的数字。**

[Русский](../ru/) · [English](../en/) · [Deutsch](../de/) · [简体中文](../zh-CN/)

[![CI](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/yhgrwav/leettest.svg)](https://pkg.go.dev/github.com/yhgrwav/leettest)
[![Go version](https://img.shields.io/github/go-mod/go-version/yhgrwav/leettest)](../../go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](../../LICENSE)

</div>

---

> 译自 [README.md](../../README.md)，对应 4c1eb2e，2026-10-29。如有出入，以俄文版为准。
> 另附中文文档目录，俄文原文中没有。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../en/README.md)。

本工具回答人们做压测时要问的问题：**服务在多大负载下开始扛不住，瓶颈在哪里。** 它施加接近生产的
负载——多个方法同时进行，每个方法有自己的 RPS——并且在服务退化时也保证测出的数字不说谎。不需要
`.proto`，不需要代码生成，不需要脚本。

优先级依次为：数字不说谎；工具好用——含糊的错误提示与错误的数字同样是缺陷；压测机本身绝不成为瓶颈。

项目在发展：目前已经可用的功能见下文，接下来的计划见[路线图](roadmap.md)。

这是一次运行的样子：负载进行到一半，服务冻结三秒——屏幕上在途调用不断堆积，报告则统计没有得到
应答的调用。

<img src="../../assets/demo.gif" width="100%" alt="终端中的 LeetTest 运行：负载界面，目标冻结三秒，报告显示没有应答的调用和超过 2 秒的 p99">

LeetTest 回答哪些问题——[列表](questions.md)。

## 安装

Linux 和 macOS（amd64 与 arm64）——一条命令：

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
```

脚本会把最新发布版安装到 `~/.local/bin`，校验校验和；如果该目录不在 `PATH` 中，则打印要写入 shell
配置文件的那一行。Windows——从[发布页面](https://github.com/yhgrwav/leettest/releases/latest)下载压缩包。
或者用 Go 1.26+：

```sh
go install github.com/yhgrwav/leettest/cmd/leettest@latest
```

检查：`leettest -version`。如果 `go install` 之后找不到该命令——
把该目录加入 `PATH`：

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

**在哪里运行：**在 Linux 上、紧挨着目标——同一网络、同一集群或 CI runner。压测机与目标之间的一切
都会计入延迟。详见[参考手册](reference.md#在哪里运行)。

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

```sh
leettest -c leettest.yaml
```

方法名使用全名 `package.Service/Method`。工具通过 gRPC server reflection 从服务获取方法的结构，
因此不需要 `.proto`。没有 reflection 的服务（自 v0.3 起）：结构来自 `.proto` 文件或 protoset——
[`examples/proto.yaml`](../../examples/proto.yaml)，字段 `app.proto` / `app.protoset` 见
[参考手册](reference.md)。配置、地址、方法和请求体都在启动前检查：任何错误都意味着退出码 1，且不会向
目标发出任何调用。压测前检查配置：`leettest -c leettest.yaml -dry-run`——每个调用发送一个请求，
请求和响应显示在屏幕上，没有报告。前 `warmup` 秒不计入统计。从零爬升到 `rps` 用 `load.ramp: 30s`
（[`examples/ramp.yaml`](../../examples/ramp.yaml)）：运行时长为 `ramp` + `duration`，爬升期间的调用同样不计入统计
（`pass` 限值只按保持阶段计算），而 `warmup` 与爬升不同，计入 `duration`。一个方法的每次调用都使用同一个请求体：对于带幂等键的
写操作，测到的是重复请求的路径，而不是创建记录。每次调用使用不同的请求：用 `dataset` 指定文件，
每行一个 JSON，按顺序循环使用（`dataset_order: random` 则按随机顺序，报告中的 seed 可复现该顺序）。所有字段、TLS、请求头、请求体和命令行参数都在
**[参考手册](reference.md)**中。

在终端中运行时全屏显示，按 `q` 停止。没有终端时（CI、重定向输出）——每秒一行进度：

```
32.0s  sent 25600  rps 800  in-flight 47  failed 51  not-sent 0  p99 43ms
```

报告输出到 stdout，进度和错误输出到 stderr。没有屏幕时（例如通过 ssh）：`-plain`。

## 通过 AI 智能体

不想自己写配置——把链接交给你的智能体：它会安装 LeetTest，询问你的服务，并运行负载。

```text
Read https://raw.githubusercontent.com/yhgrwav/leettest/main/AGENTS.md and load-test my gRPC service
```

它所依据的指南是 [AGENTS.md](../../AGENTS.md)。

## 阅读报告

结束时——按方法给出报告：已发送、失败、`sent/s`、p50/p90/p95/p99。

- 调用从其**计划**时刻开始计时：如果目标或压测机让它延迟了，这段时间会计入延迟。
- `sent/s` 是已发送调用数除以发送时间：运行末尾挂起的目标不会拉低这个数字。
- 如果被超时截断的调用可能占据某个百分位的位置，该百分位会以**下界**形式输出：`>2.0s`。这不是
  数值，而是“至少”。
- 表格下方，每个方法有一个 `over time` 块：运行的测量部分，从预热结束算起，最多 12
  行；JSON 中最多 60 个窗口。调用归入其计划时刻所在的行，无论它何时结束。每行显示 `sent/s`、失败
  比例，以及两位有效数字的 p50 和 p99，因此某行的 p50 可能与表格的 p50 在最后一位上不同。`no calls`
  行表示该窗口内没有发出任何调用。如果速率低于每个窗口一次调用，这是计划本身，不是故障：请对照该
  方法的 `plan:`。使用 `ramp` 时，前几行就是爬坡阶段：那里 `sent/s` 低是计划，不是故障；请寻找在
  速率达到平台之前 p99 或失败比例就开始上升的那一行。
- 带 `dataset` 的方法下面有一行 `data:`：文件中的请求用了多少个，最常用的那个发出了
  多少次。这并不表示目标看到了这么多不同的请求。
- 默认情况下负载通过**一条连接**发出，并落在**一个后端**上：在 L4 负载均衡器（Kubernetes
  ClusterIP、NLB）后面，以及 DNS 返回多个地址时都是如此。“扛不住 X”说的是那个后端而不是整个
  服务，报告不会显示这一点。要给多个后端施压，请设置 `app.connections`：报告中的
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
- `bad response`：收到了应答，但客户端不接受：超过 `app.max_response_size`，使用了客户端
  未声明的编码压缩，或违反单次应答的形式：没有消息的 OK，或多余的消息。
- `client error`：客户端自己拒绝发送：请求无法编码，或编解码器、拦截器出错。

从未到达目标的调用计为失败，但不进入百分位。已经发出却没有收到状态的请求——对端重置了流或断开了
连接——单独计为 `cut off`：目标或代理可能已经处理了它，对写操作来说这是检查重复的理由。被我们
自己停止（Ctrl+C、SIGTERM）截断的调用记为 `aborted`，与代码无关：这不是目标的拒绝。

| 代码 | 状态经网络传回 | 代码由客户端设置，请求已发出 | 代码由客户端设置，请求未发出 |
|---|---|---|---|
| `INVALID_ARGUMENT`、`NOT_FOUND`、`ALREADY_EXISTS`、`PERMISSION_DENIED`、`UNAUTHENTICATED`、`FAILED_PRECONDITION`、`OUT_OF_RANGE`、`UNIMPLEMENTED` | `request error` | `cut off` | `client error` |
| `RESOURCE_EXHAUSTED` | `overload`；grpc-go 的 “larger than max” 是 `request error` | `cut off`；超过我们上限的应答是 `bad response` | `client error` |
| `UNAVAILABLE` | `overload` | `cut off`；目标在处理前拒绝的流是 `overload` | `unreachable`；同样 |
| `CANCELLED`、`UNKNOWN`、`INTERNAL`、`DATA_LOSS`、`ABORTED` | `failure`；grpc-go 看到的没有应答的 OK 是 `bad response` | `cut off`；无法解压的应答是 `bad response` | `client error` |
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

### 运行护照

报告上方有一份护照：这次运行是在哪台机器上、用什么配置做的，这样两份报告可以直接比较，不必猜测
它们差在哪里。运行和崩溃点搜索都有护照。

```text
machine: linux/amd64, 8 CPU, GOMAXPROCS 8, quota 4, go1.27.1; config sha256:3f9a1c0d2b7e
labels: commit=abc123, env=staging
```

- `quota` 是容器的 CPU 上限（cgroup），按设置值显示；没有上限时没有这一部分。
- 哈希是配置文件在磁盘上原样的 SHA-256，`sha256sum load.yaml` 也能得到同样的值；文本中显示前 12 位，
  JSON 中是全部。文件的任何改动，哪怕只是注释，都会改变哈希。哈希在替换 `${ENV}` 之前计算：
  `metadata` 中的密钥请通过 `${ENV}` 写，不要把值写在文件里。
- 标签是配置中的 `labels:` 和命令行的 `-label key=value`（可重复；命令行参数覆盖配置中的
  同名键）：

```sh
leettest -c load.yaml -label env=staging -label commit=abc123
```

键由拉丁字母、数字和 `_.-` 组成，最多 64 个字符；值是不超过 256 字节的字符串，YAML 中的数字请加引号
（`build: "1.10"`）。违反规则时在开始前退出码为 `1`。在 JSON 中护照是字段 `passport`，
`dataset` 的 `file` 旁边有 `sha256`；所有字段见[参考手册](reference.md#json-字段)。

## 运行何时无效

有报告，但其中的数字与目标所受的负载无关（退出码 2），如果：

- **触及了 `-max-in-flight` 上限。** 启动前会检查超时和上限，确保挂起的目标不会触及它；触及即
  说明压测机 CPU 不足。
- **某个方法所有被测量的调用都是 `request error`、`client error` 或 `bad response`。** 提示会
  指出方法、原因和应对办法。
- **主机时钟比某个方法 p50 的四分之一还粗**，或运行期间步长增长到 1 µs 及以上（`clock_step`）。
  请从 Linux 上测量。
- **某个 `pass` 限值无法判定**（`thresholds_undecided`）：观测太少、压测机来晚了、调用在等空闲的
  stream、尾部是没有应答的调用。`Thresholds:` 块会指出限值和原因。

## 限值：把运行当作检查

没有 `pass` 时，运行到结束就返回 `0`，无论目标如何应答。要让运行自己说出“扛得住 / 扛不住”，在
调用下设置限值：

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
      pass:
        p99: 200ms
        errors: 1%
```

```sh
leettest -c pass.yaml
echo $?
```

报告末尾会出现一个块：最差的结论和每个限值一行。对于健康的服务：

```
Thresholds: passed
  wallet.v1.WalletService/GetBalance  p99     <= 200ms  pass  21.2ms
  wallet.v1.WalletService/GetBalance  errors  <= 1%     pass  0.0% (0 of 44000)
```

键有 `p50`、`p90`、`p95`、`p99`、`p999` 和 `errors`；延迟限值旁边必须有 `errors`，而
`errors: 100%` 会关掉失败检查——这时延迟限值也就毫无价值。崩溃点搜索的限值是
`breakpoint.pass.held_rps`。未达标的限值给出退出码 `4`，无法判定的限值（观测太少、压测机来晚了、
尾部是没有应答的调用）给出 `2`。所有键、结论和原因见[参考手册](reference.md#pass-限值)；包含全部键的示例是
[`examples/pass.yaml`](../../examples/pass.yaml)。

## 退出码

没有 `pass` 时，运行到结束就返回 `0`，无论目标如何应答：**`0` 并不表示“服务健康”**。有 `pass`
时，`0` 表示每个限值都已检查并且达标。退出码有优先级：无效运行（`2`）高于未完成运行（`3`）；输出了
报告的停止总是 `3`；已结束但未达标的运行是 `4`，限值不会把无效或被停止的运行变成 `4`。

| 代码 | 发生了什么 |
|---|---|
| `0` | 计划执行完毕，报告完整；有 `pass` 时所有限值都达标 |
| `1` | 没有得到所要求的报告：运行没有发生（命令行参数、配置、连接），或报告无法写入 `-json-file`。原因见 stderr |
| `2` | 运行无效（见上文），或某个 `pass` 限值无法判定。有报告，但数字与负载无关，或回答不了限值提出的问题 |
| `3` | 运行在计划结束前停止。有报告，只涵盖已完成的部分 |
| `4` | 已结束的运行没达到某个 `pass` 限值。有报告，JSON 中的 `outcome` 是 `complete` |
| `130` | Ctrl+C 后中止，没有报告 |
| `143` | SIGTERM 后中止，没有报告 |

停止如何工作见[参考手册](reference.md#停止)。

## 供脚本和 CI 使用的 JSON

使用 `-output json` 时，stdout 只输出一个 JSON 对象和一个换行；其他内容都输出到 stderr，因此
stdout 可以直接交给 `jq`。只有运行确实发生时（代码 `0`、`2`、`3`、`4`）才输出该对象；代码 `1`、
`130` 和 `143` 时 stdout 为空。字段 `outcome`（`complete`、`invalid` 或 `incomplete`）与退出码
`0`、`2`、`3` 一致；代码 `4` 是 `complete`，哪个限值未达标由字段 `thresholds` 说明。唯一的
例外：报告无法写入文件时，退出码为 `1`，stdout 中仍是该次运行的对象。

要在日志中保留文本、同时为下一步保留 JSON，可用 `-json-file`：

```sh
leettest -c load.yaml -json-file report.json
```

stdout 按 `-output` 的设定输出（默认文本），`report.json` 中是 `-output json` 所输出的同一个对象。
文件在开始前创建，且绝不覆盖：文件已存在或目录不存在时，退出码为 `1`，目标不会收到任何调用。
没有报告的运行（例如连接阶段的 `1`，或 `130`）不留下文件。运行后写入失败时，stderr 给出路径和
错误，退出码为 `1`。

结构按 `schema_version` 版本化，目前为 `1`，它是一份契约；屏幕文本不是：请解析 JSON，而不是
屏幕。规则：

- 新增字段不改变版本；重命名、删除字段或改变其类型会提高 `schema_version`；
- 枚举的取值（`outcome`、`unchecked[].reason`、`failure_codes` 中的代码）可以增加而不改变版本；
- 使用方必须跳过未知字段，并在遇到未知枚举值时不出错。

单位写在字段名里：`_us` 是整数微秒，`_s` 是整数秒；速率 `rps` 是唯一的小数。运行未产生的值是
`null`，不是 `0`。百分位是一个对象 `{"us": 1234, "ns": 1234567, "lower_bound": false}`：`ns` 是直方图的数值，
整数纳秒，`us` 是同一数值截断到微秒；`pass:` 和 `leettest compare` 按 `ns` 判定，对于在这个字段出现
之前写下的报告则取 `us` × 1000。`lower_bound: true` 时它是下界，而不是数值。延迟是直方图在 3 位有效数字下的取值（误差不超过 0.1%），计数是精确的。
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

调用中用 `load.breakpoint` 段代替 `rps` 和 `duration`：LeetTest 分台阶提高负载，并指出
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
    errors_limit: 1%  # 可选；失败调用的占比超过它时该级崩溃
```

一级在失败调用多于 `errors_limit`（默认 1%；恰好这么多仍算撑住，`100%` 表示错误不会使一级崩溃）或
p99 越过界线时判为崩溃。因没有连接而未发出的调用，在任何 `errors_limit` 下都使该级崩溃。客户端一侧的
错误（`request_error`、`client_error`、`bad_response`）并不能证明目标崩了：如果只有算上它们才超过
上限，搜索会以 `run_limit` 停止，原因为 `client_errors`，而不是宣布一个崩溃点；`held_rps` 保留。崩溃的台阶会在停顿后重复，并且除第一级外，
还会先在第一级上做探测：目标必须回到原来的 p99。一级只有在压测机发出了计划的调用时才算撑住——
至少是测量窗口内调用数的 99.9%。否则这是本次运行的上限，而不是目标的上限。在第一级之前，stderr
会有一行 `breakpoint: up to N steps, at most T`：共有多少级，以及搜索最坏情况下要多久。整个搜索
只用一条连接；目标断开它时，该级判为崩溃，不会重连。

结果（JSON 中的 `outcome`）是封闭列表：`broke`（撑住 X，在 Y 崩溃）、`broke_at_first`（在第一级就
崩溃——请从更低处开始）、`held_all`（一直撑到 `to`）、`run_limit`（压测机、某条连接的上限或
`-max-in-flight` 用尽：更高负载下目标的情况未知）、`stopped`（Ctrl+C）、`invalid`（某一级是无效
运行）。原因（`why`）也是封闭列表：`errors`、`p99_limit`、`p99_vs_base`、`connection`、
`no_recovery`、`generator`、`in_flight_cap`、`stream_limit`、`stream_wait`、`client_errors`、
`clock_step`、`request_errors` 或 `null`。错误上限是字符串 `breakpoint.errors_limit`。
没有对应值时，`held_rps` 和 `broke_rps` 为 `null`。在搜索模式下 JSON
是另一种对象：`mode: "breakpoint"`，普通运行的字段都不在顶层，每次运行连同其完整报告放在
`breakpoint.runs` 中（`kind`：`step`、`repeat`、`probe`；`planned_rps` 和 `sent_rps`）。
普通运行写 `mode: "run"`。

退出码：`broke`、`broke_at_first`、`held_all`、`run_limit` 为 `0`——它们是发现；`invalid` 为 `2`；
单次 Ctrl+C 停止为 `3`；搜索守住的低于 `breakpoint.pass.held_rps` 为 `4`。

### 比较两次搜索

用 `-json-file` 写下的两份搜索报告——改动之前和之后。`leettest compare` 会说出
容量（`held_rps`）下降了多少，并在给出 `-max-capacity-drop` 时据此判定。在参考测试台上，从仓库根目录
运行：先对容量 270 rps 的测试台做一次搜索（在测试台的终端里），然后在另一个终端里搜索并保存“之前”的
报告：

```sh
go run ./test/stand/cmd/stand -capacity 270 -delay 20ms
```

```sh
leettest -c examples/breakpoint-step.yaml -json-file before.json
```

停掉测试台（Ctrl+C），以容量 200 rps 重新启动，并保存“之后”的报告：

```sh
go run ./test/stand/cmd/stand -capacity 200 -delay 20ms
```

```sh
leettest -c examples/breakpoint-step.yaml -json-file after.json
```

比较（测试台可以停掉）：

```sh
leettest compare -max-capacity-drop 10% before.json after.json
```

```
base: before.json  Health  2026-10-19T19:25:28.941479Z
new:  after.json  Health  2026-10-19T19:26:46.1806268Z
capacity: 250 rps -> 200 rps, drop 20.0%, limit 10%: FAIL
```

容量从 250 降到 200 rps，即 20%，限值为 10%，退出码 `4`。你的时间和测试台会不同，数字取决于测试台的
容量。

两份报告只有在目标、方法、LeetTest 版本、运行护照（标签除外）和数据集都一致时才可比较；否则
`differs:` 行会指出字段，结论是 `unknown`。如果搜索在崩溃之前就结束了，容量是下界（`>= N rps`）；
如果目标在第一级就崩溃了，则是上界（`below N rps`）。这时下降幅度以界限形式输出
（`drop <= 33.4%`），只有当这个界限能够决定时，限值才会被判定。没有 `-max-capacity-drop` 时
什么都不判定。退出码：`0`——在限值内或没有限值，`1`——报告无法读取，`2`——报告不可比较或限值无法
判定，`4`——容量下降超过限值。

### 比较两次运行

同样的命令也能读取普通运行的报告（`-json-file`）：`-max-p99-rise` 判定每个方法 p99 相对于
“之前”p99 的增长百分比，`-max-errors-rise` 判定失败调用占比的增长（以百分点计）。没有限值时该行
会输出，但不判定。一次命令里同时给搜索报告和运行报告——退出码 `1`。在参考测试台上，从仓库根目录运行：
先在 20 ms 延迟下运行，再在 30 ms 下运行：

```sh
go run ./test/stand/cmd/stand -delay 20ms -life 60s
```

```sh
leettest -c examples/stand.yaml -json-file before.json
```

测试台一分钟后会自行退出；以 30 ms 延迟重新启动它，并保存“之后”的报告：

```sh
go run ./test/stand/cmd/stand -delay 30ms -life 60s
```

```sh
leettest -c examples/stand.yaml -json-file after.json
```

比较（测试台可以停掉）：

```sh
leettest compare -max-p99-rise 20% before.json after.json
```

```
base: before.json  Health  2026-10-20T12:40:09.5222467Z
new:  after.json  Health  2026-10-20T12:40:42.469917Z
grpc.health.v1.Health/Check: p99 21.2ms -> 31.3ms, rise 47.3%, limit 20%: FAIL
grpc.health.v1.Health/Check: errors 0.0% -> 0.0%, no change
```

p99 从 21.2 升到 31.3 ms，即 47.3%，限值为 20%，退出码 `4`。你的时间和延迟会不同。

检查结果可能是 `unknown`（退出码 `2`），当报告不可比较（如上）、运行无效或未完成、测量太少、p99
撞上了超时（因此它比输出的更大，`>=`），或客户端自身的错误使人无法判断服务是否有责任时。不在两份
报告中同时出现的方法不参与比较。退出码与搜索相同。

供脚本和 CI 使用时，同样的结果也可以作为一个 JSON 对象输出：`-output json` 只把它输出到
stdout，退出码和 stderr 与文本相同；退出码为 `1` 时 stdout 为空。

```sh
leettest compare -max-p99-rise 20% -output json before.json after.json
```

对象中的 `verdict` 是有限值的检查中最差的结论（没有限值时为 `null`），`checks` 则包含每项检查及两份
报告的数字；`unknown` 的原因在 `reason` 中，取自封闭列表：

```json
{"schema_version":1,"mode":"compare","compared_mode":"run","comparable":true,"differences":[],"verdict":"fail","checks":[{"method":"/grpc.health.v1.Health/Check","key":"p99","base_p99":{"us":21381,"ns":21381472,"lower_bound":false},"new_p99":{"us":31358,"ns":31358109,"lower_bound":false},"limit_percent":"20%","verdict":"fail","reason":null,"why":null}]}
```

没有显示全部字段：这项检查为空的字段（`base_capacity` 和其他 `null`）以及报告的描述（`base`、
`new`）被省略了。对象中没有增长百分比——脚本自己根据 `us` 计算。每个字段都在
[参考手册，“对比 JSON”](reference.md#对比-json)中说明。

## 深入了解

我们有产品的发展计划——**[roadmap](roadmap.md)**。缺少某项功能——
[提交 issue](https://github.com/yhgrwav/leettest/issues/new/choose)。

**[参考手册 →](reference.md)**——每个配置字段和命令行参数。

**[Prometheus 指标 →](reference.md#prometheus-指标)**——运行在加压期间提供 `/metrics`，
Prometheus 把它和服务自身的指标放在一起抓取。没有默认地址，`15s` 的 `linger` 会拖延退出：请阅读该
节中的警告。

| 问题 | |
|---|---|
| LeetTest 回答哪些问题？ | [阅读](questions.md) |
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
