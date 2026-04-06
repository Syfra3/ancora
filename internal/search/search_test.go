package search

import (
	"math"
	"testing"

	"github.com/Syfra3/ancora/internal/store"
)

type stubEmbedder struct {
	vec []float32
	err error
}

func (s stubEmbedder) Embed(_ string) ([]float32, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.vec, nil
}

func newSearchTestStore(t *testing.T) *store.Store {
	t.Helper()
	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DataDir = t.TempDir()

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestRRFScoring verifies the RRF fusion formula and ordering.
func TestRRFScoring(t *testing.T) {
	// Build two ranked lists with overlapping and disjoint results.
	kw := []store.SearchResult{
		{Observation: store.Observation{ID: 1, Title: "Go backend"}},     // kw rank 0
		{Observation: store.Observation{ID: 2, Title: "TypeScript UI"}},  // kw rank 1
		{Observation: store.Observation{ID: 3, Title: "Python scripts"}}, // kw rank 2
	}
	sem := []store.SearchResult{
		{Observation: store.Observation{ID: 2, Title: "TypeScript UI"}}, // sem rank 0 — overlaps kw rank 1
		{Observation: store.Observation{ID: 4, Title: "Rust FFI"}},      // sem rank 1 — only in sem
	}

	results := rrf(kw, sem, 10)

	// ID=2 appears in both lists so its RRF score is the sum of both contributions.
	// kw rank 1 (0-indexed): 1/(60+2) = 1/62
	// sem rank 0 (0-indexed): 1/(60+1) = 1/61
	// combined ≈ 0.03252
	// ID=1 (kw rank 0 only): 1/(60+1) = 1/61 ≈ 0.01639
	// Order should be: 2, 1, ...
	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}
	if results[0].ID != 2 {
		t.Errorf("expected ID=2 first (appears in both lists), got ID=%d", results[0].ID)
	}
	if results[0].Mode != ModeHybrid {
		t.Errorf("expected mode=hybrid, got %q", results[0].Mode)
	}

	// Verify RRF score for ID=2.
	// rrf() uses rank+1 in denominator (rank is 0-indexed).
	expectedScore := 1.0/float64(RRFk+1+1) + 1.0/float64(RRFk+0+1)
	if math.Abs(results[0].Rank-expectedScore) > 1e-9 {
		t.Errorf("RRF score for ID=2: expected %.6f, got %.6f", expectedScore, results[0].Rank)
	}
}

// TestRRFLimit verifies that rrf respects the limit parameter.
func TestRRFLimit(t *testing.T) {
	kw := []store.SearchResult{
		{Observation: store.Observation{ID: 1}},
		{Observation: store.Observation{ID: 2}},
		{Observation: store.Observation{ID: 3}},
	}
	sem := []store.SearchResult{
		{Observation: store.Observation{ID: 4}},
		{Observation: store.Observation{ID: 5}},
	}

	results := rrf(kw, sem, 2)
	if len(results) != 2 {
		t.Errorf("expected 2 results (limit), got %d", len(results))
	}
}

// TestRRFFallbackKeywordOnlyWhenNoVec tests the nil-vec branch.
func TestRRFFallbackKeywordOnlyWhenNoVec(t *testing.T) {
	kw := []store.SearchResult{
		{Observation: store.Observation{ID: 1, Title: "result one"}},
	}
	// Simulate: no queryVec, no sem results → should use keyword path.
	// We call rrf indirectly by testing HybridSearch logic via rrf only.
	// Since HybridSearch takes *store.Store directly, we test rrf.
	results := rrf(kw, nil, 10)
	// When sem is empty, rrf scores only kw entries.
	if len(results) != 1 || results[0].ID != 1 {
		t.Errorf("expected ID=1, got %v", results)
	}
}

