# <img src="../../assets/icon.png" width="24" alt="" align="top"> 参考手册

[Русский](../ru/reference.md) · [English](../en/reference.md) · [Deutsch](../de/reference.md) · [简体中文](reference.md)
[← 首页](README.md)

> 译自 [docs/ru/reference.md](../ru/reference.md)，对应 7b1db80，2026-10-19。如有出入，以俄文版为准。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../en/reference.md)。

每个配置字段、每个命令行参数，以及工具在启动前检查什么。如何阅读报告见
[README](README.md#阅读报告)。

## 配置

```yaml
name: wallet                     # 可选
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
      timeout: 2s
```

| 字段 | 作用 |
|---|---|
| `name` | 可选。运行在标题中的名称。省略时——如果所有方法属于同一个服务，则为服务名，否则为配置文件名 |
| `app.target` | 服务地址：`ip` 和 `port` |
| `app.tls` | TLS。省略时开启。`false`——不加密的连接 |
| `app.ca` | 用于校验服务的 PEM 证书文件，替代系统证书。需要 TLS |
| `app.cert`、`app.key` | PEM 格式的客户端证书及其私钥，用于要求 mTLS 的服务。必须同时设置，需要 TLS |
| `app.server_name` | 当服务证书不包含 `target` 中的地址时，用来校验证书的名称。需要 TLS |
| `app.metadata` | 每次调用的请求头：`authorization`、`x-api-key` 等。`${NAME}` 取自环境变量 |
| `app.max_response_size` | 一次调用接受的最大应答：`16MiB`、`512KB`。单位必填（`MB` = 10⁶ 字节，`MiB` = 2²⁰），小于 2 GiB。省略时为 4 MiB，与 gRPC 相同。更大的应答算作 `bad response`。在途的调用最多可能缓冲上限的两倍：默认每个最多 8 MiB |
| `app.connections` | 向目标打开多少条连接，1 到 256 的整数。省略时为 1。调用在这些连接间轮流进行，报告打印一个 `Connections:` 块，每条连接一行 |
| `app.compression` | 压缩请求体：`gzip`。省略时不压缩。报告在方法下打印 `bytes: <raw> -> <wire> per call`——未发布 |
| `app.proto.files` | 提供服务定义的 `.proto` 文件，用于没有 reflection 的服务。路径从配置文件所在目录算起。每个文件必须位于某个 `import_paths` 之内——未发布 |
| `app.proto.import_paths` | 在哪里查找这些文件及其 import，与 `protoc -I` 相同。路径从配置文件所在目录算起。省略时为该目录。标准类型（`google/protobuf/…`）不需要路径——未发布 |
| `app.protoset` | 用描述符集文件代替 `.proto` 文件，由 `protoc --include_imports --descriptor_set_out=FILE` 生成。路径从配置文件所在目录算起。不能与 `app.proto` 同时使用；两者都没有时，方法通过 reflection 获取——未发布 |
| `load.warmup` | 前 N 秒不计入百分位和 `sent`：冷缓存会扭曲它们。预热调用确实会到达目标；报告用一行 `warm-up N sent (M failed), excluded from stats` 输出它们——`sent` 加上这一行就是压测机尝试过的全部调用。除计为 unreachable 或 client error 的调用外，目标都收到了；`cut off` 和超时的调用可能没有完整到达：不打开 HTTP/2 窗口（流量控制）的目标只会收到请求头，它的计数器可能看不到这次调用。计入 `duration`，必须短于每个调用 |
| `load.calls[].method` | 方法全名：`package.Service/Method` |
| `load.calls[].rps` | 该方法每秒的请求数 |
| `load.calls[].duration` | 施压时长：`30s`、`5m`、`1h` |
| `load.calls[].timeout` | 等待应答的时长。省略时为 `2s`。零不会关闭超时，而是错误 |
| `load.calls[].data` | 请求体，见[下文](#请求体)。省略时为空消息 |
| `load.calls[].dataset` | 用请求体文件代替 `data`，见[下文](#每次调用不同的请求dataset)。不能与 `data` 同时使用 |
| `load.calls[].dataset_order` | `file`（省略时）或 `random`：文件中请求的顺序。需要 `dataset`——未发布 |
| `load.calls[].dataset_seed` | 0 到 9007199254740991 的整数，固定随机顺序。需要 `dataset_order: random`。省略时自行选取并在报告中输出——未发布 |
| `load.calls[].dataset_reuse` | `true`（省略时）或 `false`：为 `false` 时，整个进程中没有任何一条记录会发出两次。需要 `dataset`——未发布 |
| `load.calls[].pass` | 运行要通过必须满足的限值：`p50`、`p90`、`p95`、`p99`、`p999`、`errors`，见[下文](#pass-限值)。省略时没有限值——未发布 |
| `load.breakpoint.pass` | 崩溃点搜索的限值：`held_rps`，见[下文](#pass-限值)。省略时没有限值——未发布 |

配置严格解析：字段名拼写错误会报错并给出行号，取值错误会报错并给出调用的序号和方法，而不是以空负载
运行。`rps` 是整数：`10.5` 会被拒绝，而不是悄悄取整为十。`warmup` 必须短于每个调用，否则该调用
不会剩下任何被测量的请求。

**超时与在途上限。** 如果服务挂起，每个方法在超时触发前会有 `rps × timeout` 个请求在途，另加余量：
窗口边缘的一个请求，以及 `rps × 100 ms`，用于压测机在截止时间稍后才释放名额的情况。各方法之和不得
超过 `-max-in-flight`（默认 5000）。这会在启动前检查，错误信息会给出两条出路：多大的超时能放得下，
需要多大的上限。这样挂起的服务不会触及上限：运行会走到结束，报告会说明有多少调用在超时内没有得到
应答，以及目标在哪次发出的调用之后不再应答。如果上限仍被耗尽，说明有名额被占用超过截止时间 100 ms
以上——原因在压测机（CPU 不足）或发送方，报告会判定运行无效。同时报告会输出当时有多少名额被占用
超过了截止时间。

## Pass 限值

调用下的 `pass` 规定运行在什么取值下算通过：

```yaml
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      pass:
        p99: 200ms
        errors: 1%
```

所有键都在 [`examples/pass.yaml`](../../examples/pass.yaml) 中，搜索的限值在
[`examples/breakpoint.yaml`](../../examples/breakpoint.yaml) 中。

| 键 | 限值 |
|---|---|
| `p50`、`p90`、`p95`、`p99`、`p999` | 延迟不超过限值：带单位的时长（`200ms`），整数微秒，大于零，小于该调用的 `timeout`，且小于一小时。旁边必须有 `errors`：否则立即拒绝调用的目标也能通过延迟限值。`p999` 需要 1000 个被测量的调用：`rps` ×（`duration` − `warmup`） |
| `errors` | 失败调用的占比不超过限值：带 `%` 的百分数（`1%`、`0.5%`），小数点后最多三位，不超过 `100%`。不带 `%` 的数字不会被猜测。`errors: 100%` 会关掉失败检查，这时延迟限值就毫无价值 |
| `breakpoint.pass.held_rps` | 搜索必须至少守住这么多每秒调用。整数，且是搜索计划中的某一级。调用下的 `pass` 不能与 `load.breakpoint` 同时出现 |

`pass` 写错的配置会在启动前报错（退出码 1），而不是不做检查就运行。空的 `pass: {}` 同样报错。

**每个限值都有一个结论**：`pass`、`fail` 或 `unknown`。报告会输出 `Thresholds:` 块，给出最差的结论，
并为每个限值一行；JSON 中是 `thresholds` 字段。

| 结果 | 退出码 |
|---|---|
| 所有限值都是 `pass` | `0` |
| 已结束的运行没达到某个限值（`fail`） | `4`，`outcome` 仍为 `complete` |
| 没有 `fail`，但某个限值无法判定（`unknown`） | `2`，`invalid_reasons` 含 `thresholds_undecided` |
| 运行无效或被停止 | `2` 或 `3`，与没有限值时相同：限值不改变它们 |

未达标比未判定更重：同时有 `fail` 和 `unknown` 时，退出码为 `4`。

当数字不可信或缺失时，限值为 `unknown`；原因来自封闭列表：`incomplete`（运行被停止）、`invalid`
（运行无效）、`few_observations`（百分位未被测量）、`run_limit`（搜索触及运行自身的上限）、
`generator`（压测机来晚了）、`stream`（调用在等空闲的 stream）、`client_errors`（起决定作用的是
客户端自己的失败，而不是目标的）、`nothing_sent`（一个调用也没发出）、`censored`（尾部有在超时内没有
应答的调用：该值只是下界，且不高于限值）。

- 延迟按整数微秒比较，与报告输出一致。高于限值的下界算 `fail`：真实值还要更大。
- 占比统计所有失败的调用，以及因没有连接而未发出的调用，除以所有被统计的调用。对 `fail` 只计由目标
  决定的失败：被客户端拒绝的请求、客户端错误和错误的响应都说明不了目标。
- 限值那一行绝不与其结论矛盾：如果四舍五入到三位有效数字的值会落在所输出限值的错误一侧，就输出精确值。

## 访问服务

```yaml
app:
  target:
    ip: 10.0.3.17
    port: 443
  ca: certs/ca.pem            # 路径相对于配置文件
  cert: certs/client.pem
  key: certs/client.key
  server_name: payments.internal
  metadata:
    authorization: Bearer ${PAYMENTS_TOKEN}
    x-api-key: ${PAYMENTS_KEY}
```

**机密。** `${NAME}` 由环境变量填充，也可以出现在值的内部：`Bearer ${TOKEN}`。变量未设置或设置为
空都是启动前的错误，并会指出变量名。否则 `Bearer ` 会不带 token 发出，运行会显示 100% 的失败并
归咎于目标。要原样写出 `${`，把美元符号写两次：`$${`。单个 `$` 保持原样。替换只在 `app.metadata`
中生效：在 `data`、`target` 和其他字段中，`${NAME}` 按原样发出。请求头没有对应的命令行参数，因此
token 不会出现在 `ps` 或 shell 历史中。工具不会在任何地方输出请求头的值：报告中、运行期间、错误
信息中都不会。

**请求头。** 名称转为小写，HTTP/2 本来也是这样传输的。以下是启动前的配置错误：以 `grpc-` 开头的
键（gRPC 保留）、`:path` 之类的伪请求头、以 `-bin` 结尾的键（暂不支持二进制请求头）、`content-type` 和 `te`（由传输层自己写入）、
`connection`、`keep-alive`、`proxy-connection`、`transfer-encoding`、`upgrade`（HTTP/2 中没有这些）、
超出可打印 ASCII 的值。`user-agent` 是允许的：目标只会看到一个请求头，即你的值，后面跟着
` grpc-go/<版本>`。启动前的方法检查也会带上同样的请求头和证书。如果设置了请求头而目标对检查返回
`Unauthenticated`，运行不会开始：“target rejected credentials”。`PermissionDenied` 不会阻止运行：
token 已被接受，可能可以用于调用，只是不允许访问 reflection；这些方法会被标记为未检查。没有请求头时
返回 `Unauthenticated` 也不会阻止运行，并有提示：“target requires credentials; app.metadata is not
set”。

**证书。** `ca` 替换系统根证书，而不是追加。对目标证书的校验无法以任何方式关闭。`server_name`
只改变用来校验证书并写入 SNI 的名称；`:authority` 仍是 `target` 中的地址。不支持带密码的私钥：
请事先解密。

**连接。** 默认压测机对一个地址保持一条连接。如果 DNS 返回多个地址，或目标位于 L4 负载均衡器
之后，只有一个后端承受负载，报告不会显示这一点。`app.connections: N` 打开 N 条连接。
DNS 解析为 M 个地址的名字在启动前解析一次，第 i 条连接使用第 i mod M 个地址：三条连接对两个
地址是两条加一条。不为目标提供服务的地址（`localhost` 解析到 `::1`，而服务只监听 `127.0.0.1`）会
中止启动并在错误中点名：请改用它的 IP。报告中的块按连接给出地址、调用数、失败比例和 p99，名字
背后的宕机或缓慢的后端不会被平均掉。调用不会重试。
服务下发的 service config 会被忽略：其中的重试和负载均衡策略不会生效。应用这些策略的生产客户端
会看到不同的类别和 p99。

**启动前的错误。** 运行前能知道的一切都意味着退出码 1，且不会向目标发出任何调用：文件不存在或
不是 PEM、证书与私钥不匹配、目标证书过期或不包含该地址（提示：`server_name`）、目标在握手后立即
关闭连接（它不接受客户端证书）、开启了 TLS 而目标没有，或者相反。

## 请求体

```yaml
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      data:
        wallet_id: "w-123"
        currency: USD                         # 枚举——按名称
        filter:
          since: "2026-01-01T00:00:00Z"       # google.protobuf.Timestamp
```

`data` 是按消息结构书写的普通 YAML。工具通过 server reflection 从服务获取结构；不需要 `.proto`。
请求体在启动前构建一次，其中的任何错误——未知字段、类型错误、不存在的方法——都会立即暴露，并给出
方法名和字段名，早于第一个请求。没有 `data` 时发出空消息，这样的方法不需要 reflection。

**每个方法一个请求体。** 一个方法的每次调用都使用同一份 `data`。对于带幂等键的写操作，这意味着
第一次调用创建记录，之后每次调用都走重复请求的路径：目标返回它已保存的应答。测到的是这条路径，
而不是创建记录。

取值规则是 protobuf 的标准 JSON 规则（`protojson`）：

- 字段名可以用 `.proto` 中的写法（`wallet_id`）或其 JSON 形式（`walletId`）；
- 枚举按名称；`Timestamp`、`Duration` 用各自格式的字符串；
- `int64` 和 `uint64` 精确传递，超过 2^53 也一样（snowflake ID、以最小单位计的金额）：数字从不
  经过浮点数；
- 带前导零的数字（`0123`、`007`）是配置错误：YAML 会把它当作八进制。需要前导零——写成字符串
  `"0123"`；
- **`bytes` 用 base64 字符串表示。** `signature: abcd` 不是 `abcd` 这四个字节，而是另外三个
  字节：`abcd` 本身就是合法的 base64，不会报错。四个字节 `abcd` 应写作 `signature: YWJjZA==`。

**方法检查。** 启动前会对照服务检查配置中的每个方法：名称拼写错误或服务没有的方法是第一个请求
之前的错误，而不是一次所有调用都以 `Unimplemented` 失败的运行。如果服务关闭了 reflection，带
`data` 的方法无法启动——错误会明确说明。不带 `data` 的方法没有 reflection 也能运行：没有东西可以
用来检查它，并会有警告说明——运行前在 stderr 输出，并在报告中再写一行。警告会说明为什么无法检查：
reflection 已关闭、拒绝了请求，或根本没有应答。

## 每次调用不同的请求：dataset

```yaml
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      dataset: wallets.jsonl
```

```
{"wallet_id": "w-1", "currency": "USD"}
{"wallet_id": "w-2", "currency": "EUR"}
```

`dataset` 是请求体文件，每行一个 JSON，形式与 `data` 相同。相对路径从配置文件所在目录算起。第一个
请求用第一行，第二个请求用第二行，用完最后一行后从头再来。空行会被跳过。启动前会读取文件，并把
每一行对照方法的消息进行检查——按 `data` 的规则并通过 reflection；错误会指出行号，如
`wallets.jsonl:3`，但绝不会打印那一行的内容。没有任何请求的文件是错误。

报告在方法那一行下面说明文件用了多少：
`data: 3 of 3 requests from wallets.jsonl, each used up to 5 times`。这是请求交给发送方的次数，而不是
目标看到了什么：在文件中出现两次的请求会发出两次，没有到达目标的调用也会被计入。在崩溃点搜索中，
每一级都从上一级结束的地方继续，这一行统计整个搜索。

`dataset_order: random` 按随机顺序给出请求：每遍文件都是一次新的排列，其中每个请求恰好发出一次，
所以上面的数字不变。这一行此时以 `, random order, seed 42` 结尾。要重复这次运行，把这个数填进
`dataset_seed`（0 到 9007199254740991 的整数）：相同的 seed、文件和请求数量给出相同的顺序。省略时
seed 在启动时选取，每个调用各有一个。

`dataset_reuse: false` 让每条记录最多发送一次，按调用并在整个进程范围内：预热、搜索的每一级和每条
连接都从同一个计数中取记录（两个调用用同一个文件——两个计数）。这一行此时是：
`data: 3 of 3 requests from wallets.jsonl, each at most once`。文件无法覆盖的运行会在发送前被拒绝，
并给出两个数字。搜索需要一条记录而文件中已没有时，像按 Ctrl+C 一样停止（退出码 3）：正在进行的
运行不做判定，报告会指出文件和速率。只限一个进程：记录不会在机器之间共享，停止会让记录保持未发送。

## 命令行参数

| 参数 | 作用 |
|---|---|
| `-c` | 配置文件路径 |
| `-output` | stdout 上的报告格式：`text`（默认）或供脚本和 CI 使用的 `json` |
| `-json-file` | 新文件的路径：JSON 报告（与 `-output json` 输出的对象相同）写入该文件，stdout 仍按 `-output` 的设定。文件已存在或目录不存在，则在开始前退出码 1。没有报告的运行不留下文件。写入失败为退出码 1——未发布 |
| `-connect-timeout` | 服务接受了连接却不应答时等待多久。默认 `10s`。连接被拒绝和地址错误不会等待 |
| `-max-in-flight` | 等待应答的请求数上限。默认 `5000` |
| `-plain` | 不用实时屏幕：每秒一行进度输出到 stderr，与没有终端时相同，并且没有首次运行的对话。报告和退出码不变 |
| `-fake` | 用内置的桩代替配置中的服务施压——无需服务即可查看工具。报告会标记为 `fake target` |
| `-fake-delay`、`-fake-jitter`、`-fake-fail-ratio` | 桩的行为。只能与 `-fake` 一起使用 |
| `-version` | 输出版本并退出。从源码构建时输出提交哈希 |

## 停止

全屏界面中按 `q`，没有全屏界面时按 Ctrl+C：

- 第一次——不再发出新请求，在途请求一直等到各自超时，并照常计入报告；
- 第二次——在途请求被截断，单独计数（`aborted`）：这不是服务的失败，而是时间的下界；输出报告；
- 第三次——立即退出，不输出报告。

在全屏界面中，顶部一行会逐步提示这些步骤。

SIGTERM（`docker stop`、Kubernetes、被取消的 CI 任务）会立即截断在途请求并输出报告，跳过温和
停止：编排器会在几秒后杀死进程，报告必须来得及输出。如果截断已在进行中，SIGTERM 不会打断它。

停止的运行在报告中标记为未完成（退出码 3）：数字是诚实的，但涵盖的内容少于计划。

## 在哪里运行

在 Linux 上、紧挨着目标：同一网络、同一集群、CI runner。压测机与目标之间的一切都会计入延迟，看起来
像目标的耗时：在我们的测试台上以 1000 RPS 压测，经 Docker Desktop 端口转发 p99 为 38 ms，在 Docker
网络内为 2 ms。

- 精确的调用调度只在 Linux 上提供。在 CPU 配额低于两个核的容器里，偶尔会出现最长为配额周期
  （通常 100 ms）的启动延迟；报告会显示出来。
- 在 L4 负载均衡器后面只有一个后端承受负载：一条连接——一个后端。见
  [阅读报告](README.md#阅读报告)。
- 在 Windows 上时钟步长约 0.5 ms，针对快速服务的运行会被判为无效（`clock_step`，退出码 2）。

## JSON 字段

契约的规则——结构版本、单位、用 `null` 而不是 `0`、未知字段——见
[README](README.md#供脚本和-ci-使用的-json)。这里列出 `schema_version` 1 的每个字段。带 `?` 的类型
可能为 `null`。预热计数不包含在任何其他计数中；`sent` 及其所有组成部分只包括被测量的调用。第一列是
字段的完整路径。

**百分位**——一个对象；`null`——没有任何观测。`<percentile>` 指下文中类型为 “percentile?” 的任一字段。

| 字段 | 类型 | 含义 |
|---|---|---|
| `<percentile>.us` | int | 数值，整数微秒 |
| `<percentile>.lower_bound` | bool | `true`——下界，而不是数值（[README](README.md#阅读报告)） |

### 运行

| 字段 | 类型 | 含义 |
|---|---|---|
| `schema_version` | int | 结构版本，目前为 `1` |
| `mode` | string | 普通运行为 `run`，崩溃点搜索为 `breakpoint` |
| `leettest_version` | string | 工具版本；从源码构建时为提交哈希 |
| `target` | string | 配置中的目标地址，使用 `-fake` 时为 `fake target` |
| `name` | string | 运行的名称：配置中的 `name`，否则为服务名，否则为配置文件名；始终是字符串——未发布 |
| `methods_source` | string? | 方法的来源：`reflection`、`proto`（`app.proto` 的文件）或 `protoset`；使用 `-fake` 时为 `null`——未发布 |
| `outcome` | string | `complete`、`invalid`、`incomplete`——对应退出码 `0`、`2`、`3`；已结束但没达到 `pass` 限值的运行是 `complete`，退出码 `4`；使用 `-json-file` 且写入失败时退出码为 `1`，`outcome` 仍是该次运行的 |
| `invalid_reasons` | []string | 运行无效的原因：`in_flight_cap`、`nothing_measured`、`clock_step`、`thresholds_undecided`（某个 `pass` 限值无法判定，且原因不是运行被停止或已经无效）。为空——有效 |
| `tail_wait_cause` | string? | 决定尾部的客户端侧等待：`generator`、`stream`、`connection`；`null`——没有结论 |
| `clock_step_ns` | int | 主机时钟步长，纳秒：每个延迟和等待都有 ± 这个值的误差 |
| `started_at` | string | 计划开始时刻，RFC 3339 UTC，包括预热。所有 `_us` 和 `_s` 都从它算起 |
| `duration_us` | int | 运行实际持续时间，包括预热 |
| `planned_us` | int | 计划持续时间 |
| `warmup_us` | int | 预热时长 |
| `settings` | object | 运行启动时的设置，按配置而非测量值——未发布 |
| `settings.max_in_flight` | int；搜索：`int?` | 在途请求上限：`-max-in-flight` 的值，否则为默认值。搜索：未设该参数时为 `null`（每次运行取其能运行的最小上限）；在其各次运行中为该次运行的上限——未发布 |
| `settings.connections` | int | 按配置的 `app.connections`，默认 1；`connections.open` 是实际打开的数量——未发布 |
| `settings.warmup_us` | int | 按配置的预热。搜索：`settle` 时长——未发布 |
| `settings.compression` | string? | 按配置的 `app.compression`：`"gzip"`，`null` 表示省略——未发布 |
| `sent` | int | 被测量的调用，不含 `not_sent`。包括 `unreachable` 和 `aborted` |
| `failed` | int | `sent` 中所有未成功的调用，不含 `aborted` |
| `aborted` | int | 被我们自己的停止截断 |
| `not_sent` | int | 发送前超时已耗尽：既不在 `sent` 中也不在 `failed` 中 |
| `not_sent_generator`、`not_sent_stream`、`not_sent_connection` | int | 按原因拆分的 `not_sent`：压测机迟到、没有空闲的流、没有就绪的连接。三者之和等于 `not_sent` |
| `warmup_sent`、`warmup_failed`、`warmup_not_sent` | int | 预热期间计划的调用的同类数值 |
| `cap_hit` | object? | 触及了 `-max-in-flight` 上限；`null`——没有触及 |
| `cap_hit.at_us` | int | 何时 |
| `cap_hit.unsent` | int | 被上限拒绝的调用 |
| `cap_hit.over_deadline` | int | 当时在途、占用名额超过截止时间的调用 |
| `start_lag` | object | 调用相对计划开始得晚了多少 |
| `start_lag.p99` | percentile? | 延迟的 p99 |
| `start_lag.max` | percentile? | 最大值，始终精确；没有调用时为 `null` |
| `connections` | object? | 连接；发送方不报告时为 `null` |
| `connections.open` | int | 同时承载调用的连接数 |
| `connections.reconnects` | int | 第一次之后成功的握手次数 |
| `connections.first_limit`、`connections.last_limit` | int? | 第一次和最后一次握手时的 `MAX_CONCURRENT_STREAMS`；`null`——未声明（`0`——声明为零）。多条连接时始终为 `null`：每条连接有自己的上限 |
| `connections.limit_changes` | int | 声明的上限与上一次不同的握手次数 |
| `connections.resolved` | []string? | 目标对应的地址，按解析器的顺序：全部，即使连接更少。IP 地址就是它自己；一条连接时为 `null` |
| `connections.in_flight_limit` | int? | 目标允许同时在途的调用数：单条连接的上限，或所有连接上限之和；只要有连接未声明上限就为 `null` |
| `connections.per_connection` | []object? | 每条连接一个对象，编号与报告中的块一致；一条连接时为 `null` |
| `connections.per_connection[].address` | string | 连接的地址 |
| `connections.per_connection[].calls` | int | 分配给该连接的调用，已发送与否，不含预热 |
| `connections.per_connection[].failed` | int | 该连接的失败：运行的 `failed` 规则，加上因连接未就绪而未发出的调用 |
| `connections.per_connection[].stream_waited`、`connections.per_connection[].not_sent_stream` | int | 该连接上等待空闲流的调用：等待后发出的，和等待中过期的 |
| `connections.per_connection[].p99` | percentile? | 该连接调用的 p99；没有带延迟的调用时为 `null` |
| `connections.per_connection[].first_limit`、`connections.per_connection[].last_limit` | int? | 该连接第一次和最后一次握手时的流上限；`null`——未声明 |
| `connections.per_connection[].limit_changes` | int | 该连接声明的上限与上一次不同的握手次数 |
| `client_waits` | object | 在客户端侧等待超过阈值的调用，按原因（[README](README.md#阅读报告)） |
| `client_waits.generator_calls`、`client_waits.stream_calls`、`client_waits.connection_calls` | int | 所有此类调用，无论是否发出。一个已发出的调用可能同时计入多个原因 |
| `client_waits.generator_tail_calls`、`client_waits.stream_tail_calls`、`client_waits.connection_tail_calls` | int | 只统计 p99 尾部和未发出的调用：由它们决定 `tail_wait_cause` |
| `generator_cpu` | object? | 运行占用了 LeetTest 自身可用 CPU 的多少，按秒给出；无法读取进程的 CPU 时间时为 `null`。有繁忙的秒时，报告会打印一行 — 未发布 |
| `generator_cpu.available_cores` | float | 进程可使用的 CPU：`GOMAXPROCS`，或 cgroup 配额（若更小） — 未发布 |
| `generator_cpu.share` | float | CPU 时间除以墙钟时间和 `available_cores`，四舍五入到 2 位小数，范围是预热之后的计划秒数。可能超过 1：系统调用中的时间发生在 P 之外 — 未发布 |
| `generator_cpu.busy_seconds` | int | 其中 `share` 不低于 0.90 的秒数（按四舍五入后的值比较）。这些秒加起来不足一秒时为 `0` — 未发布 |
| `generator_cpu.first_busy_s` | int? | 第一个这样的秒，从运行开始算起，预热计入；没有则为 `null` — 未发布 |
| `generator_cpu.seconds` | []float | 从运行开始算起每一秒的占比，预热和超时尾部都包含：条目数与最长的 `methods[].seconds` 相同。没有任何读数覆盖的秒为 `null`，与前一秒合并计算的不足半秒的最后一秒也为 `null` — 未发布 |
| `methods` | []object | 方法，见下文 |
| `unchecked` | []object | 无法通过 reflection 检查的方法 |
| `unchecked[].method` | string | 方法 |
| `unchecked[].reason` | string | `reflection_off` 或 `reflection_failed` |
| `unchecked[].error` | string | 给人看的文本，随 grpc-go 变化 |
| `thresholds` | []object? | `pass` 限值的结论，每个限值一个对象，键都相同；不属于该限值的键为 `null`。`null`——没有设置限值（不是空列表，空列表会被读成“全部通过”）。搜索把它写在顶层——未发布 |
| `thresholds[].method` | string? | 带前导斜杠的方法，与报告中的写法一致；`null`——搜索的限值——未发布 |
| `thresholds[].key` | string | `p50`、`p90`、`p95`、`p99`、`p999`、`errors`、`held_rps`——未发布 |
| `thresholds[].verdict` | string | `pass`、`fail`、`unknown`——未发布 |
| `thresholds[].reason` | string? | 为什么是 `unknown`，取自列表：`incomplete`、`invalid`、`few_observations`、`run_limit`、`generator`、`stream`、`client_errors`、`nothing_sent`、`censored`；`pass` 和 `fail` 时为 `null`——未发布 |
| `thresholds[].limit_us` | int? | 延迟限值，整数微秒；其他键为 `null`——未发布 |
| `thresholds[].value` | percentile? | 用来比较的被测百分位；`null`——未被测量，或该键与延迟无关——未发布 |
| `thresholds[].limit_percent` | string? | `errors` 限值，按配置中的写法，如 `"0.5%"`；其他键为 `null`——未发布 |
| `thresholds[].bad`、`thresholds[].bad_client`、`thresholds[].total` | int? | `errors` 的占比：失败的调用（含因没有连接而未发出的）、其中不由目标决定的、所有被统计的调用。其他键以及一个调用也没被统计时为 `null`——未发布 |
| `thresholds[].limit_rps`、`thresholds[].held_rps` | int? | 搜索的限值和它守住的值；其他键为 `null`——未发布 |
| `notes` | []string | 报告中的提示，给人看的 ASCII 文本；不要解析 |

### 方法：`methods[]`

`<category>` 指在 JSON 中带有应答时间对象的四个类别之一：`request_error`、`overload`、`failure`、
`bad_response`（[类别](README.md#阅读报告)）。其余类别只是计数。

| 字段 | 类型 | 含义 |
|---|---|---|
| `methods[].method` | string | 方法全名 |
| `methods[].invalid_reason` | string? | 该方法没有测到任何与负载相关的内容：`request_error`、`client_error`、`bad_response`、`mixed`；`null`——测到了 |
| `methods[].sent`、`methods[].failed`、`methods[].aborted`、`methods[].not_sent`、`methods[].not_sent_generator`、`methods[].not_sent_stream`、`methods[].not_sent_connection`、`methods[].warmup_sent`、`methods[].warmup_failed`、`methods[].warmup_not_sent` | int | 该方法在运行同名字段中所占的部分 |
| `methods[].rps` | float? | `sent/s`：已发送调用数除以发送时间；`null`——没有调用 |
| `methods[].timeout_us` | int | 该方法的超时 |
| `methods[].planned_rps_low`、`methods[].planned_rps_high` | int | 整个计划中最低和最高的计划速率 |
| `methods[].planned_duration_us` | int | 该方法计划运行多久，按配置——未发布 |
| `methods[].request_bytes` | object? | 已发出的被测调用的请求体平均大小；无压缩时为 `null`——未发布 |
| `methods[].request_bytes.raw`, `methods[].request_bytes.wire` | float | 压缩前与线上的字节数（不含 5 字节帧头）——未发布 |
| `methods[].latency` | object | 成功调用的延迟，超时和被截断的调用作为下界 |
| `methods[].latency.min`、`methods[].latency.p50`、`methods[].latency.p90`、`methods[].latency.p95`、`methods[].latency.p99`、`methods[].latency.max` | percentile? | 最小值、百分位、最大值 |
| `methods[].latency.p999` | percentile? | 按同一规则得出的 p99.9；方法的观测数（`observations`）少于 1000 时为 null，此时 p99.9 只会是最大值——未发布 |
| `methods[].p99_without_client_waits` | percentile? | 同一批调用去掉客户端侧等待后的 p99；可能略微偏低 |
| `methods[].observations` | int | `latency` 百分位背后的观测数 |
| `methods[].censored` | int | 其中只知道下界的数量 |
| `methods[].invalid_latencies` | int | 被判为不可能而丢弃的（负延迟）——说明代码有缺陷，而不是目标 |
| `methods[].timed_out` | int | 已发出且在超时内没有得到应答 |
| `methods[].timed_out_after_wait` | int | `timed_out` 中发出时剩余时间不到超时一半的调用 |
| `methods[].cut_off` | int | 已发出但没有得到状态 |
| `methods[].unreachable` | int | 从未到达目标 |
| `methods[].unclassified` | int | 发送方没有给出类别——发送方的缺陷，而不是对目标的观测 |
| `methods[].outside_timeline` | int | 未计入 `seconds`：调用的某个时刻落在时间线之外 |
| `methods[].<category>` | object | 该类别的应答 |
| `methods[].<category>.count` | int | 其中的调用数 |
| `methods[].<category>.p50`、`methods[].<category>.p90`、`methods[].<category>.p95`、`methods[].<category>.p99`、`methods[].<category>.max` | percentile? | 其应答时间 |
| `methods[].client_error` | int | 客户端拒绝发送 |
| `methods[].failure_codes` | []object | 按 gRPC 代码统计的失败调用 |
| `methods[].failure_codes[].code` | string | 规范的代码名（`UNAVAILABLE`）；列表可能扩充 |
| `methods[].failure_codes[].count` | int | 数量 |
| `methods[].failure_codes[].from_target` | bool | `true`——拒绝来自目标：经网络传回的状态或 REFUSED_STREAM，`false`——代码由客户端设置 |
| `methods[].silent_from_s` | int? | 目标从哪一秒起不再应答任何已发出的调用；`null`——没有沉默 |
| `methods[].silent_sent_rps` | int? | 在那之前一秒发出的调用数（如果沉默从第一秒开始，则为第一秒） |
| `methods[].silent_planned_rps_low`、`methods[].silent_planned_rps_high` | int? | 那一秒各阶段的计划速率；那一秒没有阶段在运行时也为 `null` |
| `methods[].last_answer_at_us` | int? | 目标应答过的最后一个调用的发出时刻；`null`——从未应答 |
| `methods[].dataset` | object? | 该调用的请求文件；`null`——没有 |
| `methods[].dataset.file` | string | 配置里写的路径 |
| `methods[].dataset.records` | int | 文件中的请求数 |
| `methods[].dataset.used` | int | 其中至少发出过一次的数量 |
| `methods[].dataset.used_max` | int | 被用得最多的那条发出了几次；在搜索中——从第一步算到本次运行结束 |
| `methods[].dataset.order` | string | `file` 或 `random`：请求发出的顺序——未发布 |
| `methods[].dataset.seed` | int? | 随机顺序的 seed，0 到 9007199254740991；文件顺序为 `null`——未发布 |
| `methods[].dataset.reuse` | bool | `false`——该调用每条记录最多发送一次（`dataset_reuse: false`）；否则为 `true`——未发布 |
| `methods[].seconds` | []object | 按秒的时间线，见下文 |

### 秒：`methods[].seconds[]`

时间线覆盖整个运行，包括预热，直到最后一个发生了事情的秒。与总数不同，它保留每一个调用。核对方式：
`Σ begun` + `outside_timeline` 就是该方法的全部调用。

| 字段 | 类型 | 含义 |
|---|---|---|
| `methods[].seconds[].warmup` | bool | 该秒处于预热期 |
| `methods[].seconds[].begun` | int | 在该秒开始的调用 |
| `methods[].seconds[].succeeded`、`methods[].seconds[].overload`、`methods[].seconds[].failure`、`methods[].seconds[].client_error`、`methods[].seconds[].bad_response`、`methods[].seconds[].request_error`、`methods[].seconds[].timed_out`、`methods[].seconds[].unreachable`、`methods[].seconds[].cut_off`、`methods[].seconds[].aborted`、`methods[].seconds[].unclassified` | int | 在该秒结束的调用，按类别 |
| `methods[].seconds[].not_sent_generator`、`methods[].seconds[].not_sent_stream`、`methods[].seconds[].not_sent_connection` | int | 在该秒结束的发送前超时，按原因 |
| `methods[].seconds[].in_flight` | int | 该秒结束时的在途调用 |
| `methods[].seconds[].lag_calls`、`methods[].seconds[].lag_sum_us`、`methods[].seconds[].lag_max_us` | int | 对计划在该秒的调用：数量、启动延迟之和与最大值 |
| `methods[].seconds[].observed_calls` | int | 其中成功的调用 |
| `methods[].seconds[].observed_lag_sum_us`、`methods[].seconds[].transport_wait_sum_us`、`methods[].seconds[].service_time_sum_us` | int | 对成功的调用：启动延迟之和、发送前等待（连接和流）之和、从发送到应答的时间之和。三者相加即为它们的延迟 |

### 崩溃点搜索：`mode: "breakpoint"`

搜索输出的是另一种对象：`schema_version`、`leettest_version`、`target`、`started_at` 与上面的运行相同，
普通运行的其他字段不在顶层。每次运行连同它自己的完整报告都在 `breakpoint.runs` 中（[搜索](README.md#崩溃点搜索)）。

| 字段 | 类型 | 含义 |
|---|---|---|
| `method` | string | 搜索所施压的方法 |
| `breakpoint` | object | 搜索结果 |
| `breakpoint.outcome` | string | 封闭列表：`broke`、`broke_at_first`、`held_all`、`run_limit`、`stopped`、`invalid` |
| `breakpoint.held_rps` | int? | 目标撑住的最高速率；`null` — 没有撑住的 |
| `breakpoint.broke_rps` | int? | 目标崩溃或运行上限使搜索停下的最低速率；`null` — 未崩溃 |
| `breakpoint.why` | string? | 结束搜索的那次运行的原因，来自封闭列表：`errors`、`p99_limit`、`p99_vs_base`、`connection`、`no_recovery`、`generator`、`in_flight_cap`、`stream_limit`、`stream_wait`、`clock_step`、`request_errors`；`null` — 没有原因 |
| `breakpoint.stop_reason` | string? | 谁结束了搜索：`dataset_exhausted`（`dataset_reuse: false` 的文件用完了）、`requested`（用户停止）；`null`——搜索自行结束——未发布 |
| `breakpoint.notes` | []string | 搜索的说明，面向人的 ASCII 文本；不要解析 |
| `breakpoint.runs` | []object | 按顺序列出所有运行 |
| `breakpoint.runs[].kind` | string | `step`、`repeat` 或 `probe` |
| `breakpoint.runs[].planned_rps` | int | 该次运行的计划速率 |
| `breakpoint.runs[].sent_rps` | int | 调用实际发出的速率：只有实际发出的才算撑住 |
| `breakpoint.runs[].broken` | bool | `true` — 该次运行使目标崩溃 |
| `breakpoint.runs[].recovered` | bool? | 对 `probe`：目标回到基线 p99 的 1.5 倍以内；`step` 和 `repeat` 为 `null` |
| `breakpoint.runs[].why` | string? | 该次运行的原因，与 `breakpoint.why` 相同的封闭列表；`null` — 没有原因 |
| `breakpoint.runs[].report` | object | 这次运行的完整报告，与普通运行的 JSON 是同一种对象（字段见上） |
