# Agentic Version Control (AVC) — Project Plan & Implementation

## Project Vision
A local version control system built specifically for the agent era. AVC gives agents and users the four primitives required to do real work safely: **commit**, **branch**, **diff**, and **merge** — without the complexity of Git.

Allowing an agent to perform real operational work introduces nondeterminism. You can no longer make hard guarantees about state or behavior. AVC is the explicit mechanism for reasoning about that nondeterminism:

- **Committing** — every change an agent makes is auditable and reversible.
- **Branching** — agents work in an isolated, reproducible environment that cannot affect the production project until explicitly approved.
- **Diffing** — it is immediately clear what an agent did. The diff reveals exactly what changed after the operational step.
- **Merging** — if branching and diffing handle the case where the agent is wrong, merging handles the case where it's right. Changes made on an agent branch are applied to the main branch through a controlled, reviewable mechanism.

### Problem Statement
- **User Pain:** When agents work on features, regressions happen. Without version history, users can't see what changed or easily revert. Worse, without isolated workspaces, a bad agent run corrupts the live project before anyone can intervene.
- **Audience:** Everyone — technical and non-technical users alike. Designed to be simple, intuitive, and accessible to all.
- **Goal:** Make version control effortlessly usable for everyone, and make agent-assisted development safe by default.

---

## Core Requirements (MVP)

### Feature Scope
1. **Snapshots** — Save project state with a timestamp and user-provided label
2. **Diff View** — See file-by-file changes between snapshots (with code highlighting)
3. **Restore** — Roll back project to any previous snapshot
4. **Metadata** — Track agent name, timestamp, user notes, file counts

### Storage & Triggers
- **Storage:** Local SQLite database for metadata; file hashing for efficient change tracking
- **Triggers:** Manual snapshots only (user clicks "save version") in v1
- **Scope:** Single project per AVC instance
- **History:** Linear history; no branching in v1

---

## User-Facing Interfaces

### VSCode Extension
**Primary experience for non-technical users:**
- Sidebar panel showing version list (newest first)
- Each version entry shows: label, timestamp, agent name, # files changed, user notes
- Click version → show side-by-side diff view with syntax highlighting
- Available actions: Set as Current, Delete, View Details
- Quick action: "Save Current Snapshot" button in sidebar header

### CLI Tool
**Programmatic access for agents and power users:**
```
avc snapshot <label> [--agent "agent_name"] [--notes "..."]
avc list
avc diff <version_id_1> <version_id_2>
avc restore <version_id>
avc info <version_id>
avc init <project_path>
```

### Agent Integration (Future)
- Agents invoke CLI directly
- AVC CLI returns JSON output for structured consumption
- No dedicated API yet; CLI is the primary integration point

---

## Data Model

### SQLite Schema

**projects table**
```sql
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  path TEXT UNIQUE,
  name TEXT,
  created_at INTEGER
);
```

**snapshots table**
```sql
CREATE TABLE snapshots (
  id TEXT PRIMARY KEY,
  project_id TEXT,
  timestamp INTEGER,
  label TEXT,
  agent_name TEXT,
  notes TEXT,
  file_count INTEGER,
  total_size INTEGER,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);
```

**files table**
```sql
CREATE TABLE files (
  id TEXT PRIMARY KEY,
  snapshot_id TEXT,
  relative_path TEXT,
  file_hash TEXT,
  file_size INTEGER,
  FOREIGN KEY (snapshot_id) REFERENCES snapshots(id)
);
```

**diffs table** (cache for performance)
```sql
CREATE TABLE diffs (
  id TEXT PRIMARY KEY,
  from_snapshot_id TEXT,
  to_snapshot_id TEXT,
  file_path TEXT,
  diff_type TEXT,
  old_hash TEXT,
  new_hash TEXT,
  change_summary TEXT,
  FOREIGN KEY (from_snapshot_id) REFERENCES snapshots(id),
  FOREIGN KEY (to_snapshot_id) REFERENCES snapshots(id)
);
```

---

## System Architecture

```
┌─────────────────────────────────────────────────────┐
│                   VSCode Extension                   │
│  (Snapshot list, diff viewer, restore UI, settings)  │
└────────────────────┬────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────┐
│           CLI Tool (avc command)                     │
│  (snapshot, list, diff, restore, init)              │
└────────────────────┬────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────┐
│          Core Engine (Go)                            │
│  • Snapshot creation & hashing                       │
│  • File diffing & delta detection                    │
│  • SQLite operations & queries                       │
│  • Project initialization & validation              │
└────────────────────┬────────────────────────────────┘
                     │
                     ▼
         ┌──────────────────────────┐
         │  .avc/ Project Directory │
         │  ├── avc.db (SQLite)     │
         │  ├── config.toml         │
         │  └── .gitignore          │
         └──────────────────────────┘
```

