# LeetTest in CI

[← Back to docs](README.md)

> Translated from [docs/ru/ci.md](../ru/ci.md) at ecdd128, 2026-11-09. If they differ, the Russian
> one is right.

This page shows how to run a load test on every change to a service and fail the build when the
service no longer holds the limits you set. The example is a GitHub Actions workflow; any CI that
runs shell commands does the same four things:

1. Install LeetTest.
2. Start the service under test.
3. Run the load. The exit code is the verdict.
4. Keep the JSON report.

## The config

`load.yaml` in the service's repository. The limits under `pass` make the exit code a verdict:
without them a run that reaches its end returns `0` however the service answered
([pass limits](README.md#pass-limits-a-run-as-a-check)). `app.target` is the service the job
started; in this file it is the reference stand from this repository, the target of our own CI.

```yaml
# A load test for CI. app.target is the service the job started; here it is the reference stand,
# from the repository root:
#   go run ./test/stand/cmd/stand -delay 20ms
#   leettest -c examples/ci.yaml
# The pass limits decide the exit code: both hold on the stand, so it is 0; a missed limit is 4.

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

## The workflow

`.github/workflows/load-test.yml`:

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

The step by step:

- **Install.** `install.sh` puts the latest release into `~/.local/bin` and checks its checksum
  ([install](README.md#install)). A build that must not change under you pins a release: put
  `LEETTEST_VERSION=<tag>` before `sh`, with a tag from the
  [releases page](https://github.com/yhgrwav/leettest/releases).
- **Start the service.** Replace the step with however your service starts, and point
  `app.target` in `load.yaml` at it. The load starts as soon as LeetTest does: wait until the
  service answers before that step ends (`--wait` does it for Docker Compose).
- **Run the load.** `-json-file report.json` writes the JSON report to a file while the text
  report goes to the job log. The `case` turns each exit code into one error line; any
  non-zero code fails the build. The file is created before the start and never overwritten, so a
  fresh runner is what it expects.
- **Read the report.** `jq` takes one field out of the JSON: here the p99 of the first method, into
  the job summary. The second line checks that `pass` limits were set and every one passed: an exit
  code `0` from a config with no limits says nothing about the service. The step runs also after a
  failed load, whenever the file exists.
- **Keep the report.** `if: always()` uploads it for a failed run too. A run that did not start
  (exit code `1`) leaves no file, hence `if-no-files-found: ignore`.

The two `# example workflow` comment lines mark the part that LeetTest's own CI runs, as written.

## Exit codes in a build

| Code | What happened | The build |
|---|---|---|
| `0` | The plan ran, the report is complete; with `pass`, every limit held | passes |
| `1` | The report you asked for was not produced: the run did not happen or the report was not written | fails; stderr says why |
| `2` | The run is invalid, or a `pass` limit cannot be decided | fails; this is not a pass |
| `3` | The run stopped before its plan | fails; the report covers only what ran |
| `4` | A finished run missed a `pass` limit | fails; `thresholds` in the JSON says which |
| `130` | Aborted without a report after Ctrl+C | fails; no report |
| `143` | Aborted without a report after SIGTERM | fails; no report |

`2` is neither a pass nor a miss: the numbers in that report are not about the service's load, so
they cannot show that a limit held. The report names the reason (`invalid_reasons` in the JSON): the
generator was short of CPU, the host clock is too coarse, a limit could not be decided. Fix the
setup; loosening a limit does not help. The codes do not change within version 1.x:
[what stays the same](stability.md#exit-codes).

## Reading the JSON

The text of the report is for people, the JSON and the exit code are for scripts. A script may
rely on the fields and values listed in [what stays the same](stability.md); every field is in the
[reference](reference.md#json-fields). A few that a build usually reads:

| Field | What it holds |
|---|---|
| `outcome` | `complete`, `invalid` or `incomplete`; the same fact as exit codes `0`/`4`, `2`, `3` |
| `thresholds[].verdict` | `pass`, `fail` or `unknown` for each `pass` limit |
| `invalid_reasons` | why a run is invalid; empty for a valid one |
| `methods[].latency.p99.us` | the p99 of a method in whole microseconds |

A script skips the fields it does not know and does not fail on a new value of an enum.

## Where the runner matters

The load goes out from the runner, so everything between the runner and the service is in the
latency, and a runner short of CPU cannot keep the schedule: the report then says
`generator_late` and the exit code is `2`. Run the job next to the service, in the same network or
the same job, and keep the rate within what the runner sends. More in
[where to run](reference.md#where-to-run) and [what the numbers do not cover](limitations.md).

## The example is tested

The part of the workflow between the two marker comments is the job `example workflow` of
[`.github/workflows/ci.yml`](../../.github/workflows/ci.yml), byte for byte;
[`examples/ci_page_test.go`](../../examples/ci_page_test.go) fails when they differ. The job runs on
every pull request, against the reference stand and a build of that commit. It differs from the
workflow above only in the steps before the marked part: the page installs the release and starts
your service, the job builds the commit and starts the stand.
