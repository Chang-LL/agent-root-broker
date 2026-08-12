package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func projectFile(t *testing.T, elements ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{".."}, elements...)...)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestGrokSafePreservesOnlyProxyEnvironment(t *testing.T) {
	wrapper := projectFile(t, "packaging", "bin", "grok-safe.in")
	if !strings.Contains(wrapper, "--preserve-env=HTTP_PROXY,HTTPS_PROXY,ALL_PROXY,NO_PROXY") {
		t.Fatal("proxy allowlist is missing")
	}
	for _, forbidden := range []string{"sudo -E", "XAI_API_KEY"} {
		if strings.Contains(wrapper, forbidden) {
			t.Fatalf("wrapper contains forbidden environment behavior: %s", forbidden)
		}
	}
}

func TestInstallerUsesUnprivilegedSETENVTargetAndStaticBinary(t *testing.T) {
	installer := projectFile(t, "install.sh")
	for _, wanted := range []string{
		"NOPASSWD: SETENV: /usr/local/libexec/grok-agent-launch *",
		"ALL=($AGENT_USER)",
		"/usr/local/libexec/hostctl-bin",
	} {
		if !strings.Contains(installer, wanted) {
			t.Fatalf("installer is missing %q", wanted)
		}
	}
	if strings.Contains(installer, "src/hostctl") || strings.Contains(installer, "python") {
		t.Fatal("installer still depends on the Python implementation")
	}
}
