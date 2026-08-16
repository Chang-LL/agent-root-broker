#!/bin/sh
set -eu

[ "$(uname -s)" = Linux ] || { echo "SKIP: Linux required"; exit 0; }
[ "$(id -u)" -eq 0 ] || { echo "pre-alpha migration system test must run as root" >&2; exit 1; }
[ "${ROOTBROKER_SYSTEM_TEST_ALLOW_MUTATION:-}" = 1 ] || {
  echo "refusing to modify the host without ROOTBROKER_SYSTEM_TEST_ALLOW_MUTATION=1" >&2
  exit 1
}

PROJECT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TEST_DIR=$(/usr/bin/mktemp -d /tmp/rootbroker-prealpha-migration.XXXXXX)
/bin/chmod 0755 "$TEST_DIR"
APPROVER_USER=${ROOTBROKER_TEST_APPROVER_USER:-}
AGENT_USER=grok-agent
APPROVER_CREATED=0
AGENT_CREATED=0
AGENT_PID=

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

cleanup() {
  [ -z "$AGENT_PID" ] || /bin/kill "$AGENT_PID" 2>/dev/null || true
  /usr/bin/systemctl disable --now hostctld.service >/dev/null 2>&1 || true
  /bin/rm -f -- \
    /etc/sudoers.d/hostctl-grok-agent \
    /etc/systemd/system/hostctld.service \
    /usr/local/bin/grok-safe \
    /usr/local/bin/hostctl \
    /usr/local/bin/hostctl-admin \
    /usr/local/sbin/hostctld \
    /usr/local/libexec/hostctl-bin \
    /usr/local/libexec/hostctl-grok-hook \
    /usr/local/libexec/grok-agent-launch \
    /usr/local/libexec/grok-hostctl-bin
  /bin/rm -rf -- /etc/hostctl /run/hostctl /var/lib/hostctl /usr/local/share/hostctl
  /bin/rm -f -- /etc/grok/managed_config.toml
  /usr/sbin/gpasswd -d "$APPROVER_USER" hostctl-approver >/dev/null 2>&1 || true
  /usr/sbin/gpasswd -d "$AGENT_USER" hostctl-agent >/dev/null 2>&1 || true
  /usr/sbin/groupdel hostctl-approver >/dev/null 2>&1 || true
  /usr/sbin/groupdel hostctl-agent >/dev/null 2>&1 || true
  if [ "$AGENT_CREATED" -eq 1 ]; then
    /usr/sbin/userdel -r "$AGENT_USER" >/dev/null 2>&1 || true
  fi
  if [ "$APPROVER_CREATED" -eq 1 ]; then
    /usr/sbin/userdel -r "$APPROVER_USER" >/dev/null 2>&1 || true
  fi
  /bin/rmdir /etc/grok /usr/local/libexec 2>/dev/null || true
  /usr/bin/systemctl daemon-reload >/dev/null 2>&1 || true
  /bin/rm -f -- /tmp/rootbroker-prealpha-revoke-called
  /bin/rm -rf -- "$TEST_DIR"
}
trap cleanup EXIT HUP INT TERM

[ -n "$APPROVER_USER" ] || fail "ROOTBROKER_TEST_APPROVER_USER is required"
for path in \
  /etc/hostctl \
  /run/hostctl \
  /var/lib/hostctl \
  /etc/systemd/system/hostctld.service \
  /usr/local/bin/hostctl \
  /usr/local/bin/hostctl-admin \
  /usr/local/sbin/hostctld \
  /usr/local/libexec/hostctl-bin \
  /usr/local/share/hostctl; do
  [ ! -e "$path" ] || fail "refusing to replace pre-existing path: $path"
done
for group in hostctl-agent hostctl-approver; do
  ! /usr/bin/getent group "$group" >/dev/null || fail "test group already exists: $group"
done
! /usr/bin/getent passwd "$AGENT_USER" >/dev/null || fail "test account already exists: $AGENT_USER"

