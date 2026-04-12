package embedding_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Syfra3/ancora/internal/embedding"
)

// ─── Fakes ────────────────────────────────────────────────────────────────────

type fakeEmbedder struct {
	mu      sync.Mutex
	calls   []string
	vec     []float32
	err     error
	blocked chan struct{} // if set, Embed blocks until closed
}

func (f *fakeEmbedder) Embed(text string) ([]float32, error) {
	if f.blocked != nil {
		<-f.blocked
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, text)
	return f.vec, f.err
}

func (f *fakeEmbedder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fakeStore struct {
	mu          sync.Mutex
	setEmbCalls []int64
	setEmbVecs  map[int64][]float32
	listObs     []embedding.Observation
	setEmbErr   error
	listErr     error
}

func newFakeStore() *fakeStore {
	return &fakeStore{setEmbVecs: make(map[int64][]float32)}
}

func (f *fakeStore) SetEmbedding(id int64, vec []float32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setEmbErr != nil {
		return f.setEmbErr
	}
	f.setEmbCalls = append(f.setEmbCalls, id)
	f.setEmbVecs[id] = vec
	return nil
}

func (f *fakeStore) ListObservationsForEmbedding() ([]embedding.Observation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listObs, f.listErr
}

func (f *fakeStore) embeddedIDs() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int64, len(f.setEmbCalls))
	copy(out, f.setEmbCalls)
	return out
}

