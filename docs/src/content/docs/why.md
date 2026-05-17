---
title: Why AVC
description: Why a new version control system designed for AI agents.
---

## The problem with Git for agent workflows

Git was designed in 2005 for distributed collaboration between humans. The mental model — staging area, working tree, three trees, refs, rebase, detached HEAD — assumes you're a developer who has spent years learning it.

AI agents don't have years. Neither do the users who run them.

When an agent modifies your code, you need to answer three questions immediately:
1. **What changed?**
2. **Did it break anything?**
3. **How do I undo it?**

Git can answer all three, but the path is long: `git status` → `git diff` → maybe `git stash` → `git checkout -- .` → hope you didn't lose something. Each of those commands has dozens of edge cases.

## What AVC does differently

AVC is built around **four primitives** designed for agent-assisted development:

| Primitive | Problem it solves | How |
|-----------|-------------------|-----|
| **Snapshot** | Changes must be auditable and reversible | Content-addressed snapshots with one command |
| **Diff** | It must be clear what the agent changed | File-by-file unified diff, line counts, syntax highlighting |
| **Branch** | Agents need isolated workspaces that can't corrupt main | Workspace-isolated branches at `.avc/workspaces/<name>/` |
| **Merge** | Approved agent work must flow back to main safely | Three-way merge with conflict detection and auto-rollback |

No staging area. No rebase. No detached HEAD. Branches always have a workspace directory the agent edits in — the real project root is untouched until you merge.

## Designed for nondeterminism

Allowing an agent to do real work introduces nondeterminism. You can no longer make hard guarantees about project state or behaviour. AVC is the explicit mechanism for reasoning about that nondeterminism:

- **Committing** — every change an agent makes is auditable and reversible
- **Branching** — agents work in an isolated, reproducible environment that cannot affect the production project until explicitly approved
- **Diffing** — it is immediately clear what an agent did
- **Merging** — if branching and diffing handle the case where the agent is wrong, merging handles the case where it's right

## Who is it for?

- **Developers using AI agents** (Claude Code, Cursor, Windsurf, custom MCP clients) who want a safety net
- **Non-technical users** running agents who need an undo button for autonomous code changes
- **Agent framework authors** building tools that need a reversible filesystem layer

AVC is **not** trying to replace Git. Use Git for your repo, branches, PRs, and remote collaboration. Use AVC for the granular, agent-driven snapshots that happen *inside* a single dev session — the work that doesn't deserve a commit but absolutely needs to be reversible.

## What AVC is not

- **Not** a Git wrapper — completely independent storage and data model
- **Not** a cloud service — 100% local, your code never leaves your machine
- **Not** a collaboration tool — single-user by design (v1)
- **Not** a backup tool — focused on dev-session snapshots, not long-term archival
