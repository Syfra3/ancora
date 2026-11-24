package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// All connections belong to one temp WAL database. No live store/socket access.
func TestHarnessReliabilityPooledMutationCompletionReconciliation(t *testing.T) {
	s := reliabilityStore(t)
	s.db.SetMaxOpenConns(5)
	id := reliabilityAdd(t, s, "")
	j := reliabilityLease(t, s, reliabilityNow)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE observations SET content='concurrent replacement' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	makePeer := func(fragment string) (*Store, chan struct{}) {
		entered := make(chan struct{})
		p := &Store{db: s.db, cfg: s.cfg, hooks: defaultStoreHooks()}
		p.hooks.exec = func(db execer, q string, args ...any) (sql.Result, error) {
			if strings.Contains(q, fragment) {
				close(entered)
			}
			return db.Exec(q, args...)
		}
		return p, entered
	}
	completer, completionEntered := makePeer("UPDATE embedding_jobs SET state='ready'")
	reconciler, reconciliationEntered := makePeer("UPDATE embedding_config SET id=id")
	completionDone := make(chan error, 1)
	reconciliationDone := make(chan error, 1)
	go func() { completionDone <- completer.CompleteEmbedding(j, []float32{1, 0}, reliabilityNow) }()
	go func() { _, err := reconciler.ReconcileEmbeddings(2); reconciliationDone <- err }()
	for _, entered := range []chan struct{}{completionEntered, reconciliationEntered} {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("pooled writer did not reach controlled barrier")
		}
	}
	// A fourth pooled connection sees the last committed source while writer holds lock.
	old, err := s.GetObservation(id)
	if err != nil || old.Content != "Alpha  Beta" {
		t.Fatalf("uncommitted source visible: %+v %v", old, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-completionDone; !errors.Is(err, ErrEmbeddingFence) {
		t.Fatalf("completion crossed committed mutation: %v", err)
	}
	if err := <-reconciliationDone; err != nil {
		t.Fatalf("reconciliation write upgrade contention: %v", err)
	}
	after := reliabilityJob(t, s, id)
	if after.Input != "Exact Title. concurrent replacement" || after.State != "pending" || after.Revision <= j.Revision {
		t.Fatalf("bad concurrent job: %+v", after)
	}
	reliabilityNoVector(t, s)
}

// Existing read-modify-write source APIs may return SQLITE_BUSY_SNAPSHOT when
// a separate writer commits after their read. Verify rollback plus caller retry,
// rather than pretending this is a committed mutation or certifying stale data.
func TestHarnessReliabilityPooledSnapshotUpgradeRollbackRetry(t *testing.T) {
	s := reliabilityStore(t)
	s.db.SetMaxOpenConns(4)
	id := reliabilityAdd(t, s, "")
	j := reliabilityLease(t, s, reliabilityNow)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	s.hooks.exec = func(db execer, q string, args ...any) (sql.Result, error) {
		if strings.Contains(q, "revision_count = revision_count + 1") {
			close(entered)
			<-release
		}
		return db.Exec(q, args...)
	}
	go func() {
		text := "after retry"
		_, err := s.UpdateObservation(id, UpdateObservationParams{Content: &text})
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("mutation failed before the controlled barrier: %v", err)
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("mutation did not reach controlled barrier")
	}
	peer := &Store{db: s.db, cfg: s.cfg, hooks: defaultStoreHooks()}
	completionErr := peer.CompleteEmbedding(j, []float32{1, 0}, reliabilityNow)
	close(release)
	updateErr := <-done
	s.hooks = defaultStoreHooks()
	if completionErr != nil {
		t.Fatal(completionErr)
	}
	if updateErr == nil || !strings.Contains(strings.ToLower(updateErr.Error()), "locked") {
		t.Fatalf("expected controlled SQLite upgrade contention: %v", updateErr)
	}
	old, err := s.GetObservation(id)
	if err != nil || old.Content != "Alpha  Beta" {
		t.Fatalf("failed transaction changed source: %+v %v", old, err)
	}
	if reliabilityJob(t, s, id).State != "ready" {
		t.Fatal("failed source transaction changed certificate")
	}
	text := "after retry"
	if _, err := s.UpdateObservation(id, UpdateObservationParams{Content: &text}); err != nil {
		t.Fatalf("caller retry: %v", err)
	}
	if err := s.CompleteEmbedding(j, []float32{1, 0}, reliabilityNow); !errors.Is(err, ErrEmbeddingFence) {
		t.Fatalf("old completion after retry: %v", err)
	}
	reliabilityNoVector(t, s)
}

func TestHarnessReliabilityConfigEpochABAAndUpgradeRollback(t *testing.T) {
	s := reliabilityStore(t)
	id := reliabilityAdd(t, s, "")
	held := reliabilityLease(t, s, reliabilityNow)
	base, err := s.GetEmbeddingConfig()
	if err != nil {
		t.Fatal(err)
	}
	other := reliabilitySpec
	other.Model = "other"
	if err := s.ConfigureEmbedding(other); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureEmbedding(reliabilitySpec); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteEmbedding(held, []float32{1, 0}, reliabilityNow); !errors.Is(err, ErrEmbeddingFence) {
		t.Fatalf("ABA revived held lease: %v", err)
	}
	if _, err := s.CompareAndSwapEmbedding(base.Epoch, other); !errors.Is(err, ErrEmbeddingFence) {
		t.Fatalf("ABA accepted stale selection: %v", err)
	}
	// Simulate the previous uncommitted job-schema revision in a temporary DB.
	if _, err := s.db.Exec(`DROP TRIGGER embedding_observation_insert; DROP TRIGGER embedding_observation_update; ALTER TABLE embedding_jobs DROP COLUMN config_epoch; ALTER TABLE embedding_config DROP COLUMN epoch`); err != nil {
		t.Fatal(err)
	}
	s.hooks.exec = func(db execer, q string, args ...any) (sql.Result, error) {
		if strings.Contains(q, "ALTER TABLE embedding_jobs ADD COLUMN") {
			return nil, errors.New("injected epoch upgrade failure")
		}
		return db.Exec(q, args...)
	}
	if err := s.migrateEmbeddingJobs(); err == nil {
		t.Fatal("epoch fault not reached")
	}
	s.hooks = defaultStoreHooks()
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('embedding_config') WHERE name='epoch'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial epoch upgrade: %d %v", count, err)
	}
	for i := 0; i < 2; i++ {
		if err := s.migrateEmbeddingJobs(); err != nil {
			t.Fatal(err)
		}
	}
	obs, err := s.GetObservation(id)
	if err != nil || obs.Content != "Alpha  Beta" {
		t.Fatalf("epoch upgrade lost observation: %+v %v", obs, err)
	}
}
