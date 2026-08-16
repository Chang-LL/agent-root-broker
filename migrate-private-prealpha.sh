#!/bin/sh
set -eu

APPROVER_USER=
AGENT_USER=grok-agent
CHECK_ONLY=0
TMP_DIR=

usage() {
  echo "Usage: sudo ./migrate-private-prealpha.sh --approver-user USER [--agent-user USER] [--check]" >&2
  echo "Removes the stateless private hostctl pre-alpha while preserving the agent account and home." >&2
}

fail() {
  echo "migrate-private-prealpha: $*" >&2
  exit 1
}

valid_user() {
  printf '%s\n' "$1" | /bin/grep -Eq '^[a-z_][a-z0-9_-]*[$]?$'
}

secure_root_file() {
  migration_path=$1
  [ -f "$migration_path" ] && [ ! -L "$migration_path" ] || return 1
  [ "$(/usr/bin/stat -c '%u' "$migration_path")" -eq 0 ] || return 1
  [ -z "$(/usr/bin/find "$migration_path" -maxdepth 0 -perm /022 -print -quit)" ]
}

secure_owned_dir() {
  migration_path=$1
  migration_uid=$2
  [ -d "$migration_path" ] && [ ! -L "$migration_path" ] || return 1
  [ "$(/usr/bin/stat -c '%u' "$migration_path")" -eq "$migration_uid" ] || return 1
  [ -z "$(/usr/bin/find "$migration_path" -maxdepth 0 -perm /022 -print -quit)" ]
}

secure_root_dir() {
  secure_owned_dir "$1" 0
}

exact_symlink() {
  migration_path=$1
  migration_target=$2
  [ -L "$migration_path" ] && [ "$(/usr/bin/readlink -- "$migration_path")" = "$migration_target" ]
}

group_has_only() {
  migration_group=$1
  migration_user=$2
  migration_members=$(/usr/bin/getent group "$migration_group" | /usr/bin/cut -d: -f4)
  case "$migration_members" in
    ""|"$migration_user") return 0 ;;
    *) return 1 ;;
  esac
}

validate_managed_config() {
  migration_config=$1
  /usr/bin/awk '
    $0 == "# BEGIN hostctl managed hooks" {
      if (inside || seen) exit 1
      inside = 1
      seen = 1
      next
    }
    $0 == "# END hostctl managed hooks" {
      if (!inside) exit 1
      inside = 0
      next
    }
    END { if (inside) exit 1 }
  ' "$migration_config"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --approver-user)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      APPROVER_USER=$2
      shift 2
      ;;
    --agent-user)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      AGENT_USER=$2
      shift 2
      ;;
    --check)
      CHECK_ONLY=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

[ "$(/usr/bin/id -u)" -eq 0 ] || fail "must run as root"
[ -n "$APPROVER_USER" ] || { usage; exit 2; }
valid_user "$APPROVER_USER" || fail "invalid approver user"
valid_user "$AGENT_USER" || fail "invalid agent user"
/usr/bin/getent passwd "$APPROVER_USER" >/dev/null || fail "approver user does not exist"
/usr/bin/getent passwd "$AGENT_USER" >/dev/null || fail "agent user does not exist"
[ "$(/usr/bin/id -u "$APPROVER_USER")" -ne 0 ] || fail "approver user must not be root"
[ "$(/usr/bin/id -u "$AGENT_USER")" -ne 0 ] || fail "agent user must not be root"

if [ -f /var/lib/hostctl/install-state ] || [ -x /usr/local/sbin/hostctl-uninstall ]; then
  fail "stateful private install detected; use sudo /usr/local/sbin/hostctl-uninstall"
fi

secure_root_file /usr/local/libexec/hostctl-bin || fail "legacy broker binary is missing or has unsafe ownership/mode"
LEGACY_VERSION=$(/usr/local/libexec/hostctl-bin version) || fail "legacy broker binary cannot run"
case "$LEGACY_VERSION" in
  "hostctl 0.2.0-dev"*) ;;
  *) fail "unsupported legacy broker version: $LEGACY_VERSION" ;;
esac

