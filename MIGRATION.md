# Migrating from the private pre-alpha name

Before its first public release, this project used the name `hostctl`. That name conflicts with an
established project, so `rootbroker` deliberately uses new commands, service names, groups,
sockets, configuration paths, managed Grok assets, and ACL metadata. It does not install legacy
aliases.

This migration applies only to private pre-alpha installations. Close active `grok-safe` sessions
and stop `hostctl-admin watch`, then remove the old installation using its root-owned uninstaller:

```sh
sudo /usr/local/sbin/hostctl-uninstall
```

The old uninstaller revokes optional approver-home ACL access and removes the old service, groups,
sockets, sudoers entry, and managed Grok assets. It preserves the `grok-agent` account and its home
directory. Do not use `--purge-agent-account` during migration.

Then install `rootbroker` from a verified archive or checkout:

```sh
sudo ./install.sh \
  --profile grok \
  --approver-user "$USER" \
  --agent-bin /absolute/path/to/grok \
  --rootbroker-bin /absolute/path/to/rootbroker
```

If the old installation had full access to the approver's home and that tradeoff is still desired,
add `--allow-approver-home-rw` to the installation command. The access is re-created with
`rootbroker`'s own tracked ACL metadata instead of retaining ambiguous legacy markers.

Verify that only the new installation remains:

```sh
systemctl is-active rootbrokerd
rootbroker version
rootbroker --json doctor
rootbroker-admin home-access status
```

The new installer refuses to proceed while the old install state or service is present. This avoids
running two root brokers with different sockets and approval planes.
