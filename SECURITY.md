# Security Policy

## Supported versions

Specter is pre-1.0. Security fixes are applied to the latest release and to
`main`. Older tagged releases are not patched — upgrade to the latest version.

## Reporting a vulnerability

**Do not open a public issue for a security vulnerability.**

Report it privately using GitHub's
[private vulnerability reporting](https://github.com/bakhod1r/spector/security/advisories/new),
or by email to **bakhodiryashinmansur@gmail.com**.

Please include:

- a description of the issue and its impact,
- the smallest input or steps that reproduce it,
- the version or commit affected.

You can expect an acknowledgement within a few days. Once the issue is
confirmed and a fix is prepared, a release and an advisory are published, and
your contribution is credited unless you prefer otherwise.

## Scope notes

Specter generates documentation and serves a developer console. A few things
are intentional rather than vulnerabilities:

- The console's `AccessKey` is a deployment secret, not user authentication —
  there are no accounts, expiry, or per-user revocation. Keep the console off
  an internet-facing deployment.
- The mock and verifying-proxy modes are development tools. The mock is shape,
  not state, and open CORS is the default unless restricted with `-mock-origin`.

Reports about these documented behaviours are welcome as design feedback, but
are triaged as such rather than as vulnerabilities.
