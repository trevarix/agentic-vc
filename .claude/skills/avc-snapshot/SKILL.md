---
name: avc-snapshot
description: Save an AVC snapshot before making changes — call this proactively, not on request
---

Call **avc_snapshot** before making any code change. No exceptions.

Do not assess whether the change is "small enough" or "easily reversible" — that judgement is not yours to make. The cost of an unnecessary snapshot is negligible. The cost of skipping one when it was needed is not.

## How to call

```json
{
  "label": "auto: before <what you are about to do>",
  "agent_name": "claude",
  "notes": "<brief description of the change planned>"
}
```

## Label format — always use the `auto:` prefix

All agent-created snapshots MUST start with `auto:` so they are distinguishable from user-created snapshots in `avc list`.

The `<action>` part should be 2–5 words describing the specific change:
- CORRECT: `auto: before auth middleware refactor`
- WRONG: `auth routes added` (missing prefix)
- WRONG: `auto: making changes to the authentication system` (too vague, too long)

## On failure

If the task breaks something, do NOT attempt repeated fixes. Call **avc_restore** immediately to return to the last good snapshot, then retry from a clean state.
