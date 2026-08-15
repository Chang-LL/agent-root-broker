#!/bin/sh
set -eu

PROJECT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
PROFILE=grok
PROFILE_DIR=
AGENT_USER=
APPROVER_USER=
AGENT_BIN=
HOSTCTL_BIN=
HOSTCTL_SOURCE=
TMP_DIR=
ALLOW_APPROVER_HOME_RW=0

usage() {
  echo "Usage: sudo ./install.sh --profile PROFILE --approver-user USER --agent-bin PATH [--agent-user USER] [--hostctl-bin PATH] [--allow-approver-home-rw]" >&2
  echo "       sudo ./install.sh --approver-user USER --grok-bin PATH [...]  # Grok compatibility alias" >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --profile)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      PROFILE=$2
      shift 2
      ;;
    --agent-user)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      AGENT_USER=$2
      shift 2
      ;;
    --approver-user)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      APPROVER_USER=$2
      shift 2
      ;;
    --agent-bin)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      [ -z "$AGENT_BIN" ] || { echo "agent binary was specified more than once" >&2; exit 2; }
      AGENT_BIN=$2
      shift 2
      ;;
    --grok-bin)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      [ -z "$AGENT_BIN" ] || { echo "agent binary was specified more than once" >&2; exit 2; }
      AGENT_BIN=$2
      shift 2
      ;;
    --hostctl-bin)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      HOSTCTL_BIN=$2
      shift 2
      ;;
    --allow-approver-home-rw)
      ALLOW_APPROVER_HOME_RW=1
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

[ "$(id -u)" -eq 0 ] || { echo "install.sh must run as root" >&2; exit 1; }

case "$PROFILE" in
  grok)
    PROFILE_DIR="$PROJECT_DIR/profiles/grok"
    ;;
  *)
    echo "unsupported integration profile: $PROFILE" >&2
    exit 2
    ;;
esac
PROFILE_SCRIPT="$PROFILE_DIR/profile.sh"
[ -f "$PROFILE_SCRIPT" ] || { echo "integration profile is incomplete: $PROFILE_SCRIPT" >&2; exit 2; }
# shellcheck source=profiles/grok/profile.sh
. "$PROFILE_SCRIPT"
[ "${PROFILE_CONTRACT_VERSION:-}" = 1 ] || { echo "unsupported profile contract for $PROFILE" >&2; exit 2; }
for profile_function in profile_preflight profile_install profile_install_sudoers profile_print_next_steps; do
  command -v "$profile_function" >/dev/null 2>&1 || {
    echo "integration profile is missing function: $profile_function" >&2
    exit 2
  }
done
[ -n "${PROFILE_DISPLAY_NAME:-}" ] && [ -n "${PROFILE_DEFAULT_AGENT_USER:-}" ] && \
  [ -n "${PROFILE_AGENT_EXECUTABLE:-}" ] && [ -n "${PROFILE_SUDOERS_FILE:-}" ] || {
  echo "integration profile metadata is incomplete: $PROFILE" >&2
  exit 2
}
[ -n "$AGENT_USER" ] || AGENT_USER=$PROFILE_DEFAULT_AGENT_USER
[ -n "$APPROVER_USER" ] && [ -n "$AGENT_BIN" ] || { usage; exit 2; }

valid_user() {
  printf '%s\n' "$1" | /bin/grep -Eq '^[a-z_][a-z0-9_-]*[$]?$'
}

