#!/bin/sh
set -eu

PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
AGENT_USER=grok-agent
APPROVER_USER=
GROK_BIN=

usage() {
  echo "Usage: sudo ./install.sh --approver-user USER --grok-bin PATH [--agent-user USER]" >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
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
    --grok-bin)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      GROK_BIN=$2
      shift 2
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
[ -n "$APPROVER_USER" ] && [ -n "$GROK_BIN" ] || { usage; exit 2; }

valid_user() {
  printf '%s\n' "$1" | /bin/grep -Eq '^[a-z_][a-z0-9_-]*[$]?$'
}

valid_user "$AGENT_USER" || { echo "invalid agent user name" >&2; exit 2; }
valid_user "$APPROVER_USER" || { echo "invalid approver user name" >&2; exit 2; }
/usr/bin/getent passwd "$APPROVER_USER" >/dev/null || { echo "approver user does not exist" >&2; exit 2; }
[ -f "$GROK_BIN" ] && [ -x "$GROK_BIN" ] || { echo "Grok binary is not an executable file" >&2; exit 2; }
GROK_BIN=$(/usr/bin/readlink -f -- "$GROK_BIN")

/usr/bin/getent group hostctl-agent >/dev/null || /usr/sbin/groupadd --system hostctl-agent
/usr/bin/getent group hostctl-approver >/dev/null || /usr/sbin/groupadd --system hostctl-approver
if ! /usr/bin/getent passwd "$AGENT_USER" >/dev/null; then
  /usr/sbin/useradd --create-home --shell /usr/sbin/nologin --user-group "$AGENT_USER"
fi
[ "$(/usr/bin/id -u "$AGENT_USER")" -ne 0 ] || { echo "agent user must not be root" >&2; exit 2; }
/usr/sbin/usermod -a -G hostctl-agent "$AGENT_USER"
/usr/sbin/usermod -a -G hostctl-approver "$APPROVER_USER"

/usr/bin/install -d -o root -g root -m 0755 /usr/local/lib/hostctl/hostctl
/usr/bin/install -o root -g root -m 0644 "$PROJECT_DIR"/src/hostctl/*.py /usr/local/lib/hostctl/hostctl/
/usr/bin/install -o root -g root -m 0755 "$PROJECT_DIR/packaging/bin/hostctl" /usr/local/bin/hostctl
/usr/bin/install -o root -g root -m 0755 "$PROJECT_DIR/packaging/bin/hostctl-admin" /usr/local/bin/hostctl-admin
/usr/bin/install -o root -g root -m 0755 "$PROJECT_DIR/packaging/bin/hostctld" /usr/local/sbin/hostctld
/usr/bin/install -d -o root -g root -m 0755 /usr/local/libexec
/usr/bin/install -o root -g root -m 0755 "$PROJECT_DIR/packaging/bin/hostctl-grok-hook" /usr/local/libexec/hostctl-grok-hook
/usr/bin/install -o root -g root -m 0755 "$PROJECT_DIR/packaging/bin/grok-agent-launch" /usr/local/libexec/grok-agent-launch
/usr/bin/install -o root -g root -m 0755 "$GROK_BIN" /usr/local/libexec/grok-hostctl-bin

TMP_DIR=$(/usr/bin/mktemp -d)
trap '/bin/rm -rf -- "$TMP_DIR"' EXIT HUP INT TERM
/bin/sed "s/@AGENT_USER@/$AGENT_USER/g; s/@APPROVER_USER@/$APPROVER_USER/g" \
  "$PROJECT_DIR/packaging/config/config.json.in" >"$TMP_DIR/config.json"
/bin/sed "s/@AGENT_USER@/$AGENT_USER/g" "$PROJECT_DIR/packaging/bin/grok-safe.in" >"$TMP_DIR/grok-safe"
/usr/bin/install -d -o root -g root -m 0755 /etc/hostctl
/usr/bin/install -o root -g root -m 0644 "$TMP_DIR/config.json" /etc/hostctl/config.json
/usr/bin/install -o root -g root -m 0755 "$TMP_DIR/grok-safe" /usr/local/bin/grok-safe

/usr/bin/install -d -o root -g root -m 0755 /usr/local/share/hostctl/grok
/usr/bin/install -o root -g root -m 0644 "$PROJECT_DIR/grok/rules/hostctl.md" /usr/local/share/hostctl/grok/hostctl.md
/bin/cp -R "$PROJECT_DIR/grok/skills/hostctl-admin" /usr/local/share/hostctl/grok/
/bin/chown -R root:root /usr/local/share/hostctl/grok/hostctl-admin
/bin/chmod -R go-w /usr/local/share/hostctl/grok/hostctl-admin

AGENT_HOME=$(/usr/bin/getent passwd "$AGENT_USER" | /usr/bin/cut -d: -f6)
/usr/bin/install -d -o "$AGENT_USER" -g "$AGENT_USER" -m 0700 "$AGENT_HOME/.grok"
/usr/bin/install -d -o root -g root -m 0755 "$AGENT_HOME/.grok/hooks"
/usr/bin/install -o root -g root -m 0644 "$PROJECT_DIR/grok/hooks/hostctl.json" "$AGENT_HOME/.grok/hooks/hostctl.json"
/usr/bin/install -d -o "$AGENT_USER" -g "$AGENT_USER" -m 0755 "$AGENT_HOME/.grok/skills"
if [ -e "$AGENT_HOME/.grok/skills/hostctl-admin" ] && [ ! -L "$AGENT_HOME/.grok/skills/hostctl-admin" ]; then
  echo "refusing to replace existing $AGENT_HOME/.grok/skills/hostctl-admin" >&2
  exit 1
fi
/bin/ln -sfn /usr/local/share/hostctl/grok/hostctl-admin "$AGENT_HOME/.grok/skills/hostctl-admin"

/usr/bin/install -d -o root -g root -m 0755 /etc/grok
if [ ! -e /etc/grok/managed_config.toml ]; then
  /usr/bin/install -o root -g root -m 0644 /dev/null /etc/grok/managed_config.toml
fi
if ! /bin/grep -Fq '# BEGIN hostctl managed hooks' /etc/grok/managed_config.toml; then
  printf '\n' >>/etc/grok/managed_config.toml
  /bin/sed -n '1,$p' "$PROJECT_DIR/grok/managed-hooks.toml" >>/etc/grok/managed_config.toml
fi
/bin/chown root:root /etc/grok/managed_config.toml
/bin/chmod 0644 /etc/grok/managed_config.toml

{
  echo "%hostctl-approver ALL=($AGENT_USER) NOPASSWD: /usr/local/libexec/grok-agent-launch *"
} >"$TMP_DIR/hostctl-grok-agent"
/usr/sbin/visudo -cf "$TMP_DIR/hostctl-grok-agent"
/usr/bin/install -o root -g root -m 0440 "$TMP_DIR/hostctl-grok-agent" /etc/sudoers.d/hostctl-grok-agent
/usr/sbin/visudo -cf /etc/sudoers

/usr/bin/install -o root -g root -m 0644 "$PROJECT_DIR/packaging/systemd/hostctld.service" /etc/systemd/system/hostctld.service
/usr/bin/systemctl daemon-reload
/usr/bin/systemctl enable --now hostctld.service

echo "hostctl installed."
echo "Open a second terminal and run: hostctl-admin watch"
echo "Launch the isolated Grok account with: grok-safe"
echo "The Grok account may require its own one-time login. Do not copy another user's auth files."
