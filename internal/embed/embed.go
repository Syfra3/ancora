// Package embed provides local text embedding using the nomic-embed-text model.
//
// Ancora uses float32 vector embeddings for semantic search. The embedder is
// optional — if the GGUF model is not available, Ancora falls back to
// keyword-only FTS5 search transparently.
//
// # Architecture decision
//
// We chose a subprocess-based approach (llama.cpp CLI) rather than CGO
// bindings. Reasons:
//   - modernc.org/sqlite is pure Go; CGO would break the single-binary promise
//   - llama.cpp CGO bindings add significant build complexity
//   - The subprocess approach keeps the Go binary dependency-free at runtime
//   - Users who want semantic search install llama.cpp independently
//
// Model: nomic-embed-text-v1.5.Q4_K_M.gguf (768-dim, Apache 2.0)
// Size:  ~270MB (quantized)
package embed

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

var (
	osStatEmbed  = os.Stat
	execLookPath = exec.LookPath
	execCommand  = exec.Command
)

// ErrModelNotFound is returned when the GGUF model file does not exist.
// This is not a fatal error — the caller should fall back to keyword search.
var ErrModelNotFound = errors.New("ancora: nomic-embed model not found")

// ErrEmbedderUnavailable is returned when llama-embedding CLI is not in PATH.
var ErrEmbedderUnavailable = errors.New("ancora: llama-embedding binary not found in PATH")

// Dims is the embedding dimension for nomic-embed-text-v1.5.
const Dims = 768

// Embedder is the interface for text embedding.
type Embedder interface {
	// Embed returns a 768-dim float32 vector for the given text.
	// Returns ErrModelNotFound if the model file does not exist.
	// Returns ErrEmbedderUnavailable if the CLI binary is not in PATH.
	Embed(text string) ([]float32, error)
}

// NomicEmbedder embeds text using the nomic-embed-text-v1.5 GGUF model
// via the llama-embedding CLI (from llama.cpp).
type NomicEmbedder struct {
	ModelPath string // Path to the .gguf model file
	CLIPath   string // Path to llama-embedding binary (resolved at creation time)
}

// New creates a NomicEmbedder, resolving model path and CLI binary.
// Returns ErrModelNotFound if the model does not exist.
// Returns ErrEmbedderUnavailable if llama-embedding is not in PATH.
func New() (*NomicEmbedder, error) {
	modelPath := modelFilePath()

	if _, err := osStatEmbed(modelPath); os.IsNotExist(err) {
		return nil, ErrModelNotFound
	}

	cliPath, err := resolveEmbedCLI()
	if err != nil {
		return nil, ErrEmbedderUnavailable
	}

	return &NomicEmbedder{ModelPath: modelPath, CLIPath: cliPath}, nil
}

// Embed generates a 768-dim float32 embedding for text.
func (e *NomicEmbedder) Embed(text string) ([]float32, error) {
	if _, err := osStatEmbed(e.ModelPath); os.IsNotExist(err) {
		return nil, ErrModelNotFound
	}

	// llama-embedding outputs JSON lines: {"embedding": [...]}
	cmd := execCommand(e.CLIPath, embedCommandArgs(e.CLIPath, e.ModelPath, text)...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ancora: llama-embedding failed: %w (stderr: %s)", err, stderr.String())
	}

	return parseEmbeddingOutput(stdout.Bytes())
}

// ModelInstallPath returns the default directory where the model should be placed.
func ModelInstallPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ancora", "models")
}

// ModelFileName is the expected GGUF model filename.
const ModelFileName = "nomic-embed-text-v1.5.Q4_K_M.gguf"

// modelFilePath returns the resolved path to the GGUF model file.
// Priority: ANCORA_EMBED_MODEL env var → ~/.ancora/models/nomic-embed-text-v1.5.Q4_K_M.gguf
func modelFilePath() string {
	if p := strings.TrimSpace(os.Getenv("ANCORA_EMBED_MODEL")); p != "" {
		return p
	}
	return filepath.Join(ModelInstallPath(), ModelFileName)
}

// resolveEmbedCLI finds the llama-embedding binary in PATH.
// Tries multiple common names used by llama.cpp distributions.
func resolveEmbedCLI() (string, error) {
	candidates := []string{
		"llama-embedding",
		"llama.cpp-embedding",
		"embedding",
		"llama-cli",
	}
	for _, name := range candidates {
		if p, err := execLookPath(name); err == nil {
			return p, nil
		}
	}
	return "", ErrEmbedderUnavailable
}

func embedCommandArgs(cliPath, modelPath, text string) []string {
	args := []string{"--model", modelPath, "--embd-output-format", "json"}
	if path.Base(cliPath) == "llama-cli" {
		args = append(args, "--embeddings")
	}
	args = append(args, "-p", text)
	return args
}

// embeddingResponse handles both legacy llama-embedding output and the newer
// OpenAI-style JSON shape returned by modern llama.cpp.
type embeddingResponse struct {
	Embedding []float32 `json:"embedding"`
	Data      []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// parseEmbeddingOutput parses the JSON output from llama-embedding.
// Handles both single-object and newline-delimited JSON formats.
func parseEmbeddingOutput(data []byte) ([]float32, error) {
	data = bytes.TrimSpace(data)

	// Try direct JSON object first.
	var resp embeddingResponse
	if err := json.Unmarshal(data, &resp); err == nil {
		if embedding := firstEmbedding(resp); len(embedding) > 0 {
			return embedding, nil
		}
	}

	// Try newline-delimited JSON (take last non-empty line).
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var r embeddingResponse
		if err := json.Unmarshal(line, &r); err == nil {
			if embedding := firstEmbedding(r); len(embedding) > 0 {
				return embedding, nil
			}
		}
	}

	return nil, fmt.Errorf("ancora: could not parse embedding output (got %d bytes)", len(data))
}

func firstEmbedding(resp embeddingResponse) []float32 {
	if len(resp.Embedding) > 0 {
		return resp.Embedding
	}
	if len(resp.Data) > 0 && len(resp.Data[0].Embedding) > 0 {
		return resp.Data[0].Embedding
	}
	return nil
}

// MockEmbedder is a test embedder that returns a fixed vector.
// Used in unit tests to avoid requiring the real model.
type MockEmbedder struct {
	Vector []float32
	Err    error
}

// Embed returns the pre-set vector or error.
func (m *MockEmbedder) Embed(_ string) ([]float32, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Vector, nil
}
