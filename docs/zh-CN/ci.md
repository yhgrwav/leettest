# CI 中的 LeetTest

[← 返回文档](README.md)

> 译自 [docs/ru/ci.md](../ru/ci.md)，对应 dc7fd95，2026-11-10。如有出入，以俄文版为准。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../en/ci.md)。

本页说明如何在服务每次变更时运行负载测试，并在服务不再满足你设定的限值时让构建失败。示例是一个
GitHub Actions 工作流；任何能执行 shell 命令的 CI 做的都是同样四件事：

1. 安装 LeetTest。
2. 启动被测服务。
3. 运行负载。退出码就是结论。
4. 保存 JSON 报告。

## 配置

服务仓库中的 `load.yaml`。`pass` 中的限值让退出码成为结论：没有它们，一次跑到结束的运行不论服务如何
响应都会返回 `0`（[`pass` 限值](../en/report.md#pass-limits-a-run-as-a-check)，英文）。`app.target` 是
任务启动的服务；在这个文件里它是我们仓库中的参考测试台，也就是我们自己 CI 的目标。

```yaml
# 用于 CI 的负载测试。app.target 是任务启动的服务；这里是参考测试台，
# 在仓库根目录运行：
#   go run ./test/stand/cmd/stand -delay 20ms
#   leettest -c examples/ci.yaml
# pass 限值决定退出码：在测试台上两个限值都达标，所以是 0；未达标的限值是 4。

app:
  target:
    ip: 127.0.0.1
    port: 50051
  tls: false

load:
  warmup: 2s
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 100
      duration: 10s
      pass:
        p99: 200ms
        errors: 1%
```

## 工作流

`.github/workflows/load-test.yml`：

```yaml
name: Load test

on:
  pull_request:

permissions:
  contents: read

jobs:
  load:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - name: Install LeetTest
        run: |
          curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
      - name: Start the service under test
        run: docker compose up -d --wait
      # example workflow: start
      - name: Run the load
        run: |
          rc=0
          leettest -c load.yaml -json-file report.json || rc=$?
          case "$rc" in
            0) ;;
            2) echo "::error::invalid run: the numbers are not about the service's load"; exit 1 ;;
            3) echo "::error::the run stopped before its plan"; exit 1 ;;
            4) echo "::error::a pass limit was missed"; exit 1 ;;
            *) echo "::error::leettest exited with $rc and no report"; exit 1 ;;
          esac
      - name: Read the report
        if: always() && hashFiles('report.json') != ''
        run: |
          echo "p99: $(jq '.methods[0].latency.p99.us' report.json) us" >> "$GITHUB_STEP_SUMMARY"
          jq -e '.thresholds != null and all(.thresholds[]; .verdict == "pass")' report.json
      - uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        if: always()
        with:
          name: leettest-report
          path: report.json
          if-no-files-found: ignore
      # example workflow: end
```

YAML 中的步骤名称和错误文本是英文：这段文字与英文页面以及我们 CI 的任务逐字一致。

逐步说明：

- **安装。** `install.sh` 把最新版本放进 `~/.local/bin` 并校验其校验和
  （[安装](../en/guide/first-run.md#install)，英文）。不希望构建在脚本之下悄悄变化的人要固定版本：
  在 `sh` 之前设置 `LEETTEST_VERSION=<标签>`，标签取自[发布页面](https://github.com/yhgrwav/leettest/releases)。
- **启动服务。** 把这一步换成你的服务的启动方式，并让 `load.yaml` 中的 `app.target` 指向它。LeetTest 一
  启动负载就开始：在这一步结束前要等到服务能够响应（Docker Compose 用 `--wait` 做到这一点）。
- **负载。** `-json-file report.json` 把 JSON 报告写入文件，文本报告进入任务日志。`case` 把每个退出码
  变成一行错误；任何非零退出码都会让构建失败。文件在启动前创建且绝不覆盖，所以需要一个干净的 runner。
- **读取报告。** `jq` 从 JSON 中取出一个字段：这里是第一个方法的 p99，写入任务摘要。第二行检查已设置
  `pass` 限值且每个都达标：没有限值的配置返回退出码 `0` 并不能说明服务的任何情况。负载失败后只要文件
  存在，这一步照样执行。
- **保存报告。** `if: always()` 让失败的运行也上传报告。没有启动的运行（退出码 `1`）不会留下文件，所以
  用 `if-no-files-found: ignore`。

两行 `# example workflow` 注释标出了我们自己的 CI 按原样运行的部分。

## 构建中的退出码

| 退出码 | 发生了什么 | 构建 |
|---|---|---|
| `0` | 计划已执行完，报告完整；使用 `pass` 时——所有限值都达标 | 通过 |
| `1` | 没有得到要求的报告：运行没有进行或报告没有写出 | 失败；原因在 stderr |
| `2` | 运行无效，或无法判定某个 `pass` 限值 | 失败；这不算通过 |
| `3` | 运行在计划结束前被停止 | 失败；报告只涉及已经发生的部分 |
| `4` | 已完成的运行没有达到某个 `pass` 限值 | 失败；哪个限值见 JSON 的 `thresholds` |
| `130` | 按 Ctrl+C 后无报告地异常退出 | 失败；没有报告 |
| `143` | 收到 SIGTERM 后无报告地异常退出 | 失败；没有报告 |

`2` 既不是通过也不是未达标：这样的报告中的数字说的不是服务所承受的负载，所以不能证明某个限值已达标。
报告会指出原因（JSON 中的 `invalid_reasons`）：生成器处理器不足、主机时钟太粗、某个限值无法判定。要修
的是环境；放宽限值没有用。这些退出码在 1.x 内不会改变：[1.x 内不变的内容](stability.md#退出码)。

## 读取 JSON

报告文字给人看，JSON 和退出码给脚本用。脚本可以依赖[1.x 内不变的内容](stability.md)一页中列出的字段和
取值；所有字段见[参考](../en/reference.md#json-fields)（英文）。构建通常会读取的几个字段：

| 字段 | 内容 |
|---|---|
| `outcome` | `complete`、`invalid` 或 `incomplete`；与退出码 `0`/`4`、`2`、`3` 是同一回事 |
| `thresholds[].verdict` | 每个 `pass` 限值的 `pass`、`fail` 或 `unknown` |
| `invalid_reasons` | 运行为何无效；有效的运行为空 |
| `methods[].latency.p99.us` | 方法的 p99，单位为整数微秒 |

脚本应跳过不认识的字段，遇到枚举的新取值不得崩溃。

## runner 的位置为何重要

负载从 runner 发出，所以 runner 与服务之间的一切都会计入延迟，而处理器不足的 runner 守不住调度：这时
报告会写 `generator_late`，退出码为 `2`。请把任务放在服务旁边，同一网络或同一任务中，并让频率保持在
runner 能发出的范围内。详见[在哪里运行](../en/reference.md#where-to-run)（英文）和
[数字没有涵盖的内容](limitations.md)。

## 示例是经过测试的

两个标记注释之间的工作流部分，就是 [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) 中的
`example workflow` 任务，逐字一致；两者不一致时 [`examples/ci_page_test.go`](../../examples/ci_page_test.go)
会失败。该任务在每个 pull request 上运行，针对参考测试台和这个提交的构建。它与上面的工作流只在标记部分
之前的步骤不同：本页安装发布版并启动你的服务，任务则构建该提交并启动测试台。
