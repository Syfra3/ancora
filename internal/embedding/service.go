// Package embedding processes local durable embedding jobs. Store mutations
// commit desired input and jobs together; queue messages only wake the worker.
// Fenced completion, persisted backoff and bounded reconciliation make lost
// wakeups and worker restarts safe. Custom legacy Store implementations retain
// the old in-memory adapter behavior but cannot certify real store vectors.
package embedding

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Syfra3/ancora/internal/embed"
	localstore "github.com/Syfra3/ancora/internal/store"
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
// Stop waits for in-flight work; unprocessed durable work remains recoverable.
//
// If the embedder is nil (model not installed), EnqueueWithText is a no-op and
// Backfill returns immediately — the service degrades silently, exactly
// like the old behaviour.
type Service struct {
	embedder      Embedder
	store         Store
	durable       *localstore.Store
	spec          localstore.EmbeddingSpec
	initErr       error
	now           func() time.Time
	identityMu    sync.Mutex // serializes refresh/inference and concurrent Backfill
	config        localstore.EmbeddingConfig
	haveConfig    bool
	nextProbe     time.Time
	nextReconcile time.Time
	retryAt       time.Time
	probeFailures int
	resolveSpec   func(Embedder) (localstore.EmbeddingSpec, error)
	configure     func(int64, localstore.EmbeddingSpec) (localstore.EmbeddingConfig, error)

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
	return newService(embedder, store, serviceHooks{})
}

// Instance-local seams keep recovery/fault tests offline without global hooks.
type serviceHooks struct {
	now       func() time.Time
	resolve   func(Embedder) (localstore.EmbeddingSpec, error)
	configure func(int64, localstore.EmbeddingSpec) (localstore.EmbeddingConfig, error)
}

func newService(embedder Embedder, store Store, hooks serviceHooks) *Service {
	svc := &Service{
		embedder:     embedder,
		store:        store,
		queue:        make(chan int64, defaultQueueSize),
		pendingTexts: make(map[int64]string),
		stopCh:       make(chan struct{}),
		now:          time.Now,
		resolveSpec:  ResolveSpec,
	}
	if hooks.now != nil {
		svc.now = hooks.now
	}
	if hooks.resolve != nil {
		svc.resolveSpec = hooks.resolve
	}
	// Keep the existing adapter/call sites source-compatible; real stores never
	// use the legacy ID-only completion path below.
	if adapter, ok := store.(*StoreAdapter); ok {
		svc.durable = adapter.s
		svc.configure = adapter.s.CompareAndSwapEmbedding
		if hooks.configure != nil {
			svc.configure = hooks.configure
		}
		if embedder != nil {
			_ = svc.refreshIdentity(false)
		}
	}
	return svc
}

