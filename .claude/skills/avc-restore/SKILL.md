---
name: avc-restore
description: Restore to a previous AVC snapshot when something breaks or the user asks to undo
---

Call **avc_restore** to roll back to a previous state. Do this immediately when something breaks — do not attempt fixes on broken state.

## MUST call when

- Tests fail after your changes
- The build breaks or the app crashes
- You introduced a regression
- The user says: "undo", "revert", "roll back", "start over", "go back to before"
- You want to try a different approach to the same problem

## Steps

1. Call **avc_list** to see available snapshots — NEVER guess an ID
2. Identify the last known-good snapshot
3. Call **avc_restore**:

```json
{ "id": "<snapshot-id>" }
```

4. Call **avc_snapshot** immediately after restoring to create a clean baseline before retrying

## Important

On an agent branch, restore only affects your workspace. The real project root is untouched.
