from __future__ import annotations

import argparse
import grp
import json
import logging
import os
import pwd
import signal
import select
import socket
import socketserver
import struct
import sys
import threading
from pathlib import Path
from typing import Any

from .broker import Broker, BrokerError
from .config import Config, DEFAULT_CONFIG_PATH, load_config
from .executor import CommandError, prepare_command
from .proc import ProcessIdentity, find_agent_process


MAX_REQUEST_BYTES = 256 * 1024


def _peer_credentials(connection: socket.socket) -> tuple[int, int, int]:
    if not hasattr(socket, "SO_PEERCRED"):
        raise RuntimeError("hostctld requires Linux SO_PEERCRED")
    raw = connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, struct.calcsize("3i"))
    return struct.unpack("3i", raw)


def _username(uid: int) -> str:
    try:
        return pwd.getpwuid(uid).pw_name
    except KeyError:
        return ""


def _user_in_group(uid: int, group_name: str) -> bool:
    try:
        user = pwd.getpwuid(uid)
        group = grp.getgrnam(group_name)
    except KeyError:
        return False
    return user.pw_gid == group.gr_gid or user.pw_name in group.gr_mem


def _authorized_agent(uid: int, config: Config) -> bool:
    return _username(uid) in config.agent_users and _user_in_group(uid, config.request_group)


def _authorized_approver(uid: int, config: Config) -> bool:
    name = _username(uid)
    return (not config.approver_users or name in config.approver_users) and _user_in_group(uid, config.admin_group)


def _error(code: str, message: str) -> dict[str, object]:
    return {"ok": False, "error": {"code": code, "message": message}}


class HostctlServer(socketserver.ThreadingUnixStreamServer):
    daemon_threads = True
    allow_reuse_address = False

    def __init__(self, path: str, broker: Broker, config: Config, plane: str):
        self.broker = broker
        self.config = config
        self.plane = plane
        super().__init__(path, HostctlHandler)


class HostctlHandler(socketserver.StreamRequestHandler):
    server: HostctlServer

    def handle(self) -> None:
        try:
            _peer_pid, peer_uid, _peer_gid = _peer_credentials(self.connection)
            raw = self.rfile.readline(MAX_REQUEST_BYTES + 1)
            if len(raw) > MAX_REQUEST_BYTES or not raw.endswith(b"\n"):
                response = _error("invalid_request", "request is too large or missing newline terminator")
            else:
                payload = json.loads(raw)
                if not isinstance(payload, dict):
                    raise ValueError("request root must be an object")
                response = self._dispatch(payload, _peer_pid, peer_uid)
        except json.JSONDecodeError:
            response = _error("invalid_json", "request is not valid JSON")
        except (BrokerError, CommandError) as exc:
            response = _error(getattr(exc, "code", "invalid_command"), str(exc))
        except Exception:
            logging.getLogger("hostctl").exception("unhandled request failure")
            response = _error("internal_error", "internal broker error")
        self.wfile.write(json.dumps(response, ensure_ascii=False, separators=(",", ":")).encode("utf-8") + b"\n")

    def _dispatch(self, payload: dict[str, Any], peer_pid: int, peer_uid: int) -> dict[str, object]:
        if self.server.plane == "request":
            if not _authorized_agent(peer_uid, self.server.config):
                raise BrokerError("unauthorized", "peer is not an authorized agent user")
            process = find_agent_process(peer_pid, peer_uid, self.server.config.agent_executables)
            if process is None:
                raise BrokerError("not_agent_child", "request is not descended from an approved agent process")
            operation = payload.get("op")
            if operation == "hook":
                event = payload.get("event")
                if not isinstance(event, dict):
                    raise BrokerError("invalid_hook", "event must be an object")
                self.server.broker.handle_hook(process, event)
                return {"ok": True}
            if operation == "request":
                argv = payload.get("argv")
                cwd = payload.get("cwd")
                timeout = payload.get("timeoutSeconds")
                if not isinstance(argv, list) or not all(isinstance(item, str) for item in argv):
                    raise CommandError("argv must be an array of strings")
                if not isinstance(cwd, str):
                    raise CommandError("cwd must be a string")
                if timeout is not None and (isinstance(timeout, bool) or not isinstance(timeout, int)):
                    raise CommandError("timeoutSeconds must be an integer")
                command = prepare_command(argv, cwd, timeout, self.server.config)
                return self.server.broker.request(process, peer_uid, command, self._client_disconnected)
            raise BrokerError("invalid_operation", "unknown request-plane operation")

        if not _authorized_approver(peer_uid, self.server.config):
            raise BrokerError("unauthorized", "peer is not an authorized approver")
        operation = payload.get("op")
        if operation == "pending":
            return {"ok": True, "pending": self.server.broker.list_pending()}
        if operation == "leases":
            return {"ok": True, "leases": self.server.broker.list_leases()}
        if operation == "decide":
            request_id = payload.get("requestId")
            decision = payload.get("decision")
            scope = payload.get("scope", "command")
            if not all(isinstance(item, str) for item in (request_id, decision, scope)):
                raise BrokerError("invalid_request", "requestId, decision, and scope must be strings")
            self.server.broker.decide(request_id, decision, scope, peer_uid)
            return {"ok": True}
        if operation == "revoke":
            lease_id = payload.get("leaseId")
            if not isinstance(lease_id, str):
                raise BrokerError("invalid_request", "leaseId must be a string")
            self.server.broker.revoke(lease_id, peer_uid)
            return {"ok": True}
        raise BrokerError("invalid_operation", "unknown admin-plane operation")

    def _client_disconnected(self) -> bool:
        try:
            readable, _, _ = select.select((self.connection,), (), (), 0)
            return bool(readable) and self.connection.recv(1, socket.MSG_PEEK) == b""
        except OSError:
            return True


