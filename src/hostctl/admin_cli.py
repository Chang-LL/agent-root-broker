from __future__ import annotations

import argparse
import json
import os
import shlex
import sys
import time
from typing import Any

from .client import ClientError, call


DEFAULT_SOCKET = os.environ.get("HOSTCTL_ADMIN_SOCKET", "/run/hostctl/admin.sock")


def _request(socket_path: str, payload: dict[str, object]) -> dict[str, Any]:
    response = call(socket_path, payload)
    if not response.get("ok"):
        error = response.get("error", {})
        message = error.get("message", "request failed") if isinstance(error, dict) else "request failed"
        raise ClientError(str(message))
    return response


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="hostctl-admin", description="review and decide hostctl requests")
    parser.add_argument("--socket", default=DEFAULT_SOCKET, help=argparse.SUPPRESS)
    parser.add_argument("--json", action="store_true", help="emit JSON for non-interactive commands")
    subparsers = parser.add_subparsers(dest="operation", required=True)
    subparsers.add_parser("pending", help="list pending requests")
    subparsers.add_parser("leases", help="list active message/session approvals")
    approve = subparsers.add_parser("approve", help="approve one request")
    approve.add_argument("request_id")
    approve.add_argument("--scope", choices=("command", "message", "session"), default="command")
    deny = subparsers.add_parser("deny", help="deny one request")
    deny.add_argument("request_id")
    revoke = subparsers.add_parser("revoke", help="revoke an active lease")
    revoke.add_argument("lease_id")
    watch = subparsers.add_parser("watch", help="interactively wait for and review requests")
    watch.add_argument("--interval", type=float, default=1.0)
    return parser


def _format_request(item: dict[str, Any]) -> str:
    command = item.get("command", {})
    argv = command.get("argv", []) if isinstance(command, dict) else []
    quoted = " ".join(shlex.quote(str(arg)) for arg in argv)
    risks = ", ".join(command.get("risks", [])) if isinstance(command, dict) else ""
    return "\n".join(
        (
            f"request: {item.get('id')}",
            f"session: {item.get('sessionId')}  turn: {item.get('turn')}",
            f"command: {quoted}",
            f"cwd: {command.get('cwd') if isinstance(command, dict) else ''}",
            f"timeout: {command.get('timeoutSeconds') if isinstance(command, dict) else ''}s",
            f"hash: {command.get('hash') if isinstance(command, dict) else ''}",
            f"risks: {risks or 'none detected'}",
        )
    )


def _watch(socket_path: str, interval: float) -> int:
    if interval < 0.1:
        print("hostctl-admin: --interval must be at least 0.1", file=sys.stderr)
        return 2
    print("Waiting for hostctl requests. Ctrl+C quits.")
    seen: set[str] = set()
    while True:
        response = _request(socket_path, {"op": "pending"})
        pending = response.get("pending", [])
        current = next((item for item in pending if str(item.get("id")) not in seen), None)
        if current is None:
            time.sleep(interval)
            continue
        request_id = str(current["id"])
        print("\n" + _format_request(current))
        while True:
            choice = input("Approve [c]ommand, [m]essage, [s]ession; [d]eny; [l]ater; [q]uit? ").strip().lower()
            if choice in {"c", "m", "s"}:
                scope = {"c": "command", "m": "message", "s": "session"}[choice]
                _request(
                    socket_path,
                    {"op": "decide", "requestId": request_id, "decision": "approved", "scope": scope},
                )
                print(f"Approved {request_id} for {scope} scope.")
                break
            if choice == "d":
                _request(
                    socket_path,
                    {"op": "decide", "requestId": request_id, "decision": "denied", "scope": "command"},
                )
                print(f"Denied {request_id}.")
                break
            if choice == "l":
                seen.add(request_id)
                break
            if choice == "q":
                return 0


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        if args.operation == "watch":
            return _watch(args.socket, args.interval)
        if args.operation in {"pending", "leases"}:
            response = _request(args.socket, {"op": args.operation})
            values = response[args.operation]
            if args.json:
                print(json.dumps(response, ensure_ascii=False, separators=(",", ":")))
            elif args.operation == "pending":
                if not values:
                    print("No pending requests.")
                for item in values:
                    print(_format_request(item) + "\n")
            else:
                if not values:
                    print("No active leases.")
                for item in values:
                    print(
                        f"{item['id']}  scope={item['scope']}  session={item['sessionId']}  "
                        f"turn={item['turn']}  expires={time.strftime('%Y-%m-%d %H:%M:%S', time.localtime(item['expiresAt']))}"
                    )
            return 0
        if args.operation == "approve":
            response = _request(
                args.socket,
                {"op": "decide", "requestId": args.request_id, "decision": "approved", "scope": args.scope},
            )
        elif args.operation == "deny":
            response = _request(
                args.socket,
                {"op": "decide", "requestId": args.request_id, "decision": "denied", "scope": "command"},
            )
        else:
            response = _request(args.socket, {"op": "revoke", "leaseId": args.lease_id})
        if args.json:
            print(json.dumps(response, ensure_ascii=False, separators=(",", ":")))
        return 0
    except KeyboardInterrupt:
        print("\nStopped.")
        return 130
    except ClientError as exc:
        if args.json:
            print(json.dumps({"ok": False, "error": {"code": "client_error", "message": str(exc)}}))
        else:
            print(f"hostctl-admin: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
