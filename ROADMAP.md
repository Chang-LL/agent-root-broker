**English** | [简体中文](ROADMAP.zh-CN.md)

# Roadmap

`hostctl` is alpha software that places a root daemon between an untrusted local AI agent and the
host. Its roadmap is ordered by confidence in that boundary, not by feature count. Milestones have
exit criteria instead of target dates.

## Engineering principles

- Test the real Linux boundary. Mocks are useful for logic, but they do not replace tests involving
  users, groups, sudoers, systemd, Unix sockets, process credentials, and POSIX ACLs.
- Default to denial. Missing hooks, stale state, malformed input, and partial operations must not
  create authority.
- Keep the implementation small. Production builds should contain no known unreachable functions,
  and replaced paths should be removed in the same change.
- Prefer explicit code over speculative abstraction. A helper should clarify an invariant, isolate
  a platform boundary, or remove meaningful duplication.
- Build releases in CI from a tagged commit and make their origin and contents verifiable.

## Current baseline

- A dedicated unprivileged agent account and separate request/approval sockets.
- Command-, message-, and session-scoped human approval with in-memory leases.
- Direct argv execution without a shell, with executable ownership checks, timeouts, and bounded
  output.
- A Grok Build integration with lifecycle hooks, managed rules, and an agent-facing skill.
- Optional, explicit POSIX ACL access to the approver's home.
- Race tests, Linux socket integration, root POSIX ACL integration, vet, formatting checks, and
  static cross-builds for Linux amd64 and arm64.

## Public alpha gate

The repository should remain private until these items are complete. The intended first public
version is `v0.1.0-alpha.1`.

### Deterministic installation and upgrade

- [x] Make binary selection unambiguous. A source checkout must not silently install an ignored or
  stale `dist/` artifact.
- [x] Print the selected binary path, embedded version, architecture, and checksum before changing
  the system.
- [x] Test first install, repeated install, and replacement by a distinct versioned artifact on a
  clean Linux host.
- [x] Test upgrade from a previous release-layout artifact; retain published release fixtures after
  the first public tag.
- [x] Document and test rollback and complete uninstallation, including users, groups, sudoers,
  sockets, service files, managed Grok files, and optional ACLs.
- [x] Fail safely when validation or daemon readiness fails, and keep interrupted installs
  rerunnable without granting root authority.
- [ ] Select a conflict-free public project, command, and package name before publishing to package
  registries; `hostctl` is already used by an established project.

### System-level verification

- [x] Run the installer in a disposable Linux system and verify systemd, account/group membership,
  file ownership/modes, sudoers syntax, and socket permissions.
- [x] Verify that the agent cannot invoke `sudo` directly but can submit an approved command through
  `hostctl`.
- [x] Exercise approve, deny, and a reused message lease through the installed system end to end.
- [x] Exercise timeout, revoke, daemon restart, and command/session lease boundaries through the
  installed system end to end.
- [x] Exercise Grok lifecycle integration, including missing, duplicated, delayed, and out-of-order
  hook events.
- [x] Exercise home-access grant, repeat grant, partial failure, status, revoke, default ACL
  inheritance, restrictive modes, symlinks, and filesystem boundaries.
- [x] Run privileged tests on every release, not only on pushes to `main`.

### Code health

- [x] Require `deadcode ./...` to report no unreachable production functions for the supported Linux
  build.
- [x] Add pinned, high-signal checks: `staticcheck`, `errcheck`, `shellcheck`, and `actionlint`.
- [x] Keep `gofmt`, `go vet`, and `go test -race ./...` as required checks.
- [x] Publish Linux coverage by package. Improve security-boundary coverage before imposing a global
  percentage gate; prioritize `server`, `proc`, `commands`, `broker`, `executor`, and `homeaccess`.
- [x] Review low-level ACL syscalls and platform code for a smaller maintained interface, including
  whether `golang.org/x/sys/unix` is a better tradeoff than raw `unsafe` syscalls.

### Release and repository hygiene

- [x] Make the release workflow run the same required checks as pull requests.
- [x] Pin third-party GitHub Actions to immutable commit SHAs.
- [x] Publish checksums, a CycloneDX SBOM, and build provenance from the tagged release workflow.
- [x] Scan the complete Git history for credentials, private host details, and unintended personal
  information before changing repository visibility.
- [ ] Enable protected `main`, private vulnerability reporting, secret scanning, and code scanning
  when the repository becomes public.
- [x] Add concise contributing, support, compatibility, upgrade, troubleshooting, and uninstall
  documentation.

### Public alpha exit criteria

- All required CI checks pass from a clean clone and on the release tag.
- A clean-host install, end-to-end approval, upgrade, revoke, and uninstall test passes without
  manual repair.
- No known path lets the isolated agent reach root or the admin plane without the documented human
  approval boundary.
- No known unreachable production functions or unexplained lint suppressions remain.
- Release archives can be traced to the tagged source and reproduce the documented version.
- Known limitations and the consequences of full-home access are prominent and accurate.

## Alpha hardening

After the first public alpha, prioritize field evidence over adding broad policy features:

- [x] Define normalized lifecycle events and an agent-adapter contract so vendor hook payloads do
  not enter the core broker.
- [x] Separate core host installation from agent-specific integration profiles; migrate Grok to the
  first profile before claiming support for another agent.
- [x] Define a decision-provider interface and migrate local human review to the default manual
  provider while keeping leases and execution in the broker.
- [ ] Extract the Unix-socket server behind a transport interface before adding authenticated remote
  reviewers; define separate trust and audit models for every non-default provider and transport.
- [ ] Add a directory-scoped sharing mode that is safer and cheaper than recursively granting an
  entire home directory.
- [ ] Define supported Linux distributions, filesystems, Grok versions, and an explicit compatibility
  matrix from CI results.
- [ ] Improve recovery from partial ACL changes and interrupted upgrades.
- [ ] Stabilize the JSON protocol and document compatibility guarantees.
- [ ] Add structured, privacy-preserving diagnostics for support reports.
- [ ] Collect real installation and threat-model feedback before expanding agent integrations.

## Toward a stable release

A `v1.0.0` release requires:

- a stable configuration, admin, and JSON compatibility policy;
- tested migrations and rollback across supported release lines;
- a maintained Linux/systemd compatibility matrix;
- an independent security review with resolved critical and high-severity findings;
- documented vulnerability response and supported-version policies; and
- evidence from public alpha use that approval scopes and lifecycle binding behave predictably.

## Current scope boundary

The alpha release wires the manual human provider to local Unix-socket transport. A compile-time
decision-provider interface now separates approval decisions from leases and execution, but no
unattended provider or alternative transport is shipped. Network transport and AI-generated policy
remain outside the current supported mode, not permanent architectural non-goals. Each future mode
needs explicit isolation, authentication, failure behavior, auditability, and threat models. No
provider should silently weaken or claim equivalence to the default human-approval boundary.
Command allowlisting may also be explored as a provider or an additional constraint rather than a
substitute for explicit approval.
