//go:build linux

package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type info struct {
	Identity
	PPID int
	Exe  string
}

func read(pid int) (info, error) {
	statBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return info{}, err
	}
	statText := string(statBytes)
	closeParen := strings.LastIndex(statText, ")")
	if closeParen < 0 || closeParen+2 >= len(statText) {
		return info{}, fmt.Errorf("malformed proc stat")
	}
	fields := strings.Fields(statText[closeParen+2:])
	if len(fields) < 20 {
		return info{}, fmt.Errorf("malformed proc stat fields")
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return info{}, err
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return info{}, err
	}
	statusBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return info{}, err
	}
	var uid uint64
	foundUID := false
	for _, line := range strings.Split(string(statusBytes), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return info{}, fmt.Errorf("malformed uid line")
			}
			uid, err = strconv.ParseUint(parts[1], 10, 32)
			if err != nil {
				return info{}, err
			}
			foundUID = true
			break
		}
	}
	if !foundUID {
		return info{}, fmt.Errorf("uid not found")
	}
	exe, err := filepath.EvalSymlinks(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return info{}, err
	}
	return info{Identity: Identity{PID: pid, UID: uint32(uid), StartTime: startTime}, PPID: ppid, Exe: exe}, nil
}

func FindAgent(peerPID int, peerUID uint32, executables []string) (Identity, bool) {
	wanted := make(map[string]struct{}, len(executables))
	for _, path := range executables {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			wanted[resolved] = struct{}{}
		}
	}
	seen := make(map[int]struct{})
	pid := peerPID
	for count := 0; count < 128 && pid > 1; count++ {
		if _, ok := seen[pid]; ok {
			break
		}
		seen[pid] = struct{}{}
		item, err := read(pid)
		if err != nil {
			return Identity{}, false
		}
		if item.UID == peerUID {
			if _, ok := wanted[item.Exe]; ok {
				fileInfo, err := os.Stat(item.Exe)
				if err != nil {
					return Identity{}, false
				}
				stat, ok := fileInfo.Sys().(*syscall.Stat_t)
				if ok && stat.Uid == 0 && fileInfo.Mode().Perm()&0o022 == 0 {
					return item.Identity, true
				}
			}
		}
		pid = item.PPID
	}
	return Identity{}, false
}

func Alive(identity Identity) bool {
	item, err := read(identity.PID)
	return err == nil && item.Identity == identity
}
