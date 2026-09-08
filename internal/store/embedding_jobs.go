package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

// EmbeddingSpec identifies the model and the exact preprocessing contract.
// Empty identity means unavailable, never an implicit default model.
type EmbeddingSpec struct {
	Model         string
	Preprocessing string
	Dimensions    int
}

func (p EmbeddingSpec) Valid() bool {
	return p.Model != "" && p.Preprocessing != "" && p.Dimensions > 0
}

// EmbeddingJob is local derived work, deliberately absent from sync/export DTOs.
// Input is the exact submitted text, not the deduplication normalized_hash.
type EmbeddingJob struct {
	ObservationID int64
	Revision      int64
	Input         string
	EmbeddingSpec
	State       string
	Attempts    int
	NextAttempt int64
	LeaseUntil  int64
	Token       string
	LastError   string
	ConfigEpoch int64
}

var ErrEmbeddingFence = errors.New("embedding: obsolete identity or lease")

const embeddingJobColumns = `observation_id, revision, input, model, preprocessing, dimensions, state, attempts, next_attempt, lease_until, token, last_error, config_epoch`

func scanEmbeddingJob(row interface{ Scan(...any) error }) (*EmbeddingJob, error) {
	j := new(EmbeddingJob)
	err := row.Scan(&j.ObservationID, &j.Revision, &j.Input, &j.Model, &j.Preprocessing, &j.Dimensions, &j.State, &j.Attempts, &j.NextAttempt, &j.LeaseUntil, &j.Token, &j.LastError, &j.ConfigEpoch)
	return j, err
}

