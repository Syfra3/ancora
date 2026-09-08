package embedding

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Syfra3/ancora/internal/embed"
	"github.com/Syfra3/ancora/internal/store"
)

// Resolve real native identity from temporary bytes; inference is always fake.
type nativeRecoveryFixture struct {
	native *embed.NomicEmbedder
	infer  func(string) ([]float32, error)
}

func TestHarnessReliabilityInitializationRecoversWithoutRestart(t *testing.T) {
	for _, fault := range []string{"resolve", "configure"} {
		t.Run(fault, func(t *testing.T) {
			s, _, e := reliabilityService(t)
			e.spec.Model = "recovering-model"
			now := time.Unix(100, 0)
			resolveCalls, configCalls := 0, 0
			svc := newService(e, NewStoreAdapter(s), serviceHooks{now: func() time.Time { return now }, resolve: func(e Embedder) (store.EmbeddingSpec, error) {
				resolveCalls++
				if fault == "resolve" && resolveCalls == 1 {
					return store.EmbeddingSpec{}, errors.New("transient resolution failure")
				}
				return ResolveSpec(e)
			}, configure: func(epoch int64, spec store.EmbeddingSpec) (store.EmbeddingConfig, error) {
				configCalls++
				if fault == "configure" && configCalls == 1 {
					return store.EmbeddingConfig{}, errors.New("transient configuration failure")
				}
				return s.CompareAndSwapEmbedding(epoch, spec)
			}})
			if svc.initErr == nil {
				t.Fatal("initial fault not injected")
			}
			id := reliabilityObservation(t, s, "init retry")
			r, c := resolveCalls, configCalls
			for i := 0; i < 100; i++ {
				svc.EnqueueWithText(id, "wake")
				_, _ = svc.processDurable()
			}
			if resolveCalls != r || configCalls != c {
				t.Fatalf("retry deadline bypassed by wakeups: resolve %d/%d configure %d/%d", resolveCalls, r, configCalls, c)
			}
			j, err := s.GetEmbeddingJob(id)
			if err != nil || j.State != "pending" || j.Attempts != 0 {
				t.Fatalf("unavailable work: %+v %v", j, err)
			}
			now = now.Add(5 * time.Second)
			worked, err := svc.processDurable()
			if err != nil || !worked {
				t.Fatalf("same worker did not recover: %v %v", worked, err)
			}
			j, err = s.GetEmbeddingJob(id)
			if err != nil || j.State != "ready" || j.Model != e.spec.Model {
				t.Fatalf("wrong recovered identity: %+v %v", j, err)
			}
		})
	}
}

