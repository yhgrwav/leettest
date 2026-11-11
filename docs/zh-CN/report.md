# 报告、退出码、JSON、对比

[← 返回文档](README.md)

> 译自 [docs/ru/report.md](../ru/report.md)，对应 4cba437，2026-11-10。如有出入，以俄文版为准。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../en/report.md)。

这里汇集了原先在 README 中的章节：如何阅读报告、限值、退出码、JSON、断点搜索、运行对比。每个字段和标志见
[参考](../en/reference.md)（英文）。

## 如何阅读报告

末尾是每个方法的报告：已发送、失败、`sent/s`、p50/p90/p95/p99。

- 请求从**计划**时刻开始计算：如果目标或生成器延误了它，这会计入延迟。
- `sent/s`——已发送的请求数除以发送时间：运行结尾目标卡住不会拉低这个数字。
- 可能落在被超时中断的请求位置上的百分位数，会以**下界**打印：`>2.0s`。这不是一个值，而是“不小于”。
- 方法表格下是 `over time` 块：运行中被测量的部分，从预热结束起，最多 12 行；JSON 中最多 60 个窗口。
  请求归入其计划时刻所在的行，无论它何时结束。每行包含：`sent/s`、失败占比、p50 和 p99（两位有效数字），
  因此该行的 p50 可能与表格的 p50 在最后一位上不同。`no calls` 一行表示该窗口内没有发出任何请求。
  如果速率低于每窗口一个请求，这是计划所致，并非故障：请与该方法的 `plan:` 核对。使用 `ramp` 时，最初
  几行就是爬升：那里 `sent/s` 低是计划内的，不是故障；请找出速率还没到达平台期，p99 或失败占比就先上升
  的那一行。
- 使用 `ramp` 时，报告里每个方法还有 `ramp by second` 块（尚未发布）：被排除窗口的各个秒，每一秒给出
  计划速率、已发出的、计划在这一秒的请求，以及其中失败的占比。只有整秒，最多 20 行。失败占比是下界：
  运行结束时仍在途中的请求不算失败。断点搜索会为它的第一级显示这个块。
- 运行头部的 `traffic:` 一行——消息体每秒通过了多少比特：发出和收到。这是下界：不含 HTTP/2 帧、请求头和
  TLS；接收按解压前的大小计算。方法下的 `response: N B per call`——成功请求响应的平均大小；有预热时的
  `warm-up: N calls, p50, p99`——预热自己的数字，与运行分开。
- 带 `dataset` 的方法下有一行 `data:`：文件中用了多少个请求，以及最常见的那个发出了多少次。这并不意味着
  目标看到了这么多不同的请求。
- 默认负载走**一条连接**，而它落在**一个后端**：在 L4 负载均衡器（Kubernetes ClusterIP、NLB）之后，
  以及 DNS 返回多个地址时。“撑不住 X”的结论针对的是这个后端，而不是服务，报告不会显示这一点。要给多个
  后端施压，设置 `app.connections`：报告中的 `Connections:` 块每条连接一行——地址、请求数、失败占比和
  p99，因此落后的后端一目了然。按单个请求分发的 L7 负载均衡器（Envoy、gRPC ingress）也会分发单条连接。
  报告会打印连接重连了多少次，以及目标声明的并发流上限。

**类别。** 成功以外的响应按行分开，每行有各自的百分位数。状态可能不是目标发来的，而是它前面的代理：
没有存活后端的 nginx 会回复 `UNAVAILABLE`，客户端无法区分两者。

- `request error`——无论什么速率该请求都会失败：请求有误，或请求被目标或代理认为太大。
- `overload`——状态表示“过载或不可用”：`RESOURCE_EXHAUSTED`、`UNAVAILABLE`。这是状态的字面含义，不是
  对目标的诊断。
- `failure`——状态表示请求出了故障：`INTERNAL`、`UNKNOWN`、`DATA_LOSS`、来自对端的 `CANCELLED`、
  `ABORTED`（并发修改冲突，而不是容量不足）。
- `bad response`——响应到了，但客户端没有接受：大于 `app.max_response_size`，或用客户端没有声明的编码
  压缩，或违反一元性：OK 却没有消息，或多出一条消息。
- `client error`——客户端自己拒绝发送：请求无法编码，编解码器或拦截器出错。

