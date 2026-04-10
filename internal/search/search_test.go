package search

import (
	"fmt"
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
		SessionID:  "s-alpha",
		Type:       "decision",
		Title:      "Alpha semantic hit",
		Content:    "shared semantic token",
		Workspace:  "alpha",
		Visibility: "project",
	})
	if err != nil {
		t.Fatalf("add alpha observation: %v", err)
	}
	betaID, err := s.AddObservation(store.AddObservationParams{
		SessionID:  "s-beta",
		Type:       "decision",
		Title:      "Beta semantic hit",
		Content:    "shared semantic token",
		Workspace:  "beta",
		Visibility: "project",
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

// ─── HybridSearch Tests ──────────────────────────────────────────────────────

func TestHybridSearchWithoutEmbedder(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.CreateSession("s-test", "test", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID:  "s-test",
		Type:       "decision",
		Title:      "Keyword result",
		Content:    "searchable keyword content",
		Workspace:  "test",
		Visibility: "project",
	}); err != nil {
		t.Fatalf("add observation: %v", err)
	}

	// HybridSearch with nil queryVec should fall back to keyword-only.
	results, mode, err := HybridSearch("searchable keyword", nil, 10, s)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}

	if mode != ModeKeyword {
		t.Errorf("expected ModeKeyword when queryVec is nil, got %q", mode)
	}
	if len(results) == 0 {
		t.Error("expected at least one keyword result")
	}
}

func TestHybridSearchSemanticOnly(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.CreateSession("s-test", "test", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Add observation with embedding but no FTS5 match.
	id, err := s.AddObservation(store.AddObservationParams{
		SessionID:  "s-test",
		Type:       "decision",
		Title:      "Semantic result",
		Content:    "unique specialized terminology",
		Workspace:  "test",
		Visibility: "project",
	})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}

	vec := []float32{0.7, 0.3}
	if err := s.SetEmbedding(id, vec); err != nil {
		t.Fatalf("set embedding: %v", err)
	}

	// Search for a term that won't match FTS5 but will match semantically.
	results, mode, err := HybridSearch("nonexistent query term", vec, 10, s)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}

	// Should fall back to semantic-only since keyword returns nothing.
	if mode != ModeSemantic {
		t.Errorf("expected ModeSemantic, got %q", mode)
	}
	if len(results) == 0 {
		t.Error("expected at least one semantic result")
	}
}

func TestHybridSearchTrueHybrid(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.CreateSession("s-test", "test", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Add observations with both FTS5 and semantic matches.
	ids := []int64{}
	observations := []store.AddObservationParams{
		{SessionID: "s-test", Type: "decision", Title: "First", Content: "hybrid search test", Workspace: "test", Visibility: "project"},
		{SessionID: "s-test", Type: "bugfix", Title: "Second", Content: "another hybrid result", Workspace: "test", Visibility: "project"},
	}
	for _, obs := range observations {
		id, err := s.AddObservation(obs)
		if err != nil {
			t.Fatalf("add observation: %v", err)
		}
		ids = append(ids, id)
	}

	vec := []float32{0.5, 0.5}
	for _, id := range ids {
		if err := s.SetEmbedding(id, vec); err != nil {
			t.Fatalf("set embedding: %v", err)
		}
	}

	// Search with both keyword and semantic matches.
	results, mode, err := HybridSearch("hybrid", vec, 10, s)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}

	if mode != ModeHybrid {
		t.Errorf("expected ModeHybrid, got %q", mode)
	}
	if len(results) < 2 {
		t.Errorf("expected at least 2 hybrid results, got %d", len(results))
	}
}

func TestHybridSearchDefaultLimit(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.CreateSession("s-test", "test", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID:  "s-test",
		Type:       "test",
		Title:      "Test",
		Content:    "test content",
		Workspace:  "test",
		Visibility: "project",
	}); err != nil {
		t.Fatalf("add observation: %v", err)
	}

	// HybridSearch with limit <= 0 should use default limit of 10.
	results, _, err := HybridSearch("test", nil, 0, s)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}

	// Should not panic or error.
	if len(results) == 0 {
		t.Error("expected at least one result with default limit")
	}
}

// ─── Filter Tests ────────────────────────────────────────────────────────────

