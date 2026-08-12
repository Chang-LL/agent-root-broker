//go:build linux || darwin

package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func validateExecutable(path string, requireRoot bool) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("not an executable regular file: %s", path)
	}
	if !requireRoot {
		return nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return fmt.Errorf("executable must be owned by root")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("executable must not be group- or world-writable")
	}
	current := filepath.Dir(path)
	for {
		parentInfo, err := os.Stat(current)
		if err != nil {
			return fmt.Errorf("stat executable parent: %w", err)
		}
		parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
		if !ok || parentStat.Uid != 0 || parentInfo.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("executable parent must be root-owned and non-writable: %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return nil
}

func processGroupAttributes() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }
func killProcessGroup(pid int)                     { _ = syscall.Kill(-pid, syscall.SIGKILL) }
