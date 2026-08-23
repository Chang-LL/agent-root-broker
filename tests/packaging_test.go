package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrokManagedConfigMarkersFailClosed(t *testing.T) {
	profile, err := filepath.Abs(filepath.Join("..", "profiles", "grok", "profile.sh"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		content string
		valid   bool
	}{
		{name: "absent", content: "other = true\n", valid: true},
		{name: "managed", content: "# BEGIN rootbroker managed hooks\nitem = true\n# END rootbroker managed hooks\n", valid: true},
		{name: "unclosed", content: "# BEGIN rootbroker managed hooks\n", valid: false},
		{name: "reversed", content: "# END rootbroker managed hooks\n# BEGIN rootbroker managed hooks\n", valid: false},
		{name: "duplicate", content: "# BEGIN rootbroker managed hooks\n# END rootbroker managed hooks\n# BEGIN rootbroker managed hooks\n# END rootbroker managed hooks\n", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "managed_config.toml")
			if err := os.WriteFile(config, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("/bin/sh", "-c", `. "$1"; profile_validate_managed_config "$2"`, "rootbroker-profile-test", profile, config)
			err := command.Run()
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
		})
	}
}

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
	uninstaller := projectFile(t, "uninstall.sh")
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
		"/usr/local/libexec/rootbroker-bin",
		"/usr/local/sbin/rootbroker-uninstall",
		"ROOTBROKER_OBJECT",
		"rootbrokerd --check-config",
		"profile_install_sudoers",
	} {
		if !strings.Contains(installer, wanted) {
			t.Fatalf("installer is missing %q", wanted)
		}
	}
	for _, wanted := range []string{"rootbroker-maint", "profile_uninstall", "--purge-agent-account", "Agent home data is always preserved"} {
		if !strings.Contains(uninstaller, wanted) {
			t.Fatalf("uninstaller is missing %q", wanted)
		}
	}
	if strings.Contains(installer, "src/rootbroker") || strings.Contains(installer, "python") {
		t.Fatal("installer still depends on the Python implementation")
	}
	if strings.Contains(installer, `dist/rootbroker-linux-`) {
		t.Fatal("source installer silently selects an ignored dist artifact")
	}
	for _, wanted := range []string{
		"source checkout detected but Go is unavailable",
		"explicit --rootbroker-bin",
		"Selected rootbroker:",
		"ROOTBROKER_SHA256",
		"/var/lib/hostctl/install-state",
		"See MIGRATION.md",
	} {
		if !strings.Contains(installer, wanted) {
			t.Fatalf("installer provenance output is missing %q", wanted)
		}
	}
}

func TestStatelessPreAlphaMigrationFailsClosed(t *testing.T) {
	migration := projectFile(t, "migrate-private-prealpha.sh")
	for _, wanted := range []string{
		`"hostctl 0.2.0-dev"*`,
		"secure_root_file",
		"exact_symlink",
		"group_has_only",
		"unsupported legacy broker version",
		"agent account still has running processes",
		"hostctl-admin home-access revoke",
		"systemctl disable --now hostctld.service",
		"Preserved agent account and home",
		"CHECK_ONLY=1",
	} {
		if !strings.Contains(migration, wanted) {
			t.Fatalf("stateless migration is missing safety behavior %q", wanted)
		}
	}
	for _, forbidden := range []string{"eval ", "userdel", `rm -rf -- "$AGENT_HOME"`} {
		if strings.Contains(migration, forbidden) {
			t.Fatalf("stateless migration contains forbidden behavior %q", forbidden)
		}
	}
}