func TestHarnessReliabilityStaleWorkersCannotRevertSelection(t *testing.T) {
	s, older, e := reliabilityService(t)
	now := time.Now().Add(time.Minute)
	older.now = func() time.Time { return now }
	id := reliabilityObservation(t, s, "two workers")
	oldJob, err := s.LeaseEmbedding(e.spec, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	base, err := s.GetEmbeddingConfig()
	if err != nil {
		t.Fatal(err)
	}
	newEmbedder := &reliabilityEmbedder{spec: e.spec, fn: e.fn}
	newEmbedder.spec.Model = "newer-selection"
	newer := newService(newEmbedder, NewStoreAdapter(s), serviceHooks{now: func() time.Time { return now }})
	if err := s.CompleteEmbedding(*oldJob, []float32{1, 0}, now); !errors.Is(err, store.ErrEmbeddingFence) {
		t.Fatalf("held lease crossed selection: %v", err)
	}
	for i := 0; i < 3; i++ {
		_, _ = older.processDurable()
		now = now.Add(2 * time.Minute)
	}
	selected, err := s.GetEmbeddingConfig()
	if err != nil || selected.Model != newEmbedder.spec.Model || selected.Epoch <= base.Epoch {
		t.Fatalf("old worker reverted selection: %+v %v", selected, err)
	}
	if _, err := s.CompareAndSwapEmbedding(base.Epoch, e.spec); !errors.Is(err, store.ErrEmbeddingFence) {
		t.Fatalf("stale CAS accepted: %v", err)
	}
	if worked, err := newer.processDurable(); err != nil || !worked {
		t.Fatalf("new worker failed: %v %v", worked, err)
	}
	j, err := s.GetEmbeddingJob(id)
	if err != nil || j.State != "ready" || j.Model != newEmbedder.spec.Model {
		t.Fatalf("wrong certificate: %+v %v", j, err)
	}
	// A retired worker can rejoin only after its local bytes match selection.
	e.spec = newEmbedder.spec
	now = now.Add(2 * time.Minute)
	if _, err := older.processDurable(); err != nil {
		t.Fatalf("matching identity could not rejoin: %v", err)
	}
	joined, err := s.GetEmbeddingConfig()
	if err != nil || joined.Epoch != selected.Epoch {
		t.Fatalf("joining changed selection: %+v %v", joined, err)
	}
}

func TestHarnessReliabilityStaleInitializationDoesNotRebase(t *testing.T) {
	s, active, e := reliabilityService(t)
	now := time.Now().Add(time.Minute)
	active.now = func() time.Time { return now }
	delayedEmbedder := &reliabilityEmbedder{spec: e.spec, fn: e.fn}
	delayedEmbedder.spec.Model = "delayed-initializer"
	calls := 0
	delayed := newService(delayedEmbedder, NewStoreAdapter(s), serviceHooks{now: func() time.Time { return now }, configure: func(epoch int64, p store.EmbeddingSpec) (store.EmbeddingConfig, error) {
		calls++
		if calls == 1 {
			return store.EmbeddingConfig{}, errors.New("transient busy")
		}
		return s.CompareAndSwapEmbedding(epoch, p)
	}})
	e.spec.Model = "selected-after-initializer-snapshot"
	reliabilityObservation(t, s, "delayed init")
	if _, err := active.processDurable(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := delayed.processDurable(); !errors.Is(err, store.ErrEmbeddingFence) {
		t.Fatalf("delayed init rebased: %v", err)
	}
	c, err := s.GetEmbeddingConfig()
	if err != nil || c.Model != e.spec.Model {
		t.Fatalf("selection reverted: %+v %v", c, err)
	}
}

func TestHarnessReliabilityNativeReplacementDuringInference(t *testing.T) {
	s, _, _ := reliabilityService(t)
	dir := t.TempDir()
	model := filepath.Join(dir, "model")
	cli := filepath.Join(dir, "cli")
	if err := os.WriteFile(model, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, []byte("never-executed"), 0600); err != nil {
		t.Fatal(err)
	}
	vec := make([]float32, embed.Dims)
	vec[0] = 1
	calls := 0
	e := &nativeRecoveryFixture{native: &embed.NomicEmbedder{ModelPath: model, CLIPath: cli}, infer: func(string) ([]float32, error) {
		calls++
		if calls == 1 {
			if err := os.WriteFile(model, []byte("after"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return vec, nil
	}}
	now := time.Unix(100, 0)
	svc := newService(e, NewStoreAdapter(s), serviceHooks{now: func() time.Time { return now }})
	id := reliabilityObservation(t, s, "inflight replacement")
	if _, err := svc.processDurable(); err != nil {
		t.Fatal(err)
	}
	j, err := s.GetEmbeddingJob(id)
	if err != nil || j.State == "ready" {
		t.Fatalf("inflight old result certified: %+v %v", j, err)
	}
	if _, err := svc.processDurable(); err != nil {
		t.Fatal(err)
	}
	j, err = s.GetEmbeddingJob(id)
	if err != nil || j.State != "ready" || j.Model != e.EmbeddingSpec().Model || calls != 2 {
		t.Fatalf("same worker replacement failed: %+v %v calls=%d", j, err, calls)
	}
}

func TestHarnessReliabilityWorkerInvalidDocumentRetry(t *testing.T) {
	for name, vec := range map[string][]float32{"nan": {float32(math.NaN()), 1}, "positive-inf": {float32(math.Inf(1)), 1}, "negative-inf": {float32(math.Inf(-1)), 1}, "zero": {0, 0}, "empty": {}, "wrong-dim": {1}} {
		t.Run(name, func(t *testing.T) {
			s, svc, e := reliabilityService(t)
			id := reliabilityObservation(t, s, "invalid output")
			now := time.Unix(100, 0)
			svc.now = func() time.Time { return now }
			e.fn = func(string) ([]float32, error) { return vec, nil }
			for i := 1; i <= 5; i++ {
				if _, err := svc.processDurable(); err != nil {
					t.Fatal(err)
				}
				j, err := s.GetEmbeddingJob(id)
				if err != nil {
					t.Fatal(err)
				}
				want := "pending"
				if i == 5 {
					want = "exhausted"
				}
				if j.Attempts != i || j.State != want {
					t.Fatalf("invalid delivery not retried exactly once: %+v", j)
				}
				now = time.UnixMilli(j.NextAttempt)
			}
		})
	}
}

func (f *nativeRecoveryFixture) EmbeddingSpec() store.EmbeddingSpec {
	p, _ := ResolveSpec(f.native)
	return p
}
func (f *nativeRecoveryFixture) Embed(s string) ([]float32, error) { return f.infer(s) }

func TestHarnessReliabilitySameWorkerNativeReplacement(t *testing.T) {
	s, _, _ := reliabilityService(t)
	dir := t.TempDir()
	model := filepath.Join(dir, "model")
	cli := filepath.Join(dir, "cli")
	if err := os.WriteFile(model, []byte("model-A"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, []byte("fake-cli-bytes-never-executed"), 0600); err != nil {
		t.Fatal(err)
	}
	vec := make([]float32, embed.Dims)
	vec[0] = 1
	e := &nativeRecoveryFixture{native: &embed.NomicEmbedder{ModelPath: model, CLIPath: cli}, infer: func(string) ([]float32, error) { return vec, nil }}
	svc := New(e, NewStoreAdapter(s))
	now := time.Now()
	svc.now = func() time.Time { return now }
	id := reliabilityObservation(t, s, "native replacement")
	if _, err := svc.processDurable(); err != nil {
		t.Fatal(err)
	}
	old, err := s.GetEmbeddingJob(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, []byte("model-B"), 0600); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	for i := 0; i < 3; i++ {
		_, _ = svc.processDurable()
		now = now.Add(2 * time.Minute)
	}
	current, err := s.GetEmbeddingJob(id)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "ready" || current.Model == old.Model || current.Model != e.EmbeddingSpec().Model {
		t.Fatalf("same worker stalled after byte replacement: old=%+v current=%+v", old, current)
	}
}