---

## Communication Architecture

### Extension-to-CLI Communication Model

The VSCode extension communicates with the Go CLI through **subprocess execution** with JSON-based message passing:

```
┌─────────────────────────────────┐
│      VSCode Extension           │
│  (TypeScript + VSCode API)      │
└────────────┬────────────────────┘
             │
             │ spawns child process
             │ (execFile / exec)
             │
             ▼
┌─────────────────────────────────┐
│    avc CLI (Go executable)      │
│  $ avc list --json              │
│  $ avc snapshot "label" --json   │
│  $ avc restore <id> --json       │
└────────────┬────────────────────┘
             │
             │ stdout (JSON)
             │ stderr (errors)
             │ exit code (status)
             │
             ▼
┌─────────────────────────────────┐
│  Extension parses JSON,         │
│  updates sidebar UI             │
└─────────────────────────────────┘
```

### Design Principles

1. **Stateless Calls** — Each CLI invocation is independent; no persistent daemon
2. **JSON Protocol** — All data exchange uses JSON with `--json` flag
3. **Human-Readable Output** — Without `--json`, CLI outputs pretty-printed text for manual use
4. **Quick Execution** — Each operation completes in < 1 second
5. **Error Signaling** — Non-zero exit codes indicate errors; stderr contains error messages

### CLI Commands and JSON Output

**List all snapshots:**
```bash
$ avc list --json
[
  {
    "id": "snap-abc123",
    "label": "Before refactor",
    "timestamp": 1712275200,
    "agent_name": "gpt-4",
    "files_changed": 12,
    "total_size": 524288,
    "notes": "Initial snapshot"
  },
  {
    "id": "snap-def456",
    "label": "Fixed bug in auth",
    "timestamp": 1712282400,
    "agent_name": "claude-opus",
    "files_changed": 3,
    "total_size": 512000,
    "notes": "Security patch"
  }
]
```

**Create a snapshot:**
```bash
$ avc snapshot "v1.2.0 release" --agent "my-agent" --notes "Ready for testing" --json
{
  "id": "snap-xyz789",
  "label": "v1.2.0 release",
  "timestamp": 1712289600,
  "agent_name": "my-agent",
  "files_changed": 42,
  "total_size": 1048576,
  "notes": "Ready for testing",
  "success": true
}
```

**Restore to a snapshot:**
```bash
$ avc restore snap-abc123 --json
{
  "id": "snap-abc123",
  "restored_files": 12,
  "restored_size": 524288,
  "success": true,
  "message": "Successfully restored snapshot snap-abc123"
}
```

**Get diff between snapshots:**
```bash
$ avc diff snap-abc123 snap-def456 --json
{
  "from_snapshot": "snap-abc123",
  "to_snapshot": "snap-def456",
  "files": [
    {
      "path": "src/auth.go",
      "type": "modified",
      "old_hash": "abc...",
      "new_hash": "def...",
      "lines_added": 5,
      "lines_removed": 2,
      "diff_preview": "..."
    },
    {
      "path": "README.md",
      "type": "modified",
      "old_hash": "ghi...",
      "new_hash": "jkl...",
      "lines_added": 10,
      "lines_removed": 0
    }
  ]
}
```

### Extension Implementation Pattern

**In extension's `cliProxy.ts`:**
```typescript
import { execFile } from 'child_process';

function runAvcCommand(args: string[]): Promise<any> {
  return new Promise((resolve, reject) => {
    execFile('avc', [...args, '--json'], (error, stdout, stderr) => {
      if (error) {
        reject(new Error(stderr || error.message));
        return;
      }
      try {
        resolve(JSON.parse(stdout));
      } catch (e) {
        reject(new Error('Invalid JSON response from CLI'));
      }
    });
  });
}

// Usage in sidebar:
async function loadSnapshots() {
  try {
    const snapshots = await runAvcCommand(['list']);
    updateSidebarUI(snapshots);
  } catch (err) {
    showErrorMessage(`Failed to load snapshots: ${err.message}`);
  }
}

async function restoreSnapshot(snapshotId: string) {
  try {
    const result = await runAvcCommand(['restore', snapshotId]);
    showInfoMessage(`Restored ${result.restored_files} files`);
    await loadSnapshots(); // refresh list
  } catch (err) {
    showErrorMessage(`Restore failed: ${err.message}`);
  }
}
```

