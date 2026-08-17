---
name: avc-branch
description: Create an isolated AVC branch workspace before starting any task — no exceptions
---

Call **avc_branch_create** before starting any task. No exceptions.

Do not assess whether the task is "simple enough" to skip a branch — that judgement is not yours to make. NEVER edit files in the real project root directly.

## MUST call when

- You are about to create, edit, or delete any file
- The task involves more than one file or more than one step
- The user asks you to implement, refactor, fix, or add anything

## Steps

1. Create the branch:

```json
{ "name": "feat/<short-task-name>" }
```

2. The response includes a `workspace` path. **Set your working directory to that path immediately.** Every file you create or edit MUST be inside this directory. NEVER touch files in the real project root while on a branch.

3. Take a snapshot before each significant change:
```json
{ "label": "initial workspace state", "agent_name": "claude" }
```

4. When the task is complete, call **avc_branch_diff** and show the full output to the user before asking for merge approval.

## NEVER

- Edit files outside the workspace path while on a branch
- Call **avc_merge** without explicit user approval — show the diff first and wait for yes
- Retry a failed merge without calling **avc_merge_abort** first
