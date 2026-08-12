import os
import tempfile
import unittest

from hostctl.config import Config
from hostctl.server import _prepare_runtime_dir


class ServerTests(unittest.TestCase):
    def test_runtime_directory_mode_ignores_restrictive_umask(self):
        with tempfile.TemporaryDirectory() as parent:
            runtime = os.path.join(parent, "run")
            config = Config(
                runtime_dir=runtime,
                request_socket=os.path.join(runtime, "request.sock"),
                admin_socket=os.path.join(runtime, "admin.sock"),
                require_root_daemon=False,
            )
            previous_umask = os.umask(0o077)
            try:
                _prepare_runtime_dir(config)
            finally:
                os.umask(previous_umask)
            self.assertEqual(os.stat(runtime).st_mode & 0o777, 0o755)


if __name__ == "__main__":
    unittest.main()
