package search

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Syfra3/ancora/internal/store"
)

var harnessSpec = store.EmbeddingSpec{Model: "fake-v1", Preprocessing: "title-dot-content-v1", Dimensions: 2}

type harnessEmbedder struct{}

func (harnessEmbedder) Embed(string) ([]float32, error)    { return []float32{1, 0}, nil }
func (harnessEmbedder) EmbeddingSpec() store.EmbeddingSpec { return harnessSpec }

func harnessComplete(t *testing.T, s *store.Store, id int64, vec []float32) {
	t.Helper()
	j, err := s.LeaseEmbedding(harnessSpec, time.Unix(100, 0), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if j.ObservationID != id {
		t.Fatalf("job %d want %d", j.ObservationID, id)
	}
	if err := s.CompleteEmbedding(*j, vec, time.Unix(101, 0)); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessReliabilityInvalidQueriesFallBack(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.CreateSession("harness", "w", ""); err != nil {
		t.Fatal(err)
	}
	id, err := s.AddObservation(store.AddObservationParams{SessionID: "harness", Type: "decision", Title: "fixture", Content: "keyword"})
	if err != nil {
		t.Fatal(err)
	}
	harnessComplete(t, s, id, []float32{1, 0})
	for name, vec := range map[string][]float32{"nan": {float32(math.NaN()), 1}, "positive-inf": {float32(math.Inf(1)), 1}, "negative-inf": {float32(math.Inf(-1)), 1}, "zero": {0, 0}, "empty": {}, "wrong-dim": {1}} {
		t.Run(name, func(t *testing.T) {
			results, mode, err := SearchWithOptions("keyword", store.SearchOptions{}, stubEmbedder{vec: vec}, s)
			if err != nil || mode != ModeKeyword || len(results) != 1 {
				t.Fatalf("bad query ranked: mode=%s results=%d err=%v", mode, len(results), err)
			}
			direct, err := s.SearchSemanticWithOptions(vec, store.SearchOptions{})
			if err != nil || len(direct) != 0 {
				t.Fatalf("direct bad query ranked: %v %+v", err, direct)
			}
		})
	}
}

// Scenario: Semantic scope precedes ranking. All data is in t.TempDir;
// the embedder is an in-process constant, never a model or subprocess.
func TestHarnessReliabilitySemanticScopePrecedesRanking(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.ConfigureEmbedding(harnessSpec); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("harness", "w", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 52; i++ {
		org, vec := "B", []float32{1, 0}
		if i >= 50 {
			org, vec = "A", []float32{0.8, 0.2}
		}
		id, err := s.AddObservation(store.AddObservationParams{SessionID: "harness", Type: "decision", Title: fmt.Sprintf("item %d", i), Content: "fixture", Workspace: "w", Visibility: "work", Organization: org})
		if err != nil {
			t.Fatal(err)
		}
		harnessComplete(t, s, id, vec)
	}
	for _, p := range []store.AddObservationParams{
		{Workspace: "elsewhere", Visibility: "work", Type: "decision"},
		{Workspace: "w", Visibility: "personal", Type: "decision"},
		{Workspace: "w", Visibility: "work", Type: "bugfix"},
	} {
		p.SessionID = "harness"
		p.Organization = "A"
		p.Title = "decoy " + p.Workspace + p.Visibility + p.Type
		p.Content = "fixture"
		id, err := s.AddObservation(p)
		if err != nil {
			t.Fatal(err)
		}
		harnessComplete(t, s, id, []float32{1, 0})
	}
	results, mode, err := SearchWithOptions("absentkeyword", store.SearchOptions{Workspace: " W ", Visibility: "project", Organization: "A", Type: "decision", Limit: 2}, harnessEmbedder{}, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || mode != ModeSemantic {
		t.Fatalf("eligible results starved: count=%d mode=%s", len(results), mode)
	}
	for _, r := range results {
		if r.Organization == nil || *r.Organization != "A" || r.Workspace == nil || *r.Workspace != "w" || r.Visibility != "work" || r.Type != "decision" || r.DeletedAt != nil {
			t.Fatalf("scope leak: %+v", r)
		}
	}
}

// Omitted and normalized scope predicates must agree in both candidate paths.
func TestHarnessReliabilityScopeParityAndUnavailableFallback(t *testing.T) {
	s := newSearchTestStore(t)
	if err := s.CreateSession("harness", "w", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureEmbedding(harnessSpec); err != nil {
		t.Fatal(err)
	}
	for i, p := range []store.AddObservationParams{
		{Workspace: "w", Visibility: "work", Organization: "A", Type: "decision"},
		{Workspace: "z", Visibility: "personal", Organization: "B", Type: "bugfix"},
		{Workspace: "w", Visibility: "personal", Organization: "A", Type: "decision"},
	} {
		p.SessionID = "harness"
		p.Title = fmt.Sprint("fixture ", i)
		p.Content = "commonword"
		id, err := s.AddObservation(p)
		if err != nil {
			t.Fatal(err)
		}
		harnessComplete(t, s, id, []float32{1, 0})
	}
	for _, opts := range []store.SearchOptions{{}, {Workspace: " W "}, {Visibility: "project"}, {Visibility: " "}, {Visibility: "PERSONAL"}, {Organization: "A"}, {Type: "bugfix"}, {Organization: " "}} {
		kw, err := s.Search("commonword", opts)
		if err != nil {
			t.Fatal(err)
		}
		sem, err := s.SearchSemanticWithOptions([]float32{1, 0}, opts)
		if err != nil {
			t.Fatal(err)
		}
		ids := map[int64]bool{}
		for _, r := range kw {
			ids[r.ID] = true
		}
		if len(kw) != len(sem) {
			t.Fatalf("parity %+v: kw=%d sem=%d", opts, len(kw), len(sem))
		}
		for _, r := range sem {
			if !ids[r.ID] {
				t.Fatalf("parity leak %+v", opts)
			}
		}
	}
	results, mode, err := SearchWithOptions("commonword", store.SearchOptions{}, nil, s)
	if err != nil || mode != ModeKeyword || len(results) != 3 {
		t.Fatalf("fallback: %v %s %d", err, mode, len(results))
	}
	if err := s.ConfigureEmbedding(store.EmbeddingSpec{Model: "other", Preprocessing: harnessSpec.Preprocessing, Dimensions: 2}); err != nil {
		t.Fatal(err)
	}
	results, mode, err = SearchWithOptions("commonword", store.SearchOptions{}, harnessEmbedder{}, s)
	if err != nil || mode != ModeKeyword || len(results) != 3 {
		t.Fatalf("model mismatch fallback: %v %s %d", err, mode, len(results))
	}
}
