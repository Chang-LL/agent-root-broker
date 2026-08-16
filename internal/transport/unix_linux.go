//go:build linux

package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

type UnixFactory struct{}

type unixListener struct {
	listener *net.UnixListener
	path     string
}

type unixConnection struct {
	*net.UnixConn
}

func (UnixFactory) Listen(endpoint Endpoint) (Listener, error) {
	if err := removeStaleSocket(endpoint.Address); err != nil {
		return nil, err
	}
	address := &net.UnixAddr{Name: endpoint.Address, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", endpoint.Address, err)
	}
	group, err := user.LookupGroup(endpoint.AccessGroup)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("lookup group %s: %w", endpoint.AccessGroup, err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("parse group ID %s: %w", group.Gid, err)
	}
	if err := os.Chown(endpoint.Address, os.Geteuid(), gid); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("own socket %s: %w", endpoint.Address, err)
	}
	if err := os.Chmod(endpoint.Address, 0o660); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("set socket mode %s: %w", endpoint.Address, err)
	}
	return &unixListener{listener: listener, path: endpoint.Address}, nil
}

func (l *unixListener) Accept() (Connection, error) {
	connection, err := l.listener.AcceptUnix()
	if errors.Is(err, net.ErrClosed) {
		return nil, ErrClosed
	}
	if err != nil {
		return nil, err
	}
	return &unixConnection{UnixConn: connection}, nil
}

func (l *unixListener) Close() error {
	closeErr := l.listener.Close()
	removeErr := os.Remove(l.path)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return closeErr
}

func (c *unixConnection) Peer() (Peer, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return Peer{}, err
	}
	var credentials *unix.Ucred
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, socketErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return Peer{}, err
	}
	if socketErr != nil {
		return Peer{}, socketErr
	}
	return Peer{Kind: UnixPeerKind, PID: int(credentials.Pid), UID: credentials.Uid, GID: credentials.Gid}, nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket path: %s", path)
	}
	if connection, dialErr := net.DialTimeout("unix", path, 150*time.Millisecond); dialErr == nil {
		_ = connection.Close()
		return fmt.Errorf("another daemon is listening on %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}
