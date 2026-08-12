//go:build linux

package homeaccess

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	accessACLName  = "system.posix_acl_access"
	defaultACLName = "system.posix_acl_default"
)

func platformManage(action, home string, uid uint32) (string, error) {
	fd, err := syscall.Open(home, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return "", fmt.Errorf("open home directory safely: %w", err)
	}
	if action == "status" {
		defer syscall.Close(fd)
		return aclStatus(fd, uid)
	}
	defer syscall.Close(fd)
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return "", fmt.Errorf("inspect home filesystem: %w", err)
	}
	marker := homeMarkerName(uid)
	if action == "grant" {
		_ = removeXattr(fd, marker)
	}
	walkRoot, err := syscall.Dup(fd)
	if err != nil {
		return "", fmt.Errorf("duplicate home descriptor: %w", err)
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
	if err := removeXattr(fd, marker); err != nil && !errors.Is(err, syscall.ENODATA) {
		return "", fmt.Errorf("clear completed home access grant marker: %w", err)
	}
	return "disabled", nil
}

func walkFD(fd int, rootDevice uint64, action string, uid uint32) error {
	file := os.NewFile(uintptr(fd), "hostctl-home")
	if file == nil {
		syscall.Close(fd)
		return fmt.Errorf("open home directory file descriptor")
	}
	defer file.Close()
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return err
	}
	isDirectory := stat.Mode&syscall.S_IFMT == syscall.S_IFDIR
	if uint64(stat.Dev) != rootDevice {
		return nil
	}
	if err := changeACLs(fd, stat.Mode, isDirectory, action, uid); err != nil {
		return err
	}
	if !isDirectory {
		return nil
	}
	entries, err := file.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("read home directory: %w", err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect home entry: %w", err)
		}
		initialKind := info.Mode() & os.ModeType
		if initialKind != 0 && initialKind != os.ModeDir {
			continue
		}
		name := entry.Name()
		child, err := syscall.Openat(fd, name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ELOOP) {
			continue
		}
		if err != nil {
			return fmt.Errorf("open home entry safely: %w", err)
		}
		var childStat syscall.Stat_t
		if err := syscall.Fstat(child, &childStat); err != nil {
			syscall.Close(child)
			return err
		}
		kind := childStat.Mode & syscall.S_IFMT
		if uint64(childStat.Dev) != rootDevice || (kind != syscall.S_IFDIR && kind != syscall.S_IFREG) {
			syscall.Close(child)
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
		if errors.Is(err, syscall.ENODATA) || err == nil {
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
			if err := removeXattr(fd, defaultACLName); err != nil && !errors.Is(err, syscall.ENODATA) {
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
		if err := removeXattr(fd, defaultACLName); err != nil && !errors.Is(err, syscall.ENODATA) {
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
	namePointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, false, err
	}
	size, _, errno := syscall.Syscall6(syscall.SYS_FGETXATTR, uintptr(fd), uintptr(unsafe.Pointer(namePointer)), 0, 0, 0, 0)
	if errno == syscall.ENODATA {
		return nil, false, nil
	}
	if errno != 0 {
		return nil, false, errno
	}
	if size == 0 {
		return []byte{}, true, nil
	}
	for attempts := 0; attempts < 3; attempts++ {
		data := make([]byte, int(size))
		read, _, readErr := syscall.Syscall6(syscall.SYS_FGETXATTR, uintptr(fd), uintptr(unsafe.Pointer(namePointer)), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 0, 0)
		if readErr == syscall.ERANGE {
			size, _, errno = syscall.Syscall6(syscall.SYS_FGETXATTR, uintptr(fd), uintptr(unsafe.Pointer(namePointer)), 0, 0, 0, 0)
			if errno != 0 {
				return nil, false, errno
			}
			continue
		}
		if readErr != 0 {
			return nil, false, readErr
		}
		return data[:int(read)], true, nil
	}
	return nil, false, fmt.Errorf("POSIX ACL changed repeatedly while reading")
}

func setXattr(fd int, name string, data []byte) error {
	namePointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	var dataPointer unsafe.Pointer
	if len(data) > 0 {
		dataPointer = unsafe.Pointer(&data[0])
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_FSETXATTR, uintptr(fd), uintptr(unsafe.Pointer(namePointer)), uintptr(dataPointer), uintptr(len(data)), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func hasXattr(fd int, name string) (bool, error) {
	_, present, err := getXattr(fd, name)
	return present, err
}

func removeXattr(fd int, name string) error {
	namePointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall(syscall.SYS_FREMOVEXATTR, uintptr(fd), uintptr(unsafe.Pointer(namePointer)), 0)
	if errno != 0 {
		return errno
	}
	return nil
}
