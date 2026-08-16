#!/bin/sh
set -eu

[ "$(uname -s)" = Linux ] || { echo "SKIP: Linux required"; exit 0; }
[ "$(id -u)" -eq 0 ] || { echo "install system test must run as root" >&2; exit 1; }
[ "${HOSTCTL_SYSTEM_TEST_ALLOW_MUTATION:-}" = 1 ] || {
  echo "refusing to modify the host without HOSTCTL_SYSTEM_TEST_ALLOW_MUTATION=1" >&2
  exit 1
}

PROJECT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TEST_DIR=$(/usr/bin/mktemp -d /tmp/hostctl-install-system.XXXXXX)
/bin/chmod 0755 "$TEST_DIR"
APPROVER_USER=${HOSTCTL_TEST_APPROVER_USER:-}
AGENT_USER=grok-agent
AGENT_PID=
CLEANUP_ARMED=0
APPROVER_CREATED=0
OLD_OBJECT=
NEW_OBJECT=
BROKEN_OBJECT=

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

cleanup() {
  [ -z "$AGENT_PID" ] || /bin/kill "$AGENT_PID" 2>/dev/null || true
  if [ "$CLEANUP_ARMED" -eq 1 ]; then
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
    [ -z "$OLD_OBJECT" ] || /bin/rm -f -- "$OLD_OBJECT"
    [ -z "$NEW_OBJECT" ] || /bin/rm -f -- "$NEW_OBJECT"
    [ -z "$BROKEN_OBJECT" ] || /bin/rm -f -- "$BROKEN_OBJECT"
    /bin/rm -rf -- /etc/hostctl /run/hostctl /var/lib/hostctl /usr/local/share/hostctl
    /bin/rm -f -- /etc/grok/managed_config.toml
    /usr/sbin/userdel -r "$AGENT_USER" >/dev/null 2>&1 || true
    /bin/rm -rf -- /home/grok-agent
    /usr/sbin/groupdel "$AGENT_USER" >/dev/null 2>&1 || true
    /usr/sbin/groupdel hostctl-agent >/dev/null 2>&1 || true
    /usr/sbin/groupdel hostctl-approver >/dev/null 2>&1 || true
    /bin/rmdir /etc/grok /usr/local/share/hostctl /usr/local/libexec 2>/dev/null || true
    /usr/bin/systemctl daemon-reload >/dev/null 2>&1 || true
  fi
  if [ "$APPROVER_CREATED" -eq 1 ]; then
    /usr/sbin/userdel -r "$APPROVER_USER" >/dev/null 2>&1 || true
  fi
  /bin/rm -rf -- "$TEST_DIR"
}
trap cleanup EXIT HUP INT TERM

[ -n "$APPROVER_USER" ] || fail "HOSTCTL_TEST_APPROVER_USER is required"
if ! /usr/bin/getent passwd "$APPROVER_USER" >/dev/null && [ "${HOSTCTL_TEST_CREATE_APPROVER:-}" = 1 ]; then
  /usr/sbin/useradd --create-home --shell /bin/sh "$APPROVER_USER"
  APPROVER_CREATED=1
fi
/usr/bin/getent passwd "$APPROVER_USER" >/dev/null || fail "approver user does not exist"
[ "$(/usr/bin/id -u "$APPROVER_USER")" -ne 0 ] || fail "approver user must not be root"

! /usr/bin/getent passwd "$AGENT_USER" >/dev/null || fail "test account already exists: $AGENT_USER"
for group in "$AGENT_USER" hostctl-agent hostctl-approver; do
  ! /usr/bin/getent group "$group" >/dev/null || fail "test group already exists: $group"
done
for path in \
  /etc/hostctl \
  /etc/grok/managed_config.toml \
  /home/grok-agent \
  /run/hostctl \
  /var/lib/hostctl/install-state \
  /etc/sudoers.d/hostctl-grok-agent \
  /etc/systemd/system/hostctld.service \
  /usr/local/bin/grok-safe \
  /usr/local/bin/hostctl \
  /usr/local/bin/hostctl-admin \
  /usr/local/sbin/hostctld \
  /usr/local/libexec/hostctl-bin \
  /usr/local/libexec/hostctl-grok-hook \
  /usr/local/libexec/grok-agent-launch \
  /usr/local/libexec/grok-hostctl-bin \
  /usr/local/share/hostctl; do
  [ ! -e "$path" ] || fail "refusing to replace pre-existing path: $path"