### Error Handling Strategy

**CLI Errors:**
```bash
$ avc restore invalid-id --json
# Exits with code 1 and stderr:
# Error: snapshot 'invalid-id' not found
```

**Extension catches and displays:**
- Toast notification with user-friendly message
- Sidebar remains functional; user can retry
- No data loss on failed operations

### Why This Approach?

| Aspect | Subprocess + JSON | HTTP Server | Direct Library |
|--------|-------------------|-------------|-----------------|
| **Setup** | None — just run binary | Server mgmt, port mgmt | Language barrier |
| **Reliability** | Simple, proven | More moving parts | Tight coupling |
| **Testing** | Test CLI independently | Need test server | Harder to isolate |
| **For Agents** | Rich CLI interface | Wrap HTTP | Force library usage |
| **Platform** | Works everywhere | Works everywhere | Language-specific |

---

## Technology Stack

### CLI & Core Engine
**Decision:** Choose one of the following:

| Language | Pros | Cons | Recommendation |
|----------|------|------|-----------------|
| **Go** | Single binary, fast execution, simple concurrency, excellent stdlib (crypto, sqlite, io), easy multi-platform builds, fast iteration | Slightly larger binaries than Rust, GC pauses (negligible for this use) | ✅ **Recommended** — Best balance of speed, simplicity, and distribution |
| **Rust** | Fastest execution, smallest binaries, maximum control, production-hardened | Steeper learning curve, longer build times | Excellent choice if max performance is critical; otherwise slower iteration |
| **Python** | Fastest MVP, simplest syntax, quick prototyping | Requires pip/poetry for distribution, slower execution, runtime dependency | Good for rapid iteration; migrate to Go/Rust for production if needed |

### VSCode Extension
- **TypeScript** + VSCode API
- **Webview** for diff viewer with syntax highlighting (Prism.js or Monaco editor)
- Packaged via VSCode Extension CLI

### Database
- **SQLite** — Zero-setup, file-based, local-only, ideal for single-user tools

### Diff Library
- **Unified diff format** (industry standard)
- Display: line-by-line diffs with added/modified/deleted indicators and syntax highlighting

---

## Implementation Roadmap

### Phase 1: Core CLI Engine
**Goal:** Build the foundational snapshot, restore, and diff CLI.

**Deliverables:**
- [ ] `avc init <project_path>` — Initialize AVC for a project (create .avc/ directory)
- [ ] `avc snapshot <label>` — Create a snapshot with file hashing
- [ ] `avc list` — Show all snapshots with metadata
- [ ] `avc restore <version_id>` — Roll back project files to a snapshot
- [ ] `avc diff <from_id> <to_id>` — Generate file-by-file diff
- [ ] SQLite schema and CRUD operations
- [ ] File hashing and change detection (SHA256)
- [ ] Ignore-file pattern matching (.gitignore-like rules)
- [ ] Error handling and validation

**Success Criteria:**
- Users can snapshot their project
- Users can restore to any previous snapshot without data loss
- CLI returns structured output (JSON for programmatic use)

---

### Phase 2: VSCode Extension UI
**Goal:** Make snapshots and restoration accessible to non-technical users.

**Deliverables:**
- [ ] VSCode extension project setup (TypeScript, manifest)
- [ ] Sidebar panel showing all snapshots
  - Display for each: label, timestamp, agent name, # files changed
  - Sortable, searchable by label
- [ ] "Save Snapshot" quick-action button with dialog for label/notes
- [ ] "Restore to This Version" button with confirmation prompt
- [ ] "Delete Snapshot" with safety confirmation
- [ ] Integration with CLI (extension calls `avc list`, `avc restore`, etc.)
- [ ] Settings: AVC project path, default agent name
- [ ] Status bar indicator: "X versions saved"

**Success Criteria:**
- Non-technical users can snapshot and restore via UI
- No need to touch CLI
- Snapshot labels appear immediately in sidebar

---

### Phase 3: Code Diff Viewer
**Goal:** Show users exactly what changed between snapshots.

