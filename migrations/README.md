# Ancora Database Migrations

This directory contains SQL migration scripts for the Ancora database schema.

## Migration History

### 004_drop_old_columns.sql (PR5 - Schema Finalization)

**Status**: Ready for production (after code cleanup)

**Purpose**: Complete the workspace/visibility/organization migration by dropping old `project` and `scope` columns.

**Prerequisites**:
1. All code must use new column names (workspace, visibility, organization)
2. All observations must have workspace/visibility populated
3. Backup of production database created

**Verification before running**:
```sql
-- Check that all observations have workspace populated
SELECT COUNT(*) FROM observations WHERE workspace IS NULL AND project != '';

-- Check that all observations have visibility populated  
SELECT COUNT(*) FROM observations WHERE visibility = '';

-- Count total observations for verification
SELECT COUNT(*) FROM observations;
```

**Rollback plan**:
A backup table `observations_old_columns_backup` is created during migration containing the old project/scope values. If needed, you can manually restore by recreating columns and copying from backup.

**What it does**:
1. Drops indexes referencing old columns (idx_obs_project, idx_obs_scope, idx_obs_topic, idx_obs_dedupe)
2. Recreates FTS table and triggers with new column names (workspace instead of project)
3. Recreates indexes with new column names
4. Creates backup table with old column values
5. Recreates observations table without project/scope columns
6. Copies all data to new table structure
7. Recreates all indexes

**Post-migration verification**:
```sql
-- Verify row count matches
SELECT COUNT(*) FROM observations;
SELECT COUNT(*) FROM observations_fts;

-- Verify FTS search works
SELECT * FROM observations_fts WHERE observations_fts MATCH 'test' LIMIT 5;

-- Verify integrity
PRAGMA integrity_check;
PRAGMA foreign_key_check;

-- Verify old columns are gone
PRAGMA table_info(observations);
```

## Running Migrations

**IMPORTANT**: Always backup before running migrations!

```bash
# Backup database
cp ~/.ancora/ancora.db ~/.ancora/ancora.db.backup-$(date +%Y%m%d-%H%M%S)

# Run migration
sqlite3 ~/.ancora/ancora.db < migrations/004_drop_old_columns.sql

# Verify
sqlite3 ~/.ancora/ancora.db "PRAGMA integrity_check; PRAGMA foreign_key_check;"
```

## Schema Evolution

### Phase 1 (PR1): Add New Columns
- Added `workspace`, `visibility`, `organization` columns
- Kept `project` and `scope` for backward compatibility
- Backfilled new columns from old columns

### Phase 2 (PR2): Dual-Write Period  
- Code writes to both old and new columns
- FTS uses old columns for compatibility
- Migration functions update both sets

### Phase 3 (PR3): Code Transition
- Update all code to use new columns
- Remove old column references from structs
- Keep database columns for safety

### Phase 4 (PR4): Verification
- Run full test suite with new columns
- Verify all features work
- Check FTS search works

### Phase 5 (PR5): Cleanup - **THIS MIGRATION**
- Drop old columns from database
- Update FTS to use new columns
- Remove backward compatibility code
- Final production verification