done
CLEANUP_ARMED=1

BUILD_CACHE="$TEST_DIR/go-cache"
OLD_BIN="$TEST_DIR/hostctl-old"
NEW_BIN="$TEST_DIR/hostctl-new"
BROKEN_BIN="$TEST_DIR/hostctl-broken"
FAKE_GROK="$TEST_DIR/grok-system-agent"
GOCACHE="$BUILD_CACHE" CGO_ENABLED=0 go build -trimpath -ldflags '-s -w -X main.version=system-old' -o "$OLD_BIN" "$PROJECT_DIR/cmd/hostctl"
GOCACHE="$BUILD_CACHE" CGO_ENABLED=0 go build -trimpath -ldflags '-s -w -X main.version=system-new' -o "$NEW_BIN" "$PROJECT_DIR/cmd/hostctl"
GOCACHE="$BUILD_CACHE" CGO_ENABLED=0 go build -trimpath -o "$BROKEN_BIN" "$PROJECT_DIR/tests/brokenhostctl"
GOCACHE="$BUILD_CACHE" CGO_ENABLED=0 go build -trimpath -o "$FAKE_GROK" "$PROJECT_DIR/tests/systemagent"
OLD_OBJECT="/usr/local/libexec/hostctl-bin-$(/usr/bin/sha256sum "$OLD_BIN" | /usr/bin/cut -d' ' -f1)"
NEW_OBJECT="/usr/local/libexec/hostctl-bin-$(/usr/bin/sha256sum "$NEW_BIN" | /usr/bin/cut -d' ' -f1)"
BROKEN_OBJECT="/usr/local/libexec/hostctl-bin-$(/usr/bin/sha256sum "$BROKEN_BIN" | /usr/bin/cut -d' ' -f1)"

# Profile names are selected from the installer's built-in allowlist. Reject an
# unknown profile before creating any hostctl system path.
if "$PROJECT_DIR/install.sh" --profile unknown --approver-user "$APPROVER_USER" --agent-bin "$FAKE_GROK" --hostctl-bin "$OLD_BIN" >"$TEST_DIR/unknown-profile.log" 2>&1; then
  fail "unknown integration profile was accepted"
fi
/bin/grep -q 'unsupported integration profile: unknown' "$TEST_DIR/unknown-profile.log" || {
  /bin/cat "$TEST_DIR/unknown-profile.log"
  fail "unknown profile failure was not explicit"
}
[ ! -e /etc/hostctl ] || fail "unknown profile changed the system"

# A source checkout without Go must fail before making any system changes. In
# particular, an ignored dist artifact must never be selected implicitly.
NO_GO_PATH="$TEST_DIR/no-go-path"
/bin/mkdir "$NO_GO_PATH"
/bin/ln -s /usr/bin/dirname "$NO_GO_PATH/dirname"
/bin/ln -s /usr/bin/id "$NO_GO_PATH/id"
if PATH="$NO_GO_PATH" "$PROJECT_DIR/install.sh" --profile grok --approver-user "$APPROVER_USER" --agent-bin "$FAKE_GROK" >"$TEST_DIR/no-go.log" 2>&1; then
  fail "source checkout unexpectedly installed without Go"
fi
/bin/grep -q 'source checkout detected but Go is unavailable' "$TEST_DIR/no-go.log" || {
  /bin/cat "$TEST_DIR/no-go.log"
  fail "source checkout failure did not explain how to select a binary"
}

# Build the same layout users receive from a release archive, then exercise
# its implicit, colocated binary selection.
RELEASE_DIR="$TEST_DIR/release"
/usr/bin/install -d "$RELEASE_DIR/packaging/config" "$RELEASE_DIR/packaging/systemd"
/usr/bin/install -m 0755 "$OLD_BIN" "$RELEASE_DIR/hostctl"
/usr/bin/install -m 0755 "$PROJECT_DIR/install.sh" "$RELEASE_DIR/install.sh"
/usr/bin/install -m 0755 "$PROJECT_DIR/uninstall.sh" "$RELEASE_DIR/uninstall.sh"
/bin/cp -R "$PROJECT_DIR/profiles" "$RELEASE_DIR/profiles"
/bin/cp "$PROJECT_DIR/packaging/config/config.json.in" "$RELEASE_DIR/packaging/config/"
/bin/cp "$PROJECT_DIR/packaging/systemd/hostctld.service" "$RELEASE_DIR/packaging/systemd/"

