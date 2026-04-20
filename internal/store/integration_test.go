//go:build integration

package store

import (
	"testing"

	"github.com/Syfra3/ancora/internal/embed"
)

// TestIntegrationSaveEmbedHybridSearch is an end-to-end integration test:
// it saves an observation, stores a mock embedding vector, and retrieves it
// via SearchSemantic.
//
// Run with:
//
//	go test -tags integration ./internal/store/
//
// This test does NOT require the actual GGUF model — it uses embed.MockEmbedder
// returning a fixed vector. The purpose is to verify the full pipeline:
// store schema → SetEmbedding → SearchSemantic → correct result.
func TestIntegrationSaveEmbedHybridSearch(t *testing.T) {
	s := newTestStore(t)

	// Use a mock embedder — fixed 768-dim vector (all zeros except dim 0 = 1.0).
	dims := 768
	vec := make([]float32, dims)
	vec[0] = 1.0
	mock := &embed.MockEmbedder{Vector: vec}

	// Create session + observation.
	sessID := "integ-sess-1"
	if err := s.CreateSession(sessID, "integ-project", "/tmp"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	id, err := s.AddObservation(AddObservationParams{
		SessionID:  sessID,
		Type:       "decision",
		Title:      "Chose Go for local client",
		Content:    "Go compiles to a single binary with no runtime dependencies, ideal for CLI tools.",
		Workspace:  "integ-project",
		Visibility: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	// Embed and store the vector.
	embedding, err := mock.Embed("Chose Go for local client")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(embedding) != dims {
		t.Fatalf("expected %d dims, got %d", dims, len(embedding))
	}

	if err := s.SetEmbedding(id, embedding); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}

	// Query vector identical to stored vector → should return the observation.
	results, err := s.SearchSemantic(embedding, 5)
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one semantic search result")
	}
	if results[0].ID != id {
		t.Errorf("expected observation id=%d first, got id=%d", id, results[0].ID)
	}
	if results[0].Rank < 0.99 {
		t.Errorf("expected cosine similarity ~1.0, got %.4f", results[0].Rank)
	}

	t.Logf("Integration test passed: save → embed → search in %d dims", dims)
}
