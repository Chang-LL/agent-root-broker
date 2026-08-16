//go:build linux

package homeaccess

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

const (
	accessACLName  = "system.posix_acl_access"
	defaultACLName = "system.posix_acl_default"
)

func platformManage(action, home string, uid uint32) (string, error) {
	fd, err := unix.Open(home, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", fmt.Errorf("open home directory safely: %w", err)
	}
	if action == "status" {
		defer func() { _ = unix.Close(fd) }()
		return aclStatus(fd, uid)
	}
	defer func() { _ = unix.Close(fd) }()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return "", fmt.Errorf("inspect home filesystem: %w", err)
	}
	marker := homeMarkerName(uid)
	if action == "grant" {
		_ = removeXattr(fd, marker)
	}
	// Open a fresh directory description instead of duping fd. A duplicate
	// shares its directory offset with the original description, which makes a
	// traversal vulnerable to another reader advancing that shared offset.
	walkRoot, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", fmt.Errorf("open home traversal descriptor: %w", err)
	}
	if err := walkFD(walkRoot, uint64(stat.Dev), action, uid); err != nil {
		return "", err
	}
	if action == "grant" {
		if err := setXattr(fd, marker, []byte{1}); err != nil {
			return "", fmt.Errorf("mark completed home access grant: %w", err)
		}
		return "enabled", nil
	}
	if err := removeXattr(fd, marker); err != nil && !errors.Is(err, unix.ENODATA) {
		return "", fmt.Errorf("clear completed home access grant marker: %w", err)
	}
	return "disabled", nil
}

func walkFD(fd int, rootDevice uint64, action string, uid uint32) error {
	file := os.NewFile(uintptr(fd), "hostctl-home")
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open home directory file descriptor")
	}
	defer func() { _ = file.Close() }()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	isDirectory := stat.Mode&unix.S_IFMT == unix.S_IFDIR
	if uint64(stat.Dev) != rootDevice {
		return nil
	}
	if err := changeACLs(fd, stat.Mode, isDirectory, action, uid); err != nil {
		return err
	}
	if !isDirectory {
		return nil
	}
	names, err := file.Readdirnames(-1)
	if err != nil {
		return fmt.Errorf("read home directory: %w", err)
	}
	for _, name := range names {
		child, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENXIO) || errors.Is(err, unix.ENODEV) {
			continue
		}
		if err != nil {
			return fmt.Errorf("open home entry safely: %w", err)
		}
		var childStat unix.Stat_t
		if err := unix.Fstat(child, &childStat); err != nil {
			_ = unix.Close(child)
			return err
		}
		kind := childStat.Mode & unix.S_IFMT
		if uint64(childStat.Dev) != rootDevice || (kind != unix.S_IFDIR && kind != unix.S_IFREG) {
			_ = unix.Close(child)
			continue
		}
		if err := walkFD(child, rootDevice, action, uid); err != nil {
			return err
		}
	}
	return nil
}

func changeACLs(fd int, mode uint32, isDirectory bool, action string, uid uint32) error {
	if action == "revoke" && isDirectory {
		if err := revokeDefault(fd, uid); err != nil {
			return err
		}
	}
	access, present, err := readACL(fd, accessACLName)
	if err != nil {
		return err
	}
	if !present {
		access = baseACL(mode)
	}
	if action == "grant" {
		permission := uint16(6)
		if isDirectory || mode&0o111 != 0 {
			permission = 7
		}
		access.grantUser(uid, permission)
		if err := writeACL(fd, accessACLName, access); err != nil {
			return err
		}
		if isDirectory {
			return grantDefault(fd, uid, access.base())
		}
		return nil
	}
	if access.revokeUser(uid) {
		return writeACL(fd, accessACLName, access)
	}
	return nil
}