func TestInstallerKeepsGrokDetailsBehindProfile(t *testing.T) {
	installer := projectFile(t, "install.sh")
	profile := projectFile(t, "profiles", "grok", "profile.sh")
	configTemplate := projectFile(t, "packaging", "config", "config.json.in")
	for _, detail := range []string{
		"/etc/grok", "managed_config.toml", "rootbroker-grok-hook", "grok-agent-launch", "grok-safe",
	} {
		if strings.Contains(installer, detail) {
			t.Fatalf("Grok detail %q leaked into core installer", detail)
		}
		if !strings.Contains(profile, detail) {
			t.Fatalf("Grok profile is missing detail %q", detail)
		}
	}
	for _, wanted := range []string{
		"PROFILE_CONTRACT_VERSION=2", "profile_preflight", "profile_prepare", "profile_install", "profile_install_sudoers", "profile_uninstall",
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
	if !strings.Contains(configTemplate, "@AGENT_EXECUTABLE@") || strings.Contains(configTemplate, "grok-rootbroker-bin") {
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
		"rootbroker-admin home-access grant",
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
	releasing := projectFile(t, "RELEASING.md")
	if !strings.Contains(release, "cp -R profiles") {
		t.Fatal("release archive omits integration profiles")
	}
	if !strings.Contains(release, "uninstall.sh") {
		t.Fatal("release archive omits uninstaller")
	}
	if !strings.Contains(release, "migrate-private-prealpha.sh") {
		t.Fatal("release archive omits private pre-alpha migration tool")
	}
	for _, wanted := range []string{"scripts/build-deb.sh", "scripts/render-homebrew-formula.sh", "release/*.deb", "release/agent-root-broker.rb"} {
		if !strings.Contains(release, wanted) {
			t.Fatalf("release workflow is missing package artifact behavior %q", wanted)
		}
	}
	if !strings.Contains(release, "--prerelease") {
		t.Fatal("release workflow does not mark prerelease tags")
	}
	for _, wanted := range []string{
		"environment: cloudsmith-publish",
		"cloudsmith-io/cloudsmith-cli-action@ad73fafb92e3e29a5166c529464c2df7658a608e",
		`cli-version: "1.24.0"`,
		"actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c",
		`cloudsmith push deb "$target" "$package" --component "$CLOUDSMITH_COMPONENT"`,
	} {
		if !strings.Contains(release, wanted) {
			t.Fatalf("release workflow is missing Cloudsmith behavior %q", wanted)
		}
	}
	if strings.Contains(release, "CLOUDSMITH_API_KEY") {
		t.Fatal("release workflow must not store a long-lived Cloudsmith API key")
	}
	for _, wanted := range []string{"`repository`: `Chang-LL/agent-root-broker`", "`ref`: `refs/tags/v.*`", "CLOUDSMITH_SERVICE_SLUG"} {
		if !strings.Contains(releasing, wanted) {
			t.Fatalf("release documentation is missing Cloudsmith trust guidance %q", wanted)
		}
	}
	debBuilder := projectFile(t, "scripts", "build-deb.sh")
	if !strings.Contains(debBuilder, `agent-root-broker_${FILE_VERSION}_${ARCH}.deb`) {
		t.Fatal("Debian release filename may be rewritten by GitHub")
	}
	for _, wanted := range []string{`DEB_VERSION="${DEB_BASE}~${DEB_PRERELEASE}"`, "packaging/debian/preinst"} {
		if !strings.Contains(debBuilder, wanted) {
			t.Fatalf("Debian builder is missing package/version migration behavior %q", wanted)
		}
	}
	if !strings.Contains(debBuilder, "rootbroker-migrate-private-prealpha") {
		t.Fatal("Debian package omits the private pre-alpha migration command")
	}
	debControl := projectFile(t, "packaging", "debian", "control.in")
	for _, wanted := range []string{"Package: agent-root-broker", "Conflicts: rootbroker", "Replaces: rootbroker"} {
		if !strings.Contains(debControl, wanted) {
			t.Fatalf("Debian control is missing %q", wanted)
		}
	}
	debPreinstall := projectFile(t, "packaging", "debian", "preinst")
	for _, wanted := range []string{"dpkg-query", "rootbroker-uninstall", "apt remove rootbroker"} {
		if !strings.Contains(debPreinstall, wanted) {
			t.Fatalf("Debian preinstall migration guard is missing %q", wanted)
		}
	}
	formula := projectFile(t, "packaging", "homebrew", "agent-root-broker.rb.in")
	if !strings.Contains(formula, "class AgentRootBroker < Formula") {
		t.Fatal("Homebrew formula does not use the public project name")
	}
	for _, wanted := range []string{"on_linux do", "if Hardware::CPU.arm?"} {
		if !strings.Contains(formula, wanted) {
			t.Fatalf("Homebrew formula is missing current architecture selection %q", wanted)
		}
	}
	for _, forbidden := range []string{"on_intel do", "on_arm do"} {
		if strings.Contains(formula, forbidden) {
			t.Fatalf("Homebrew formula uses a block that cannot contain URL stanzas: %q", forbidden)
		}
	}
	if !strings.Contains(formula, "rootbroker-migrate-private-prealpha") {
		t.Fatal("Homebrew formula omits the private pre-alpha migration command")
	}
	for name, document := range map[string]string{"README.md": readmeEnglish, "README.zh-CN.md": readmeChinese} {
		for _, wanted := range []string{"brew install agent-root-broker", "brew install Chang-LL/tap/agent-root-broker"} {
			if !strings.Contains(document, wanted) {
				t.Fatalf("%s is missing canonical Homebrew command %q", name, wanted)
			}
		}
		if strings.Contains(document, "brew install Chang-LL/tap/rootbroker") {
			t.Fatalf("%s still recommends the old Homebrew formula name", name)
		}
		if !strings.Contains(document, "apt install ./agent-root-broker_VERSION_ARCH.deb") {
			t.Fatalf("%s is missing the canonical Debian package name", name)
		}
		if strings.Contains(document, "apt install ./rootbroker_VERSION_ARCH.deb") {
			t.Fatalf("%s still recommends the old Debian package filename", name)
		}
	}
	if !strings.Contains(release, `release_root="$RUNNER_TEMP/rootbroker-release"`) ||
		!strings.Contains(release, `git status --porcelain`) {
		t.Fatal("release build does not keep generated files outside the source tree")
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
	for _, filename := range []string{
		"CONTRIBUTING.md", "SUPPORT.md", "COMPATIBILITY.md", "UPGRADE.md", "UNINSTALL.md",
		"MIGRATION.md", "TROUBLESHOOTING.md", "THREAT_MODEL.md", "CHANGELOG.md",
	} {
		if !strings.Contains(release, filename) {
			t.Fatalf("release archive omits %s", filename)
		}
	}
	if strings.Count(roadmapEnglish, "- [x]") != strings.Count(roadmapChinese, "- [x]") ||
		strings.Count(roadmapEnglish, "- [ ]") != strings.Count(roadmapChinese, "- [ ]") {
		t.Fatal("English and Chinese roadmap checklist states are out of sync")
	}
}

func TestCloudsmithAPTDistributionIsDocumentedAndVerified(t *testing.T) {
	release := projectFile(t, ".github", "workflows", "release.yml")
	for _, wanted := range []string{
		"verify-cloudsmith-apt:",
		"needs: publish-cloudsmith",
		"debian:12",
		"ubuntu:24.04",
		"https://dl.cloudsmith.io/public/lc-software/agent-root-broker/setup.deb.sh",
		"previous_package_version",
		"--only-upgrade agent-root-broker",
		`test "$installed_version" = "$expected_version"`,
		`dpkg-query -W -f='${Package}' agent-root-broker`,
	} {
		if !strings.Contains(release, wanted) {
			t.Fatalf("release workflow is missing public APT verification behavior %q", wanted)
		}
	}

	for _, filename := range []string{"README.md", "README.zh-CN.md"} {
		document := projectFile(t, filename)
		for _, wanted := range []string{
			"https://broadcasts.cloudsmith.com/lc-software/agent-root-broker",
			"https://dl.cloudsmith.io/public/lc-software/agent-root-broker/setup.deb.sh",
			"sudo env component=alpha bash",
			"sudo apt-get install agent-root-broker",
			"Cloudsmith",
		} {
			if !strings.Contains(document, wanted) {
				t.Fatalf("%s is missing public APT guidance %q", filename, wanted)
			}
		}
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

func TestUnixDetailsStayBehindTransportBoundary(t *testing.T) {
	contract := projectFile(t, "internal", "transport", "transport.go")
	unixTransport := projectFile(t, "internal", "transport", "unix_linux.go")
	server := projectFile(t, "internal", "server", "server_linux.go")
	for _, wanted := range []string{"type Factory interface", "type Listener interface", "type Connection interface", "type Peer struct"} {
		if !strings.Contains(contract, wanted) {
			t.Fatalf("transport contract is missing %q", wanted)
		}
	}
	for _, detail := range []string{"SO_PEERCRED", "net.ListenUnix", "GetsockoptUcred"} {
		if strings.Contains(server, detail) {
			t.Fatalf("Unix transport detail %q leaked into server", detail)
		}
		if !strings.Contains(unixTransport, detail) {
			t.Fatalf("Unix transport is missing %q", detail)
		}
	}
	for _, wanted := range []string{"transport.Factory", "transport.UnixFactory", "transport.UnixPeerKind"} {
		if !strings.Contains(server, wanted) {
			t.Fatalf("server transport seam is missing %q", wanted)
		}
	}
}