"$RELEASE_DIR/install.sh" --profile grok --approver-user "$APPROVER_USER" --agent-bin "$FAKE_GROK" >"$TEST_DIR/install-first.log"
/bin/grep -q 'Selected hostctl: release archive' "$TEST_DIR/install-first.log"
/bin/grep -q 'integration profile: grok' "$TEST_DIR/install-first.log"
/bin/grep -q 'version: hostctl system-old' "$TEST_DIR/install-first.log"
[ "$(/usr/local/bin/hostctl version)" = 'hostctl system-old' ] || fail "first install used the wrong binary"
/usr/bin/systemctl is-active --quiet hostctld.service || fail "hostctld is not active"
/usr/bin/systemctl is-enabled --quiet hostctld.service || fail "hostctld is not enabled"
/usr/sbin/visudo -cf /etc/sudoers >/dev/null
[ "$(/usr/bin/stat -Lc '%U:%G %a' /usr/local/libexec/hostctl-bin)" = 'root:root 755' ] || fail "installed binary ownership or mode is wrong"
/usr/bin/id -nG "$AGENT_USER" | /bin/grep -qw hostctl-agent || fail "agent lacks request group"
/usr/bin/id -nG "$APPROVER_USER" | /bin/grep -qw hostctl-approver || fail "approver lacks admin group"

# Reinstallation must be idempotent and must not duplicate managed config.
# Use the compatibility alias to ensure existing Grok upgrade commands remain valid.
"$RELEASE_DIR/install.sh" --approver-user "$APPROVER_USER" --grok-bin "$FAKE_GROK" >"$TEST_DIR/install-repeat.log"
[ "$(/bin/grep -c '# BEGIN hostctl managed hooks' /etc/grok/managed_config.toml)" -eq 1 ] || fail "managed Grok config was duplicated"

# Upgrade using an explicit artifact and verify the selected provenance.
"$PROJECT_DIR/install.sh" --profile grok --approver-user "$APPROVER_USER" --agent-bin "$FAKE_GROK" --hostctl-bin "$NEW_BIN" >"$TEST_DIR/install-upgrade.log"
/bin/grep -q 'Selected hostctl: explicit --hostctl-bin' "$TEST_DIR/install-upgrade.log"
/bin/grep -q 'version: hostctl system-new' "$TEST_DIR/install-upgrade.log"
[ "$(/usr/local/bin/hostctl version)" = 'hostctl system-new' ] || fail "upgrade used the wrong binary"
/usr/bin/systemctl is-active --quiet hostctld.service || fail "hostctld is not active after upgrade"

# A candidate that passes offline validation but cannot start as a daemon must
# not replace the running version.
if "$PROJECT_DIR/install.sh" --profile grok --approver-user "$APPROVER_USER" --agent-bin "$FAKE_GROK" --hostctl-bin "$BROKEN_BIN" >"$TEST_DIR/install-broken.log" 2>&1; then
  fail "broken daemon upgrade unexpectedly succeeded"
fi
/bin/grep -q 'restoring previous hostctl binary' "$TEST_DIR/install-broken.log" || {
  /bin/cat "$TEST_DIR/install-broken.log"
  fail "broken upgrade did not report rollback"
}
[ "$(/usr/local/bin/hostctl version)" = 'hostctl system-new' ] || fail "broken upgrade did not restore the prior binary"
/usr/bin/systemctl is-active --quiet hostctld.service || fail "prior daemon is not active after failed upgrade"
[ ! -e "$BROKEN_OBJECT" ] || fail "failed candidate object was not removed"

as_approver() {
  /usr/bin/sudo -u "$APPROVER_USER" -H -- "$@"
}

pending_id() {
  for _ in $(/usr/bin/seq 1 100); do
    pending=$(as_approver /usr/local/bin/hostctl-admin --json pending)
    request_id=$(printf '%s\n' "$pending" | /bin/sed -n 's/.*"id":"\([0-9a-f]*\)".*/\1/p')
    if [ -n "$request_id" ]; then
      printf '%s\n' "$request_id"
      return 0
    fi
    /bin/sleep 0.05
  done
  return 1
}

