package search

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Syfra3/ancora/internal/store"
)

// These manually assigned axes encode fixture labels, not model-generated embeddings.
var evalSpec = store.EmbeddingSpec{Model: "synthetic-eval-v1", Preprocessing: "fixture-labels-v1", Dimensions: 3}

type evalEmbedder struct {
	vec  []float32
	fail bool
}

func (e evalEmbedder) EmbeddingSpec() store.EmbeddingSpec { return evalSpec }
func (e evalEmbedder) Embed(string) ([]float32, error) {
	if e.fail {
		return nil, errors.New("synthetic failure")
	}
	return e.vec, nil
}

func evalStore(tb testing.TB) *store.Store {
	tb.Helper()
	cfg, err := store.DefaultConfig()
	if err != nil {
		tb.Fatal(err)
	}
	cfg.DataDir = tb.TempDir()
	s, err := store.New(cfg)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = s.Close() })
	if err = s.ConfigureEmbedding(evalSpec); err != nil {
		tb.Fatal(err)
	}
	if err = s.CreateSession("eval", "eval", ""); err != nil {
		tb.Fatal(err)
	}
	return s
}
func evalAdd(tb testing.TB, s *store.Store, title, content, workspace, visibility, org, typ string, vec []float32) int64 {
	tb.Helper()
	id, err := s.AddObservation(store.AddObservationParams{SessionID: "eval", Title: title, Content: content, Workspace: workspace, Visibility: visibility, Organization: org, Type: typ})
	if err != nil {
		tb.Fatal(err)
	}
	now := time.Unix(100, 0)
	job, err := s.LeaseEmbedding(evalSpec, now, time.Minute)
	if err != nil {
		tb.Fatal(err)
	}
	if job == nil || job.ObservationID != id {
		tb.Fatalf("unexpected embedding job: %+v, want %d", job, id)
	}
	if err = s.CompleteEmbedding(*job, vec, now); err != nil {
		tb.Fatal(err)
	}
	return id
}

type evalScore struct {
	recall, rr float64
	eligible   bool
}

func scoreEval(expected, ranked []int64, k int) evalScore {
	relevant := make(map[int64]bool)
	for _, id := range expected {
		relevant[id] = true
	}
	if len(relevant) == 0 {
		return evalScore{}
	}
	seen := make(map[int64]bool)
	hits := 0
	rr := 0.0
	for i, id := range ranked {
		if i >= k {
			break
		}
		if relevant[id] && !seen[id] {
			hits++
			seen[id] = true
			if rr == 0 {
				rr = 1 / float64(i+1)
			}
		}
	}
	return evalScore{float64(hits) / float64(len(relevant)), rr, true}
}

// scoreEvalSlots counts every returned slot, including repeated IDs. Group labels
// are independent of relevance and only apply within one query's ranking.
func scoreEvalSlots(expected, ranked []int64, groups map[int64]string, k int) (slots, noise, redundant int) {
	relevant := make(map[int64]bool)
	for _, id := range expected {
		relevant[id] = true
	}
	seenIDs := make(map[int64]bool)
	seenGroups := make(map[string]bool)
	for i, id := range ranked {
		if i >= k {
			break
		}
		slots++
		if !relevant[id] {
			noise++
		}
		group := groups[id]
		if seenIDs[id] || (group != "" && seenGroups[group]) {
			redundant++
		}
		seenIDs[id] = true
		if group != "" {
			seenGroups[group] = true
		}
	}
	return
}

func evalRate(n, denominator int) string {
	if denominator == 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(denominator))
}

