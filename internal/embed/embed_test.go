package embed

import (
	"errors"
	"testing"
)

// TestMockEmbedder verifies the MockEmbedder satisfies the Embedder interface
// and returns the expected vector or error.
func TestMockEmbedder(t *testing.T) {
	t.Run("returns fixed vector", func(t *testing.T) {
		want := []float32{0.1, 0.2, 0.3}
		m := &MockEmbedder{Vector: want}

		got, err := m.Embed("any text")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != len(want) {
			t.Fatalf("expected %d dims, got %d", len(want), len(got))
		}
		for i, v := range want {
			if got[i] != v {
				t.Errorf("dim[%d]: expected %f, got %f", i, v, got[i])
			}
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		sentinel := errors.New("embed error")
		m := &MockEmbedder{Err: sentinel}

		_, err := m.Embed("any text")
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel error, got %v", err)
		}
	})
}

// TestModelNotFound verifies that New() returns ErrModelNotFound when
// the GGUF model file doesn't exist (the typical case before model download).
func TestModelNotFound(t *testing.T) {
	t.Setenv("ANCORA_EMBED_MODEL", "/nonexistent/path/to/model.gguf")

	_, err := New()
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}
}

// TestParseEmbeddingOutput tests the JSON output parser.
func TestParseEmbeddingOutput(t *testing.T) {
	t.Run("direct JSON object", func(t *testing.T) {
		input := `{"embedding": [0.1, 0.2, 0.3]}`
		got, err := parseEmbeddingOutput([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 3 || got[0] != 0.1 {
			t.Errorf("unexpected result: %v", got)
		}
	})

	t.Run("newline-delimited JSON", func(t *testing.T) {
		input := `{"other": "data"}
{"embedding": [0.4, 0.5, 0.6]}`
		got, err := parseEmbeddingOutput([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 3 || got[0] != 0.4 {
			t.Errorf("unexpected result: %v", got)
		}
	})

	t.Run("invalid input returns error", func(t *testing.T) {
		_, err := parseEmbeddingOutput([]byte(`{invalid json}`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

// TestMockEmbedderImplementsInterface is a compile-time check.
var _ Embedder = (*MockEmbedder)(nil)
var _ Embedder = (*NomicEmbedder)(nil)