lease_id() {
  leases=$(as_approver /usr/local/bin/hostctl-admin --json leases)
  printf '%s\n' "$leases" | /bin/sed -n 's/.*"id":"\([0-9a-f]*\)".*/\1/p'
}

wait_for_file() {
  wait_path=$1
  for _ in $(/usr/bin/seq 1 200); do
    [ -e "$wait_path" ] && return 0
    /bin/sleep 0.05
  done
  return 1
}

# The real launcher, sudoers rule, peer credentials, process ancestry, admin
# socket, message lease, and root executor all participate in this test.
as_approver /bin/sh -c 'cd /tmp && exec /usr/local/bin/grok-safe --hostctl-system-test=message' >"$TEST_DIR/approved.out" 2>&1 &
AGENT_PID=$!
REQUEST_ID=$(pending_id) || fail "installed agent did not create an approval request"
as_approver /usr/local/bin/hostctl-admin approve "$REQUEST_ID" --scope message
wait "$AGENT_PID"
AGENT_PID=
[ "$(/bin/grep -c '"approvalScope":"message"' "$TEST_DIR/approved.out")" -eq 2 ] || {
  /bin/cat "$TEST_DIR/approved.out"
  fail "message lease was not used for both installed-agent commands"
}
/bin/grep -q '"stdout":"0\\n"' "$TEST_DIR/approved.out" || fail "approved command did not execute as root"
/bin/grep -q '"stdout":"root\\n"' "$TEST_DIR/approved.out" || fail "second approved command did not execute as root"

# Command approval must cover exactly one execution.
as_approver /bin/sh -c 'cd /tmp && exec /usr/local/bin/grok-safe --hostctl-system-test=command' >"$TEST_DIR/command.out" 2>&1 &
AGENT_PID=$!
REQUEST_ID=$(pending_id) || fail "command-scope test did not create its first request"
as_approver /usr/local/bin/hostctl-admin approve "$REQUEST_ID" --scope command
REQUEST_ID=$(pending_id) || fail "command scope leaked into a second execution"
as_approver /usr/local/bin/hostctl-admin deny "$REQUEST_ID"
if wait "$AGENT_PID"; then
  fail "second command unexpectedly reused command approval"
fi
AGENT_PID=
[ "$(/bin/grep -c '"approvalScope":"command"' "$TEST_DIR/command.out")" -eq 1 ] || fail "command approval count is wrong"

# Session approval survives a turn boundary and is removed at session end.
as_approver /bin/sh -c 'cd /tmp && exec /usr/local/bin/grok-safe --hostctl-system-test=session' >"$TEST_DIR/session.out" 2>&1 &
AGENT_PID=$!
REQUEST_ID=$(pending_id) || fail "session-scope test did not create a request"
as_approver /usr/local/bin/hostctl-admin approve "$REQUEST_ID" --scope session
wait "$AGENT_PID" || { /bin/cat "$TEST_DIR/session.out"; fail "session lease was not reused across turns"; }
AGENT_PID=
[ "$(/bin/grep -c '"approvalScope":"session"' "$TEST_DIR/session.out")" -eq 2 ] || fail "session lease was not used twice"
as_approver /usr/local/bin/hostctl-admin --json leases | /bin/grep -q '"leases":\[\]'

# Missing lifecycle signals fail closed before a request reaches approval.
if as_approver /bin/sh -c 'cd /tmp && exec /usr/local/bin/grok-safe --hostctl-system-test=missing' >"$TEST_DIR/missing.out" 2>&1; then
  fail "request without lifecycle hooks unexpectedly succeeded"
fi
/bin/grep -q '"code":"no_active_turn"' "$TEST_DIR/missing.out" || fail "missing lifecycle did not fail closed"

# Execution timeout must kill the process group and return the stable timeout shape.
as_approver /bin/sh -c 'cd /tmp && exec /usr/local/bin/grok-safe --hostctl-system-test=timeout' >"$TEST_DIR/timeout.out" 2>&1 &
AGENT_PID=$!
REQUEST_ID=$(pending_id) || fail "timeout test did not create a request"
as_approver /usr/local/bin/hostctl-admin approve "$REQUEST_ID" --scope command
if wait "$AGENT_PID"; then
  fail "timed out command unexpectedly succeeded"
