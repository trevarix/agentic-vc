---
title: avc branch
description: Create and manage branches (agent workspaces).
---

Branches isolate agent work in `.avc/workspaces/<branch>/` so experiments never touch your real project until you merge. See [Concepts → Branches](/agentic-vc/concepts/branches/) for the conceptual model.

## Subcommands

```bash
avc branch create <name>       # create a branch from the current snapshot
avc branch list                # list all branches; * marks active
avc branch switch <name>       # switch active branch
avc branch delete <name>       # delete branch + remove workspace
avc branch diff <name>         # cumulative diff from branch point to branch HEAD
```

## `avc branch create`

```bash
avc branch create feat-auth
avc branch create feat-auth --from snap-abc123
```

| Flag | Description |
|------|-------------|
| `--from <snapshot_id>` | Branch from a specific snapshot instead of the latest on main |
| `--json` | JSON output |

JSON output:

```json
{
  "id": "br-feat-auth",
  "name": "feat-auth",
  "base_snapshot_id": "snap-abc123",
  "workspace_path": "/path/to/project/.avc/workspaces/feat-auth",
  "success": true
}
```

`avc branch create` **auto-switches** to the new branch. After running it, you're on the new branch and subsequent snapshots land there.

## `avc branch list`

```bash
avc branch list
avc branch list --json
```

JSON output:

```json
[
  { "id": "br-main", "name": "main", "active": true,  "base_snapshot_id": "", "workspace_path": "" },
  { "id": "br-feat-auth", "name": "feat-auth", "active": false, "base_snapshot_id": "snap-abc123", "workspace_path": "/path/.../feat-auth" }
]
```

## `avc branch switch`

```bash
avc branch switch main
avc branch switch feat-auth
```

Updates `.avc/config.toml`'s `[branch] active` field. No files are moved — switching is instantaneous because each branch already has its own workspace.

## `avc branch delete`

```bash
avc branch delete feat-bad-idea
```

Removes the branch record from the database **and** deletes the workspace directory. The object store is unaffected (other branches/snapshots may still reference those blobs).

You cannot delete `main`.

## `avc branch diff`

```bash
avc branch diff feat-auth
avc branch diff feat-auth --json
```

Returns the cumulative diff from the branch's base snapshot to the latest snapshot on that branch — i.e., "what has the agent done so far?".

The JSON shape is identical to [`avc diff`](/agentic-vc/cli/diff/).

## Workflow example

```bash
# Start a new agent task on a branch
avc branch create feat-refactor

# Agent works in .avc/workspaces/feat-refactor/
# Snapshots as usual; they land on the branch
avc snapshot "WIP refactor" --agent claude

# Review and merge back when done
avc merge feat-refactor --preview
avc merge feat-refactor
```
