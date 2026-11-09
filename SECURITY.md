# Security policy

## Reporting a vulnerability

Report it privately: open the [Security tab](https://github.com/yhgrwav/leettest/security) of this
repository and choose "Report a vulnerability". Do not open a public issue or pull request for it.

Please include the version (`leettest -version`), the input that triggers it (a config, a `.proto`
or protoset file, a dataset) with any secrets removed, and what you saw.

We aim to acknowledge a report within 7 days and to tell you what we decided after that. This is
an intention, not a guarantee: the project is maintained by one person.

## Supported versions

The latest minor version of the latest major version. A fix goes into the next release of it;
older versions get none.

## What counts as a vulnerability

- The CLI or the library crashes, hangs or runs out of memory on untrusted input: a config, a
  `.proto` or protoset file, a dataset file, a reply from the target.
- It prints or sends a secret it should not: a value from `${ENV}` or from `metadata` in the
  report, the live view, an error or a file.
- It reads or writes a file outside the places the config names.

## What does not

- LeetTest generating load. That is its purpose: run it only against a service you are allowed to
  test.
- A wrong or imprecise number in a report. That is a bug: open a normal issue.
- A problem in a dependency that LeetTest does not reach. Report it to that project.
