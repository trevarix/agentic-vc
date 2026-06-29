# Workspace Isolation — Phase 4.5

## Problem

Phase 4 branches are **logical** — they tag snapshots by branch but the agent still
edits files in the real project root. There is no actual isolation. Switching branches
does not change the working directory, and two concurrent agents would clobber each
other's work.

True isolation requires the agent to work in a **separate materialized copy** of the
project. The real project root is untouched until the user explicitly merges.

---

## Design

### Workspace directory

Each non-`main` branch gets a workspace materialized at:

```
.avc/workspaces/<branch-name>/
```

This lives inside `.avc/`, which is already excluded from all snapshots and
`.avcignore` patterns. No configuration needed — the path is derived from the branch
name.

`main` has no workspace. It always operates on the real project root directly.

### What changes on each branch operation

| Operation | Before (Phase 4) | After (Phase 4.5) |
|-----------|-----------------|-------------------|
| `branch create <name>` | Creates DB record, sets config | + materializes workspace from base snapshot |
| `branch switch <name>` | Updates config only | Unchanged — no file movement |
| `branch delete <name>` | Removes DB record | + removes `.avc/workspaces/<name>/` |
| `avc snapshot` on branch | Walks project root | Walks workspace dir |
| `avc restore <id>` on branch | Restores to project root | Restores to workspace dir |
| `avc snapshot` on main | Walks project root | Unchanged |
| `avc restore <id>` on main | Restores to project root | Unchanged |

### Storage: hardlinks over copies

Materializing a workspace by copying a 50 MB project for every branch is wasteful.
Instead, unchanged files are **hardlinked** — they share the same inode on disk.
Only files that the agent actually modifies consume new disk space (the OS breaks the
hardlink on write automatically on all major filesystems).

```
project/src/app.go          (inode 1001)
.avc/workspaces/feat/src/app.go  → hardlink to inode 1001

# agent modifies feat/src/app.go
.avc/workspaces/feat/src/app.go  → new inode 1002  (copy-on-write via OS)
project/src/app.go          (inode 1001)  ← untouched
```

Fallback: if `os.Link` fails (cross-device, permissions, filesystem limitation),
fall back to a regular file copy. The workspace still works — just uses more disk.

---

## Implementation plan

### Step 1 — `restore`: add target directory parameter

**File:** `avc/internal/restore/restore.go`

Add `RestoreToDir(projectRoot, snapshotID, targetDir string) (*Result, error)`.

- `projectRoot` — where `.avc/` lives (object store + DB lookups)
- `targetDir` — where files are written (workspace or real project root)
- Existing `Restore(projectRoot, snapshotID)` becomes a one-line wrapper:
  `RestoreToDir(projectRoot, snapshotID, projectRoot)`

No DB or object store changes needed — objects are always read from the same store
regardless of where files land.

### Step 2 — `snapshot`: add source directory parameter

**File:** `avc/internal/snapshot/snapshot.go`

Add `sourceDir string` parameter to `Create`. The directory walk happens in
`sourceDir`; DB writes and object store writes still go to `projectRoot`.

```go
func Create(projectRoot, label, agentName, notes, branchID, sourceDir string) (*Result, error)
```

If `sourceDir == ""`, default to `projectRoot` (backwards-compatible for main).

Update all callers: `cmd/avc/snapshot.go`, `tests/`.

### Step 3 — `branch`: workspace helpers

**File:** `avc/internal/branch/branch.go`

Add:

```go
// WorkspacePath returns the materialized workspace directory for a branch.
// main branch returns "" (uses project root).
func WorkspacePath(projectRoot, branchName string) string

// MaterializeWorkspace populates the workspace directory from the branch's
// base snapshot using hardlinks where possible, copies otherwise.
func MaterializeWorkspace(projectRoot string, b *db.Branch) error

// RemoveWorkspace deletes the workspace directory for a branch.
func RemoveWorkspace(projectRoot, branchName string) error
```

