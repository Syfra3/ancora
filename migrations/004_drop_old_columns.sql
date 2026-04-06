-- Migration 004: Drop Old Columns (project, scope)
-- This migration completes the transition from project/scope to workspace/visibility/organization
-- Run this ONLY after verifying all code uses the new column names

-- ─── Step 1: Verify data migration is complete ───────────────────────────────
-- Before running this migration, verify:
-- 1. All observations have workspace populated (or intentionally NULL for global)
-- 2. All observations have visibility populated (work/personal)
-- 3. No critical data exists only in project/scope columns

-- Quick verification queries (run these first):
-- SELECT COUNT(*) FROM observations WHERE workspace IS NULL AND project != '';
-- SELECT COUNT(*) FROM observations WHERE visibility = '' AND scope != '';

-- ─── Step 2: Drop old indexes that reference old columns ─────────────────────
DROP INDEX IF EXISTS idx_obs_project;
DROP INDEX IF EXISTS idx_obs_scope;
DROP INDEX IF EXISTS idx_obs_topic;  -- References old project/scope
DROP INDEX IF EXISTS idx_obs_dedupe; -- References old project/scope

-- ─── Step 3: Recreate FTS table with new column names ─────────────────────────
DROP TRIGGER IF EXISTS obs_fts_insert;
DROP TRIGGER IF EXISTS obs_fts_update;
DROP TRIGGER IF EXISTS obs_fts_delete;
DROP TABLE IF EXISTS observations_fts;

CREATE VIRTUAL TABLE observations_fts USING fts5(
    title,
    content,
    tool_name,
    type,
    workspace,
    topic_key,
    content='observations',
    content_rowid='id'
);

-- Repopulate FTS with new column
INSERT INTO observations_fts(rowid, title, content, tool_name, type, workspace, topic_key)
SELECT id, title, content, tool_name, type, workspace, topic_key
FROM observations
WHERE deleted_at IS NULL;

-- ─── Step 4: Recreate FTS triggers with new column names ──────────────────────
CREATE TRIGGER obs_fts_insert AFTER INSERT ON observations BEGIN
    INSERT INTO observations_fts(rowid, title, content, tool_name, type, workspace, topic_key)
    VALUES (new.id, new.title, new.content, new.tool_name, new.type, new.workspace, new.topic_key);
END;

CREATE TRIGGER obs_fts_delete AFTER DELETE ON observations BEGIN
    INSERT INTO observations_fts(observations_fts, rowid, title, content, tool_name, type, workspace, topic_key)
    VALUES ('delete', old.id, old.title, old.content, old.tool_name, old.type, old.workspace, old.topic_key);
END;

CREATE TRIGGER obs_fts_update AFTER UPDATE ON observations BEGIN
    INSERT INTO observations_fts(observations_fts, rowid, title, content, tool_name, type, workspace, topic_key)
    VALUES ('delete', old.id, old.title, old.content, old.tool_name, old.type, old.workspace, old.topic_key);
    INSERT INTO observations_fts(rowid, title, content, tool_name, type, workspace, topic_key)
    VALUES (new.id, new.title, new.content, new.tool_name, new.type, new.workspace, new.topic_key);
END;

-- ─── Step 5: Create a temporary backup table for old columns ──────────────────
-- This allows rollback if something goes wrong
CREATE TABLE IF NOT EXISTS observations_old_columns_backup AS
SELECT id, project, scope
FROM observations;

-- ─── Step 6: Drop old columns ──────────────────────────────────────────────────
-- SQLite doesn't support DROP COLUMN directly in older versions
-- We need to recreate the table without those columns

-- Create new table structure without project/scope
CREATE TABLE observations_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_id    TEXT,
    session_id TEXT    NOT NULL,
    type       TEXT    NOT NULL,
    title      TEXT    NOT NULL,
    content    TEXT    NOT NULL,
    tool_name  TEXT,
    topic_key  TEXT,
    normalized_hash TEXT,
    revision_count INTEGER NOT NULL DEFAULT 1,
    duplicate_count INTEGER NOT NULL DEFAULT 1,
    last_seen_at TEXT,
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at TEXT,
    embedding BLOB,
    workspace TEXT,
    visibility TEXT NOT NULL DEFAULT 'work',
    organization TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

-- Copy data from old table to new table (excluding project and scope)
INSERT INTO observations_new (
    id, sync_id, session_id, type, title, content, tool_name,
    topic_key, normalized_hash, revision_count, duplicate_count,
    last_seen_at, created_at, updated_at, deleted_at, embedding,
    workspace, visibility, organization
)
SELECT 
    id, sync_id, session_id, type, title, content, tool_name,
    topic_key, normalized_hash, revision_count, duplicate_count,
    last_seen_at, created_at, updated_at, deleted_at, embedding,
    workspace, visibility, organization
FROM observations;

-- Drop old table and rename new one
DROP TABLE observations;
ALTER TABLE observations_new RENAME TO observations;

-- ─── Step 7: Recreate all remaining indexes ───────────────────────────────────
CREATE INDEX idx_obs_session  ON observations(session_id);
CREATE INDEX idx_obs_type     ON observations(type);
CREATE INDEX idx_obs_created  ON observations(created_at DESC);
CREATE INDEX idx_obs_sync_id ON observations(sync_id);
CREATE INDEX idx_obs_deleted ON observations(deleted_at);
CREATE INDEX idx_obs_workspace ON observations(workspace);
CREATE INDEX idx_obs_visibility ON observations(visibility);
CREATE INDEX idx_obs_topic ON observations(topic_key, workspace, visibility, updated_at DESC);
CREATE INDEX idx_obs_dedupe ON observations(normalized_hash, workspace, visibility, type, title, created_at DESC);

-- ─── Step 8: Update user_prompts FTS (it also references project) ─────────────
-- The user_prompts table still has a project column that should be migrated similarly
-- For now, we'll leave it as-is since it's not part of the workspace/visibility migration
-- TODO: Consider migrating user_prompts.project to user_prompts.workspace in a future migration

-- ─── Migration Complete ───────────────────────────────────────────────────────
-- Verification queries:
-- SELECT COUNT(*) FROM observations;
-- SELECT COUNT(*) FROM observations_fts;
-- PRAGMA integrity_check;
-- PRAGMA foreign_key_check;
