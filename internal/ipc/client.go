package ipc

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// Client connects to an existing IPC server (running in another process),
// authenticates, and forwards events over the shared socket.
//
// This is used when ancora mcp starts and the IPC socket is already owned by
// another ancora process (e.g. the system-installed binary). Rather than
// failing silently, the local build connects as a client and emits events
// through the existing server — so Vela receives them normally.
//
// It implements the same EventEmitter interface as *Server so the store wires
// it in identically.
type Client struct {
	transport Transport
	secret    string

	mu     sync.Mutex
	conn   net.Conn
	writer *bufio.Writer
}

// NewClient creates a Client that will emit events through the given transport.
// Call Connect before using Emit.
func NewClient(transport Transport, secret string) *Client {
	return &Client{
		transport: transport,
		secret:    secret,
	}
}

// Connect dials the server, performs the AUTH handshake, and keeps the
// connection open for subsequent Emit calls. Returns an error if the dial or
// auth fails.
func (c *Client) Connect() error {
	conn, err := c.transport.Dial()
	if err != nil {
		return fmt.Errorf("ipc client: dial: %w", err)
	}

	if err := c.authenticate(conn); err != nil {
		conn.Close()
		return fmt.Errorf("ipc client: auth: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.writer = bufio.NewWriter(conn)
	c.mu.Unlock()

	log.Printf("[ipc] client connected to %s (forwarding events to existing server)", c.transport.Path())
	return nil
}

// Emit serialises e and writes it to the server connection.
// If the connection is broken, it logs the error and drops the event —
// same non-blocking contract as Server.Emit.
func (c *Client) Emit(e Event) {
	wire, err := MarshalEvent(e)
	if err != nil {
		log.Printf("[ipc] client: marshal event %s: %v", e.Type, err)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.writer == nil {
		log.Printf("[ipc] client: not connected, dropping event %s", e.Type)
		return
	}

	if _, err := c.writer.Write(wire); err != nil {
		log.Printf("[ipc] client: write event %s: %v", e.Type, err)
		return
	}
	if err := c.writer.Flush(); err != nil {
		log.Printf("[ipc] client: flush event %s: %v", e.Type, err)
	}
}

// Close disconnects from the server.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.writer = nil
	}
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

func (c *Client) authenticate(conn net.Conn) error {
	if c.secret == "" {
		return nil // no-auth mode
	}

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}

	if _, err := fmt.Fprintf(conn, "AUTH %s\n", c.secret); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("no auth response: %w", err)
		}
		return fmt.Errorf("no auth response")
	}
	resp := strings.TrimSpace(scanner.Text())

	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("clear deadline: %w", err)
	}

	if resp != "OK" {
		return fmt.Errorf("auth rejected: %s", resp)
	}
	return nil
}
