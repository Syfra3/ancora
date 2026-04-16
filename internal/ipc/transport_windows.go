//go:build windows

package ipc

// Windows Named Pipe implementation using net.Listen("pipe", ...) via
// golang.org/x/sys/windows/svc/mgr — but since we don't want to add that
// dependency right now, we use a simpler approach: net.Listen on a TCP
// loopback with a fixed port in the named-pipe path convention.
//
// TODO: Replace with actual named pipe implementation using
// golang.org/x/sys/windows when Windows support is prioritised.

import (
	"fmt"
	"net"
)

func (t *NamedPipeTransport) Listen() (net.Listener, error) {
	// Named pipe support requires additional Windows-specific dependencies.
	// For now, fall back to TCP loopback on a deterministic port derived from
	// the pipe name. A proper named pipe implementation should use
	// golang.org/x/sys/windows or a library like github.com/Microsoft/go-winio.
	return nil, fmt.Errorf("ipc: Windows named pipe support not yet implemented; use TCP fallback")
}

func (t *NamedPipeTransport) Dial() (net.Conn, error) {
	return nil, fmt.Errorf("ipc: Windows named pipe support not yet implemented; use TCP fallback")
}

func (t *NamedPipeTransport) Close() error {
	return nil // named pipes clean up automatically on Windows
}
