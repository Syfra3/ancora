#!/usr/bin/env bash
set -euo pipefail

# Ralph - Autonomous AI Agent Loop for PRD Execution
# Reads prd.json and executes user stories iteratively until all pass

RALPH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PRD_FILE="$RALPH_DIR/prd.json"
PROGRESS_FILE="$RALPH_DIR/progress.txt"
ARCHIVE_DIR="$RALPH_DIR/archive"

# Check if prd.json exists
if [[ ! -f "$PRD_FILE" ]]; then
  echo "❌ Error: prd.json not found in $RALPH_DIR"
  echo "Create one first using the ralph skill"
  exit 1
fi

# Extract project and branch name from prd.json
PROJECT=$(jq -r '.project' "$PRD_FILE")
BRANCH_NAME=$(jq -r '.branchName' "$PRD_FILE")

echo "🚀 Ralph Autonomous Loop Starting..."
echo "   Project: $PROJECT"
echo "   Branch: $BRANCH_NAME"
echo ""

# Check if we need to archive previous run
if [[ -f "$PROGRESS_FILE" ]]; then
  CURRENT_BRANCH=$(jq -r '.branchName' "$PRD_FILE")
  
  # Check if progress.txt has content beyond header
  if [[ $(wc -l < "$PROGRESS_FILE") -gt 3 ]]; then
    TIMESTAMP=$(date +%Y-%m-%d-%H%M%S)
    FEATURE_NAME=$(echo "$CURRENT_BRANCH" | sed 's/.*\///')
    ARCHIVE_PATH="$ARCHIVE_DIR/$TIMESTAMP-$FEATURE_NAME"
    
    echo "📦 Archiving previous run to: $ARCHIVE_PATH"
    mkdir -p "$ARCHIVE_PATH"
    cp "$PRD_FILE" "$ARCHIVE_PATH/"
    cp "$PROGRESS_FILE" "$ARCHIVE_PATH/"
    echo ""
  fi
fi

# Initialize progress.txt with header
cat > "$PROGRESS_FILE" << EOF
# Ralph Progress Log - $PROJECT
Branch: $BRANCH_NAME
Started: $(date '+%Y-%m-%d %H:%M:%S')

EOF

# Main loop: iterate through stories that haven't passed yet
MAX_ITERATIONS=10
iteration=0

while [[ $iteration -lt $MAX_ITERATIONS ]]; do
  iteration=$((iteration + 1))
  
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "🔄 Iteration $iteration"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo ""
  
  # Find next incomplete story
  NEXT_STORY=$(jq -r '.userStories[] | select(.passes == false) | .id' "$PRD_FILE" | head -n1)
  
  if [[ -z "$NEXT_STORY" ]]; then
    echo "✅ All user stories complete!"
    echo ""
    echo "Final status:"
    jq -r '.userStories[] | "  [\(.passes | if . then "✅" else "❌" end)] \(.id): \(.title)"' "$PRD_FILE"
    exit 0
  fi
  
  STORY_TITLE=$(jq -r ".userStories[] | select(.id == \"$NEXT_STORY\") | .title" "$PRD_FILE")
  STORY_DESC=$(jq -r ".userStories[] | select(.id == \"$NEXT_STORY\") | .description" "$PRD_FILE")
  STORY_CRITERIA=$(jq -r ".userStories[] | select(.id == \"$NEXT_STORY\") | .acceptanceCriteria[]" "$PRD_FILE")
  
  echo "📋 Working on: $NEXT_STORY - $STORY_TITLE"
  echo ""
  echo "Description: $STORY_DESC"
  echo ""
  echo "Acceptance Criteria:"
  echo "$STORY_CRITERIA" | sed 's/^/  • /'
  echo ""
  
  # Launch Claude/Gemini agent with story context
  # This is where you'd integrate with your AI agent CLI
  # For now, we'll use a placeholder that prompts the user
  
  echo "🤖 Launching AI agent for $NEXT_STORY..."
  echo ""
  
  # TODO: Replace with actual agent invocation
  # Example: claude-code --task "Implement $NEXT_STORY" --context "$PRD_FILE"
  
  read -p "Press Enter after agent completes the story, or Ctrl+C to stop..."
  
  # Prompt user to mark story as passed
  echo ""
  read -p "Did $NEXT_STORY pass all criteria? (y/n): " PASSED
  
  if [[ "$PASSED" == "y" || "$PASSED" == "Y" ]]; then
    # Update prd.json to mark story as passed
    jq ".userStories |= map(if .id == \"$NEXT_STORY\" then .passes = true else . end)" "$PRD_FILE" > "$PRD_FILE.tmp"
    mv "$PRD_FILE.tmp" "$PRD_FILE"
    
    echo "✅ $NEXT_STORY marked as complete"
    echo "" >> "$PROGRESS_FILE"
    echo "## $NEXT_STORY - PASSED ($(date '+%Y-%m-%d %H:%M:%S'))" >> "$PROGRESS_FILE"
    echo "$STORY_TITLE" >> "$PROGRESS_FILE"
  else
    read -p "Enter notes for retry: " NOTES
    jq ".userStories |= map(if .id == \"$NEXT_STORY\" then .notes = \"$NOTES\" else . end)" "$PRD_FILE" > "$PRD_FILE.tmp"
    mv "$PRD_FILE.tmp" "$PRD_FILE"
    
    echo "⚠️  $NEXT_STORY needs retry"
    echo "" >> "$PROGRESS_FILE"
    echo "## $NEXT_STORY - RETRY ($(date '+%Y-%m-%d %H:%M:%S'))" >> "$PROGRESS_FILE"
    echo "Notes: $NOTES" >> "$PROGRESS_FILE"
  fi
  
  echo ""
done

echo "⚠️  Max iterations reached. Some stories may still be incomplete."
jq -r '.userStories[] | "  [\(.passes | if . then "✅" else "❌" end)] \(.id): \(.title)"' "$PRD_FILE"
