**English** | [简体中文](README.zh-CN.md)

# hostctl

`hostctl` is a small Linux broker for letting a local AI agent request arbitrary root commands
without giving the agent `sudo`, Docker, LXD, or another persistent path to root. Every escalation
is denied by default and waits for a human decision on a separate Unix socket.

The initial integration targets Grok Build. Agent hooks are normalized through an adapter before
they reach the broker, while approval behavior is selected through a separate decision-provider
interface. These are internal Go extension points in the alpha release, not yet a stable or
dynamically loaded plugin API.

> **Alpha:** Use this on a test host first. An approved arbitrary root command is still an approved
> arbitrary root command. The design reduces unattended privilege, not the consequences of a bad
> human approval.

Release gates and planned hardening work are tracked in [ROADMAP.md](ROADMAP.md).
Operational documentation: [compatibility](COMPATIBILITY.md), [upgrade/rollback](UPGRADE.md),
[uninstall](UNINSTALL.md), [troubleshooting](TROUBLESHOOTING.md),
[support](SUPPORT.md), [threat model](THREAT_MODEL.md), and [contributing](CONTRIBUTING.md).

## How it works

```text
Grok hook payload ─> Grok adapter ─> normalized lifecycle ─┐
agent: hostctl sudo -- argv ─> normalized command request ─┼─> broker
                                                          │      │
human ─> admin Unix socket ─> ManualProvider.Reviewer ─────┘      │
                                                     Provider.Decide()
                                                                │
                                                        lease + exec(argv)
```

The request socket is accessible only to the dedicated agent account. The admin socket is
accessible only to the approver group. `hostctld` also verifies Linux peer credentials and binds
each request to the process identity (`uid`, PID, `/proc` start time), Grok session, and active turn
reported by hooks. The ancestor must be the exact configured root-owned Grok executable, not merely
a process with a matching name.

Hooks improve lifecycle accuracy and teach Grok the workflow, but hooks fail open in Grok and are
**not** the security boundary. The dedicated OS user, socket permissions, peer-credential checks,
and broker's default-deny state are the boundary.

### Extension boundaries

The implementation now has three explicit seams:

- `agent.HookAdapter` converts vendor hook fields into four normalized lifecycle events and extracts
  shell commands for integration-side guardrails. Grok fields exist only in
  `internal/integrations/grok`; neither the server nor broker parses them.
- `approval.Provider` receives a normalized request through `Decide(ctx, request)`. The shipped
  `ManualProvider` waits for a human and also implements the optional `Reviewer` interface used by
  the admin socket. A non-interactive provider does not need to expose a review queue. In every
  case, the broker validates the returned decision, records its provider/principal identity, and
  retains ownership of leases and execution.
- The core installer owns host accounts/groups, the hostctl binary, daemon configuration, systemd,
  and optional home ACLs. An allowlisted, versioned integration profile owns the agent executable,
  launcher, hooks, managed assets, and profile-specific sudoers rule. Grok is the first profile in
  `profiles/grok`.

Agent adapters and decision providers are currently compile-time extension points, while installer
profiles are root-executed shell code selected from a built-in allowlist. The Unix sockets remain the
only implemented transport. Making transport replaceable and deciding whether to stabilize public
extension APIs remain roadmap work. Any added profile, provider, or transport must be opt-in,
auditable, and document its own trust model instead of silently inheriting the security claims of
the default Grok/local-human mode.

## Approval scopes

- `command`: approve only the exact resolved executable, argv, cwd, timeout, and request hash.
- `message`: also approve subsequent requests during the current user prompt and Grok response.
- `session`: also approve subsequent requests in the current Grok process and conversation.

Message approval ends when the turn ends, a new prompt starts, the process exits, it is revoked, or
its TTL expires. Session approval ends on session/process exit, revocation, daemon restart, or TTL.
All state is memory-only, so restarting `hostctld` revokes every approval.

## Requirements

- Linux with systemd and `SO_PEERCRED`
- Grok Build with hook support
- root access for installation
- a human account that is allowed to approve host commands

The optional full-home access mode additionally requires a local filesystem with Linux POSIX ACL
and extended-attribute support.

