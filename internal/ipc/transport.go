// Package ipc provides cross-platform IPC transport for Syfra ecosystem modules.
//
// Unix/macOS: Unix domain sockets at ~/.syfra/<name>.sock
// Windows:    Named pipes at \\.\pipe\syfra-<name>
//
// Authentication uses a shared secret stored in ~/.syfra/ipc-secret.
// All events are NDJSON (newline-delimited JSON) over the socket.
package ipc

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

// Transport abstracts the underlying socket mechanism so the rest of the
// codebase is cross-platform without ifdefs scattered everywhere.
type Transport interface {
	// Listen creates the server-side listener.
	Listen() (net.Listener, error)

	// Dial opens a client connection to the server.
	Dial() (net.Conn, error)

	// Path returns the socket path or named pipe name.
	Path() string

	// Close removes the socket file (no-op on Windows pipes).
	Close() error
}

// New returns the correct Transport implementation for the current OS.
// name is the module name, e.g. "ancora".
// socketDir defaults to ~/.syfra if empty.
func New(name, socketDir string) (Transport, error) {
	if socketDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("ipc: resolve home dir: %w", err)
		}
		socketDir = filepath.Join(home, ".syfra")
	}
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return nil, fmt.Errorf("ipc: create socket dir %s: %w", socketDir, err)
	}

	if runtime.GOOS == "windows" {
		return &NamedPipeTransport{name: name}, nil
	}
	return &UnixTransport{
		path: filepath.Join(socketDir, name+".sock"),
	}, nil
}

// ─── UnixTransport ────────────────────────────────────────────────────────────

// UnixTransport uses Unix domain sockets (Linux/macOS).
type UnixTransport struct {
	path string
}

func (t *UnixTransport) Path() string { return t.path }

func (t *UnixTransport) Listen() (net.Listener, error) {
	// Remove stale socket if it exists but has no active listener.
	if err := removeStaleSocket(t.path); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", t.path)
	if err != nil {
		return nil, fmt.Errorf("ipc: listen unix %s: %w", t.path, err)
	}
	// Only owner can connect.
	if err := os.Chmod(t.path, 0600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("ipc: chmod socket: %w", err)
	}
	return ln, nil
}

func (t *UnixTransport) Dial() (net.Conn, error) {
	conn, err := net.Dial("unix", t.path)
	if err != nil {
		return nil, fmt.Errorf("ipc: dial unix %s: %w", t.path, err)
	}
	return conn, nil
}

func (t *UnixTransport) Close() error {
	if err := os.Remove(t.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ipc: remove socket %s: %w", t.path, err)
	}
	return nil
}

// removeStaleSocket attempts to detect a stale socket (file exists but no
// listener) and removes it so a new listener can bind.
func removeStaleSocket(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // nothing to remove
	}
	// Try connecting; if it fails the socket is stale.
	conn, err := net.Dial("unix", path)
	if err != nil {
		// Stale — remove it.
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("ipc: remove stale socket %s: %w", path, rmErr)
		}
		return nil
	}
	conn.Close()
	// A live listener is already there — the caller will get EADDRINUSE from net.Listen.
	return nil
}

// ─── NamedPipeTransport ───────────────────────────────────────────────────────

// NamedPipeTransport uses Windows Named Pipes.
// The actual pipe I/O is handled in transport_windows.go via build tags.
type NamedPipeTransport struct {
	name string
}

func (t *NamedPipeTransport) Path() string {
	return `\\.\pipe\syfra-` + t.name
}

// ─── Secret Management ────────────────────────────────────────────────────────

// LoadOrCreateSecret reads ~/.syfra/ipc-secret (32 random bytes, hex-encoded).
// If the file does not exist it is created with secure permissions.
func LoadOrCreateSecret(socketDir string) (string, error) {
	if socketDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("ipc: resolve home dir: %w", err)
		}
		socketDir = filepath.Join(home, ".syfra")
	}
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return "", fmt.Errorf("ipc: create syfra dir: %w", err)
	}

	secretPath := filepath.Join(socketDir, "ipc-secret")
	data, err := os.ReadFile(secretPath)
	if err == nil {
		secret := string(data)
		if len(secret) == 64 { // 32 bytes hex-encoded = 64 chars
			return secret, nil
		}
		// Corrupt file — regenerate.
	}

	// Generate a fresh 32-byte secret.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("ipc: generate secret: %w", err)
	}
	secret := hex.EncodeToString(raw)
	if err := os.WriteFile(secretPath, []byte(secret), 0600); err != nil {
		return "", fmt.Errorf("ipc: write secret file: %w", err)
	}
	return secret, nil
}
