import unittest

from hostctl.hook_cli import contains_direct_sudo


class HookTests(unittest.TestCase):
    def test_detects_direct_sudo(self):
        for command in ("sudo mount /dev/x /mnt/x", "/usr/bin/sudo id", "echo ok && sudo id"):
            with self.subTest(command=command):
                self.assertTrue(contains_direct_sudo(command))

    def test_does_not_mistake_hostctl_or_text_for_direct_sudo(self):
        for command in (
            "hostctl sudo -- mount /dev/x /mnt/x",
            "/usr/local/bin/hostctl sudo -- id",
            "echo sudowoodo",
            "printf sudoers",
        ):
            with self.subTest(command=command):
                self.assertFalse(contains_direct_sudo(command))

    def test_detects_direct_sudo_after_hostctl_request(self):
        self.assertTrue(contains_direct_sudo("hostctl sudo -- id && sudo reboot"))


if __name__ == "__main__":
    unittest.main()
