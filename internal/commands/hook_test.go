package commands

import "testing"

func TestContainsDirectSudo(t *testing.T) {
	for _, command := range []string{
		"sudo id", "/usr/bin/sudo id", "echo ok && sudo reboot", "hostctl sudo -- id && sudo reboot",
	} {
		if !ContainsDirectSudo(command) {
			t.Errorf("did not detect direct sudo in %q", command)
		}
	}
	for _, command := range []string{
		"hostctl sudo -- id", "/usr/local/bin/hostctl sudo -- id", "echo sudowoodo", "printf sudoers",
		"grep sudo /etc/group", "hostctl sudo -- grep sudo /etc/group",
	} {
		if ContainsDirectSudo(command) {
			t.Errorf("false positive in %q", command)
		}
	}
}
