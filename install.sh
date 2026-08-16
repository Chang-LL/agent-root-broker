#!/bin/sh
set -eu

PROJECT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
PROFILE=grok
PROFILE_DIR=
AGENT_USER=
APPROVER_USER=
AGENT_BIN=
ROOTBROKER_BIN=
ROOTBROKER_SOURCE=
TMP_DIR=
ALLOW_APPROVER_HOME_RW=0
STATE_PATH=/var/lib/rootbroker/install-state
STATE_PRESENT=0
CREATED_AGENT_USER=0
CREATED_AGENT_GROUP=0
CREATED_REQUEST_GROUP=0
CREATED_APPROVER_GROUP=0
ADDED_AGENT_MEMBERSHIP=0
ADDED_APPROVER_MEMBERSHIP=0

usage() {
  echo "Usage: sudo ./install.sh --profile PROFILE --approver-user USER --agent-bin PATH [--agent-user USER] [--rootbroker-bin PATH] [--allow-approver-home-rw]" >&2
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
    --rootbroker-bin)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      ROOTBROKER_BIN=$2
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

if [ -f /var/lib/hostctl/install-state ] || [ -e /etc/systemd/system/hostctld.service ]; then
  echo "A private pre-alpha hostctl installation is still present." >&2
  echo "Close active Grok sessions, run: sudo /usr/local/sbin/hostctl-uninstall" >&2
  echo "Then rerun this installer. See MIGRATION.md; add --allow-approver-home-rw to restore optional full-home access." >&2
  exit 2
fi

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
[ -f "$PROJECT_DIR/uninstall.sh" ] || { echo "uninstaller is missing: $PROJECT_DIR/uninstall.sh" >&2; exit 2; }
# shellcheck source=profiles/grok/profile.sh
. "$PROFILE_SCRIPT"
[ "${PROFILE_CONTRACT_VERSION:-}" = 2 ] || { echo "unsupported profile contract for $PROFILE" >&2; exit 2; }
for profile_function in \
  profile_preflight profile_prepare profile_install profile_install_sudoers \
  profile_uninstall profile_print_next_steps; do
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

is_group_member() {
  /usr/bin/id -nG "$1" | /usr/bin/tr ' ' '\n' | /bin/grep -Fxq "$2"
}

state_value() {
  /bin/sed -n "s/^$1=//p" "$STATE_PATH"
}

load_state_flag() {
  state_flag_value=$(state_value "$1")
  case "$state_flag_value" in
    0|1) printf '%s\n' "$state_flag_value" ;;
    *) echo "invalid install state flag: $1" >&2; return 1 ;;
  esac
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

if [ -n "$ROOTBROKER_BIN" ]; then
  ROOTBROKER_SOURCE="explicit --rootbroker-bin"
elif [ -f "$PROJECT_DIR/go.mod" ]; then
  if command -v go >/dev/null 2>&1; then
    (
      cd "$PROJECT_DIR"
      CGO_ENABLED=0 go build -trimpath -o "$TMP_DIR/rootbroker" ./cmd/rootbroker
    )
    ROOTBROKER_BIN="$TMP_DIR/rootbroker"
    ROOTBROKER_SOURCE="source build from $PROJECT_DIR"
  else
    echo "source checkout detected but Go is unavailable; pass --rootbroker-bin explicitly or use a release archive" >&2
    exit 2
  fi
elif [ -x "$PROJECT_DIR/rootbroker" ] && [ -f "$PROJECT_DIR/rootbroker" ]; then
  ROOTBROKER_BIN="$PROJECT_DIR/rootbroker"
  ROOTBROKER_SOURCE="release archive"
else
  echo "rootbroker binary not found; use a release archive or pass --rootbroker-bin PATH" >&2
  exit 2
fi
[ -f "$ROOTBROKER_BIN" ] && [ -x "$ROOTBROKER_BIN" ] || { echo "rootbroker binary is not an executable file" >&2; exit 2; }
ROOTBROKER_BIN=$(/usr/bin/readlink -f -- "$ROOTBROKER_BIN")
ROOTBROKER_VERSION=$("$ROOTBROKER_BIN" version) || { echo "rootbroker binary cannot run on this host" >&2; exit 2; }
case "$ROOTBROKER_VERSION" in
  "rootbroker "*) ;;
  *) echo "rootbroker binary returned an invalid version" >&2; exit 2 ;;
esac
ROOTBROKER_SHA256=$(/usr/bin/sha256sum "$ROOTBROKER_BIN" | /usr/bin/cut -d' ' -f1)
ROOTBROKER_OBJECT="/usr/local/libexec/rootbroker-bin-$ROOTBROKER_SHA256"

