package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Chang-LL/agent-root-broker/internal/broker"
	"github.com/Chang-LL/agent-root-broker/internal/executor"
)

func TestAdminRendererPendingComfortable(t *testing.T) {
	config := defaultAdminUIConfig()
	renderer := newAdminRenderer(config, false, 64)
	item := broker.PendingView{
		ID: "0123456789abcdef", SessionID: "01900000-0000-7000-8000-000000000001", Turn: 2,
		Command: executor.Command{
			Argv: []string{"/usr/bin/systemd-run", "--unit=example", "--description=Agent Root Broker activation", "--collect"},
			CWD:  "/srv/project", TimeoutSeconds: 300, Hash: "abcdef0123456789",
			Risks: []string{"system-service-management"},
		},
	}

	output := renderer.pending(item)
	for _, expected := range []string{
		"==> Root request  0123456789abcdef",
		"Command\n  /usr/bin/systemd-run --unit=example",
		"    '--description=Agent Root Broker activation' --collect",
		"Warning: system service management",
		"Context",
		"session   01900000-0000-7000-8000-000000000001",
		"turn      2",
		"cwd       /srv/project",
		"timeout   300s",
		"hash      abcdef0123456789",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("pending output missing %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "\x1b[") {
		t.Fatalf("plain renderer emitted ANSI escapes: %q", output)
	}
}

func TestAdminRendererNeverHidesCommandOrRisks(t *testing.T) {
	config := defaultAdminUIConfig()
	config.Density = adminDensityCompact
	config.ShowHash = false
	config.WrapCommand = false
	renderer := newAdminRenderer(config, false, 100)
	item := broker.PendingView{
		ID: "request", SessionID: "session", Turn: 3,
		Command: executor.Command{
			Argv: []string{"/usr/bin/sh", "-c", "id"}, CWD: "/tmp", TimeoutSeconds: 30,
			Hash: "hidden", Risks: []string{"interpreter-or-shell", "executes-inline-or-module-code"},
		},
	}

	output := renderer.pending(item)
	for _, expected := range []string{
		"/usr/bin/sh -c id",
		"Warning: interpreter or shell",
		"Warning: executes inline or module code",
		"session   session",
		"turn      3",
		"cwd       /tmp",
		"timeout   30s",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("pending output missing %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "hidden") {
		t.Fatalf("showHash=false still rendered the hash: %s", output)
	}
}

func TestAdminRendererBuiltInThemesUseANSIHierarchy(t *testing.T) {
	for _, theme := range []string{adminThemeDefault, adminThemeMono, adminThemeHighContrast} {
		t.Run(theme, func(t *testing.T) {
			config := defaultAdminUIConfig()
			config.Theme = theme
			renderer := newAdminRenderer(config, true, 100)
			for role, style := range map[string]string{
				"heading": renderer.palette.heading,
				"warning": renderer.palette.warning,
				"once":    renderer.palette.once,
				"broader": renderer.palette.broader,
				"deny":    renderer.palette.deny,
			} {
				if !strings.HasPrefix(style, "\x1b[") {
					t.Fatalf("%s style %q is not ANSI", role, style)
				}
			}
		})
	}
}

func TestAdminRendererStylesAndExplainsApprovalScopes(t *testing.T) {
	renderer := newAdminRenderer(defaultAdminUIConfig(), true, 100)
	prompt := renderer.approvalPrompt(7, true)
	for _, expected := range []string{
		"only this command",
		"remaining requests in turn 7",
		"remaining requests in this session",
		"leave it pending",
		"stop watching",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("approval prompt missing %q: %q", expected, prompt)
		}
	}
	if !strings.Contains(prompt, "\x1b[1;32m[c] once\x1b[0m") {
		t.Fatalf("approval prompt missing default theme styling: %q", prompt)
	}
	if got := renderer.approvalPrompt(7, false); !strings.Contains(got, "Choice: ") || strings.Contains(got, "only this command") {
		t.Fatalf("retry prompt = %q, want only Choice", got)
	}
}

func TestShouldUseANSI(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		isTTY    bool
		terminal string
		want     bool
	}{
		{name: "auto terminal", mode: adminColorAuto, isTTY: true, terminal: "xterm-256color", want: true},
		{name: "auto pipe", mode: adminColorAuto, isTTY: false, terminal: "xterm-256color", want: false},
		{name: "dumb terminal", mode: adminColorAuto, isTTY: true, terminal: "dumb", want: false},
		{name: "always pipe", mode: adminColorAlways, isTTY: false, terminal: "dumb", want: true},
		{name: "never terminal", mode: adminColorNever, isTTY: true, terminal: "xterm-256color", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldUseANSI(test.mode, test.isTTY, test.terminal); got != test.want {
				t.Fatalf("shouldUseANSI(%q, %v, %q) = %v, want %v", test.mode, test.isTTY, test.terminal, got, test.want)
			}
		})
	}
}

