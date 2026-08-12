from __future__ import annotations

import json
import logging
import threading
import time
import uuid
from dataclasses import dataclass, field
from typing import Callable

from .config import Config
from .executor import CommandSpec, execute_command
from .proc import ProcessIdentity, process_is_alive


LOG = logging.getLogger("hostctl.audit")


@dataclass
class SessionState:
    process: ProcessIdentity
    session_id: str
    turn: int = 0
    active: bool = False
    updated_at: float = field(default_factory=time.time)


@dataclass
class PendingRequest:
    request_id: str
    process: ProcessIdentity
    session_id: str
    turn: int
    agent_uid: int
    command: CommandSpec
    created_at: float
    expires_at: float
    decision: str | None = None
    scope: str | None = None
    approver_uid: int | None = None
    condition: threading.Condition | None = field(default=None, repr=False)

    def public(self, include_argv: bool = True) -> dict[str, object]:
        command = self.command.public()
        if not include_argv:
            command["argv"] = [self.command.argv[0], "<redacted>"]
        return {
            "id": self.request_id,
            "sessionId": self.session_id,
            "turn": self.turn,
            "agentUid": self.agent_uid,
            "process": self.process.key,
            "createdAt": self.created_at,
            "expiresAt": self.expires_at,
            "decision": self.decision,
            "scope": self.scope,
            "command": command,
        }


@dataclass
class Lease:
    lease_id: str
    scope: str
    process: ProcessIdentity
    session_id: str
    turn: int | None
    approver_uid: int
    created_at: float
    expires_at: float

    def matches(self, session: SessionState, now: float) -> bool:
        if self.expires_at <= now or self.process != session.process or self.session_id != session.session_id:
            return False
        if self.scope == "message":
            return session.active and self.turn == session.turn
        return self.scope == "session"

    def public(self) -> dict[str, object]:
        return {
            "id": self.lease_id,
            "scope": self.scope,
            "process": self.process.key,
            "sessionId": self.session_id,
            "turn": self.turn,
            "approverUid": self.approver_uid,
            "createdAt": self.created_at,
            "expiresAt": self.expires_at,
        }


class BrokerError(RuntimeError):
    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code


