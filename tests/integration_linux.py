"""No-root Linux integration test for sockets, lifecycle, approval, lease, and execution."""

from __future__ import annotations

import getpass
import grp
import json
import os
import platform
import subprocess
import sys
import tempfile
import time
from pathlib import Path

from hostctl.client import call


def wait_for_path(path: Path, process: subprocess.Popen[str]) -> None:
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        if path.exists():
            return
        if process.poll() is not None:
            raise RuntimeError(f"hostctld exited early: {process.stderr.read()}")
        time.sleep(0.05)
    raise RuntimeError(f"timed out waiting for {path}")


def main() -> int:
    if platform.system() != "Linux":
        print("SKIP: integration test requires Linux SO_PEERCRED")
        return 0
    project = Path(__file__).resolve().parents[1]
    environment = dict(os.environ)
    environment["PYTHONPATH"] = str(project / "src")
    username = getpass.getuser()
    group_name = grp.getgrgid(os.getgid()).gr_name
    with tempfile.TemporaryDirectory(prefix="hostctl-integration-") as directory:
        runtime = Path(directory)
        config_path = runtime / "config.json"
        request_socket = runtime / "request.sock"
        admin_socket = runtime / "admin.sock"
        config = {
            "runtime_dir": str(runtime),
            "request_socket": str(request_socket),
            "admin_socket": str(admin_socket),
            "request_group": group_name,
            "admin_group": group_name,
            "agent_users": [username],
            "approver_users": [username],
            "agent_executables": [os.path.realpath("/bin/sh")],
            "require_root_daemon": False,
            "require_root_owned_executable": True,
            "request_ttl_seconds": 5,
        }
        config_path.write_text(json.dumps(config), encoding="utf-8")
        daemon = subprocess.Popen(
            [sys.executable, "-m", "hostctl.server", "--config", str(config_path)],
            cwd=project,
            env=environment,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        try:
            wait_for_path(request_socket, daemon)
            helper = project / "tests" / "linux_agent_flow.py"
            agent = subprocess.Popen(
                ["/bin/sh", "-c", '"$1" "$2" "$3"', "hostctl-test", sys.executable, str(helper), str(request_socket)],
                cwd=project,
                env=environment,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
            )
            deadline = time.monotonic() + 5
            pending = []
            while time.monotonic() < deadline:
                pending = call(str(admin_socket), {"op": "pending"}).get("pending", [])
                if pending:
                    break
                time.sleep(0.05)
            if not pending:
                raise RuntimeError("agent request did not become pending")
            call(
                str(admin_socket),
                {
                    "op": "decide",
                    "requestId": pending[0]["id"],
                    "decision": "approved",
                    "scope": "message",
                },
            )
            stdout, stderr = agent.communicate(timeout=10)
            if agent.returncode != 0:
                raise RuntimeError(f"agent flow failed: {stderr}")
            result = json.loads(stdout)
            assert result["first"]["ok"] and result["first"]["approvalScope"] == "message"
            assert result["second"]["ok"] and result["second"]["stdout"] == "lease-ok\n"
            assert not result["afterStop"]["ok"]
            assert result["afterStop"]["error"]["code"] == "no_active_turn"
            print("PASS: Linux socket integration")
            return 0
        finally:
            daemon.terminate()
            try:
                daemon.wait(timeout=5)
            except subprocess.TimeoutExpired:
                daemon.kill()
                daemon.wait()


if __name__ == "__main__":
    raise SystemExit(main())