if ! /usr/bin/getent passwd "$APPROVER_USER" >/dev/null; then
  /usr/sbin/useradd --create-home --shell /bin/sh "$APPROVER_USER"
  APPROVER_CREATED=1
fi
/usr/sbin/useradd --create-home --shell /usr/sbin/nologin --user-group "$AGENT_USER"
AGENT_CREATED=1
/usr/sbin/groupadd --system hostctl-agent
/usr/sbin/groupadd --system hostctl-approver
/usr/sbin/usermod -a -G hostctl-agent "$AGENT_USER"
/usr/sbin/usermod -a -G hostctl-approver "$APPROVER_USER"

/usr/bin/install -d -o root -g root -m 0755 \
  /etc/hostctl /etc/grok /usr/local/libexec /usr/local/share/hostctl/grok/hostctl-admin
/usr/bin/install -d -o "$AGENT_USER" -g "$AGENT_USER" -m 0700 "/home/$AGENT_USER/.grok"
/usr/bin/install -d -o root -g root -m 0755 "/home/$AGENT_USER/.grok/hooks"
/usr/bin/install -d -o "$AGENT_USER" -g "$AGENT_USER" -m 0755 "/home/$AGENT_USER/.grok/skills"
printf 'preserve-me\n' >"/home/$AGENT_USER/.grok/auth.json"
/bin/chown "$AGENT_USER:$AGENT_USER" "/home/$AGENT_USER/.grok/auth.json"
printf '{}\n' >"/home/$AGENT_USER/.grok/hooks/hostctl.json"
/bin/chown root:root "/home/$AGENT_USER/.grok/hooks/hostctl.json"
/bin/chmod 0644 "/home/$AGENT_USER/.grok/hooks/hostctl.json"
/bin/ln -s /usr/local/share/hostctl/grok/hostctl-admin "/home/$AGENT_USER/.grok/skills/hostctl-admin"

REVOKE_MARKER=/tmp/rootbroker-prealpha-revoke-called
[ ! -e "$REVOKE_MARKER" ] && [ ! -L "$REVOKE_MARKER" ] || fail "refusing pre-existing revoke marker"
/usr/bin/install -o "$APPROVER_USER" -g "$(/usr/bin/id -gn "$APPROVER_USER")" -m 0600 /dev/null "$REVOKE_MARKER"
/usr/bin/install -o root -g root -m 0755 "$PROJECT_DIR/tests/fake_legacy_hostctl.sh" /usr/local/libexec/hostctl-bin
/bin/ln -s /usr/local/libexec/hostctl-bin /usr/local/bin/hostctl
/bin/ln -s /usr/local/libexec/hostctl-bin /usr/local/bin/hostctl-admin
/bin/ln -s /usr/local/libexec/hostctl-bin /usr/local/sbin/hostctld
/bin/ln -s /usr/local/libexec/hostctl-bin /usr/local/libexec/hostctl-grok-hook
for managed_binary in /usr/local/bin/grok-safe /usr/local/libexec/grok-agent-launch /usr/local/libexec/grok-hostctl-bin; do
  /usr/bin/install -o root -g root -m 0755 /bin/true "$managed_binary"
done

cat >"$TEST_DIR/config.json" <<EOF
{
  "runtime_dir": "/run/hostctl",
  "request_group": "hostctl-agent",
  "admin_group": "hostctl-approver",
  "agent_users": ["$AGENT_USER"],
  "approver_users": ["$APPROVER_USER"]
}
EOF
/usr/bin/install -o root -g root -m 0644 "$TEST_DIR/config.json" /etc/hostctl/config.json
printf '%%hostctl-approver ALL=(%s) NOPASSWD: SETENV: /usr/local/libexec/grok-agent-launch *\n' "$AGENT_USER" >"$TEST_DIR/sudoers"
/usr/bin/install -o root -g root -m 0440 "$TEST_DIR/sudoers" /etc/sudoers.d/hostctl-grok-agent
/usr/sbin/visudo -cf /etc/sudoers >/dev/null

