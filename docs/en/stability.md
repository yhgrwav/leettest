# What does not change within version 1.x

[← Back to docs](README.md)

> Translated from [docs/ru/stability.md](../ru/stability.md) at ecdd128, 2026-11-09. If they
> differ, the Russian one is right.

**In force from v1.0.0; before that, see «Upgrading» in the [release notes](https://github.com/yhgrwav/leettest/releases).** Before 1.0 a minor version (`0.7`, `0.8`) may break what worked
before, and every such break is named under «Upgrading».

A version is written `major.minor.patch`. From v1.0.0 a script, config or dashboard that works on
`1.0` keeps working on every `1.y`. What exactly is covered is below; the values are taken from the
code and the reference as they are now.

The page has two parts: what changes only in a new major version (`2.0`) and what may change in a
minor one. The text of the human report is not part of the promise: scripts read the JSON and the
exit code.

## Does not change before `2.0`

### Exit codes

| Code | What happened |
|---|---|
| `0` | The plan ran, the report is complete; with `pass`, every limit held |
| `1` | The report you asked for was not produced: the run did not happen or the report was not written |
| `2` | The run is invalid, or a `pass` limit cannot be decided |
| `3` | The run stopped before its plan |
| `4` | A finished run missed a `pass` limit |
| `130` | Aborted without a report after Ctrl+C |
| `143` | Aborted without a report after SIGTERM |

The full description is in [exit codes](report.md#exit-codes).

### The report's JSON: `-output json`, `schema_version` 1

Every existing field keeps its name, type and meaning, and `schema_version` rises only with a major
version. The top-level fields:

- A run (`mode: "run"`): `schema_version`, `mode`, `leettest_version`, `target`, `name`,
  `methods_source`, `outcome`, `invalid_reasons`, `tail_wait_cause`,
  `generator_limit`, `stop_reason`, `stop`, `seq_overflow`, `clock_step_ns`, `started_at`, `duration_us`, `planned_us`, `warmup_us`,
  `settings`, `passport`, `sent`, `failed`, `aborted`, `not_sent`, `not_sent_generator`,
  `not_sent_stream`, `not_sent_connection`, `warmup_sent`, `warmup_failed`, `warmup_not_sent`,
  `cap_hit`, `start_lag`, `schedule`, `connections`, `client_waits`, `generator_cpu`, `traffic`,
  `methods`, `unchecked`, `thresholds`, `notes`.
- A breaking-point search (`mode: "breakpoint"`): `schema_version`, `mode`, `leettest_version`,
  `target`, `name`, `methods_source`, `method`, `started_at`, `settings`, `passport`, `breakpoint`,
  `thresholds`.

The nested fields, their types and meaning are in [JSON fields](reference.md#json-fields). The
schema's rules (units, `null` instead of `0`, what to do with unknown fields) are in
[JSON for scripts and CI](report.md#json-for-scripts-and-ci). `notes` and `unchecked[].error` are
text for people: their wording changes freely, do not parse them. The values of fields that name a
cause or a state (`outcome`, `invalid_reasons`, `stop_reason`, `unchecked[].reason`, the codes in
`failure_codes`) may grow: a script must not fail on a value it does not know.

### The 11 call category names

`success`, `request_error`, `overload`, `failure`, `timed_out`, `cut_off`, `unreachable`,
`client_error`, `bad_response`, `aborted`, `unclassified`. They are also the field names in the JSON
and the values of the `category` label in Prometheus. What each means is in
[reading the report](report.md#reading-the-report).

### Config keys and flags

Every config key and every flag that exists at 1.0: its name, meaning and accepted values. The full
list is in the [config](reference.md#config), the [flags](reference.md#flags) and
[comparing reports](reference.md#comparing-reports-leettest-compare) with its own flags.

### Prometheus metrics

The series names and the label names, and their meaning. The series are these now:

`leettest_planned_rps`, `leettest_calls_total`, `leettest_latency_seconds`,
`leettest_censored_total`, `leettest_excluded_total`, `leettest_not_sent_total`,
`leettest_in_flight`, `leettest_start_lag_seconds`, `leettest_warmup_sent_total`,
`leettest_warmup`.

The labels are `method`, `category`, `cause`, `le`. The histograms have the usual `_bucket`, `_sum`
and `_count`. What each series counts, its labels and type are in
[Prometheus metrics](reference.md#prometheus-metrics).

## May change in a minor version

- New JSON fields, flags, config keys, series and labels.
- The text of the human report.
- The stand and the CI tooling.
- Stricter config validation: it rejects only what was already wrong or undefined.
- A fix of a wrong number: a number that lied is a bug, not a change of the promise.

## Deprecation

A deprecated flag, key or field is announced under «Upgrading» in the release notes, prints one
warning on stderr when used, keeps working for the rest of the major version and goes in the next
one. Nothing is deprecated now.

## Where to look next

- [Reference](reference.md) — every key, flag, series and JSON field.
- [Reading the report](report.md#reading-the-report) — how to read the report; the exit codes and the JSON are further down the same page.
- [What LeetTest does not do](limitations.md) — the limits where the report's numbers stop being about the target.