def _prepare_socket(path: str, group_name: str, mode: int) -> None:
    group = grp.getgrnam(group_name)
    os.chown(path, 0 if os.geteuid() == 0 else os.geteuid(), group.gr_gid)
    os.chmod(path, mode)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="hostctl approval broker daemon")
    parser.add_argument("--config", default=DEFAULT_CONFIG_PATH)
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        config = load_config(args.config)
    except ValueError as exc:
        print(f"hostctld: {exc}", file=sys.stderr)
        return 2
    if config.require_root_daemon and os.geteuid() != 0:
        print("hostctld: must run as root", file=sys.stderr)
        return 2

    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s %(levelname)s %(message)s")
    Path(config.runtime_dir).mkdir(mode=0o755, parents=True, exist_ok=True)
    for path in (config.request_socket, config.admin_socket):
        try:
            os.unlink(path)
        except FileNotFoundError:
            pass

    broker = Broker(config)
    request_server = HostctlServer(config.request_socket, broker, config, "request")
    admin_server = HostctlServer(config.admin_socket, broker, config, "admin")
    try:
        _prepare_socket(config.request_socket, config.request_group, 0o660)
        _prepare_socket(config.admin_socket, config.admin_group, 0o660)
    except Exception:
        request_server.server_close()
        admin_server.server_close()
        for path in (config.request_socket, config.admin_socket):
            try:
                os.unlink(path)
            except FileNotFoundError:
                pass
        raise

    stopping = threading.Event()

    def stop(_signum: int, _frame: object) -> None:
        stopping.set()

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    threads = [
        threading.Thread(target=request_server.serve_forever, name="request-server", daemon=True),
        threading.Thread(target=admin_server.serve_forever, name="admin-server", daemon=True),
    ]
    for thread in threads:
        thread.start()
    logging.getLogger("hostctl").info("hostctld started")
    stopping.wait()
    request_server.shutdown()
    admin_server.shutdown()
    request_server.server_close()
    admin_server.server_close()
    for path in (config.request_socket, config.admin_socket):
        try:
            os.unlink(path)
        except FileNotFoundError:
            pass
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
