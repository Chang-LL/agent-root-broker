# Integration profiles

Integration profiles contain the installation behavior that belongs to one agent product. They are
not dynamically loaded plugins: `install.sh` selects only names in its built-in allowlist, then
sources the matching reviewed profile file with root authority.

Contract version 2 requires these metadata variables:

- `PROFILE_CONTRACT_VERSION`
- `PROFILE_DISPLAY_NAME`
- `PROFILE_DEFAULT_AGENT_USER`
- `PROFILE_AGENT_EXECUTABLE`
- `PROFILE_SUDOERS_FILE`

It also requires six functions:

- `profile_preflight`: validate every profile asset before the core installer changes the system;
- `profile_prepare AGENT_USER TMP_DIR`: render and validate profile-specific files without changing
  the system;
- `profile_install AGENT_BIN AGENT_USER TMP_DIR`: install the agent executable, launcher, hooks, and
  managed assets;
- `profile_install_sudoers TMP_DIR`: install the already validated profile-specific rule;
- `profile_uninstall AGENT_HOME TMP_DIR`: remove only profile-owned files and managed blocks;
- `profile_print_next_steps`: print profile-specific launch or authentication guidance.

The core installer owns rootbroker identities/groups, the rootbroker binary and multicall links, daemon
configuration, systemd, and optional home ACLs. A profile must be idempotent, keep installed control
files root-owned, grant no root sudo rule to the agent, avoid network activity, and fail closed when
an expected asset or invariant is missing.

Adding a profile requires a code-reviewed allowlist entry in `install.sh`, tests that its vendor
paths do not leak into the core installer, release packaging coverage, and a full Linux system test.
