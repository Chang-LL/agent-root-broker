package main

import (
	"fmt"
	"os"
	"path/filepath"

	"hostctl/internal/commands"
	"hostctl/internal/config"
	"hostctl/internal/server"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	name := filepath.Base(os.Args[0])
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "hostctl", "hostctl-admin", "hostctl-grok-hook", "hostctl-maint", "hostctld":
			name, args = args[0], args[1:]
		}
	}
	switch name {
	case "hostctl-admin":
		return commands.Admin(args)
	case "hostctl-grok-hook":
		return commands.GrokHook()
	case "hostctl-maint":
		return commands.Maintenance(args)
	case "hostctld":
		configPath := config.DefaultPath
		checkOnly := false
		if len(args) == 2 && args[0] == "--config" {
			configPath = args[1]
		} else if len(args) == 2 && args[0] == "--check-config" {
			configPath, checkOnly = args[1], true
		} else if len(args) != 0 {
			fmt.Fprintln(os.Stderr, "Usage: hostctld [--config PATH|--check-config PATH]")
			return 2
		}
		if checkOnly {
			if _, err := config.Load(configPath); err != nil {
				fmt.Fprintf(os.Stderr, "hostctld: %v\n", err)
				return 2
			}
			return 0
		}
		return server.Main(configPath)
	default:
		return commands.Hostctl(args, version)
	}
}