没有到达目标的请求计为失败，但不进入百分位数。已发出但没有收到状态的请求——对端重置了流或断开了
连接——单独计为 `cut off`：目标或代理可能已经处理了它，对写操作来说这是检查重复的理由。被我们自己的
停止（Ctrl+C、SIGTERM）中断的请求，无论什么代码都是 `aborted`：这不是目标的失败。

| 代码 | 状态经网络到达 | 代码由客户端设置，请求已发出 | 代码由客户端设置，请求未发出 |
|---|---|---|---|
| `INVALID_ARGUMENT`、`NOT_FOUND`、`ALREADY_EXISTS`、`PERMISSION_DENIED`、`UNAUTHENTICATED`、`FAILED_PRECONDITION`、`OUT_OF_RANGE`、`UNIMPLEMENTED` | `request error` | `cut off` | `client error` |
| `RESOURCE_EXHAUSTED` | `overload`；grpc-go 的 “larger than max”——`request error` | `cut off`；响应超过我们的上限——`bad response` | `client error` |
| `UNAVAILABLE` | `overload` | `cut off`；被目标在处理前拒绝的流——`overload` | `unreachable`；同左 |
| `CANCELLED`、`UNKNOWN`、`INTERNAL`、`DATA_LOSS`、`ABORTED` | `failure`；grpc-go 的无响应 OK——`bad response` | `cut off`；响应无法解压——`bad response` | `client error` |
| `DEADLINE_EXCEEDED` | 超时 | 超时 | 超时，未发送 |

过大的请求是通过 grpc-go 的错误文本识别的。运行在其他实现上的目标（Envoy、Java）写法不同，这样的拒绝会
落入 `overload`，而不是 `request error`。

被拒绝的流的 `UNAVAILABLE` 是 grpc-go 对目标发来的 RST_STREAM REFUSED_STREAM 的转译：代码由客户端设置，
但拒绝是目标发来的，所以它归在 “sent by the target” 一行。目标没有处理过这样的流（RFC 9113 §8.7）。

报告下方，失败的请求按 gRPC 代码分为两行。“sent by the target”——状态经网络到达，来自目标或代理。
“set by the client”——代码由客户端自己设置：没有人响应、流被重置、我们的 deadline 到期，或客户端没有接受
响应。目标返回的 `DEADLINE_EXCEEDED` 通常就是我们自己的 deadline：它通过 `grpc-timeout` 头发给目标。
类别说明是谁的责任，代码说明该到目标日志里找什么。

**客户端一侧的等待。** 延迟里包含请求在我们这里等待过的一切：生成器的延误、等待连接、等待空闲的流。
如果去掉它们后任一方法的 p99 下降 10% 及以上，或有一部分请求始终没有发出，报告会给出结论，指出在 p99
尾部中最常见的原因，并打印去掉等待后的 p99——请求在目标那里度过的时间。如果原因是流用完了，高于“上限 ×
连接数”的目标容量没有被测量。如果请求前在等待解析器，调用方的拦截器也会计入连接等待，因此去掉等待后的
p99 可能略微偏低。未发送的请求按原因细分。偏移小于 10% 不是结论，而是带数字的备注；如果 p99 是下界，
偏移未知，也没有备注。

**时钟。** 工具在运行前后测量主机时钟的步长。步长明显时，会打印一行
`clock step 502us on this host: every latency and wait is +/- 502us`。在 Linux 上步长是几十纳秒，没有这一行。

### 运行护照

报告上方是护照：运行是在哪台机器上、用什么配置做的，这样两份报告可以对比，而不必猜它们有什么不同。
运行和断点搜索都有它。

```text
machine: linux/amd64, 8 CPU, GOMAXPROCS 8, quota 4, go1.27.1; config sha256:3f9a1c0d2b7e
labels: commit=abc123, env=staging
```

- `quota`——容器（cgroup）的 CPU 限额，按设定值；没有限额就没有这一行。
- 哈希——配置文件按磁盘上的原样计算的 SHA-256，`sha256sum load.yaml` 也会显示它；文本中是前 12 位，
  JSON 中是全部。对文件的任何修改，哪怕是注释，都会改变哈希。哈希在代入 `${ENV}` 之前计算：
  `metadata` 中的密钥请通过 `${ENV}` 写，不要把值写在文件里。
- 标签——配置中的 `labels:` 和命令行中的 `-label 键=值`（可重复；该标志会覆盖配置中的同名键）：

```sh
leettest -c load.yaml -label env=staging -label commit=abc123
```

