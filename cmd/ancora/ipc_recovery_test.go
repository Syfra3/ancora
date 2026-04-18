package main

import (
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/Syfra3/ancora/internal/ipc"
	"github.com/Syfra3/ancora/internal/store"
)

type fakeIPCTransport struct {
	path        string
	closeCalls  int
	listenCalls int
}

func (f *fakeIPCTransport) Listen() (net.Listener, error) {
	f.listenCalls++
	return nil, nil
}

func (f *fakeIPCTransport) Dial() (net.Conn, error) { return nil, nil }
func (f *fakeIPCTransport) Path() string            { return f.path }
func (f *fakeIPCTransport) Close() error {
	f.closeCalls++
	return nil
}

type fakeIPCServer struct {
	startCalls int
	stopCalls  int
	startErrs  []error
}

func (f *fakeIPCServer) Start() error {
	f.startCalls++
	if len(f.startErrs) == 0 {
		return nil
	}
	err := f.startErrs[0]
	f.startErrs = f.startErrs[1:]
	return err
}

func (f *fakeIPCServer) Stop()          { f.stopCalls++ }
func (f *fakeIPCServer) Emit(ipc.Event) {}

type fakeIPCClient struct {
	connectCalls int
	connectErr   error
	closeCalls   int
}

func (f *fakeIPCClient) Connect() error {
	f.connectCalls++
	return f.connectErr
}

func (f *fakeIPCClient) Close()         { f.closeCalls++ }
func (f *fakeIPCClient) Emit(ipc.Event) {}

func TestStartIPCEventServerReclaimsUnresponsiveSocket(t *testing.T) {
	cfg := testConfig(t)
	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	transport := &fakeIPCTransport{path: "/tmp/ancora.sock"}
	server := &fakeIPCServer{startErrs: []error{fmt.Errorf("listen: %w", syscall.EADDRINUSE), nil}}
	client := &fakeIPCClient{connectErr: fmt.Errorf("ipc client: auth: no auth response: i/o timeout")}

	oldLoadSecret := ipcLoadOrCreateSecret
	oldNewTransport := ipcNewTransport
	oldNewServer := ipcNewServer
	oldNewClient := ipcNewClient
	t.Cleanup(func() {
		ipcLoadOrCreateSecret = oldLoadSecret
		ipcNewTransport = oldNewTransport
		ipcNewServer = oldNewServer
		ipcNewClient = oldNewClient
	})

	ipcLoadOrCreateSecret = func(string) (string, error) { return "secret", nil }
	ipcNewTransport = func(name, socketDir string) (ipc.Transport, error) {
		return transport, nil
	}
	ipcNewServer = func(ipc.Transport, string) ipcServer { return server }
	ipcNewClient = func(ipc.Transport, string) ipcClient { return client }

	stop, err := startIPCEventServer(s)
	if err != nil {
		t.Fatalf("startIPCEventServer: %v", err)
	}
	if stop == nil {
		t.Fatal("expected non-nil stop function")
	}
	if server.startCalls != 2 {
		t.Fatalf("server.Start calls = %d, want 2", server.startCalls)
	}
	if client.connectCalls != 1 {
		t.Fatalf("client.Connect calls = %d, want 1", client.connectCalls)
	}
	if transport.closeCalls != 1 {
		t.Fatalf("transport.Close calls = %d, want 1", transport.closeCalls)
	}

	stop()
	if server.stopCalls != 1 {
		t.Fatalf("server.Stop calls = %d, want 1", server.stopCalls)
	}
}

func TestShouldReclaimIPCServer(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "timeout text", err: fmt.Errorf("connect IPC client: no auth response: i/o timeout"), want: true},
		{name: "plain no auth response", err: fmt.Errorf("connect IPC client: no auth response"), want: true},
		{name: "auth rejected", err: fmt.Errorf("connect IPC client: auth rejected: ERR unauthorized"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldReclaimIPCServer(tc.err); got != tc.want {
				t.Fatalf("shouldReclaimIPCServer() = %v, want %v", got, tc.want)
			}
		})
	}
}
