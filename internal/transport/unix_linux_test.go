//go:build linux

package transport

import (
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestUnixFactorySuppliesKernelPeerAndSocketPermissions(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	group, err := user.LookupGroupId(current.Gid)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "request.sock")
	listener, err := (UnixFactory{}).Listen(Endpoint{Plane: "request", Address: path, AccessGroup: group.Name})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o660 || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket mode=%v", info.Mode())
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		wantedGID, _ := strconv.ParseUint(current.Gid, 10, 32)
		if uint64(stat.Gid) != wantedGID {
			t.Fatalf("socket gid=%d want=%d", stat.Gid, wantedGID)
		}
	}

	accepted := make(chan Connection, 1)
	errors := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			errors <- acceptErr
			return
		}
		accepted <- connection
	}()
	client, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	select {
	case err := <-errors:
		t.Fatal(err)
	case connection := <-accepted:
		defer func() { _ = connection.Close() }()
		peer, err := connection.Peer()
		if err != nil {
			t.Fatal(err)
		}
		if peer.Kind != UnixPeerKind || peer.PID != os.Getpid() || peer.UID != uint32(os.Geteuid()) {
			t.Fatalf("peer=%+v", peer)
		}
	case <-time.After(time.Second):
		t.Fatal("accept timed out")
	}
}

func TestUnixFactoryRefusesNonSocketPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.sock")
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (UnixFactory{}).Listen(Endpoint{Address: path, AccessGroup: "unused"}); err == nil {
		t.Fatal("non-socket path was replaced")
	}
}
