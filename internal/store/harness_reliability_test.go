package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var reliabilitySpec = EmbeddingSpec{Model: "fake-model-v1", Preprocessing: "title-dot-content-v1", Dimensions: 2}
var reliabilityNow = time.Unix(1000, 0)

func TestHarnessReliabilityInvalidDocumentsPersistFailure(t *testing.T) {
	for name, vec := range map[string][]float32{"nan": {float32(math.NaN()), 1}, "positive-inf": {float32(math.Inf(1)), 1}, "negative-inf": {float32(math.Inf(-1)), 1}, "zero": {0, 0}, "empty": {}, "wrong-dim": {1}} {
		t.Run(name, func(t *testing.T) {
			s := reliabilityStore(t)
			id := reliabilityAdd(t, s, "")
			now := reliabilityNow
			for i := 1; i <= 5; i++ {
				j := reliabilityLease(t, s, now)
				if err := s.CompleteEmbedding(j, vec, now); err == nil {
					t.Fatal("invalid document certified")
				}
				actual := reliabilityJob(t, s, id)
				want := "pending"
				if i == 5 {
					want = "exhausted"
				}
				if actual.Attempts != i || actual.State != want || actual.Token != "" || actual.NextAttempt <= now.UnixMilli() {
					t.Fatalf("invalid document stranded or uncertified failure: %+v", actual)
				}
				now = time.UnixMilli(actual.NextAttempt)
			}
			reliabilityNoVector(t, s)
		})
	}
}

