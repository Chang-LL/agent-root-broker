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
	if strings.Contains(installer, `dist/hostctl-linux-`) {
		t.Fatal("source installer silently selects an ignored dist artifact")
	}
	for _, wanted := range []string{
		"source checkout detected but Go is unavailable",
		"explicit --hostctl-bin",
		"Selected hostctl:",
		"HOSTCTL_SHA256",
	} {
		if !strings.Contains(installer, wanted) {
			t.Fatalf("installer provenance output is missing %q", wanted)
		}
	}
}

func TestApproverHomeAccessIsExplicitAndACLBased(t *testing.T) {
	installer := projectFile(t, "install.sh")
	admin := projectFile(t, "internal", "commands", "admin.go")
	for _, wanted := range []string{
		"--allow-approver-home-rw",
		"hostctl-admin home-access grant",
		"agent user must be different from approver user",
	} {
		if !strings.Contains(installer, wanted) {
			t.Fatalf("installer home-access mode is missing %q", wanted)
		}
	}
	for _, wanted := range []string{"home-access", "status", "grant", "revoke"} {
		if !strings.Contains(admin, wanted) {
			t.Fatalf("admin CLI home-access mode is missing %q", wanted)
		}
	}
	for _, forbidden := range []string{
		"chmod -R g+",
		"chown -R \"$APPROVER_USER\"",
	} {
		if strings.Contains(installer, forbidden) {
			t.Fatalf("installer mutates broad Unix ownership/mode in home-access mode: %s", forbidden)
		}
	}
}
