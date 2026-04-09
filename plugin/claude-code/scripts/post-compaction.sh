#!/bin/bash
# Ancora — Post-compaction hook for Claude Code
#
# When compaction happens, inject Memory Protocol + context and instruct
# the agent to persist the compacted summary via ancora_summarize.

ANCORA_PORT="${ANCORA_PORT:-7437}"
ANCORA_URL="http://127.0.0.1:${ANCORA_PORT}"

# Load shared helpers
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/_helpers.sh"

# Read hook input from stdin
INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
CWD=$(echo "$INPUT" | jq -r '.cwd // empty')
PROJECT=$(detect_project "$CWD")

# Ensure session exists
if [ -n "$SESSION_ID" ] && [ -n "$PROJECT" ]; then
  curl -sf "${ANCORA_URL}/sessions" \
    -X POST \
    -H "Content-Type: application/json" \
    -d "$(jq -n --arg id "$SESSION_ID" --arg project "$PROJECT" --arg dir "$CWD" \
      '{id: $id, project: $project, directory: $dir}')" \
    > /dev/null 2>&1
fi

# Fetch context from previous sessions
ENCODED_PROJECT=$(printf '%s' "$PROJECT" | jq -sRr @uri)
CONTEXT=$(curl -sf "${ANCORA_URL}/context?workspace=${ENCODED_PROJECT}" --max-time 3 2>/dev/null | jq -r '.context // empty')

# Inject Memory Protocol + compaction instruction + context
cat <<'PROTOCOL'
## Ancora Persistent Memory — ACTIVE PROTOCOL

You have ancora memory tools. This protocol is MANDATORY and ALWAYS ACTIVE.

### CORE TOOLS — always available, no ToolSearch needed
ancora_save, ancora_search, ancora_context, ancora_summarize, ancora_get, ancora_save_prompt

Use ToolSearch for other tools: ancora_update, ancora_suggest_topic, ancora_start, ancora_end, ancora_stats, ancora_delete, ancora_timeline, ancora_capture

### PROACTIVE SAVE — do NOT wait for user to ask
Call `ancora_save` IMMEDIATELY after ANY of these:
- Decision made (architecture, convention, workflow, tool choice)
- Bug fixed (include root cause)
- Convention or workflow documented/updated
- Notion/Jira/GitHub artifact created or updated with significant content
- Non-obvious discovery, gotcha, or edge case found
- Pattern established (naming, structure, approach)
- User preference or constraint learned
- Feature implemented with non-obvious approach

**Self-check after EVERY task**: "Did I just make a decision, fix a bug, learn something, or establish a convention? If yes → ancora_save NOW."

### SEARCH MEMORY when:
- User asks to recall anything ("remember", "what did we do", "acordate", "qué hicimos")
- Starting work on something that might have been done before
- User mentions a topic you have no context on

### SESSION CLOSE — before saying "done"/"listo":
Call `ancora_summarize` with: Goal, Discoveries, Accomplished, Next Steps, Relevant Files.

---

CRITICAL INSTRUCTION POST-COMPACTION — follow these steps IN ORDER:
PROTOCOL

printf "\n1. FIRST: Call ancora_summarize with the content of the compacted summary above. Use project: '%s'.\n" "$PROJECT"
printf "   This preserves what was accomplished before compaction.\n\n"
printf "2. THEN: Call ancora_context with project: '%s' to recover recent session history and observations.\n" "$PROJECT"
printf "   Read the returned context carefully — it tells you what was being worked on.\n\n"
cat <<'PROTOCOL'
3. If you need more detail on a specific topic, call ancora_search with relevant keywords.

4. Only THEN continue working on what the user asked.

All 4 steps are MANDATORY. Without them, you lose context and start blind.
PROTOCOL

# Inject memory context if available
if [ -n "$CONTEXT" ]; then
  printf "\n%s\n" "$CONTEXT"
fi

exit 0
