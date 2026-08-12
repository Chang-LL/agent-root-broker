//go:build !linux && !darwin

package executor

import (
	"fmt"
	"syscall"
)

func validateExecutable(_ string, _ bool) error    { return fmt.Errorf("hostctl execution requires Unix") }
func processGroupAttributes() *syscall.SysProcAttr { return nil }
func killProcessGroup(_ int)                       {}
