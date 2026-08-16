# Upgrade and rollback

Use a verified release archive and rerun its installer with the same profile, agent user, and
approver user:

```sh
sudo ./install.sh \
  --profile grok \
  --approver-user "$USER" \
  --agent-bin /absolute/path/to/grok
```

The installer validates assets, sudoers, configuration, and the candidate binary before replacing
the active binary link. Binaries are stored by SHA-256. It restarts the daemon and waits for both
Unix sockets; if the candidate does not become ready, it restores and restarts the previous binary.
Approval leases are memory-only and are therefore revoked by every daemon restart.

Install state is root-owned at `/var/lib/rootbroker/install-state`. Do not edit it. Changing the
profile, agent identity, or approver identity requires uninstalling first so ownership and group
cleanup remain unambiguous.

To roll back after an otherwise successful upgrade, rerun `install.sh` from the older verified
archive. The candidate and installed rollback binary must both accept the rendered configuration;
the installer refuses the operation otherwise. Back up intentional edits to
`/etc/rootbroker/config.json` before an upgrade because the current alpha installer renders its managed
configuration again.

If an install reports failure, first verify that `systemctl status rootbrokerd` shows the restored
version as active. Do not manually repoint `/usr/local/libexec/rootbroker-bin`; preserve the failure
output and follow [TROUBLESHOOTING.md](TROUBLESHOOTING.md).