func reliabilityStore(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	if err := s.ConfigureEmbedding(reliabilitySpec); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("harness", "fixture", ""); err != nil {
		t.Fatal(err)
	}
	return s
}
func reliabilityAdd(t *testing.T, s *Store, topic string) int64 {
	t.Helper()
	id, err := s.AddObservation(AddObservationParams{SessionID: "harness", Type: "decision", Title: "Exact Title", Content: "Alpha  Beta", Workspace: "fixture", Visibility: "personal", TopicKey: topic})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func reliabilityLease(t *testing.T, s *Store, now time.Time) EmbeddingJob {
	t.Helper()
	j, err := s.LeaseEmbedding(reliabilitySpec, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return *j
}
func reliabilityJob(t *testing.T, s *Store, id int64) EmbeddingJob {
	t.Helper()
	j, err := s.GetEmbeddingJob(id)
	if err != nil {
		t.Fatal(err)
	}
	return *j
}
func reliabilityNoVector(t *testing.T, s *Store) {
	t.Helper()
	r, err := s.SearchSemantic([]float32{1, 0}, 10)
	if err != nil || len(r) != 0 {
		t.Fatalf("obsolete vector eligible: %v %+v", err, r)
	}
}

// Scenario: Obsolete work cannot become current (update and topic entrypoints).
func TestHarnessReliabilityObsoleteWorkAndMetadata(t *testing.T) {
	for _, entry := range []string{"update", "topic"} {
		t.Run(entry, func(t *testing.T) {
			s := reliabilityStore(t)
			id := reliabilityAdd(t, s, "identity/test")
			first := reliabilityLease(t, s, reliabilityNow)
			if first.Input != "Exact Title. Alpha  Beta" || first.Revision != 1 {
				t.Fatalf("identity: %+v", first)
			}
			if err := s.CompleteEmbedding(first, []float32{1, 0}, reliabilityNow); err != nil {
				t.Fatal(err)
			}
			// Every metadata field may change without altering submitted text.
			metadata := "new"
			vis := "work"
			refs := `[{"type":"concept","target":"x"}]`
			_, err := s.UpdateObservation(id, UpdateObservationParams{Type: &metadata, Workspace: &metadata, Visibility: &vis, Organization: &metadata, TopicKey: &metadata, References: &refs})
			if err != nil {
				t.Fatal(err)
			}
			ready := reliabilityJob(t, s, id)
			if ready.Revision != first.Revision || ready.State != "ready" {
				t.Fatalf("metadata recomputed: %+v", ready)
			}
			var hashBefore string
			if err := s.db.QueryRow(`SELECT normalized_hash FROM observations WHERE id=?`, id).Scan(&hashBefore); err != nil {
				t.Fatal(err)
			}
			change := func(content string) {
				t.Helper()
				if entry == "update" {
					_, err = s.UpdateObservation(id, UpdateObservationParams{Content: &content})
				} else {
					var updatedID int64
					updatedID, err = s.AddObservation(AddObservationParams{SessionID: "harness", Type: metadata, Title: "Exact Title", Content: content, Workspace: metadata, Visibility: vis, Organization: metadata, TopicKey: metadata})
					if updatedID != id {
						t.Fatalf("topic changed id %d -> %d", id, updatedID)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			change("Alpha Beta") // same normalized hash, different exact input
			var hashAfter string
			if err := s.db.QueryRow(`SELECT normalized_hash FROM observations WHERE id=?`, id).Scan(&hashAfter); err != nil {
				t.Fatal(err)
			}
			if hashAfter != hashBefore {
				t.Fatal("fixture must retain dedupe hash")
			}
			reliabilityNoVector(t, s)
			second := reliabilityLease(t, s, reliabilityNow)
			if second.Revision != first.Revision+1 {
				t.Fatalf("no input revision: %+v", second)
			}
			change("Newest input")
			third := reliabilityLease(t, s, reliabilityNow)
			if err := s.CompleteEmbedding(third, []float32{0, 1}, reliabilityNow); err != nil {
				t.Fatal(err)
			}
			for _, stale := range []EmbeddingJob{first, second, third} {
				if err := s.CompleteEmbedding(stale, []float32{1, 0}, reliabilityNow); !errors.Is(err, ErrEmbeddingFence) {
					t.Fatalf("accepted stale/duplicate: %v", err)
				}
			}
			r, err := s.SearchSemantic([]float32{0, 1}, 10)
			if err != nil || len(r) != 1 || r[0].Content != "Newest input" || r[0].Rank != 1 {
				t.Fatalf("out-of-order overwrite: %+v %v", r, err)
			}
		})
	}
}

// Scenario: Work survives failure; fake times, temporary on-disk restart, no sleeps.
func TestHarnessReliabilityRestartLeaseBackoffExhaustion(t *testing.T) {
	s := reliabilityStore(t)
	id := reliabilityAdd(t, s, "")
	old := reliabilityLease(t, s, reliabilityNow)
	cfg := s.cfg
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	s, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.LeaseEmbedding(reliabilitySpec, reliabilityNow.Add(time.Second), time.Minute); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unexpired lease: %v", err)
	}
	now := reliabilityNow.Add(time.Minute)
	j := reliabilityLease(t, s, now)
	if j.Token == old.Token {
		t.Fatal("lease token reused")
	}
	if err = s.CompleteEmbedding(old, []float32{1, 0}, now); !errors.Is(err, ErrEmbeddingFence) {
		t.Fatalf("old lease accepted: %v", err)
	}
	for attempt := 1; attempt <= 5; attempt++ {
		if attempt > 1 {
			j = reliabilityLease(t, s, now)
		}
		if err = s.FailEmbedding(j, errors.New("fake inference failure"), now); err != nil {
			t.Fatal(err)
		}
		if err = s.FailEmbedding(j, errors.New("duplicate"), now); !errors.Is(err, ErrEmbeddingFence) {
			t.Fatalf("double failure: %v", err)
		}
		persisted := reliabilityJob(t, s, id)
		if persisted.Attempts != attempt || persisted.LastError != "fake inference failure" {
			t.Fatalf("attempt: %+v", persisted)
		}
		if attempt < 5 && persisted.NextAttempt != now.UnixMilli()+int64(1000<<(attempt-1)) {
			t.Fatalf("backoff: %+v", persisted)
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
		s, err = New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.LeaseEmbedding(reliabilitySpec, now, time.Minute); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("spinning: %v", err)
		}
		now = time.UnixMilli(persisted.NextAttempt)
	}
	defer s.Close()
	if _, err = s.ReconcileEmbeddings(5); err != nil {
		t.Fatal(err)
	}
	if _, err = s.LeaseEmbedding(reliabilitySpec, now.Add(24*time.Hour), time.Minute); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("exhausted auto retry: %v", err)
	}
	if j := reliabilityJob(t, s, id); j.State != "exhausted" || j.Attempts != 5 {
		t.Fatalf("exhaustion lost: %+v", j)
	}
	kw, err := s.Search("Alpha", SearchOptions{})
	if err != nil || len(kw) != 1 {
		t.Fatalf("keyword fallback %v %d", err, len(kw))
	}
	if err = s.ResetEmbedding(id); err != nil {
		t.Fatal(err)
	}
	j = reliabilityLease(t, s, now)
	if j.Attempts != 0 {
		t.Fatal("explicit reset failed")
	}
	if err = s.FailEmbedding(j, errors.New("again"), now); err != nil {
		t.Fatal(err)
	}
	text := "new identity"
	if _, err = s.UpdateObservation(id, UpdateObservationParams{Title: &text}); err != nil {
		t.Fatal(err)
	}
	j = reliabilityLease(t, s, now)
	if j.Attempts != 0 || j.Input != "new identity. Alpha  Beta" {
		t.Fatalf("identity did not supersede: %+v", j)
	}
}

func TestHarnessReliabilityDeleteAndLegacySetterSafety(t *testing.T) {
	for _, hard := range []bool{false, true} {
		t.Run(fmt.Sprint(hard), func(t *testing.T) {
			s := reliabilityStore(t)
			id := reliabilityAdd(t, s, "")
			j := reliabilityLease(t, s, reliabilityNow)
			if err := s.DeleteObservation(id, hard); err != nil {
				t.Fatal(err)
			}
			if err := s.CompleteEmbedding(j, []float32{1, 0}, reliabilityNow); !errors.Is(err, ErrEmbeddingFence) {
				t.Fatal(err)
			}
			if err := s.SetEmbedding(id, []float32{1, 0}); err != nil {
				t.Fatal(err)
			}
			reliabilityNoVector(t, s)
			if _, err := s.GetEmbeddingJob(id); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("deleted job retained: %v", err)
			}
		})
	}
	s := reliabilityStore(t)
	id := reliabilityAdd(t, s, "")
	j := reliabilityLease(t, s, reliabilityNow)
	if err := s.CompleteEmbedding(j, []float32{1, 0}, reliabilityNow); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbedding(id, []float32{0, 1}); err != nil {
		t.Fatal(err)
	}
	reliabilityNoVector(t, s)
	j = reliabilityLease(t, s, reliabilityNow)
	if err := s.CompleteEmbedding(j, []float32{1}, reliabilityNow); err == nil {
		t.Fatal("wrong dimension accepted")
	}
	if err := s.CompleteEmbedding(j, []float32{float32(math.NaN()), 0}, reliabilityNow); err == nil {
		t.Fatal("non-finite vector accepted")
	}
	if err := s.CompleteEmbedding(j, []float32{1, 0}, reliabilityNow.Add(time.Minute)); !errors.Is(err, ErrEmbeddingFence) {
		t.Fatalf("expired completion: %v", err)
	}
}

func TestHarnessReliabilityCompletionRollbackAndModelSupersession(t *testing.T) {
	s := reliabilityStore(t)
	id := reliabilityAdd(t, s, "")
	j := reliabilityLease(t, s, reliabilityNow)
	s.hooks.exec = func(db execer, q string, args ...any) (sql.Result, error) {
		if strings.Contains(q, "UPDATE observations SET embedding=") {
			return nil, errors.New("injected vector failure")
		}
		return db.Exec(q, args...)
	}
	if err := s.CompleteEmbedding(j, []float32{1, 0}, reliabilityNow); err == nil {
		t.Fatal("missing vector fault")
	}
	s.hooks = defaultStoreHooks()
	if actual := reliabilityJob(t, s, id); actual.State != "leased" || actual.Token != j.Token {
		t.Fatalf("partial ready certificate: %+v", actual)
	}
	newSpec := reliabilitySpec
	newSpec.Model = "new model"
	if err := s.ConfigureEmbedding(newSpec); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteEmbedding(j, []float32{1, 0}, reliabilityNow); !errors.Is(err, ErrEmbeddingFence) {
		t.Fatalf("old model accepted: %v", err)
	}
	if _, err := s.ReconcileEmbeddings(1); err != nil {
		t.Fatal(err)
	}
	current, err := s.LeaseEmbedding(newSpec, reliabilityNow, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision <= j.Revision {
		t.Fatal("model did not supersede revision")
	}
	if err := s.CompleteEmbedding(*current, []float32{1, 0}, reliabilityNow); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessReliabilityImportSyncAndLocalPersonalJobs(t *testing.T) {
	s := reliabilityStore(t)
	id := reliabilityAdd(t, s, "")
	old := reliabilityLease(t, s, reliabilityNow)
	obs, err := s.GetObservation(id)
	if err != nil {
		t.Fatal(err)
	}
	// Import's existing contract allocates a NEW ID, never overwrites by ID/sync_id.
	copy := *obs
	copy.Title = "imported exact title"
	if _, err := s.Import(&ExportData{Observations: []Observation{copy}}); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.ListEmbeddingJobs(0, 10)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("import jobs: %v %+v", err, jobs)
	}
	if jobs[1].ObservationID == id || jobs[1].Input != copy.Title+". "+copy.Content {
		t.Fatalf("import provenance: %+v", jobs[1])
	}
	if err := s.CompleteEmbedding(old, []float32{1, 0}, reliabilityNow); err != nil {
		t.Fatal(err)
	}
	importJob := reliabilityLease(t, s, reliabilityNow)
	if err := s.CompleteEmbedding(importJob, []float32{1, 0}, reliabilityNow); err != nil {
		t.Fatal(err)
	}
	// Use a unique sync identity for remote insertion then upsert/deletion.
	payload := syncObservationPayload{SyncID: "remote-harness", SessionID: "harness", Type: "decision", Title: "remote", Content: "first", Visibility: "personal"}
	apply := func(seq int64, op string) {
		t.Helper()
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.ApplyPulledMutation("cloud", SyncMutation{Seq: seq, Entity: SyncEntityObservation, EntityKey: payload.SyncID, Op: op, Payload: string(data)}); err != nil {
			t.Fatal(err)
		}
	}
	apply(1, SyncOpUpsert)
	remote := reliabilityLease(t, s, reliabilityNow)
	payload.Content = "second"
	apply(2, SyncOpUpsert)
	if err := s.CompleteEmbedding(remote, []float32{1, 0}, reliabilityNow); !errors.Is(err, ErrEmbeddingFence) {
		t.Fatal(err)
	}
	newJob := reliabilityLease(t, s, reliabilityNow)
	if newJob.Revision <= remote.Revision || newJob.Input != "remote. second" {
		t.Fatalf("sync invalidation: %+v", newJob)
	}
	// Metadata-only remote delivery must not supersede the lease.
	payload.Type = "bugfix"
	apply(3, SyncOpUpsert)
	if err := s.CompleteEmbedding(newJob, []float32{1, 0}, reliabilityNow); err != nil {
		t.Fatal(err)
	}
	payload.Content = "third"
	apply(4, SyncOpUpsert)
	late := reliabilityLease(t, s, reliabilityNow)
	apply(5, SyncOpDelete)
	if err := s.CompleteEmbedding(late, []float32{1, 0}, reliabilityNow); !errors.Is(err, ErrEmbeddingFence) {
		t.Fatal(err)
	}
	var outbound int
	if err := s.db.QueryRow(`SELECT count(*) FROM sync_mutations WHERE entity='observation'`).Scan(&outbound); err != nil {
		t.Fatal(err)
	}
	if outbound != 0 {
		t.Fatalf("personal/remote jobs leaked to sync: %d", outbound)
	}
}

func TestHarnessReliabilityMutationAndJobRollbackTogether(t *testing.T) {
	for _, entry := range []string{"create", "update", "topic", "import", "sync"} {
		t.Run(entry, func(t *testing.T) {
			s := reliabilityStore(t)
			id := reliabilityAdd(t, s, "rollback/topic")
			before := reliabilityJob(t, s, id)
			// SQL fault injection aborts the queue write inside the source transaction.
			if _, err := s.db.Exec(`CREATE TRIGGER harness_job_fault BEFORE INSERT ON embedding_jobs BEGIN SELECT RAISE(ABORT,'injected job failure'); END`); err != nil {
				t.Fatal(err)
			}
			var err error
			text := "changed"
			switch entry {
			case "create":
				_, err = s.AddObservation(AddObservationParams{SessionID: "harness", Title: text, Content: text, Type: "decision"})
			case "update":
				_, err = s.UpdateObservation(id, UpdateObservationParams{Content: &text})
			case "topic":
				_, err = s.AddObservation(AddObservationParams{SessionID: "harness", Title: text, Content: text, Type: "decision", Workspace: "fixture", Visibility: "personal", TopicKey: "rollback/topic"})
			case "import":
				_, err = s.Import(&ExportData{Observations: []Observation{{SessionID: "harness", Title: text, Content: text, Type: "decision", Visibility: "work"}}})
			case "sync":
				o, e := s.GetObservation(id)
				if e != nil {
					t.Fatal(e)
				}
				p := observationPayloadFromObservation(o)
				p.Content = text
				data, e := json.Marshal(p)
				if e != nil {
					t.Fatal(e)
				}
				err = s.ApplyPulledMutation("cloud", SyncMutation{Seq: 1, Entity: SyncEntityObservation, EntityKey: p.SyncID, Op: SyncOpUpsert, Payload: string(data)})
			}
			if err == nil || !strings.Contains(err.Error(), "injected job failure") {
				t.Fatalf("missing fault: %v", err)
			}
			var count int
			if err := s.db.QueryRow(`SELECT count(*) FROM observations`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("partial observation commit %d", count)
			}
			after := reliabilityJob(t, s, id)
			if after != before {
				t.Fatalf("partial job commit: %+v", after)
			}
			o, err := s.GetObservation(id)
			if err != nil || o.Content != "Alpha  Beta" {
				t.Fatalf("partial source update: %+v %v", o, err)
			}
			kw, err := s.Search("Alpha", SearchOptions{})
			if err != nil || len(kw) != 1 {
				t.Fatalf("FTS rollback: %v %d", err, len(kw))
			}
		})
	}
}

// Scenario: Additive migration is recoverable. Construct a pre-job schema only
// in a temp DB, inject every DDL/commit failure, close/reopen, then migrate twice.
func TestHarnessReliabilityLegacyMigrationRollbackRestartTwice(t *testing.T) {
	for failAt := 1; failAt <= 5; failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			s := reliabilityStore(t)
			id := reliabilityAdd(t, s, "")
			if err := s.SetEmbedding(id, []float32{1, 0}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`DROP TRIGGER embedding_observation_insert; DROP TRIGGER embedding_observation_update; DROP TABLE embedding_jobs; DROP TABLE embedding_config`); err != nil {
				t.Fatal(err)
			}
			before, err := s.GetObservation(id)
			if err != nil {
				t.Fatal(err)
			}
			steps := 0
			s.hooks.exec = func(db execer, q string, args ...any) (sql.Result, error) {
				steps++
				if steps == failAt {
					return nil, errors.New("injected migration failure")
				}
				return db.Exec(q, args...)
			}
			if failAt == 5 {
				s.hooks.commit = func(*sql.Tx) error { return errors.New("injected commit failure") }
			}
			if err := s.migrateEmbeddingJobs(); err == nil {
				t.Fatal("fault not reached")
			}
			s.hooks = defaultStoreHooks()
			var partial int
			if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name LIKE 'embedding_%'`).Scan(&partial); err != nil {
				t.Fatal(err)
			}
			if partial != 0 {
				t.Fatalf("partial migration accepted: %d", partial)
			}
			path := filepath.Join(s.cfg.DataDir, "ancora.db")
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", sqliteDSN(path))
			if err != nil {
				t.Fatal(err)
			}
			s = &Store{db: db, cfg: s.cfg, hooks: defaultStoreHooks()}
			defer s.Close()
			for i := 0; i < 2; i++ {
				if err := s.migrateEmbeddingJobs(); err != nil {
					t.Fatal(err)
				}
			}
			after, err := s.GetObservation(id)
			if err != nil || after.ID != before.ID || after.Title != before.Title || after.Content != before.Content || after.SyncID != before.SyncID {
				t.Fatalf("migration data loss: %v %+v", err, after)
			}
			reliabilityNoVector(t, s)
			kw, err := s.Search("Alpha", SearchOptions{})
			if err != nil || len(kw) != 1 {
				t.Fatalf("FTS migration: %v %d", err, len(kw))
			}
			if err := s.ConfigureEmbedding(reliabilitySpec); err != nil {
				t.Fatal(err)
			}
			if n, err := s.ReconcileEmbeddings(1); err != nil || n != 1 {
				t.Fatalf("legacy reconcile %d %v", n, err)
			}
			j := reliabilityLease(t, s, reliabilityNow)
			if err := s.CompleteEmbedding(j, []float32{1, 0}, reliabilityNow); err != nil {
				t.Fatal(err)
			}
			if err := s.migrateEmbeddingJobs(); err != nil {
				t.Fatal(err)
			}
			if reliabilityJob(t, s, id).State != "ready" {
				t.Fatal("second migration invalidated certified vector")
			}
		})
	}
}

func TestHarnessReliabilityBoundedReconciliationAndModelIdentity(t *testing.T) {
	s := reliabilityStore(t)
	for i := 0; i < 5; i++ {
		id, err := s.AddObservation(AddObservationParams{SessionID: "harness", Type: "decision", Title: fmt.Sprint(i), Content: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		j := reliabilityLease(t, s, reliabilityNow)
		if j.ObservationID != id {
			t.Fatal("wrong job")
		}
		if err := s.CompleteEmbedding(j, []float32{1, 0}, reliabilityNow); err != nil {
			t.Fatal(err)
		}
	}
	for _, spec := range []EmbeddingSpec{{Model: "changed", Preprocessing: reliabilitySpec.Preprocessing, Dimensions: 2}, {Model: "changed", Preprocessing: "v2", Dimensions: 2}, {Model: "changed", Preprocessing: "v2", Dimensions: 3}} {
		if err := s.ConfigureEmbedding(spec); err != nil {
			t.Fatal(err)
		}
		reliabilityNoVector(t, s)
		for _, want := range []int{2, 2, 1, 0} {
			n, err := s.ReconcileEmbeddings(2)
			if err != nil || n != want {
				t.Fatalf("batch bound: %d want %d %v", n, want, err)
			}
		}
		for i := 0; i < 5; i++ {
			j, err := s.LeaseEmbedding(spec, reliabilityNow, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			vec := make([]float32, spec.Dimensions)
			vec[0] = 1
			if err := s.CompleteEmbedding(*j, vec, reliabilityNow); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Simulate legacy/missing/corrupt vectors without touching live files.
	if _, err := s.db.Exec(`DELETE FROM embedding_jobs WHERE observation_id=1; UPDATE observations SET embedding=NULL WHERE id=2; UPDATE observations SET embedding=x'01' WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ReconcileEmbeddings(2); err != nil || n != 2 {
		t.Fatalf("missing batch %d %v", n, err)
	}
	if n, err := s.ReconcileEmbeddings(2); err != nil || n != 1 {
		t.Fatalf("mismatch batch %d %v", n, err)
	}
}

func TestHarnessReliabilityConcurrentLeaseFencing(t *testing.T) {
	s := reliabilityStore(t)
	reliabilityAdd(t, s, "")
	var wg sync.WaitGroup
	results := make(chan *EmbeddingJob, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, err := s.LeaseEmbedding(reliabilitySpec, reliabilityNow, time.Minute)
			if err != nil {
				errs <- err
			} else {
				results <- j
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	if len(results) != 1 {
		t.Fatalf("multiple deliveries %d", len(results))
	}
	for err := range errs {
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
	}
	for j := range results {
		if err := s.CompleteEmbedding(*j, []float32{1, 0}, reliabilityNow); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHarnessReliabilityExhaustedIdentitySupersession(t *testing.T) {
	for _, change := range []string{"input", "model"} {
		t.Run(change, func(t *testing.T) {
			s := reliabilityStore(t)
			id := reliabilityAdd(t, s, "")
			now := reliabilityNow
			for i := 0; i < 5; i++ {
				j := reliabilityLease(t, s, now)
				if err := s.FailEmbedding(j, errors.New("failed"), now); err != nil {
					t.Fatal(err)
				}
				now = time.UnixMilli(reliabilityJob(t, s, id).NextAttempt)
			}
			before := reliabilityJob(t, s, id)
			metadata := "metadata-only"
			if _, err := s.UpdateObservation(id, UpdateObservationParams{Type: &metadata}); err != nil {
				t.Fatal(err)
			}
			if err := s.SetEmbedding(id, []float32{1, 0}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ReconcileEmbeddings(1); err != nil {
				t.Fatal(err)
			}
			if j := reliabilityJob(t, s, id); j.State != "exhausted" || j.Attempts != 5 || j.Revision != before.Revision {
				t.Fatalf("implicit reset: %+v", j)
			}
			spec := reliabilitySpec
			if change == "input" {
				text := "new exact input"
				if _, err := s.UpdateObservation(id, UpdateObservationParams{Content: &text}); err != nil {
					t.Fatal(err)
				}
			} else {
				spec.Model = "superseding model"
				if err := s.ConfigureEmbedding(spec); err != nil {
					t.Fatal(err)
				}
				if _, err := s.ReconcileEmbeddings(1); err != nil {
					t.Fatal(err)
				}
			}
			j, err := s.LeaseEmbedding(spec, now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if j.Attempts != 0 || j.Revision <= before.Revision {
				t.Fatalf("new identity stayed exhausted: %+v", j)
			}
		})
	}
}
