// Package embedding provides an asynchronous embedding service for Ancora.
//
// # Problem
//
// The store's AddObservation saves observations with embedding = NULL. Semantic
// search is blind to any observation until its embedding is generated. The
// manual "ancora embeddings backfill" was the only trigger — meaning every
// new observation from the MCP or HTTP save paths was invisible to semantic
// search until the user remembered to run backfill manually.
//
// # Solution
//
// Service wraps an Embedder and a Store in a background worker. Callers fire
// Enqueue(id) after a successful AddObservation and return immediately. The
// worker drains the queue, generates the embedding, and calls SetEmbedding.
//
// The service is also used for batch backfill, so cmdEmbeddingsBackfill and
// runBackfill delegate to Backfill() instead of duplicating the loop logic.
//
// # Future use
//
// The isolated module is intended as the foundation for background memory
// organization features (deduplication, delta merging, summarization) that
// also need access to observation embeddings.
package embedding

import (
	"fmt"
	"log"
	"sync"
)

// Embedder generates a float32 vector for a text string.
// Satisfied by embed.NomicEmbedder and embed.MockEmbedder.
type Embedder interface {
	Embed(text string) ([]float32, error)
}

// Observation is the minimal view of a store observation needed for embedding.
type Observation struct {
	ID      int64
	Title   string
	Content string
}

// Store is the subset of store.Store used by the embedding service.
type Store interface {
	SetEmbedding(observationID int64, vec []float32) error
	ListObservationsForEmbedding() ([]Observation, error)
}

// Service is a background embedding worker.
//
// Create with New(), call Start() once, then EnqueueWithText() after each save.
// Call Stop() for graceful shutdown (drains the queue before returning).
//
// If the embedder is nil (model not installed), EnqueueWithText is a no-op and
// Backfill returns immediately — the service degrades silently, exactly
// like the old behaviour.
type Service struct {
	embedder Embedder
	store    Store

	queue        chan int64
	pendingTexts map[int64]string // id → "title. content", protected by mu
	wg           sync.WaitGroup

	once    sync.Once
	stopCh  chan struct{}
	stopped bool
	mu      sync.Mutex
}

const defaultQueueSize = 256

// New creates a Service. embedder may be nil (disabled — no-op mode).
// store must not be nil.
func New(embedder Embedder, store Store) *Service {
	return &Service{
		embedder:     embedder,
		store:        store,
		queue:        make(chan int64, defaultQueueSize),
		pendingTexts: make(map[int64]string),
		stopCh:       make(chan struct{}),
	}
}

// Start launches the background worker goroutine. Call once after creating
// the service. Safe to call on a nil *Service (no-op).
func (svc *Service) Start() {
	if svc == nil || svc.embedder == nil {
		return
	}
	svc.once.Do(func() {
		svc.wg.Add(1)
		go svc.worker()
	})
}

// Stop signals the worker to stop and waits for it to drain the queue.
// Safe to call on a nil *Service or when the worker was never started.
// Safe to call multiple times (idempotent).
func (svc *Service) Stop() {
	if svc == nil || svc.embedder == nil {
		return
	}

	svc.mu.Lock()
	if svc.stopped {
		svc.mu.Unlock()
		return
	}
	svc.stopped = true
	svc.mu.Unlock()

	close(svc.stopCh)
	svc.wg.Wait()
}

// Backfill embeds all observations that currently have embedding = NULL.
// Runs synchronously in the caller's goroutine — use from CLI commands.
// Returns (successCount, totalCount, error).
// Safe to call on a nil *Service (returns 0, 0, nil).
func (svc *Service) Backfill() (int, int, error) {
	if svc == nil || svc.embedder == nil {
		return 0, 0, nil
	}

	observations, err := svc.store.ListObservationsForEmbedding()
	if err != nil {
		return 0, 0, fmt.Errorf("embedding: list observations for backfill: %w", err)
	}

	total := len(observations)
	success := 0
	for _, obs := range observations {
		if err := svc.embedOne(obs.ID, obs.Title, obs.Content); err == nil {
			success++
		}
	}

	return success, total, nil
}

// worker runs in a background goroutine. It processes IDs from the queue,
// fetches title+content from the store, generates the embedding, and persists
// it. On Stop(), it drains remaining items before exiting.
func (svc *Service) worker() {
	defer svc.wg.Done()
	defer func() {
		// Clear any leaked pendingTexts on shutdown to avoid memory leaks.
		svc.mu.Lock()
		svc.pendingTexts = make(map[int64]string)
		svc.mu.Unlock()
	}()

	for {
		select {
		case id := <-svc.queue:
			svc.processID(id)
		case <-svc.stopCh:
			// Drain remaining items before shutdown.
			for {
				select {
				case id := <-svc.queue:
					svc.processID(id)
				default:
					return
				}
			}
		}
	}
}

// processID embeds a single observation by ID using the observation content
// already known from the enqueue-time title+content. Because we only have the
// ID here, we rely on the embedOne helper which the Backfill path also uses.
//
// For the async path we embed by ID — but the store only exposes
// ListObservationsForEmbedding (which filters by embedding IS NULL) and
// SetEmbedding. We avoid adding a GetObservation call in the hot path by
// encoding the text at enqueue time via EnqueueWithText instead.
//
// This internal helper is called from worker when items come via EnqueueWithText.
func (svc *Service) processID(id int64) {
	// Reached via EnqueueWithText — text is stored in pendingTexts.
	svc.mu.Lock()
	text, ok := svc.pendingTexts[id]
	if ok {
		delete(svc.pendingTexts, id)
	}
	svc.mu.Unlock()

	if !ok {
		// Fallback: item was queued without text (should not happen in normal flow).
		log.Printf("[embedding] no text found for observation #%d, skipping", id)
		return
	}

	vec, err := svc.embedder.Embed(text)
	if err != nil {
		log.Printf("[embedding] failed to embed observation #%d: %v", id, err)
		return
	}
	if err := svc.store.SetEmbedding(id, vec); err != nil {
		log.Printf("[embedding] failed to save embedding for #%d: %v", id, err)
	}
}

// EnqueueWithText schedules embedding generation for observation id with the
// given pre-built text (typically title + ". " + content).
//
// This is the preferred entry point from save handlers because it avoids a
// round-trip to the DB to look up the observation text.
// Safe to call on a nil *Service or when embedder is nil.
func (svc *Service) EnqueueWithText(id int64, text string) {
	if svc == nil || svc.embedder == nil {
		return
	}

	svc.mu.Lock()
	stopped := svc.stopped
	if !stopped {
		svc.pendingTexts[id] = text
	}
	svc.mu.Unlock()

	if stopped {
		return
	}

	select {
	case svc.queue <- id:
	default:
		// Queue full — remove the text we just stored to avoid leaking memory.
		svc.mu.Lock()
		delete(svc.pendingTexts, id)
		svc.mu.Unlock()
		log.Printf("[embedding] queue full, dropping embedding for observation #%d", id)
	}
}

// embedOne generates and stores the embedding for a single observation.
// Used by Backfill and as the shared implementation detail.
func (svc *Service) embedOne(id int64, title, content string) error {
	text := title + ". " + content
	vec, err := svc.embedder.Embed(text)
	if err != nil {
		return fmt.Errorf("embed observation #%d: %w", id, err)
	}
	if err := svc.store.SetEmbedding(id, vec); err != nil {
		return fmt.Errorf("save embedding for #%d: %w", id, err)
	}
	return nil
}