`MaterializeWorkspace` calls `restore.RestoreToDir(projectRoot, b.BaseSnapshotID,
WorkspacePath(projectRoot, b.Name))`.

### Step 4 — wire into `branch create` and `branch delete`

**File:** `avc/cmd/avc/branch.go`

`runBranchCreate`:
- After inserting the branch record, call `branch.MaterializeWorkspace`
- Print workspace path in both human and JSON output:
  ```
  Branch created: feature/experiment
    Workspace: .avc/workspaces/feature-experiment
  ```

`runBranchDelete`:
- After deleting the branch record, call `branch.RemoveWorkspace`

### Step 5 — `avc snapshot` resolves source directory

**File:** `avc/cmd/avc/snapshot.go`

After resolving the active branch ID, compute the source directory:

```go
sourcDir := projectPath
if branchName != "main" {
    ws := branchpkg.WorkspacePath(projectPath, branchName)
    if _, err := os.Stat(ws); err == nil {
        sourceDir = ws
    }
}
```

Pass `sourceDir` to `snapshot.Create`.

### Step 6 — `avc restore` resolves target directory

**File:** `avc/cmd/avc/restore.go`

After branch scoping check, compute the target directory:

```go
targetDir := projectPath
if snap.BranchID != "" && activeBranchName != "main" {
    ws := branchpkg.WorkspacePath(projectPath, activeBranchName)
    if _, err := os.Stat(ws); err == nil {
        targetDir = ws
    }
}
```

Call `restore.RestoreToDir(projectPath, snapshotID, targetDir)`.

### Step 7 — tests

**File:** `avc/tests/workspace_test.go`

- `TestWorkspace_MaterializeCreatesFiles` — branch create → workspace contains base snapshot files
- `TestWorkspace_HardlinkSharedInode` — two files in workspace share inode with project root (Unix only, skip on Windows)
- `TestWorkspace_SnapshotWalksWorkspace` — snapshot on branch captures workspace state, not project root
- `TestWorkspace_RestoreTargetsWorkspace` — restore on branch writes to workspace, not project root
- `TestWorkspace_DeleteRemovesWorkspace` — branch delete removes workspace directory
- `TestWorkspace_MainBranchNoWorkspace` — main branch workspace path is empty string

### Step 8 — extension: workspace path in branch info

**File:** `extension/src/cliProxy.ts`

Add `workspace_path: string` to the `Branch` interface (populated from CLI JSON output).

**File:** `extension/src/extension.ts`

After `createBranch`, show the workspace path in the notification so the user (or
agent instructions) can see where to direct the agent:

```
AVC: Branch "feature/experiment" created.
Workspace: /path/to/.avc/workspaces/feature-experiment
```

---

## Agent instruction (Phase 6 — `avc init --skills`)

The agent instruction files written by `avc init --skills <framework>` must include:

> After running `avc branch create <name>`, set your working directory to the
> `workspace` path returned in the output before making any file changes.
> All edits, file creates, and deletes must happen inside the workspace directory.
> Do not modify files in the project root while on an agent branch.

This is the bridge between workspace isolation and agent behaviour — without it, the
agent still edits the real project root even though a workspace exists.

---

## What this unblocks

Once workspace isolation is in place:

- **Phase 5 merge** becomes meaningful: it applies workspace changes to the real
  project root, which is provably unmodified since branch creation
- **Conflict detection** is reliable: the base snapshot, main HEAD, and branch HEAD
  are all truly independent states
- **Multiple concurrent agents** can each work in their own workspace without
  stepping on each other

---

## What this does not change

- The object store is still shared — identical files across workspaces and the real
  project share one stored blob. Content-addressing handles deduplication
  automatically.
- The DB is still a single `avc.db` — all branch and snapshot metadata in one place.
- `main` branch behaviour is identical to Phase 1-4 — no workspace, no new concepts
  for users who don't use agent branches.