/bin/sed "s|@AGENT_USER@|$AGENT_USER|g; s|@APPROVER_USER@|$APPROVER_USER|g; s|@AGENT_EXECUTABLE@|$PROFILE_AGENT_EXECUTABLE|g" \
  "$PROJECT_DIR/packaging/config/config.json.in" >"$TMP_DIR/config.json"
"$ROOTBROKER_BIN" rootbrokerd --check-config "$TMP_DIR/config.json"
profile_prepare "$AGENT_USER" "$TMP_DIR"

if [ -f "$STATE_PATH" ]; then
  [ "$(/usr/bin/stat -c '%u' "$STATE_PATH")" -eq 0 ] && [ $(( $(/usr/bin/stat -c '%a' "$STATE_PATH") % 100 )) -eq 0 ] || {
    echo "refusing unsafe install state ownership or mode" >&2
    exit 2
  }
  [ "$(state_value format)" = 1 ] || { echo "unsupported install state format" >&2; exit 2; }
  [ "$(state_value profile)" = "$PROFILE" ] && [ "$(state_value agent_user)" = "$AGENT_USER" ] && \
    [ "$(state_value approver_user)" = "$APPROVER_USER" ] || {
    echo "installed identities differ; uninstall before changing profile or users" >&2
    exit 2
  }
  CREATED_AGENT_USER=$(load_state_flag created_agent_user)
  CREATED_AGENT_GROUP=$(load_state_flag created_agent_group)
  CREATED_REQUEST_GROUP=$(load_state_flag created_request_group)
  CREATED_APPROVER_GROUP=$(load_state_flag created_approver_group)
  ADDED_AGENT_MEMBERSHIP=$(load_state_flag agent_membership_added)
  ADDED_APPROVER_MEMBERSHIP=$(load_state_flag approver_membership_added)
  STATE_PRESENT=1
fi

echo "Selected rootbroker: $ROOTBROKER_SOURCE"
echo "  integration profile: $PROFILE"
echo "  binary: $ROOTBROKER_BIN"
echo "  version: $ROOTBROKER_VERSION"
echo "  host architecture: $(uname -m)"
echo "  sha256: $ROOTBROKER_SHA256"

if ! /usr/bin/getent group rootbroker-agent >/dev/null; then
  /usr/sbin/groupadd --system rootbroker-agent
  [ "$STATE_PRESENT" -eq 1 ] || CREATED_REQUEST_GROUP=1
fi
if ! /usr/bin/getent group rootbroker-approver >/dev/null; then
  /usr/sbin/groupadd --system rootbroker-approver
  [ "$STATE_PRESENT" -eq 1 ] || CREATED_APPROVER_GROUP=1
fi
if ! /usr/bin/getent passwd "$AGENT_USER" >/dev/null; then
  if /usr/bin/getent group "$AGENT_USER" >/dev/null; then
    /usr/sbin/useradd --create-home --shell /usr/sbin/nologin --gid "$AGENT_USER" "$AGENT_USER"
  else
    /usr/sbin/useradd --create-home --shell /usr/sbin/nologin --user-group "$AGENT_USER"
    [ "$STATE_PRESENT" -eq 1 ] || CREATED_AGENT_GROUP=1
  fi
  [ "$STATE_PRESENT" -eq 1 ] || CREATED_AGENT_USER=1
fi
[ "$(/usr/bin/id -u "$AGENT_USER")" -ne 0 ] || { echo "agent user must not be root" >&2; exit 2; }
[ "$(/usr/bin/id -u "$AGENT_USER")" -ne "$(/usr/bin/id -u "$APPROVER_USER")" ] || {
  echo "agent user must be different from approver user" >&2
  exit 2
}
if ! is_group_member "$AGENT_USER" rootbroker-agent; then
  /usr/sbin/usermod -a -G rootbroker-agent "$AGENT_USER"
  [ "$STATE_PRESENT" -eq 1 ] || ADDED_AGENT_MEMBERSHIP=1
fi
if ! is_group_member "$APPROVER_USER" rootbroker-approver; then
  /usr/sbin/usermod -a -G rootbroker-approver "$APPROVER_USER"
  [ "$STATE_PRESENT" -eq 1 ] || ADDED_APPROVER_MEMBERSHIP=1