func testEvalSlots(t *testing.T) {
	groups := map[int64]string{1: "a", 2: "a", 3: "b"}
	for _, tc := range []struct {
		name                       string
		expected, ranked           []int64
		k, slots, noise, redundant int
	}{
		{"mixed group and repeat", []int64{1, 2}, []int64{1, 2, 3, 3, 4}, 5, 5, 3, 2},
		{"noise can be redundant", []int64{1}, []int64{1, 2, 2}, 5, 3, 2, 2},
		{"cutoff", []int64{1}, []int64{1, 2, 2}, 1, 1, 0, 0},
		{"empty results", []int64{1}, nil, 5, 0, 0, 0},
		{"no relevance", nil, []int64{4, 4}, 5, 2, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slots, noise, redundant := scoreEvalSlots(tc.expected, tc.ranked, groups, tc.k)
			if slots != tc.slots || noise != tc.noise || redundant != tc.redundant {
				t.Fatalf("got slots/noise/redundant %d/%d/%d, want %d/%d/%d", slots, noise, redundant, tc.slots, tc.noise, tc.redundant)
			}
		})
	}
	if got := evalRate(0, 0); got != "N/A" {
		t.Fatalf("empty denominator: %s", got)
	}
	if got := evalRate(1, 4); got != "25.0%" {
		t.Fatalf("rate: %s", got)
	}
}

