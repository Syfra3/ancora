package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syfra3/ancora/internal/embed"
	"github.com/Syfra3/ancora/internal/search"
	"github.com/Syfra3/ancora/internal/setup"
	"github.com/Syfra3/ancora/internal/store"
)

func installFakeEmbeddingCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "llama-embedding")
	script := `#!/bin/sh
prompt=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--prompt" ] || [ "$prev" = "-p" ]; then
    prompt="$arg"
  fi
  prev="$arg"
done
if [ -n "$ANCORA_FAKE_EMBED_LOG" ]; then
  printf '%s\n' "$prompt" >> "$ANCORA_FAKE_EMBED_LOG"
fi
kind="other"
case "$prompt" in
  *database*|*Database*|*query*|*Query*) kind="database" ;;
  *frontend*|*Frontend*|*react*|*React*|*UI*|*ui*) kind="frontend" ;;
esac
printf '{"data":[{"embedding":['
i=0
while [ "$i" -lt 768 ]; do
  val=0
  if [ "$kind" = "database" ] && [ "$i" -eq 0 ]; then val=1; fi
  if [ "$kind" = "frontend" ] && [ "$i" -eq 1 ]; then val=1; fi
  if [ "$kind" = "other" ] && [ "$i" -eq 2 ]; then val=1; fi
  if [ "$i" -gt 0 ]; then printf ','; fi
  printf '%s' "$val"
  i=$((i + 1))
done
printf ']}]}'
printf '\n'
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake llama-embedding: %v", err)
	}
	return dir
}

type stubEmbeddingsDownloader struct {
	downloadErr error
	downloaded  bool
}

func (d *stubEmbeddingsDownloader) Download() error {
	d.downloaded = true
	return d.downloadErr
}

func TestCmdEmbeddingsStatusPrintsVerificationDetails(t *testing.T) {
	oldCheck := setupCheckEmbeddingsStatus
	defer func() { setupCheckEmbeddingsStatus = oldCheck }()

	setupCheckEmbeddingsStatus = func() (*setup.EmbeddingsSetupResult, error) {
		return &setup.EmbeddingsSetupResult{
			ModelInstalled: true,
			CLIAvailable:   true,
			ModelPath:      "/tmp/model.gguf",
			CLIPath:        "/tmp/llama-embedding",
			Tested:         true,
			Usable:         true,
			TestDimensions: 768,
			Message:        "Embeddings fully configured and tested",
		}, nil
	}

	stdout, stderr := captureOutput(t, func() { cmdEmbeddingsStatus() })
	if stderr != "" {
		t.Fatalf("expected no stderr, got %q", stderr)
	}
	for _, want := range []string{"Verified: yes", "Dims:     768", "Status:   Embeddings fully configured and tested"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output, got:\n%s", want, stdout)
		}
	}
}

func TestRunEmbeddingsInstallReturnsStableContract(t *testing.T) {
	oldNewDownloader := newEmbeddingsDownloader
	oldCheckLlamaCpp := checkLlamaCpp
	defer func() {
		newEmbeddingsDownloader = oldNewDownloader
		checkLlamaCpp = oldCheckLlamaCpp
	}()

	stubDownloader := &stubEmbeddingsDownloader{}
	var gotDestPath string
	newEmbeddingsDownloader = func(destPath string) embeddingsDownloader {
		gotDestPath = destPath
		return stubDownloader
	}
	checkLlamaCpp = func() (bool, string) {
		return true, "/usr/local/bin/llama-cli"
	}

	result, err := runEmbeddingsInstall()
	if err != nil {
		t.Fatalf("runEmbeddingsInstall: %v", err)
	}
	if !stubDownloader.downloaded {
		t.Fatal("expected downloader to run")
	}
	if result.ModelPath != gotDestPath {
		t.Fatalf("model path = %q, want %q", result.ModelPath, gotDestPath)
	}
	if !strings.HasSuffix(result.ModelPath, filepath.Join("models", embed.ModelFileName)) {
		t.Fatalf("unexpected model path: %q", result.ModelPath)
	}
	if !result.CLIFound || result.CLIPath != "/usr/local/bin/llama-cli" {
		t.Fatalf("unexpected CLI result: %#v", result)
	}
}

func TestRunEmbeddingsInstallPropagatesDownloadFailure(t *testing.T) {
	oldNewDownloader := newEmbeddingsDownloader
	defer func() { newEmbeddingsDownloader = oldNewDownloader }()

	newEmbeddingsDownloader = func(string) embeddingsDownloader {
		return &stubEmbeddingsDownloader{downloadErr: errors.New("network down")}
	}

	_, err := runEmbeddingsInstall()
	if err == nil || !strings.Contains(err.Error(), "download failed: network down") {
		t.Fatalf("expected wrapped download error, got %v", err)
	}
}

func TestPrintEmbeddingsInstallResultHandlesCLIPresenceAndMissingCLI(t *testing.T) {
	oldInstructions := llamaCppInstallInstructions
	defer func() { llamaCppInstallInstructions = oldInstructions }()
	llamaCppInstallInstructions = func() string { return "install llama instructions" }

	t.Run("cli available", func(t *testing.T) {
		stdout, stderr := captureOutput(t, func() {
			printEmbeddingsInstallResult(&embeddingsInstallResult{
				ModelPath: "/tmp/model.gguf",
				CLIPath:   "/usr/local/bin/llama-cli",
				CLIFound:  true,
			})
		})
		if stderr != "" {
			t.Fatalf("expected no stderr, got %q", stderr)
		}
		for _, want := range []string{"Installing embedding model...", "✓ Model installed: /tmp/model.gguf", "✓ llama.cpp CLI found: /usr/local/bin/llama-cli", "Embeddings ready!"} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("expected %q in output, got:\n%s", want, stdout)
			}
		}
	})

	t.Run("cli missing", func(t *testing.T) {
		stdout, stderr := captureOutput(t, func() {
			printEmbeddingsInstallResult(&embeddingsInstallResult{ModelPath: "/tmp/model.gguf"})
		})
		if stderr != "" {
			t.Fatalf("expected no stderr, got %q", stderr)
		}
		for _, want := range []string{"Installing embedding model...", "✓ Model installed: /tmp/model.gguf", "llama.cpp CLI not found in PATH", "install llama instructions"} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("expected %q in output, got:\n%s", want, stdout)
			}
		}
	})
}

func TestRunBackfillUsesEmbeddingsForSemanticRetrieval(t *testing.T) {
	cfg := testConfig(t)
	logPath := filepath.Join(t.TempDir(), "embed.log")
	modelPath := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	cliDir := installFakeEmbeddingCLI(t)

	t.Setenv("ANCORA_EMBED_MODEL", modelPath)
	t.Setenv("ANCORA_FAKE_EMBED_LOG", logPath)
	t.Setenv("PATH", cliDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	s := mustOpenStore(t, cfg)
	defer s.Close()
	if err := s.CreateSession("backfill-session", "searchproj", "/tmp"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID: "backfill-session",
		Type:      "decision",
		Title:     "Database query tuning",
		Content:   "Database indexes made the slow query fast.",
		Project:   "searchproj",
		Scope:     "project",
	}); err != nil {
		t.Fatalf("AddObservation database: %v", err)
	}
	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID: "backfill-session",
		Type:      "decision",
		Title:     "Frontend button polish",
		Content:   "React UI cleanup for the settings button.",
		Project:   "searchproj",
		Scope:     "project",
	}); err != nil {
		t.Fatalf("AddObservation frontend: %v", err)
	}

	if err := runBackfill(cfg); err != nil {
		t.Fatalf("runBackfill: %v", err)
	}

	remaining, err := s.ListObservationsForEmbedding()
	if err != nil {
		t.Fatalf("ListObservationsForEmbedding: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected backfill to embed all observations, still missing %d", len(remaining))
	}

	queryEmbedder, err := embed.New()
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	queryVec, err := queryEmbedder.Embed("database query optimization")
	if err != nil {
		t.Fatalf("Embed query: %v", err)
	}
	semanticResults, err := s.SearchSemantic(queryVec, 5)
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}
	if len(semanticResults) == 0 {
		t.Fatal("expected semantic results after backfill")
	}
	if semanticResults[0].Title != "Database query tuning" {
		t.Fatalf("expected database observation first, got %q", semanticResults[0].Title)
	}

	hybridResults, mode, err := search.HybridSearch("database query", queryVec, 5, s)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if mode != search.ModeHybrid {
		t.Fatalf("expected hybrid mode, got %q", mode)
	}
	if len(hybridResults) == 0 || hybridResults[0].Title != "Database query tuning" {
		t.Fatalf("expected hybrid result to use embeddings for database observation, got %#v", hybridResults)
	}

	rawLog, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read embed log: %v", err)
	}
	logText := string(rawLog)
	for _, want := range []string{"test", "Database query tuning. Database indexes made the slow query fast.", "Frontend button polish. React UI cleanup for the settings button.", "database query optimization"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("expected fake embedder to be invoked with %q, log was:\n%s", want, logText)
		}
	}
}

func mustOpenStore(t *testing.T, cfg store.Config) *store.Store {
	t.Helper()
	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return s
}