fi
AGENT_HOME=$(/usr/bin/getent passwd "$AGENT_USER" | /usr/bin/cut -d: -f6)
case "$AGENT_HOME" in
  /*) [ "$AGENT_HOME" != / ] || { echo "agent home must not be /" >&2; exit 2; } ;;
  *) echo "agent home must be absolute" >&2; exit 2 ;;
esac

/usr/bin/install -d -o root -g root -m 0755 /usr/local/libexec /usr/local/bin /usr/local/sbin
PREVIOUS_ROOTBROKER_TARGET=
if [ -e /usr/local/libexec/rootbroker-bin ] || [ -L /usr/local/libexec/rootbroker-bin ]; then
  if [ -L /usr/local/libexec/rootbroker-bin ]; then
    PREVIOUS_ROOTBROKER_TARGET=$(/usr/bin/readlink -f -- /usr/local/libexec/rootbroker-bin)
    printf '%s\n' "$PREVIOUS_ROOTBROKER_TARGET" | /bin/grep -Eq '^/usr/local/libexec/rootbroker-bin-[0-9a-f]{64}$' && \
      [ -f "$PREVIOUS_ROOTBROKER_TARGET" ] && [ -x "$PREVIOUS_ROOTBROKER_TARGET" ] || {
      echo "refusing invalid existing rootbroker binary link" >&2
      exit 2
    }
  else
    PREVIOUS_ROOTBROKER_SHA256=$(/usr/bin/sha256sum /usr/local/libexec/rootbroker-bin | /usr/bin/cut -d' ' -f1)
    PREVIOUS_ROOTBROKER_TARGET="/usr/local/libexec/rootbroker-bin-$PREVIOUS_ROOTBROKER_SHA256"
    if [ ! -e "$PREVIOUS_ROOTBROKER_TARGET" ]; then
      /usr/bin/install -o root -g root -m 0755 /usr/local/libexec/rootbroker-bin "$PREVIOUS_ROOTBROKER_TARGET"
    fi
  fi
fi
if [ -n "$PREVIOUS_ROOTBROKER_TARGET" ] && [ "$PREVIOUS_ROOTBROKER_TARGET" != "$ROOTBROKER_OBJECT" ]; then
  "$PREVIOUS_ROOTBROKER_TARGET" rootbrokerd --check-config "$TMP_DIR/config.json" || {
    echo "new configuration is incompatible with the installed rollback binary" >&2
    exit 2
  }
fi
if [ -e "$ROOTBROKER_OBJECT" ] || [ -L "$ROOTBROKER_OBJECT" ]; then
  [ ! -L "$ROOTBROKER_OBJECT" ] && [ -f "$ROOTBROKER_OBJECT" ] && \
    [ "$(/usr/bin/sha256sum "$ROOTBROKER_OBJECT" | /usr/bin/cut -d' ' -f1)" = "$ROOTBROKER_SHA256" ] || {
    echo "refusing invalid existing content-addressed rootbroker binary" >&2
    exit 2
  }
fi
/usr/bin/install -o root -g root -m 0755 "$ROOTBROKER_BIN" "$ROOTBROKER_OBJECT"
/bin/ln -sfn "$ROOTBROKER_OBJECT" /usr/local/libexec/rootbroker-bin
/bin/ln -sfn /usr/local/libexec/rootbroker-bin /usr/local/bin/rootbroker
/bin/ln -sfn /usr/local/libexec/rootbroker-bin /usr/local/bin/rootbroker-admin
/bin/ln -sfn /usr/local/libexec/rootbroker-bin /usr/local/sbin/rootbrokerd
profile_install "$AGENT_BIN" "$AGENT_USER" "$TMP_DIR"
/usr/bin/install -d -o root -g root -m 0755 /usr/local/share/rootbroker/installer/profiles/grok
/usr/bin/install -o root -g root -m 0755 "$PROJECT_DIR/uninstall.sh" /usr/local/share/rootbroker/installer/uninstall.sh
/usr/bin/install -o root -g root -m 0644 "$PROFILE_SCRIPT" /usr/local/share/rootbroker/installer/profiles/grok/profile.sh
/bin/ln -sfn /usr/local/share/rootbroker/installer/uninstall.sh /usr/local/sbin/rootbroker-uninstall

/usr/bin/install -d -o root -g root -m 0755 /etc/rootbroker /var/lib/rootbroker
/usr/bin/install -o root -g root -m 0644 "$TMP_DIR/config.json" /etc/rootbroker/config.json
{
  printf 'format=1\nprofile=%s\nagent_user=%s\napprover_user=%s\nagent_home=%s\n' \
    "$PROFILE" "$AGENT_USER" "$APPROVER_USER" "$AGENT_HOME"
  printf 'created_agent_user=%s\ncreated_agent_group=%s\ncreated_request_group=%s\ncreated_approver_group=%s\n' \
    "$CREATED_AGENT_USER" "$CREATED_AGENT_GROUP" "$CREATED_REQUEST_GROUP" "$CREATED_APPROVER_GROUP"
  printf 'agent_membership_added=%s\napprover_membership_added=%s\ninstalled_version=%s\n' \
    "$ADDED_AGENT_MEMBERSHIP" "$ADDED_APPROVER_MEMBERSHIP" "$ROOTBROKER_VERSION"
  if [ "$STATE_PRESENT" -eq 1 ]; then
    /bin/grep '^rootbroker_object=' "$STATE_PATH" || true
  fi
  if [ -n "$PREVIOUS_ROOTBROKER_TARGET" ] && [ "$PREVIOUS_ROOTBROKER_TARGET" != "$ROOTBROKER_OBJECT" ] && \
    printf '%s\n' "$PREVIOUS_ROOTBROKER_TARGET" | /bin/grep -Eq '^/usr/local/libexec/rootbroker-bin-[0-9a-f]{64}$'; then
    if [ "$STATE_PRESENT" -eq 0 ] || ! /bin/grep -Fxq "rootbroker_object=$PREVIOUS_ROOTBROKER_TARGET" "$STATE_PATH"; then
      printf 'rootbroker_object=%s\n' "$PREVIOUS_ROOTBROKER_TARGET"
    fi
  fi
  if [ "$STATE_PRESENT" -eq 0 ] || ! /bin/grep -Fxq "rootbroker_object=$ROOTBROKER_OBJECT" "$STATE_PATH"; then
    printf 'rootbroker_object=%s\n' "$ROOTBROKER_OBJECT"
  fi
} >"$TMP_DIR/install-state"
profile_install_sudoers "$TMP_DIR"
/usr/sbin/visudo -cf /etc/sudoers

/usr/bin/systemctl is-active --quiet rootbrokerd.service && SERVICE_WAS_ACTIVE=1 || SERVICE_WAS_ACTIVE=0
/usr/bin/systemctl is-enabled --quiet rootbrokerd.service && SERVICE_WAS_ENABLED=1 || SERVICE_WAS_ENABLED=0
/usr/bin/install -o root -g root -m 0644 "$PROJECT_DIR/packaging/systemd/rootbrokerd.service" /etc/systemd/system/rootbrokerd.service
/usr/bin/systemctl daemon-reload
/usr/bin/systemctl enable rootbrokerd.service
SERVICE_READY=0
if /usr/bin/systemctl restart rootbrokerd.service; then
  for _ in $(/usr/bin/seq 1 100); do
    if /usr/bin/systemctl is-active --quiet rootbrokerd.service && \
      [ -S /run/rootbroker/request.sock ] && [ -S /run/rootbroker/admin.sock ]; then
      SERVICE_READY=1
      break
    fi
    /bin/sleep 0.05
  done
fi
if [ "$SERVICE_READY" -ne 1 ]; then
  echo "new rootbrokerd failed to start" >&2
  if [ -n "$PREVIOUS_ROOTBROKER_TARGET" ] && [ -e "$PREVIOUS_ROOTBROKER_TARGET" ]; then
    echo "restoring previous rootbroker binary: $PREVIOUS_ROOTBROKER_TARGET" >&2
    /bin/ln -sfn "$PREVIOUS_ROOTBROKER_TARGET" /usr/local/libexec/rootbroker-bin
    if [ "$SERVICE_WAS_ACTIVE" -eq 1 ]; then
      /usr/bin/systemctl reset-failed rootbrokerd.service >/dev/null 2>&1 || true
      /usr/bin/systemctl restart rootbrokerd.service || true
    fi
  elif [ "$SERVICE_WAS_ENABLED" -eq 0 ]; then
    /usr/bin/systemctl disable --now rootbrokerd.service >/dev/null 2>&1 || true
  fi
  if [ "$ROOTBROKER_OBJECT" != "$PREVIOUS_ROOTBROKER_TARGET" ]; then
    /bin/rm -f -- "$ROOTBROKER_OBJECT"
  fi
  exit 1
fi
/usr/bin/install -o root -g root -m 0600 "$TMP_DIR/install-state" "$STATE_PATH"

if [ "$ALLOW_APPROVER_HOME_RW" -eq 1 ]; then
  echo "WARNING: granting $AGENT_USER read/write access to the complete home of $APPROVER_USER." >&2
  echo "WARNING: this includes SSH keys, application credentials, configuration, and personal files." >&2
  echo "WARNING: the agent may be able to impersonate $APPROVER_USER; approval is no longer a strong isolation boundary." >&2
  /usr/bin/sudo -u "$APPROVER_USER" -H -- /usr/local/bin/rootbroker-admin home-access grant
fi

echo "rootbroker installed: $(/usr/local/bin/rootbroker version)"
echo "Integration profile installed: $PROFILE"
echo "Open a second terminal and run: rootbroker-admin watch"
profile_print_next_steps
if [ "$ALLOW_APPROVER_HOME_RW" -eq 1 ]; then
  echo "Home access enabled for $AGENT_USER. Revoke it with: rootbroker-admin home-access revoke"
fi
