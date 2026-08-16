package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Chang-LL/rootbroker/internal/config"
	"github.com/Chang-LL/rootbroker/internal/homeaccess"
)

// Maintenance exposes only recovery operations that must work without a
// running daemon. It is intentionally root-only and is not installed as a
// standalone multicall link.
func Maintenance(args []string) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "rootbroker-maint: root is required")
		return 1
	}
	configPath := config.DefaultPath
	if len(args) >= 2 && args[0] == "--config" {
		configPath, args = args[1], args[2:]
	}
	if len(args) != 2 || args[0] != "home-access" || (args[1] != "status" && args[1] != "revoke") {
		fmt.Fprintln(os.Stderr, "Usage: rootbroker-maint [--config PATH] home-access status|revoke")
		return 2
	}
	cfg, err := config.Load(filepath.Clean(configPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "rootbroker-maint: %v\n", err)
		return 1
	}
	if len(cfg.ApproverUsers) != 1 || len(cfg.AgentUsers) != 1 {
		fmt.Fprintln(os.Stderr, "rootbroker-maint: home access requires exactly one approver and one agent")
		return 1
	}
	home, agentUser, err := homeaccess.ResolveTarget(cfg.ApproverUsers[0], cfg.AgentUsers[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "rootbroker-maint: %v\n", err)
		return 1
	}
	result, err := homeaccess.New().Manage(args[1], home, agentUser)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rootbroker-maint: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "Full-home access: %s  agent=%s  home=%s\n", result.State, result.AgentUser, result.Home)
	return 0
}
