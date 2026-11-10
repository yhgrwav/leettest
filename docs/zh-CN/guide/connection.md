# 连接

[← 返回文档](../README.md)

> 译自 [docs/ru/guide/connection.md](../../ru/guide/connection.md)，对应 feb2d1d，2026-11-10。如有出入，以俄文版为准。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../../en/guide/connection.md)。

LeetTest 如何找到并接入你的服务：地址、加密、带令牌的请求头和方法的 schema。配置、证书、环境变量和方法
的 schema 都在启动前检查：出错时退出码为 1，且不会向服务发出任何请求。

## 最小配置

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

load:
  calls:
    - method: payments.v1.PaymentService/GetPayment
      rps: 100
      duration: 1m
```

- `target`——服务的地址：`ip`（名称或 IP）和 `port`。
- `tls`——未指定：加密开启。`tls: false`——不加密连接。
- `ca`——PEM 文件（证书的文本格式，以 `-----BEGIN` 开头的行起头），其中的证书用来代替系统证书验证服务。
  需要 TLS。
- `cert` 和 `key`——PEM 格式的客户端证书及其密钥，用于 mTLS 服务（双向 TLS：服务也验证客户端的证书）。
  只能一起使用。
- `server_name`——当服务的证书不包含 `target` 中的地址时，用来验证证书的名称。
- `metadata`——每个请求的请求头：`authorization`、`x-api-key` 等。

每个字段见[参考](../../en/reference.md#config)（英文）。

## 令牌和机密

`${NAME}` 会从环境变量中替换，也包括值的内部：`Bearer ${TOKEN}`。

```sh
export PAYMENTS_TOKEN=令牌的值
export PAYMENTS_KEY=密钥的值
leettest -c leettest.yaml
```

- 变量未设置或为空——启动前报错并给出变量名。否则会发出没有令牌的 `Bearer `，运行就会把 100% 的失败
  算作服务的过错。
- 替换只在 `metadata` 中生效。请求头没有命令行标志，所以令牌既不会出现在 `ps` 中，也不会出现在 shell
  历史里。
- 请求头的值不会出现在报告里，不会出现在运行过程的输出里，也不会出现在错误信息里。
- 要字面写出 `${`，把美元符号写两遍：`$${`。
- 请求头名称会转为小写。以下属于配置错误：带 `grpc-` 的键、形如 `:path` 的伪请求头、带 `-bin` 的键、
  `content-type`、`te`、`connection`、`keep-alive`、`proxy-connection`、`transfer-encoding`、`upgrade`，
  以及超出可打印 ASCII 的值。

## 报告里会出现什么

- 没有关闭服务证书验证的标志。`ca` 取代系统根证书，而不是追加到它们之上。`server_name` 只改变与证书
  核对的名称；不支持用密码加密的密钥——请事先解密。
- 服务在检查时回了 `Unauthenticated`，而请求头已设置：运行不会开始，错误为“target rejected
  credentials”。`PermissionDenied` 不会中止运行：令牌已被接受，但方法会被标为未检查。没有设置请求头时的
  `Unauthenticated` 也不会中止，说明中写的是：“target requires credentials; app.metadata is not set”。
- 多条连接：默认一条连接只落到一个服务实例上，而在 L4 负载均衡器（Kubernetes ClusterIP、NLB）之后，或
  DNS 返回多个地址时，只有它承受负载，报告不会显示这一点。`app.connections: N` 会打开 N 条连接，报告会
  打印 `Connections:` 块：每条连接的地址、请求数、失败占比和 p99。详见[参考](../../en/reference.md#access-to-the-service)（英文）。

## 方法的 schema：reflection 还是 .proto

默认情况下 LeetTest 通过 gRPC server reflection 从服务获取 schema（服务自己提供方法和消息的列表），不需要
`.proto`。如果 reflection 被关闭，带 `data` 的方法无法启动：错误信息会直接说明这一点。两种办法：

- 去掉 `data`：会发送空消息，该方法没有被检查，并打印一条警告；
- 以文件提供 schema（自 v0.3 起）：`app.proto`（文件和 `import_paths`，即查找其导入的位置）或
  `app.protoset`（用 `protoc --include_imports` 生成的描述符集）——
  [`examples/proto.yaml`](../../../examples/proto.yaml)、
  [`examples/protoset.yaml`](../../../examples/protoset.yaml)。

## 常见错误

- **TLS 已开启，而服务不加密**（或相反）：启动前报错。设置或去掉 `tls: false`。
- **服务的证书不包含该地址。** 错误信息会提示 `server_name`。
- **服务在握手后立即关闭了连接。** 它没有接受客户端证书：检查 `cert` 和 `key`。
- **证书文件找不到或不是 PEM、证书与密钥不匹配、证书已过期。** 这些错误都在第一个请求之前出现。
- **地址是 `localhost`，而服务只监听 `127.0.0.1`。** 名称可能解析为 `::1`；请指定 IP。
- **令牌以明文值写在配置里。** 报告中的配置哈希会显示文件的任何改动，而令牌本身会进入仓库：请写成
  `${NAME}`。
