#!/bin/sh
set -eu

PROJECT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
STATE_PATH=/var/lib/hostctl/install-state
PURGE_AGENT_ACCOUNT=0
SKIP_HOME_REVOKE=0
TMP_DIR=

usage() {
  echo "Usage: sudo ./uninstall.sh [--purge-agent-account] [--skip-home-access-revoke]" >&2
  echo "Agent home data is always preserved." >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --purge-agent-account) PURGE_AGENT_ACCOUNT=1; shift ;;
    --skip-home-access-revoke) SKIP_HOME_REVOKE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
done

[ "$(id -u)" -eq 0 ] || { echo "uninstall.sh must run as root" >&2; exit 1; }
[ -f "$STATE_PATH" ] || { echo "hostctl install state not found: $STATE_PATH" >&2; exit 1; }
[ "$(/usr/bin/stat -c '%u' "$STATE_PATH")" -eq 0 ] && [ $(( $(/usr/bin/stat -c '%a' "$STATE_PATH") % 100 )) -eq 0 ] || {
  echo "refusing unsafe install state ownership or mode" >&2
  exit 1
}

state_value() { /bin/sed -n "s/^$1=//p" "$STATE_PATH"; }
state_flag() {
  state_flag_value=$(state_value "$1")
  case "$state_flag_value" in
    0|1) printf '%s\n' "$state_flag_value" ;;
    *) echo "invalid install state flag: $1" >&2; return 1 ;;
  esac
}
valid_user() { printf '%s\n' "$1" | /bin/grep -Eq '^[a-z_][a-z0-9_-]*[$]?$'; }

[ "$(state_value format)" = 1 ] || { echo "unsupported install state format" >&2; exit 1; }
PROFILE=$(state_value profile)
AGENT_USER=$(state_value agent_user)
APPROVER_USER=$(state_value approver_user)
AGENT_HOME=$(state_value agent_home)
CREATED_AGENT_USER=$(state_flag created_agent_user)
CREATED_AGENT_GROUP=$(state_flag created_agent_group)
CREATED_REQUEST_GROUP=$(state_flag created_request_group)
CREATED_APPROVER_GROUP=$(state_flag created_approver_group)
ADDED_AGENT_MEMBERSHIP=$(state_flag agent_membership_added)
ADDED_APPROVER_MEMBERSHIP=$(state_flag approver_membership_added)
if ! valid_user "$AGENT_USER" || ! valid_user "$APPROVER_USER"; then
  echo "invalid user in install state" >&2
  exit 1