// Start launches the background worker goroutine. Call once after creating
// the service. Safe to call on a nil *Service (no-op).
func (svc *Service) Start() {
	if svc == nil || svc.embedder == nil {
		return
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.stopped {
		return
	}
	svc.once.Do(func() {
		svc.wg.Add(1)
		go svc.worker()
	})
}

// Stop signals the worker to stop and waits for it to drain the queue.
// Safe to call on a nil *Service or when the worker was never started.
func (svc *Service) Stop() {
	if svc == nil || svc.embedder == nil {
		return
	}

	svc.mu.Lock()
	if !svc.stopped {
		svc.stopped = true
		close(svc.stopCh)
	}
	svc.mu.Unlock()

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
	if svc.durable != nil {
		return svc.backfillDurable()
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
	if svc.durable != nil {
		svc.durableWorker()
		return
	}

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
	if svc.durable != nil {
		// A lossy wake-up is safe: the job/input already committed in SQLite.
		// Never trust handler text (it may precede sanitization or a topic update).
		select {
		case svc.queue <- id:
		default:
		}
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

// ResolveSpec accepts an explicit identity from custom embedders. Native nomic
// identity hashes both model and inference executable bytes, not their paths.
// Unknown embedders remain usable for keyword fallback but cannot certify vectors.
func ResolveSpec(e Embedder) (localstore.EmbeddingSpec, error) {
	if p, ok := e.(interface {
		EmbeddingSpec() localstore.EmbeddingSpec
	}); ok {
		spec := p.EmbeddingSpec()
		if !spec.Valid() {
			return spec, errors.New("embedding: incomplete identity")
		}
		return spec, nil
	}
	if n, ok := e.(*embed.NomicEmbedder); ok && n != nil {
		h := sha256.New()
		for _, path := range []string{n.ModelPath, n.CLIPath} {
			f, err := os.Open(path)
			if err != nil {
				return localstore.EmbeddingSpec{}, err
			}
			part := sha256.New()
			_, err = io.Copy(part, f)
			closeErr := f.Close()
			if err != nil {
				return localstore.EmbeddingSpec{}, err
			}
			if closeErr != nil {
				return localstore.EmbeddingSpec{}, closeErr
			}
			h.Write(part.Sum(nil))
		}
		preprocessing := "title-dot-content/llama-json-p-v1"
		if filepath.Base(n.CLIPath) == "llama-cli" {
			preprocessing += "/embeddings-flag"
		}
		return localstore.EmbeddingSpec{Model: hex.EncodeToString(h.Sum(nil)), Preprocessing: preprocessing, Dimensions: embed.Dims}, nil
	}
	return localstore.EmbeddingSpec{}, errors.New("embedding: embedder has no model identity")
}

func (svc *Service) durableWorker() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 128; i++ {
			select {
			case <-svc.stopCh:
				return
			default:
			}
			worked, err := svc.processDurable()
			if err != nil {
				break
			}
			if !worked {
				break
			}
		}
		select {
		case <-svc.stopCh:
			return
		case <-svc.queue:
		case <-ticker.C:
		}
	}
}

func (svc *Service) processDurable() (bool, error) {
	worked, _, err := svc.processDurableResult()
	return worked, err
}

func (svc *Service) processDurableResult() (bool, bool, error) {
	svc.identityMu.Lock()
	defer svc.identityMu.Unlock()
	if err := svc.refreshIdentity(false); err != nil {
		return false, false, err
	}
	if !svc.now().Before(svc.nextReconcile) {
		if _, err := svc.durable.ReconcileEmbeddings(128); err != nil {
			return false, false, err
		}
		svc.nextReconcile = svc.now().Add(time.Second)
	}
	j, err := svc.durable.LeaseEmbedding(svc.spec, svc.now(), time.Minute)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	// Recheck exact bytes before inference, even within the periodic probe interval.
	if err := svc.refreshIdentity(true); err != nil {
		return true, false, svc.deferJob(*j, err)
	}
	if j.ConfigEpoch != svc.config.Epoch {
		_ = svc.deferJob(*j, nil)
		return true, false, nil
	}
	vec, embedErr := svc.embedder.Embed(j.Input)
	if embedErr == nil {
		after, identityErr := svc.resolveSpec(svc.embedder)
		if identityErr != nil {
			svc.deferIdentity(identityErr)
			return true, false, svc.deferJob(*j, identityErr)
		}
		if after != svc.spec {
			err := svc.acceptIdentity(after)
			return true, false, svc.deferJob(*j, err)
		}
		embedErr = svc.durable.CompleteEmbedding(*j, vec, svc.now())
		if embedErr == nil || errors.Is(embedErr, localstore.ErrEmbeddingFence) {
			return true, embedErr == nil, nil
		}
	}
	if errors.Is(embedErr, embed.ErrModelNotFound) || errors.Is(embedErr, embed.ErrEmbedderUnavailable) {
		svc.deferIdentity(embedErr)
		return true, false, svc.deferJob(*j, embedErr)
	}
	err = svc.durable.FailEmbedding(*j, embedErr, svc.now())
	if errors.Is(err, localstore.ErrEmbeddingFence) {
		err = nil
	}
	return true, false, err
}

func (svc *Service) backfillDurable() (int, int, error) {
	svc.identityMu.Lock()
	err := svc.refreshIdentity(false)
	svc.identityMu.Unlock()
	if err != nil {
		return 0, 0, err
	}
	for {
		n, err := svc.durable.ReconcileEmbeddings(128)
		if err != nil {
			return 0, 0, err
		}
		if n < 128 {
			break
		}
	}
	// Reset each exhausted job once, before processing, with bounded pages.
	total := 0
	for after := int64(0); ; {
		jobs, err := svc.durable.ListEmbeddingJobs(after, 128)
		if err != nil {
			return 0, total, err
		}
		for _, j := range jobs {
			after = j.ObservationID
			if j.State != "ready" {
				total++
			}
			if err := svc.durable.ResetEmbedding(j.ObservationID); err != nil {
				return 0, total, err
			}
		}
		if len(jobs) < 128 {
			break
		}
	}
	success := 0
	// Bound deliveries by the initial pending count. Failed work keeps backoff.
	for i := 0; i < total; i++ {
		worked, completed, err := svc.processDurableResult()
		if err != nil {
			return success, total, err
		}
		if !worked {
			break
		}
		if completed {
			success++
		}
	}
	return success, total, nil
}

// Healthy workers probe at most once per 30s while idle. Failed resolution/CAS
// retries use 5..60s backoff; wakeups cannot bypass it. Pending leases have the
// same durable retry deadline, so another unavailable worker cannot spin them.
func (svc *Service) deferIdentity(err error) error {
	svc.initErr = err
	delay := 5 * time.Second << min(svc.probeFailures, 4)
	if delay > time.Minute {
		delay = time.Minute
	}
	if svc.probeFailures < 4 {
		svc.probeFailures++
	}
	svc.retryAt = svc.now().Add(delay)
	return err
}

func (svc *Service) refreshIdentity(force bool) error {
	if svc.initErr != nil && svc.now().Before(svc.retryAt) {
		return svc.initErr
	}
	if svc.initErr == nil && !force && svc.now().Before(svc.nextProbe) {
		return nil
	}
	// Snapshot BEFORE resolving. Retain this epoch across failures; silently
	// rebasing on a CAS loss would let an old worker revert a newer selection.
	if !svc.haveConfig {
		c, err := svc.durable.GetEmbeddingConfig()
		if err != nil {
			return svc.deferIdentity(err)
		}
		svc.config = c
		svc.haveConfig = true
	}
	spec, err := svc.resolveSpec(svc.embedder)
	if err != nil {
		return svc.deferIdentity(err)
	}
	return svc.acceptIdentity(spec)
}

func (svc *Service) acceptIdentity(spec localstore.EmbeddingSpec) error {
	c, err := svc.configure(svc.config.Epoch, spec)
	if errors.Is(err, localstore.ErrEmbeddingFence) {
		// Joining an already-selected identical model is safe. A different model
		// cannot take selection authority merely by observing the newer epoch.
		current, readErr := svc.durable.GetEmbeddingConfig()
		if readErr != nil {
			return svc.deferIdentity(readErr)
		}
		if current.EmbeddingSpec == spec {
			c, err = current, nil
		}
	}
	if err != nil {
		return svc.deferIdentity(err)
	}
	if svc.config.Epoch != c.Epoch {
		svc.nextReconcile = time.Time{}
	}
	svc.config = c
	svc.spec = spec
	svc.initErr = nil
	svc.probeFailures = 0
	svc.retryAt = time.Time{}
	svc.nextProbe = svc.now().Add(30 * time.Second)
	return nil
}

func (svc *Service) deferJob(j localstore.EmbeddingJob, cause error) error {
	retryAt := svc.retryAt
	if retryAt.IsZero() {
		retryAt = svc.now()
	}
	err := svc.durable.ReleaseEmbedding(j, svc.now(), retryAt)
	// A model/source transition already fenced this job; reconciliation owns it.
	if err != nil && !errors.Is(err, localstore.ErrEmbeddingFence) {
		return err
	}
	return cause
}
