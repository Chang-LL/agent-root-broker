package commands

import "testing"

func TestContainsDirectSudo(t *testing.T) {
	for _, command := range []string{
		"sudo id", "/usr/bin/sudo id", "echo ok && sudo reboot", "rootbroker sudo -- id && sudo reboot",
	} {
		if !ContainsDirectSudo(command) {
			t.Errorf("did not detect direct sudo in %q", command)
		}
	}
	for _, command := range []string{
		"rootbroker sudo -- id", "/usr/local/bin/rootbroker sudo -- id", "echo sudowoodo", "printf sudoers",
		"grep sudo /etc/group", "rootbroker sudo -- grep sudo /etc/group",
	} {
		if ContainsDirectSudo(command) {
			t.Errorf("false positive in %q", command)
		}
	}
}
