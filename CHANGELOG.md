# Changelog

All notable release changes will be documented here. The format follows Keep a Changelog and the
project intends to use Semantic Versioning after the public alpha contract is defined.

## Unreleased

## [0.1.0-alpha.2] - 2026-08-16

### Fixed

- Added a fail-closed migration tool and privileged system test for the earliest stateless private
  pre-alpha installations, which predated the root-owned `hostctl-uninstall` command.
- Preserve semantic prerelease separators in future Debian asset filenames and mark prerelease tags
  correctly in the GitHub release workflow.

## [0.1.0-alpha.1] - 2026-08-16

### Changed

- Renamed the private pre-alpha project and every installed identity/path from `hostctl` to
  `rootbroker` before public release. Legacy command aliases are intentionally not installed.

### Added

- Local human approval with command, message, and session scopes.
- Dedicated Grok integration profile with lifecycle hooks and agent guidance.
- Optional, explicit approver-home POSIX ACL access.
- Deterministic install, upgrade readiness checks, automatic binary rollback, and complete
  uninstallation with preserved agent-home data.
- Linux root-level tests for account isolation, sudoers, systemd, approval lifecycle, timeout,
  revoke, daemon restart, ACL behavior, and uninstall.
- Agent-adapter, decision-provider, installer-profile, and authenticated-transport extension
  boundaries without shipping unattended approval or remote transport.
- Reproducible apt-installable Debian packages and a generated Linuxbrew tap formula, both using an
  explicit setup step rather than configuring root authority during package installation.
- Checksums, CycloneDX SBOMs, GitHub build provenance, bilingual usage documentation, and an
  explicit threat model.

[Unreleased]: https://github.com/Chang-LL/rootbroker/compare/v0.1.0-alpha.2...HEAD
[0.1.0-alpha.2]: https://github.com/Chang-LL/rootbroker/compare/v0.1.0-alpha.1...v0.1.0-alpha.2
[0.1.0-alpha.1]: https://github.com/Chang-LL/rootbroker/releases/tag/v0.1.0-alpha.1
