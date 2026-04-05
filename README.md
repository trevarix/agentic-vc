# AVC — Agentic Version Control

A local version control system built for the agent era. AVC gives agents and users four primitives to work safely: **snapshot**, **diff**, **restore**, and (coming) **branch** and **merge** — without the complexity of Git.

- **Snapshot** — save the current state of your project with a label and optional notes
- **Diff** — see exactly what changed between any two snapshots, line by line
- **Restore** — roll back to any previous snapshot instantly
- **Branch / Merge** — agent workspaces and controlled integration (Phase 4–5)

---

## Requirements

| Tool | Version |
|------|---------|
| Go | 1.22+ |
| Node.js | 18+ |
| npm | 9+ |
| VSCode | 1.85+ |

---

## Running the CLI locally

### 1. Clone and install dependencies

```bash
git clone <repo-url>
cd agentic-vc
go mod tidy
```

### 2. Install the binary

```bash
go install .
```

This builds `avc` and puts it in `$GOPATH/bin` (usually `~/go/bin`). Make sure that directory is on your `PATH`.

Verify it works:

```bash
avc --help
```

### 3. Initialize a project

```bash
cd /path/to/your/project
avc init
```

This creates `.avc/` with a SQLite database and a default `.avcignore`.

### 4. Common commands

```bash
# Save a snapshot
avc snapshot "Before refactor"
avc snapshot "Agent run #3" --agent "claude" --notes "Fixed the auth bug"

# List snapshots
avc list

# See what changed between two snapshots
avc diff snap-abc123 snap-def456

# Restore to a previous snapshot
avc restore snap-abc123

# Show snapshot details and file list
avc info snap-abc123

# Delete a snapshot
avc delete snap-abc123
```

All commands support `--json` for machine-readable output:

```bash
avc list --json
avc snapshot "WIP" --json
```

---

## Running the VSCode extension locally

### 1. Install extension dependencies

```bash
cd extension
npm install
```

### 2. Open the extension folder in VSCode

```bash
code extension/
```

### 3. Launch the Extension Development Host

Press **F5** inside VSCode. This compiles the TypeScript and opens a second VSCode window with the AVC extension loaded.

> If prompted to select a debug configuration, choose **Run Extension**.

### 4. Open an AVC-initialized project

In the Extension Development Host window, open a folder that has been initialized with `avc init`. The AVC sidebar will appear in the activity bar (camera icon).

### 5. Configure the CLI path (if needed)

If `avc` is not on the system `PATH` in the dev host, set it explicitly in VSCode settings:

```json
"avc.cliPath": "/Users/you/go/bin/avc"
```

---

## Development workflow

### CLI changes

After editing Go source files, reinstall the binary:

```bash
go install .
```

### Extension changes

The extension compiles on `F5`. For live recompilation while editing:

```bash
cd extension
npm run watch
```

Then reload the Extension Development Host window (`Ctrl+Shift+P` → **Developer: Reload Window**).

### Run tests

```bash
# Go tests
go test ./...

# TypeScript compile check
cd extension && npm run compile
```

---

## Project layout

```
main.go                  # entry point
cmd/avc/                 # one file per CLI subcommand
internal/
  db/                    # SQLite schema and CRUD
  fileutil/              # SHA256 hashing, directory walk, .avcignore
  snapshot/              # snapshot creation
  restore/               # object store read-back and file write
  diff/                  # two-snapshot comparison
  config/                # .avc/config.toml read/write
tests/                   # integration and cross-package tests
extension/src/           # TypeScript — extension, sidebar, diff viewer, CLI proxy
docs/                    # architecture, CLI reference, contributing guide
```

---

## Windows note

AVC binaries built with `go install` are placed in `%USERPROFILE%\go\bin\`. If Windows Smart App Control blocks the binary, either:

- Turn off Smart App Control in **Windows Security → App & browser control → Smart App Control settings** (recommended for developer machines)
- Or use `go run . <command>` during development instead of the installed binary

---

## Docs

- [Architecture](docs/architecture.md)
- [CLI Reference](docs/cli-reference.md)
- [Contributing](docs/contributing.md)