fi
case "$AGENT_HOME" in
  /*) [ "$AGENT_HOME" != / ] || { echo "unsafe agent home in install state" >&2; exit 1; } ;;
  *) echo "unsafe agent home in install state" >&2; exit 1 ;;
esac

case "$PROFILE" in
  grok) PROFILE_DIR="$PROJECT_DIR/profiles/grok" ;;
  *) echo "unsupported integration profile in install state: $PROFILE" >&2; exit 1 ;;
esac
PROFILE_SCRIPT="$PROFILE_DIR/profile.sh"
[ -f "$PROFILE_SCRIPT" ] || { echo "integration profile is missing: $PROFILE_SCRIPT" >&2; exit 1; }
# shellcheck source=profiles/grok/profile.sh
. "$PROFILE_SCRIPT"
if [ "${PROFILE_CONTRACT_VERSION:-}" != 2 ] || ! command -v profile_uninstall >/dev/null 2>&1; then
  echo "unsupported integration profile contract" >&2
  exit 1
fi

TMP_DIR=$(/usr/bin/mktemp -d)
trap '/bin/rm -rf -- "$TMP_DIR"' EXIT HUP INT TERM
/bin/sed -n 's/^hostctl_object=//p' "$STATE_PATH" >"$TMP_DIR/hostctl-objects"
while IFS= read -r hostctl_object; do
  [ -n "$hostctl_object" ] || continue
  printf '%s\n' "$hostctl_object" | /bin/grep -Eq '^/usr/local/libexec/hostctl-bin-[0-9a-f]{64}$' || {
    echo "refusing malformed hostctl object path: $hostctl_object" >&2
    exit 1
  }
done <"$TMP_DIR/hostctl-objects"

if [ "$PURGE_AGENT_ACCOUNT" -eq 1 ]; then
  [ "$CREATED_AGENT_USER" -eq 1 ] || {
    echo "refusing to remove an agent account that hostctl did not create" >&2
    exit 1
  }
  if command -v pgrep >/dev/null 2>&1 && /usr/bin/pgrep -u "$AGENT_USER" >/dev/null 2>&1; then
    echo "agent account still has running processes; stop them before uninstalling" >&2
    exit 1
  fi
fi

if [ "$SKIP_HOME_REVOKE" -eq 0 ]; then
  [ -x /usr/local/libexec/hostctl-bin ] || { echo "cannot revoke home access: hostctl binary is missing" >&2; exit 1; }
  /usr/local/libexec/hostctl-bin hostctl-maint --config /etc/hostctl/config.json home-access revoke
else
  echo "WARNING: skipping home-access revoke; ACL access may remain on $APPROVER_USER's home" >&2
fi

/usr/bin/systemctl disable --now hostctld.service >/dev/null 2>&1 || true
profile_uninstall "$AGENT_HOME" "$TMP_DIR"

/bin/rm -f -- \
  "/etc/sudoers.d/$PROFILE_SUDOERS_FILE" \
  /etc/systemd/system/hostctld.service \
  /etc/hostctl/config.json \
  /usr/local/bin/hostctl \
  /usr/local/bin/hostctl-admin \
  /usr/local/sbin/hostctl-uninstall \
  /usr/local/sbin/hostctld \
  /usr/local/libexec/hostctl-bin \
  /usr/local/share/hostctl/installer/uninstall.sh \
  /usr/local/share/hostctl/installer/profiles/grok/profile.sh
while IFS= read -r hostctl_object; do
  [ -n "$hostctl_object" ] || continue
  /bin/rm -f -- "$hostctl_object"
done <"$TMP_DIR/hostctl-objects"
/bin/rm -f -- "$STATE_PATH"
/usr/bin/systemctl daemon-reload
/usr/sbin/visudo -cf /etc/sudoers >/dev/null

if [ "$ADDED_APPROVER_MEMBERSHIP" -eq 1 ] && /usr/bin/getent group hostctl-approver >/dev/null; then
  /usr/bin/gpasswd -d "$APPROVER_USER" hostctl-approver >/dev/null 2>&1 || true
fi
if [ "$ADDED_AGENT_MEMBERSHIP" -eq 1 ] && /usr/bin/getent passwd "$AGENT_USER" >/dev/null && \
  /usr/bin/getent group hostctl-agent >/dev/null; then
  /usr/bin/gpasswd -d "$AGENT_USER" hostctl-agent >/dev/null 2>&1 || true
fi
if [ "$PURGE_AGENT_ACCOUNT" -eq 1 ]; then
  /usr/sbin/userdel "$AGENT_USER"
  if [ "$CREATED_AGENT_GROUP" -eq 1 ]; then
    /usr/sbin/groupdel "$AGENT_USER" >/dev/null 2>&1 || true
  fi
  echo "Removed agent account $AGENT_USER; preserved its home directory: $AGENT_HOME"
else
  echo "Preserved agent account and home: $AGENT_USER ($AGENT_HOME)"
fi
if [ "$CREATED_REQUEST_GROUP" -eq 1 ]; then
  /usr/sbin/groupdel hostctl-agent >/dev/null 2>&1 || true
fi
if [ "$CREATED_APPROVER_GROUP" -eq 1 ]; then
  /usr/sbin/groupdel hostctl-approver >/dev/null 2>&1 || true
fi
/bin/rmdir /run/hostctl /etc/hostctl /etc/grok /var/lib/hostctl \
  /usr/local/share/hostctl/installer/profiles/grok \
  /usr/local/share/hostctl/installer/profiles \
  /usr/local/share/hostctl/installer \
  /usr/local/share/hostctl /usr/local/libexec 2>/dev/null || true

echo "hostctl uninstalled."
