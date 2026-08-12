import json
import tempfile
import unittest
from pathlib import Path

from hostctl.config import Config, load_config


class ConfigTests(unittest.TestCase):
    def test_rejects_unknown_key(self):
        with self.assertRaisesRegex(ValueError, "unknown configuration keys"):
            Config.from_dict({"surprise": True})

    def test_rejects_socket_outside_runtime_dir(self):
        with self.assertRaisesRegex(ValueError, "socket paths"):
            Config.from_dict({"request_socket": "/tmp/request.sock"})

    def test_loads_tuple_fields_from_json_arrays(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory, "config.json")
            path.write_text(json.dumps({"agent_users": ["agent"], "approver_users": ["admin"]}))
            config = load_config(path)
        self.assertEqual(config.agent_users, ("agent",))
        self.assertEqual(config.approver_users, ("admin",))


if __name__ == "__main__":
    unittest.main()
