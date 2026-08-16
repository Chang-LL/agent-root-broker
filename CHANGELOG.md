# Changelog

All notable release changes will be documented here. The format follows Keep a Changelog and the
project intends to use Semantic Versioning after the public alpha contract is defined.

## Unreleased

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