**Deliverables:**
- [ ] Webview-based diff viewer in editor panel
- [ ] Load file diffs between two snapshots
- [ ] Side-by-side diff layout (old vs. new) or unified view
- [ ] Syntax highlighting for 50+ languages
- [ ] Color-coded indicators: green for additions, red for deletions
- [ ] File-by-file navigation (dropdown or list)
- [ ] Copy-to-clipboard for diffs
- [ ] File statistics: lines changed per file
- [ ] Link from snapshot detail to diff view

**Success Criteria:**
- Users can understand what their agent changed at a glance
- Code is readable with proper syntax highlighting
- Performance is acceptable even for large files (< 1s load time)

---

### Phase 4: Branching — Agent Workspaces
**Goal:** Give agents an isolated environment to work in, keeping the main project untouched until the user approves.

A branch in AVC is a named sequence of snapshots that diverges from a point on the main branch. The agent works freely on its branch. The user can review the cumulative diff, then keep or discard the entire branch. Nothing reaches main until the user says so.

**CLI additions:**
```
avc branch create <name> [--from <snapshot_id>]   # fork from latest or a specific snapshot
avc branch list                                    # show all branches
avc branch switch <name>                           # set active branch for subsequent snapshots
avc branch delete <name>                           # discard a branch and all its snapshots
avc branch diff <branch_name>                      # cumulative diff vs. the branch point on main
```

**Deliverables:**
- [ ] `branches` table in SQLite schema (`id`, `name`, `project_id`, `base_snapshot_id`, `created_at`)
- [ ] `branch_id` foreign key on `snapshots` table; `main` branch created on `avc init`
- [ ] `avc branch create / list / switch / delete` commands
- [ ] `avc branch diff <name>` — cumulative diff from branch point to latest branch snapshot
- [ ] Active branch tracked in `.avc/config.toml` (`[branch] active = "main"`)
- [ ] VSCode extension: branch selector in sidebar header; branch-scoped snapshot list
- [ ] `avc snapshot` respects the active branch — snapshots land on the correct branch
- [ ] Restore scoped to a branch — restoring a branch snapshot does not affect main

**Success Criteria:**
- An agent can be pointed at an AVC branch and snapshot its work without touching main
- The user can see the full cumulative diff of what the agent did on its branch
- Discarding a bad agent branch leaves main completely unchanged

---

### Phase 5: Merging — Controlled Integration
**Goal:** Provide a controlled mechanism for applying a reviewed agent branch to main.

Branching handles the case where the agent is wrong. Merging handles the case where it's right.

**CLI additions:**
```
avc merge <branch_name>               # apply branch changes to main (clean merges only)
avc merge <branch_name> --preview     # show what would change before committing
```

**Merge strategy:**
- **Clean merge** — files modified only on the agent branch (not touched on main since branch point) are applied automatically.
- **Conflict** — files modified on both the agent branch and main since the branch point are surfaced to the user. AVC writes both versions to disk with conflict markers and records the conflict in the DB.
- **Discard** — `avc branch delete` with no merge; main is untouched.

**Deliverables:**
- [ ] `avc merge <branch>` — applies clean changes, identifies conflicts
- [ ] `avc merge <branch> --preview` — dry-run with conflict report
- [ ] `merges` table: records merge attempts, per-file outcomes (`clean` / `conflict` / `skipped`)
- [ ] Conflict marker format written to working tree for conflicted files
- [ ] `avc merge --abort` — revert in-progress merge back to pre-merge state
- [ ] VSCode extension: "Merge to Main" button on agent branches with conflict summary panel
- [ ] Auto-snapshot main before every merge (safety net)

**Success Criteria:**
- Clean agent branches merge to main in one command with no manual intervention
- Conflicted files are clearly identified and the user can resolve them in the editor
- A failed or unwanted merge can always be fully reverted

---

### Phase 6: Polish, Testing & Release
**Goal:** Production-ready, documented, tested system covering all four primitives.

**Deliverables:**
- [ ] Comprehensive error handling and user-friendly messages
- [ ] Unit tests for core engine (file hashing, diff logic, DB operations)
- [ ] Integration tests (snapshot → restore workflow, branch → merge workflow)
- [ ] VSCode extension tests
- [ ] Performance benchmarks
  - Snapshot a 50MB project: < 2 seconds
  - Generate diff view: < 500ms
  - Restore: < 5 seconds
  - Branch create / switch: < 100ms
  - Clean merge: < 3 seconds
- [ ] Documentation
  - README with quick-start guide
  - CLI help text and examples
  - VSCode extension guide
  - Architecture documentation for future contributors
- [ ] VSCode Marketplace listing and publishing

