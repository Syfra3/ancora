// Package search implements hybrid search combining FTS5 keyword search
// and vector semantic search using Reciprocal Rank Fusion (RRF).
//
// # Hybrid Search Strategy
//
// Ancora uses RRF to merge results from two independent rankers:
//  1. FTS5 keyword search (BM25-like ranking built into SQLite)
//  2. Cosine similarity over float32 embedding BLOBs
//
// RRF formula: score(d) = Σ 1 / (k + rank(d))
// where k=60 is the standard constant that dampens early-rank advantages.
//
// Graceful degradation:
//   - If no query vector (embedder unavailable): falls back to FTS5-only
//   - If FTS5 returns no results: falls back to semantic-only
//   - If both return results: RRF fusion with search_mode = "hybrid"
package search

import (
	"github.com/Syfra3/ancora/internal/store"
)

const (
	// RRFk is the standard dampening constant for Reciprocal Rank Fusion.
	RRFk = 60
)

// Mode describes which search backends contributed to the results.
type Mode string

const (
	ModeHybrid   Mode = "hybrid"
	ModeKeyword  Mode = "keyword"
	ModeSemantic Mode = "semantic"
)

// Result wraps a store.SearchResult with the search mode that produced it.
type Result struct {
	store.SearchResult
	Mode Mode
}

// HybridSearch merges FTS5 keyword results with vector semantic results
// using Reciprocal Rank Fusion. If queryVec is nil, falls back to keyword-only.
//
// Parameters:
//   - query: the FTS5 search query string
//   - queryVec: the embedding of the query (nil = keyword-only mode)
//   - limit: max number of results to return (default 10 if <=0)
//   - s: the ancora store
func HybridSearch(query string, queryVec []float32, limit int, s *store.Store) ([]Result, Mode, error) {
	if limit <= 0 {
		limit = 10
	}

	// Fetch FTS5 results.
	kwResults, err := s.Search(query, store.SearchOptions{Limit: limit * 2})
	if err != nil {
		return nil, ModeKeyword, err
	}

	// Semantic results — only if we have a query vector.
	var semResults []store.SearchResult
	if queryVec != nil {
		semResults, err = s.SearchSemantic(queryVec, limit*2)
		if err != nil {
			// Non-fatal: degrade to keyword-only.
			semResults = nil
		}
	}

	// Determine mode and fuse results.
	switch {
	case len(semResults) == 0 && len(kwResults) == 0:
		return nil, ModeKeyword, nil

	case queryVec == nil || len(semResults) == 0:
		// Keyword-only.
		out := make([]Result, 0, min(limit, len(kwResults)))
		for i := 0; i < limit && i < len(kwResults); i++ {
			out = append(out, Result{kwResults[i], ModeKeyword})
		}
		return out, ModeKeyword, nil

	case len(kwResults) == 0:
		// Semantic-only.
		out := make([]Result, 0, min(limit, len(semResults)))
		for i := 0; i < limit && i < len(semResults); i++ {
			out = append(out, Result{semResults[i], ModeSemantic})
		}
		return out, ModeSemantic, nil

	default:
		// Hybrid: RRF fusion.
		fused := rrf(kwResults, semResults, limit)
		return fused, ModeHybrid, nil
	}
}

// rrf applies Reciprocal Rank Fusion to two ranked lists and returns the top-limit results.
func rrf(kw, sem []store.SearchResult, limit int) []Result {
	scores := make(map[int64]float64)
	byID := make(map[int64]store.SearchResult)

	for rank, r := range kw {
		scores[r.ID] += 1.0 / float64(RRFk+rank+1)
		byID[r.ID] = r
	}
	for rank, r := range sem {
		scores[r.ID] += 1.0 / float64(RRFk+rank+1)
		if _, ok := byID[r.ID]; !ok {
			byID[r.ID] = r
		}
	}

	// Collect and sort by descending RRF score.
	type scored struct {
		id    int64
		score float64
	}
	sorted := make([]scored, 0, len(scores))
	for id, score := range scores {
		sorted = append(sorted, scored{id, score})
	}
	// Insertion sort (small N, typically <20 results).
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].score > sorted[j-1].score; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	out := make([]Result, 0, min(limit, len(sorted)))
	for i := 0; i < limit && i < len(sorted); i++ {
		sr := byID[sorted[i].id]
		sr.Rank = sorted[i].score
		out = append(out, Result{sr, ModeHybrid})
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
