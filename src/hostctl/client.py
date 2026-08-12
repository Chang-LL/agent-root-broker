from __future__ import annotations

import json
import socket
from typing import Any


MAX_RESPONSE_BYTES = 4 * 1024 * 1024


class ClientError(RuntimeError):
    pass


def call(socket_path: str, payload: dict[str, object]) -> dict[str, Any]:
    encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8") + b"\n"
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
            connection.connect(socket_path)
            connection.sendall(encoded)
            chunks: list[bytes] = []
            size = 0
            while True:
                chunk = connection.recv(65_536)
                if not chunk:
                    break
                size += len(chunk)
                if size > MAX_RESPONSE_BYTES:
                    raise ClientError("broker response exceeded size limit")
                chunks.append(chunk)
                if b"\n" in chunk:
                    break
    except OSError as exc:
        raise ClientError(f"cannot reach hostctl broker at {socket_path}: {exc}") from exc
    raw = b"".join(chunks).split(b"\n", 1)[0]
    try:
        response = json.loads(raw)
    except (json.JSONDecodeError, UnicodeDecodeError) as exc:
        raise ClientError("broker returned invalid JSON") from exc
    if not isinstance(response, dict):
        raise ClientError("broker returned a non-object response")
    return response