**Success Criteria:**
- Zero critical bugs in MVP
- All integration tests pass
- Performance meets benchmarks
- Users can get started in < 5 minutes

---

## Key Design Decisions

1. **Manual snapshots only** — No automatic triggers in v1; keeps scope tight and lets users maintain explicit control over history.

2. **SQLite + file hashing** — Store metadata in SQLite for efficient querying; use SHA256 hashes to detect file changes without duplicating entire file contents unless needed.

3. **CLI-first architecture** — Build the CLI first; VSCode extension wraps it. This ensures agents can use AVC independently.

4. **Ignore patterns** — Respect `.avcignore` (similar to `.gitignore`) to exclude build artifacts, node_modules, environment files, etc.

5. **Linear history in v1, branching in Phase 4** — The main branch is linear in v1 to keep the mental model simple for non-technical users. Branches are introduced in Phase 4 specifically as agent workspaces, not as a general-purpose feature. The history DAG is intentionally shallow: one main branch, N agent branches, no nested branching.

6. **Local-only storage** — No cloud sync, no remote repositories. Simplifies design and respects privacy.

7. **Immutable snapshots** — Once created, snapshots cannot be modified. Users can delete them, but not edit them.

---

## Non-Goals (all phases)

- ❌ Collaboration or multi-user sync
- ❌ Remote backup or cloud storage
- ❌ Integration with Git (keep AVC independent)
- ❌ Automatic triggers or file watches
- ❌ Compression (unless storage becomes an issue)

## Deferred to later phases (not v1)

- 🔜 Branching — agent workspaces (Phase 4)
- 🔜 Merging — applying agent branches to main (Phase 5)
- 🔜 Fine-grained conflict resolution UI (Phase 5)

---

## Project Structure

```
agentic_version_ctl/
├── README.md                        # Quick-start guide
├── go.mod                           # Go module definition
├── go.sum                           # Dependency checksums
├── main.go                          # CLI entry point
├── cmd/avc/                         # CLI commands
│   ├── init.go                      # avc init command
│   ├── snapshot.go                  # avc snapshot command
│   ├── restore.go                   # avc restore command
│   ├── diff.go                      # avc diff command
│   └── list.go                      # avc list command
├── internal/                        # Core engine (not exported)
│   ├── db/                          # SQLite operations
│   │   └── db.go
│   ├── snapshot/                    # Snapshot logic
│   │   └── snapshot.go
│   ├── restore/                     # Restore logic
│   │   └── restore.go
│   ├── diff/                        # Diff generation
│   │   └── diff.go
│   ├── fileutil/                    # File hashing, I/O
│   │   └── fileutil.go
│   └── config/                      # Configuration parsing
│       └── config.go
├── tests/                           # Unit & integration tests
│   ├── snapshot_test.go
│   ├── restore_test.go
│   ├── diff_test.go
│   └── integration_test.go
├── extension/                       # VSCode extension
│   ├── package.json
│   ├── src/
│   │   ├── extension.ts             # Extension entry point
│   │   ├── sidebar.ts               # Sidebar panel UI
│   │   ├── diffViewer.ts            # Diff viewer logic
│   │   └── cliProxy.ts              # Calls to avc CLI
│   └── media/                       # UI assets, CSS
├── examples/                        # Example usage scripts
└── docs/                            # Extended documentation
    ├── architecture.md
    ├── cli-reference.md
    └── contributing.md
    └── project-description.md       # This file
```

---

## Success Metrics

- **Usability:** Non-technical user can snapshot and restore within 2 minutes of opening VSCode extension
- **Reliability:** 0% data loss in snapshot/restore cycles
- **Performance:** Snapshot a 50MB project in < 2 seconds
- **Adoption:** 100+ VSCode extension installs in first month
- **User Feedback:** "Finally, I can see what my agent broke and undo it painlessly"

---

## Next Steps

1. **Set up Go project** — Initialize `go mod`, create folder structure
2. **Create base CLI scaffolding** — Set up command structure with `cobra` or `flag` package
3. **Define ignore patterns** — Create default `.avcignore` rules
4. **Create mockups** — Sketch VSCode sidebar and diff viewer UI
5. **Begin Phase 1** — Start building core CLI engine

---

## Questions for Refinement

- Should snapshots be point-in-time backups or just metadata?
- How many snapshots should we support before warning the user (e.g., archive old ones)?
- Should there be a way to "tag" a snapshot as "stable" or "working"?
- Do we need to support selective file restoration (restore only certain files)?