The release binary is statically linked. The target host does not need Go, Python, or third-party
packages.

## Install

Download the archive for `linux_amd64` or `linux_arm64` from GitHub Releases, verify it against
`checksums.txt`, extract it, and review `install.sh` plus `profiles/grok/profile.sh`. Then select the
Grok profile and pass the real human account and existing Grok executable explicitly:

```sh
sudo ./install.sh \
  --profile grok \
  --approver-user "$USER" \
  --agent-bin /absolute/path/to/grok
```

For upgrades, the previous `--grok-bin PATH` form remains a compatibility alias for
`--profile grok --agent-bin PATH`.

The installer:

- creates locked-down `grok-agent`, `hostctl-agent`, and `hostctl-approver` identities/groups;
- installs one statically linked Go binary and root-owned multicall links;
- invokes the allowlisted Grok profile to install the supplied agent binary, launcher, managed hooks,
  rules, `hostctl-admin` skill, and narrowly scoped unprivileged-user sudoers rule;
- installs `hostctl-uninstall` as a root-owned maintenance entry point;
- enables `hostctld.service`.

It does **not** give the agent account sudo rights. Do not add that account to `sudo`, `docker`,
`lxd`, `disk`, or similar privileged groups.

`grok-safe` preserves only the standard HTTP/HTTPS/ALL/NO proxy variables, in upper- and lowercase,
when switching users. This supports hosts whose outbound network requires the human shell's proxy.
It does not preserve API keys or the rest of the caller environment.

The agent account has separate Grok state. On the first launch, authenticate it normally; do not
copy another user's login tokens into its home.

Rerun a verified newer archive to upgrade. The installer keeps content-addressed binaries and
automatically restores the previous binary if the new daemon does not become ready. See
[UPGRADE.md](UPGRADE.md) for rollback behavior. To remove hostctl while preserving agent-home data:

```sh
sudo hostctl-uninstall
```

Account-removal and ACL-recovery options are documented in [UNINSTALL.md](UNINSTALL.md).

### Optional access to the approver's home

By default, `grok-agent` cannot read or write the approver's home. If convenience is more important
than data isolation on a personal host, the approver can enable persistent read/write access at any
time after installation:

```sh
hostctl-admin home-access status
hostctl-admin home-access grant
hostctl-admin home-access revoke
```

The grant persists across reboots and does not require reinstalling hostctl. Direct admin calls
require the configured approver identity; the isolated agent account cannot call them by itself. As
a first-install convenience, `--allow-approver-home-rw` performs the same grant while running
`install.sh`. After a grant, however, home write access may give the agent ways to impersonate that
approver, as described below.

This keeps Grok under the separate unprivileged account and does not grant sudo or membership in the
approver's groups. The static hostctl daemon manages Linux POSIX ACLs directly through safely opened
file descriptors; no separate ACL package is required. It does not follow symbolic links or cross
filesystem boundaries, and it adds default ACLs so newly created content remains available to Grok.
Running `grok-safe` from the approver's home or one of its subdirectories then allows normal file
editing there.

Files explicitly created or later changed with a restrictive mode such as `0600` can mask inherited
ACL rights; run `grant` again to reconcile existing files. `status` reports `enabled` only after a
complete successful grant, `partial` for incomplete/top-level ACL state, and `disabled` otherwise.

This option deliberately lets Grok read sensitive files in that home, including SSH keys, shell
configuration, browser/application state, and credentials. It also lets Grok modify or delete the
approver's files. Root operations still require `hostctl`, but confidentiality and integrity of the
approver's home are no longer isolation boundaries.

More importantly, write access to files such as `.ssh/authorized_keys`, shell startup files, user
services, and executable search paths may let the agent impersonate the approver. In full-home mode,
human-only approval should be treated as a guard against mistakes, not a strong security boundary.
Use a dedicated shared directory instead when that boundary matters.

Revocation removes the named `grok-agent` access/default ACL entries throughout the home. It cannot
distinguish an identically named ACL entry that existed before hostctl.

## Use

Open a second SSH session or tmux pane and start the reviewer:

