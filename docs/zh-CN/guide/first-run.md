# 第一次运行

[← 返回文档](../README.md)

> 译自 [docs/ru/guide/first-run.md](../../ru/guide/first-run.md)，对应 feb2d1d，2026-11-10。如有出入，以俄文版为准。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../../en/guide/first-run.md)。

这里讲的是从安装到第一份报告的路径：安装 LeetTest，为两个方法写好配置，运行并读懂结果。为此不需要你自己的
服务：下面有一个随 LeetTest 一起提供的测试服务。如果你通过 AI 智能体工作，把 [docs/agent.md](../../agent.md)
交给它（给它的那句话在 [README](../README.md#通过-ai-智能体) 中）：它会自己完成同样的步骤。

## 在哪里运行

请在 Linux 或 macOS 上、紧挨着服务运行：同一网络、同一集群或 CI 的 runner 上。位于 LeetTest 与服务之间的
一切都会计入延迟，看起来像服务的耗时：在每秒 1000 个请求下，经 Docker Desktop 端口转发 p99 为 38 毫秒，在
Docker 网络内部为 2 毫秒（我们的测试台，单台机器；在其他条件下数字会不同）。

Windows 适合试用。在我们的 Windows 11 机器上时钟步长约为 0.5 毫秒（LeetTest 自己测量并打印），对快速服务的
运行 LeetTest 可能判为无效（`clock_step`，退出码 2）：时钟步长与延迟本身相当。要做测量请用 Linux。关于运行
位置的更多内容见[参考](../../en/reference.md#where-to-run)（英文）。

## 安装

**Linux 和 macOS**（amd64 和 arm64）。需要 `curl` 和 `tar`：

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
```

脚本把最新版本安装到 `~/.local/bin` 并核对校验和。如果该目录不在 `PATH` 中，它会打印一行用于 shell 配置的
命令：在当前终端执行它，要长期生效就写进配置文件。更换目录用变量 `LEETTEST_INSTALL_DIR`，指定其他版本用
`LEETTEST_VERSION`；变量写在 `sh` 之前，而不是 `curl` 之前：

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | LEETTEST_VERSION=v0.2.0 sh
```

**Windows。** 从[发布页面](https://github.com/yhgrwav/leettest/releases/latest)下载 `.zip` 压缩包：把
`leettest.exe` 解压到 `PATH` 中的某个目录。

**任何装有 Go 1.26 或更新版本的系统**（[安装 Go](https://go.dev/doc/install)；更旧的 Go 配合
`GOTOOLCHAIN=auto` 会自己下载所需版本）：

```sh
go install github.com/yhgrwav/leettest/cmd/leettest@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

任何方式安装后都这样检查：

```sh
leettest -version
```

## 运行

保存为 `leettest.yaml`，换上你的服务的地址和方法：

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

- `tls: false`——不加密的服务。没有这一行时 TLS 是开启的。
- `method`——完整名称：`包.服务/方法`。不需要 `.proto`：LeetTest 通过 gRPC server reflection 从服务获取
  schema（服务自己提供方法和消息的列表）。如果服务上的 reflection 被关闭，见[连接](connection.md)。
- `rps`——该方法每秒的请求数，`duration`——对它施压多久。
- `warmup`——最初几秒会发给服务，但不计入统计。

不加负载地检查配置：LeetTest 会连接、检查方法、给每个方法发一个请求并显示请求和响应。没有报告。

```sh
leettest -c leettest.yaml -dry-run
```

运行：

```sh
leettest -c leettest.yaml
```

启动前 LeetTest 会连接服务并检查每个方法和请求体。配置错误、未知方法、请求体中的错误、设置了请求头却收到
`Unauthenticated`——退出码 1，不会向服务发出任何请求。响应 `PermissionDenied` 不会中止运行，详见
[连接](connection.md#报告里会出现什么)。

终端里会出现实时画面：每秒请求数、未响应的请求、错误和按秒的百分位数，每个方法一个标签页，`?` 是帮助。
停止：第一次 `q`（或 Ctrl+C）——不再发出新请求，已发出的等待各自的超时；第二次——中断它们，报告仍会打印；
第三次——不出报告直接退出。没有终端时（CI、输出到文件、ssh）取代画面的是每秒一行写到 stderr 的进度；
`-plain` 在终端里也会启用它：

```text
32.0s  sent 25600  rps 800  in-flight 47  failed 51  not-sent 0  p99 43ms
```

`in-flight`——已发出但还没有响应的请求；`not-sent`——按计划本该发出却没有发出的请求（例如在等空闲连接）。

报告输出到 stdout，进度和错误输出到 stderr。在终端里第一次运行会询问配色主题（只问一次；`-plain` 跳过
这个问题）。

## 报告里有什么

结尾每个方法一行：发出了多少、多少失败、`sent/s` 以及百分位数 p50、p90、p95、p99。要点：

- 延迟从请求的**计划**时刻开始计算。如果服务或 LeetTest 自己使请求延迟，这段延迟会计入测得的延迟。请求
  按计划发出，不等待前面请求的响应。
- `>2.0s` 不是一个数，而是“不小于 2 秒”：一部分请求因超时而中断，它们真实的延迟更高。LeetTest 不会拿超时
  去顶替未知的值。
- 错误按类别划分（`overload`、`failure`、`timed out` 等），报告下方的 gRPC 状态码分成两行：“sent by the
  target”——状态由服务或其前面的代理发来，“set by the client”——状态由 LeetTest 设定，没有收到响应。
- 退出码 `0` 并不表示“服务健康”：它表示计划已执行完。想让运行自己给出“通过/不通过”的结论，请设置
  [限值](../../en/report.md#pass-limits-a-run-as-a-check)（英文）。

每个数字和类别见[如何阅读报告](../../en/report.md#reading-the-report)（英文）一节。

## 用测试服务试跑

测试服务就在仓库里：一个行为可控的 gRPC 服务。它响应 `grpc.health.v1.Health/Check` 和
`wallet.v1.WalletService`，并像真实服务一样提供 reflection。正确答案事先已知，所以可以把报告与服务实际做了
什么对照。需要 `git` 和 Go 1.26+；同一个克隆里的测试服务和 `leettest` 在版本上与它的配置相符。

```sh
git clone --depth 1 https://github.com/yhgrwav/leettest && cd leettest
go build -o bin/ ./cmd/leettest ./test/stand/cmd/stand
bin/stand -delay 20ms -hang-from 25s -life 3m > stand.log 2>&1 &
sleep 1
```

服务在后台运行（`sleep 1`——等一秒让它开始监听端口），3 分钟后自行停止（`-life 3m`：预热、40 秒运行和
余量）。它在 20 毫秒内响应，而在第一个请求后的 25 秒起完全不再响应——这就是在负载下垮掉的服务的样子。

配置 [`examples/tour.yaml`](../../../examples/tour.yaml) 用到了所有可以针对本地服务检查的字段：

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
      dataset_order: random
      dataset_seed: 42
```

- `name`——运行在标题和 JSON 报告中的名称。
- `metadata`——每个请求的请求头。`${LEETTEST_TOKEN}` 取自环境：令牌既不进入配置，也不进入 shell 历史；
  请求头的值不会进入报告。变量未设置——启动前报错，而不是带着空令牌运行。
- `max_response_size`——超过上限的响应算作 `bad response`，而不是成功。
- `timeout`——等待响应多久（默认 2 秒）。
- `data`——请求体，用普通 YAML。请求体中的错误在启动前就能看到。
- `dataset`——代替 `data` 的请求体文件，每行一个 JSON
  （[`examples/wallets.jsonl`](../../../examples/wallets.jsonl)）：第一个请求用第一行，第二个用第二行，
  最后一行之后回到第一行。`dataset_order: random` 在每一轮打乱文件；报告会打印 seed，`dataset_seed` 可以
  重现顺序。在方法那一行旁边，报告会说明文件用到了多少。
- `connections`——有多少条连接承载负载（默认 1）。在 L4 负载均衡器之后，一条连接只对一个服务实例施压；
  两条及以上时报告中会出现 `Connections:` 块——每条连接一行。

多个方法——`calls` 中的多项，每项有自己的速率。一个方法——一项。

```sh
export LEETTEST_TOKEN=demo
bin/leettest -c examples/tour.yaml
```

`demo` 是这次演练用的令牌。运行结束时是这样：

```text
run finished: 127.0.0.1:50051 in 40.5s
name: tour
settings: warm-up 3s, max in flight 5000, connections 1
sent 12950, failed 5248

method                                           sent   failed    sent/s       p50       p90       p95       p99
grpc.health.v1.Health/Check                     11100     4499       300    20.5ms    >500ms    >500ms    >500ms
  plan: 300 rps for 40s
wallet.v1.WalletService/GetBalance               1850      749        50    20.5ms    >500ms    >500ms    >500ms
  plan: 50 rps for 40s
  data: 4 of 4 requests from wallets.jsonl, each used up to 500 times, random order, seed 42

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

（Linux，Docker，9a08876。）其中的要点：

- **这是怎样的一次运行。** `name:` 和 `settings:` 重复运行的名称和启动参数，方法下的 `plan:` 是给它设定的
  速率和时长：贴进工单的报告可以与另一份对比。
- **服务在多大负载下开始吃不消。** “at 300 rps … nothing after the call sent at 25.0s”——服务在第 25 秒沉默，
  正如被要求的那样。这个时刻按请求的发送时间而不是按计划计算：LeetTest 自身的落后不会被当成服务的沉默。
- **`>500ms`**——“不小于 500 毫秒”：一部分请求没有等到响应。
- **`start lag`**——LeetTest 落后于计划多少。如果很大，瓶颈是运行 LeetTest 的机器而不是服务；当延迟的尾部
  可以用 LeetTest 内部的延迟而不是服务来解释时，报告会自己说明。
- **`clock step`**——机器时钟的精度。在 Linux 上以纳秒计，没有这一行。在 Windows 上（我们这里约 0.5 毫秒）
  会出现这一行，如果步长比 p50 的四分之一粗，运行就无效：小于时钟步长的数字没有意义。

同样的配置，针对处于另一种模式的测试服务。每个块先停止之前的服务（`kill %%` 结束该终端中最后一个后台命令），
再启动新的服务并运行；令牌 `LEETTEST_TOKEN` 已经设置好。

服务在 500 毫秒超时下冻结 2 秒：

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -freeze-at 10s -freeze-for 2s -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```

p50 和 p90 保持在约 21 毫秒，而落入暂停期的请求没有等到响应：约 4% 为 `got no answer within 500ms`，
p99 打印为 `>500ms`。百分位数不做平均：短暂的冻结体现在尾部，不会被平均值抹平。

每十个响应有一个是 `RESOURCE_EXHAUSTED` 错误：

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -fail-every 10 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```

类别是 `overload`，状态码在“sent by the target”那一行。

服务每条连接只允许一个流，每个响应耗时 150 毫秒：

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 150ms -max-streams 1 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour.yaml
```

在一条连接上，服务每秒最多只能响应 6–7 个请求。报告会说明它们卡在了哪里：`connections: 1; target stream
limit 1` 和 `not sent N: waited for a stream N`——请求在 LeetTest 内部等待空闲的流，在各自超时之前没有发到
服务。这是连接的限制，而不是服务慢。

使用 TLS：带 `-mtls` 的测试服务（mTLS——双向 TLS：服务不仅向客户端出示自己的证书，也验证客户端的证书）在
启动时把证书写到 `test/stand/certs` 并要求客户端证书；
[`examples/tour-tls.yaml`](../../../examples/tour-tls.yaml) 指向这些文件。

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -mtls -delay 20ms -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/tour-tls.yaml
```

完全不需要服务：`-fake` 对内置的替身而不是服务施压。配置取自克隆，其中的 `target` 被忽略。报告会标注
`fake target`。

```sh
bin/leettest -c examples/leettest.yaml -fake -fake-delay 30ms -fake-jitter 10ms
```

## 常见错误

- **把 `0` 当成“服务健康”。** 退出码 `0` 的意思是“计划已执行完”。健康状况由 `pass` 限值和阅读报告来判断。
- **服务不加密，却没有 `tls: false`。** TLS 默认开启，连接不加密的服务会在启动前失败，退出码为 1。
- **方法没有写完整。** 需要 `包.服务/方法`；拼写错误 LeetTest 会在第一个请求之前发现。
- **服务上的 reflection 被关闭。** 带 `data` 的方法无法启动；可以用 `.proto` 文件或 protoset（把方法描述
  打包成一个文件）提供 schema——见[连接](connection.md)。
- **在 Windows 上出现退出码 2 和 “clock step”。** 请从 Linux 上、紧挨着服务运行。
- **通过 Docker Desktop 的端口转发运行。** 延迟会包含这段路径。

## 接下来

- [连接](connection.md)——TLS、请求头、令牌。
- [负载](load.md)——方法、速率、爬升、请求体。
- [参考](../../en/reference.md)（英文）——每个字段和标志。
- [常见陷阱](../pitfalls.md)——什么会破坏测量。