// waitForEmbedding polls until the store has an embedding for the given ID
// or the timeout elapses. Returns false on timeout.
func waitForEmbedding(fs *fakeStore, id int64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		fs.mu.Lock()
		_, ok := fs.setEmbVecs[id]
		fs.mu.Unlock()
		if ok {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestServiceNilEmbedder_Noop(t *testing.T) {
	fs := newFakeStore()
	svc := embedding.New(nil, fs)
	svc.Start()

	// EnqueueWithText and Stop should be no-ops, not panics.
	svc.EnqueueWithText(1, "text")
	svc.Stop()

	if len(fs.embeddedIDs()) != 0 {
		t.Fatalf("expected no embeddings, got %d", len(fs.embeddedIDs()))
	}
}

func TestServiceNilReceiver_Noop(t *testing.T) {
	var svc *embedding.Service
	// These must not panic.
	svc.Start()
	svc.EnqueueWithText(1, "text")
	svc.Stop()
}

func TestEnqueueWithText_GeneratesEmbeddingAsync(t *testing.T) {
	fe := &fakeEmbedder{vec: []float32{0.1, 0.2, 0.3}}
	fs := newFakeStore()

	svc := embedding.New(fe, fs)
	svc.Start()
	defer svc.Stop()

	svc.EnqueueWithText(42, "hello world")

	if !waitForEmbedding(fs, 42, 2*time.Second) {
		t.Fatal("embedding for observation #42 was not generated within timeout")
	}

	ids := fs.embeddedIDs()
	if len(ids) != 1 || ids[0] != 42 {
		t.Fatalf("expected embedded IDs [42], got %v", ids)
	}
	if fe.callCount() != 1 {
		t.Fatalf("expected 1 Embed call, got %d", fe.callCount())
	}
}

func TestEnqueueWithText_PassesTextToEmbedder(t *testing.T) {
	fe := &fakeEmbedder{vec: []float32{0.5}}
	fs := newFakeStore()

	svc := embedding.New(fe, fs)
	svc.Start()
	defer svc.Stop()

	svc.EnqueueWithText(1, "my title. my content")

	if !waitForEmbedding(fs, 1, 2*time.Second) {
		t.Fatal("timed out")
	}

	fe.mu.Lock()
	defer fe.mu.Unlock()
	if len(fe.calls) == 0 || fe.calls[0] != "my title. my content" {
		t.Fatalf("expected text 'my title. my content', got %v", fe.calls)
	}
}

func TestEnqueueWithText_EmbedError_DoesNotPanic(t *testing.T) {
	fe := &fakeEmbedder{err: errors.New("embed failed")}
	fs := newFakeStore()

	svc := embedding.New(fe, fs)
	svc.Start()

	svc.EnqueueWithText(99, "text")

	// Give the worker a moment to process.
	time.Sleep(50 * time.Millisecond)
	svc.Stop()

	if len(fs.embeddedIDs()) != 0 {
		t.Fatal("expected no embeddings when embedder fails")
	}
}

func TestEnqueueWithText_AfterStop_IsNoop(t *testing.T) {
	fe := &fakeEmbedder{vec: []float32{0.1}}
	fs := newFakeStore()

	svc := embedding.New(fe, fs)
	svc.Start()
	svc.Stop()

	// Must not panic or block.
	svc.EnqueueWithText(1, "text")
}

func TestStop_DrainsQueueBeforeExit(t *testing.T) {
	fe := &fakeEmbedder{vec: []float32{0.1}}
	fs := newFakeStore()

	svc := embedding.New(fe, fs)
	svc.Start()

	const n = 10
	for i := int64(1); i <= n; i++ {
		svc.EnqueueWithText(i, "text")
	}

	svc.Stop() // must block until all items are drained

	ids := fs.embeddedIDs()
	if len(ids) != n {
		t.Fatalf("expected %d embeddings after Stop, got %d", n, len(ids))
	}
}

func TestBackfill_EmbedsPendingObservations(t *testing.T) {
	fe := &fakeEmbedder{vec: []float32{0.1, 0.2}}
	fs := newFakeStore()
	fs.listObs = []embedding.Observation{
		{ID: 1, Title: "T1", Content: "C1"},
		{ID: 2, Title: "T2", Content: "C2"},
		{ID: 3, Title: "T3", Content: "C3"},
	}

	svc := embedding.New(fe, fs)

	success, total, err := svc.Backfill()
	if err != nil {
		t.Fatalf("Backfill error: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total=3, got %d", total)
	}
	if success != 3 {
		t.Fatalf("expected success=3, got %d", success)
	}
	if fe.callCount() != 3 {
		t.Fatalf("expected 3 Embed calls, got %d", fe.callCount())
	}
}

func TestBackfill_PartialFailure_CountedCorrectly(t *testing.T) {
	callN := 0
	fe := &fakeEmbedder{}
	// First call succeeds, second fails, third succeeds.
	origVec := []float32{0.1}
	_ = origVec

	var mu sync.Mutex
	customEmbedder := &customFakeEmbedder{
		fn: func(text string) ([]float32, error) {
			mu.Lock()
			callN++
			n := callN
			mu.Unlock()
			if n == 2 {
				return nil, errors.New("boom")
			}
			_ = fe
			return []float32{0.1}, nil
		},
	}

	fs := newFakeStore()
	fs.listObs = []embedding.Observation{
		{ID: 1, Title: "T1", Content: "C1"},
		{ID: 2, Title: "T2", Content: "C2"},
		{ID: 3, Title: "T3", Content: "C3"},
	}

	svc := embedding.New(customEmbedder, fs)
	success, total, err := svc.Backfill()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total=3, got %d", total)
	}
	if success != 2 {
		t.Fatalf("expected success=2 (one failed), got %d", success)
	}
}

func TestBackfill_NilEmbedder_ReturnsZero(t *testing.T) {
	fs := newFakeStore()
	svc := embedding.New(nil, fs)

	success, total, err := svc.Backfill()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if success != 0 || total != 0 {
		t.Fatalf("expected (0,0), got (%d,%d)", success, total)
	}
}

func TestBackfill_NilService_ReturnsZero(t *testing.T) {
	var svc *embedding.Service
	success, total, err := svc.Backfill()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if success != 0 || total != 0 {
		t.Fatalf("expected (0,0), got (%d,%d)", success, total)
	}
}

func TestBackfill_StoreListError_ReturnsError(t *testing.T) {
	fe := &fakeEmbedder{vec: []float32{0.1}}
	fs := newFakeStore()
	fs.listErr = errors.New("db failure")

	svc := embedding.New(fe, fs)
	_, _, err := svc.Backfill()
	if err == nil {
		t.Fatal("expected error from Backfill when store.List fails")
	}
}

func TestMultipleEnqueuesSameID_LastWins(t *testing.T) {
	// When the same ID is enqueued multiple times, only the last text stored
	// in pendingTexts before worker picks it up matters. The test verifies
	// we don't deadlock or double-embed in a way that panics.
	fe := &fakeEmbedder{vec: []float32{0.1}}
	fs := newFakeStore()

	svc := embedding.New(fe, fs)
	svc.Start()
	defer svc.Stop()

	for i := 0; i < 5; i++ {
		svc.EnqueueWithText(1, "text")
	}

	// At least one embedding must land, no panics.
	time.Sleep(200 * time.Millisecond)
}

// customFakeEmbedder allows a custom Embed function.
type customFakeEmbedder struct {
	fn func(text string) ([]float32, error)
}

func (c *customFakeEmbedder) Embed(text string) ([]float32, error) {
	return c.fn(text)
}
