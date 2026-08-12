from __future__ import annotations

import json
import os
import re
import sys

from .client import ClientError, call


DEFAULT_SOCKET = os.environ.get("HOSTCTL_SOCKET", "/run/hostctl/request.sock")
SUDO_TOKEN = re.compile(r"(^|[\s;&|()])(?:/usr/bin/|/bin/)?sudo(?=$|[\s;&|()])")
HOSTCTL_PREFIX = re.compile(r"(?:^|[;&|()]\s*)(?:/usr/local/bin/)?hostctl\s+$")


def contains_direct_sudo(command: str) -> bool:
    for match in SUDO_TOKEN.finditer(command):
        sudo_position = match.start() + len(match.group(1))
        if HOSTCTL_PREFIX.search(command[:sudo_position]):
            continue
        return True
    return False


def main() -> int:
    try:
        event = json.load(sys.stdin)
    except (json.JSONDecodeError, UnicodeDecodeError):
        return 0
    if not isinstance(event, dict):
        return 0

    try:
        call(DEFAULT_SOCKET, {"op": "hook", "event": event})
    except ClientError:
        pass

    name = str(event.get("hookEventName", "")).replace("_", "").lower()
    if name == "pretooluse":
        tool_name = str(event.get("toolName", ""))
        tool_input = event.get("toolInput", {})
        command = tool_input.get("command", "") if isinstance(tool_input, dict) else ""
        if tool_name in {"Bash", "run_terminal_command"} and isinstance(command, str) and contains_direct_sudo(command):
            print(
                json.dumps(
                    {
                        "decision": "deny",
                        "reason": "Direct sudo is disabled. Use: hostctl sudo -- <program> <args>",
                    },
                    separators=(",", ":"),
                )
            )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
