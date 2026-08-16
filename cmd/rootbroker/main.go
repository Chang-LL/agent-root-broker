package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Chang-LL/rootbroker/internal/commands"
	"github.com/Chang-LL/rootbroker/internal/config"
	"github.com/Chang-LL/rootbroker/internal/server"
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
		case "rootbroker", "rootbroker-admin", "rootbroker-grok-hook", "rootbroker-maint", "rootbrokerd":
			name, args = args[0], args[1:]
		}
	}
	switch name {
	case "rootbroker-admin":
		return commands.Admin(args)
	case "rootbroker-grok-hook":
		return commands.GrokHook()
	case "rootbroker-maint":
		return commands.Maintenance(args)
	case "rootbrokerd":
		configPath := config.DefaultPath
		checkOnly := false
		if len(args) == 2 && args[0] == "--config" {
			configPath = args[1]
		} else if len(args) == 2 && args[0] == "--check-config" {
			configPath, checkOnly = args[1], true
		} else if len(args) != 0 {
			fmt.Fprintln(os.Stderr, "Usage: rootbrokerd [--config PATH|--check-config PATH]")
			return 2
		}
		if checkOnly {
			if _, err := config.Load(configPath); err != nil {
				fmt.Fprintf(os.Stderr, "rootbrokerd: %v\n", err)
				return 2
			}
			return 0
		}
		return server.Main(configPath)
	default:
		return commands.Rootbroker(args, version)
	}
}