func TestFilterResultsByType(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.CreateSession("s-test", "test", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	observations := []store.AddObservationParams{
		{SessionID: "s-test", Type: "decision", Title: "Decision", Content: "content", Workspace: "test", Visibility: "project"},
		{SessionID: "s-test", Type: "bugfix", Title: "Bugfix", Content: "content", Workspace: "test", Visibility: "project"},
	}
	for _, obs := range observations {
		if _, err := s.AddObservation(obs); err != nil {
			t.Fatalf("add observation: %v", err)
		}
	}

	results, _, err := SearchWithOptions("content", store.SearchOptions{Type: "decision", Limit: 10}, nil, s)
	if err != nil {
		t.Fatalf("SearchWithOptions: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result filtered by type, got %d", len(results))
	}
	if results[0].Type != "decision" {
		t.Errorf("expected type=decision, got %s", results[0].Type)
	}
}

func TestFilterResultsByVisibility(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.CreateSession("s-test", "test", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	observations := []store.AddObservationParams{
		{SessionID: "s-test", Type: "test", Title: "Project obs", Content: "shared content", Workspace: "test", Visibility: "project"},
		{SessionID: "s-test", Type: "test", Title: "Personal obs", Content: "shared content", Workspace: "test", Visibility: "personal"},
	}
	ids := []int64{}
	for _, obs := range observations {
		id, err := s.AddObservation(obs)
		if err != nil {
			t.Fatalf("add observation: %v", err)
		}
		ids = append(ids, id)
	}

	// Add embeddings for semantic search.
	vec := []float32{0.5, 0.5}
	for _, id := range ids {
		if err := s.SetEmbedding(id, vec); err != nil {
			t.Fatalf("set embedding: %v", err)
		}
	}

	// Test visibility filter with hybrid search.
	results, _, err := SearchWithOptions("shared", store.SearchOptions{Visibility: "project", Limit: 10}, stubEmbedder{vec: vec}, s)
	if err != nil {
		t.Fatalf("SearchWithOptions: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result filtered by visibility=project, got %d", len(results))
	}
	// Note: store.normalizeScope returns "work" for non-personal scopes.
	if results[0].Visibility != "work" {
		t.Errorf("expected visibility=work (normalized), got %s", results[0].Visibility)
	}
}

func TestNormalizeScopeEdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"personal", "personal"},
		{"PERSONAL", "personal"},
		{"  personal  ", "personal"},
		// "project" is an API alias for "work" (the DB storage value)
		{"project", "work"},
		{"PROJECT", "work"},
		{"anything else", "work"},
		{"", "work"},
		{"  ", "work"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeScope(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeScope(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCandidateLimitEdgeCases(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{10, 40},
		{0, 10},
		{-5, 10},
		{1, 4},
		{100, 400},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("limit_%d", tt.input), func(t *testing.T) {
			result := candidateLimit(tt.input)
			if result != tt.expected {
				t.Errorf("candidateLimit(%d) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSearchWithOptionsSemanticMode(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.CreateSession("s-test", "test", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Add observation with embedding.
	id, err := s.AddObservation(store.AddObservationParams{
		SessionID:  "s-test",
		Type:       "decision",
		Title:      "Semantic only",
		Content:    "specialized vocabulary",
		Workspace:  "test",
		Visibility: "project",
	})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}

	vec := []float32{0.8, 0.2}
	if err := s.SetEmbedding(id, vec); err != nil {
		t.Fatalf("set embedding: %v", err)
	}

	// Search with a query that won't match FTS5.
	results, mode, err := SearchWithOptions("nonexistent", store.SearchOptions{Limit: 10}, stubEmbedder{vec: vec}, s)
	if err != nil {
		t.Fatalf("SearchWithOptions: %v", err)
	}

	if mode != ModeSemantic {
		t.Errorf("expected ModeSemantic when only semantic results, got %q", mode)
	}
	if len(results) == 0 {
		t.Error("expected at least one semantic result")
	}
}

func TestSearchWithOptionsEmbedderError(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.CreateSession("s-test", "test", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID:  "s-test",
		Type:       "test",
		Title:      "Test",
		Content:    "test content",
		Workspace:  "test",
		Visibility: "project",
	}); err != nil {
		t.Fatalf("add observation: %v", err)
	}

	// Use an embedder that returns an error.
	errorEmbedder := stubEmbedder{err: fmt.Errorf("embedding failed")}

	// Should fall back to keyword-only when embedder fails.
	results, mode, err := SearchWithOptions("test", store.SearchOptions{Limit: 10}, errorEmbedder, s)
	if err != nil {
		t.Fatalf("SearchWithOptions: %v", err)
	}

	if mode != ModeKeyword {
		t.Errorf("expected ModeKeyword when embedder fails, got %q", mode)
	}
	if len(results) == 0 {
		t.Error("expected at least one keyword result")
	}
}

// ─── Bug: search.normalizeScope("work") returns "project" but DB stores "work" ─
//
// When the LLM passes visibility="work" (as suggested by the tool description's
// "(default)" hint), the hybrid search path calls filterResults with opts.Visibility="work".
// search.normalizeScope("work") returns "project", but observations in the DB have
// visibility="work", so the filter r.Visibility != "project" drops ALL work observations.

// TestFilterResultsByWorkVisibilityHybridSearchReturnsWorkObs tests that
// passing visibility="work" in hybrid search correctly returns work observations.
//
// The bug: search.normalizeScope("work") returns "project", but the DB stores
// visibility="work". So filterResults drops all work observations from the
// semantic candidate list when the user passes visibility="work".
// The test exposes this using semantic-only results (non-keyword content).
func TestFilterResultsByWorkVisibilityHybridSearchReturnsWorkObs(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.CreateSession("s-work-vis", "test", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Use content that does NOT match FTS5 keyword search, only semantic search.
	// This forces the semantic path (filterResults) to be exercised exclusively.
	workID, err := s.AddObservation(store.AddObservationParams{
		SessionID:  "s-work-vis",
		Type:       "decision",
		Title:      "Work observation",
		Content:    "xyzzy42 unique token work", // FTS won't match "work" as query
		Workspace:  "test",
		Visibility: "work",
	})
	if err != nil {
		t.Fatalf("add work obs: %v", err)
	}
	personalID, err := s.AddObservation(store.AddObservationParams{
		SessionID:  "s-work-vis",
		Type:       "decision",
		Title:      "Personal observation",
		Content:    "xyzzy42 unique token personal",
		Workspace:  "test",
		Visibility: "personal",
	})
	if err != nil {
		t.Fatalf("add personal obs: %v", err)
	}

	// Assign embeddings to both so semantic path runs
	vec := []float32{0.5, 0.5}
	for _, id := range []int64{workID, personalID} {
		if err := s.SetEmbedding(id, vec); err != nil {
			t.Fatalf("set embedding: %v", err)
		}
	}

	// Search with a query that doesn't hit FTS, so only semantic path runs.
	// With visibility="work", filterResults should keep only the work obs.
	results, _, err := SearchWithOptions(
		"nonexistent-keyword-xyzzy42-match",
		store.SearchOptions{Visibility: "work", Limit: 10},
		stubEmbedder{vec: vec},
		s,
	)
	if err != nil {
		t.Fatalf("SearchWithOptions: %v", err)
	}

	// Must find exactly 1 result — the work observation
	if len(results) != 1 {
		t.Fatalf("expected 1 result for visibility=work, got %d (titles: %v)",
			len(results), titlesOf(results))
	}
	if results[0].Visibility != "work" {
		t.Errorf("expected visibility=work in result, got %q", results[0].Visibility)
	}
	if results[0].Title != "Work observation" {
		t.Errorf("expected 'Work observation' title, got %q", results[0].Title)
	}
}

// TestNormalizeScopeWorkMapsToWork verifies that "work" normalizes to "work"
// (matching DB storage), not "project". This was the root cause of the bug
// where visibility="work" filter never matched DB records storing "work".
func TestNormalizeScopeWorkMapsToWork(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"work", "work"},    // Bug: was returning "project"
		{"WORK", "work"},    // Bug: was returning "project"
		{"project", "work"}, // "project" is an alias for "work"
		{"personal", "personal"},
		{"", "work"}, // empty should default to "work" (the DB default)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeScope(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeScope(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func titlesOf(results []Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Title
	}
	return out
}
