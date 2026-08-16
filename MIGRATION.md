# Migrating from the private pre-alpha name

Before its first public release, this project used the name `hostctl`. That name conflicts with an
established project, so `rootbroker` deliberately uses new commands, service names, groups,
sockets, configuration paths, managed Grok assets, and ACL metadata. It does not install legacy
aliases.

This migration applies only to private pre-alpha installations. Close active `grok-safe` sessions
and stop `hostctl-admin watch` first.

Later private builds installed a root-owned uninstaller. Use it when present:

```sh
sudo /usr/local/sbin/hostctl-uninstall
```

The earliest private builds were stateless and did not install that command. For those builds, use
the migration tool from a trusted rootbroker archive or checkout. Its check mode does not mutate the
host:

```sh
sudo ./migrate-private-prealpha.sh --approver-user "$USER" --check
sudo ./migrate-private-prealpha.sh --approver-user "$USER"
```

The tool accepts only the known stateless `hostctl 0.2.0-dev*` layout. Before changing anything it
checks the binary version, root ownership and modes, exact symlink targets, service command,
sudoers rule, configured identities, group membership, Grok markers, and absence of running agent
processes. Any unexpected state is a hard failure. If rootbroker was installed as a Debian or
Linuxbrew carrier package, the equivalent command is `rootbroker-migrate-private-prealpha`.

Both paths revoke optional approver-home ACL access and remove the old service, groups, sockets,
sudoers entry, and managed Grok assets. They preserve the `grok-agent` account, its home directory,
and Grok authentication data. Do not use `--purge-agent-account` during migration.

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