func TestAdminWatchOptionsPrecedence(t *testing.T) {
	clearAdminUIEnvironment(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "admin.json")
	contents := `{"color":"auto","theme":"mono","density":"compact","showHash":false,"wrapCommand":false}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NO_COLOR", "1")
	t.Setenv("ROOTBROKER_THEME", "high-contrast")

	options, help, err := parseAdminWatchOptions([]string{
		"--config", path,
		"--interval", "0.25",
		"--color", "always",
		"--density", "comfortable",
		"--show-hash=true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if help {
		t.Fatal("help = true, want false")
	}
	if options.Interval != 250*time.Millisecond {
		t.Fatalf("interval = %s, want 250ms", options.Interval)
	}
	if options.UI.Color != adminColorAlways || options.UI.Theme != adminThemeHighContrast || options.UI.Density != adminDensityComfortable {
		t.Fatalf("UI precedence result = %+v", options.UI)
	}
	if !options.UI.ShowHash || options.UI.WrapCommand {
		t.Fatalf("UI boolean precedence result = %+v", options.UI)
	}
}

func TestAdminWatchOptionsHonorNoColor(t *testing.T) {
	clearAdminUIEnvironment(t)
	t.Setenv("NO_COLOR", "1")
	options, _, err := parseAdminWatchOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if options.UI.Color != adminColorNever {
		t.Fatalf("color = %q, want never", options.UI.Color)
	}
}

func TestAdminWatchHelpDoesNotRequireValidConfiguration(t *testing.T) {
	clearAdminUIEnvironment(t)
	t.Setenv("ROOTBROKER_ADMIN_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	_, help, err := parseAdminWatchOptions([]string{"--help"})
	if err != nil {
		t.Fatal(err)
	}
	if !help {
		t.Fatal("help = false, want true")
	}
}

func TestAdminWatchOptionsRejectNonFiniteInterval(t *testing.T) {
	clearAdminUIEnvironment(t)
	for _, value := range []string{"NaN", "+Inf"} {
		t.Run(value, func(t *testing.T) {
			if _, _, err := parseAdminWatchOptions([]string{"--interval", value}); err == nil || !strings.Contains(err.Error(), "finite") {
				t.Fatalf("error = %v, want finite interval error", err)
			}
		})
	}
}

func TestAdminUIConfigRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.json")
	if err := os.WriteFile(path, []byte(`{"color":"auto","hideRisks":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAdminUIConfig(path, true); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want unknown field", err)
	}
}

func TestAdminUIConfigRejectsInvalidEnums(t *testing.T) {
	for name, config := range map[string]adminUIConfig{
		"color":   {Color: "sometimes", Theme: adminThemeDefault, Density: adminDensityComfortable},
		"theme":   {Color: adminColorAuto, Theme: "rainbow", Density: adminDensityComfortable},
		"density": {Color: adminColorAuto, Theme: adminThemeDefault, Density: "spacious"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateAdminUIConfig(config); err == nil {
				t.Fatalf("validateAdminUIConfig(%+v) succeeded, want error", config)
			}
		})
	}
}

func clearAdminUIEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"NO_COLOR", "ROOTBROKER_ADMIN_CONFIG", "ROOTBROKER_COLOR", "ROOTBROKER_THEME",
		"ROOTBROKER_DENSITY", "ROOTBROKER_SHOW_HASH", "ROOTBROKER_WRAP_COMMAND", "XDG_CONFIG_HOME",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("HOME", t.TempDir())
}