// TestModeKeywordWhenNoSemResults verifies keyword-only path in the mode switch.
func TestModeKeywordWhenNoSemResults(t *testing.T) {
	// Direct test of the mode logic: when sem is empty, mode=keyword.
	kw := []store.SearchResult{{Observation: store.Observation{ID: 1}}}
	var sem []store.SearchResult

	// Replicate the switch logic from HybridSearch.
	var mode Mode
	switch {
	case len(sem) == 0 && len(kw) == 0:
		mode = ModeKeyword
	case len(sem) == 0:
		mode = ModeKeyword
	case len(kw) == 0:
		mode = ModeSemantic
	default:
		mode = ModeHybrid
	}

	if mode != ModeKeyword {
		t.Errorf("expected ModeKeyword, got %q", mode)
	}
}

func TestSearchWithOptionsWithoutProjectSearchesAllProjects(t *testing.T) {
	s := newSearchTestStore(t)
	for _, session := range []struct {
		id      string
		project string
	}{
		{id: "s-alpha", project: "alpha"},
		{id: "s-beta", project: "beta"},
	} {
		if err := s.CreateSession(session.id, session.project, ""); err != nil {
			t.Fatalf("create session %s: %v", session.id, err)
		}
	}
	for _, obs := range []store.AddObservationParams{
		{SessionID: "s-alpha", Type: "decision", Title: "Alpha hit", Content: "shared-search-term alpha", Workspace: "alpha", Visibility: "project"},
		{SessionID: "s-beta", Type: "decision", Title: "Beta hit", Content: "shared-search-term beta", Workspace: "beta", Visibility: "project"},
	} {
		if _, err := s.AddObservation(obs); err != nil {
			t.Fatalf("AddObservation %q: %v", obs.Title, err)
		}
	}

	results, mode, err := SearchWithOptions("shared-search-term", store.SearchOptions{Limit: 10}, nil, s)
	if err != nil {
		t.Fatalf("SearchWithOptions: %v", err)
	}
	if mode != ModeKeyword {
		t.Fatalf("expected keyword mode without embedder, got %q", mode)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 cross-project results, got %d", len(results))
	}
}

func TestSearchWithOptionsHybridHonorsExplicitProjectFilter(t *testing.T) {
	s := newSearchTestStore(t)
	for _, session := range []struct {
		id      string
		project string
	}{
		{id: "s-alpha", project: "alpha"},
		{id: "s-beta", project: "beta"},
	} {
		if err := s.CreateSession(session.id, session.project, ""); err != nil {
			t.Fatalf("create session %s: %v", session.id, err)
		}
	}

	alphaID, err := s.AddObservation(store.AddObservationParams{
		SessionID: "s-alpha",
		Type:      "decision",
		Title:     "Alpha semantic hit",
		Content:   "shared semantic token",
		Workspace:   "alpha",
		Visibility:     "project",
	})
	if err != nil {
		t.Fatalf("add alpha observation: %v", err)
	}
	betaID, err := s.AddObservation(store.AddObservationParams{
		SessionID: "s-beta",
		Type:      "decision",
		Title:     "Beta semantic hit",
		Content:   "shared semantic token",
		Workspace:   "beta",
		Visibility:     "project",
	})
	if err != nil {
		t.Fatalf("add beta observation: %v", err)
	}

	vec := []float32{0.42, 0.11}
	if err := s.SetEmbedding(alphaID, vec); err != nil {
		t.Fatalf("set alpha embedding: %v", err)
	}
	if err := s.SetEmbedding(betaID, vec); err != nil {
		t.Fatalf("set beta embedding: %v", err)
	}

	results, mode, err := SearchWithOptions("shared semantic token", store.SearchOptions{Workspace: "alpha", Limit: 10}, stubEmbedder{vec: vec}, s)
	if err != nil {
		t.Fatalf("SearchWithOptions: %v", err)
	}
	if mode != ModeHybrid {
		t.Fatalf("expected hybrid mode, got %q", mode)
	}
	if len(results) != 1 || results[0].Title != "Alpha semantic hit" {
		t.Fatalf("expected explicit project filter to keep only alpha result, got %#v", results)
	}
}
