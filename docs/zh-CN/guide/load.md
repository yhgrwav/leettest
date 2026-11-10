# 负载

[← 返回文档](../README.md)

> 译自 [docs/ru/guide/load.md](../../ru/guide/load.md)，对应 feb2d1d，2026-11-10。如有出入，以俄文版为准。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../../en/guide/load.md)。

对哪些方法、以什么频率、持续多久施加负载。速率以每秒请求数给出：请求按计划发出，不等待前面请求的响应，
延迟从计划时刻开始计算。

## 最小配置

```yaml
app:
  target:
    ip: localhost
    port: 50051
  tls: false

load:
  warmup: 5s
  ramp: 30s
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      timeout: 500ms
      data:
        wallet_id: "w-123"

    - method: wallet.v1.WalletService/Transfer
      rps: 50
      duration: 1m
```

```sh
leettest -c leettest.yaml
```

- `method`——完整名称：`包.服务/方法`。一个方法对应 `calls` 中的一项；多个速率不同的方法就是多项。
- `rps`——每秒请求数，整数。`10.5` 会被拒绝，而不是悄悄取整。
- `duration`——对该方法施压多久：`30s`、`5m`、`1h`。
- `timeout`——等待响应多久。未指定——`2s`。零不会关闭超时，而是报错。
- `warmup`——最初几秒会发给服务，但不计入统计，因为冷缓存会破坏统计。预热包含在 `duration` 内，并且
  必须短于每个调用。报告里它单独占一行 `warm-up N sent`。
- `ramp`——爬升：每个方法在这段时间内从零线性升到它的 `rps`，然后才开始它的 `duration`。运行时长为
  `ramp` + `duration`（设置了 `warmup` 时它包含在 `duration` 内）。爬升阶段的请求不计入统计；`pass`
  限值和失败占比只按保持阶段计算。示例——[`examples/ramp.yaml`](../../../examples/ramp.yaml)。
- `data`——请求体，用普通 YAML 按消息的形状书写。没有 `data` 就发送空消息。

同一方法的所有请求都带着同一个请求体。对于带幂等键的写入，这意味着只有第一个请求会创建记录，其余走重试
路径：被测量的就是这条路径。要每次调用发不同的请求，用 `dataset` 文件（每行一个 JSON）或带 `{{uuid}}`
和 `{{seq}}` 的 `templates: true`；见[参考](../../en/reference.md#request-body)（英文）。

## 报告里会出现什么

- 每个方法一行：`sent`、`failed`、`sent/s` 和百分位数。其下的 `plan:` 是你设定的速率和时长。
- `sent/s`——已发送的请求数除以发送时间：运行结尾服务卡住不会拉低这个数字。
- 如果发出的请求少于 `plan:`，报告会说明原因：`not sent`（按计划本该发出而没有发出的请求）及其原因。
- 方法表格下的 `over time` 块按时间窗口显示速率、失败占比、p50 和 p99 如何变化。使用 `ramp` 时，最初
  几行就是爬升：那里 `sent/s` 低是计划内的，不是坏了。请留意速率还没到达平台期，p99 或失败占比就先上升
  的那一行。
- 服务卡住时，表格下的一行会指出从哪一秒起它没有响应任何请求。

每一部分如何阅读——[如何阅读报告](../../en/report.md#reading-the-report)（英文）。

## 启动前

LeetTest 在第一个请求之前检查配置：未知字段、错误的值、服务没有的方法、请求体中的错误——退出码 1，不会
向服务发出任何请求。还会检查未响应请求的上限（in-flight：已发出、响应未到）：卡住的服务会积累
`rps × timeout` 个这样的请求，各方法之和不得超过 `-max-in-flight`（默认 5000）。错误信息会给出两条出路：
多大的超时可行，以及需要多高的上限。

## 常见错误

- **`warmup` 长于或等于 `duration`。** 该调用将没有任何被测量的请求：配置错误。
- **想要负载“增长”，却直接设置了 `rps`。** 没有 `ramp` 时方法以满速启动。要增长就设置 `ramp`；想知道
  服务在多大负载下开始吃不消——[断点搜索](../../en/report.md#breaking-point-search)（英文）。
- **超时对上限来说太大。** 启动前报错，见上文。
- **超时太短。** 来不及响应的请求会因超时而中断，看起来像服务的失败；它们的百分位数显示为 `>500ms`——
  “不小于 500 毫秒”。
- **每个请求都一样，而需要不同的请求。** 见上文关于 `dataset` 和 `templates` 的说明。
- **通过 ssh 在没有终端的情况下运行。** 加上 `-plain`：每秒一行进度，代替实时画面。

## 接下来

- [连接](connection.md)——TLS、请求头、令牌。
- [参考](../../en/reference.md)（英文）——每个字段和标志。
