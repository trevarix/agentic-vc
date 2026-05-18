# Contributing to AVC — Deep Dive

This document covers architecture rules, the internal package map, and conventions for non-trivial changes. For the quick-start (prereqs, build, test, PR rules), see the root [CONTRIBUTING.md](../CONTRIBUTING.md).

---

## Architecture rules — always follow these

1. **CLI-first.** All logic lives in `avc/internal/`. `avc/cmd/avc/` files only parse flags, call `internal/`, and format output. Never put business logic in a command file.

2. **Extension talks to CLI only.** The VSCode extension never touches `.avc/` directly. All data flows through `cliProxy.ts` → `execFile` → `avc --json`.

3. **Content-addressed objects.** File blobs are stored in `.avc/objects/<hash[:2]>/<hash[2:]>`. Objects are write-once. Identical files across snapshots share one object automatically.

4. **SQLite for metadata, objects for content.** The DB holds no file bytes — only hashes, sizes, and relational metadata. File content lives only in the object store.

5. **`--json` on every command.** Every CLI command must support `--json` and emit valid JSON to stdout. Errors go to stderr; exit code 1 on any failure.

6. **Errors propagate up.** `internal/` functions return errors. `cmd/` files handle them. Never swallow errors silently.

7. **Workspace isolation.** On a non-main branch, snapshots walk `.avc/workspaces/<branch>/` as source and restore targets that directory — never the real project root. `branch.WorkspacePath` returns `""` for main (use project root).

8. **One DB connection per operation.** Never hold a DB connection open across multiple logical operations. Open, do work, close. The merge engine uses three separate open/close phases to avoid lock contention.

---

## Project layout

```
avc/
  main.go                # entry point — delegates to cmd/avc
  cmd/avc/               # one file per CLI subcommand; thin — parse flags, call internal/, format output
  internal/
    db/          # SQLite schema, migrations, all CRUD (snapshots, branches, merges)
    fileutil/    # SHA256 hashing, directory walk, .avcignore parsing
    snapshot/    # orchestrates snapshot creation; workspace-aware source dir
    restore/     # reads object store, writes files back to disk; RestoreToDir for workspaces
    diff/        # compares two snapshots; LCS line counting; unified diff preview
    branch/      # branch CRUD, workspace materialization, active branch tracking
    merge/       # three-way merge engine; Preview, Merge, Abort
    mcp/         # MCP JSON-RPC 2.0 server, tool registry, all tool handlers
    skills/      # writes MCP configs and agent instruction files per framework
    config/      # reads/writes .avc/config.toml; active branch name
    statcache/   # mtime+size cache to skip re-hashing unchanged files
    annotate/    # line-blame: traces each file line to its originating snapshot
    workspace/   # sandboxed command runner; env scrubbing, venv isolation, process tree kill
    web/         # standalone web UI server
  tests/                 # integration and cross-package tests
extension/src/           # TypeScript — extension.ts, sidebar.ts, diffViewer.ts, cliProxy.ts, etc.
docs/                    # architecture, CLI reference, contributing, project description, design docs
```

---

## Building the CLI in detail

```bash
cd avc

# Download dependencies
go mod tidy

# Build the binary in place
go build -o avc .

# Install to $GOPATH/bin
go install .

# Run directly without building
go run . snapshot "test"
```

Cross-compile:
```bash
GOOS=windows GOARCH=amd64 go build -o avc.exe .
GOOS=darwin  GOARCH=arm64 go build -o avc-mac .
GOOS=linux   GOARCH=amd64 go build -o avc-linux .
```

---

## Running tests

```bash
cd avc

go test ./...                  # all tests
go test ./internal/db/...      # one package
go test ./tests/... -v         # integration tests, verbose
go test -race ./...            # race detector (requires CGO)
```

Tests in `avc/tests/` use `t.TempDir()` for isolation — no setup or teardown.

---

## Building the VSCode extension

```bash
cd extension
npm install
npm run compile        # one-off build
npm run watch          # watch mode during development
```

Launch the Extension Development Host: open `extension/` in VSCode and press `F5`.

Package for distribution:
```bash
npm install -g @vscode/vsce
vsce package           # produces avc-<version>.vsix
```

---

## Adding a new CLI command

1. Create `avc/cmd/avc/<name>.go` with `var <name>Cmd = &cobra.Command{…}`.
2. Register it in `avc/cmd/avc/root.go`: `rootCmd.AddCommand(<name>Cmd)`.
3. Put logic in the appropriate `internal/` package (or a new one).
4. Implement `--json` output — check the `jsonOutput` flag from `root.go`.
5. Add an integration test in `avc/tests/<name>_test.go`.
6. Document in [cli-reference.md](cli-reference.md) and the root [README.md](../README.md).

---

## Adding a new MCP tool

1. Add a `Tool` entry to `AllTools()` in [../avc/internal/mcp/tools.go](../avc/internal/mcp/tools.go). Include name, description (be specific — agents read this), and input schema.
2. Add a handler in `avc/internal/mcp/handlers.go` that:
   - Validates inputs
   - Calls into the relevant `internal/` package
   - Returns a JSON-serializable result
3. Register the handler in the dispatch table.
4. Add an integration test that exercises the tool through the JSON-RPC server.
5. Update the MCP tools table in the root [README.md](../README.md).

Tool descriptions are the only documentation an agent sees at runtime. Be precise about preconditions, side effects, and the exact shape of the response.

---

## Extension internals

- `extension.ts` — activation, command registration, status bar items
- `sidebar.ts` — `TreeDataProvider` for the snapshot list
- `cliProxy.ts` — every `execFile` call to `avc` lives here; nowhere else
- `diffViewer.ts` — webview-based diff renderer
- `infoViewer.ts` — snapshot detail webview
- `timelineViewer.ts` — chronological snapshot view
- `fileHistoryViewer.ts` — per-file snapshot history
- `gutterAnnotations.ts` — blame-style overlay using `avc annotate`
- `snapshotContentProvider.ts` — `TextDocumentContentProvider` for the `avc-snapshot://` URI scheme
- `autoSnapshot.ts` — debounced auto-snapshot on file save
- `scmProvider.ts` — VSCode SCM panel integration
- `webviewUtil.ts` — shared webview helpers

---

## Code conventions

**Go**
- `gofmt` is enforced — run before committing.
- Keep `cmd/` files thin: parse flags, call `internal/`, format output.
- All `internal/` functions return errors; callers in `cmd/` handle them.
- No global state outside `cmd/avc/root.go` (the Cobra flags).
- Package names are short nouns: `db`, `snapshot`, `diff`, `fileutil`.

**TypeScript**
- Strict mode is enabled (`tsconfig.json`).
- All CLI calls go through `cliProxy.ts` — never `execFile` directly elsewhere.
- UI messages follow the pattern `AVC: <What happened>` for discoverability.
- No direct filesystem access from the extension.

**Both**
- No magic literals inline — assign to a named constant.
- No dead code, no commented-out blocks.
- One concern per function.

---

## Commit style

```
<type>: <short description>
```

Types: `feat` · `fix` · `refactor` · `test` · `docs` · `chore`

Examples:
```
feat: add avc annotate command
fix: exclude .avc/ directory from snapshot walk
docs: clarify branch workspace lifecycle
```
