//go:build !windows

package ipc

// On non-Windows platforms, NamedPipeTransport falls back to Unix sockets.
// New() never returns a NamedPipeTransport on non-Windows, but the type must
// satisfy the Transport interface for the compiler.

import (
	"fmt"
	"net"
)

func (t *NamedPipeTransport) Listen() (net.Listener, error) {
	return nil, fmt.Errorf("ipc: named pipes only supported on Windows")
}

func (t *NamedPipeTransport) Dial() (net.Conn, error) {
	return nil, fmt.Errorf("ipc: named pipes only supported on Windows")
}

func (t *NamedPipeTransport) Close() error {
	return nil // no file to remove for named pipes
}
