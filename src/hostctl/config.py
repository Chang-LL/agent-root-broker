from __future__ import annotations

import json
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any


DEFAULT_CONFIG_PATH = "/etc/hostctl/config.json"


@dataclass(frozen=True)
class Config:
    runtime_dir: str = "/run/hostctl"
    request_socket: str = "/run/hostctl/request.sock"
    admin_socket: str = "/run/hostctl/admin.sock"
    request_group: str = "hostctl-agent"
    admin_group: str = "hostctl-approver"
    agent_users: tuple[str, ...] = ("grok-agent",)
    approver_users: tuple[str, ...] = ()
    agent_executables: tuple[str, ...] = ("/usr/local/libexec/grok-hostctl-bin",)
    allowed_cwd_roots: tuple[str, ...] = ("/",)
    clean_path: str = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    default_timeout_seconds: int = 300
    max_timeout_seconds: int = 900
    max_output_bytes: int = 1_048_576
    request_ttl_seconds: int = 300
    message_lease_ttl_seconds: int = 900
    session_lease_ttl_seconds: int = 14_400
    require_root_daemon: bool = True
    require_root_owned_executable: bool = True
    log_argv: bool = False

    @classmethod
    def from_dict(cls, raw: dict[str, Any]) -> "Config":
        known = set(cls.__dataclass_fields__)
        unknown = sorted(set(raw) - known)
        if unknown:
            raise ValueError(f"unknown configuration keys: {', '.join(unknown)}")
        values = dict(raw)
        tuple_fields = {
            "agent_users",
            "approver_users",
            "agent_executables",
            "allowed_cwd_roots",
        }
        for field in tuple_fields:
            if field in values:
                value = values[field]
                if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
                    raise ValueError(f"{field} must be an array of strings")
                values[field] = tuple(value)
        config = cls(**values)
        config.validate()
        return config

    def validate(self) -> None:
        if not self.agent_users:
            raise ValueError("agent_users must not be empty")
        if not self.agent_executables or any(not os.path.isabs(path) for path in self.agent_executables):
            raise ValueError("agent_executables must contain absolute paths")
        for value in (
            self.default_timeout_seconds,
            self.max_timeout_seconds,
            self.max_output_bytes,
            self.request_ttl_seconds,
            self.message_lease_ttl_seconds,
            self.session_lease_ttl_seconds,
        ):
            if value <= 0:
                raise ValueError("timeouts and output limits must be positive")
        if self.default_timeout_seconds > self.max_timeout_seconds:
            raise ValueError("default timeout cannot exceed maximum timeout")
        for socket_path in (self.request_socket, self.admin_socket):
            if os.path.dirname(socket_path) != os.path.normpath(self.runtime_dir):
                raise ValueError("socket paths must be directly inside runtime_dir")
        if not os.path.isabs(self.runtime_dir):
            raise ValueError("runtime_dir must be absolute")
        if not self.allowed_cwd_roots or any(not os.path.isabs(path) for path in self.allowed_cwd_roots):
            raise ValueError("allowed_cwd_roots must contain absolute paths")


def load_config(path: str | os.PathLike[str] = DEFAULT_CONFIG_PATH) -> Config:
    config_path = Path(path)
    try:
        raw = json.loads(config_path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise ValueError(f"configuration not found: {config_path}") from exc
    except json.JSONDecodeError as exc:
        raise ValueError(f"invalid JSON in {config_path}: {exc}") from exc
    if not isinstance(raw, dict):
        raise ValueError("configuration root must be an object")
    return Config.from_dict(raw)
