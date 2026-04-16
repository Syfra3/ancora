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

// Auth protocol constants.
const (
	authPrefix  = "AUTH "
	authOK      = "OK\n"
	authErr     = "ERR unauthorized\n"
	authTimeout = 5 * time.Second
)

// Server listens on a Transport, authenticates clients via shared secret,
// and broadcasts Events to all authenticated subscribers.
//
// It is safe for concurrent use from multiple goroutines.
type Server struct {
	transport Transport
	secret    string

	mu       sync.RWMutex
	clients  map[*serverConn]struct{}
	listener net.Listener

	stopCh chan struct{}
	wg     sync.WaitGroup
}

type serverConn struct {
	conn   net.Conn
	writer *bufio.Writer
	mu     sync.Mutex
}

// NewServer creates a Server using the given Transport.
// secret is the shared IPC secret (hex-encoded, 64 chars).
func NewServer(transport Transport, secret string) *Server {
	return &Server{
		transport: transport,
		secret:    secret,
		clients:   make(map[*serverConn]struct{}),
		stopCh:    make(chan struct{}),
	}
}

// Start binds the socket and begins accepting connections.
// It returns immediately; client handling runs in background goroutines.
func (s *Server) Start() error {
	ln, err := s.transport.Listen()
	if err != nil {
		return fmt.Errorf("ipc server: start: %w", err)
	}
	s.listener = ln

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop(ln)
	}()

	log.Printf("[ipc] server listening at %s", s.transport.Path())
	return nil
}

// Stop closes the listener and disconnects all clients.
func (s *Server) Stop() {
	close(s.stopCh)
	if s.listener != nil {
		s.listener.Close()
	}
	s.mu.Lock()
	for sc := range s.clients {
		sc.conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	if err := s.transport.Close(); err != nil {
		log.Printf("[ipc] server: cleanup socket: %v", err)
	}
}

// Emit serialises e and sends it to every authenticated client.
// Slow or disconnected clients are removed without blocking the caller.
func (s *Server) Emit(e Event) {
	wire, err := MarshalEvent(e)
	if err != nil {
		log.Printf("[ipc] server: marshal event %s: %v", e.Type, err)
		return
	}

	s.mu.RLock()
	clients := make([]*serverConn, 0, len(s.clients))
	for sc := range s.clients {
		clients = append(clients, sc)
	}
	s.mu.RUnlock()

	var dead []*serverConn
	for _, sc := range clients {
		sc.mu.Lock()
		if _, err := sc.writer.Write(wire); err != nil {
			dead = append(dead, sc)
			sc.mu.Unlock()
			continue
		}
		if err := sc.writer.Flush(); err != nil {
			dead = append(dead, sc)
		}
		sc.mu.Unlock()
	}

	if len(dead) > 0 {
		s.mu.Lock()
		for _, sc := range dead {
			delete(s.clients, sc)
			sc.conn.Close()
		}
		s.mu.Unlock()
	}
}

// ClientCount returns the number of currently authenticated subscribers.
func (s *Server) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// ─── Internal ─────────────────────────────────────────────────────────────────

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return // clean shutdown
			default:
				log.Printf("[ipc] server: accept error: %v", err)
				return
			}
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			s.handleConn(c)
		}(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	// Auth handshake — client must send "AUTH <secret>\n" within authTimeout.
	if err := conn.SetDeadline(time.Now().Add(authTimeout)); err != nil {
		return
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}
	line := scanner.Text()

	if !strings.HasPrefix(line, authPrefix) || strings.TrimPrefix(line, authPrefix) != s.secret {
		_, _ = conn.Write([]byte(authErr))
		return
	}
	if _, err := conn.Write([]byte(authOK)); err != nil {
		return
	}

	// Clear deadline — keep the connection open for event streaming.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return
	}

	sc := &serverConn{
		conn:   conn,
		writer: bufio.NewWriter(conn),
	}

	s.mu.Lock()
	s.clients[sc] = struct{}{}
	s.mu.Unlock()

	log.Printf("[ipc] server: client connected from %s (total: %d)", conn.RemoteAddr(), s.ClientCount())

	// Block here until the connection drops (client disconnect / server Stop).
	// We use a tiny scanner that discards any data the client might send.
	for scanner.Scan() {
		// Clients are subscribers only; ignore any incoming data.
	}

	s.mu.Lock()
	delete(s.clients, sc)
	s.mu.Unlock()

	log.Printf("[ipc] server: client disconnected (total: %d)", s.ClientCount())
}
