from __future__ import annotations

import hashlib
import json
import os
import shutil
import signal
import stat
import subprocess
import threading
import time
from dataclasses import dataclass

from .config import Config


BLOCKED_EXECUTABLES = {"hostctl", "hostctl-admin", "hostctld", "pkexec", "su", "sudo"}
INTERPRETERS = {
    "bash", "dash", "fish", "node", "perl", "python", "python3", "ruby", "sh", "zsh",
}


class CommandError(ValueError):
    pass


@dataclass(frozen=True)
class CommandSpec:
    argv: tuple[str, ...]
    cwd: str
    timeout_seconds: int
    digest: str
    risks: tuple[str, ...]

    def public(self) -> dict[str, object]:
        return {
            "argv": list(self.argv),
            "cwd": self.cwd,
            "timeoutSeconds": self.timeout_seconds,
            "hash": self.digest,
            "risks": list(self.risks),
        }


def _is_within(path: str, roots: tuple[str, ...]) -> bool:
    for root in roots:
        normalized_root = os.path.realpath(root)
        try:
            if os.path.commonpath((path, normalized_root)) == normalized_root:
                return True
        except ValueError:
            continue
    return False


def _require_protected_path(path: str) -> None:
    current = os.path.dirname(path)
    while True:
        try:
            current_stat = os.stat(current)
        except OSError as exc:
            raise CommandError(f"cannot stat executable parent: {current}") from exc
        if current_stat.st_uid != 0 or current_stat.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
            raise CommandError(f"executable parent must be root-owned and non-writable: {current}")
        parent = os.path.dirname(current)
        if parent == current:
            break
        current = parent


def prepare_command(argv: list[str], cwd: str, timeout_seconds: int | None, config: Config) -> CommandSpec:
    if not argv or not all(isinstance(item, str) and "\x00" not in item for item in argv):
        raise CommandError("command must be a non-empty argv array without NUL bytes")
    if len(argv) > 256 or sum(len(item) for item in argv) > 65_536:
        raise CommandError("command is too large")

    requested_executable = argv[0]
    if "/" in requested_executable:
        candidate = requested_executable if os.path.isabs(requested_executable) else os.path.join(cwd, requested_executable)
        executable = os.path.realpath(candidate)
    else:
        found = shutil.which(requested_executable, path=config.clean_path)
        if not found:
            raise CommandError(f"executable not found in broker PATH: {requested_executable}")
        executable = os.path.realpath(found)

    basename = os.path.basename(executable)
    if basename in BLOCKED_EXECUTABLES:
        raise CommandError(f"recursive or privilege-wrapper executable is not allowed: {basename}")
    try:
        executable_stat = os.stat(executable)
    except OSError as exc:
        raise CommandError(f"cannot stat executable: {executable}") from exc
    if not stat.S_ISREG(executable_stat.st_mode) or not os.access(executable, os.X_OK):
        raise CommandError(f"not an executable regular file: {executable}")
    if config.require_root_owned_executable:
        if executable_stat.st_uid != 0:
            raise CommandError("executable must be owned by root")
        if executable_stat.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
            raise CommandError("executable must not be group- or world-writable")
        _require_protected_path(executable)

    real_cwd = os.path.realpath(cwd)
    if not os.path.isdir(real_cwd):
        raise CommandError(f"working directory does not exist: {real_cwd}")
    if not _is_within(real_cwd, config.allowed_cwd_roots):
        raise CommandError("working directory is outside allowed_cwd_roots")

    timeout = config.default_timeout_seconds if timeout_seconds is None else timeout_seconds
    if not isinstance(timeout, int) or timeout <= 0 or timeout > config.max_timeout_seconds:
        raise CommandError(f"timeout must be between 1 and {config.max_timeout_seconds} seconds")

    resolved_argv = (executable, *argv[1:])
    risks: list[str] = []
    if basename in INTERPRETERS:
        risks.append("interpreter-or-shell")
    if any(item in {"-c", "--command", "-m"} for item in argv[1:]):
        risks.append("executes-inline-or-module-code")
    if any("*" in item or "?" in item for item in argv[1:]):
        risks.append("wildcard-is-literal-without-shell")
    canonical = json.dumps(
        {"argv": resolved_argv, "cwd": real_cwd, "timeoutSeconds": timeout},
        ensure_ascii=False,
        separators=(",", ":"),
    ).encode("utf-8")
    digest = hashlib.sha256(canonical).hexdigest()
    return CommandSpec(resolved_argv, real_cwd, timeout, digest, tuple(risks))


def _drain_limited(stream: object, limit: int, result: dict[str, object]) -> None:
    captured = bytearray()
    truncated = False
    try:
        while True:
            chunk = stream.read(65_536)
            if not chunk:
                break
            remaining = limit - len(captured)
            if remaining > 0:
                captured.extend(chunk[:remaining])
            if len(chunk) > remaining:
                truncated = True
    except (OSError, ValueError):
        pass
    result["text"] = captured.decode("utf-8", errors="replace")
    result["truncated"] = truncated


def execute_command(spec: CommandSpec, config: Config) -> dict[str, object]:
    environment = {
        "HOME": "/root",
        "LANG": "C.UTF-8",
        "LC_ALL": "C.UTF-8",
        "LOGNAME": "root",
        "PATH": config.clean_path,
        "SHELL": "/bin/sh",
        "USER": "root",
    }
    started = time.monotonic()
    timed_out = False
    try:
        process = subprocess.Popen(
            spec.argv,
            cwd=spec.cwd,
            env=environment,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            start_new_session=True,
        )
    except OSError as exc:
        return {
            "exitCode": 126,
            "stdout": "",
            "stderr": f"hostctl: execution failed: {exc}\n",
            "timedOut": False,
            "durationMs": round((time.monotonic() - started) * 1000),
            "stdoutTruncated": False,
            "stderrTruncated": False,
        }
    stdout_result: dict[str, object] = {}
    stderr_result: dict[str, object] = {}
    stdout_thread = threading.Thread(
        target=_drain_limited,
        args=(process.stdout, config.max_output_bytes, stdout_result),
        daemon=True,
    )
    stderr_thread = threading.Thread(
        target=_drain_limited,
        args=(process.stderr, config.max_output_bytes, stderr_result),
        daemon=True,
    )
    stdout_thread.start()
    stderr_thread.start()
    try:
        exit_code = process.wait(timeout=spec.timeout_seconds)
    except subprocess.TimeoutExpired:
        timed_out = True
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait()
        exit_code = 124
    stdout_thread.join(timeout=1)
    stderr_thread.join(timeout=1)
    if stdout_thread.is_alive() or stderr_thread.is_alive():
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        stdout_thread.join(timeout=1)
        stderr_thread.join(timeout=1)
    if process.stdout:
        process.stdout.close()
    if process.stderr:
        process.stderr.close()
    stdout = str(stdout_result.get("text", ""))
    stderr = str(stderr_result.get("text", ""))
    stdout_truncated = bool(stdout_result.get("truncated", False))
    stderr_truncated = bool(stderr_result.get("truncated", False))
    if timed_out:
        stderr += f"hostctl: command timed out after {spec.timeout_seconds}s\n"
    return {
        "exitCode": exit_code,
        "stdout": stdout,
        "stderr": stderr,
        "timedOut": timed_out,
        "durationMs": round((time.monotonic() - started) * 1000),
        "stdoutTruncated": stdout_truncated,
        "stderrTruncated": stderr_truncated,
    }
