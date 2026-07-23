<div align="center">

# AVC — Agentic Version Control

**Version control built for the agent era.**
Snapshot, diff, branch, and merge — without the complexity of Git.

[![CI](https://github.com/trevarix/agentic-vc/actions/workflows/ci.yml/badge.svg)](https://github.com/trevarix/agentic-vc/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](avc/go.mod)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-server-6E56CF)](#agent-integration)
[![Docs](https://img.shields.io/badge/docs-avc.trevarix.com-informational)](https://avc.trevarix.com)

</div>

AVC gives agents and users four primitives to work safely:

| Primitive | What it does |
|-----------|-------------|
| **Snapshot** | Save the current project state with a label and optional notes |
| **Diff** | See exactly what changed between any two snapshots, line by line |
| **Branch** | Create isolated agent workspaces — agents work in a copy, not the real project |
| **Merge** | Line-level three-way merge back to main; auto-snapshots before writing |

Beyond the four primitives, AVC adds the trust and scale layer agent-assisted development needs: **`avc undo`** reverses the last restore or merge with zero arguments, **protected paths** mechanically block agents from touching files like CI config or secrets, **`avc verify`** audits stored history for corruption, **`avc watch`** checkpoints continuously so safety doesn't depend on an agent remembering to snapshot, **`avc bisect`** finds regressions in O(log n) test runs, **`avc timeline`** tells the story of what your agents did session by session, and **`avc merge --train`** merges a fleet of agent branches in sequence.

AVC also runs as an **MCP server** so any agent framework (Claude Code, Cursor, Windsurf) can call it directly as a tool.

📖 **Full documentation, installation guides, and API reference:** **[avc.trevarix.com](https://avc.trevarix.com)**

### Contents

- [Requirements](#requirements)
- [Quick start](#quick-start)
- [Agent integration](#agent-integration)
- [How branching works](#how-branching-works)
- [VSCode extension](#vscode-extension)
- [Development](#development)
- [Project layout](#project-layout)
- [Docs](#docs)

---

## Requirements

| Tool | Version |
|------|---------|
| Go | 1.22+ |
| Node.js | 18+ (extension only) |
| npm | 9+ (extension only) |
| VSCode | 1.85+ (extension only) |

---

## Quick start

### 1. Install

**Windows (Scoop):**

Don't have [Scoop](https://scoop.sh) yet? Install it first, in PowerShell (no admin needed):

```powershell
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
Invoke-RestMethod get.scoop.sh | Invoke-Expression
```

Then add the AVC bucket and install:

```powershell
scoop bucket add trevarix https://github.com/trevarix/scoop-bucket
scoop install avc
```

**Build from source** (any platform, requires Go 1.22+):

```bash
git clone <repo-url>
cd agentic-vc/avc
go install .
```

This builds `avc` and places it in `$GOPATH/bin` (usually `~/go/bin`). Make sure that directory is on your `PATH`.

Verify the install:

```bash
avc --help
```

> For macOS (Homebrew), Linux (direct download), and the VSCode extension, see the [full installation guide](https://avc.trevarix.com/install/).

### 2. Initialize a project

```bash
cd /path/to/your/project
avc init
```

Creates `.avc/` with a SQLite database, a default `.avcignore`, and a `config.toml`.

To wire up your agent framework at the same time:

```bash
avc init --skills claude-code
avc init --skills claude-code,cursor,windsurf
```

This writes the MCP server config and agent instruction files for each framework. See [Agent integration](#agent-integration) below.

### 3. Core commands

**Snapshots & history**

```bash
avc snapshot "Before refactor"
avc snapshot "Agent run #3" --agent "claude" --notes "Fixed the auth bug" --session sess-1 --task "add auth"
avc list                              # avc search "auth refactor" for full-text search
avc info <snapshot-id>
avc diff <from-id> <to-id>
avc restore <snapshot-id>
avc log
avc delete <snapshot-id>
avc status                            # working tree vs. last snapshot
```

**Undo, continuous checkpointing & timeline**

```bash
avc undo                              # reverse the last restore or merge, zero arguments
avc undo --list

avc watch                             # foreground daemon; debounced auto-checkpoints
avc watch --status                    # makes safety structural, not behavioral

avc timeline                          # what your agents did, grouped by session
avc timeline --session sess-1
```

**Branches (agent workspaces)**

```bash
avc branch create feature/my-task
avc branch create feature/tests --from-branch feature/my-task   # stack on another branch
avc branch list
avc branch switch main
avc branch diff feature/my-task              # cumulative diff vs. base
avc branch diff main..feature/my-task        # compare two branches' HEADs
avc branch delete feature/my-task
```

**Merge**

```bash
avc merge feature/my-task --preview   # dry-run: shows clean/merged/conflict/skipped counts
avc merge feature/my-task             # apply: auto-snapshots main first
avc merge --abort                     # undo: restores main from pre-merge snapshot
avc merge --train a b c --validate "go test ./..."   # merge a fleet in sequence
```

**Bisect** — find the snapshot that broke a command, O(log n)

```bash
avc bisect --good <snapshot-id> --cmd "go test ./..."
```

**File inspection & surgical restore**

```bash
avc annotate <file>                   # show which snapshot introduced each line
avc cat <snapshot-id> <file>          # print file content from a snapshot
avc diff-current <snapshot-id>        # diff a snapshot against the current working tree
avc file-history <file>               # list all snapshots that contain a file
avc restore-file <snapshot-id> <file> # restore a single file from a snapshot
```

**Workspace execution, integrity, storage & portability**

```bash
avc run --branch <name> <command>     # run a command inside a branch workspace

avc verify --repair                   # audit stored history for corruption
avc trash list                        # files quarantined by a restore, recoverable
avc gc --run                          # reclaim disk space from orphaned objects
avc storage                           # disk usage breakdown, compression stats
avc export --branch feature/my-task   # bundle snapshots to a .avc.tar.gz file
avc import --from bundle.avc.tar.gz

avc ui                                # standalone web UI at localhost:3004
```

All commands support `--json` for machine-readable output:

```bash
avc list --json
avc merge feature/my-task --preview --json
```

---

## Agent integration

AVC runs as an MCP (Model Context Protocol) server over stdio. Start it with:

```bash
avc mcp serve
```

### Automatic setup with `--skills`

`avc init --skills <framework>` writes the MCP config and agent instruction files for your framework:

| Framework | MCP config written | Instructions written |
|-----------|--------------------|----------------------|
| `claude-code` | `.mcp.json` (project-level) | `.claude/skills/avc-*/SKILL.md` |
| `claude-desktop` | Claude Desktop config (global, with `AVC_PROJECT` env) | `.claude/skills/avc-*/SKILL.md` |
| `cursor` | `.cursor/mcp.json` (project-level) | `.cursor/rules/avc.mdc` |
| `windsurf` | `~/.codeium/windsurf/mcp_config.json` (global) | `.windsurfrules` |
| `generic` | — | `AGENT_INSTRUCTIONS.md` |

Project-level configs are safe to commit and are auto-discovered by the framework in that project — the AVC server is never registered machine-wide for frameworks that support project scope.

Running `--skills` multiple times is safe — existing files are never overwritten, JSON configs are merged (not replaced), and rules files are append-only with a deduplication marker. If a target directory is gitignored, AVC warns you so you know the files won't be committed.

### MCP tools

Tools are exposed in three tiers (`avc mcp serve --tier core|standard|full`; `standard` is the default) so agents with small context windows aren't handed every tool at once.

<details>
<summary><strong>Full tool list (27 tools across core / standard / full)</strong></summary>
<br>

| Tool | Tier | Description |
|------|------|-------------|
| `avc_snapshot` | core | Save a snapshot (workspace-aware on agent branches; accepts `session_id`/`task`) |
| `avc_list` | core | List snapshots on the active branch |
| `avc_diff` | core | Diff two snapshots |
| `avc_restore` | core | Restore to a snapshot (workspace-aware) |
| `avc_status` | standard | Files changed since the last snapshot |
| `avc_undo` | standard | Reverse the last restore or merge, zero arguments |
| `avc_branch_create` | standard | Create a branch + auto-switch (`from_branch` to stack on another branch) |
| `avc_branch_list` | standard | List branches |
| `avc_branch_switch` | standard | Switch active branch |
| `avc_branch_diff` | standard | Cumulative diff from branch base to HEAD, or `against` another branch's HEAD |
| `avc_merge` | standard | Perform three-way merge (checks for conflicts automatically) |
| `avc_merge_abort` | standard | Abort merge and restore main |
| `avc_info` | full | Snapshot details and file list |
| `avc_delete` | full | Delete a snapshot |
| `avc_branch_rename` | full | Rename a branch |
| `avc_branch_abandon` | full | Mark a branch abandoned without deleting its history |
| `avc_branch_prune_merged` | full | Remove workspace directories for merged branches |
| `avc_merge_preview` | full | Preview a merge without writing |
| `avc_merge_train` | full | Merge several branches in sequence, with optional `--validate` rollback |
| `avc_run_in_workspace` | full | Run a sandboxed command inside a branch workspace (requires human opt-in) |
| `avc_restore_file` | full | Restore a single file from a snapshot |
| `avc_annotate` | full | Show which snapshot introduced each line of a file |
| `avc_tag_snapshot` / `avc_untag_snapshot` | full | Apply or remove a machine-readable milestone tag |
| `avc_list_conflicts` / `avc_resolve_conflict` | full | Inspect and resolve merge conflicts |
| `avc_bisect` | full | Find the snapshot that broke a command, O(log n) (requires human opt-in) |

</details>

---

## How branching works

When an agent creates a branch, AVC materializes a full copy of the project files into `.avc/workspaces/<branch-name>/`. The agent works exclusively inside that directory — the real project root is untouched until the user approves a merge.

```
avc branch create feature/add-auth   →  workspace at .avc/workspaces/feature/add-auth/
  agent edits files in workspace
  agent snapshots regularly
avc branch diff feature/add-auth     →  shows everything changed vs base snapshot
avc merge feature/add-auth --preview →  shows clean / merged / conflict / skipped per file
avc merge feature/add-auth           →  applies clean + line-merged files; writes conflict markers
avc merge --abort                    →  restores main from pre-merge auto-snapshot
```

Conflicting edits are resolved line-by-line, not file-by-file: if two branches (or a branch and main) touch different regions of the same file, AVC combines both edits automatically — only genuinely overlapping lines produce a conflict marker.

Branches can also stack on each other (`avc branch create <name> --from-branch <parent>`), and several branches can be merged in one pass with `avc merge --train a b c` — each merge sees the ones before it, and the train stops (leaving completed merges in place) at the first conflict or failed `--validate` run.

If `[protect]` is configured in `.avc/config.toml`, a merge that would touch a protected path (CI config, secrets, etc.) is refused mechanically — an agent cannot override it; only a human running `avc merge --allow-protected` can.

---

## VSCode extension

### Features

**Snapshots**
- Snapshot list in sidebar (newest first, per active branch)
- Save, restore, and delete snapshots from the UI
- Filter snapshots by label, agent name, or date
- Compare any two snapshots side-by-side (not just adjacent ones)
- Detailed snapshot info viewer (file list, metadata)
- Auto-snapshot on file save (configurable, debounced)
- Continuous checkpointing: `avc.watch.enabled` runs the `avc watch` daemon alongside the editor, superseding save-triggered snapshots

**Working tree awareness**
- Status bar change indicator showing `+added ~modified -deleted` since last snapshot — click to view diff
- Diff any snapshot against the current working tree
- Quick diff: latest snapshot vs. working tree (one click from status bar)
- Gutter annotations — blame-style overlay showing which snapshot introduced each line; toggle on/off

**File-level operations**
- Restore a single file from a snapshot without affecting the rest of the project
- Open any file from any snapshot in a read-only editor tab
- Diff a single file between a snapshot and the current working tree
- File history viewer — see every snapshot that touched a given file

**Branches**
- Branch status bar item — click to switch branches
- Create branch, switch branch, delete branch commands
- Open a branch workspace folder in a new VSCode window
- Merge branch to main with preview modal and abort support

---

## Development

### CLI

```bash
cd avc
go mod tidy
go build .          # build binary in place
go install .        # install to $GOPATH/bin
go test ./...       # run all tests
```

### Extension

```bash
cd extension
npm install
npm run compile     # one-shot build
npm run watch       # rebuild on save
```

---

## Project layout

<details>
<summary><strong>Expand full tree</strong></summary>

```
avc/
  main.go
  cmd/avc/               # one file per CLI subcommand (thin — parse flags, call internal/)
  internal/
    db/                  # SQLite schema, migrations, all CRUD
    fileutil/            # SHA256 hashing, directory walk, .avcignore
    snapshot/            # snapshot creation; heuristic change summaries
    restore/             # object store read-back, file write
    objstore/            # content-addressed object store (zstd compression, format v2)
    diff/                # two-snapshot comparison, unified diff, change summaries
    branch/              # branch CRUD, workspace materialization, stacked branches
    merge/               # line-level three-way merge (diff3), merge trains
    policy/              # [protect] path enforcement
    oplog/, undo/        # operations log; zero-argument undo/redo
    trash/               # quarantine for files a restore would otherwise delete
    fsck/                # object store integrity verification (avc verify)
    watch/               # avc watch — debounced continuous checkpointing daemon
    bisect/              # avc bisect — binary search for a breaking snapshot
    timeline/            # avc timeline — session-grouped history report
    workspace/           # sandboxed command runner for avc run / avc bisect / --validate
    retention/           # automatic snapshot pruning policy
    mcp/                 # MCP JSON-RPC server, tool registry, handlers
    skills/              # writes MCP configs and agent instruction files
    web/                 # standalone web UI server (avc ui)
    api/                 # shared operation wrappers used by the CLI and web server
    config/              # .avc/config.toml read/write
    statcache/           # mtime+size cache to skip unchanged files
  tests/                 # integration and cross-package tests
extension/src/           # TypeScript — extension, sidebar, diff viewer, CLI proxy, watch manager
docs/                    # architecture, CLI reference, contributing guide
```

</details>

---

## Docs

- **[Full documentation](https://avc.trevarix.com)** — detailed guides and installation steps
- [Architecture](docs/architecture.md)
- [CLI Reference](docs/cli-reference.md)
- [Contributing](docs/contributing.md)
- [Project Description](docs/project-description.md)

---

## License

This project is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0).
See the [LICENSE](LICENSE) file for the full text.

Copyright (c) 2026 TREVARIX Corp.
