package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Syfra3/ancora/internal/store"
	mcppkg "github.com/mark3labs/mcp-go/mcp"
)

type harnessMCPEmbedder struct{}

func (harnessMCPEmbedder) Embed(string) ([]float32, error) { return []float32{1, 0}, nil }
func (harnessMCPEmbedder) EmbeddingSpec() store.EmbeddingSpec {
	return store.EmbeddingSpec{Model: "fake-mcp", Preprocessing: "title-dot-content-v1", Dimensions: 2}
}
func harnessRequest(args map[string]any) mcppkg.CallToolRequest {
	r := mcppkg.CallToolRequest{}
	r.Params.Arguments = args
	return r
}

// Calls handlers directly: no MCP server, IPC socket, external model or live DB.
func TestHarnessReliabilityMCPOrganizationScopeAndKeywordFallback(t *testing.T) {
	s := newMCPTestStore(t)
	spec := harnessMCPEmbedder{}.EmbeddingSpec()
	if err := s.ConfigureEmbedding(spec); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("harness", "w", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 52; i++ {
		org, title, vec := "B", fmt.Sprint("outside-", i), []float32{1, 0}
		if i >= 50 {
			org, title, vec = "A", fmt.Sprint("eligible-", i), []float32{0.8, 0.2}
		}
		_, err := s.AddObservation(store.AddObservationParams{SessionID: "harness", Type: "decision", Title: title, Content: "commonword", Workspace: "w", Visibility: "work", Organization: org})
		if err != nil {
			t.Fatal(err)
		}
		j, err := s.LeaseEmbedding(spec, time.Unix(100, 0), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.CompleteEmbedding(*j, vec, time.Unix(100, 0)); err != nil {
			t.Fatal(err)
		}
	}
	for _, semantic := range []bool{false, true} {
		cfg := MCPConfig{}
		query := "commonword"
		if semantic {
			cfg.Embedder = harnessMCPEmbedder{}
			query = "absentkeyword"
		}
		res, err := handleSearch(s, cfg)(context.Background(), harnessRequest(map[string]any{"query": query, "workspace": " W ", "visibility": "project", "organization": "A", "type": "decision", "limit": float64(2)}))
		if err != nil || res.IsError {
			t.Fatalf("handler: %v %+v", err, res)
		}
		text := callResultText(t, res)
		if !strings.Contains(text, "Found 2 memories") || strings.Contains(text, "outside-") || !strings.Contains(text, "eligible-") {
			t.Fatalf("scoped ranking: %s", text)
		}
	}
}

func TestHarnessReliabilityMCPSaveTopicUpdateAndDeleteDurableWithoutWorker(t *testing.T) {
	s := newMCPTestStore(t)
	spec := harnessMCPEmbedder{}.EmbeddingSpec()
	if err := s.ConfigureEmbedding(spec); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"session_id": "harness", "workspace": "w", "visibility": "personal", "title": "title", "content": "first", "topic_key": "harness/topic"}
	for i := 0; i < 2; i++ {
		if i == 1 {
			args["content"] = "second"
		}
		res, err := handleSave(s, MCPConfig{})(context.Background(), harnessRequest(args))
		if err != nil || res.IsError {
			t.Fatalf("save: %v %+v", err, res)
		}
	}
	jobs, err := s.ListEmbeddingJobs(0, 10)
	if err != nil || len(jobs) != 1 || jobs[0].Revision != 2 || jobs[0].Input != "title. second" {
		t.Fatalf("topic durable: %+v %v", jobs, err)
	}
	old, err := s.LeaseEmbedding(spec, time.Unix(100, 0), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	res, err := handleUpdate(s)(context.Background(), harnessRequest(map[string]any{"id": float64(old.ObservationID), "content": "third"}))
	if err != nil || res.IsError {
		t.Fatalf("update: %v %+v", err, res)
	}
	if err := s.CompleteEmbedding(*old, []float32{1, 0}, time.Unix(100, 0)); !errors.Is(err, store.ErrEmbeddingFence) {
		t.Fatalf("stale update: %v", err)
	}
	late, err := s.LeaseEmbedding(spec, time.Unix(100, 0), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	res, err = handleDelete(s)(context.Background(), harnessRequest(map[string]any{"id": float64(late.ObservationID)}))
	if err != nil || res.IsError {
		t.Fatalf("delete: %v %+v", err, res)
	}
	if err := s.CompleteEmbedding(*late, []float32{1, 0}, time.Unix(100, 0)); !errors.Is(err, store.ErrEmbeddingFence) {
		t.Fatalf("deleted completion: %v", err)
	}
}
