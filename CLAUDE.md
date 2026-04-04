# AVC — Claude Code Instructions

## What this project is

Agentic Version Control (AVC) is a local version control system built for the agent era. It delivers four primitives that make agent-assisted development safe: **commit** (snapshots), **diff**, **branch** (agent workspaces), and **merge** (controlled integration). It is not a Git wrapper — it is independent.

Full project context: [docs/project-description.md](docs/project-description.md)  
Architecture details: [docs/architecture.md](docs/architecture.md)  
CLI commands: [docs/cli-reference.md](docs/cli-reference.md)

---

## Tech stack

| Layer | Technology |
|-------|-----------|
| CLI & core engine | Go 1.22+, Cobra, `modernc.org/sqlite` (pure Go, no CGO), BurntSushi/toml |
| VSCode extension | TypeScript, VSCode API, Webview |
| Database | SQLite via `.avc/avc.db` |
| File storage | Content-addressed blobs in `.avc/objects/<2-char-shard>/<62-char-hash>` |

---

## Build & test

```bash
# CLI
go mod tidy
go build -o avc .
go test ./...
go test -race ./...

# Extension
cd extension && npm install && npm run compile
```

---

## Project layout

```
main.go                  # entry point — delegates to cmd/avc
cmd/avc/                 # one file per CLI subcommand; thin — parse flags, call internal/, format output
internal/
  db/         # SQLite schema, migrations, all CRUD
  fileutil/   # SHA256 hashing, directory walk, .avcignore
  snapshot/   # orchestrates snapshot creation
  restore/    # reads object store, writes files back to disk
  diff/       # compares two snapshots; returns FileDiff list
  config/     # reads/writes .avc/config.toml
tests/                   # integration and cross-package tests
extension/src/           # TypeScript — extension.ts, sidebar.ts, diffViewer.ts, cliProxy.ts
docs/                    # architecture.md, cli-reference.md, contributing.md, project-description.md
```

---

## Architecture rules — always follow these

1. **CLI-first.** All logic lives in `internal/`. `cmd/avc/` files only parse flags, call `internal/`, and format output. Never put business logic in a command file.

2. **Extension talks to CLI only.** The VSCode extension never touches `.avc/` directly. All data flows through `cliProxy.ts` → `execFile` → `avc --json`.

3. **Content-addressed objects.** File blobs are stored in `.avc/objects/<hash[:2]>/<hash[2:]>`. Objects are write-once. Never modify a stored object. Identical files across snapshots share one object automatically.

4. **SQLite for metadata, objects for content.** The DB holds no file bytes — only hashes, sizes, and relational metadata. File content lives only in the object store.

5. **`--json` on every command.** Every CLI command must support `--json` and emit valid JSON to stdout. Errors go to stderr; exit code 1 on any failure.

6. **Errors propagate up.** `internal/` functions return errors. `cmd/` files handle them. Never swallow errors silently.

---

## Phase boundaries — do not cross

| Phase | Scope |
|-------|-------|
| **1 (current)** | `snapshot`, `list`, `diff`, `restore`, `info`, `init` — linear history on implicit `main` branch |
| **2** | VSCode extension sidebar + restore UI |
| **3** | Diff viewer Webview |
| **4** | `avc branch` commands — branches table, agent workspaces |
| **5** | `avc merge` — clean merge, conflict detection, auto-snapshot before merge |
| **6** | Polish, tests, release |

Do not add branching or merging logic to Phase 1 code. If a change is Phase 4+ work, note it and defer it.

---

## Code conventions

**Go**
- `gofmt` always
- Package names are short nouns: `db`, `snapshot`, `diff`, `fileutil`
- No global state except Cobra flags in `cmd/avc/root.go`
- Test helpers live in `tests/` and use `t.TempDir()` for isolation

**TypeScript**
- Strict mode on
- All `execFile` calls go through `cliProxy.ts` — nowhere else
- UI messages: `AVC: <What happened>` prefix for discoverability
- No direct filesystem access from the extension

**Both**
- No magic literals inline — assign to a named constant first
- No dead code, no commented-out blocks
- One concern per function