class Broker:
    def __init__(self, config: Config):
        self.config = config
        self._lock = threading.RLock()
        self._sessions: dict[tuple[ProcessIdentity, str], SessionState] = {}
        self._pending: dict[str, PendingRequest] = {}
        self._leases: dict[str, Lease] = {}

    def _audit(self, event: str, **fields: object) -> None:
        payload = {"event": event, **fields}
        LOG.info("%s", json.dumps(payload, ensure_ascii=False, separators=(",", ":")))

    @staticmethod
    def _event_name(event: dict[str, object]) -> str:
        raw = str(event.get("hookEventName", ""))
        return raw.replace("_", "").lower()

    def handle_hook(self, process: ProcessIdentity, event: dict[str, object]) -> None:
        session_id = str(event.get("sessionId", ""))
        if not session_id or len(session_id) > 256:
            raise BrokerError("invalid_hook", "hook event is missing a valid sessionId")
        name = self._event_name(event)
        key = (process, session_id)
        now = time.time()
        with self._lock:
            self._prune_locked(now)
            if name == "sessionstart":
                self._sessions[key] = SessionState(process, session_id, updated_at=now)
            elif name == "userpromptsubmit":
                state = self._sessions.setdefault(key, SessionState(process, session_id))
                self._cancel_pending_locked(process, session_id, "new-turn")
                state.turn += 1
                state.active = True
                state.updated_at = now
                self._revoke_message_leases_locked(process, session_id)
            elif name in {"stop", "stopfailure"}:
                state = self._sessions.get(key)
                if state and (name == "stopfailure" or event.get("reason") == "end_turn"):
                    state.active = False
                    state.updated_at = now
                    self._revoke_message_leases_locked(process, session_id)
                    self._cancel_pending_locked(process, session_id, "turn-ended")
            elif name == "sessionend":
                self._remove_session_locked(process, session_id, "session-end")
            self._audit("hook", hook=name, session=session_id, process=process.key)

    def _active_session_locked(self, process: ProcessIdentity) -> SessionState:
        candidates = [state for (identity, _), state in self._sessions.items() if identity == process and state.active]
        if len(candidates) != 1:
            raise BrokerError(
                "no_active_turn",
                "no unique active Grok turn; start Grok through grok-safe and submit a prompt",
            )
        return candidates[0]

    def request(
        self,
        process: ProcessIdentity,
        agent_uid: int,
        command: CommandSpec,
        client_disconnected: Callable[[], bool] | None = None,
    ) -> dict[str, object]:
        now = time.time()
        pending: PendingRequest | None = None
        with self._lock:
            self._prune_locked(now)
            session = self._active_session_locked(process)
            lease = next((item for item in self._leases.values() if item.matches(session, now)), None)
            if lease is None:
                request_id = uuid.uuid4().hex[:16]
                pending = PendingRequest(
                    request_id=request_id,
                    process=process,
                    session_id=session.session_id,
                    turn=session.turn,
                    agent_uid=agent_uid,
                    command=command,
                    created_at=now,
                    expires_at=now + self.config.request_ttl_seconds,
                )
                pending.condition = threading.Condition(self._lock)
                self._pending[request_id] = pending
                self._audit(
                    "request-created",
                    request=request_id,
                    command_hash=command.digest,
                    executable=command.argv[0],
                    argv=list(command.argv) if self.config.log_argv else None,
                    session=session.session_id,
                    turn=session.turn,
                )
                while pending.decision is None:
                    remaining = pending.expires_at - time.time()
                    if remaining <= 0:
                        pending.decision = "expired"
                        break
                    if client_disconnected and client_disconnected():
                        pending.decision = "cancelled"
                        break
                    pending.condition.wait(timeout=min(remaining, 0.25))
                self._pending.pop(request_id, None)
                if pending.decision != "approved":
                    decision = pending.decision or "denied"
                    raise BrokerError(decision, f"request {request_id} was {decision}")
                approved_by = pending.approver_uid
                scope = pending.scope
            else:
                approved_by = lease.approver_uid
                scope = lease.scope
                request_id = None

        if client_disconnected and client_disconnected():
            raise BrokerError("cancelled", "requesting client disconnected before execution")
        if not process_is_alive(process):
            raise BrokerError("cancelled", "agent process exited before execution")

        self._audit(
            "execution-started",
            request=request_id,
            command_hash=command.digest,
            scope=scope,
            approver_uid=approved_by,
        )
        result = execute_command(command, self.config)
        self._audit(
            "execution-finished",
            request=request_id,
            command_hash=command.digest,
            exit_code=result["exitCode"],
            duration_ms=result["durationMs"],
        )
        return {
            "ok": True,
            "requestId": request_id,
            "approvalScope": scope,
            "commandHash": command.digest,
            **result,
        }

    def list_pending(self) -> list[dict[str, object]]:
        with self._lock:
            self._prune_locked(time.time())
            return [item.public() for item in sorted(self._pending.values(), key=lambda value: value.created_at)]

    def list_leases(self) -> list[dict[str, object]]:
        with self._lock:
            self._prune_locked(time.time())
            return [item.public() for item in sorted(self._leases.values(), key=lambda value: value.created_at)]

    def decide(self, request_id: str, decision: str, scope: str, approver_uid: int) -> None:
        if decision not in {"approved", "denied"}:
            raise BrokerError("invalid_decision", "decision must be approved or denied")
        if scope not in {"command", "message", "session"}:
            raise BrokerError("invalid_scope", "scope must be command, message, or session")
        now = time.time()
        with self._lock:
            self._prune_locked(now)
            pending = self._pending.get(request_id)
            if not pending or pending.decision is not None:
                raise BrokerError("not_found", f"pending request not found: {request_id}")
            pending.decision = decision
            pending.scope = "command" if decision == "denied" else scope
            pending.approver_uid = approver_uid
            if decision == "approved" and scope in {"message", "session"}:
                ttl = (
                    self.config.message_lease_ttl_seconds
                    if scope == "message"
                    else self.config.session_lease_ttl_seconds
                )
                lease = Lease(
                    lease_id=uuid.uuid4().hex[:16],
                    scope=scope,
                    process=pending.process,
                    session_id=pending.session_id,
                    turn=pending.turn if scope == "message" else None,
                    approver_uid=approver_uid,
                    created_at=now,
                    expires_at=now + ttl,
                )
                self._leases[lease.lease_id] = lease
                for other in self._pending.values():
                    same_context = (
                        other.process == pending.process
                        and other.session_id == pending.session_id
                        and (scope == "session" or other.turn == pending.turn)
                    )
                    if same_context and other.decision is None:
                        other.decision = "approved"
                        other.scope = scope
                        other.approver_uid = approver_uid
                        if other.condition:
                            other.condition.notify_all()
            if pending.condition:
                pending.condition.notify_all()
            self._audit(
                "request-decided",
                request=request_id,
                decision=decision,
                scope=pending.scope,
                approver_uid=approver_uid,
                command_hash=pending.command.digest,
            )

    def revoke(self, lease_id: str, approver_uid: int) -> None:
        with self._lock:
            if not self._leases.pop(lease_id, None):
                raise BrokerError("not_found", f"lease not found: {lease_id}")
            self._audit("lease-revoked", lease=lease_id, approver_uid=approver_uid)

    def _revoke_message_leases_locked(self, process: ProcessIdentity, session_id: str) -> None:
        for lease_id, lease in list(self._leases.items()):
            if lease.scope == "message" and lease.process == process and lease.session_id == session_id:
                self._leases.pop(lease_id, None)

    def _remove_session_locked(self, process: ProcessIdentity, session_id: str, reason: str) -> None:
        self._sessions.pop((process, session_id), None)
        for lease_id, lease in list(self._leases.items()):
            if lease.process == process and lease.session_id == session_id:
                self._leases.pop(lease_id, None)
        self._cancel_pending_locked(process, session_id, reason)
        self._audit("session-removed", session=session_id, process=process.key, reason=reason)

    def _cancel_pending_locked(self, process: ProcessIdentity, session_id: str, reason: str) -> None:
        for pending in self._pending.values():
            if pending.process == process and pending.session_id == session_id and pending.decision is None:
                pending.decision = "cancelled"
                if pending.condition:
                    pending.condition.notify_all()
                self._audit("request-cancelled", request=pending.request_id, reason=reason)

    def _prune_locked(self, now: float) -> None:
        for lease_id, lease in list(self._leases.items()):
            if lease.expires_at <= now or not process_is_alive(lease.process):
                self._leases.pop(lease_id, None)
        for (process, session_id), _state in list(self._sessions.items()):
            if not process_is_alive(process):
                self._remove_session_locked(process, session_id, "process-exited")
        for pending in self._pending.values():
            if pending.decision is None and pending.expires_at <= now:
                pending.decision = "expired"
                if pending.condition:
                    pending.condition.notify_all()
