package embedding

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Syfra3/ancora/internal/embed"
	"github.com/Syfra3/ancora/internal/store"
)

type reliabilityEmbedder struct {
	spec store.EmbeddingSpec
	fn   func(string) ([]float32, error)
}

func (e *reliabilityEmbedder) Embed(text string) ([]float32, error) { return e.fn(text) }
func (e *reliabilityEmbedder) EmbeddingSpec() store.EmbeddingSpec   { return e.spec }

func reliabilityService(t *testing.T) (*store.Store, *Service, *reliabilityEmbedder) {
	t.Helper()
	s, err := store.New(store.FallbackConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.CreateSession("fixture", "fixture", ""); err != nil {
		t.Fatal(err)
	}
	e := &reliabilityEmbedder{spec: store.EmbeddingSpec{Model: "fake-v1", Preprocessing: "title-dot-content-v1", Dimensions: 2}, fn: func(string) ([]float32, error) { return []float32{1, 0}, nil }}
	svc := New(e, NewStoreAdapter(s))
	svc.now = func() time.Time { return time.Unix(100, 0) }
	return s, svc, e
}
func reliabilityObservation(t *testing.T, s *store.Store, title string) int64 {
	t.Helper()
	id, err := s.AddObservation(store.AddObservationParams{SessionID: "fixture", Type: "decision", Title: title, Content: "safe <private>secret</private> content", Visibility: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Scenario: Work survives failure. 300 committed jobs exceed the old volatile
// capacity; discard that service, recover with a fresh one, and process all work.
func TestHarnessReliabilityWorkerRestartOverflowAndExactInput(t *testing.T) {
	s, svc, e := reliabilityService(t)
	var ids []int64
	for i := 0; i < 300; i++ {
		id := reliabilityObservation(t, s, fmt.Sprint("fixture ", i))
		ids = append(ids, id)
		svc.EnqueueWithText(id, "wrong unsanitized stale handler text")
	}
	recovered := New(e, NewStoreAdapter(s))
	recovered.now = svc.now
	e.fn = func(input string) ([]float32, error) {
		if input == "wrong unsanitized stale handler text" || strings.Contains(input, "secret") {
			t.Fatalf("untrusted input: %q", input)
		}
		return []float32{1, 0}, nil
	}
	for i := 0; i < 300; i++ {
		worked, err := recovered.processDurable()
		if err != nil || !worked {
			t.Fatalf("recovery %d: %v %v", i, worked, err)
		}
	}
	if worked, err := recovered.processDurable(); err != nil || worked {
		t.Fatalf("duplicate work: %v %v", worked, err)
	}
	for _, id := range ids {
		j, err := s.GetEmbeddingJob(id)
		if err != nil || j.State != "ready" {
			t.Fatalf("lost job %d: %+v %v", id, j, err)
		}
	}
}

func TestHarnessReliabilityWorkerLateCompletionAndUnavailableModel(t *testing.T) {
	s, svc, e := reliabilityService(t)
	id := reliabilityObservation(t, s, "original")
	e.fn = func(string) ([]float32, error) {
		title := "replacement"
		if _, err := s.UpdateObservation(id, store.UpdateObservationParams{Title: &title}); err != nil {
			t.Fatal(err)
		}
		return []float32{1, 0}, nil
	}
	if _, err := svc.processDurable(); err != nil {
		t.Fatal(err)
	}
	j, err := s.GetEmbeddingJob(id)
	if err != nil || j.State != "pending" || j.Revision != 2 {
		t.Fatalf("late completion: %+v %v", j, err)
	}
	e.fn = func(string) ([]float32, error) { return nil, embed.ErrModelNotFound }
	if _, err := svc.processDurable(); !errors.Is(err, embed.ErrModelNotFound) {
		t.Fatalf("unavailable model: %v", err)
	}
	j, err = s.GetEmbeddingJob(id)
	if err != nil || j.Attempts != 0 || j.State != "pending" {
		t.Fatalf("unavailable consumed attempts: %+v %v", j, err)
	}
	New(nil, NewStoreAdapter(s)).Start() // nil model never drops durable work
	kw, err := s.Search("replacement", store.SearchOptions{})
	if err != nil || len(kw) != 1 {
		t.Fatalf("keyword unavailable: %v %d", err, len(kw))
	}
	svc.now = func() time.Time { return time.Unix(161, 0) }
	e.fn = func(string) ([]float32, error) { return []float32{1, 0}, nil }
	if worked, err := svc.processDurable(); err != nil || !worked {
		t.Fatalf("model recovery: %v %v", worked, err)
	}
}

func TestHarnessReliabilityWorkerFailuresAndExplicitBackfill(t *testing.T) {
	s, svc, e := reliabilityService(t)
	id := reliabilityObservation(t, s, "failure")
	now := time.Unix(100, 0)
	svc.now = func() time.Time { return now }
	e.fn = func(string) ([]float32, error) { return nil, errors.New("fake failure") }
	for i := 0; i < 5; i++ {
		if worked, err := svc.processDurable(); err != nil || !worked {
			t.Fatalf("failure attempt %d: %v %v", i, worked, err)
		}
		j, err := s.GetEmbeddingJob(id)
		if err != nil {
			t.Fatal(err)
		}
		now = time.UnixMilli(j.NextAttempt)
	}
	if worked, err := svc.processDurable(); err != nil || worked {
		t.Fatalf("exhaustion spins: %v %v", worked, err)
	}
	e.fn = func(string) ([]float32, error) { return []float32{1, 0}, nil }
	success, total, err := svc.Backfill()
	if err != nil || success != 1 || total != 1 {
		t.Fatalf("explicit backfill %d/%d %v", success, total, err)
	}
	j, err := s.GetEmbeddingJob(id)
	if err != nil || j.State != "ready" || j.Attempts != 0 {
		t.Fatalf("reset: %+v %v", j, err)
	}
}

func TestHarnessReliabilityWorkerPollingAndStop(t *testing.T) {
	s, svc, _ := reliabilityService(t)
	id := reliabilityObservation(t, s, "poll without enqueue")
	svc.Start()
	deadline := time.Now().Add(3 * time.Second)
	for {
		j, err := s.GetEmbeddingJob(id)
		if err != nil {
			t.Fatal(err)
		}
		if j.State == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("poll did not discover committed job")
		}
		time.Sleep(time.Millisecond)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); svc.Stop() }()
	}
	wg.Wait()
	svc.EnqueueWithText(id, "after stop")
}

// Hash fixture files, never execute them or inspect a user's installed model.
func TestHarnessReliabilityNativeModelIdentityUsesBytes(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "model")
	cli := filepath.Join(dir, "cli")
	if err := os.WriteFile(model, []byte("model-one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, []byte("cli-one"), 0600); err != nil {
		t.Fatal(err)
	}
	e := &embed.NomicEmbedder{ModelPath: model, CLIPath: cli}
	first, err := ResolveSpec(e)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, []byte("model-two"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := ResolveSpec(e)
	if err != nil || first.Model == second.Model {
		t.Fatalf("path reused model identity: %v", err)
	}
	if err := os.WriteFile(cli, []byte("cli-two"), 0600); err != nil {
		t.Fatal(err)
	}
	third, err := ResolveSpec(e)
	if err != nil || second.Model == third.Model {
		t.Fatalf("inference identity: %v", err)
	}
	otherCLI := filepath.Join(dir, "llama-cli")
	if err := os.WriteFile(otherCLI, []byte("cli-two"), 0600); err != nil {
		t.Fatal(err)
	}
	e.CLIPath = otherCLI
	fourth, err := ResolveSpec(e)
	if err != nil || fourth.Preprocessing == third.Preprocessing {
		t.Fatalf("CLI flag identity: %v", err)
	}
}
