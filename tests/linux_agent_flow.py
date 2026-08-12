"""Helper process for integration_linux.py; run beneath the configured trusted shell."""

from __future__ import annotations

import json
import os
import sys

from hostctl.client import call


def main() -> int:
    socket_path = sys.argv[1]
    session_id = "integration-session"
    call(socket_path, {"op": "hook", "event": {"hookEventName": "session_start", "sessionId": session_id}})
    call(
        socket_path,
        {"op": "hook", "event": {"hookEventName": "user_prompt_submit", "sessionId": session_id}},
    )
    first = call(socket_path, {"op": "request", "argv": ["id", "-u"], "cwd": os.getcwd()})
    second = call(socket_path, {"op": "request", "argv": ["echo", "lease-ok"], "cwd": os.getcwd()})
    call(
        socket_path,
        {
            "op": "hook",
            "event": {"hookEventName": "stop", "sessionId": session_id, "reason": "end_turn"},
        },
    )
    after_stop = call(socket_path, {"op": "request", "argv": ["true"], "cwd": os.getcwd()})
    print(json.dumps({"first": first, "second": second, "afterStop": after_stop}, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