fi
AGENT_PID=
/bin/grep -q '"exitCode":124' "$TEST_DIR/timeout.out" || fail "timeout exit code is wrong"
/bin/grep -q '"timedOut":true' "$TEST_DIR/timeout.out" || fail "timeout flag is missing"

# Revoking a live session lease forces the next command back through review.
REVOKE_GATE="$TEST_DIR/revoke-gate"
/usr/bin/install -d -o "$AGENT_USER" -g "$AGENT_USER" -m 0700 "$REVOKE_GATE"
# $1 is intentionally expanded by the inner shell after the user switch.
# shellcheck disable=SC2016
as_approver /bin/sh -c 'cd /tmp && exec /usr/local/bin/grok-safe --hostctl-system-test=revoke --hostctl-test-gate="$1"' hostctl-test "$REVOKE_GATE/run" >"$TEST_DIR/revoke.out" 2>&1 &
AGENT_PID=$!
REQUEST_ID=$(pending_id) || fail "revoke test did not create a request"
as_approver /usr/local/bin/hostctl-admin approve "$REQUEST_ID" --scope session
wait_for_file "$REVOKE_GATE/run.ready" || fail "revoke test agent did not reach its gate"
LEASE_ID=$(lease_id)
[ -n "$LEASE_ID" ] || fail "session lease was not visible for revoke"
as_approver /usr/local/bin/hostctl-admin revoke "$LEASE_ID"
/usr/bin/touch "$REVOKE_GATE/run.continue"
REQUEST_ID=$(pending_id) || fail "revoked lease was reused"
as_approver /usr/local/bin/hostctl-admin deny "$REQUEST_ID"
if wait "$AGENT_PID"; then
  fail "revoked session lease authorized another command"
fi
AGENT_PID=
/bin/grep -q '"code":"denied"' "$TEST_DIR/revoke.out" || fail "revoked request did not return denial"

# Daemon restart clears both leases and lifecycle state. The stale turn fails
# closed; a fresh prompt can submit a new request.
RESTART_GATE="$TEST_DIR/restart-gate"
/usr/bin/install -d -o "$AGENT_USER" -g "$AGENT_USER" -m 0700 "$RESTART_GATE"
# $1 is intentionally expanded by the inner shell after the user switch.
# shellcheck disable=SC2016
as_approver /bin/sh -c 'cd /tmp && exec /usr/local/bin/grok-safe --hostctl-system-test=restart --hostctl-test-gate="$1"' hostctl-test "$RESTART_GATE/run" >"$TEST_DIR/restart.out" 2>&1 &
AGENT_PID=$!
REQUEST_ID=$(pending_id) || fail "restart test did not create a request"
as_approver /usr/local/bin/hostctl-admin approve "$REQUEST_ID" --scope session
wait_for_file "$RESTART_GATE/run.ready" || fail "restart test agent did not reach its gate"
/usr/bin/systemctl restart hostctld.service
for _ in $(/usr/bin/seq 1 100); do
  [ -S /run/hostctl/request.sock ] && [ -S /run/hostctl/admin.sock ] && break
  /bin/sleep 0.05
done
/usr/bin/touch "$RESTART_GATE/run.continue"
REQUEST_ID=$(pending_id) || { /bin/cat "$TEST_DIR/restart.out"; fail "fresh turn after restart did not create a request"; }
as_approver /usr/local/bin/hostctl-admin deny "$REQUEST_ID"
if wait "$AGENT_PID"; then
  fail "restart test unexpectedly succeeded"
fi
AGENT_PID=
/bin/grep -q '"code":"no_active_turn"' "$TEST_DIR/restart.out" || fail "stale turn survived daemon restart"
/bin/grep -q '"code":"denied"' "$TEST_DIR/restart.out" || fail "fresh post-restart request was not reviewed"

# A direct sudo attempt by the agent must remain unavailable.
# The root test harness, not sudo, intentionally owns this diagnostic redirect.
# shellcheck disable=SC2024
if /usr/bin/sudo -u "$AGENT_USER" -H -- /usr/bin/sudo -n /usr/bin/id -u >"$TEST_DIR/direct-sudo.out" 2>&1; then
  fail "agent bypassed hostctl with direct sudo"
fi

