//go:build !linux

package server

import (
	"fmt"
	"os"

	"hostctl/internal/config"
)

func Run(_ config.Config) error { return fmt.Errorf("hostctld requires Linux SO_PEERCRED") }

func Main(_ string) int {
	fmt.Fprintln(os.Stderr, "hostctld: requires Linux SO_PEERCRED")
	return 1
}
