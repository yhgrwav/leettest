# AGENTS.md

Instructions for AI coding agents. People: start with [README](README.md),
[the quickstart](docs/en/quickstart.md) and [CONTRIBUTING](CONTRIBUTING.md).
Claude Code reads `CLAUDE.md`, not this file: put a `CLAUDE.md` with the single line
`@AGENTS.md` in your checkout.

LeetTest is a load generator for gRPC services. Its purpose: from one run, a report to the
service's developers saying **at what load the service stops coping and where the bottleneck
is**. Priorities, in order: the numbers in the report do not lie > convenience > speed.

The generator sees only the client side. Never infer the target's resources (CPU, memory,
queues) from client data, in code or in text.

---

## Part 1. Using LeetTest from an agent

### Where it runs

Linux and macOS first: the generator runs next to the target (a server in the same network, a
container, a CI runner). Windows works, but its clock steps about 0.5 ms, and a fast target there
often ends as an invalid run (`clock_step`, exit code 2). Measure on Linux.

### Run without a terminal

With stderr not a terminal, LeetTest prints a progress line a second to stderr instead of the
live view, and never waits for a key. Ask for JSON on stdout:

```console
$ leettest -c load.yaml -output json > report.json
$ echo $?
```

Stdout carries only the report; progress and errors go to stderr. The text report is ASCII; text
from outside (a method name, the target's error text) is escaped as `\uXXXX`.

### Exit codes — decide by these first

| Code | Meaning | What to do |
|---|---|---|
| `0` | The plan ran, the report is complete | Read the report |
| `1` | No run: flags, config, connection. No report | Read stderr: it names the field, the call or the cause |
| `2` | Invalid run: the in-flight cap was hit, every measured call of a method failed as a request error, client error or bad response, or the host clock is coarser than a quarter of a p50. A report exists, its numbers are not about the load | Read `invalid_reasons`; do not report latency from this run |
| `3` | Stopped before the plan ended. The report covers what ran | Say the run was partial |
| `130`, `143` | Killed by Ctrl+C or SIGTERM, no report | Re-run |

### Reading the JSON

`schema_version` is 1; fields are added, never renamed or retyped within a version. Decisions are
fields, not text — read these, not `notes`:

- `invalid_reasons`: `in_flight_cap`, `nothing_measured`, `clock_step`.
- `methods[].invalid_reason`: `request_error`, `client_error`, `bad_response`, `mixed`, or `null`.
- `methods[].silent_from_s`: the second (by when calls went out) from which the target answered
  nothing sent; `methods[].silent_sent_rps`: how many calls went out the second before it;
  `methods[].last_answer_at_us`: when the last call the target was heard on went out. All `null`
  without a silence.
- `tail_wait_cause`: `generator`, `stream`, `connection` or `null` — whether the latency tail is
  waiting on the client's side. If it is not `null`, the tail is not the target's.
- Percentiles: `{"us": …, "lower_bound": true}` (shown as `>500ms` in text) is a lower bound —
  calls that timed out. Never report it as the latency.
- `clock_step_ns`: the host clock's step; every latency is `±` that.
- `notes`: human text, reworded freely. Do not parse it.

### Writing a config

Every field, with what it does: [README, «Config»](docs/en/README.md). A config using all of them
against the reference stand: [`examples/tour.yaml`](examples/tour.yaml). Rules an agent trips on:

- The config is strict: an unknown field or a wrong value is an error with its line or call, not
  an empty run. `rps` is an integer.
- One call per method; several methods, each with its own `rps`, go as several calls.
- `method` is `package.Service/Method`. The schema comes from server reflection: no `.proto`.
- Secrets go in `app.metadata` as `${ENV_NAME}` only. Never write a token into a config, a command
  line or a log. LeetTest never prints header values.
- The in-flight cap must hold `rps × timeout` per method plus margin; LeetTest checks it before
  the start and names both ways out.

### Trying without a target

`-fake` loads a built-in fake target (`-fake-delay`, `-fake-jitter`, `-fake-fail-ratio`). The
report is marked `fake target`. Use it to check a config and the pipeline, never for numbers.

---

## Part 2. Changing LeetTest

### Platforms: where users are, not where you are

Users run LeetTest on Linux and macOS, next to the target and in CI; Windows is last. Do not
generalize from the machine you happen to run on:

- A timing, a rendering or a path that works on your machine is one data point. Check behaviour
  on Linux (Docker, CI) before calling it done; check the live view in a macOS and a Linux
  terminal before Windows.
- Platform-specific code goes only where a platform differs, and the Linux/macOS path is the one
  every test runs.
- A Windows-only problem does not outrank a Linux or macOS one.

### Architecture

```
cmd/leettest/       CLI: config → core → report → exit code. Thin.
pkg/engine/         scheduler, rate, in-flight cap, worker pool — the hot path
pkg/grpcsender/     real gRPC delivery, transport causes
pkg/descriptor/     method descriptors via server reflection
pkg/metrics/        HDR histograms, percentiles
pkg/config/         config parsing and validation
pkg/clock/          host clock step measurement
internal/cli/       live view, text and JSON reports
test/stand/         reference stand; test/measure/ — end-to-end measurement tests
```

- `pkg/*` is a library: it knows nothing of YAML, the CLI or output, and never logs. It returns
  errors and events; the CLI prints them.
- Core is scheduler, pool, sender, metrics. A feature is an option on top; off means absent from
  the send loop entirely. Do not add work to the hot path without a benchmark.

### The order of work

1. **Spec first.** A behaviour change starts as a list of checkable assertions, plus edge cases
   and what must not happen. A maintainer accepts it before code.
2. **Failing tests from the spec.** They fail on their assertions (against a stub returning zero
   values), not on compilation.
3. **Code** — the minimum that turns them green.
4. **Mutation check.** Break each line the tests guard, see a test go red, revert. A test that
   stays green on broken code checks nothing. For a test expecting "nothing", run the inverse too.

Tests: end-to-end against the reference stand (`test/measure`) first. A unit test needs one of
five grounds, named in a comment: `// Ground: boundary|concurrency|contract|hot path|signal — why.`

A number the report prints needs an outside source — the reference stand with a known answer,
another tool, a publication. "Sounds right" is not a source.

### Commands

```console
$ make check      # gofmt (via the linter), vet, tests, golangci-lint — before every push
$ make race       # tests under -race; needs gcc
$ make stand      # the reference stand on 127.0.0.1:50051
$ make demo       # a run against the built-in fake target
```

`make check && git push` — never put a pipe between a check and a push: a pipe returns the last
command's exit code and hides the failure.

Timing tests: reproduce flakes on Linux in Docker under load (`--cpus=1`, four `yes` processes in
the container), not on a desktop.

### Commits and pull requests

- Conventional Commits (`feat:`, `fix:`, `test:`, `docs:`, `ci:`, `chore:`, `refactor:`, `perf:`),
  in English, the subject lowercase.
- A pull request body is short: what changed, why, how it was checked.
- Dependencies: Apache-2.0, MIT, BSD, ISC only.
- The author signs the [CLA](CLA.md); an agent's change is the person's contribution, and that
  person answers for it.

### Docs

The README is written in Russian and translated; a README change ships in Russian and English
together. The README describes what works now: it is not a requirements source, and a mismatch
between it and the code is a question to ask, not a task to make the code match.
