//go:build !linux

package server

import (
	"fmt"
	"os"

	"github.com/Chang-LL/rootbroker/internal/config"
)

func Run(_ config.Config) error { return fmt.Errorf("rootbrokerd requires Linux SO_PEERCRED") }

func Main(_ string) int {
	fmt.Fprintln(os.Stderr, "rootbrokerd: requires Linux SO_PEERCRED")
	return 1
}
