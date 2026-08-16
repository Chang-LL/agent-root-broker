# Compatibility

## Public alpha support matrix

| Area | Supported | Notes |
| --- | --- | --- |
| Operating system | Ubuntu 24.04 with systemd | This is the clean-host CI target for the first alpha. |
| Architecture | Linux amd64 | Full privileged end-to-end CI. |
| Architecture | Linux arm64 | Static cross-build is verified; privileged end-to-end coverage is pending. |
| Init/security APIs | systemd, Unix sockets, `SO_PEERCRED`, `/proc` | Required by the current daemon and installer. |
| Agent integration | Grok Build with lifecycle hooks | Grok is the only shipped profile; upstream versions are not yet pinned as a compatibility promise. |
| Home access | Local Linux filesystem with POSIX ACL and xattr support | Optional; symlinks and filesystem boundaries are not traversed. |

Other systemd distributions may work, but are not supported until their complete install, approval,
upgrade, and uninstall path runs in CI. The script installer currently relies on standard Ubuntu
locations and GNU user/group, `stat`, `readlink`, `sha256sum`, sudoers, and systemd tools.

Containers, WSL without a normal systemd host, macOS, non-systemd Linux, network filesystems, and
rootless environments are not supported deployment targets. The daemon intentionally has no
non-Linux fallback because kernel peer credentials and `/proc` process identity are part of its
security model.

The JSON interface is documented for automation, but remains alpha: additive fields may appear and
incompatible changes may occur before v1. Stable compatibility and migration policy is tracked in
[ROADMAP.md](ROADMAP.md).
