> 译自 [docs/ru/quickstart.md](../ru/quickstart.md)，对应 67f8732，2026-10-08。如有出入，以俄文版为准。

# 快速上手

用半小时完整走一遍 LeetTest：从安装到一份可以交给服务开发者的报告，再到在 CI 中运行。每个字段和
命令行参数见[参考手册](reference.md)，如何阅读报告见 [README](README.md)；这里展示它们如何配合。

通过 AI 智能体工作？把 [AGENTS.md](../../AGENTS.md) 交给它：内容相同，换成智能体需要的形式——
无终端运行、JSON、退出码。

## 在哪里运行

LeetTest 为 **Linux 和 macOS** 而设计：压测机放在目标旁边——同一网络中的服务器、容器、CI runner。
离得越近，数字里混入的无关网络就越少。

Windows 也受支持，但测量效果更差：它的时钟步长约 0.5 ms，针对快速服务的运行会被如实判为无效
（`clock_step`，退出码 2）——时钟步长与延迟本身一样大。请在 Linux 上测量；Windows 适合试用。

## 1. 安装

```console
$ go install github.com/yhgrwav/leettest/cmd/leettest@latest
$ leettest -version
```

需要 Go 1.26 或更高版本。Linux、macOS 和 Windows（amd64 与 arm64）的预编译二进制文件在 Releases
页面。

## 2. 目标：参考测试台

体验时不需要自己的服务：仓库里有一个参考测试台，一个行为由你设定的 gRPC 服务。它应答
`grpc.health.v1.Health/Check` 和 `wallet.v1.WalletService`，并像真实服务一样提供 reflection。

```console
$ git clone https://github.com/yhgrwav/leettest && cd leettest
$ go run ./test/stand/cmd/stand -delay 20ms -hang-from 25s -life 60s
```

测试台在 20 ms 内应答，在第一次调用 25 秒后完全停止应答——这就是一个在负载下倒下的服务的样子。
正确答案事先已知，因此可以看出报告是否说了实话。

## 3. 配置

[`examples/tour.yaml`](../../examples/tour.yaml) 用到了本地测试台能检验的所有字段：

```yaml
name: tour

app:
  target:
    ip: 127.0.0.1
    port: 50051
  tls: false
  metadata:
    authorization: Bearer ${LEETTEST_TOKEN}
    x-request-source: leettest-tour
  max_response_size: 1MiB
  connections: 1

load:
  warmup: 3s
  calls:
    - method: grpc.health.v1.Health/Check
      rps: 300
      duration: 40s
      timeout: 500ms
      data:
        service: wallet.v1.WalletService
    - method: wallet.v1.WalletService/GetBalance
      rps: 50
      duration: 40s
      timeout: 500ms
      dataset: wallets.jsonl
```

- **`name`**——运行在标题中的名称。
- **`tls: false`**——测试台不加密。TLS 默认开启；`ca`、`cert`、`key`、`server_name` 见
  [`examples/tour-tls.yaml`](../../examples/tour-tls.yaml)（下文 “TLS”）。
- **`metadata`**——每次调用附带的请求头。`${LEETTEST_TOKEN}` 来自环境变量：token 既不出现在配置里，
  也不出现在 shell 历史中，LeetTest 也不会在任何地方输出请求头的值。变量未设置是启动前的错误，
  而不是用空 token 运行。
- **`max_response_size`**——超过上限的应答算作 `bad response`，而不是成功。
- **`warmup`**——前 3 秒的调用会发往目标，但不计入百分位。
- **`rps`、`duration`**——每个方法各自设定。Open model：即使之前的调用尚未应答，调用也按计划
  发出，延迟从计划时刻算起。慢服务既不会拖慢负载，也藏不住自己的慢。
- **`timeout`**——等待应答的时长（默认 2 s）。
- **`data`**——请求体，普通 YAML。结构通过 reflection 从服务获取，不需要 `.proto`。请求体中的
  错误会在启动前暴露。
- **`dataset`**——用请求体文件代替 `data`，每行一个 JSON
  （[`examples/wallets.jsonl`](../../examples/wallets.jsonl)）：第一次调用用第一行，第二次用第二行，
  用完最后一行后从头再来。报告在方法那一行旁边说明文件用了多少。
- **`connections`**——多少条连接承载负载（默认 1）。在 L4 负载均衡器后面，一条连接只给
  一个后端施压；N ≥ 2 时报告会多出一个 `Connections:` 块，每条连接一行。

多个方法就是多个调用，各有各的速率。一个方法对应一个调用：报告按方法给出。

## 4. 运行

```console
$ export LEETTEST_TOKEN=demo
$ leettest -c examples/tour.yaml
```

启动前，LeetTest 会连接目标，通过 reflection 检查每个方法，构建请求体，并检查在途上限
（`-max-in-flight`）能否承受目标挂起。运行前能知道的一切，都会在第一次调用之前作为错误报告。

在终端中你会看到实时界面：按秒显示 RPS、在途调用、错误和百分位，每个方法一个标签页，`?` 查看帮助。
**停止：**第一次按 `q`（或 Ctrl+C）不再发出新调用，已发出的调用等到各自超时；第二次会截断它们并
仍然输出报告；第三次直接退出、不输出报告。

没有终端时（CI、输出到文件），改为每秒在 stderr 输出一行进度。

**TLS。** 使用 `-mtls` 时，测试台在启动时把 CA、自己的证书和一份客户端证书写入
`test/stand/certs`，并要求客户端证书；[`examples/tour-tls.yaml`](../../examples/tour-tls.yaml)
指向这些文件：

