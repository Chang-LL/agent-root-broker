import unittest
from pathlib import Path


PROJECT = Path(__file__).resolve().parents[1]


class PackagingTests(unittest.TestCase):
    def test_grok_safe_preserves_only_proxy_environment(self):
        wrapper = (PROJECT / "packaging" / "bin" / "grok-safe.in").read_text(encoding="utf-8")
        self.assertIn("--preserve-env=HTTP_PROXY,HTTPS_PROXY,ALL_PROXY,NO_PROXY", wrapper)
        self.assertNotIn("sudo -E", wrapper)
        self.assertNotIn("XAI_API_KEY", wrapper)

    def test_sudoers_rule_is_setenv_but_not_root_target(self):
        installer = (PROJECT / "install.sh").read_text(encoding="utf-8")
        self.assertIn("NOPASSWD: SETENV: /usr/local/libexec/grok-agent-launch *", installer)
        self.assertIn('ALL=($AGENT_USER)', installer)


if __name__ == "__main__":
    unittest.main()
