---
title: Merges
description: Three-way merge with conflict detection, written markers, and always-reversible operations.
---

A **merge** applies the work an agent did on a branch back to `main`. AVC's merge is a **three-way merge**: it compares the base snapshot (where the branch was created), the current state of `main`, and the branch's HEAD snapshot to decide what to apply.

If diffing answers "what did the agent do?" and branching answers "where did it work?", merging answers "what reaches the main project?".

## How the three-way merge works

```
                   base snapshot
                       /    \
              (main edits)   (branch edits)
                    /          \
              main HEAD     branch HEAD
                    \          /
                     \        /
                    merged main
```

For every file:

| Modified on `main`? | Modified on branch? | Outcome |
|:---:|:---:|---|
| ❌ | ✅ | **Clean apply** — branch version overwrites `main` |
| ✅ | ❌ | **Skipped** — main's version stays |
| ❌ | ❌ | **Untouched** — no change |
| ✅ | ✅ | **Conflict** — both versions diverged since base |

Clean and skipped files apply automatically. Conflicts are surfaced to you.

## Conflict markers

When AVC detects a conflict, it writes the file with standard conflict markers to your working tree:

```
<<<<<<< main
the version from main
=======
the version from the branch
>>>>>>> feat-auth
```

You resolve the conflict by editing the file (just like Git), then take a new snapshot to mark the resolution.

## Always reversible

Before every merge, AVC takes an automatic snapshot of `main` labelled `Pre-merge safety snapshot`. If the merge goes wrong — wrong files applied, conflicts you don't want to deal with, anything — undo it with:

```bash
avc merge --abort
```

This restores `main` from the pre-merge snapshot. The agent branch is untouched.

## Commands

```bash
avc merge <branch>                  # apply clean changes, surface conflicts
avc merge <branch> --preview        # dry-run: show counts only, modify nothing
avc merge --abort                   # restore main from the pre-merge snapshot
```

Preview output:

```
Preview: merge "feat-auth" → main
  Clean:     12 file(s)
  Conflicts:  1 file(s)
  Skipped:    0 file(s)
```

This lets you decide whether to proceed before any files are touched.

## What the merge records

Every merge attempt is logged in the `merges` and `merge_files` tables:

| Table | Stores |
|-------|--------|
| `merges` | merge ID, branch, timestamp, base snapshot, target branch HEAD, pre-merge safety snapshot ID |
| `merge_files` | per-file decision: `clean` / `conflict` / `skip` |

You can query these via the database if you want a merge audit trail.

## Implementation notes

The merge engine opens the database in **three separate phases** to avoid lock contention:

1. **Plan** — read all three snapshots' file lists, compute per-file decisions
2. **Apply** — write clean files to the working tree, write conflict markers where needed
3. **Record** — write the `merges` row and per-file results

Each phase opens and closes the DB connection. This is one of the architectural rules in [CLAUDE.md](https://github.com/SkillMythOrg/agentic-vc/blob/main/CLAUDE.md) — one DB connection per operation.

## When merge is appropriate

Merge a branch when:

- The agent's work has been reviewed (by you or by tests)
- You want to incorporate the changes into `main` for downstream work
- The branch is "done" and you no longer need its isolation

If the agent's work was bad, **don't merge — delete**:

```bash
avc branch delete feat-bad-idea
```

Nothing reaches `main`. The workspace is cleaned up. You're back to square one.

## What merge is not

- **Not** a Git merge — no merge commits, no `--no-ff`, no octopus merges
- **Not** a rebase — branch snapshots are preserved as-is, they're just applied to `main`'s working tree
- **Not** automatic — you always run it explicitly; agents don't merge their own work
