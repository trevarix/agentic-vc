---
name: avc-merge
description: Merge an AVC branch into main — requires explicit user approval
---

Merge your branch into main only after the user has reviewed the diff and said yes.

## Required sequence — no exceptions

1. Call **avc_branch_diff** and show the full output to the user
2. Ask the user: "Shall I merge branch X into main?"
3. If the user says yes: call **avc_merge**
   - avc_merge checks for conflicts automatically before writing anything
   - If conflicts are found, it returns them without modifying main — show them to the user and ask how to resolve
   - If clean, it auto-snapshots main and applies the changes

## NEVER

- Call **avc_merge** without explicit user approval
- Infer approval from context — the user must say yes explicitly
- Retry a failed merge without calling **avc_merge_abort** first

## If something goes wrong

Call **avc_merge_abort** immediately. This restores main from the pre-merge auto-snapshot. No data is lost.