# A denied request must return a denial and must not execute.
as_approver /bin/sh -c 'cd /tmp && exec /usr/local/bin/grok-safe --hostctl-system-test=deny' >"$TEST_DIR/denied.out" 2>&1 &
AGENT_PID=$!
REQUEST_ID=$(pending_id) || fail "denial test did not create an approval request"
as_approver /usr/local/bin/hostctl-admin deny "$REQUEST_ID"
if wait "$AGENT_PID"; then
  fail "denied agent command unexpectedly succeeded"
fi
AGENT_PID=
/bin/grep -q '"code":"denied"' "$TEST_DIR/denied.out" || {
  /bin/cat "$TEST_DIR/denied.out"
  fail "agent did not receive the denial"
}

as_approver /usr/local/bin/hostctl-admin home-access status | /bin/grep -q 'Full-home access: disabled'

# Exercise the real approver home with restrictive modes, repeated grants,
# inherited defaults, and idempotent revoke.
APPROVER_HOME=$(/usr/bin/getent passwd "$APPROVER_USER" | /usr/bin/cut -d: -f6)
/usr/bin/sudo -u "$APPROVER_USER" -H -- /bin/sh -c '
  umask 077
  mkdir -p "$HOME/hostctl-home-test/private"
  printf existing >"$HOME/hostctl-home-test/private/existing"
'
/bin/chmod 0700 "$APPROVER_HOME" "$APPROVER_HOME/hostctl-home-test" "$APPROVER_HOME/hostctl-home-test/private"
as_approver /usr/local/bin/hostctl-admin home-access grant
as_approver /usr/local/bin/hostctl-admin home-access grant
as_approver /usr/local/bin/hostctl-admin home-access status | /bin/grep -q 'Full-home access: enabled'
/usr/bin/sudo -u "$AGENT_USER" -H -- /bin/grep -q existing "$APPROVER_HOME/hostctl-home-test/private/existing" || fail "agent cannot read granted restrictive file"
/usr/bin/sudo -u "$APPROVER_USER" -H -- /bin/sh -c '
  umask 077
  printf inherited >"$HOME/hostctl-home-test/private/inherited"
'
/usr/bin/sudo -u "$AGENT_USER" -H -- /bin/grep -q inherited "$APPROVER_HOME/hostctl-home-test/private/inherited" || fail "default ACL did not reach a new file"
as_approver /usr/local/bin/hostctl-admin home-access revoke
as_approver /usr/local/bin/hostctl-admin home-access revoke
as_approver /usr/local/bin/hostctl-admin home-access status | /bin/grep -q 'Full-home access: disabled'
if /usr/bin/sudo -u "$AGENT_USER" -H -- /bin/cat "$APPROVER_HOME/hostctl-home-test/private/existing" >/dev/null 2>&1; then
  fail "agent retained home access after revoke"
fi

# Uninstall through the shipped release layout. Managed system files, users,
# groups, versioned binaries, hooks, and ACLs must be removed; agent home data
# is deliberately preserved.
"$RELEASE_DIR/uninstall.sh" --purge-agent-account >"$TEST_DIR/uninstall.log"
! /usr/bin/getent passwd "$AGENT_USER" >/dev/null || fail "uninstall left the agent account"
! /usr/bin/getent group "$AGENT_USER" >/dev/null || fail "uninstall left the agent primary group"
! /usr/bin/getent group hostctl-agent >/dev/null || fail "uninstall left the request group"
! /usr/bin/getent group hostctl-approver >/dev/null || fail "uninstall left the approver group"
[ -d /home/grok-agent ] || fail "uninstall removed agent home data"
for path in \
  /etc/hostctl/config.json \
  /etc/grok/managed_config.toml \
  /etc/sudoers.d/hostctl-grok-agent \
  /etc/systemd/system/hostctld.service \
  /var/lib/hostctl/install-state \
  /usr/local/bin/grok-safe \
  /usr/local/bin/hostctl \
  /usr/local/bin/hostctl-admin \
  /usr/local/sbin/hostctld \
  /usr/local/libexec/hostctl-bin \
  "$OLD_OBJECT" \
  "$NEW_OBJECT"; do
  [ ! -e "$path" ] && [ ! -L "$path" ] || fail "uninstall left managed path: $path"
done

echo "PASS: install, idempotence, rollback, approval, denial, ACL lifecycle, and uninstall"