```console
$ go run ./test/stand/cmd/stand -mtls -delay 20ms -life 30s
$ leettest -c examples/tour-tls.yaml
```

## 5. 报告

报告输出到 stdout，只用 ASCII——可以存成文件或用 `grep` 查找。上面的体验以如下结果结束：

```
run finished: 127.0.0.1:50051 in 40.5s
sent 12950, failed 5248

method                                           sent   failed    sent/s       p50       p90       p95       p99
grpc.health.v1.Health/Check                     11100     4499       300    20.5ms    >500ms    >500ms    >500ms
wallet.v1.WalletService/GetBalance               1850      749        50    20.5ms    >500ms    >500ms    >500ms
  data: 4 of 4 requests from wallets.jsonl, each used up to 500 times

warm-up 1050 sent, excluded from stats

grpc.health.v1.Health/Check: at 300 rps, 4499 of 11100 calls (40.5%) got no answer within 500ms,
and nothing after the call sent at 25.0s of the run got one.
...
failed calls by gRPC code:
grpc.health.v1.Health/Check codes sent by the target: DeadlineExceeded 2
grpc.health.v1.Health/Check codes set by the client: DeadlineExceeded 4497
...
start lag, how late calls began against their schedule: p99 109us, max 225us.
...
5248 requests were abandoned before answering. A percentile shown as "> value"
is a lower bound: the real tail lies above it. Raise the timeout to see it.
```

（Linux，Docker，9a08876。）

要点：

- **服务在多大负载下开始扛不住。** “at 300 rps … nothing after the call sent at 25.0s”——目标在
  第 25 秒沉默，恰好是测试台被设定的时刻。时刻按调用实际发出的时间计算，而不是按计划时间：落后的
  压测机不会被当成目标的沉默。
- **`>500ms`** 不是数字，而是下界：部分调用没有得到应答，它们真实的延迟高于超时。LeetTest 不会
  用超时值顶替未知的数值。
- **代码分两行。** “sent by the target”——状态来自目标（或其前面的代理）；“set by the client”
  ——由我们的客户端设置，没有收到应答。对开发者来说这是两回事。
- **`start lag`**——压测机落后于计划多少。如果很大，瓶颈在压测机所在的机器，而不在目标；当延迟
  尾部在我们这一侧等待时，报告会自己指出这一点。
- **`clock step`**——机器时钟的精度；在 Linux 上是纳秒级，不会出现这一行。在 Windows 上（约
  0.5 ms）会出现，如果比 p50 的四分之一还粗，运行无效：小于时钟步长的数字没有意义。

失败类别（`overload`、`failure`、`request error`、`timed out`、`cut off`、`unreachable` 等）
及其含义见 [README 中的“阅读报告”](README.md#阅读报告)。

## 6. 目标的其他行为

同一份配置对另一种模式的测试台——报告会显示什么：

```console
$ go run ./test/stand/cmd/stand -delay 20ms -freeze-at 10s -freeze-for 2s -life 60s
```
超时 500 ms 时冻结 2 秒：p50 和 p90 保持在 21 ms 左右，暂停期间到达的调用没有得到应答——约 4%
`got no answer within 500ms`，p99 输出为 `>500ms`。百分位不做平均：短暂冻结体现在尾部，而不会被
均值抹平。

```console
$ go run ./test/stand/cmd/stand -delay 20ms -fail-every 10 -life 60s
```
每第十个调用返回 `RESOURCE_EXHAUSTED`：类别为 `overload`，其代码出现在 “sent by the target” 行。

```console
$ go run ./test/stand/cmd/stand -delay 150ms -max-streams 1 -life 60s
```
目标每条连接只允许一个流，并且每个应答保持 150 ms：通过一条连接每秒最多只能应答 6–7 个调用。
报告会说明调用卡在哪里：`connections: 1; target stream limit 1` 和
`not sent N: waited for a stream N`——调用在我们这一侧等待流，没能在超时前到达目标。这是连接的
上限，而不是目标慢。

## 7. 脚本与 CI

```console
$ leettest -c load.yaml -output json > report.json
$ echo $?
```

JSON 是版本化的（`schema_version`）；结论以字段而不是文本给出：`invalid_reasons`、
`methods[].invalid_reason`、`tail_wait_cause`、`methods[].silent_from_s`。说明见
[README 中的“供脚本和 CI 使用的 JSON”](README.md#供脚本和-ci-使用的-json)。

退出码：

| 代码 | 含义 |
|---|---|
| `0` | 计划执行完毕，报告完整 |
| `1` | 没有运行：命令行参数、配置、连接。没有报告 |
| `2` | 运行无效：触及在途上限、某个方法所有调用都是请求错误，或主机时钟比 p50 的四分之一还粗。有报告，但数字与负载无关 |
| `3` | 在计划结束前停止。报告涵盖已运行的部分 |
| `130`、`143` | 被 Ctrl+C 或 SIGTERM 终止，没有报告 |

目前还没有通过/失败阈值：在 CI 中检查退出码和 JSON 字段。

## 8. 完全没有服务

```console
$ leettest -c examples/leettest.yaml -fake -fake-delay 30ms -fake-jitter 10ms
```

`-fake` 加载内置的假目标代替服务——无需服务即可查看界面和报告。报告会标记为 `fake target`。

## 下一步

- [参考手册](reference.md)——每个字段和命令行参数；[README](README.md)——阅读报告。
- [常见陷阱](pitfalls.md)——什么会破坏测量。
- [CONTRIBUTING](../../CONTRIBUTING.md)——以个人身份或借助 AI 智能体提出修改。