exact_symlink /usr/local/bin/hostctl /usr/local/libexec/hostctl-bin || fail "unexpected /usr/local/bin/hostctl"
exact_symlink /usr/local/bin/hostctl-admin /usr/local/libexec/hostctl-bin || fail "unexpected /usr/local/bin/hostctl-admin"
exact_symlink /usr/local/sbin/hostctld /usr/local/libexec/hostctl-bin || fail "unexpected /usr/local/sbin/hostctld"
exact_symlink /usr/local/libexec/hostctl-grok-hook /usr/local/libexec/hostctl-bin || fail "unexpected hostctl Grok hook"

secure_root_file /etc/hostctl/config.json || fail "legacy config is missing or has unsafe ownership/mode"
/bin/grep -Fqx "  \"agent_users\": [\"$AGENT_USER\"]," /etc/hostctl/config.json || fail "legacy config agent does not match"
/bin/grep -Fqx "  \"approver_users\": [\"$APPROVER_USER\"]," /etc/hostctl/config.json || fail "legacy config approver does not match"
/bin/grep -Fqx '  "request_group": "hostctl-agent",' /etc/hostctl/config.json || fail "unexpected legacy request group"
/bin/grep -Fqx '  "admin_group": "hostctl-approver",' /etc/hostctl/config.json || fail "unexpected legacy approver group"

secure_root_file /etc/systemd/system/hostctld.service || fail "legacy service is missing or has unsafe ownership/mode"
/bin/grep -Fqx 'ExecStart=/usr/local/sbin/hostctld --config /etc/hostctl/config.json' /etc/systemd/system/hostctld.service || fail "unexpected legacy service command"
secure_root_file /etc/sudoers.d/hostctl-grok-agent || fail "legacy sudoers entry is missing or has unsafe ownership/mode"
/bin/grep -Fqx "%hostctl-approver ALL=($AGENT_USER) NOPASSWD: SETENV: /usr/local/libexec/grok-agent-launch *" /etc/sudoers.d/hostctl-grok-agent || fail "unexpected legacy sudoers rule"

/usr/bin/getent group hostctl-agent >/dev/null || fail "legacy request group is missing"
/usr/bin/getent group hostctl-approver >/dev/null || fail "legacy approver group is missing"
group_has_only hostctl-agent "$AGENT_USER" || fail "legacy request group contains an unexpected member"
group_has_only hostctl-approver "$APPROVER_USER" || fail "legacy approver group contains an unexpected member"
/usr/bin/id -nG "$AGENT_USER" | /bin/grep -qw hostctl-agent || fail "agent is not a legacy request-group member"
/usr/bin/id -nG "$APPROVER_USER" | /bin/grep -qw hostctl-approver || fail "approver is not a legacy approver-group member"