```sh
hostctl-admin watch
```

Then launch Grok in the isolated account:

```sh
grok-safe
```

When root is necessary, Grok is instructed to use direct argv form:

```sh
hostctl sudo -- mount /dev/disk/by-uuid/EXAMPLE /mnt/data
```

The reviewer displays the resolved command, quoted argv, cwd, timeout, risk hints, session/turn,
and SHA-256 request hash. Choose command, message, session, or deny. The command's stdout, stderr,
and exit status are returned to Grok.

Non-interactive reviewer commands are also available for a human-operated workflow:

```sh
hostctl-admin pending
hostctl-admin approve REQUEST_ID --scope command
hostctl-admin deny REQUEST_ID
hostctl-admin leases
hostctl-admin revoke LEASE_ID
```

`--json` emits stable JSON from `hostctl`, `pending`, `leases`, approval, denial, and revocation
commands.

To verify a binary and local installation without submitting a command:

```sh
hostctl --json doctor
```

### JSON contract

Success from `hostctl sudo --json -- ...` includes `ok`, `requestId` (null when a lease was reused),
`approvalScope`, `commandHash`, `exitCode`, `stdout`, `stderr`, timeout/duration fields, and output
truncation flags. Broker failures use this envelope:

```json
{"ok":false,"error":{"code":"denied","message":"request 0123456789abcdef was denied"}}
```

Reviewer collection commands return `{"ok":true,"pending":[...]}` or
`{"ok":true,"leases":[...]}`. Mutations return `{"ok":true}`. JSON errors never include proxy
values, credentials, or Grok authentication data.

## Configuration

The installed configuration is `/etc/hostctl/config.json`. Useful controls include:

- request, message, and session TTLs;
- maximum command runtime and captured-output size;
- allowed working-directory roots;
- exact agent and approver users;
- whether full argv is written to the journal (off by default).

After changing configuration, restart the daemon; this intentionally revokes existing approvals:

```sh
sudo systemctl restart hostctld
```

Audit events are available in the system journal:

```sh
journalctl -u hostctld
```

Full argv is not journaled by default because command lines can accidentally contain private data.
The executable path and command hash are logged.

## Development

Go 1.23 or newer is required to build from source. The only runtime Go module dependency is the
official `golang.org/x/sys` platform interface:

```sh
make test
make test-race
make vet
make snapshot VERSION=dev
# Linux only; exercises SO_PEERCRED and real Unix sockets:
make integration
```

The required quality gate also uses pinned versions of `deadcode`, `staticcheck`, `errcheck`,
`shellcheck`, and `actionlint`. After installing those tools, run:

```sh
make lint
```

`deadcode` is evaluated for the supported Linux build and must produce no output. The privileged
installer test changes users, groups, sudoers, systemd units, and `/usr/local` files, so it refuses
to run without an explicit mutation guard and should only be used in a disposable Linux machine:

```sh
sudo make system-test
```

The daemon is Linux-only because it treats kernel-supplied peer PID/UID/GID as part of its security
model. Pure logic tests and Linux cross-compilation run on macOS. Tagged releases build static
`linux-amd64` and `linux-arm64` archives with the current supported Go toolchain in GitHub Actions.
Each archive includes a CycloneDX SBOM; releases also publish SHA-256 checksums and GitHub build
provenance attestations. CI separately tests the minimum Go 1.23 source-build contract.

## Non-goals and limits

- This is not policy-based command allowlisting. The human may approve any direct executable.
- Message/session approval deliberately delegates more power than command approval for a limited
  context and time. Prefer command scope for destructive or difficult-to-review operations.
- A root command can change files after approval, spawn processes, or alter the machine. Review it
  exactly as carefully as a command typed after `sudo`.
- Approval binds argv metadata, not the contents of mutable files referenced by argv. Treat shell
  scripts, interpreters, package-manager hooks, and user-writable configuration as high risk.
- Root can read agent-owned secrets. Do not keep valuable credentials in the agent account.
- This project does not secure the human approver account. Malware already running as an approver
  can use the admin socket.

See [SECURITY.md](SECURITY.md) for the threat model and reporting guidance.
