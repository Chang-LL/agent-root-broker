# Security

## Security model

This section describes the shipped `ManualProvider` over local Unix sockets. The provider interface
is an extension boundary, not a claim that other decision sources have an equivalent trust model.

`hostctl` assumes:

- the AI agent and anything it launches may be malicious;
- the dedicated agent user has no other route to host privilege;
- the root-owned broker, installed agent binary (currently Grok), launcher, configuration, and
  executable search path cannot be modified by the agent;
- the human approver account and kernel are trusted;
- the human reads the displayed command before approving it.

The request and admin sockets use separate Unix groups. The daemon verifies `SO_PEERCRED` instead
of trusting claimed identities in JSON. Agent requests must descend from the exact configured
root-owned configured agent executable, then match an active adapter-reported turn. Approval state
is memory-only and bound to the process start time so PID reuse does not inherit a lease.

Commands are resolved through a fixed PATH, require a root-owned non-writable executable and protected
parent directory chain by default, and execute as an argv array with a fixed environment, closed stdin,
bounded runtime, and bounded captured output. `sudo`, `su`, `pkexec`, and recursive hostctl executables
are rejected.

## Important limitations

Grok hooks are fail-open. They are lifecycle and usability signals, not a complete enforcement
mechanism. A missing lifecycle signal causes the broker to deny new escalation rather than grant it,
while process binding and TTLs constrain stale broad approvals.

Approval does not freeze the filesystem. For example, an approved interpreter may load mutable code,
or an approved package manager may execute package hooks. The reviewer UI flags common interpreters,
but it cannot make an arbitrary command safe.

A custom decision provider is trusted to authorize arbitrary root execution. The broker validates
the decision shape, binds it to a still-active turn, records the provider and principal, and retains
lease/execution control; it cannot determine whether the provider's policy was wise. Any non-default
provider needs its own authentication, failure-mode, audit, and threat-model review.

Do not expose either Unix socket through TCP, a container bind mount, or a group shared with unrelated
users. Do not run Grok in the approver account if the goal is isolation.

The optional `hostctl-admin home-access grant` command (and its installation shortcut
`--allow-approver-home-rw`) intentionally removes the approver home as a data-isolation boundary.
Direct admin calls require an authenticated approver; the isolated agent UID cannot use the admin
socket by itself. The agent remains a separate unprivileged UID, but it can read,
modify, replace, or delete accessible home files. That includes credentials and configuration which
may provide authority in other systems. Use this mode only when the agent is trusted with the entire
home directory. In particular, modifying SSH authorization, shell startup, user services, or similar
control files may let the agent impersonate the approver and reach the admin plane. Full-home mode
therefore downgrades human approval from a strong isolation boundary to a safety/intent check. ACL
traversal does not follow symlinks or cross filesystem boundaries.

The `grok-safe` sudo rule has `SETENV` because the wrapper uses sudo's explicit proxy-variable
allowlist. The target is the unprivileged agent account, not root, and the wrapper does not use broad
`sudo -E`. Treat proxy URLs as potentially sensitive and avoid embedding credentials in them.

## Reporting a vulnerability

Please report vulnerabilities privately through the repository's security-advisory feature. Include
the affected version, deployment assumptions, reproduction steps, and impact. Do not include real
credentials, hostnames, IP addresses, or production command output.
