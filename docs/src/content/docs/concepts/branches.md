---
title: Branches
description: Isolated agent workspaces for parallel experimentation.
---

A **branch** in AVC is a named sequence of snapshots that diverges from a point on `main`. It comes with its own materialized **workspace directory** at `.avc/workspaces/<branch>/`, so the agent edits files there — not in your real project root.

This is the key difference between AVC branches and Git branches: AVC branches give you **physical isolation**, not just logical labels. Your real project is provably untouched while an agent is working on a branch.

## Why branches matter for agents

When you let an AI agent loose on your project, you want two guarantees:

1. **Reversibility** — if the agent does the wrong thing, undoing is one command
2. **Isolation** — while the agent works, your real project files are not at risk

Snapshots alone give you (1). Branches give you (2). They're how AVC supports parallel agent runs — two agents can work on two branches without stepping on each other.

## How branches work

```
project/                       ← your real project root (main branch)
  src/
  README.md

.avc/
  avc.db                       ← all branches share one database
  objects/                     ← all branches share one object store
  workspaces/
    feat-auth/                 ← workspace for branch "feat-auth"
      src/                     ← hardlinked from project/ initially
      README.md
    feat-payments/             ← workspace for branch "feat-payments"
      src/
      README.md
```

When you run `avc branch create feat-auth`:

1. A new `branches` row is inserted in SQLite
2. A workspace directory is materialized at `.avc/workspaces/feat-auth/`
3. Files from the branch's base snapshot are **hardlinked** into the workspace (not copied — they share inodes until modified)
4. The active branch in `.avc/config.toml` is switched to `feat-auth`

The agent then works in `.avc/workspaces/feat-auth/`. When it modifies a file, the OS automatically copies-on-write — the original inode in `project/` stays untouched.

## Branch lifecycle

```bash
avc branch create feat-auth        # create + auto-switch + materialize workspace
avc branch list                    # list all branches; * marks active
avc branch switch main             # switch back to main
avc branch diff feat-auth          # cumulative diff from branch point to branch HEAD
avc branch delete feat-auth        # delete branch + remove workspace
```

Snapshots taken while on a branch land on that branch only. They're invisible to `main` until you merge.

## Main is special

The `main` branch is the only one without a workspace directory. It always operates on the real project root directly. This keeps things simple for users who don't use agent branches — `avc snapshot` on `main` works exactly as it did before branches existed.

## When to create a branch

Create a branch when:

- An agent is about to do anything non-trivial (refactor, add a feature, fix a bug)
- You want to try two approaches in parallel
- You want a sandbox to experiment in without disturbing your working tree

You don't need to create branches for tiny one-off changes — just snapshot on `main`. Branches add value when the work is uncertain and you want isolation.

## Storage cost

Branches use **hardlinks**, not full copies. A 50 MB project takes ~0 extra disk space per branch. Only files the agent actually modifies consume new bytes (the OS breaks the hardlink on write).

If hardlinks aren't available (cross-device, restrictive permissions), AVC falls back to a regular copy. The workspace still works — it just uses more disk.

## Merging back

Once an agent's work on a branch is reviewed and accepted, merge it back to `main`:

```bash
avc merge feat-auth --preview     # dry-run, see clean/conflict/skipped counts
avc merge feat-auth               # actually merge
avc merge --abort                 # undo if something went wrong
```

See [Concepts → Merges](/agentic-vc/concepts/merges/) for the full three-way merge story.

## What branches are not

- **Not** Git branches — independent storage, no remote tracking, no rebase
- **Not** nested — you can't branch off a branch (yet); the topology is `main` + flat agent branches
- **Not** shared — there's no remote, no fetch, no push. Branches live in `.avc/` and travel with the project

## Working with branches

| Action | Command |
|--------|---------|
| Create + switch | `avc branch create <name>` |
| List | `avc branch list` |
| Switch active | `avc branch switch <name>` |
| Delete + remove workspace | `avc branch delete <name>` |
| Cumulative diff vs branch point | `avc branch diff <name>` |