valid_user "$AGENT_USER" || { echo "invalid agent user name" >&2; exit 2; }
valid_user "$APPROVER_USER" || { echo "invalid approver user name" >&2; exit 2; }
valid_user "$PROFILE_DEFAULT_AGENT_USER" || { echo "profile contains an invalid default agent user" >&2; exit 2; }
printf '%s\n' "$PROFILE_SUDOERS_FILE" | /bin/grep -Eq '^[a-z0-9][a-z0-9_-]*$' || {
  echo "profile contains an invalid sudoers file name" >&2
  exit 2
}
case "$PROFILE_AGENT_EXECUTABLE" in
  /*) ;;
  *) echo "profile agent executable must be absolute" >&2; exit 2 ;;
esac
/usr/bin/getent passwd "$APPROVER_USER" >/dev/null || { echo "approver user does not exist" >&2; exit 2; }
[ "$(/usr/bin/id -u "$APPROVER_USER")" -ne 0 ] || { echo "approver user must not be root" >&2; exit 2; }
[ -f "$AGENT_BIN" ] && [ -x "$AGENT_BIN" ] || { echo "$PROFILE_DISPLAY_NAME binary is not an executable file" >&2; exit 2; }
AGENT_BIN=$(/usr/bin/readlink -f -- "$AGENT_BIN")
profile_preflight

TMP_DIR=$(/usr/bin/mktemp -d)
trap '/bin/rm -rf -- "$TMP_DIR"' EXIT HUP INT TERM

if [ -n "$HOSTCTL_BIN" ]; then
  HOSTCTL_SOURCE="explicit --hostctl-bin"
elif [ -f "$PROJECT_DIR/go.mod" ]; then
  if command -v go >/dev/null 2>&1; then
    (
      cd "$PROJECT_DIR"
      CGO_ENABLED=0 go build -trimpath -o "$TMP_DIR/hostctl" ./cmd/hostctl
    )
    HOSTCTL_BIN="$TMP_DIR/hostctl"
    HOSTCTL_SOURCE="source build from $PROJECT_DIR"
  else
    echo "source checkout detected but Go is unavailable; pass --hostctl-bin explicitly or use a release archive" >&2
    exit 2
  fi
elif [ -x "$PROJECT_DIR/hostctl" ] && [ -f "$PROJECT_DIR/hostctl" ]; then
  HOSTCTL_BIN="$PROJECT_DIR/hostctl"
  HOSTCTL_SOURCE="release archive"
else
  echo "hostctl binary not found; use a release archive or pass --hostctl-bin PATH" >&2
  exit 2
fi
[ -f "$HOSTCTL_BIN" ] && [ -x "$HOSTCTL_BIN" ] || { echo "hostctl binary is not an executable file" >&2; exit 2; }
HOSTCTL_BIN=$(/usr/bin/readlink -f -- "$HOSTCTL_BIN")
HOSTCTL_VERSION=$("$HOSTCTL_BIN" version) || { echo "hostctl binary cannot run on this host" >&2; exit 2; }
case "$HOSTCTL_VERSION" in
  "hostctl "*) ;;
  *) echo "hostctl binary returned an invalid version" >&2; exit 2 ;;
esac
HOSTCTL_SHA256=$(/usr/bin/sha256sum "$HOSTCTL_BIN" | /usr/bin/cut -d' ' -f1)
echo "Selected hostctl: $HOSTCTL_SOURCE"
echo "  integration profile: $PROFILE"
echo "  binary: $HOSTCTL_BIN"
echo "  version: $HOSTCTL_VERSION"
echo "  host architecture: $(uname -m)"
echo "  sha256: $HOSTCTL_SHA256"

/usr/bin/getent group hostctl-agent >/dev/null || /usr/sbin/groupadd --system hostctl-agent
/usr/bin/getent group hostctl-approver >/dev/null || /usr/sbin/groupadd --system hostctl-approver
if ! /usr/bin/getent passwd "$AGENT_USER" >/dev/null; then
  /usr/sbin/useradd --create-home --shell /usr/sbin/nologin --user-group "$AGENT_USER"
fi
[ "$(/usr/bin/id -u "$AGENT_USER")" -ne 0 ] || { echo "agent user must not be root" >&2; exit 2; }
[ "$(/usr/bin/id -u "$AGENT_USER")" -ne "$(/usr/bin/id -u "$APPROVER_USER")" ] || {
  echo "agent user must be different from approver user" >&2
  exit 2
}
/usr/sbin/usermod -a -G hostctl-agent "$AGENT_USER"
/usr/sbin/usermod -a -G hostctl-approver "$APPROVER_USER"

/usr/bin/install -d -o root -g root -m 0755 /usr/local/libexec /usr/local/bin /usr/local/sbin
/usr/bin/install -o root -g root -m 0755 "$HOSTCTL_BIN" /usr/local/libexec/hostctl-bin
/bin/ln -sfn /usr/local/libexec/hostctl-bin /usr/local/bin/hostctl
/bin/ln -sfn /usr/local/libexec/hostctl-bin /usr/local/bin/hostctl-admin
/bin/ln -sfn /usr/local/libexec/hostctl-bin /usr/local/sbin/hostctld
profile_install "$AGENT_BIN" "$AGENT_USER" "$TMP_DIR"

/bin/sed "s|@AGENT_USER@|$AGENT_USER|g; s|@APPROVER_USER@|$APPROVER_USER|g; s|@AGENT_EXECUTABLE@|$PROFILE_AGENT_EXECUTABLE|g" \
  "$PROJECT_DIR/packaging/config/config.json.in" >"$TMP_DIR/config.json"
/usr/bin/install -d -o root -g root -m 0755 /etc/hostctl
/usr/bin/install -o root -g root -m 0644 "$TMP_DIR/config.json" /etc/hostctl/config.json
profile_install_sudoers "$AGENT_USER" "$TMP_DIR"
/usr/sbin/visudo -cf /etc/sudoers

/usr/bin/install -o root -g root -m 0644 "$PROJECT_DIR/packaging/systemd/hostctld.service" /etc/systemd/system/hostctld.service
/usr/bin/systemctl daemon-reload
/usr/bin/systemctl enable hostctld.service
/usr/bin/systemctl restart hostctld.service

if [ "$ALLOW_APPROVER_HOME_RW" -eq 1 ]; then
  echo "WARNING: granting $AGENT_USER read/write access to the complete home of $APPROVER_USER." >&2
  echo "WARNING: this includes SSH keys, application credentials, configuration, and personal files." >&2
  echo "WARNING: the agent may be able to impersonate $APPROVER_USER; approval is no longer a strong isolation boundary." >&2
  /usr/bin/sudo -u "$APPROVER_USER" -H -- /usr/local/bin/hostctl-admin home-access grant
fi

echo "hostctl installed: $(/usr/local/bin/hostctl version)"
echo "Integration profile installed: $PROFILE"
echo "Open a second terminal and run: hostctl-admin watch"
profile_print_next_steps
if [ "$ALLOW_APPROVER_HOME_RW" -eq 1 ]; then
  echo "Home access enabled for $AGENT_USER. Revoke it with: hostctl-admin home-access revoke"
fi
