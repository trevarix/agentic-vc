# AVC — Agentic Version Control

A local version control system built for the agent era. AVC gives agents and users four primitives to work safely — **snapshot**, **diff**, **branch**, and **merge** — without the complexity of Git.

| Primitive | What it does |
|-----------|-------------|
| **Snapshot** | Save the current project state with a label and optional notes |
| **Diff** | See exactly what changed between any two snapshots, line by line |
| **Branch** | Create isolated agent workspaces — agents work in a copy, not the real project |
| **Merge** | Three-way merge branch changes back to main; auto-snapshots before writing |

AVC also runs as an **MCP server** so any agent framework (Claude Code, Cursor, Windsurf) can call it directly as a tool.

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

```bash
git clone <repo-url>
cd agentic-vc/avc
go install .
```

This builds `avc` and places it in `$GOPATH/bin` (usually `~/go/bin`). Make sure that directory is on your `PATH`.

```bash
avc --help
```

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

```bash
# Snapshots
avc snapshot "Before refactor"
avc snapshot "Agent run #3" --agent "claude" --notes "Fixed the auth bug"
avc list
avc info <snapshot-id>
avc diff <from-id> <to-id>
avc restore <snapshot-id>
avc log
avc delete <snapshot-id>

# Branches (agent workspaces)
avc branch create feature/my-task
avc branch list
avc branch switch main
avc branch diff feature/my-task
avc branch delete feature/my-task

# Merge
avc merge feature/my-task --preview   # dry-run: shows clean/conflict/skipped counts
avc merge feature/my-task             # apply: auto-snapshots main first
avc merge --abort                     # undo: restores main from pre-merge snapshot
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
| `claude-code` | `.claude/settings.json` | `.claude/skills/avc-*/SKILL.md` |
| `cursor` | `.cursor/mcp.json` | `.cursor/rules/avc.mdc` |
| `windsurf` | `.codeium/windsurf/mcp_config.json` | `.windsurfrules` |
| `generic` | — | `AGENT_INSTRUCTIONS.md` |

Running `--skills` multiple times is safe — existing files are never overwritten, JSON configs are merged (not replaced), and rules files are append-only with a deduplication marker. If a target directory is gitignored, AVC warns you so you know the files won't be committed.

### MCP tools

| Tool | Description |
|------|-------------|
| `avc_snapshot` | Save a snapshot (workspace-aware on agent branches) |
| `avc_list` | List snapshots on the active branch |
| `avc_diff` | Diff two snapshots |
| `avc_restore` | Restore to a snapshot (workspace-aware) |
| `avc_info` | Snapshot details and file list |
| `avc_delete` | Delete a snapshot |
| `avc_branch_create` | Create a branch + auto-switch |
| `avc_branch_list` | List branches |
| `avc_branch_switch` | Switch active branch |
| `avc_branch_diff` | Cumulative diff from branch base to HEAD |
| `avc_merge_preview` | Preview a merge without writing |
| `avc_merge` | Perform three-way merge |
| `avc_merge_abort` | Abort merge and restore main |

---

## How branching works

When an agent creates a branch, AVC materializes a full copy of the project files into `.avc/workspaces/<branch-name>/`. The agent works exclusively inside that directory — the real project root is untouched until the user approves a merge.

```
avc branch create feature/add-auth   →  workspace at .avc/workspaces/feature/add-auth/
  agent edits files in workspace
  agent snapshots regularly
avc branch diff feature/add-auth     →  shows everything changed vs base snapshot
avc merge feature/add-auth --preview →  shows clean / conflict / skipped per file
avc merge feature/add-auth           →  applies clean files; writes conflict markers
avc merge --abort                    →  restores main from pre-merge auto-snapshot
```

---

## VSCode extension

### Setup

```bash
cd extension
npm install
code .
```

Press **F5** to launch the Extension Development Host. Open an `avc init`-ed folder — the AVC sidebar appears in the activity bar.

If `avc` is not on `PATH` in the dev host, set it explicitly:

```json
"avc.cliPath": "/Users/you/go/bin/avc"
```

### Features

- Snapshot list in sidebar (newest first, per active branch)
- Save / restore / delete snapshots from the UI
- Branch status bar item — click to switch branches
- Create branch, switch branch, delete branch commands
- Merge branch to main with preview modal and abort support
- Diff viewer for comparing snapshots

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

```
avc/
  main.go
  cmd/avc/               # one file per CLI subcommand (thin — parse flags, call internal/)
  internal/
    db/                  # SQLite schema, migrations, all CRUD
    fileutil/            # SHA256 hashing, directory walk, .avcignore
    snapshot/            # snapshot creation
    restore/             # object store read-back, file write
    diff/                # two-snapshot comparison, unified diff
    branch/              # branch CRUD, workspace materialization
    merge/               # three-way merge engine
    mcp/                 # MCP JSON-RPC server, tool registry, handlers
    skills/              # writes MCP configs and agent instruction files
    config/              # .avc/config.toml read/write
    statcache/           # mtime+size cache to skip unchanged files
  tests/                 # integration and cross-package tests
extension/src/           # TypeScript — extension, sidebar, diff viewer, CLI proxy
docs/                    # architecture, CLI reference, contributing guide
```

---

## Windows note

Binaries built with `go install` are placed in `%USERPROFILE%\go\bin\`. If Windows Smart App Control blocks the binary:

- Disable Smart App Control in **Windows Security → App & browser control → Smart App Control settings** (recommended for developer machines)
- Or use `go run . <command>` during development instead of the installed binary

---

## Docs

- [Architecture](docs/architecture.md)
- [CLI Reference](docs/cli-reference.md)
- [Contributing](docs/contributing.md)
- [Project Description](docs/project-description.md)
