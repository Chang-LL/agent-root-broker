//go:build linux

package homeaccess

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPlatformGrantStatusRevoke(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("trusted completion markers require root")
	}
	const agentUID = uint32(424242)
	home := t.TempDir()
	outside := t.TempDir()
	file := filepath.Join(home, "file")
	if err := os.WriteFile(file, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "outside")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}

	state, err := platformManage("grant", home, agentUID)
	if err != nil || state != "enabled" {
		t.Fatalf("grant state=%q err=%v", state, err)
	}
	state, err = platformManage("status", home, agentUID)
	if err != nil || state != "enabled" {
		t.Fatalf("status state=%q err=%v", state, err)
	}
	assertACLUser(t, file, agentUID, 6, true)
	assertACLUser(t, filepath.Join(home, "dir"), agentUID, 7, true)
	assertACLUser(t, outsideFile, agentUID, 1, false)

	newFile := filepath.Join(home, "new-file")
	if err := os.WriteFile(newFile, []byte("new"), 0o660); err != nil {
		t.Fatal(err)
	}
	assertACLUser(t, newFile, agentUID, 6, true)

	state, err = platformManage("revoke", home, agentUID)
	if err != nil || state != "disabled" {
		t.Fatalf("revoke state=%q err=%v", state, err)
	}
	state, err = platformManage("status", home, agentUID)
	if err != nil || state != "disabled" {
		t.Fatalf("post-revoke status state=%q err=%v", state, err)
	}
	assertACLUser(t, file, agentUID, 1, false)
	assertACLUser(t, newFile, agentUID, 1, false)

	fd, err := syscall.Open(home, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if _, present, err := readACL(fd, defaultACLName); err != nil || present {
		t.Fatalf("hostctl-created default ACL remains: present=%v err=%v", present, err)
	}
}

func assertACLUser(t *testing.T, path string, uid uint32, permission uint16, wanted bool) {
	t.Helper()
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	value, present, err := readACL(fd, accessACLName)
	if err != nil {
		t.Fatal(err)
	}
	got := present && value.userHas(uid, permission)
	if got != wanted {
		t.Fatalf("ACL on %s: got=%v want=%v entries=%#v", path, got, wanted, value.entries)
	}
}