AGENT_HOME=$(/usr/bin/getent passwd "$AGENT_USER" | /usr/bin/cut -d: -f6)
case "$AGENT_HOME" in
  /*) [ "$AGENT_HOME" != / ] || fail "unsafe agent home" ;;
  *) fail "agent home is not absolute" ;;
esac
AGENT_UID=$(/usr/bin/id -u "$AGENT_USER")
secure_owned_dir "$AGENT_HOME" "$AGENT_UID" || fail "agent home has unsafe ownership, mode, or type"
secure_owned_dir "$AGENT_HOME/.grok" "$AGENT_UID" || fail "agent Grok directory has unsafe ownership, mode, or type"
secure_root_dir "$AGENT_HOME/.grok/hooks" || fail "agent hook directory has unsafe ownership, mode, or type"
secure_owned_dir "$AGENT_HOME/.grok/skills" "$AGENT_UID" || fail "agent skill directory has unsafe ownership, mode, or type"

for migration_file in \
  /usr/local/bin/grok-safe \
  /usr/local/libexec/grok-agent-launch \
  /usr/local/libexec/grok-hostctl-bin; do
  secure_root_file "$migration_file" || fail "legacy managed file is missing or unsafe: $migration_file"
done
secure_root_dir /usr/local/share/hostctl || fail "legacy share directory is missing or unsafe"

if [ -e "$AGENT_HOME/.grok/hooks/hostctl.json" ]; then
  secure_root_file "$AGENT_HOME/.grok/hooks/hostctl.json" || fail "legacy agent hook is not a root-owned regular file"
fi
if [ -e "$AGENT_HOME/.grok/skills/hostctl-admin" ] || [ -L "$AGENT_HOME/.grok/skills/hostctl-admin" ]; then
  exact_symlink "$AGENT_HOME/.grok/skills/hostctl-admin" /usr/local/share/hostctl/grok/hostctl-admin || fail "legacy agent skill is not the managed symlink"
fi
if [ -f /etc/grok/managed_config.toml ]; then
  secure_root_file /etc/grok/managed_config.toml || fail "managed Grok config has unsafe ownership/mode"
  validate_managed_config /etc/grok/managed_config.toml || fail "managed Grok config contains malformed hostctl markers"
fi

if [ -x /usr/bin/pgrep ] && /usr/bin/pgrep -u "$AGENT_USER" >/dev/null 2>&1; then
  fail "agent account still has running processes; close Grok before migrating"
fi
/usr/bin/systemctl is-active --quiet hostctld.service || fail "legacy hostctld service is not active; start it so home access can be revoked"

if [ "$CHECK_ONLY" -eq 1 ]; then
  echo "Stateless private pre-alpha is recognized and ready to remove: $LEGACY_VERSION"
  echo "The agent account and home will be preserved: $AGENT_USER ($AGENT_HOME)"
  exit 0
fi

TMP_DIR=$(/usr/bin/mktemp -d)
trap '/bin/rm -rf -- "$TMP_DIR"' EXIT HUP INT TERM

/usr/bin/sudo -u "$APPROVER_USER" -H -- /usr/local/bin/hostctl-admin home-access revoke
/usr/bin/systemctl disable --now hostctld.service >/dev/null

if [ -f /etc/grok/managed_config.toml ]; then
  /usr/bin/awk '
    $0 == "# BEGIN hostctl managed hooks" { managed = 1; next }
    $0 == "# END hostctl managed hooks" { managed = 0; next }
    !managed { print }
  ' /etc/grok/managed_config.toml >"$TMP_DIR/managed_config.toml"
  if /bin/grep -q '[^[:space:]]' "$TMP_DIR/managed_config.toml"; then
    /usr/bin/install -o root -g root -m 0644 "$TMP_DIR/managed_config.toml" /etc/grok/managed_config.toml
  else
    /bin/rm -f -- /etc/grok/managed_config.toml
  fi
fi

/bin/rm -f -- \
  /etc/sudoers.d/hostctl-grok-agent \
  /etc/systemd/system/hostctld.service \
  /etc/hostctl/config.json \
  /usr/local/bin/grok-safe \
  /usr/local/bin/hostctl \
  /usr/local/bin/hostctl-admin \
  /usr/local/sbin/hostctld \
  /usr/local/libexec/hostctl-bin \
  /usr/local/libexec/hostctl-grok-hook \
  /usr/local/libexec/grok-agent-launch \
  /usr/local/libexec/grok-hostctl-bin \
  "$AGENT_HOME/.grok/hooks/hostctl.json" \
  "$AGENT_HOME/.grok/skills/hostctl-admin"
/bin/rm -rf -- /usr/local/share/hostctl
/bin/rm -f -- /run/hostctl/request.sock /run/hostctl/admin.sock

/usr/sbin/gpasswd -d "$APPROVER_USER" hostctl-approver >/dev/null
/usr/sbin/gpasswd -d "$AGENT_USER" hostctl-agent >/dev/null
/usr/sbin/groupdel hostctl-approver
/usr/sbin/groupdel hostctl-agent

/bin/rmdir /run/hostctl /etc/hostctl /var/lib/hostctl /etc/grok /usr/local/libexec 2>/dev/null || true
/usr/bin/systemctl daemon-reload
/usr/sbin/visudo -cf /etc/sudoers >/dev/null

echo "Stateless private hostctl pre-alpha removed."
echo "Preserved agent account and home: $AGENT_USER ($AGENT_HOME)"
echo "Install rootbroker next; add --allow-approver-home-rw only if you accept restoring full-home access."
