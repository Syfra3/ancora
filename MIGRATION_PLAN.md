# Schema Refactor: workspace, visibility, organization

## Goal

Refactor Ancora's data model to make field names clearer and support Syfra Cloud organization-based sync.

## Schema Changes

### Field Renames
- `project` → `workspace` (repo/folder name)
- `scope` → `visibility` (data classification: work/personal)

### New Field
- `organization` (org name for Syfra Cloud: glim, syfra, google, etc.)

### Semantics

**workspace** (nullable string):
- The repo/folder/project name
- Examples: `glim-api`, `syfra`, `health`, `life`
- Replaces current `project` field

**visibility** (required string):
- `work` = Professional knowledge (code, bugs, architecture, research)
  - Can sync to Syfra Cloud (if org enrolled)
  - Shareable with team
- `personal` = Private life data (health, finances, goals)
  - NEVER syncs automatically
  - Requires explicit permission
  - Local device only

**organization** (nullable string):
- Only meaningful when `visibility=work`
- NULL when `visibility=personal`
- Examples: `glim`, `syfra`, `google`, `personal-projects`
- Used for Syfra Cloud:
  - Subscription routing
  - Team workspace isolation
  - Billing/usage tracking

## Examples

| Scenario | workspace | visibility | organization |
|----------|-----------|------------|--------------|
| Fix glim bug | `glim-api` | `work` | `glim` |
| Fix syfra bug | `syfra` | `work` | `syfra` |
| My 2026 goals | `life` | `personal` | NULL |
| Body measurements | `health` | `personal` | NULL |
| Learn Rust notes | `learning` | `work` | `personal-projects` |

## Migration Strategy

### 1. Data Migration

```sql
-- Add new columns
ALTER TABLE observations ADD COLUMN workspace TEXT;
ALTER TABLE observations ADD COLUMN visibility TEXT NOT NULL DEFAULT 'work';
ALTER TABLE observations ADD COLUMN organization TEXT;

-- Copy data
UPDATE observations SET workspace = project;
UPDATE observations SET visibility = CASE 
    WHEN scope = 'personal' THEN 'personal'
    ELSE 'work'
END;

-- Infer organization for existing data
UPDATE observations SET organization = CASE
    WHEN visibility = 'personal' THEN NULL
    WHEN workspace LIKE 'glim-%' THEN 'glim'
    WHEN workspace IN ('syfra', 'ancora', 'opencode') THEN 'syfra'
    ELSE 'personal-projects'
END;

-- Drop old columns (after migration complete)
-- ALTER TABLE observations DROP COLUMN project;
-- ALTER TABLE observations DROP COLUMN scope;
```

### 2. Code Changes

#### Files to Update

**Core Types (internal/store/store.go)**:
- [ ] `Observation` struct
- [ ] `TimelineEntry` struct
- [ ] `SearchOptions` struct
- [ ] `AddObservationParams` struct
- [ ] `UpdateObservationParams` struct
- [ ] `syncObservationPayload` struct

**SQL Queries (internal/store/store.go)**:
- [ ] All SELECT queries (~30 occurrences)
- [ ] All INSERT queries (~10 occurrences)
- [ ] All UPDATE queries (~15 occurrences)
- [ ] All WHERE clauses with project/scope (~40 occurrences)
- [ ] All indexes (~5 indexes)
- [ ] FTS triggers (~6 triggers)

**MCP Server (internal/mcp/mcp.go)**:
- [ ] Tool parameter definitions (~8 tools)
- [ ] Tool handlers (~8 handlers)
- [ ] Response formatting (~10 locations)

**CLI (cmd/ancora/main.go)**:
- [ ] Command flag definitions (~5 commands)
- [ ] Flag parsing (~10 locations)
- [ ] Output formatting (~8 locations)

**TUI (internal/tui/)**:
- [ ] Model fields (model.go)
- [ ] View rendering (view.go)
- [ ] Update handlers (update.go)
- [ ] Filter logic

**Tests**:
- [ ] store_test.go (~30 test functions)
- [ ] mcp_test.go (~10 test functions)
- [ ] main_test.go (~5 test functions)
- [ ] TUI tests

**Documentation**:
- [ ] Plugin protocol (plugin/claude-code/skills/memory/SKILL.md)
- [ ] README.md
- [ ] API documentation

### 3. Auto-Detection Logic

Add function to infer visibility from context:

```go
func InferVisibility(title, content, workspace string) string {
    personalTriggers := []string{
        "my goals", "my health", "my weight", "my finances",
        "my salary", "my budget", "body measurement",
    }
    
    combined := strings.ToLower(title + " " + content)
    for _, trigger := range personalTriggers {
        if strings.Contains(combined, trigger) {
            return "personal"
        }
    }
    
    return "work" // default
}
```

### 4. Breaking Changes

This refactor breaks:
1. **MCP API**: Tool parameters change (scope→visibility, add organization)
2. **CLI**: Flags change (--scope→--visibility, add --org)
3. **HTTP API**: Query params change
4. **JSON exports**: Field names change
5. **Sync protocol**: Payload structure changes

### 5. Rollback Plan

If migration fails:
1. Restore from backup (--backup flag during migration)
2. Old columns (`project`, `scope`) remain until migration confirmed successful
3. Can switch back by reverting code changes

## Implementation Order

1. ✅ Design schema and semantics
2. ✅ Create feature branch
3. ⏳ Write migration SQL
4. ⏳ Update Go types
5. ⏳ Update all SQL queries
6. ⏳ Update MCP handlers
7. ⏳ Update CLI
8. ⏳ Update TUI
9. ⏳ Update tests
10. ⏳ Update documentation
11. ⏳ Test migration on copy of real database
12. ⏳ Merge and release

## Testing Plan

1. Unit tests for all changed functions
2. Integration test for migration script
3. E2E test for MCP tools
4. Manual test of TUI
5. Test on copy of production database

## Estimated Effort

- Migration script: 2 hours
- Type updates: 1 hour
- SQL query updates: 3 hours
- MCP/CLI updates: 2 hours
- TUI updates: 1 hour
- Test updates: 3 hours
- Documentation: 1 hour

**Total: ~13 hours**
