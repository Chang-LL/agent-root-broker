# Uninstall

The installer leaves a root-owned maintenance entry point, so the original archive or source
checkout is not required:

```sh
sudo rootbroker-uninstall
```

By default, uninstall revokes optional approver-home ACL access, disables the daemon, removes
rootbroker/profile files and memberships recorded in install state, and preserves the agent account
and its home.

To remove an agent account that rootbroker itself created:

```sh
sudo rootbroker-uninstall --purge-agent-account
```

The home directory is still preserved deliberately. The command refuses account removal while the
agent has running processes and refuses to remove an account not recorded as rootbroker-created.
Review and remove preserved data separately if desired.

Home-access revocation is fail-closed: if it cannot inspect and revoke ACLs, uninstall stops before
removing the maintenance binary. If the filesystem is permanently unavailable and residual ACLs
are understood and accepted, `--skip-home-access-revoke` bypasses that step with a warning.

The uninstaller removes only its marked block from `/etc/grok/managed_config.toml`; unrelated
content is preserved. Malformed or duplicated rootbroker markers cause it to stop for manual review.

If rootbroker was installed through apt or Linuxbrew, run the root uninstaller first and then remove
the carrier package:

```sh
sudo rootbroker-uninstall
sudo apt remove rootbroker
# or: brew uninstall rootbroker
```

The Debian package refuses removal while `/var/lib/rootbroker/install-state` shows an active
configuration. This prevents package removal from silently leaving an unmanaged root daemon.