// migrateEmbeddingJobs is an additive, transactional and idempotent migration.
// Legacy vectors are retained but cannot pass the ready-job provenance join.
// Invalidation revokes readiness without recursively writing observations:
// recursive observation updates can run before the outer FTS trigger and corrupt
// its external-content index. Stale bytes are not evidence of a current vector.
// Triggers cover local, topic, import, remote sync and future SQL mutation paths
// in the same transaction as source/FTS/sync writes. No outbound job events exist.
func (s *Store) migrateEmbeddingJobs() error {
	return s.withTx(func(tx *sql.Tx) error {
		for index, statement := range []string{
			`CREATE TABLE IF NOT EXISTS embedding_config (
			 id INTEGER PRIMARY KEY CHECK(id=1), model TEXT NOT NULL, preprocessing TEXT NOT NULL, dimensions INTEGER NOT NULL, epoch INTEGER NOT NULL DEFAULT 0);
			 INSERT OR IGNORE INTO embedding_config(id,model,preprocessing,dimensions) VALUES(1,'','',0)`,
			`CREATE TABLE IF NOT EXISTS embedding_jobs (
			 observation_id INTEGER PRIMARY KEY REFERENCES observations(id) ON DELETE CASCADE,
			 revision INTEGER NOT NULL DEFAULT 1, input TEXT NOT NULL,
			 model TEXT NOT NULL, preprocessing TEXT NOT NULL, dimensions INTEGER NOT NULL,
			 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','leased','ready','exhausted')),
			 attempts INTEGER NOT NULL DEFAULT 0, next_attempt INTEGER NOT NULL DEFAULT 0,
			 lease_until INTEGER NOT NULL DEFAULT 0, token TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '', config_epoch INTEGER NOT NULL DEFAULT 0);
			 CREATE INDEX IF NOT EXISTS embedding_jobs_due ON embedding_jobs(state,next_attempt,lease_until)`,
			`DROP TRIGGER IF EXISTS embedding_observation_insert;
			 CREATE TRIGGER embedding_observation_insert AFTER INSERT ON observations
			 WHEN new.deleted_at IS NULL BEGIN
			 INSERT INTO embedding_jobs(observation_id,input,model,preprocessing,dimensions,config_epoch)
			 SELECT new.id,new.title || '. ' || new.content,model,preprocessing,dimensions,epoch FROM embedding_config WHERE id=1;
			 END`,
			`DROP TRIGGER IF EXISTS embedding_observation_update;
			 CREATE TRIGGER embedding_observation_update AFTER UPDATE OF title,content,deleted_at ON observations
			 WHEN (old.title || '. ' || old.content) IS NOT (new.title || '. ' || new.content)
			 OR old.deleted_at IS NOT new.deleted_at BEGIN
			 DELETE FROM embedding_jobs WHERE observation_id=new.id AND new.deleted_at IS NOT NULL;
			 INSERT INTO embedding_jobs(observation_id,input,model,preprocessing,dimensions,config_epoch)
			 SELECT new.id,new.title || '. ' || new.content,model,preprocessing,dimensions,epoch FROM embedding_config WHERE id=1 AND new.deleted_at IS NULL
			 ON CONFLICT(observation_id) DO UPDATE SET revision=embedding_jobs.revision+1,
			 input=excluded.input,model=excluded.model,preprocessing=excluded.preprocessing,dimensions=excluded.dimensions,config_epoch=excluded.config_epoch,
			 state='pending',attempts=0,next_attempt=0,lease_until=0,token='',last_error='';
			 END`,
		} {
			if _, err := s.execHook(tx, statement); err != nil {
				return err
			}
			if index == 1 {
				// Upgrade the earlier additive job schema within this transaction.
				for _, column := range []struct{ table, name string }{{"embedding_config", "epoch"}, {"embedding_jobs", "config_epoch"}} {
					var count int
					if err := tx.QueryRow("SELECT count(*) FROM pragma_table_info(?) WHERE name=?", column.table, column.name).Scan(&count); err != nil {
						return err
					}
					if count == 0 {
						if _, err := s.execHook(tx, "ALTER TABLE "+column.table+" ADD COLUMN "+column.name+" INTEGER NOT NULL DEFAULT 0"); err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	})
}

// ConfigureEmbedding selects the desired model atomically. Readers immediately
// exclude the previous spec, while reconciliation replaces old jobs in batches.
func (s *Store) ConfigureEmbedding(spec EmbeddingSpec) error {
	if !spec.Valid() {
		return errors.New("embedding: incomplete model identity")
	}
	_, err := s.execHook(s.db, `UPDATE embedding_config SET model=?,preprocessing=?,dimensions=?,epoch=epoch+1 WHERE id=1 AND (model!=? OR preprocessing!=? OR dimensions!=?)`, spec.Model, spec.Preprocessing, spec.Dimensions, spec.Model, spec.Preprocessing, spec.Dimensions)
	return err
}

// EmbeddingConfig is a monotonic selection fence, including ABA model changes.
type EmbeddingConfig struct {
	EmbeddingSpec
	Epoch int64
}

func (s *Store) GetEmbeddingConfig() (EmbeddingConfig, error) {
	var c EmbeddingConfig
	err := s.db.QueryRow(`SELECT model,preprocessing,dimensions,epoch FROM embedding_config WHERE id=1`).Scan(&c.Model, &c.Preprocessing, &c.Dimensions, &c.Epoch)
	return c, err
}

// CompareAndSwapEmbedding never rebases a stale worker onto a newer selection.
// The administrative ConfigureEmbedding API remains explicit/unconditional.
func (s *Store) CompareAndSwapEmbedding(expected int64, spec EmbeddingSpec) (EmbeddingConfig, error) {
	if !spec.Valid() {
		return EmbeddingConfig{}, errors.New("embedding: incomplete model identity")
	}
	var c EmbeddingConfig
	err := s.db.QueryRow(`UPDATE embedding_config SET model=?,preprocessing=?,dimensions=?,
	 epoch=epoch+CASE WHEN model!=? OR preprocessing!=? OR dimensions!=? THEN 1 ELSE 0 END
	 WHERE id=1 AND epoch=? RETURNING model,preprocessing,dimensions,epoch`, spec.Model, spec.Preprocessing, spec.Dimensions, spec.Model, spec.Preprocessing, spec.Dimensions, expected).Scan(&c.Model, &c.Preprocessing, &c.Dimensions, &c.Epoch)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrEmbeddingFence
	}
	return c, err
}

// ReconcileEmbeddings repairs at most limit missing, legacy, or mismatched jobs.
// Exhausted identities are deliberately not reset. A missing ready vector is
// repaired too; arbitrary ID-only writes explicitly revoke readiness.
func (s *Store) ReconcileEmbeddings(limit int) (int, error) {
	if limit <= 0 || limit > 256 {
		limit = 256
	}
	n := 0
	err := s.withTx(func(tx *sql.Tx) error {
		// Acquire the write lock before reading to avoid deferred-TX upgrades.
		if _, err := s.execHook(tx, `UPDATE embedding_config SET id=id WHERE id=1`); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT o.id,o.title || '. ' || o.content,c.model,c.preprocessing,c.dimensions,c.epoch
		 FROM observations o CROSS JOIN embedding_config c LEFT JOIN embedding_jobs j ON j.observation_id=o.id
		 WHERE o.deleted_at IS NULL AND (j.observation_id IS NULL OR j.input IS NOT (o.title || '. ' || o.content)
		 OR j.model!=c.model OR j.preprocessing!=c.preprocessing OR j.dimensions!=c.dimensions OR j.config_epoch!=c.epoch
		 OR (j.state='ready' AND (o.embedding IS NULL OR length(o.embedding)!=4*c.dimensions))) ORDER BY o.id LIMIT ?`, limit)
		if err != nil {
			return err
		}
		var jobs []EmbeddingJob
		for rows.Next() {
			var j EmbeddingJob
			if err := rows.Scan(&j.ObservationID, &j.Input, &j.Model, &j.Preprocessing, &j.Dimensions, &j.ConfigEpoch); err != nil {
				rows.Close()
				return err
			}
			jobs = append(jobs, j)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, j := range jobs {
			if _, err := s.execHook(tx, `UPDATE observations SET embedding=NULL WHERE id=?`, j.ObservationID); err != nil {
				return err
			}
			if _, err := s.execHook(tx, `INSERT INTO embedding_jobs(observation_id,input,model,preprocessing,dimensions,config_epoch) VALUES(?,?,?,?,?,?)
			 ON CONFLICT(observation_id) DO UPDATE SET revision=embedding_jobs.revision+1,input=excluded.input,
			 model=excluded.model,preprocessing=excluded.preprocessing,dimensions=excluded.dimensions,config_epoch=excluded.config_epoch,state='pending',attempts=0,next_attempt=0,lease_until=0,token='',last_error=''`,
				j.ObservationID, j.Input, j.Model, j.Preprocessing, j.Dimensions, j.ConfigEpoch); err != nil {
				return err
			}
		}
		n = len(jobs)
		return nil
	})
	return n, err
}

// LeaseEmbedding uses an atomic UPDATE ... RETURNING; each delivery receives a
// fresh unpredictable fencing token, including recovery after an expired lease.
func (s *Store) LeaseEmbedding(spec EmbeddingSpec, now time.Time, duration time.Duration) (*EmbeddingJob, error) {
	if !spec.Valid() || duration <= 0 {
		return nil, errors.New("embedding: invalid lease")
	}
	return scanEmbeddingJob(s.db.QueryRow(`UPDATE embedding_jobs SET state='leased',token=?,lease_until=?
	 WHERE observation_id=(SELECT j.observation_id FROM embedding_jobs j JOIN observations o ON o.id=j.observation_id
	 JOIN embedding_config c ON c.id=1 WHERE o.deleted_at IS NULL AND j.input=(o.title || '. ' || o.content)
	 AND j.model=? AND j.preprocessing=? AND j.dimensions=? AND j.model=c.model AND j.preprocessing=c.preprocessing AND j.dimensions=c.dimensions AND j.config_epoch=c.epoch
	 AND ((j.state='pending' AND j.next_attempt<=?) OR (j.state='leased' AND j.lease_until<=?))
	 ORDER BY j.next_attempt,j.observation_id LIMIT 1) RETURNING `+embeddingJobColumns,
		newSyncID("lease"), now.Add(duration).UnixMilli(), spec.Model, spec.Preprocessing, spec.Dimensions, now.UnixMilli(), now.UnixMilli()))
}

const embeddingFenceSQL = `observation_id=? AND revision=? AND input=? AND model=? AND preprocessing=? AND dimensions=?
 AND state='leased' AND token=? AND lease_until>? AND config_epoch=?
 AND EXISTS(SELECT 1 FROM observations o JOIN embedding_config c ON c.id=1 WHERE o.id=observation_id AND o.deleted_at IS NULL
 AND (o.title || '. ' || o.content)=input AND c.model=embedding_jobs.model AND c.preprocessing=embedding_jobs.preprocessing AND c.dimensions=embedding_jobs.dimensions AND c.epoch=embedding_jobs.config_epoch)`

func embeddingFenceArgs(j EmbeddingJob, now time.Time) []any {
	return []any{j.ObservationID, j.Revision, j.Input, j.Model, j.Preprocessing, j.Dimensions, j.Token, now.UnixMilli(), j.ConfigEpoch}
}

// ReleaseEmbedding defers unavailable inference without consuming an attempt.
func (s *Store) ReleaseEmbedding(j EmbeddingJob, now, retryAt time.Time) error {
	res, err := s.execHook(s.db, `UPDATE embedding_jobs SET state='pending',token='',lease_until=0,next_attempt=max(next_attempt,?) WHERE `+embeddingFenceSQL, append([]any{retryAt.UnixMilli()}, embeddingFenceArgs(j, now)...)...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrEmbeddingFence
	}
	return nil
}

func (s *Store) CompleteEmbedding(j EmbeddingJob, vec []float32, now time.Time) error {
	if err := ValidateEmbeddingVector(vec, j.Dimensions); err != nil {
		// Validation failures are deliveries too. Persist failure atomically under
		// the same fence; callers cannot accidentally strand an invalid lease.
		if failureErr := s.FailEmbedding(j, err, now); failureErr != nil {
			return failureErr
		}
		return err
	}
	return s.withTx(func(tx *sql.Tx) error {
		res, err := s.execHook(tx, `UPDATE embedding_jobs SET state='ready',token='',lease_until=0,last_error='' WHERE `+embeddingFenceSQL, embeddingFenceArgs(j, now)...)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrEmbeddingFence
		}
		_, err = s.execHook(tx, `UPDATE observations SET embedding=? WHERE id=? AND deleted_at IS NULL`, serializeVec(vec), j.ObservationID)
		return err
	})
}

// ValidateEmbeddingVector is shared by query ranking and document certification.
// Float64 accumulation avoids float32 overflow/underflow for finite components.
func ValidateEmbeddingVector(vec []float32, dimensions int) error {
	if dimensions <= 0 || len(vec) != dimensions {
		return errors.New("embedding: vector dimension mismatch")
	}
	var norm float64
	for _, v := range vec {
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return errors.New("embedding: non-finite vector")
		}
		norm += f * f
	}
	if norm == 0 || math.IsInf(norm, 0) {
		return errors.New("embedding: invalid vector norm")
	}
	return nil
}

// FailEmbedding persists bounded exponential backoff (1,2,4,8 seconds) and
// retains the fifth failure for inspection. No automatic path resets exhaustion.
func (s *Store) FailEmbedding(j EmbeddingJob, cause error, now time.Time) error {
	message := "embedding failed"
	if cause != nil {
		message = cause.Error()
	}
	if len(message) > 1024 {
		message = message[:1024]
	}
	res, err := s.execHook(s.db, `UPDATE embedding_jobs SET attempts=attempts+1,
	 state=CASE WHEN attempts+1>=5 THEN 'exhausted' ELSE 'pending' END,
	 next_attempt=? + (1000 << min(attempts,4)),last_error=?,token='',lease_until=0 WHERE `+embeddingFenceSQL,
		append([]any{now.UnixMilli(), message}, embeddingFenceArgs(j, now)...)...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrEmbeddingFence
	}
	return nil
}

// ResetEmbedding is an explicit user/backfill retry, never reconciliation.
func (s *Store) ResetEmbedding(id int64) error {
	_, err := s.execHook(s.db, `UPDATE embedding_jobs SET state='pending',attempts=0,next_attempt=0,last_error='',token='',lease_until=0 WHERE observation_id=? AND state='exhausted'`, id)
	return err
}

func (s *Store) GetEmbeddingJob(id int64) (*EmbeddingJob, error) {
	return scanEmbeddingJob(s.db.QueryRow(`SELECT `+embeddingJobColumns+` FROM embedding_jobs WHERE observation_id=?`, id))
}

// ListEmbeddingJobs is a bounded inspection API; callers paginate by observation ID.
func (s *Store) ListEmbeddingJobs(after int64, limit int) ([]EmbeddingJob, error) {
	if limit <= 0 || limit > 256 {
		limit = 256
	}
	rows, err := s.db.Query(`SELECT `+embeddingJobColumns+` FROM embedding_jobs WHERE observation_id>? ORDER BY observation_id LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []EmbeddingJob
	for rows.Next() {
		j, err := scanEmbeddingJob(rows)
		if err != nil {
			return nil, fmt.Errorf("embedding jobs: %w", err)
		}
		jobs = append(jobs, *j)
	}
	return jobs, rows.Err()
}
