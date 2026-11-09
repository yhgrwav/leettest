# LeetTest: instructions for an AI agent

You are setting up LeetTest, a load generator for gRPC services, for a person who asked you to. Do the
four steps below in order. Where a step needs a decision, the person makes it.

Purpose: from one run, a report to the service's developers saying **at what load the service stops
coping**. The generator sees only the client side. Never say anything about the target's CPU, memory
or queues: the report does not contain them.

**Never send load to a service the person has not named and confirmed, with the rate and duration said
back to them.** A production service only on the person's explicit yes.

## Step 1. Install LeetTest and check it starts

Linux and macOS need `curl` and `tar`, not Go. The script verifies the checksum, installs into
`~/.local/bin` (`LEETTEST_INSTALL_DIR` changes it, `LEETTEST_VERSION` pins a tag) and exits 1 with a
message on any failure. A variable goes before `sh`, not before `curl`:
`... | LEETTEST_VERSION=v0.2.0 sh`.

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
leettest -version
```

Windows: take the `.zip` from the [latest release](https://github.com/yhgrwav/leettest/releases/latest),
unpack `leettest.exe` into a folder and add that folder to `PATH` (the `export` lines here are bash;
in PowerShell use `$env:Path += ";C:\folder"`). Or `go install github.com/yhgrwav/leettest/cmd/leettest@latest` (Go 1.26+; an older Go with
`GOTOOLCHAIN=auto` fetches it), then `export PATH="$(go env GOPATH)/bin:$PATH"`.

Done when `leettest -version` prints a version.

Where it should run: Linux or macOS, next to the service (same network, container or CI runner).
Windows works, but its clock steps about 0.5 ms, and a fast service there often ends as an invalid run
(`clock_step`, exit code 2). If you are on Windows, say so in step 4 (a valid run there may still carry
`schedule.not_exact_reasons`: the timing is not exact), and run for numbers on Linux.

## Step 2. Ask the person

Ask, in one message:

1. the service address (a name or an IP) and port;
2. TLS or not (TLS is on by default; a plaintext service needs `tls: false`), and a client certificate
   if the service wants one;
3. which methods to load, as `package.Service/Method`;
4. a token or API key, if the service wants one: ask which environment variable holds it. Never ask for
   the value, never write it into a file, a command line or a log;
5. how hard: requests per second and how long. Suggest 10 rps for 10 seconds for the first run;
6. whether this is a production service. A production service only on an explicit yes.

Before the first run say the address, the rate and the duration back and wait for the person's yes,
unless they already stated all three in their request.

If the person has no service to try, use the test one below (step 3, "Without a service").

## Step 3. Write the config, check it without load, run a short trial

Write `load.yaml` ([every field](https://github.com/yhgrwav/leettest/blob/main/docs/en/reference.md#config)):

```yaml
app:
  target: {ip: HOST, port: 50051}    # a name or an IP
  tls: false
  metadata: {authorization: "Bearer ${SERVICE_TOKEN}"}   # only if the service wants it
load:
  calls:
    - {method: package.Service/Method, rps: 10, duration: 10s}   # data: {...} for a body
