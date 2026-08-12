# hostctl

`hostctl` is a small Linux broker for letting a local AI agent request arbitrary root commands
without giving the agent `sudo`, Docker, LXD, or another persistent path to root. Every escalation
is denied by default and waits for a human decision on a separate Unix socket.

The initial integration targets Grok Build. The broker itself is agent-agnostic; an integration
only needs to report session/turn lifecycle events and invoke `hostctl sudo -- ...`.

> **Alpha:** Use this on a test host first. An approved arbitrary root command is still an approved
> arbitrary root command. The design reduces unattended privilege, not the consequences of a bad
> human approval.

## How it works

```text
unprivileged Grok process
  ├─ lifecycle hooks ───────────┐
  └─ hostctl sudo -- argv ─────┼─> /run/hostctl/request.sock
                               │             │
human in another SSH/tmux pane └─ hostctl-admin watch
                                      │
                                      └────> /run/hostctl/admin.sock
                                                   │
                                             root hostctld
                                                   │
                                             exec(argv), no shell
```

The request socket is accessible only to the dedicated agent account. The admin socket is
accessible only to the approver group. `hostctld` also verifies Linux peer credentials and binds
each request to the process identity (`uid`, PID, `/proc` start time), Grok session, and active turn
reported by hooks. The ancestor must be the exact configured root-owned Grok executable, not merely
a process with a matching name.

Hooks improve lifecycle accuracy and teach Grok the workflow, but hooks fail open in Grok and are
**not** the security boundary. The dedicated OS user, socket permissions, peer-credential checks,
and broker's default-deny state are the boundary.

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

The release binary is statically linked. The target host does not need Go, Python, or third-party
packages.

## Install

Download the archive for `linux_amd64` or `linux_arm64` from GitHub Releases, verify it against
`checksums.txt`, extract it, and review `install.sh`. Then pass the real human account and existing
Grok executable explicitly:

```sh
sudo ./install.sh \
  --approver-user "$USER" \
  --grok-bin /absolute/path/to/grok
```

The installer:

- creates locked-down `grok-agent`, `hostctl-agent`, and `hostctl-approver` identities/groups;
- installs one statically linked Go binary and root-owned multicall links;
- copies the supplied Grok binary to a root-owned location;
- installs root-managed Grok hooks, rules, and the `hostctl-admin` skill;
- grants approvers passwordless permission only to enter the unprivileged Grok account through a
  fixed launcher;
- enables `hostctld.service`.

It does **not** give the agent account sudo rights. Do not add that account to `sudo`, `docker`,
`lxd`, `disk`, or similar privileged groups.

`grok-safe` preserves only the standard HTTP/HTTPS/ALL/NO proxy variables, in upper- and lowercase,
when switching users. This supports hosts whose outbound network requires the human shell's proxy.
It does not preserve API keys or the rest of the caller environment.

The agent account has separate Grok state. On the first launch, authenticate it normally; do not
copy another user's login tokens into its home.

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

Go 1.23 or newer is required to build from source. The project has no third-party Go modules:

```sh
make test
make test-race
make vet
make snapshot VERSION=dev
# Linux only; exercises SO_PEERCRED and real Unix sockets:
make integration
```

The daemon is Linux-only because it treats kernel-supplied peer PID/UID/GID as part of its security
model. Pure logic tests and Linux cross-compilation run on macOS. Tagged releases build static
`linux-amd64` and `linux-arm64` archives in GitHub Actions and publish SHA-256 checksums.

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
