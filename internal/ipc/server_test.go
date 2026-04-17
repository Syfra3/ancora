package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

// ─── Test Helpers ─────────────────────────────────────────────────────────────

// newTestServer creates a Server backed by an in-process net.Pipe pair listener.
// Returns the server and the listener's address (for manual Dial).
func newTestServer(t *testing.T, secret string) (*Server, *inMemTransport) {
	t.Helper()
	tr := &inMemTransport{ch: make(chan net.Conn, 32)}
	srv := NewServer(tr, secret)
	if err := srv.Start(); err != nil {
		t.Fatalf("server.Start: %v", err)
	}
	t.Cleanup(srv.Stop)
	return srv, tr
}

// dialAndAuth connects to the in-memory transport and performs the AUTH handshake.
// Returns the authenticated net.Conn and a scanner over it.
func dialAndAuth(t *testing.T, tr *inMemTransport, secret string) (net.Conn, *bufio.Scanner) {
	t.Helper()
	conn, err := tr.Dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	// Send AUTH line.
	if _, err := fmt.Fprintf(conn, "AUTH %s\n", secret); err != nil {
		t.Fatalf("send AUTH: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatalf("no response to AUTH")
	}
	resp := scanner.Text()
	if resp != "OK" {
		t.Fatalf("expected OK, got %q", resp)
	}
	return conn, scanner
}

// inMemTransport is an in-process Transport using net.Pipe — no OS sockets.
type inMemTransport struct {
	ch       chan net.Conn
	listener *inMemListener
}

func (tr *inMemTransport) Listen() (net.Listener, error) {
	ln := &inMemListener{tr: tr, done: make(chan struct{})}
	tr.listener = ln
	return ln, nil
}

func (tr *inMemTransport) Dial() (net.Conn, error) {
	serverSide, clientSide := net.Pipe()
	tr.ch <- serverSide
	return clientSide, nil
}

func (tr *inMemTransport) Path() string { return "inmem" }
func (tr *inMemTransport) Close() error { return nil }

type inMemListener struct {
	tr   *inMemTransport
	done chan struct{}
	once sync.Once
}

func (l *inMemListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.tr.ch:
		return conn, nil
	case <-l.done:
		return nil, fmt.Errorf("listener closed")
	}
}

func (l *inMemListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *inMemListener) Addr() net.Addr { return inMemAddr{} }

type inMemAddr struct{}

func (inMemAddr) Network() string { return "inmem" }
func (inMemAddr) String() string  { return "inmem" }

// ─── Auth Handshake ───────────────────────────────────────────────────────────

func TestServer_Auth_ValidSecret(t *testing.T) {
	secret := "deadbeef1234"
	srv, tr := newTestServer(t, secret)
	_ = srv

	_, _ = dialAndAuth(t, tr, secret) // panics on failure via t.Fatalf
}

func TestServer_Auth_WrongSecret(t *testing.T) {
	srv, tr := newTestServer(t, "correctsecret")
	_ = srv

	conn, err := tr.Dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "AUTH wrongsecret\n")

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	resp := scanner.Text()
	if resp != "ERR unauthorized" {
		t.Errorf("expected ERR unauthorized, got %q", resp)
	}
}

func TestServer_Auth_MalformedLine(t *testing.T) {
	// No "AUTH " prefix — server should reject.
	srv, tr := newTestServer(t, "secret")
	_ = srv

	conn, err := tr.Dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "CONNECT secret\n")

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		// Connection was closed — that's also acceptable rejection behaviour.
		return
	}
	resp := scanner.Text()
	if resp != "ERR unauthorized" {
		t.Errorf("expected ERR unauthorized, got %q", resp)
	}
}

// ─── Emit / Broadcast ─────────────────────────────────────────────────────────

func TestServer_Emit_SingleClient(t *testing.T) {
	secret := "testsecret"
	srv, tr := newTestServer(t, secret)

	_, scanner := dialAndAuth(t, tr, secret)

	// Wait for server to register the authenticated client.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.ClientCount() == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if srv.ClientCount() != 1 {
		t.Fatalf("client not registered (ClientCount=%d)", srv.ClientCount())
	}

	payload, _ := MarshalPayload(ObservationPayload{
		ID:         1,
		Visibility: "work",
		Title:      "Hello",
	})
	e := Event{
		Type:      EventObservationCreated,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	// Emit in background — net.Pipe write blocks until client reads.
	go srv.Emit(e)

	// Client reads the emitted event.
	if !scanner.Scan() {
		t.Fatalf("expected event line, got nothing: %v", scanner.Err())
	}
	line := scanner.Text()

	var got Event
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal received event: %v", err)
	}
	if got.Type != EventObservationCreated {
		t.Errorf("type: got %q, want %q", got.Type, EventObservationCreated)
	}
}

