# Contributing to LeetTest

Thanks for your interest. The project is early: expect the internals to move.

Two ways to work on it, with the same bar for both:

- **You write the change yourself** — read on.
- **An AI agent writes it** — point the agent at [AGENTS.md](AGENTS.md), then read the section
  [«With an AI agent»](#with-an-ai-agent) below. You answer for what it writes.

Bug reports and questions need none of the ceremony below: open an issue.

## What the project is for

LeetTest answers one question for a service's developers: **at what load does the service stop
coping, and where is the bottleneck**. Priorities, in order: the numbers do not lie >
convenience > speed. A change that makes a number nicer but less true is not accepted.

## Where users are: Linux and macOS first

People run LeetTest on Linux and macOS — next to the target, in containers, in CI. Windows is
supported, and it comes last. So:

- Check behaviour on Linux before calling a change done: CI runs there, and Docker covers it
  locally. A timing that holds on your laptop is one data point.
- Check the live view in a macOS and a Linux terminal; Windows terminals after them.
- Do not generalize from your own machine, editor or terminal. A problem seen only there is real,
  but it does not outrank the platforms the users are on.

## Before you write code

**Open an issue first** for anything beyond a typo. Describe the problem or the change and wait
for a reply: the architecture is still settling, and a pull request that cuts across it may be
turned down for reasons unrelated to its quality.

For a change in behaviour, the issue ends with a **spec**: a list of checkable statements about
what the tool will do, the edge cases, and what must not happen. Code starts once the spec is
agreed.

## Working on a change

Requirements: Go 1.26 or newer, `make`, and [golangci-lint](https://golangci-lint.run) at the
version in `.github/workflows/ci.yml` (`make lint-install`). `make race` needs a C compiler.

```console
$ make check      # vet, tests, golangci-lint (gofmt included) — must pass before a pull request
$ make race       # tests under the race detector
$ make stand      # the reference stand on 127.0.0.1:50051, for running the CLI against
$ make proto      # regenerate the stand's code after changing wallet.proto (needs Docker)
```

The order of work:

1. **Failing tests from the spec**, before the code. They fail on their assertions, not on
   compilation: start with a stub that returns zero values.
2. **The code**: the minimum that makes them pass.
3. **A mutation check**: break each line the tests guard, watch a test fail, revert. A test that
   passes on broken code checks nothing.

Where tests go: an end-to-end test against the reference stand (`test/measure`) is the main proof
that a number is right. A unit test states why it is not end-to-end, in a comment:
`// Ground: boundary|concurrency|contract|hot path|signal — why.`

A number the report prints needs an outside source: the reference stand with a known answer,
another tool, a publication.

## Conventions

- `pkg/` is a library. It does not know the CLI, the config file or any output format, and it
  does not log: it returns errors and events, and the CLI prints them.
- The core (scheduler, worker pool, sender, metrics) knows no features. A feature that is off is
  absent from the send loop entirely.
- Code reads on its own. Packages and exported names have doc comments; comments say what the
  name cannot — not what the code does.
- Dependencies: Apache-2.0, MIT, BSD, ISC only. CI rejects anything copyleft.

## Commits and pull requests

[Conventional Commits](https://www.conventionalcommits.org/), in English, the subject in
lowercase: `feat:`, `fix:`, `test:`, `docs:`, `ci:`, `chore:`, `refactor:`, `perf:`. The pull
request title follows the same format and is checked in CI: a squash merge turns it into the
commit on `main`.

The pull request body is short: **what changed, why, how it was checked.**

Trunk-based: short-lived branches (`feat/…`, `fix/…`, `test/…`, `docs/…`, `ci/…`) squashed into
`main`. `main` is always green and releasable; a release is a `vX.Y.Z` tag on it.

One approval and green checks (tests, lint, licenses, Conventional Commits, CLA) are required to
merge.

## With an AI agent

Agents are welcome to write code here. The rules are the same as for people, and a few matter
more:

- **Give the agent [AGENTS.md](AGENTS.md).** It holds the architecture, the order of work, the
  commands and the platform rule. For Claude Code, add a `CLAUDE.md` with the line `@AGENTS.md`.
- **Spec and failing tests before code** — agents write plausible code quickly; the spec is what
  makes it the right code. Read the spec yourself before the agent implements it.
- **Run the checks yourself, or read their output.** "The tests pass" from an agent is a claim;
  `make check` is the proof.
- **The pull request is yours.** Read the diff as if someone else wrote it. You sign the CLA, and
  you answer review comments.

## Docs

The README is written in Russian and translated into English. A README change ships in Russian and
English together.
The README describes what already works — it is not a list of promises.

Docs are not a dump: give the reader the most of what they need and the least of what they do
not. A guide says what LeetTest is, how to install and run it and how to read the result.
Internals the code hides (how timers work, why a rule was chosen) stay out of user docs; a
detail that changes the result or whether it can be trusted stays. Reference detail goes to a
reference. The same goes for tests (check the meaning — outcome, numbers, the claim made — and
the contract exactly, not the prose around it) and for pull requests (the change, not the review
history).
[AGENTS.md](AGENTS.md#docs) has the full rule.

## Contributor License Agreement

**Pull requests are merged only after the author has signed the [CLA](CLA.md).** It is a license,
not a transfer of ownership: you keep the copyright and may reuse your contribution anywhere. It
lets the project owner relicense the project without tracking down every past contributor.

Signing takes one comment on your first pull request, prompted by the CLA assistant:

```
I have read the CLA Document and I hereby sign the CLA
```

Once covers all your future contributions. Sign early, so a finished review does not wait on it.
