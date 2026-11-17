# 什么是调用串联，为什么重要？

[← 返回文档](README.md)

> 译自 [docs/ru/chaining.md](../ru/chaining.md)，对应 46c2f53，2026-11-17。如有出入，以俄文版为准。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../en/chaining.md)。

> **状态：自 v0.9 起可用（`scenario`、`steps` 和 `extract`，见[参考手册](reference.md#场景调用之后的步骤)）。** 这里原先描述的池已被它取代：调用之后的步骤，加上把应答字段传给下一步。

真实服务中有一半的方法无法用静态数据调用。要压测转账，需要已存在的钱包：`Transfer`
需要 `from` 和 `to`，而它们只能从 `CreateWallet` 的响应中获得。

常见的办法是提前准备测试数据。在数据可以预先准备的地方，这是正确的路径，为此提供了从文件注入数据：
CSV 或 JSON [数据集](reference.md#每次调用不同的请求dataset)，每个请求一行。但并非一切都能预先准备：
一次性令牌、会话、针对创建过程本身的负载。而在同样两个钱包之间转账一千次，压的不是转账，
而是数据库中的一行及其锁。

第二个办法是写脚本，先创建钱包再立刻从它转账。这样你测到的是
“创建加转账”这一序列，而不是单个转账；如果脚本不分别测量各个步骤，就无法从总数中得到每一步对延迟
的贡献。

我们用场景来解决。一个调用和它之后的步骤是一次迭代：它按顺序进行，每个步骤在报告中各占一行，
有自己的百分位。调用或步骤给它应答中的字段命名（`extract`），下一个步骤把它们写进自己的请求，
形式为 `{{extract.<name>}}`。这些值只在一次迭代内有效：每次迭代有它自己的钱包。

```yaml
load:
  calls:
    - method: wallet.v1.WalletService/CreateWallet
      rps: 5
      duration: 1m
      templates: true
      scenario: create-and-pay
      extract:
        wallet: wallet_id
      steps:
        - method: wallet.v1.WalletService/Transfer
          templates: true
          data:
            from: "{{extract.wallet}}"
            to: "{{uuid}}"
            amount: 100
```

这不需要改动别人的 `.proto`：字段路径会在启动时依据 reflection 数据（或 `app.proto` /
`app.protoset`）检查——缺少字段就是运行前附带路径的明确错误，而不是压测中途的垃圾响应。
还会依据 schema 检查这个值是否适合下一步的字段：从 `int64` 到 `int32` 的数字过不了，
也不会有任何值被悄悄截断。

应答中没有所需的值时会怎样。悄悄发出空值是最糟的做法：运行将不再符合设定的计划，而这一点是看不出来的。
所以这样的应答是 `bad response`：应答到了，但客户端无法使用它。迭代到此结束，该步骤算“在此失败”，
后面的步骤没有到达，而状态码仍是目标发来的 `OK`。如果某个方法的每次调用都这样应答，运行无效，
报告会说明应答中没有这些值。

这与不同调用之间共享的值池不是一回事：值不会从一次迭代传到另一次，也不会累积。需要共用的预先准备的数据时，
用数据集。

细节——字段类型、值的文本、替换规则——见
[参考手册](reference.md#extract较早应答中的值)。一个在参考测试台上可直接运行的示例是
[`examples/ru/scenario-extract.yaml`](../../examples/ru/scenario-extract.yaml)（注释为俄文；英文版是
[`examples/scenario-extract.yaml`](../../examples/scenario-extract.yaml)）。