键——拉丁字母、数字和 `_.-`，最多 64 个字符；值——最长 256 字节的字符串，在 YAML 中数字请加引号
（`build: "1.10"`）。违反——启动前退出码 `1`。在 JSON 中护照是 `passport` 字段，数据集的 `file` 旁有
`sha256`；所有字段见[参考](../en/reference.md#json-fields)（英文）。

## 运行何时无效

报告存在，但数字不是关于目标上的负载（退出码 2），当：

- **撞到了 `-max-in-flight` 上限。** 超时和上限在启动前互相核对，使卡住的目标不会撞到它；撞到说明
  生成器的处理器不够用。
- **一个方法所有被测量的请求都是 `request error`、`client error` 或 `bad response`。** 备注会指出方法、
  原因以及该怎么办。
- **主机时钟比任一方法的 p50 的四分之一更粗**，或步长在运行中增长到 1 微秒及以上（`clock_step`）。请从
  Linux 测量。
- **生成器的延误决定了 p99 尾部**（`generator_late`）：延迟包含了这台机器的延误，并没有测量目标。给生成器
  更多 CPU，降低频率，或把它分摊到多台机器上。方法从 100 个观测起才参与判定。报告会指出这些方法。
- **`pass` 限值无法判定**（`thresholds_undecided`）：观测太少、生成器延误、请求在等待空闲的流、
  由无响应请求构成的尾部。`Thresholds:` 块会指出限值和原因。

## 限值：把运行变成检查

没有 `pass` 时，跑到结尾的运行会返回 `0`，无论目标怎样响应。要让运行自己说出“撑得住 / 撑不住”，请在调用
下设置限值：

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

报告末尾会出现一个块：各结论中最差的一个，以及每个限值一行。对于健康的服务：

```
Thresholds: passed
  wallet.v1.WalletService/GetBalance  p99     <= 200ms  pass  21.2ms
  wallet.v1.WalletService/GetBalance  errors  <= 1%     pass  0.0% (0 of 44000)
```

键：`p50`、`p90`、`p95`、`p99`、`p999` 和 `errors`；延迟限值要求旁边有 `errors`，而 `errors: 100%` 会取消
对失败的检查——那时延迟也就没有意义了。断点搜索的限值是 `breakpoint.pass.held_rps`。被突破的限值给出
退出码 `4`，无法判定的限值（观测太少、生成器延误、由无响应请求构成的尾部）给出退出码 `2`。所有键、结论和
原因见[参考](../en/reference.md#pass-limits)（英文）；包含全部键的示例——
[`examples/pass.yaml`](../../examples/pass.yaml)。

## 退出码

没有 `pass` 时，跑到结尾的运行会返回 `0`，无论目标怎样响应：**`0` 不代表“服务健康”**。有 `pass` 时，`0`
表示每个限值都已检查并满足。退出码有等级：无效运行（`2`）比不完整运行（`3`）更重要；打印了报告的停止
总是 `3`；完成的运行突破了限值是 `4`，限值不会把无效或被停止的运行变成 `4`。

| 代码 | 发生了什么 |
|---|---|
| `0` | 计划已执行，报告完整；有 `pass` 时——所有限值均满足 |
| `1` | 没有得到所要求的报告：运行没有进行（标志、配置、连接），或报告没有写入 `-json-file`。原因在 stderr 中 |
| `2` | 运行无效（见上文），或 `pass` 限值无法判定。报告存在，但数字不是关于负载，或不回答限值的问题 |
| `3` | 运行在计划结束前被停止。报告存在，只覆盖已经运行的部分 |
| `4` | 完成的运行突破了 `pass` 限值。报告存在，JSON 中的 `outcome` 为 `complete` |
| `130` | Ctrl+C 之后不带报告的紧急退出 |
| `143` | SIGTERM 之后不带报告的紧急退出 |

停止如何工作——见[参考](../en/reference.md#stopping)（英文）。

## 供脚本和 CI 使用的 JSON

使用 `-output json` 时，stdout 中恰好输出一个 JSON 对象和一个换行；其余一切都进 stderr，因此可以直接把
stdout 交给 `jq`。只有已经进行的运行才会打印对象（退出码 `0`、`2`、`3`、`4`）；`1`、`130` 和 `143` 时
stdout 为空。`outcome` 字段——`complete`、`invalid` 或 `incomplete`——对应退出码 `0`、`2`、`3`；退出码
`4` 是 `complete`，突破了哪个限值由 `thresholds` 字段说明。唯一的出入：如果报告没能写入文件，退出码是
`1`，而 stdout 中仍留有运行的对象。

要从一次运行得到日志里的文本和供下一步使用的 JSON——用 `-json-file`：

```sh
leettest -c load.yaml -json-file report.json
```

stdout 中一切按 `-output` 的设置（默认文本），`report.json` 中是 `-output json` 打印的同一个对象。文件在
启动前创建，不会被覆盖：如果它已存在或目录不存在，退出码是 `1`，并且不会向目标发出任何请求。没有报告的
运行（例如连接时的 `1` 或 `130`）不会留下文件。如果运行后写入失败，stderr 会指出路径和错误，退出码为 `1`。

模式用 `schema_version` 字段做版本管理，目前是 `1`，这是一份契约；屏幕上的文本不是——请解析 JSON，不要
解析屏幕。规则：

- 新字段的添加不更改版本；重命名、删除或更改字段类型会提高 `schema_version`；
- 枚举的取值集合（`outcome`、`unchecked[].reason`、`failure_codes` 中的代码）可以在不更改版本的情况下
  扩充；
- 使用方必须跳过未知字段，并在遇到未知的枚举值时不崩溃地处理它。

单位在字段名中：`_us`——整数微秒，`_s`——整数秒；频率 `rps` 是唯一的小数。运行没有给出的值是 `null`，
而不是 `0`。百分位数是一个对象 `{"us": 1234, "ns": 1234567, "lower_bound": false}`：`ns`——直方图的值，
单位为整数纳秒，`us`——同一个值，截断到微秒；`pass:` 按同一直方图桶的下限值判定（比 `ns` 低不超过
0.1%），`leettest compare` 按 `ns` 判定；在写入该字段之前生成的报告中，取 `us` × 1000。当
`lower_bound: true` 时，它是下界，而不是值。延迟是精度为 3 位有效数字的直方图值（误差最多 0.1%），计数
是精确的。时间从 `started_at` 起算（RFC 3339，UTC）——计划的开始，包含预热；`duration_us` 同样包含预热。
某一秒中的 `in_flight`——该秒结束时有多少请求在途中。`failure_codes` 中的代码是规范名称
（`UNAVAILABLE`）。核对：运行的总计是各方法总计之和，而按方法的各秒，`Σ begun` 加 `outside_timeline`
就是它的所有请求，包括预热。

决定以字段给出，而不是文本：`invalid_reasons`（`in_flight_cap`、`nothing_measured`、`clock_step`）、
`methods[].invalid_reason`（`request_error`、`client_error`、`bad_response`、`mixed` 或 `null`）、
`tail_wait_cause`（`generator`、`stream`、`connection` 或 `null`），以及 `client_waits` 中按原因的数字。
`notes` 和 `unchecked[].error` 是给人看的文本，它可以随意变化，请不要解析它。

目标的沉默：`methods[].silent_from_s`——从哪一秒起目标对任何已发出的请求都没有响应，
`methods[].silent_sent_rps`——在它之前的一秒发出了多少请求，`methods[].silent_planned_rps_low` 和 `_high`
——同一秒各阶段的计划速率。没有沉默时全部为 `null`；如果那一秒已经没有阶段，计划速率也是 `null`。
`planned_rps_low` 和 `_high`——整个计划的速率。

文本报告只以 ASCII 打印。ASCII 以外的字符——在方法名中、在目标的错误文本中——以 `\uXXXX` 打印（超出
U+FFFF 的用代理对，与 JSON 相同）。JSON 中的 `notes` 带有同样转义后的文本。

### 断点搜索

调用中用 `load.breakpoint` 部分代替 `rps` 和 `duration`：LeetTest 分级提高负载，并指出目标在哪一级撑住了、
在哪一级崩溃。调用恰好一个，没有 `rps`、`duration` 和 `load.warmup`。

```yaml
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      timeout: 500ms
  breakpoint:
    from: 100         # 第一级，rps
    to: 2000          # 不再往上
    factor: 1.25      # 下一级 = 上一级 × factor；或 step: 100——多这么多 rps
    settle: 5s        # 一级的开头，不计入结论；小于 hold 的一半
    hold: 30s         # 一级的时长
    p99_limit: 200ms  # 可选；没有它时，一级在 p99 超过最佳通过级的 3 倍时崩溃
    errors_limit: 1%  # 可选；如果失败的请求超过这个占比，该级崩溃
```

如果失败的请求超过 `errors_limit`（默认 1%；恰好这么多仍算撑住，`100%`——错误不会使其崩溃），或 p99
超过阈值，该级就崩溃。因连接而未发送的请求，在任何 `errors_limit` 下都会使该级崩溃。客户端一侧的错误
（`request_error`、`client_error`、`bad_response`）并不证明是目标崩溃了：如果仅由它们造成限值被超过，
搜索会以 `run_limit` 停止，原因为 `client_errors`，而不会宣布断点；`held_rps` 保留。崩溃的一级会在暂停
后重复，并且除第一级外，还要在第一级上做探测之后：目标必须回到先前的 p99。只有当生成器发出了计划内的
请求——不少于测量窗口内请求的 99.9%——一级才被算作撑住。否则这是运行的上限，而不是目标的上限。在第一级
之前，stderr 中有一行 `breakpoint: up to N steps, at most T`：有多少级，以及搜索在最坏情况下要花多久。
整个搜索只有一条连接；如果目标断开它，这一级就崩溃，不会重连。

第一级必须测量足够多的请求来计算 p99：`from × (hold − settle)`，小数部分舍去，不少于 100。第一级测量更少
的搜索会在施加任何负载之前被拒绝：`invalid breakpoint: the first step measures 40 calls, a p99 needs 100:
raise from, hold or lower settle`——后续各级和探测按最佳通过级的 p99 来判断。p99 仍然没有得出的一级
（带有已测量延迟的请求太少）不会设定这个基准；没有基准的探测视为已恢复，报告会在备注中说明
（`no baseline: the first step measured too few calls for a p99, the knee was not checked`）。

结果（JSON 中的 `outcome`）是封闭列表：`broke`（撑住 X，在 Y 崩溃）、`broke_at_first`（在第一级就崩溃——
请从更低的开始）、`held_all`（撑住了一直到 `to` 的所有级）、`run_limit`（生成器、连接上限或
`-max-in-flight` 用尽：关于更高处的目标一无所知）、`stopped`（Ctrl+C）、`invalid`（这一级被证明是无效
运行）。原因（`why`）同样是封闭列表：`errors`、`p99_limit`、`p99_vs_base`、`connection`、`no_recovery`、
`generator`、`in_flight_cap`、`stream_limit`、`stream_wait`、`client_errors`、`clock_step`、
`request_errors` 或 `null`。错误上限是 `breakpoint.errors_limit` 这一行。`held_rps` 和 `broke_rps` 在没有
时为 `null`。在搜索模式下，JSON 是另一个对象：`mode: "breakpoint"`，顶层没有普通运行的字段，每次运行及其
完整报告都在 `breakpoint.runs` 中（`kind`：`step`、`repeat`、`probe`；`planned_rps` 和 `sent_rps`）。
普通运行写 `mode: "run"`。

退出码：`broke`、`broke_at_first`、`held_all`、`run_limit` 为 `0`——这些都是发现；`invalid` 为 `2`；
一次 Ctrl+C 的停止为 `3`；`4`——搜索撑住的低于 `breakpoint.pass.held_rps`。

### 对比两次搜索

两份搜索报告，由 `-json-file` 写出——更改前和更改后。`leettest compare` 会说明容量（`held_rps`）下降了多少，
并用 `-max-capacity-drop` 对其做判定。在测试台上，从仓库根目录：先在容量为 270 rps 时搜索（在测试台的
终端中），然后在另一个终端中——搜索并得到“之前”的报告：

```sh
go run ./test/stand/cmd/stand -capacity 270 -delay 20ms
```

```sh
leettest -c examples/breakpoint-step.yaml -json-file before.json
```

停止测试台（Ctrl+C），以 200 rps 的容量启动它，并得到“之后”的报告：

```sh
go run ./test/stand/cmd/stand -capacity 200 -delay 20ms
```

```sh
leettest -c examples/breakpoint-step.yaml -json-file after.json
```

对比（测试台可以停止）：

```sh
leettest compare -max-capacity-drop 10% before.json after.json
```

```
base: before.json  Health  2026-10-19T19:25:28.941479Z
new:  after.json  Health  2026-10-19T19:26:46.1806268Z
capacity: 250 rps -> 200 rps, drop 20.0%, limit 10%: FAIL
```

容量从 250 降到 200 rps，即下降 20%，而限值是 10%，退出码 `4`。时间和测试台会是你自己的，数字取决于测试台
的容量。

只有当目标、方法、LeetTest 版本、运行护照（标签除外）和数据集都一致时，报告才可比；否则 `differs:` 行会
指出字段，结论为 `unknown`。如果搜索在崩溃之前就结束了，容量是下界（`>= N rps`）；如果目标在第一级就
崩溃了——是上界（`below N rps`）。此时下降以边界形式打印（`drop <= 33.4%`），只有当这个边界能决定限值时，
限值才会被判定。没有 `-max-capacity-drop` 时什么都不判定。退出码：`0`——在限值内或没有限值，`1`——报告
没有读取，`2`——报告不可比或限值无法判定，`4`——容量下降超过限值。

### 对比两次运行

同样的命令也能读取普通运行的报告（`-json-file`）：`-max-p99-rise` 判定每个方法 p99 的上升（占“之前”
p99 的百分比），`-max-errors-rise` 判定失败请求占比的上升（以百分点计）。没有限值时，这一行会打印但
不判定。搜索报告和运行报告出现在同一条命令中——退出码 `1`。在测试台上，从仓库根目录：先在延迟 20 ms
时运行，然后在 30 ms 时：

```sh
go run ./test/stand/cmd/stand -delay 20ms -life 60s
```

```sh
leettest -c examples/stand.yaml -json-file before.json
```

测试台会在一分钟后自行结束；以 30 ms 的延迟启动它，并得到“之后”的报告：

```sh
go run ./test/stand/cmd/stand -delay 30ms -life 60s
```

```sh
leettest -c examples/stand.yaml -json-file after.json
```

对比（测试台可以停止）：

```sh
leettest compare -max-p99-rise 20% before.json after.json
```

```
base: before.json  Health  2026-10-20T12:40:09.5222467Z
new:  after.json  Health  2026-10-20T12:40:42.469917Z
grpc.health.v1.Health/Check: p99 21.2ms -> 31.3ms, rise 47.3%, limit 20%: FAIL
grpc.health.v1.Health/Check: errors 0.0% -> 0.0%, no change
```

p99 从 21.2 升到 31.3 ms，即上升 47.3%，而限值是 20%，退出码 `4`。时间和延迟会是你自己的。

当报告不可比（如上）、运行无效或不完整、测量太少、p99 顶到了超时（即它比打印的更大，`>=`），或客户端自己
的错误使得无法判定是否是服务的问题时，检查结果可能是 `unknown`（退出码 `2`）。两份报告中并非都有的方法
不参与对比。退出码与搜索相同。

对于脚本和 CI，同样的结果可以用一个 JSON 对象给出：`-output json` 只把它打印到 stdout，退出码和 stderr
与文本相同；退出码为 `1` 时 stdout 为空。

```sh
leettest compare -max-p99-rise 20% -output json before.json after.json
```

对象中的 `verdict` 是带限值的各项检查中最差的结论（没有限值时为 `null`），`checks` 中是每项检查，带有两份
报告的数字；对于 `unknown`，原因在 `reason` 中——取自封闭列表的值：

```json
{"schema_version":1,"mode":"compare","compared_mode":"run","comparable":true,"differences":[],"verdict":"fail","checks":[{"method":"/grpc.health.v1.Health/Check","key":"p99","base_p99":{"us":21381,"ns":21381472,"lower_bound":false},"new_p99":{"us":31358,"ns":31358109,"lower_bound":false},"limit_percent":"20%","verdict":"fail","reason":null,"why":null}]}
```

没有显示所有字段：这项检查中为空的字段（`base_capacity` 和其他 `null`）以及对报告的描述（`base`、`new`）
都被省略了。对象中没有上升的百分比——脚本从 `us` 计算它。每个字段在
[参考中的 “Compare JSON”](../en/reference.md#compare-json)（英文）中有说明。

## 接下来

[参考](../en/reference.md)（英文）——配置的每个字段和标志。[Prometheus 指标](../en/reference.md#prometheus-metrics)
（英文）——运行在负载进行期间提供 `/metrics`；默认不设置地址，而 15 秒的 `linger` 会推迟退出：请阅读该节的
警告。[路线图](../en/roadmap.md)（英文）。缺少功能——[提一个 issue](https://github.com/yhgrwav/leettest/issues/new/choose)。
