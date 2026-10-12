# AGENTS.md

Instructions for AI coding agents. People: start with [README](https://github.com/yhgrwav/leettest/blob/main/README.md),
[the quickstart](https://github.com/yhgrwav/leettest/blob/main/docs/en/quickstart.md) and
[CONTRIBUTING](https://github.com/yhgrwav/leettest/blob/main/CONTRIBUTING.md).
Claude Code reads `CLAUDE.md`, not this file: put a `CLAUDE.md` with the single line
`@AGENTS.md` in your checkout.

LeetTest is a load generator for gRPC services. Its purpose: from one run, a report to the
service's developers saying **at what load the service stops coping and where the bottleneck
is**. Priorities, in order: the numbers in the report do not lie > convenience > speed.

The generator sees only the client side. Never infer the target's resources (CPU, memory,
queues) from client data, in code or in text.

---

## Part 1. Using LeetTest from an agent

### From zero to a report

1. **Install** the release binary (curl and tar; no Go). Linux and macOS; Windows has a `.zip`.
   The script verifies the checksum and installs into `~/.local/bin` (`LEETTEST_INSTALL_DIR`
   changes it, `LEETTEST_VERSION` pins a tag); it exits 1 with a message on any failure.
   ```sh
   curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
   export PATH="$HOME/.local/bin:$PATH"   # the script cannot change your shell's PATH
   leettest -version
   ```
   Or `go install github.com/yhgrwav/leettest/cmd/leettest@latest` (Go 1.26+; an older Go with
   `GOTOOLCHAIN=auto` fetches it) and `export PATH="$(go env GOPATH)/bin:$PATH"`.
2. **Ask the user** the target's address, TLS or not, which methods, how hard (rps, duration).
   **Never send load to a target the user has not named and confirmed, with the rate and duration
   said back to them.** A production target — only on the user's explicit yes.
3. **Write `load.yaml`** ([every field](https://github.com/yhgrwav/leettest/blob/main/docs/en/reference.md#config)).
   TLS is on by default: a plaintext service needs `tls: false`.
   ```yaml
   app:
     target: {ip: HOST, port: 50051}    # a name or an IP
     tls: false
     metadata: {authorization: "Bearer ${SERVICE_TOKEN}"}   # only if the service wants it
   load:
     calls:
       - {method: package.Service/Method, rps: 10, duration: 10s}   # data: {...} for a body
   ```
   For **at what load the service stops coping**, use `load.breakpoint` instead of `rps` and
   `duration`: [`examples/breakpoint.yaml`](https://github.com/yhgrwav/leettest/blob/main/examples/breakpoint.yaml).
4. **First run small**, as above (10 rps, 10 s). Before the first call LeetTest connects and checks
   every method through reflection and the body; an error is exit 1, names its line or call, sent nothing.
   Reflection off on the service: a method with `data` will not start («server reflection is not
   enabled on the target»). Two ways out: drop `data` (an empty message goes out, with a warning,
   the method unchecked), or wait for [`.proto` support](https://github.com/yhgrwav/leettest/blob/main/docs/en/roadmap.md).
   Do not guess a body.
5. **Run:** `leettest -c load.yaml -plain -output json > report.json; echo $?`. It takes the longest
   `duration` (warm-up included) plus the longest `timeout` plus up to 10 s to connect; a breakpoint search
   prints its worst case on stderr first. Set your tool's timeout above that.
6. **Tell the user**, in this order, from the
   [JSON fields](https://github.com/yhgrwav/leettest/blob/main/docs/en/reference.md#json-fields):
   - the exit code (table below); `0` only means the plan ran, not that the service is healthy;
   - the load where it stopped coping, in rps. A search: `breakpoint.outcome` `broke` — held
     `held_rps`, broke at `broke_rps`; `held_all` — held every step, the limit is above `held_rps`;
     `broke_at_first` — start lower; `run_limit` — the generator ran out, nothing is known about
     the target above that. A plain run: `silent_from_s` is the second from which it answered
     nothing;
   - the cause as the client saw it: `why` or `failure_codes` (`from_target` says who set the
     code) and `tail_wait_cause` (not `null` — the slow tail is the client's wait, not the target);
   - a percentile with `lower_bound: true` is «more than», never a latency;
   - never anything about the target's CPU, memory or queues: the generator cannot see them.
7. **Without the user's service:** `-fake` needs a config, any one: `curl -fsSLO
   https://raw.githubusercontent.com/yhgrwav/leettest/main/examples/leettest.yaml`, then
   `leettest -c leettest.yaml -fake -plain` (a minute). The reference stand's tour needs the
   repository and Go: [quickstart §2](https://github.com/yhgrwav/leettest/blob/main/docs/en/quickstart.md#2-a-target-the-reference-stand).

### Where it runs

Linux and macOS first: the generator runs next to the target (a server in the same network, a
container, a CI runner). Windows works, but its clock steps about 0.5 ms, and a fast target there
often ends as an invalid run (`clock_step`, exit code 2). Measure on Linux.

### Run without a terminal

With stderr not a terminal, LeetTest prints a progress line a second to stderr instead of the
live view, and never waits for a key. Behind a pseudo-terminal (a shell driven through a pty, ssh)
add `-plain` for the same. Ask for JSON on stdout:

```sh
leettest -c load.yaml -output json > report.json
echo $?
```

Stdout carries only the report; progress and errors go to stderr. The text report is ASCII; text
from outside (a method name, the target's error text) is escaped as `\uXXXX`.

### Exit codes — decide by these first

Source of truth: [README, «Exit codes»](https://github.com/yhgrwav/leettest/blob/main/docs/en/README.md#exit-codes). What each code asks of an agent:

| Code | What to do |
|---|---|
| `0` | Read the report |
| `1` | No run, no report: read stderr, it names the field, the call or the cause |
| `2` | Invalid run: read `invalid_reasons`, do not report latency from it |
| `3` | Partial run: say so |
| `130`, `143` | Killed, no report: re-run |

### Reading the JSON

Every JSON field is described in the [reference, «JSON fields»](https://github.com/yhgrwav/leettest/blob/main/docs/en/reference.md#json-fields),
checked against `internal/cli/testdata/schema_v1.txt`; the contract rules are in the
[README, «JSON for scripts and CI»](https://github.com/yhgrwav/leettest/blob/main/docs/en/README.md#json-for-scripts-and-ci). How an agent reads them:

- Decisions are fields, not text: read `invalid_reasons`, `methods[].invalid_reason`,
  `tail_wait_cause`, `methods[].silent_from_s`. Never parse `notes` — it is reworded freely.
- A percentile with `"lower_bound": true` (`>500ms` in text) is not a latency; never report it as
  one.
- `tail_wait_cause` not `null` means the latency tail is the client's, not the target's.

### Writing a config

Every field, with what it does: [reference](https://github.com/yhgrwav/leettest/blob/main/docs/en/reference.md). Configs using all of them
against the reference stand: [`examples/tour.yaml`](https://github.com/yhgrwav/leettest/blob/main/examples/tour.yaml) (needs `LEETTEST_TOKEN`
set; `demo` is the shared token we give the first users) and, for TLS, [`examples/tour-tls.yaml`](https://github.com/yhgrwav/leettest/blob/main/examples/tour-tls.yaml) against a stand started with
`-mtls` from the repository root. Rules an agent trips on:

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

### Feedback after a problem

Offer once per session, only if something went wrong with LeetTest: an exit code 1, 2 or 3 the
user did not expect, an invalid run they could not fix, a message or doc neither of you understood,
a crash or a hang. Otherwise never. Declined — drop it, do not ask again.

- **Ask** the user to say what went wrong without the business details of their project.
- **Fields**, by form id, one line each: `version` (`leettest --version`), `platform`, `arch`
  (`uname -m` or `GOARCH`), `where`, `exit_code` (with 2, `invalid_reasons` from the JSON),
  `kind`, `expected`, `got`; `note` — the user's own words, optional.
- **Never in it:** the target address, host, service and method names, metadata, tokens, TLS
  files, config contents, report numbers — unless the user asks for a part and has seen it with
  those removed. For `kind` «Подозрительное число» offer the numbers that show it (JSON field,
  value, unit); they go in once the user has seen them.
- **The note:** keep what is about LeetTest; drop links, ads, insults, others' data; say what went.
  «Хлам, берите наш x.io, падает на payments.internal» → «Падает на нашем сервисе, ошибка неясна».
- **Duplicates first:** `gh issue list -R yhgrwav/leettest --search "<words>"`. A match — the user
  may add a 👍 or a comment there; that is theirs to do, not yours.
- **Never post yourself.** Show the final text, then the link; the user submits it:
  `https://github.com/yhgrwav/leettest/issues/new?template=agent_feedback.yml&version=…&got=…`.
  Only `template`, `title` (starting `[агент] `) and field ids; values URL-encoded as UTF-8,
  newline `%0A`; a link too long gets 414. A list takes its option as the value: `platform`
  Linux / macOS / Windows / Другое; `where` Локально / CI / Контейнер; `kind` Непонятное сообщение
  или документация / Подозрительное число / Падение или зависание / Установка / Другое. The
  checkbox «Перед отправкой» is the user's to tick.

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

```sh
make check      # gofmt (via the linter), vet, tests, golangci-lint — before every push
make race       # tests under -race; needs gcc
make stand      # the reference stand on 127.0.0.1:50051
make demo       # a run against the built-in fake target
make proto      # regenerate the stand's code from wallet.proto, in Docker
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
- The author signs the [CLA](https://github.com/yhgrwav/leettest/blob/main/CLA.md); an agent's change is the person's contribution, and that
  person answers for it.

### Docs

The README is written in Russian and translated; a README change ships in Russian and English
together, German and Chinese catch up before a release. The README describes what works now: it is not a requirements source, and a mismatch
between it and the code is a question to ask, not a task to make the code match.

**Docs are not a dump.** Write for the reader, not about the problem you just solved:

- The most of what the reader needs, the least of what they do not. A guide answers what it is,
  how to install it, how to run it, how to read the result. Everything else is cut.
- What the code hides behind an interface stays out of user docs: how timers work, how an
  internal rule was chosen, what the code already handles. That is the developer docs' topic.
- A detail stays in user docs if it changes the result or whether the result can be trusted:
  exact scheduling only on Linux, a coarse clock step that makes a run invalid, measuring from
  Linux next to the target. What the user does not need to get true numbers is cut, and there
  are no "if you are on Windows rather than macOS, then…" paragraphs beyond that.
- Reference detail (every JSON field, every enum value) lives in a reference, not in the guide.
- A fix does not leave its story in the docs. If a sentence explains how you got here, delete it.

**Tests are not a dump either.** A test checks meaning, not prose: the outcome, the numbers, the
category, which claim the output makes ("at most T", "not recovered" rather than "held", "nothing
was learned about the target"). The contract is checked exactly: JSON field names and enum
values, exit codes, the ASCII rule. The characterization frames of the screen stay: they guard
refactors and are changed on purpose, in a commit of their own. What is left out is pinning the
wording around a claim, which breaks on every edit and protects nothing.

**Pull requests carry the change, not the process.** Review threads, verdicts, internal notes and
the history of how a decision was reached stay out of public PRs, commits and docs.
