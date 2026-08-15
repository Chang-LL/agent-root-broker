# shellcheck shell=sh
# Sourced by install.sh after the profile name has passed its built-in allowlist.

PROFILE_CONTRACT_VERSION=1
PROFILE_DISPLAY_NAME="Grok Build"
PROFILE_DEFAULT_AGENT_USER=grok-agent
PROFILE_AGENT_EXECUTABLE=/usr/local/libexec/grok-hostctl-bin
PROFILE_SUDOERS_FILE=hostctl-grok-agent

profile_preflight() {
  for profile_asset in \
    "$PROFILE_DIR/bin/grok-agent-launch" \
    "$PROFILE_DIR/bin/grok-safe.in" \
    "$PROFILE_DIR/assets/hooks/hostctl.json" \
    "$PROFILE_DIR/assets/managed-hooks.toml" \
    "$PROFILE_DIR/assets/rules/hostctl.md" \
    "$PROFILE_DIR/assets/skills/hostctl-admin/SKILL.md" \
    "$PROFILE_DIR/assets/skills/hostctl-admin/agents/openai.yaml"; do
    if [ ! -f "$profile_asset" ]; then
      echo "Grok profile asset is missing: $profile_asset" >&2
      return 1
    fi
  done
}

profile_install() {
  profile_agent_bin=$1
  profile_agent_user=$2
  profile_tmp_dir=$3

  /bin/ln -sfn /usr/local/libexec/hostctl-bin /usr/local/libexec/hostctl-grok-hook
  /usr/bin/install -o root -g root -m 0755 "$PROFILE_DIR/bin/grok-agent-launch" /usr/local/libexec/grok-agent-launch
  /usr/bin/install -o root -g root -m 0755 "$profile_agent_bin" "$PROFILE_AGENT_EXECUTABLE"

  /bin/sed "s/@AGENT_USER@/$profile_agent_user/g" "$PROFILE_DIR/bin/grok-safe.in" >"$profile_tmp_dir/grok-safe"
  /usr/bin/install -o root -g root -m 0755 "$profile_tmp_dir/grok-safe" /usr/local/bin/grok-safe

  /usr/bin/install -d -o root -g root -m 0755 /usr/local/share/hostctl/grok
  /usr/bin/install -o root -g root -m 0644 "$PROFILE_DIR/assets/rules/hostctl.md" /usr/local/share/hostctl/grok/hostctl.md
  /bin/cp -R "$PROFILE_DIR/assets/skills/hostctl-admin" /usr/local/share/hostctl/grok/
  /bin/chown -R root:root /usr/local/share/hostctl/grok/hostctl-admin
  /bin/chmod -R go-w /usr/local/share/hostctl/grok/hostctl-admin

  profile_agent_home=$(/usr/bin/getent passwd "$profile_agent_user" | /usr/bin/cut -d: -f6)
  /usr/bin/install -d -o "$profile_agent_user" -g "$profile_agent_user" -m 0700 "$profile_agent_home/.grok"
  /usr/bin/install -d -o root -g root -m 0755 "$profile_agent_home/.grok/hooks"
  /usr/bin/install -o root -g root -m 0644 "$PROFILE_DIR/assets/hooks/hostctl.json" "$profile_agent_home/.grok/hooks/hostctl.json"
  /usr/bin/install -d -o "$profile_agent_user" -g "$profile_agent_user" -m 0755 "$profile_agent_home/.grok/skills"
  if [ -e "$profile_agent_home/.grok/skills/hostctl-admin" ] && [ ! -L "$profile_agent_home/.grok/skills/hostctl-admin" ]; then
    echo "refusing to replace existing $profile_agent_home/.grok/skills/hostctl-admin" >&2
    return 1
  fi
  /bin/ln -sfn /usr/local/share/hostctl/grok/hostctl-admin "$profile_agent_home/.grok/skills/hostctl-admin"

  /usr/bin/install -d -o root -g root -m 0755 /etc/grok
  if [ ! -e /etc/grok/managed_config.toml ]; then
    /usr/bin/install -o root -g root -m 0644 /dev/null /etc/grok/managed_config.toml
  fi
  if ! /bin/grep -Fq '# BEGIN hostctl managed hooks' /etc/grok/managed_config.toml; then
    printf '\n' >>/etc/grok/managed_config.toml
    /bin/sed -n '1,$p' "$PROFILE_DIR/assets/managed-hooks.toml" >>/etc/grok/managed_config.toml
  fi
  /bin/chown root:root /etc/grok/managed_config.toml
  /bin/chmod 0644 /etc/grok/managed_config.toml
}

profile_install_sudoers() {
  profile_agent_user=$1
  profile_tmp_dir=$2
  profile_sudoers_path="$profile_tmp_dir/$PROFILE_SUDOERS_FILE"

  {
    echo "%hostctl-approver ALL=($profile_agent_user) NOPASSWD: SETENV: /usr/local/libexec/grok-agent-launch *"
  } >"$profile_sudoers_path"
  /usr/sbin/visudo -cf "$profile_sudoers_path"
  /usr/bin/install -o root -g root -m 0440 "$profile_sudoers_path" "/etc/sudoers.d/$PROFILE_SUDOERS_FILE"
}

profile_print_next_steps() {
  echo "Launch the isolated Grok account with: grok-safe"
  echo "The Grok account may require its own one-time login. Do not copy another user's auth files."
}
