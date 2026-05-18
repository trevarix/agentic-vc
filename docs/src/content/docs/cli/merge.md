---
title: avc merge
description: Merge a branch into main with three-way conflict detection.
---

Three-way merge using the branch point, main HEAD, and branch HEAD. Clean changes auto-apply; conflicting files get standard `<<<` / `===` / `>>>` markers written to the working tree. See [Concepts → Merges](/agentic-vc/concepts/merges/) for the conceptual model.

## Usage

```bash
avc merge feat-auth                  # perform the merge
avc merge feat-auth --preview        # dry-run, show counts only
avc merge --abort                    # restore main from pre-merge snapshot
```

## Flags

| Flag | Description |
|------|-------------|
| `--preview` | Show counts (clean / conflict / skipped) without modifying files |
| `--abort` | Restore `main` from the auto-snapshot taken before the last merge |
| `--json` | JSON output |

## JSON output

```json
{
  "branch": "feat-auth",
  "clean": 12,
  "conflicts": 1,
  "skipped": 0,
  "pre_merge_snapshot": "snap-xyz789",
  "files": [
    { "path": "src/auth.go", "decision": "clean" },
    { "path": "src/config.go", "decision": "conflict" }
  ],
  "success": true
}
```

## Safety

Before every merge, AVC takes an automatic snapshot of `main` labelled `Pre-merge safety snapshot`. If the merge goes wrong, undo it:

```bash
avc merge --abort
```

This restores `main` from that pre-merge snapshot. The agent branch is untouched.

## Decision matrix

| Modified on `main`? | Modified on branch? | Decision |
|:---:|:---:|---|
| ❌ | ✅ | `clean` — branch version overwrites main |
| ✅ | ❌ | `skip` — main's version stays |
| ❌ | ❌ | (no change, not in output) |
| ✅ | ✅ | `conflict` — conflict markers written to file |

## Resolving conflicts

When `conflicts > 0`, AVC writes the files to your working tree with conflict markers. Edit them manually:

```
<<<<<<< main
the version from main
=======
the version from the branch
>>>>>>> feat-auth
```

Then take a new snapshot to mark the resolution as done.
