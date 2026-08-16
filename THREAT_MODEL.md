# Threat model

This model covers the shipped Grok profile, `ManualProvider`, and local Unix-socket transport. New
agent adapters, decision providers, transports, or sharing modes require their own review.

## Assets and security goals

- Root execution occurs only after a valid decision for a current request or lease.
- The agent cannot reach the admin plane or create/extend its own approval.
- Approval scope cannot survive its documented turn, session, process, TTL, revoke, or daemon
  boundary.
- Configuration, installed executables, hooks, and audit identity cannot be modified by the agent.
- Denial, malformed input, missing lifecycle state, and partial failure do not grant authority.

## Trust boundaries

```text
untrusted agent UID ── request socket ──> root hostctld ── exec ──> host
       │                                      ▲
       └── lifecycle hook adapter ────────────┤
                                              │
trusted approver UID ── admin socket ─────────┘
```

The agent UID and all of its processes are untrusted. The root daemon, kernel, root-owned installed
files, and human approver account are trusted. The default model assumes the approver actually
reviews the displayed request. Existing root compromise, kernel compromise, approver-account
compromise, hardware attacks, and a malicious approved command are outside the protection claim.

## Principal abuse paths

| Abuse path | Current controls | Residual risk |
| --- | --- | --- |
| Agent calls `sudo`, `su`, `pkexec`, or hostctl recursively | Separate UID, no privileged group, executable denylist, system test | Another host-installed privilege path would invalidate the deployment assumption. |
| Agent connects to the admin plane | Separate socket group plus kernel `SO_PEERCRED` and configured identity checks | Malware already running as the approver can decide requests. |
| Agent forges lifecycle JSON | Peer/process ancestry binding, normalized adapter, session/turn state machine, default denial | Hooks are fail-open usability signals; a compromised installed agent binary is trusted by this model. |
| PID reuse or a restarted process inherits authority | PID plus `/proc` start-time identity; in-memory leases; liveness checks | Kernel or `/proc` compromise is out of scope. |
| Duplicate, missing, delayed, or reordered hooks widen a lease | Explicit lifecycle sequencing; ambiguous active-turn starts revoke message state and fail closed | Upstream hook behavior can cause denial and reduced usability. |
| Approved argv references mutable code/configuration | Direct argv execution, protected executable chain, risk hints | Script contents, package hooks, device paths, and other operands are not frozen. Human review remains essential. |
| Candidate upgrade leaves a broken privileged service | Prevalidation, content-addressed objects, socket readiness check, automatic binary rollback | Power loss can still leave a partial set of non-daemon managed files; rerunning the installer is the recovery path. |
| Home sharing exposes approver authority | Explicit opt-in, prominent warning, ACL status/revoke, no symlink/filesystem traversal | Full-home write access can enable approver impersonation and weakens the human boundary by design. |
| Secrets leak through logs/output | Bounded output; argv logging disabled by default; stable redacted error shapes | Approved commands can intentionally read secrets and return them to the agent. |
| Release or installer is replaced | SHA-256 release checksums, immutable Action pins, GitHub build attestation, root-owned installation | Users must verify the downloaded archive and repository identity; checksums from the same compromised channel are insufficient alone. |

## Validation

CI runs unit/race/vet/static analysis, real Linux sockets and peer credentials, root POSIX ACL tests,
cross-builds, and a disposable-host test covering users/groups, sudoers, systemd, direct-sudo denial,
all approval scopes, timeout, revoke, daemon restart, lifecycle failure, home ACL behavior, failed
upgrade rollback, and uninstall.

Security assumptions and reporting policy are in [SECURITY.md](SECURITY.md). Compatibility limits
are in [COMPATIBILITY.md](COMPATIBILITY.md).
