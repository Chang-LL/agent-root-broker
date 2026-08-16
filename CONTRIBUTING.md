# Contributing

Thanks for helping improve rootbroker. This is security-sensitive Linux software: a small, reviewable
change with a clear invariant is preferred to a broad abstraction.

## Development setup

Use Go 1.25 or newer. The normal local loop is:

```sh
make test
make test-race
make vet
make snapshot VERSION=dev
```

`make lint` additionally requires the pinned tool versions declared in
`.github/workflows/ci.yml`. The CI result is authoritative for the supported Linux build.

The privileged test changes users, groups, sudoers, systemd units, `/run`, `/etc`, `/var/lib`, and
`/usr/local`. Run it only in a disposable Linux VM or CI runner:

```sh
sudo make system-test
```

## Change expectations

- Add tests for changed behavior, including a real Linux boundary test when mocks cannot exercise
  the security property.
- Preserve default denial for malformed, missing, stale, duplicated, and out-of-order input.
- Remove replaced helpers and paths in the same change; `deadcode` must remain empty.
- Keep agent-specific payloads inside their integration adapter/profile.
- Do not commit credentials, hostnames, IP addresses, command output, authentication state, or
  personal filesystem paths.
- Do not weaken socket, executable ownership, process ancestry, or account-isolation checks merely
  to make an integration easier.

AI-assisted contributions are welcome, but the contributor remains responsible for understanding
and testing every submitted change.

## Pull requests

Use a focused branch and describe the problem, trust-boundary impact, test evidence, compatibility
impact, and rollback behavior. Commit messages in this repository use short conventional prefixes
such as `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, and `ci:`.

Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md), not in an issue or
pull request.
