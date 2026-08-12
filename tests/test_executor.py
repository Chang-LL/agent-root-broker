import os
import tempfile
import unittest

from hostctl.config import Config
from hostctl.executor import CommandError, execute_command, prepare_command


class ExecutorTests(unittest.TestCase):
    def setUp(self):
        self.config = Config(require_root_owned_executable=False)

    def test_resolves_and_executes_without_shell(self):
        command = prepare_command(["echo", "hello; not-a-shell"], os.getcwd(), 5, self.config)
        result = execute_command(command, self.config)
        self.assertEqual(result["exitCode"], 0)
        self.assertEqual(result["stdout"], "hello; not-a-shell\n")

    def test_rejects_privilege_wrappers(self):
        with self.assertRaisesRegex(CommandError, "not allowed"):
            prepare_command(["sudo", "true"], os.getcwd(), 5, self.config)

    def test_rejects_out_of_range_timeout(self):
        with self.assertRaisesRegex(CommandError, "timeout"):
            prepare_command(["echo", "hello"], os.getcwd(), self.config.max_timeout_seconds + 1, self.config)

    def test_restricts_working_directory_roots(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Config(require_root_owned_executable=False, allowed_cwd_roots=(directory,))
            with self.assertRaisesRegex(CommandError, "outside"):
                prepare_command(["echo", "hello"], "/", 5, config)

    def test_flags_shell_and_hash_is_stable(self):
        first = prepare_command(["sh", "-c", "id"], os.getcwd(), 5, self.config)
        second = prepare_command(["sh", "-c", "id"], os.getcwd(), 5, self.config)
        self.assertIn("interpreter-or-shell", first.risks)
        self.assertIn("executes-inline-or-module-code", first.risks)
        self.assertEqual(first.digest, second.digest)

    def test_captured_output_is_bounded(self):
        config = Config(require_root_owned_executable=False, max_output_bytes=4)
        command = prepare_command(["echo", "123456789"], os.getcwd(), 5, config)
        result = execute_command(command, config)
        self.assertEqual(result["stdout"], "1234")
        self.assertTrue(result["stdoutTruncated"])


if __name__ == "__main__":
    unittest.main()
