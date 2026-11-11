# 1.x 版本内不会改变什么

[← 返回文档](README.md)

> 译自 [docs/ru/stability.md](../ru/stability.md)，对应 ecdd128，2026-11-10。如有出入，以俄文版为准。
> **可能滞后：** 中文版在每次发布前同步，最新内容请看[英文版](../en/stability.md)。

**自 v1.0.0 起生效；在此之前请看[发布说明](https://github.com/yhgrwav/leettest/releases)中的“Upgrading”部分。** 1.0 之前，次版本号（`0.7`、`0.8`）可能破坏以前可用的东西，每一处这样的破坏都会在“Upgrading”中点明。

版本写作 `主版本.次版本.补丁`。自 v1.0.0 起，在 `1.0` 上能运行的脚本、配置或仪表盘，在任何 `1.y` 上
都会继续运行。具体包含什么见下文：取值来自代码和参考文档，就是它们现在的样子。

下面分两部分：只有在新的主版本（`2.0`）中才会改变的内容，以及可以在次版本中改变的内容。面向人的报告
文字不在承诺范围内：脚本应读取 JSON 和退出码。

## 到 `2.0` 之前不会改变

### 退出码

| 退出码 | 发生了什么 |
|---|---|
| `0` | 计划已执行完，报告完整；使用 `pass` 时——所有限值都达标 |
| `1` | 没有得到要求的报告：运行没有进行或报告没有写出 |
| `2` | 运行无效，或无法判定某个 `pass` 限值 |
| `3` | 运行在计划结束前被停止 |
| `4` | 已完成的运行没有达到某个 `pass` 限值 |
| `130` | 按 Ctrl+C 后无报告地异常退出 |
| `143` | 收到 SIGTERM 后无报告地异常退出 |

完整说明见[退出码](../en/report.md#exit-codes)（英文）。

### 报告 JSON：`-output json`，`schema_version` 1

每个已有字段都保持名称、类型和含义，`schema_version` 只随主版本增长。顶层字段：

- 运行（`mode: "run"`）：`schema_version`、`mode`、`leettest_version`、`target`、`name`、
  `methods_source`、`outcome`、`invalid_reasons`、`tail_wait_cause`、
  `generator_limit`（尚未发布）、`stop_reason`、`stop`、`seq_overflow`、`clock_step_ns`、`started_at`、`duration_us`、`planned_us`、`warmup_us`、
  `settings`、`passport`、`sent`、`failed`、`aborted`、`not_sent`、`not_sent_generator`、
  `not_sent_stream`、`not_sent_connection`、`warmup_sent`、`warmup_failed`、`warmup_not_sent`、
  `cap_hit`、`start_lag`、`schedule`、`connections`、`client_waits`、`generator_cpu`、`traffic`、
  `methods`、`unchecked`、`thresholds`、`notes`。
- 断点搜索（`mode: "breakpoint"`）：`schema_version`、`mode`、`leettest_version`、`target`、
  `name`、`methods_source`、`method`、`started_at`、`settings`、`passport`、`breakpoint`、
  `thresholds`。

嵌套字段、类型和含义见 [JSON 字段](../en/reference.md#json-fields)（英文）。模式的规则（单位、用 `null`
代替 `0`、遇到未知字段怎么办）见 [JSON：用于脚本和 CI](../en/report.md#json-for-scripts-and-ci)（英文）。
`notes` 和 `unchecked[].error` 是给人看的文字，措辞可以随意改动，不必解析。枚举型字段的取值
（`outcome`、`invalid_reasons`、`stop_reason`、`unchecked[].reason`、`failure_codes` 中的代码）可能增加：
脚本遇到不认识的取值时不得崩溃。

### 11 个调用类别的名称

`success`、`request_error`、`overload`、`failure`、`timed_out`、`cut_off`、`unreachable`、
`client_error`、`bad_response`、`aborted`、`unclassified`。它们同时也是 JSON 中的字段名和 Prometheus 中
标签 `category` 的取值。每个的含义见[如何阅读报告](../en/report.md#reading-the-report)（英文）。

### 配置键和命令行标志

1.0 中存在的每个配置键和每个标志：名称、含义和允许的取值。完整列表见
[配置](../en/reference.md#config)、[标志](../en/reference.md#flags)，以及带有各自标志的
[比较报告](../en/reference.md#comparing-reports-leettest-compare)（均为英文）。

### Prometheus 指标

序列名和标签名，以及它们的含义。目前的序列有：

`leettest_planned_rps`、`leettest_calls_total`、`leettest_latency_seconds`、
`leettest_censored_total`、`leettest_excluded_total`、`leettest_not_sent_total`、
`leettest_in_flight`、`leettest_start_lag_seconds`、`leettest_warmup_sent_total`、
`leettest_warmup`。

标签：`method`、`category`、`cause`、`le`。直方图带有常规的 `_bucket`、`_sum`、`_count`。每个序列统计
什么、有哪些标签和什么类型，见 [Prometheus 指标](../en/reference.md#prometheus-metrics)（英文）。

## 可以在次版本中改变

- 增加 JSON 字段、标志、配置键、序列和标签。
- 修改面向人的报告文字。
- 修改测试台和 CI 工具。
- 收紧配置检查：只拒绝以前就是错误或未定义的内容。
- 修正错误的数字：说谎的数字是缺陷，不是对承诺的改动。

## 弃用

被弃用的标志、键或字段会在发布说明的“Upgrading”中宣布，使用时向 stderr 打印一条警告，在整个主版本
结束前继续可用，并在下一个主版本中移除。目前没有任何东西被宣布弃用。

## 接下来看哪里

- [参考](../en/reference.md)（英文）——每个键、标志、序列和 JSON 字段。
- [报告、退出码、JSON](../en/report.md)（英文）——如何阅读报告，以及其中哪些内容可以用脚本解析。
- [LeetTest 不做什么](limitations.md)——报告中的数字不再说的是目标服务的那些边界。
