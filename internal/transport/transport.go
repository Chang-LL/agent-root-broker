package transport

import (
	"errors"
	"io"
)

const UnixPeerKind = "unix-peer"

var ErrClosed = errors.New("transport listener is closed")

type Endpoint struct {
	Plane       string
	Address     string
	AccessGroup string
}

// Peer is the authenticated identity supplied by a transport. The current
// server accepts only UnixPeerKind, whose values come from Linux SO_PEERCRED.
// A future transport must define and explicitly wire its own authentication
// semantics instead of populating these fields from untrusted payload data.
type Peer struct {
	Kind string
	PID  int
	UID  uint32
	GID  uint32
}

type Connection interface {
	io.ReadWriteCloser
	Peer() (Peer, error)
}

type Listener interface {
	Accept() (Connection, error)
	Close() error
}

// Factory is the compile-time transport extension boundary. Shipping another
// implementation also requires an explicit server-side identity and trust
// model; implementing this interface alone grants no authority.
type Factory interface {
	Listen(Endpoint) (Listener, error)
}
