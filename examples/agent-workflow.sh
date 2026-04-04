#!/usr/bin/env bash
# Example: agent workflow using the avc CLI.
# An agent calls these commands before and after making changes.

set -euo pipefail

PROJECT_DIR="${1:-.}"

# Ensure the project is initialized.
avc init "$PROJECT_DIR"

# Snapshot before the agent starts working.
avc snapshot "Pre-agent baseline" \
  --agent "claude-sonnet" \
  --notes "Snapshot before automated refactor" \
  --json

echo "✓ Baseline snapshot saved. Starting agent work..."

# ... agent does its work here ...

# Snapshot after the agent finishes.
avc snapshot "Post-agent refactor" \
  --agent "claude-sonnet" \
  --notes "Refactored auth module" \
  --json

echo "✓ Post-work snapshot saved."

# Show what changed.
SNAPSHOTS=$(avc list --json)
FROM=$(echo "$SNAPSHOTS" | python3 -c "import sys,json; s=json.load(sys.stdin); print(s[1]['id'])")
TO=$(echo "$SNAPSHOTS"   | python3 -c "import sys,json; s=json.load(sys.stdin); print(s[0]['id'])")

echo ""
echo "Changes made by agent:"
avc diff "$FROM" "$TO"
