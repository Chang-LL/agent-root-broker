# Troubleshooting

## The reviewer cannot connect

Open a new SSH login or shell after installation so the approver's new supplementary group is
present. Then check:

```sh
id
hostctl --json doctor
systemctl status hostctld
```

The request and admin sockets should exist under `/run/hostctl`. A daemon restart intentionally
clears every lease and active lifecycle state.

## Grok reports `no_active_turn`

The broker did not receive a valid session/prompt lifecycle sequence for that Grok process. Confirm
that the Grok profile's managed hooks are loaded, launch through `grok-safe`, and start a fresh user
prompt. Missing, duplicated, or out-of-order lifecycle input denies escalation rather than creating
authority.

## Login or device-code requests time out

`grok-safe` preserves only standard uppercase/lowercase HTTP, HTTPS, ALL, and NO proxy variables.
Verify those variables in the human shell before launching. API keys and arbitrary environment
variables are intentionally not copied to the isolated account.

## Approval succeeded but no lease is listed

Command scope authorizes exactly one execution and creates no reusable lease. Message scope ends at
the turn boundary. Session scope ends at session/process exit, explicit revoke, TTL, or daemon
restart.

## Home access is `partial`

A grant did not complete or ACL state changed afterward. Run `hostctl-admin home-access grant` again
to reconcile existing entries. Restrictive modes, unsupported xattrs/ACLs, filesystem boundaries,
and concurrent filesystem changes can affect the result. `revoke` is idempotent and removes the
agent UID's named ACL entries throughout the configured home.

## Upgrade failed

The installer waits for the daemon and both sockets, then automatically restores the previous
content-addressed binary if the candidate is not ready. Check `hostctl version` and
`systemctl status hostctld`. Keep the complete installer output; do not replace symlinks manually.

## Collecting logs

```sh
hostctl version
hostctl --json doctor
systemctl status hostctld
journalctl -u hostctld
```

Full argv is not journaled by default, but logs and local paths can still be sensitive. Redact them
before sharing and follow [SUPPORT.md](SUPPORT.md).