func grantDefault(fd int, uid uint32, base aclValue) error {
	value, present, err := readACL(fd, defaultACLName)
	if err != nil {
		return err
	}
	if !present {
		value = base
		if err := setXattr(fd, markerName(uid), []byte{1}); err != nil {
			return fmt.Errorf("mark hostctl-created default ACL: %w", err)
		}
	}
	value.grantUser(uid, 7)
	if err := writeACL(fd, defaultACLName, value); err != nil {
		if !present {
			_ = removeXattr(fd, markerName(uid))
		}
		return err
	}
	return nil
}

func revokeDefault(fd int, uid uint32) error {
	value, present, err := readACL(fd, defaultACLName)
	if err != nil || !present {
		if errors.Is(err, unix.ENODATA) || err == nil {
			_ = removeXattr(fd, markerName(uid))
			return nil
		}
		return err
	}
	if !value.revokeUser(uid) {
		created, markerErr := hasXattr(fd, markerName(uid))
		if markerErr != nil {
			return markerErr
		}
		if created && !value.hasNamedEntries() {
			if err := removeXattr(fd, defaultACLName); err != nil && !errors.Is(err, unix.ENODATA) {
				return err
			}
			_ = removeXattr(fd, markerName(uid))
		}
		return nil
	}
	created, err := hasXattr(fd, markerName(uid))
	if err != nil {
		return err
	}
	if created && !value.hasNamedEntries() {
		if err := removeXattr(fd, defaultACLName); err != nil && !errors.Is(err, unix.ENODATA) {
			return err
		}
		_ = removeXattr(fd, markerName(uid))
		return nil
	}
	return writeACL(fd, defaultACLName, value)
}

func aclStatus(fd int, uid uint32) (string, error) {
	access, accessPresent, err := readACL(fd, accessACLName)
	if err != nil {
		return "", err
	}
	defaults, defaultPresent, err := readACL(fd, defaultACLName)
	if err != nil {
		return "", err
	}
	accessEnabled := accessPresent && access.userHas(uid, 7)
	defaultEnabled := defaultPresent && defaults.userHas(uid, 7)
	complete, err := hasXattr(fd, homeMarkerName(uid))
	if err != nil {
		return "", err
	}
	if accessEnabled && defaultEnabled && complete {
		return "enabled", nil
	}
	if accessEnabled || defaultEnabled {
		return "partial", nil
	}
	return "disabled", nil
}

func readACL(fd int, name string) (aclValue, bool, error) {
	data, present, err := getXattr(fd, name)
	if err != nil || !present {
		return aclValue{}, present, err
	}
	value, err := parseACL(data)
	return value, true, err
}

func writeACL(fd int, name string, value aclValue) error {
	if err := setXattr(fd, name, value.encode()); err != nil {
		return fmt.Errorf("set POSIX ACL: %w", err)
	}
	return nil
}

func markerName(uid uint32) string {
	return "trusted.hostctl.default-created." + strconv.FormatUint(uint64(uid), 10)
}

func homeMarkerName(uid uint32) string {
	return "trusted.hostctl.home-access-complete." + strconv.FormatUint(uint64(uid), 10)
}

func getXattr(fd int, name string) ([]byte, bool, error) {
	size, err := unix.Fgetxattr(fd, name, nil)
	if errors.Is(err, unix.ENODATA) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if size == 0 {
		return []byte{}, true, nil
	}
	for attempts := 0; attempts < 3; attempts++ {
		data := make([]byte, size)
		read, readErr := unix.Fgetxattr(fd, name, data)
		if errors.Is(readErr, unix.ERANGE) {
			size, err = unix.Fgetxattr(fd, name, nil)
			if err != nil {
				return nil, false, err
			}
			continue
		}
		if readErr != nil {
			return nil, false, readErr
		}
		return data[:read], true, nil
	}
	return nil, false, fmt.Errorf("POSIX ACL changed repeatedly while reading")
}

func setXattr(fd int, name string, data []byte) error {
	return unix.Fsetxattr(fd, name, data, 0)
}

func hasXattr(fd int, name string) (bool, error) {
	_, present, err := getXattr(fd, name)
	return present, err
}

func removeXattr(fd int, name string) error {
	return unix.Fremovexattr(fd, name)
}