func TestEvalScorer(t *testing.T) {
	t.Run("slot metrics", testEvalSlots)
	for _, tc := range []struct {
		name             string
		expected, ranked []int64
		k                int
		want             evalScore
	}{
		{"empty relevance", nil, []int64{1}, 5, evalScore{}},
		{"missing", []int64{1}, nil, 5, evalScore{0, 0, true}},
		{"rank and duplicates", []int64{1, 2, 2}, []int64{9, 1, 1, 2}, 3, evalScore{0.5, 0.5, true}},
		{"cutoff", []int64{1}, []int64{2, 1}, 1, evalScore{0, 0, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scoreEval(tc.expected, tc.ranked, tc.k); got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestRetrievalEval(t *testing.T) {
	s := evalStore(t)
	ids := map[string]int64{}
	for _, f := range []struct {
		name, content, workspace, visibility, org, typ string
		vec                                            []float32
	}{
		{"jwt", "JWT authentication tokens", "eval", "work", "A", "decision", []float32{1, 0, 0}},
		{"jwt-paraphrase", "JWT tokens for authentication", "eval", "work", "A", "decision", []float32{1, 0, 0}},
		{"session", "login session cookies", "eval", "work", "A", "decision", []float32{0.9, 0.1, 0}},
		{"fts", "SQLite full text indexing", "eval", "work", "A", "decision", []float32{0, 1, 0}},
		{"backup", "database backup schedule", "eval", "work", "A", "decision", []float32{0, 0, 1}},
		{"foreign-workspace", "JWT authentication tokens", "other", "work", "A", "decision", []float32{1, 0, 0}},
		{"foreign-visibility", "JWT authentication tokens", "eval", "personal", "A", "decision", []float32{1, 0, 0}},
		{"foreign-org", "JWT authentication tokens", "eval", "work", "B", "decision", []float32{1, 0, 0}},
		{"foreign-type", "JWT authentication tokens", "eval", "work", "A", "bugfix", []float32{1, 0, 0}},
	} {
		ids[f.name] = evalAdd(t, s, f.name, f.content, f.workspace, f.visibility, f.org, f.typ, f.vec)
	}
	// These distinct observations express the same fact; relevance is labeled
	// independently for each query, not inferred from this group.
	groups := map[int64]string{ids["jwt"]: "jwt-auth", ids["jwt-paraphrase"]: "jwt-auth"}
	if ids["jwt"] == ids["jwt-paraphrase"] {
		t.Fatal("duplicate group must contain distinct observation IDs")
	}
	queries := []struct {
		name, text string
		expected   []string
		vec        []float32
	}{
		{"exact", "JWT authentication tokens", []string{"jwt", "jwt-paraphrase"}, []float32{1, 0, 0}},
		{"paraphrase", "sign in credentials", []string{"jwt", "jwt-paraphrase", "session"}, []float32{1, 0, 0}},
		{"multiple", "login authentication", []string{"jwt", "jwt-paraphrase", "session"}, []float32{1, 0, 0}},
		{"no-relevant", "volcano geology", nil, []float32{0, 1, 0}},
	}
	opts := store.SearchOptions{Workspace: "eval", Visibility: "work", Organization: "A", Type: "decision", Limit: 5}
	for _, variant := range []string{"keyword", "synthetic-hybrid", "error-fallback"} {
		recall, mrr := 0.0, 0.0
		eligible, violations, emptyQueries, emptyReturns := 0, 0, 0, 0
		slots, noise, redundant, emptyResults := 0, 0, 0, 0
		for _, q := range queries {
			var emb Embedder
			if variant == "synthetic-hybrid" {
				emb = evalEmbedder{vec: q.vec}
			}
			if variant == "error-fallback" {
				emb = evalEmbedder{fail: true}
			}
			got, mode, err := SearchWithOptions(q.text, opts, emb, s)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "error-fallback" {
				base, baseMode, err := SearchWithOptions(q.text, opts, nil, s)
				if err != nil || mode != baseMode || !reflect.DeepEqual(got, base) {
					t.Fatalf("fallback parity %s: %v", q.name, err)
				}
			}
			ranked := []int64{}
			names := []string{}
			for _, r := range got {
				ranked = append(ranked, r.ID)
				names = append(names, r.Title)
				if r.Workspace == nil || *r.Workspace != "eval" || r.Visibility != "work" || r.Organization == nil || *r.Organization != "A" || r.Type != "decision" {
					violations++
				}
			}
			expected := []int64{}
			for _, name := range q.expected {
				expected = append(expected, ids[name])
			}
			querySlots, queryNoise, queryRedundant := scoreEvalSlots(expected, ranked, groups, 5)
			slots += querySlots
			noise += queryNoise
			redundant += queryRedundant
			if querySlots == 0 {
				emptyResults++
			}
			sc := scoreEval(expected, ranked, 5)
			if sc.eligible {
				eligible++
				recall += sc.recall
				mrr += sc.rr
			} else {
				emptyQueries++
				if len(got) > 0 {
					emptyReturns++
				}
			}
			t.Logf("variant=%s query=%s mode=%s expected=%v ranked=%v recall=%.3f rr=%.3f eligible=%t", variant, q.name, mode, q.expected, names, sc.recall, sc.rr, sc.eligible)
		}
		t.Logf("SUMMARY variant=%s Recall@5=%.3f MRR@5=%.3f eligible=%d scope_violations=%d no_relevant=%d no_relevant_with_results=%d noise@5=%d/%d (%s) redundancy@5=%d/%d (%s) empty_results=%d/%d (%s)", variant, recall/float64(eligible), mrr/float64(eligible), eligible, violations, emptyQueries, emptyReturns, noise, slots, evalRate(noise, slots), redundant, slots, evalRate(redundant, slots), emptyResults, len(queries), evalRate(emptyResults, len(queries)))
		if variant == "synthetic-hybrid" && redundant == 0 {
			t.Fatal("fixture did not exercise labeled redundancy")
		}
		if violations != 0 {
			t.Fatalf("scope violations: %d", violations)
		}
	}
}

func BenchmarkRetrievalEval(b *testing.B) {
	for _, n := range []int{50, 500} {
		for _, variant := range []string{"keyword", "synthetic-hybrid"} {
			b.Run(fmt.Sprintf("n=%d/%s", n, variant), func(b *testing.B) {
				b.StopTimer()
				s := evalStore(b)
				for i := 0; i < n; i++ {
					evalAdd(b, s, fmt.Sprintf("document %04d", i), fmt.Sprintf("retrieval fixture record number %d", i), "eval", "work", "A", "decision", []float32{1, float32(i%7+1) / 10, 0})
				}
				var emb Embedder
				if variant == "synthetic-hybrid" {
					emb = evalEmbedder{vec: []float32{1, 0, 0}}
				}
				opts := store.SearchOptions{Workspace: "eval", Visibility: "work", Organization: "A", Type: "decision", Limit: 5}
				b.ReportAllocs()
				b.ResetTimer()
				b.StartTimer()
				for i := 0; i < b.N; i++ {
					if _, _, err := SearchWithOptions("retrieval fixture", opts, emb, s); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
