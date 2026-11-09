# AGENTS.md

Instructions for AI coding agents that change LeetTest's code. People: start with [README](https://github.com/yhgrwav/leettest/blob/main/README.md)
and [CONTRIBUTING](https://github.com/yhgrwav/leettest/blob/main/CONTRIBUTING.md).
Claude Code reads `CLAUDE.md`, not this file: put a `CLAUDE.md` with the single line
`@AGENTS.md` in your checkout.

**An agent that sets LeetTest up for a user** reads [docs/agent.md](https://github.com/yhgrwav/leettest/blob/main/docs/agent.md), not this file.

LeetTest is a load generator for gRPC services. Its purpose: from one run, a report to the
service's developers saying **at what load the service stops coping and where the bottleneck
is**. Priorities, in order: the numbers in the report do not lie > convenience > speed.

The generator sees only the client side. Never infer the target's resources (CPU, memory,
queues) from client data, in code or in text.

---

## Changing LeetTest

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
pkg/descriptor/     method descriptors via server reflection, .proto files or a protoset
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
- **Every capability in a guide is a command to copy.** A fenced block the reader runs as is: no
  `$ ` prompt, no `[...]` optional parts, no `/path/to`. A placeholder only when it cannot be
  avoided, with a line saying what to put there. After the command, what the reader will see.
- A fix does not leave its story in the docs. If a sentence explains how you got here, delete it.

**Tests are not a dump either.** A test checks meaning, not prose: the outcome, the numbers, the
category, which claim the output makes ("at most T", "not recovered" rather than "held", "nothing
was learned about the target"). The contract is checked exactly: JSON field names and enum
values, exit codes, the ASCII rule. The characterization frames of the screen stay: they guard
refactors and are changed on purpose, in a commit of their own. What is left out is pinning the
wording around a claim, which breaks on every edit and protects nothing.

**Pull requests carry the change, not the process.** Review threads, verdicts, internal notes and
the history of how a decision was reached stay out of public PRs, commits and docs.
