package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chang-LL/agent-root-broker/internal/config"
)

func testConfig() config.Config {
	cfg := config.Default()
	cfg.RequireRootOwnedExecutable = false
	cfg.MaxOutputBytes = 4
	return cfg
}

func TestPrepareAndExecuteWithoutShell(t *testing.T) {
	cfg := testConfig()
	cwd, _ := os.Getwd()
	command, err := Prepare([]string{"echo", "hello; not-a-shell"}, cwd, intPointer(5), cfg)
	if err != nil {
		t.Fatal(err)
	}
	result := Execute(command, cfg)
	if result.ExitCode != 0 || result.Stdout != "hell" || !result.StdoutTruncated {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestPrepareRejectsWrappersAndFlagsRisk(t *testing.T) {
	cfg := testConfig()
	cwd, _ := os.Getwd()
	for _, executable := range []string{"sudo", "rootbroker"} {
		if _, err := Prepare([]string{executable, "true"}, cwd, intPointer(5), cfg); err == nil {
			t.Fatalf("expected %s rejection", executable)
		}
	}
	command, err := Prepare([]string{"sh", "-c", "id"}, cwd, intPointer(5), cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Risks, ",")
	if !strings.Contains(joined, "interpreter-or-shell") || !strings.Contains(joined, "executes-inline") {
		t.Fatalf("missing risks: %v", command.Risks)
	}
	second, _ := Prepare([]string{"sh", "-c", "id"}, cwd, intPointer(5), cfg)
	if command.Hash != second.Hash {
		t.Fatal("command hash is not stable")
	}
}

func TestPrepareFlagsSystemServiceManagement(t *testing.T) {
	cfg := testConfig()
	cfg.CleanPath = t.TempDir()
	cwd, _ := os.Getwd()
	for _, executable := range []string{"systemctl", "systemd-run"} {
		path := filepath.Join(cfg.CleanPath, executable)
		if err := os.WriteFile(path, []byte("test fixture\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		command, err := Prepare([]string{executable, "test"}, cwd, intPointer(5), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(command.Risks, ","), "system-service-management") {
			t.Fatalf("%s risks = %v, want system service warning", executable, command.Risks)
		}
	}
}

func TestExecuteTimeout(t *testing.T) {
	cfg := testConfig()
	cwd, _ := os.Getwd()
	command, err := Prepare([]string{"sh", "-c", "sleep 2"}, cwd, intPointer(1), cfg)
	if err != nil {
		t.Fatal(err)
	}
	result := Execute(command, cfg)
	if !result.TimedOut || result.ExitCode != 124 {
		t.Fatalf("unexpected timeout result: %+v", result)
	}
}

func intPointer(value int) *int { return &value }
