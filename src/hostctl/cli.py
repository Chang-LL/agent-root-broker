from __future__ import annotations

import argparse
import json
import os
import sys

from .client import ClientError, call


DEFAULT_SOCKET = os.environ.get("HOSTCTL_SOCKET", "/run/hostctl/request.sock")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="hostctl", description="submit a host command for human approval")
    parser.add_argument("--socket", default=DEFAULT_SOCKET, help=argparse.SUPPRESS)
    subparsers = parser.add_subparsers(dest="operation", required=True)
    sudo = subparsers.add_parser("sudo", help="request an approval-gated root command")
    sudo.add_argument("--json", action="store_true", help="emit one stable JSON object")
    sudo.add_argument("--timeout", type=int, dest="timeout_seconds")
    sudo.add_argument("command", nargs=argparse.REMAINDER)
    return parser


def _print_error(message: str, as_json: bool, code: str = "client_error") -> None:
    if as_json:
        print(json.dumps({"ok": False, "error": {"code": code, "message": message}}, ensure_ascii=False))
    else:
        print(f"hostctl: {message}", file=sys.stderr)


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    command = list(args.command)
    if command and command[0] == "--":
        command.pop(0)
    if not command:
        _print_error("missing command after 'hostctl sudo --'", args.json, "invalid_command")
        return 2
    payload: dict[str, object] = {"op": "request", "argv": command, "cwd": os.getcwd()}
    if args.timeout_seconds is not None:
        payload["timeoutSeconds"] = args.timeout_seconds
    try:
        response = call(args.socket, payload)
    except ClientError as exc:
        _print_error(str(exc), args.json)
        return 125
    if args.json:
        print(json.dumps(response, ensure_ascii=False, separators=(",", ":")))
    elif response.get("ok"):
        sys.stdout.write(str(response.get("stdout", "")))
        sys.stderr.write(str(response.get("stderr", "")))
    else:
        error = response.get("error", {})
        message = error.get("message", "request failed") if isinstance(error, dict) else "request failed"
        _print_error(str(message), False)
    if response.get("ok"):
        return int(response.get("exitCode", 1))
    error = response.get("error", {})
    code = error.get("code") if isinstance(error, dict) else None
    return 124 if code == "expired" else 126


if __name__ == "__main__":
    raise SystemExit(main())
