# What is call chaining and why does it matter?

[← Back to docs](README.md)

> Translated from [docs/ru/chaining.md](../ru/chaining.md) at feb2d1d, 2026-11-09, and updated
> with it. If they differ, the Russian one is right.

> **Status: works since v0.9 (`scenario`, `steps` and `extract`, see the
> [reference](reference.md#scenario-steps-after-a-call)).** The pools this page used to describe
> are replaced by it: steps after a call, and a field of a reply passed into the next step.

Half the methods of a real service cannot be called with static data. To load money transfers
you need existing wallets: `Transfer` needs `from` and `to`, and those only come from
`CreateWallet` replies.

The usual way out is to prepare test data in advance. Where data can be prepared, that is the
right path, and substitution from a file is there for it: a CSV or JSON
[dataset](reference.md#a-different-request-each-call-a-dataset), one row per request. But not
everything can be prepared: one-time tokens, sessions, load on the creation itself. And a thousand
transfers between the same two wallets are not load on transfers, they are load on one database row
and its lock.

The second way out is a script that creates a wallet and transfers from it right away. Then you
measure the sequence "create plus transfer", not a single transfer; the share of each step cannot
be taken from the combined latency unless the script times the steps separately.

We solve it with a scenario. A call and the steps after it are one iteration: it goes in order, and
each step stays a separate line in the report with its own percentiles. The call or a step names
fields of its reply (`extract`), and the next step puts them into its request as
`{{extract.<name>}}`. The values live in one iteration: each iteration has its own wallet.

```yaml
load:
  calls:
    - method: wallet.v1.WalletService/CreateWallet
      rps: 5
      duration: 1m
      templates: true
      scenario: create-and-pay
      extract:
        wallet: wallet_id
      steps:
        - method: wallet.v1.WalletService/Transfer
          templates: true
          data:
            from: "{{extract.wallet}}"
            to: "{{uuid}}"
            amount: 100
```

You don't touch someone else's `.proto`: the path to the field is checked at start against
reflection data (or `app.proto` / `app.protoset`) — a missing field is a clear error naming the
path before the run, not garbage replies in the middle of the load. The same schema decides whether
the value fits the field of the next step: an `int64` into an `int32` is refused, and nothing is cut
silently.

What happens when the reply lacks the value. Sending an empty one quietly is the worst option: the
run would stop matching the stated plan without it being visible. So such a reply is a
`bad response`: the reply came and the client cannot use it. The iteration ends there, the step is
"failed here", the later steps are not reached, and the code stays the `OK` the target sent. If
every call of a method does this, the run is invalid, and the report says the replies lack the
value.

This is not a pool of values shared between different calls: values do not pass from one iteration
to another and do not pile up. For shared data prepared in advance there is the dataset.

The details — field types, the text of a value, the rules of substitution — are in the
[reference](reference.md#extract-a-value-from-an-earlier-reply). A runnable example for the
reference stand is [`examples/scenario-extract.yaml`](../../examples/scenario-extract.yaml).