cat >"$TEST_DIR/hostctld.service" <<'EOF'
[Unit]
Description=Private pre-alpha fixture

[Service]
Type=simple
ExecStart=/usr/local/sbin/hostctld --config /etc/hostctl/config.json

[Install]
WantedBy=multi-user.target
EOF
/usr/bin/install -o root -g root -m 0644 "$TEST_DIR/hostctld.service" /etc/systemd/system/hostctld.service
cat >"$TEST_DIR/managed_config.toml" <<'EOF'
# unrelated config must survive
answer = 42
# BEGIN hostctl managed hooks
[[hooks]]
name = "hostctl"
# END hostctl managed hooks
EOF
/usr/bin/install -o root -g root -m 0644 "$TEST_DIR/managed_config.toml" /etc/grok/managed_config.toml
/usr/bin/systemctl daemon-reload
/usr/bin/systemctl enable --now hostctld.service >/dev/null

"$PROJECT_DIR/migrate-private-prealpha.sh" --approver-user "$APPROVER_USER" --check >"$TEST_DIR/check.log"
/bin/grep -q 'ready to remove' "$TEST_DIR/check.log" || fail "check mode did not recognize fixture"
/usr/bin/systemctl is-active --quiet hostctld.service || fail "check mode changed the service"
[ -e /usr/local/bin/hostctl ] || fail "check mode removed legacy files"

/usr/bin/sudo -u "$AGENT_USER" -H -- /bin/sleep 30 &
AGENT_PID=$!
for _ in $(/usr/bin/seq 1 50); do
  /usr/bin/pgrep -u "$AGENT_USER" >/dev/null 2>&1 && break
  /bin/sleep 0.02
done
if "$PROJECT_DIR/migrate-private-prealpha.sh" --approver-user "$APPROVER_USER" --check >"$TEST_DIR/running.log" 2>&1; then
  fail "migration accepted a running agent process"
fi
/bin/grep -q 'agent account still has running processes' "$TEST_DIR/running.log" || fail "running-agent refusal was unclear"
/bin/kill "$AGENT_PID"
wait "$AGENT_PID" 2>/dev/null || true
AGENT_PID=

"$PROJECT_DIR/migrate-private-prealpha.sh" --approver-user "$APPROVER_USER" >"$TEST_DIR/migrate.log"
[ "$(/bin/cat "$REVOKE_MARKER")" = revoked ] || fail "home access was not revoked before cleanup"
! /usr/bin/systemctl is-active --quiet hostctld.service || fail "legacy service is still active"
[ ! -e /etc/systemd/system/hostctld.service ] || fail "legacy service unit remains"
[ ! -e /etc/sudoers.d/hostctl-grok-agent ] || fail "legacy sudoers remains"
[ ! -e /usr/local/libexec/hostctl-bin ] || fail "legacy broker remains"
[ ! -e /usr/local/share/hostctl ] || fail "legacy managed share remains"
! /usr/bin/getent group hostctl-agent >/dev/null || fail "legacy request group remains"
! /usr/bin/getent group hostctl-approver >/dev/null || fail "legacy approver group remains"
/usr/bin/getent passwd "$AGENT_USER" >/dev/null || fail "agent account was removed"
[ "$(/bin/cat "/home/$AGENT_USER/.grok/auth.json")" = preserve-me ] || fail "agent authentication data changed"
[ ! -e "/home/$AGENT_USER/.grok/hooks/hostctl.json" ] || fail "legacy hook remains"
[ ! -e "/home/$AGENT_USER/.grok/skills/hostctl-admin" ] || fail "legacy skill remains"
/bin/grep -q 'unrelated config must survive' /etc/grok/managed_config.toml || fail "unrelated Grok config was removed"
! /bin/grep -q 'hostctl managed hooks' /etc/grok/managed_config.toml || fail "legacy managed block remains"
/usr/sbin/visudo -cf /etc/sudoers >/dev/null

echo "private pre-alpha migration system test passed"
