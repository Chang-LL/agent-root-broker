# shellcheck shell=sh
# shellcheck disable=SC2034  # Contract variables are consumed by the sourcing installer.
# Sourced by install.sh after the profile name has passed its built-in allowlist.

PROFILE_CONTRACT_VERSION=2
PROFILE_DISPLAY_NAME="Grok Build"
PROFILE_DEFAULT_AGENT_USER=grok-agent
PROFILE_AGENT_EXECUTABLE=/usr/local/libexec/grok-hostctl-bin
PROFILE_SUDOERS_FILE=hostctl-grok-agent

profile_validate_managed_config() {
  profile_config_path=$1
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
  ' "$profile_config_path"
}

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

profile_prepare() {
  profile_agent_user=$1
  profile_tmp_dir=$2

  /bin/sed "s/@AGENT_USER@/$profile_agent_user/g" "$PROFILE_DIR/bin/grok-safe.in" >"$profile_tmp_dir/grok-safe"
  {
    echo "%hostctl-approver ALL=($profile_agent_user) NOPASSWD: SETENV: /usr/local/libexec/grok-agent-launch *"
  } >"$profile_tmp_dir/$PROFILE_SUDOERS_FILE"
  /usr/sbin/visudo -cf "$profile_tmp_dir/$PROFILE_SUDOERS_FILE"

  : >"$profile_tmp_dir/managed_config.toml"
  if [ -f /etc/grok/managed_config.toml ]; then
    /bin/cp -- /etc/grok/managed_config.toml "$profile_tmp_dir/managed_config.toml"
  fi
  if ! profile_validate_managed_config "$profile_tmp_dir/managed_config.toml"; then
    echo "refusing malformed hostctl block in /etc/grok/managed_config.toml" >&2
    return 1
  fi
  profile_begin_count=$(/bin/grep -Fxc '# BEGIN hostctl managed hooks' "$profile_tmp_dir/managed_config.toml" || true)
  if [ "$profile_begin_count" -eq 0 ]; then
    printf '\n' >>"$profile_tmp_dir/managed_config.toml"
    /bin/sed -n '1,$p' "$PROFILE_DIR/assets/managed-hooks.toml" >>"$profile_tmp_dir/managed_config.toml"
  fi
}

profile_install() {
  profile_agent_bin=$1
  profile_agent_user=$2
  profile_tmp_dir=$3

  profile_agent_group=$(/usr/bin/id -gn "$profile_agent_user")

  /bin/ln -sfn /usr/local/libexec/hostctl-bin /usr/local/libexec/hostctl-grok-hook
  /usr/bin/install -o root -g root -m 0755 "$PROFILE_DIR/bin/grok-agent-launch" /usr/local/libexec/grok-agent-launch
  /usr/bin/install -o root -g root -m 0755 "$profile_agent_bin" "$PROFILE_AGENT_EXECUTABLE"

  /usr/bin/install -o root -g root -m 0755 "$profile_tmp_dir/grok-safe" /usr/local/bin/grok-safe

  /usr/bin/install -d -o root -g root -m 0755 /usr/local/share/hostctl/grok
  /usr/bin/install -o root -g root -m 0644 "$PROFILE_DIR/assets/rules/hostctl.md" /usr/local/share/hostctl/grok/hostctl.md
  /bin/cp -R "$PROFILE_DIR/assets/skills/hostctl-admin" /usr/local/share/hostctl/grok/
  /bin/chown -R root:root /usr/local/share/hostctl/grok/hostctl-admin
  /bin/chmod -R go-w /usr/local/share/hostctl/grok/hostctl-admin

  profile_agent_home=$(/usr/bin/getent passwd "$profile_agent_user" | /usr/bin/cut -d: -f6)
  /usr/bin/install -d -o "$profile_agent_user" -g "$profile_agent_group" -m 0700 "$profile_agent_home/.grok"
  /usr/bin/install -d -o root -g root -m 0755 "$profile_agent_home/.grok/hooks"
  /usr/bin/install -o root -g root -m 0644 "$PROFILE_DIR/assets/hooks/hostctl.json" "$profile_agent_home/.grok/hooks/hostctl.json"
  /usr/bin/install -d -o "$profile_agent_user" -g "$profile_agent_group" -m 0755 "$profile_agent_home/.grok/skills"
  if [ -e "$profile_agent_home/.grok/skills/hostctl-admin" ] && [ ! -L "$profile_agent_home/.grok/skills/hostctl-admin" ]; then
    echo "refusing to replace existing $profile_agent_home/.grok/skills/hostctl-admin" >&2
    return 1
  fi
  /bin/ln -sfn /usr/local/share/hostctl/grok/hostctl-admin "$profile_agent_home/.grok/skills/hostctl-admin"

  /usr/bin/install -d -o root -g root -m 0755 /etc/grok
  /usr/bin/install -o root -g root -m 0644 "$profile_tmp_dir/managed_config.toml" /etc/grok/managed_config.toml
}

profile_install_sudoers() {
  profile_tmp_dir=$1
  profile_sudoers_path="$profile_tmp_dir/$PROFILE_SUDOERS_FILE"

  /usr/bin/install -o root -g root -m 0440 "$profile_sudoers_path" "/etc/sudoers.d/$PROFILE_SUDOERS_FILE"
}

profile_uninstall() {
  profile_agent_home=$1
  profile_tmp_dir=$2

  if [ -f /etc/grok/managed_config.toml ] && ! profile_validate_managed_config /etc/grok/managed_config.toml; then
    echo "refusing malformed hostctl block in /etc/grok/managed_config.toml" >&2
    return 1
  fi

  /bin/rm -f -- \
    /usr/local/bin/grok-safe \
    /usr/local/libexec/hostctl-grok-hook \
    /usr/local/libexec/grok-agent-launch \
    "$PROFILE_AGENT_EXECUTABLE" \
    "$profile_agent_home/.grok/hooks/hostctl.json" \
    "$profile_agent_home/.grok/skills/hostctl-admin"
  /bin/rm -rf -- /usr/local/share/hostctl/grok

  if [ -f /etc/grok/managed_config.toml ]; then
    /usr/bin/awk '
      $0 == "# BEGIN hostctl managed hooks" { managed = 1; next }
      $0 == "# END hostctl managed hooks" { managed = 0; next }
      !managed { print }
    ' /etc/grok/managed_config.toml >"$profile_tmp_dir/managed_config.toml"
    if /bin/grep -q '[^[:space:]]' "$profile_tmp_dir/managed_config.toml"; then
      /usr/bin/install -o root -g root -m 0644 "$profile_tmp_dir/managed_config.toml" /etc/grok/managed_config.toml
    else
      /bin/rm -f -- /etc/grok/managed_config.toml
    fi
  fi
}

profile_print_next_steps() {
  echo "Launch the isolated Grok account with: grok-safe"
  echo "The Grok account may require its own one-time login. Do not copy another user's auth files."
}
