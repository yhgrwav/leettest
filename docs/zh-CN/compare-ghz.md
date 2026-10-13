# 在同一测试台上对比 LeetTest 与 ghz

> 译自 [docs/ru/compare-ghz.md](../ru/compare-ghz.md)，对应 6adffbf，2026-10-12。如有出入，以俄文版为准。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../en/compare-ghz.md)。

本页在一台行为已知的测试服务器上对比 LeetTest 与 [ghz](https://ghz.sh)。问题很具体：当服务器
卡住或变慢时，报告能否反映出来？
与 ghz、k6 和 JMeter 的功能对比见[表格](why.md#与-ghzk6-和-jmeter-的对比)。

简而言之：

- **服务器表现正常时**，两个工具报告的延迟相同（模式 A）。
- **服务器冻结 2 秒时**（模式 B1），同步模式（默认）的 ghz 在 `-c 10` 时报告 p99 为 11.4ms，在默认
  的 `-c 50` 时为 11.6ms。LeetTest 报告 1.71s。使用 `--async` 的 ghz 同样报告 1.71s。
- **服务器慢于负载所需时**（模式 B2），同步模式（默认）的 ghz 在 `-c 10` 时只发出了请求的 6000 次
  调用中的 2970 次。对已发出的调用，它的延迟是正确的，但那只是所要求负载的一半。在默认的 `-c 50`
  下它会发出全部 6000 次。

这不是 ghz 的缺陷。同步模式运行固定数量的 worker（`-c`，默认 50 [1]）。每个 worker 等自己的调用
完成后才取下一个 [2]：这是 closed model，设计如此。服务器卡住时，worker 也在等待，本应在卡顿期间
发出的调用没有按时发出，因此它们的延迟从未被测量。这种效应称为 *coordinated omission*。使用
`--async` 时，ghz 不会等一个调用完成再发下一个 [2][3]：这是 open model，得到的数字与 LeetTest
相同。LeetTest 只有 open model。

这里关于 ghz 的所有说法都对照 v0.121.0 核实过——来源见页末。

## 测试设置

测试台是本仓库中的 `test/stand`：一个在设定延迟后应答 `grpc.health.v1.Health/Check` 的 gRPC 服务器。
它可以冻结设定的时长，或以更慢的速度应答。

| 模式 | 测试台 | 模拟的情况 |
|---|---|---|
| A | `-delay 10ms` | 对照：跟得上的服务器 |
| B1 | `-delay 10ms -freeze-at 10s -freeze-for 2s` | 运行 10s 时卡顿 2 秒（GC 停顿、锁） |
| B2 | `-delay 100ms` | 慢于负载所需的服务器 |

每次运行的负载：

- 每秒 200 次调用，持续 30 秒，共 6000 次调用；
- 超时 5s；
- 一条连接。

ghz 的参数，同步模式（封闭模型）：

```
ghz --insecure --call grpc.health.v1.Health/Check -d '{}' --rps 200 -z 30s -t 5s \
    --duration-stop wait --connections 1 -c 10
```

ghz 的参数，使用 `--async`（开放模型）：

```
ghz --insecure --call grpc.health.v1.Health/Check -d '{}' --rps 200 -z 30s -t 5s \
    --duration-stop wait --connections 1 -c 10 --async
```

同步模式的 ghz 分别以 `-c 10` 和 ghz 默认的 `-c 50` 运行。`--duration-stop wait` 让 30s 时仍在
途的调用完成并计入。

每个工具在每种模式下运行 5 次，每次运行都使用新的测试台。运行顺序轮换，没有哪个工具总是最先运行。
表格给出 5 次运行的中位数，方括号中为最小值和最大值。

环境：

- LeetTest 提交 `fb8c7b7`；
- ghz v0.121.0（grpc-go v1.56.3）；
- Go 1.27.1；
- Linux（内核 6.18），运行在 `golang:1.27.1` 容器中，参数 `--cpus=4`；Windows 11 上的 Docker
  Desktop，即 Windows 主机上 WSL2 虚拟机里的 Linux；
- 主机处理器 AMD Ryzen 9 5900X（12 核，24 线程），虚拟机 16 GB 内存；
- 工具与测试台在同一个容器中，通过 loopback 通信。

## 结果

### B1：冻结 2 秒

| 工具 | 已发送 | 失败 | p50 | p90 | p95 | p99 | 超过 1s 的调用 |
|---|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 10.4ms [10.3–10.4] | 10.6ms [10.5–10.6] | 509ms [508–509] | 1.71s [1.71–1.71] | 不输出 |
| ghz，同步（`-c 10`） | 5999 [5999–6004] | 0 | 10.7ms [10.7–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.3–11.4] | 11.4ms [11.4–11.5] | 10 [10–10] |
| ghz，同步（`-c 50`，默认） | 5999 [5999–6000] | 0 | 10.7ms [10.7–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.4–11.4] | 11.6ms [11.5–12.3] | 50 [50–50] |
| ghz，`--async` | 6000 [5999–6000] | 0 | 10.7ms [10.7–10.7] | 11.4ms [11.4–11.4] | 512ms [506–518] | 1.71s [1.71–1.72] | 202 [202–203] |

以 200 RPS 计，2 秒内应发出约 400 次调用。使用 `--async` 时，ghz 统计到 202 次超过 1s 的调用：
冻结前半段应发出的调用等待超过一秒。在同步模式下，`-c 10` 时有 10 次、`-c 50` 时有 50 次调用超过
1s，每个 worker 一次。分别占调用的 0.17% 和 0.83%，不足以影响 p99。冻结期间 worker 停滞不动。
冻结结束后，ghz 立即把错过的调用一次性发出：落后于计划时，它的节拍器不会在下一次调用前等待 [4]。
这些调用碰到的是已经恢复快速的服务器。它们发送前的等待不计入 ghz 报告的延迟：延迟从调用本身开始
时算起 [5]。

各行的已发送调用数相同，因此计数无法暴露这次卡顿。

### B2：慢于负载所需的服务器

| 工具 | 已发送 | 失败 | p50 | p90 | p95 | p99 |
|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 100ms [100–100] | 101ms [101–101] | 101ms [101–101] | 102ms [101–102] |
| ghz，同步（`-c 10`） | 2970 [2970–2970] | 0 | 100.9ms [100.9–101] | 101.3ms [101.3–101.4] | 101.4ms [101.4–101.5] | 101.6ms [101.6–101.7] |
| ghz，同步（`-c 50`，默认） | 6000 [5999–6001] | 0 | 100.5ms [100.5–100.6] | 101.3ms [101.3–101.4] | 101.4ms [101.3–101.5] | 101.5ms [101.4–101.7] |

同步模式的 ghz 每秒最多发出 c / 延迟 次调用，其中 c 为 worker 数。`-c 10`、延迟 100ms 时就是每秒
100 次，只有所要求的 200 次的一半。延迟是对的，负载不对。只看延迟列，这次运行看起来像一台能以
101ms 处理 200 RPS 的服务器，而实际上它处理的是 100 RPS。

在默认的 `-c 50` 下上限是每秒 500 次调用，完整的负载都能发出。只要服务器足够慢，任何 c 都会碰到
同样的上限：`-c 50` 时延迟超过 250ms 就会碰到。

### A：对照

| 工具 | 已发送 | 失败 | p50 | p90 | p95 | p99 |
|---|---|---|---|---|---|---|
| LeetTest | 6000 [6000–6000] | 0 | 10.3ms [10.3–10.4] | 10.5ms [10.5–10.6] | 10.5ms [10.5–10.6] | 10.6ms [10.6–10.8] |
| ghz，同步（`-c 10`） | 6000 [6000–6008] | 0 | 10.6ms [10.6–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.3–11.4] | 11.4ms [11.4–11.5] |
| ghz，`--async` | 6001 [5996–6002] | 0 | 10.7ms [10.7–10.7] | 11.3ms [11.3–11.3] | 11.4ms [11.4–11.4] | 11.4ms [11.4–11.5] |

三者相差不超过 1ms。在此环境中 ghz 比 LeetTest 高出 0.3–0.9ms；原因我们没有测量。

## 复现

安装 ghz，然后在仓库根目录运行：

```sh
go install github.com/bojand/ghz/cmd/ghz@v0.121.0
go run ./test/compare-ghz -ghz "$(go env GOPATH)/bin/ghz"
```

脚本会构建测试台和 LeetTest，并把每种模式运行 5 次，约需 25 分钟。它会输出本页的表格，并连同每次
运行的原始输出写入 `test/compare-ghz/out/`。

短运行——只跑一次、只跑模式 B1，约需 2 分钟：

```sh
go run ./test/compare-ghz -ghz "$(go env GOPATH)/bin/ghz" -runs 1 -modes B1
```

模式有 `A`、`B1`、`B2`（`-modes`），工具有 `leettest`、`ghz-sync`、`ghz-sync50`、`ghz-async`
（`-variants`）；其余参数见 `go run ./test/compare-ghz -h`。

换一台机器数字会不同，规律不会变：在 B1 中，同步模式的 ghz 漏掉了冻结，而 LeetTest 和使用 `--async`
的 ghz 都能显示出来。

## ghz 相关来源（v0.121.0，核实于 2026-10-02）

1. `-c` 默认为 50——[`runner/options.go`，`NewConfig`](https://github.com/bojand/ghz/blob/v0.121.0/runner/options.go)。
2. 同步 worker 调用 `makeRequest` 并等待它完成；使用 `--async` 时每次调用启动一个 goroutine——
   [`runner/worker.go`，`runWorker`](https://github.com/bojand/ghz/blob/v0.121.0/runner/worker.go)。
3. `--async`、`--duration-stop`、`--connections`——[参数文档](https://ghz.sh/docs/options)。
4. 落后于计划时，节拍器返回零等待——[`load/pacer.go`，`ConstantPacer.Pace`](https://github.com/bojand/ghz/blob/v0.121.0/load/pacer.go)；
   节拍通过无缓冲通道传给 worker——[`runner/requester.go`，`runWorkers`](https://github.com/bojand/ghz/blob/v0.121.0/runner/requester.go)。
5. 调用时长为 gRPC stats 事件中的 `EndTime − BeginTime`——[`runner/stats_handler.go`，`HandleRPC`](https://github.com/bojand/ghz/blob/v0.121.0/runner/stats_handler.go)。
