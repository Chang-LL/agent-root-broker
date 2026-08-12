import os
import threading
import time
import unittest
from unittest.mock import patch

from hostctl.broker import Broker, BrokerError
from hostctl.config import Config
from hostctl.executor import prepare_command
from hostctl.proc import ProcessIdentity


class BrokerTests(unittest.TestCase):
    def setUp(self):
        self.config = Config(
            require_root_owned_executable=False,
            request_ttl_seconds=2,
            message_lease_ttl_seconds=10,
            session_lease_ttl_seconds=10,
        )
        self.broker = Broker(self.config)
        self.process = ProcessIdentity(pid=4242, uid=1001, start_time=99)
        self.alive = patch("hostctl.broker.process_is_alive", return_value=True)
        self.alive.start()
        self.addCleanup(self.alive.stop)
        self.broker.handle_hook(self.process, {"hookEventName": "session_start", "sessionId": "session-a"})
        self.broker.handle_hook(self.process, {"hookEventName": "user_prompt_submit", "sessionId": "session-a"})

    def _command(self, text="hello"):
        return prepare_command(["echo", text], os.getcwd(), 5, self.config)

    def _wait_for_pending(self):
        deadline = time.monotonic() + 1
        while time.monotonic() < deadline:
            pending = self.broker.list_pending()
            if pending:
                return pending[0]
            time.sleep(0.01)
        self.fail("request did not become pending")

    def _start_request(self, text="hello"):
        result = {}

        def run():
            try:
                result["value"] = self.broker.request(self.process, self.process.uid, self._command(text))
            except Exception as exc:
                result["error"] = exc

        thread = threading.Thread(target=run)
        thread.start()
        return thread, result

    def test_command_approval_executes_exact_request(self):
        thread, result = self._start_request()
        pending = self._wait_for_pending()
        self.broker.decide(pending["id"], "approved", "command", approver_uid=1000)
        thread.join(2)
        self.assertFalse(thread.is_alive())
        self.assertEqual(result["value"]["stdout"], "hello\n")
        self.assertEqual(result["value"]["approvalScope"], "command")
        self.assertEqual(self.broker.list_leases(), [])

    def test_message_scope_applies_until_stop(self):
        thread, result = self._start_request("first")
        pending = self._wait_for_pending()
        self.broker.decide(pending["id"], "approved", "message", approver_uid=1000)
        thread.join(2)
        self.assertEqual(result["value"]["stdout"], "first\n")
        automatic = self.broker.request(self.process, self.process.uid, self._command("second"))
        self.assertEqual(automatic["stdout"], "second\n")
        self.assertEqual(automatic["approvalScope"], "message")
        self.broker.handle_hook(
            self.process,
            {"hookEventName": "stop", "sessionId": "session-a", "reason": "end_turn"},
        )
        with self.assertRaisesRegex(BrokerError, "no unique active"):
            self.broker.request(self.process, self.process.uid, self._command("third"))
        self.assertEqual(self.broker.list_leases(), [])

    def test_session_scope_survives_next_prompt(self):
        thread, result = self._start_request()
        pending = self._wait_for_pending()
        self.broker.decide(pending["id"], "approved", "session", approver_uid=1000)
        thread.join(2)
        self.assertNotIn("error", result)
        self.broker.handle_hook(
            self.process,
            {"hookEventName": "stop", "sessionId": "session-a", "reason": "end_turn"},
        )
        self.broker.handle_hook(
            self.process,
            {"hookEventName": "user_prompt_submit", "sessionId": "session-a"},
        )
        automatic = self.broker.request(self.process, self.process.uid, self._command("next-turn"))
        self.assertEqual(automatic["approvalScope"], "session")

    def test_denial_returns_error_and_no_lease(self):
        thread, result = self._start_request()
        pending = self._wait_for_pending()
        self.broker.decide(pending["id"], "denied", "command", approver_uid=1000)
        thread.join(2)
        self.assertIsInstance(result["error"], BrokerError)
        self.assertEqual(result["error"].code, "denied")
        self.assertEqual(self.broker.list_leases(), [])

    def test_new_prompt_revokes_message_lease(self):
        thread, _result = self._start_request()
        pending = self._wait_for_pending()
        self.broker.decide(pending["id"], "approved", "message", approver_uid=1000)
        thread.join(2)
        self.assertEqual(len(self.broker.list_leases()), 1)
        self.broker.handle_hook(
            self.process,
            {"hookEventName": "user_prompt_submit", "sessionId": "session-a"},
        )
        self.assertEqual(self.broker.list_leases(), [])

    def test_disconnected_pending_request_is_cancelled(self):
        disconnected = threading.Event()
        result = {}

        def run():
            try:
                result["value"] = self.broker.request(
                    self.process,
                    self.process.uid,
                    self._command(),
                    client_disconnected=disconnected.is_set,
                )
            except Exception as exc:
                result["error"] = exc

        thread = threading.Thread(target=run)
        thread.start()
        self._wait_for_pending()
        disconnected.set()
        thread.join(1)
        self.assertFalse(thread.is_alive())
        self.assertEqual(result["error"].code, "cancelled")
        self.assertEqual(self.broker.list_pending(), [])


if __name__ == "__main__":
    unittest.main()
