//go:build !linux

package homeaccess

import "fmt"

func platformManage(_, _ string, _ uint32) (string, error) {
	return "", fmt.Errorf("home access management requires Linux POSIX ACLs")
}
