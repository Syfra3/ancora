//go:build !windows

package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// ─── UnixTransport ────────────────────────────────────────────────────────────

func TestUnixTransport_ListenAndDial(t *testing.T) {
	dir := t.TempDir()
	tr := &UnixTransport{path: filepath.Join(dir, "test.sock")}

	ln, err := tr.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	// Check socket file was created.
	if _, err := os.Stat(tr.Path()); err != nil {
		t.Fatalf("socket file not found: %v", err)
	}

	// Check permissions are 0600.
	info, err := os.Stat(tr.Path())
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("socket perms: got %o, want 0600", info.Mode().Perm())
	}

	// Dial must succeed while listener is active.
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()

	conn, err := tr.Dial()
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	conn.Close()
	<-done
}

func TestUnixTransport_Path(t *testing.T) {
	tr := &UnixTransport{path: "/tmp/ancora.sock"}
	if tr.Path() != "/tmp/ancora.sock" {
		t.Errorf("Path: got %q", tr.Path())
	}
}

func TestUnixTransport_Close_RemovesSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	tr := &UnixTransport{path: sockPath}

	ln, err := tr.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ln.Close()

	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Errorf("socket still exists after Close")
	}
}

func TestUnixTransport_Close_NoFile_IsNoop(t *testing.T) {
	tr := &UnixTransport{path: "/tmp/no-such-file-ancora-test.sock"}
	// Must not return error when socket file doesn't exist.
	if err := tr.Close(); err != nil {
		t.Errorf("Close on missing file: %v", err)
	}
}

// ─── removeStaleSocket ────────────────────────────────────────────────────────

func TestRemoveStaleSocket_NoFile(t *testing.T) {
	// No file — should be a no-op.
	err := removeStaleSocket("/tmp/ancora-nonexistent-test.sock")
	if err != nil {
		t.Errorf("removeStaleSocket on missing file: %v", err)
	}
}

func TestRemoveStaleSocket_StaleFile(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "stale.sock")

	// Create a fake stale socket file (no listener).
	if err := os.WriteFile(sockPath, []byte("stale"), 0600); err != nil {
		t.Fatalf("create stale file: %v", err)
	}

	if err := removeStaleSocket(sockPath); err != nil {
		t.Fatalf("removeStaleSocket: %v", err)
	}
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Errorf("stale socket not removed")
	}
}

func TestRemoveStaleSocket_LiveSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "live.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("create live socket: %v", err)
	}
	defer ln.Close()

	// Socket with live listener — removeStaleSocket must NOT remove it.
	if err := removeStaleSocket(sockPath); err != nil {
		t.Fatalf("removeStaleSocket: %v", err)
	}
	if _, err := os.Stat(sockPath); err != nil {
		t.Errorf("live socket was removed: %v", err)
	}
}

// ─── New() factory ────────────────────────────────────────────────────────────

func TestNew_CreatesSocketDir(t *testing.T) {
	base := t.TempDir()
	socketDir := filepath.Join(base, "subdir", "syfra")

	tr, err := New("ancora", socketDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Directory must be created.
	info, err := os.Stat(socketDir)
	if err != nil {
		t.Fatalf("socket dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("socket dir is not a directory")
	}

	expected := filepath.Join(socketDir, "ancora.sock")
	if tr.Path() != expected {
		t.Errorf("Path: got %q, want %q", tr.Path(), expected)
	}
}

func TestNew_DefaultDir_UsesHomeSyfra(t *testing.T) {
	// Empty socketDir → defaults to ~/.syfra.
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".syfra", "ancora.sock")

	tr, err := New("ancora", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if tr.Path() != expected {
		t.Errorf("Path: got %q, want %q", tr.Path(), expected)
	}
}

// ─── LoadOrCreateSecret ───────────────────────────────────────────────────────

func TestLoadOrCreateSecret_CreatesNewSecret(t *testing.T) {
	dir := t.TempDir()
	secret, err := LoadOrCreateSecret(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateSecret: %v", err)
	}

	// Must be 64 hex chars (32 bytes).
	if len(secret) != 64 {
		t.Errorf("secret length: got %d, want 64", len(secret))
	}

	// File must exist with correct permissions.
	secretPath := filepath.Join(dir, "ipc-secret")
	info, err := os.Stat(secretPath)
	if err != nil {
		t.Fatalf("secret file not created: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("secret file perms: got %o, want 0600", info.Mode().Perm())
	}
}

func TestLoadOrCreateSecret_IdempotentLoad(t *testing.T) {
	dir := t.TempDir()

	secret1, err := LoadOrCreateSecret(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	secret2, err := LoadOrCreateSecret(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if secret1 != secret2 {
		t.Errorf("secrets differ: %q vs %q", secret1, secret2)
	}
}

func TestLoadOrCreateSecret_RegeneratesCorrupt(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "ipc-secret")

	// Write a corrupt secret (not 64 chars).
	if err := os.WriteFile(secretPath, []byte("corrupt"), 0600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	secret, err := LoadOrCreateSecret(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateSecret: %v", err)
	}
	if len(secret) != 64 {
		t.Errorf("regenerated secret length: got %d, want 64", len(secret))
	}
}

func TestLoadOrCreateSecret_DefaultDir(t *testing.T) {
	// Empty socketDir → uses ~/.syfra. Just verify no error.
	secret, err := LoadOrCreateSecret("")
	if err != nil {
		t.Fatalf("LoadOrCreateSecret default dir: %v", err)
	}
	if len(secret) != 64 {
		t.Errorf("secret length: got %d, want 64", len(secret))
	}
}

// ─── NamedPipeTransport (non-Windows stub) ────────────────────────────────────

func TestNamedPipeTransport_Path(t *testing.T) {
	tr := &NamedPipeTransport{name: "ancora"}
	want := `\\.\pipe\syfra-ancora`
	if tr.Path() != want {
		t.Errorf("Path: got %q, want %q", tr.Path(), want)
	}
}

func TestNamedPipeTransport_Listen_ReturnsError(t *testing.T) {
	tr := &NamedPipeTransport{name: "test"}
	_, err := tr.Listen()
	if err == nil {
		t.Error("expected error on non-Windows Listen, got nil")
	}
}

func TestNamedPipeTransport_Dial_ReturnsError(t *testing.T) {
	tr := &NamedPipeTransport{name: "test"}
	_, err := tr.Dial()
	if err == nil {
		t.Error("expected error on non-Windows Dial, got nil")
	}
}

func TestNamedPipeTransport_Close_NoOp(t *testing.T) {
	tr := &NamedPipeTransport{name: "test"}
	if err := tr.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// ─── Full roundtrip: Listen -> Dial -> exchange data ─────────────────────────

func TestUnixTransport_FullRoundtrip(t *testing.T) {
	dir := t.TempDir()
	tr := &UnixTransport{path: filepath.Join(dir, "roundtrip.sock")}
	defer tr.Close()

	ln, err := tr.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	msg := "hello ipc\n"
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- fmt.Errorf("accept: %w", err)
			return
		}
		defer conn.Close()
		buf := make([]byte, len(msg))
		if _, err := conn.Read(buf); err != nil {
			errCh <- fmt.Errorf("read: %w", err)
			return
		}
		if string(buf) != msg {
			errCh <- fmt.Errorf("got %q, want %q", buf, msg)
			return
		}
		errCh <- nil
	}()

	conn, err := tr.Dial()
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if _, err := fmt.Fprint(conn, msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("server side: %v", err)
	}
}