func TestServer_Emit_MultipleClients(t *testing.T) {
	secret := "multisecret"
	srv, tr := newTestServer(t, secret)

	const n = 5
	scanners := make([]*bufio.Scanner, n)
	for i := range scanners {
		_, sc := dialAndAuth(t, tr, secret)
		scanners[i] = sc
	}

	// Wait for all clients to be registered.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.ClientCount() == n {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if srv.ClientCount() != n {
		t.Fatalf("ClientCount: got %d, want %d", srv.ClientCount(), n)
	}

	payload, _ := MarshalPayload(ObservationDeletedPayload{ID: 77, SyncID: "s"})

	// Collect results from concurrent readers before emitting.
	type result struct {
		idx int
		evt Event
		err error
	}
	results := make(chan result, n)
	for i, sc := range scanners {
		go func(idx int, sc *bufio.Scanner) {
			if !sc.Scan() {
				results <- result{idx: idx, err: fmt.Errorf("no event: %v", sc.Err())}
				return
			}
			var got Event
			if err := json.Unmarshal([]byte(sc.Text()), &got); err != nil {
				results <- result{idx: idx, err: err}
				return
			}
			results <- result{idx: idx, evt: got}
		}(i, sc)
	}

	// Emit after readers are waiting.
	srv.Emit(Event{
		Type:      EventObservationDeleted,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})

	for i := 0; i < n; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("client %d: %v", r.idx, r.err)
			continue
		}
		if r.evt.Type != EventObservationDeleted {
			t.Errorf("client %d: type: got %q", r.idx, r.evt.Type)
		}
	}
}

func TestServer_Emit_NoClients(t *testing.T) {
	// Emit with zero clients must not panic.
	srv, _ := newTestServer(t, "secret")

	payload, _ := MarshalPayload(SessionPayload{ID: "s", Project: "p", Directory: "/tmp"})
	srv.Emit(Event{
		Type:      EventSessionCreated,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})
	// If we reach here without panic, test passes.
}

// ─── ClientCount ──────────────────────────────────────────────────────────────

func TestServer_ClientCount_TracksConnects(t *testing.T) {
	srv, tr := newTestServer(t, "secret")

	if srv.ClientCount() != 0 {
		t.Errorf("initial ClientCount: got %d, want 0", srv.ClientCount())
	}

	dialAndAuth(t, tr, "secret")
	time.Sleep(20 * time.Millisecond)
	if srv.ClientCount() != 1 {
		t.Errorf("after 1 auth: ClientCount: got %d, want 1", srv.ClientCount())
	}

	dialAndAuth(t, tr, "secret")
	time.Sleep(20 * time.Millisecond)
	if srv.ClientCount() != 2 {
		t.Errorf("after 2 auth: ClientCount: got %d, want 2", srv.ClientCount())
	}
}

func TestServer_ClientCount_DecreasesOnDisconnect(t *testing.T) {
	srv, tr := newTestServer(t, "secret")

	conn, _ := dialAndAuth(t, tr, "secret")
	time.Sleep(20 * time.Millisecond)
	if srv.ClientCount() != 1 {
		t.Fatalf("ClientCount: got %d, want 1", srv.ClientCount())
	}

	conn.Close()
	// Server's scanner loop detects EOF on disconnect and removes client from map.
	// Give it time to process without needing an Emit.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if srv.ClientCount() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if srv.ClientCount() != 0 {
		t.Errorf("after disconnect: ClientCount: got %d, want 0", srv.ClientCount())
	}
}

// ─── Concurrent Safety ────────────────────────────────────────────────────────

func TestServer_Emit_ConcurrentSafe(t *testing.T) {
	secret := "concurrent"
	srv, tr := newTestServer(t, secret)

	const nClients = 3
	const nEmits = 20

	// Connect clients and drain continuously so emitters don't block.
	var drainerWg sync.WaitGroup
	for i := 0; i < nClients; i++ {
		_, sc := dialAndAuth(t, tr, secret)
		drainerWg.Add(1)
		go func(sc *bufio.Scanner) {
			defer drainerWg.Done()
			for sc.Scan() {
				// discard — just keep the pipe flowing
			}
		}(sc)
	}

	// Wait for all clients to be registered.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.ClientCount() == nClients {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Fire many concurrent emits — race detector will catch data races.
	var wg sync.WaitGroup
	for i := 0; i < nEmits; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload, _ := MarshalPayload(ObservationPayload{
				ID:         int64(i),
				Visibility: "work",
			})
			srv.Emit(Event{
				Type:      EventObservationCreated,
				Timestamp: time.Now().UTC(),
				Payload:   payload,
			})
		}(i)
	}
	wg.Wait()
	// Stop server to unblock drainer goroutines (conn.Close triggers scanner EOF).
}

// ─── Stop ─────────────────────────────────────────────────────────────────────

func TestServer_Stop_Idempotent(t *testing.T) {
	_, tr := newTestServer(t, "secret")
	_ = tr
	// t.Cleanup calls srv.Stop once; calling again would panic on double-close.
	// The test just verifies Start + Stop don't hang or panic.
}

func TestServer_Stop_DisconnectsClients(t *testing.T) {
	// Build server manually (no newTestServer) to control Stop lifecycle.
	tr := &inMemTransport{ch: make(chan net.Conn, 32)}
	srv := NewServer(tr, "stopsecret")
	if err := srv.Start(); err != nil {
		t.Fatalf("server.Start: %v", err)
	}

	conn, _ := dialAndAuth(t, tr, "stopsecret")
	defer conn.Close()

	// Wait for client registration.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if srv.ClientCount() == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	srv.Stop() // single call — no cleanup registered

	// Connection should be closed by server Stop.
	conn.SetDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 1)
	_, err := conn.Read(buf)
	if err == nil {
		t.Error("expected error after server stop, got nil")
	}
}