```

Rules an agent trips on:

- The config is strict: an unknown field or a wrong value is an error with its line or call, not an
  empty run. `rps` is an integer.
- `target.ip` takes a name as well as an IP. Defaults: `timeout` 2 s per call, in-flight cap 5000.
  A client certificate and the other TLS fields are in the reference linked above.
- One call per method; several methods, each with its own `rps`, go as several calls.
- The schema comes from server reflection. A service without reflection: a method with `data` will not
  start («server reflection is not enabled on the target»). Two ways out: drop `data` (an empty message
  goes out, with a warning, the method unchecked), or, from v0.3, give the schema: `app.proto`
  (`files` and the `import_paths` their imports are looked up on) or `app.protoset` (built with
  `protoc --include_imports`) —
  [`examples/proto.yaml`](https://github.com/yhgrwav/leettest/blob/main/examples/proto.yaml),
  [`examples/protoset.yaml`](https://github.com/yhgrwav/leettest/blob/main/examples/protoset.yaml).
  Do not guess a body.
- Secrets go in `app.metadata` as `${ENV_NAME}` only. LeetTest never prints header values.
- The in-flight cap must hold `rps × timeout` per method plus margin; LeetTest checks it before the
  start and names both ways out.
- Limits the person states ("p99 under 200 ms, under 1% failures") go under the call as `pass` (`p50`,
  `p90`, `p95`, `p99`, `p999`, `errors`; a search: `breakpoint.pass.held_rps`). Never invent limits the
  person did not give. A latency limit needs `errors` next to it, and `errors: 100%` switches the failure
  check off. Every key: [`examples/pass.yaml`](https://github.com/yhgrwav/leettest/blob/main/examples/pass.yaml).
- For **at what load the service stops coping**, use `load.breakpoint` instead of `rps` and `duration`:
  [`examples/breakpoint.yaml`](https://github.com/yhgrwav/leettest/blob/main/examples/breakpoint.yaml).
  Do it after the first small run has passed.

Check the config without load. LeetTest connects, checks every method through reflection and the body,
sends one request per call and prints the request and the answer; there is no report. An error is exit
1, names its line or call, and sent nothing:

```sh
leettest -c load.yaml -dry-run
```

Then the first run, small (10 rps, 10 s):

```sh
leettest -c load.yaml -plain -output json > report.json; echo $?
```

The JSON is one long line: pick fields with `jq`, for example `jq '.methods[].latency' report.json`.
The run takes the longest `duration` (warm-up included) plus the longest `timeout` plus up to 10 s to connect
(10 s run, default timeout: set your tool's timeout to 30 s or more);
a breakpoint search prints its worst case on stderr first. Set your tool's timeout above that.

Without a terminal LeetTest prints a progress line a second to stderr and never waits for a key; `-plain`
does the same behind a pseudo-terminal. Stdout carries only the report; progress and errors go to stderr.
To keep the text report on stdout and get the JSON from the same run, add `-json-file report.json`: the
file must not exist yet.

**Without a service:** `-fake` loads a built-in fake target (`-fake-delay`, `-fake-jitter`,
`-fake-fail-ratio`). It needs a config, any one: `curl -fsSLO
https://raw.githubusercontent.com/yhgrwav/leettest/main/examples/leettest.yaml`, then
`leettest -c leettest.yaml -fake -plain` (a minute). The report is marked `fake target`: use it to check
a config and the pipeline, never for numbers. A real test service with a known behaviour comes with the
repository (`make stand`, needs Go and a clone):
[tour](https://github.com/yhgrwav/leettest/blob/main/docs/ru/guide/first-run.md#пробный-прогон-на-тестовом-сервисе).

### Exit codes: decide by these first

| Code | What to do |
|---|---|
| `0` | Read the report. It means the plan ran, not that the service is healthy; with `pass` set, every limit held |
| `1` | The report you asked for was not produced (no run, or `-json-file` not written): read stderr, it names the field, the call, the path or the cause |
| `2` | Invalid run: read `invalid_reasons`, do not report latency from it |
| `3` | Partial run: say so |
| `4` | A finished run missed a `pass` limit: read `thresholds` for which one (`verdict` `fail`), tell the person the limit and the value |
| `130`, `143` | Killed, no report: re-run |

`2` also means a `pass` limit that cannot be decided (`invalid_reasons` has `thresholds_undecided`;
`thresholds[].reason` says why): tell the person the limit was not checked, never that it held.

### Reading the JSON

Every field is described in the
[reference, «JSON fields»](https://github.com/yhgrwav/leettest/blob/main/docs/en/reference.md#json-fields);
the contract rules are in the
[README, «JSON for scripts and CI»](https://github.com/yhgrwav/leettest/blob/main/docs/en/README.md#json-for-scripts-and-ci).

- Decisions are fields, not text: read `invalid_reasons`, `methods[].invalid_reason`, `tail_wait_cause`,
  `methods[].silent_from_s`. Never parse `notes`: it is reworded freely.
- A percentile with `"lower_bound": true` (`>500ms` in text) is not a latency; never report it as one.
- `tail_wait_cause` not `null` means the latency tail is the client's wait, not the target's.
- The load where the service stopped coping, in rps. A search: `breakpoint.outcome` `broke` — held
  `held_rps`, broke at `broke_rps`; `held_all` — held every step, the limit is above `held_rps`;
  `broke_at_first` — start lower; `run_limit` — the generator ran out, nothing is known about the target
  above that. A plain run: `silent_from_s` is the second from which it answered nothing.
- The cause as the client saw it: `why` or `failure_codes` (`from_target` says who set the code).

### Comparing two reports

Two search reports (`-json-file` of a search, before and after a change):
`leettest compare -max-capacity-drop 10% before.json after.json`. The exit codes mean the same: `4` the
capacity fell past the limit, `2` the reports differ in a field the printed `differs:` line names, or the
limit cannot be decided from a bound (`>=` or `below`): tell the person it was not checked, never that it
held.

Two plain-run reports take `-max-p99-rise 20%` (the p99 rise per method, in percent of the base p99) and
`-max-errors-rise 1%` (the rise of the failed share, in points):
`leettest compare -max-p99-rise 20% before.json after.json`. Same exit codes. `2` also when a run is
invalid or incomplete, a p99 hit the timeout (`>=`) or client-side errors leave it open: tell the person
the check was not made. A search report and a run report together is `1`.

For a script, `leettest compare -output json ...` prints one object (stdout is empty on exit `1`). Read
`verdict` first (`pass`, `fail`, `unknown`, `null` when no limit was given), then `comparable` and
`differences`, then `checks[]`: `method`, `key` (`capacity`, `p99`, `errors`), the two sides' values,
`limit_percent`, `verdict`, and for an `unknown` the `reason` with `why`. The object has no percentage
change: compute it from the values. Never report an `unknown` as held.

## Step 4. Tell the person

Write 3 or 4 plain sentences **in the person's language**, no jargon, no JSON field names. Cover, in this
order:

1. How to run it again: the exact command and where the config file is.
2. The number to look at first: the load at which the service stopped coping (in requests per second), or
   if the run was a plain one, the p99 and the exit code. A `>` before a latency means «at least», never
   an exact value. Exit code `0` means the plan ran, not that the service is healthy.
3. What it cannot say: LeetTest sees only the client side, so it names no cause inside the service.
4. What to do if something goes wrong: read the message LeetTest printed (it names the field or the
   call); if it stays unclear, open an issue at https://github.com/yhgrwav/leettest/issues/new/choose
   without the service's address, names or tokens.

Example, for a person who writes English: «Run it again with `leettest -c load.yaml`; the config is
`/home/me/load.yaml`. The service held 800 requests per second and broke at 1000, so start with 800.
This is what the client saw; it does not say what inside the service broke. If a run fails, the message
names the line to fix; if it is unclear, tell me or open an issue.»

## Feedback after a problem

Offer once per session, only if something went wrong with LeetTest: an exit code 1, 2 or 3 the person did
not expect, an invalid run they could not fix, a message or doc neither of you understood, a crash or a
hang. Otherwise never. Declined — drop it, do not ask again.

- **Ask** the person to say what went wrong without the business details of their project.
- **Fields**, by form id, one line each: `version` (`leettest --version`), `platform`, `arch`
  (`uname -m` or `GOARCH`), `where`, `exit_code` (with 2, `invalid_reasons` from the JSON), `kind`,
  `expected`, `got`; `note` — the person's own words, optional.
- **Never in it:** the target address, host, service and method names, metadata, tokens, TLS files,
  config contents, report numbers — unless the person asks for a part and has seen it with those removed.
  For `kind` «Подозрительное число» offer the numbers that show it (JSON field, value, unit); they go in
  once the person has seen them.
- **The note:** keep what is about LeetTest; drop links, ads, insults, others' data; say what went.
  «Хлам, берите наш x.io, падает на payments.internal» → «Падает на нашем сервисе, ошибка неясна».
- **Duplicates first:** `gh issue list -R yhgrwav/leettest --search "<words>"`. A match — the person may
  add a 👍 or a comment there; that is theirs to do, not yours.
- **Never post yourself.** Show the final text, then the link; the person submits it:
  `https://github.com/yhgrwav/leettest/issues/new?template=agent_feedback.yml&version=…&got=…`. Only
  `template`, `title` (starting `[агент] `) and field ids; values URL-encoded as UTF-8, newline `%0A`; a
  link too long gets 414. A list takes its option as the value: `platform` Linux / macOS / Windows /
  Другое; `where` Локально / CI / Контейнер; `kind` Непонятное сообщение или документация /
  Подозрительное число / Падение или зависание / Установка / Другое. The checkbox «Перед отправкой» is
  the person's to tick.
