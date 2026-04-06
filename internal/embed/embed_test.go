package embed

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fakeEmbedCommand(t *testing.T, argLogPath, stdout, stderr string, exitCode int) func(string, ...string) *exec.Cmd {
	t.Helper()
	return func(name string, args ...string) *exec.Cmd {
		cmdArgs := []string{"-test.run=TestEmbedHelperProcess", "--", name}
		cmdArgs = append(cmdArgs, args...)
		cmd := exec.Command(os.Args[0], cmdArgs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_ARG_LOG="+argLogPath,
			"HELPER_STDOUT="+stdout,
			"HELPER_STDERR="+stderr,
			"HELPER_EXIT_CODE="+strconv.Itoa(exitCode),
		)
		return cmd
	}
}

func TestEmbedHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	if argLogPath := os.Getenv("HELPER_ARG_LOG"); argLogPath != "" {
		_ = os.WriteFile(argLogPath, []byte(strings.Join(os.Args[3:], "\n")), 0644)
	}
	_, _ = fmt.Fprint(os.Stdout, os.Getenv("HELPER_STDOUT"))
	_, _ = fmt.Fprint(os.Stderr, os.Getenv("HELPER_STDERR"))
	code, _ := strconv.Atoi(os.Getenv("HELPER_EXIT_CODE"))
	os.Exit(code)
}

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

	t.Run("openai style JSON object", func(t *testing.T) {
		input := `{"data":[{"embedding":[0.7,0.8,0.9]}]}`
		got, err := parseEmbeddingOutput([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 3 || got[0] != 0.7 {
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

	t.Run("newline-delimited openai style JSON", func(t *testing.T) {
		input := `{"other": "data"}
{"data":[{"embedding":[1.1,1.2,1.3]}]}`
		got, err := parseEmbeddingOutput([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 3 || got[0] != 1.1 {
			t.Errorf("unexpected result: %v", got)
		}
	})

	t.Run("empty output returns byte count error", func(t *testing.T) {
		_, err := parseEmbeddingOutput(nil)
		if err == nil {
			t.Fatal("expected error for empty output")
		}
		if !strings.Contains(err.Error(), "got 0 bytes") {
			t.Fatalf("expected byte-count error, got %v", err)
		}
	})

	t.Run("invalid input returns error", func(t *testing.T) {
		_, err := parseEmbeddingOutput([]byte(`{invalid json}`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestNomicEmbedderEmbedInvokesCLIWithExpectedContract(t *testing.T) {
	oldExecCommand := execCommand
	oldStat := osStatEmbed
	defer func() {
		execCommand = oldExecCommand
		osStatEmbed = oldStat
	}()

	modelPath := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	osStatEmbed = os.Stat

	argLogPath := filepath.Join(t.TempDir(), "args.log")
	execCommand = fakeEmbedCommand(t, argLogPath, `{"data":[{"embedding":[0.1,0.2,0.3]}]}`+"\n", "", 0)

	embedder := &NomicEmbedder{ModelPath: modelPath, CLIPath: "/tmp/llama-embedding"}
	vec, err := embedder.Embed("semantic prompt")
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if len(vec) != 3 || vec[0] != 0.1 || vec[2] != 0.3 {
		t.Fatalf("unexpected embedding vector: %#v", vec)
	}

	rawArgs, err := os.ReadFile(argLogPath)
	if err != nil {
		t.Fatalf("read arg log: %v", err)
	}
	got := strings.Split(strings.TrimSpace(string(rawArgs)), "\n")
	want := []string{
		"/tmp/llama-embedding",
		"--model", modelPath,
		"--embd-output-format", "json",
		"-p", "semantic prompt",
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected arg count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNomicEmbedderEmbedUsesEmbeddingsFlagForLlamaCLI(t *testing.T) {
	oldExecCommand := execCommand
	oldStat := osStatEmbed
	defer func() {
		execCommand = oldExecCommand
		osStatEmbed = oldStat
	}()

	modelPath := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	osStatEmbed = os.Stat

	argLogPath := filepath.Join(t.TempDir(), "args.log")
	execCommand = fakeEmbedCommand(t, argLogPath, `{"data":[{"embedding":[0.1,0.2,0.3]}]}`+"\n", "", 0)

	embedder := &NomicEmbedder{ModelPath: modelPath, CLIPath: "/usr/local/bin/llama-cli"}
	if _, err := embedder.Embed("semantic prompt"); err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}

	rawArgs, err := os.ReadFile(argLogPath)
	if err != nil {
		t.Fatalf("read arg log: %v", err)
	}
	got := strings.Split(strings.TrimSpace(string(rawArgs)), "\n")
	want := []string{
		"/usr/local/bin/llama-cli",
		"--model", modelPath,
		"--embd-output-format", "json",
		"--embeddings",
		"-p", "semantic prompt",
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected arg count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveEmbedCLIFindsLlamaCLI(t *testing.T) {
	oldLookPath := execLookPath
	defer func() { execLookPath = oldLookPath }()

	execLookPath = func(file string) (string, error) {
		if file == "llama-cli" {
			return "/usr/local/bin/llama-cli", nil
		}
		return "", errors.New("not found")
	}

	path, err := resolveEmbedCLI()
	if err != nil {
		t.Fatalf("resolveEmbedCLI: %v", err)
	}
	if path != "/usr/local/bin/llama-cli" {
		t.Fatalf("path = %q, want %q", path, "/usr/local/bin/llama-cli")
	}
}

func TestNomicEmbedderEmbedIncludesCLIStderrOnFailure(t *testing.T) {
	oldExecCommand := execCommand
	oldStat := osStatEmbed
	defer func() {
		execCommand = oldExecCommand
		osStatEmbed = oldStat
	}()

	modelPath := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	osStatEmbed = os.Stat

	execCommand = fakeEmbedCommand(t, filepath.Join(t.TempDir(), "args.log"), "", "boom on stderr", 23)
	embedder := &NomicEmbedder{ModelPath: modelPath, CLIPath: "/tmp/llama-embedding"}

	_, err := embedder.Embed("semantic prompt")
	if err == nil {
		t.Fatal("expected command failure")
	}
	if !strings.Contains(err.Error(), "boom on stderr") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

// TestMockEmbedderImplementsInterface is a compile-time check.
var _ Embedder = (*MockEmbedder)(nil)
var _ Embedder = (*NomicEmbedder)(nil)
