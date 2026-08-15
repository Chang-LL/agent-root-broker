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
	wrapper := projectFile(t, "profiles", "grok", "bin", "grok-safe.in")
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
	profile := projectFile(t, "profiles", "grok", "profile.sh")
	for _, wanted := range []string{
		"NOPASSWD: SETENV: /usr/local/libexec/grok-agent-launch *",
		"ALL=($profile_agent_user)",
	} {
		if !strings.Contains(profile, wanted) {
			t.Fatalf("Grok profile is missing %q", wanted)
		}
	}
	for _, wanted := range []string{
		"/usr/local/libexec/hostctl-bin",
		"profile_install_sudoers",
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

func TestInstallerKeepsGrokDetailsBehindProfile(t *testing.T) {
	installer := projectFile(t, "install.sh")
	profile := projectFile(t, "profiles", "grok", "profile.sh")
	configTemplate := projectFile(t, "packaging", "config", "config.json.in")
	for _, detail := range []string{
		"/etc/grok", "managed_config.toml", "hostctl-grok-hook", "grok-agent-launch", "grok-safe",
	} {
		if strings.Contains(installer, detail) {
			t.Fatalf("Grok detail %q leaked into core installer", detail)
		}
		if !strings.Contains(profile, detail) {
			t.Fatalf("Grok profile is missing detail %q", detail)
		}
	}
	for _, wanted := range []string{
		"PROFILE_CONTRACT_VERSION=1", "profile_preflight", "profile_install", "profile_install_sudoers",
	} {
		if !strings.Contains(profile, wanted) {
			t.Fatalf("Grok profile contract is missing %q", wanted)
		}
	}
	for _, wanted := range []string{"case \"$PROFILE\" in", "unsupported integration profile", "PROFILE_CONTRACT_VERSION"} {
		if !strings.Contains(installer, wanted) {
			t.Fatalf("core installer profile guard is missing %q", wanted)
		}
	}
	if !strings.Contains(configTemplate, "@AGENT_EXECUTABLE@") || strings.Contains(configTemplate, "grok-hostctl-bin") {
		t.Fatal("core configuration template still owns a Grok executable path")
	}
	for _, legacyPath := range [][]string{{"grok"}, {"packaging", "bin"}} {
		if _, err := os.Stat(filepath.Join(append([]string{".."}, legacyPath...)...)); !os.IsNotExist(err) {
			t.Fatalf("legacy integration path still exists: %s", filepath.Join(legacyPath...))
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

func TestDocumentationShipsEnglishAndChinese(t *testing.T) {
	readmeEnglish := projectFile(t, "README.md")
	readmeChinese := projectFile(t, "README.zh-CN.md")
	roadmapEnglish := projectFile(t, "ROADMAP.md")
	roadmapChinese := projectFile(t, "ROADMAP.zh-CN.md")
	release := projectFile(t, ".github", "workflows", "release.yml")
	if !strings.Contains(release, "cp -R profiles") {
		t.Fatal("release archive omits integration profiles")
	}

	for name, item := range map[string]struct {
		document  string
		alternate string
	}{
		"README.md":        {readmeEnglish, "README.zh-CN.md"},
		"README.zh-CN.md":  {readmeChinese, "README.md"},
		"ROADMAP.md":       {roadmapEnglish, "ROADMAP.zh-CN.md"},
		"ROADMAP.zh-CN.md": {roadmapChinese, "ROADMAP.md"},
	} {
		firstLine := strings.SplitN(item.document, "\n", 2)[0]
		if !strings.Contains(firstLine, "]("+item.alternate+")") {
			t.Fatalf("%s is missing its alternate language link", name)
		}
	}

	for _, filename := range []string{"README.md", "README.zh-CN.md", "ROADMAP.md", "ROADMAP.zh-CN.md"} {
		if !strings.Contains(release, filename) {
			t.Fatalf("release archive omits %s", filename)
		}
	}
	if strings.Count(roadmapEnglish, "- [x]") != strings.Count(roadmapChinese, "- [x]") ||
		strings.Count(roadmapEnglish, "- [ ]") != strings.Count(roadmapChinese, "- [ ]") {
		t.Fatal("English and Chinese roadmap checklist states are out of sync")
	}
}

func TestVendorPayloadsStayBehindIntegrationAdapters(t *testing.T) {
	adapter := projectFile(t, "internal", "integrations", "grok", "adapter.go")
	for _, field := range []string{"hookEventName", "toolName", "toolInput", `raw["reason"]`} {
		if !strings.Contains(adapter, field) {
			t.Fatalf("Grok adapter no longer owns expected vendor field %q", field)
		}
		for _, corePath := range [][]string{{"internal", "broker", "broker.go"}, {"internal", "server", "server_linux.go"}} {
			if strings.Contains(projectFile(t, corePath...), field) {
				t.Fatalf("vendor field %q leaked into %s", field, filepath.Join(corePath...))
			}
		}
	}
	if !strings.Contains(projectFile(t, "internal", "agent", "lifecycle.go"), "type HookAdapter interface") {
		t.Fatal("agent hook adapter boundary is missing")
	}
	if !strings.Contains(projectFile(t, "internal", "approval", "provider.go"), "type Provider interface") {
		t.Fatal("approval provider boundary is missing")
	}
}
