# Contributing to AVC

## Prerequisites

- Go 1.22+
- Node.js 20+ and npm (for the VSCode extension)
- VSCode with the Extension Development Host (for extension work)

---

## Project layout

```
agentic_version_ctl/
├── main.go                  # CLI entry point
├── cmd/avc/                 # One file per CLI subcommand
├── internal/                # Core engine — not exported
│   ├── db/                  # SQLite schema and CRUD
│   ├── snapshot/            # Snapshot creation logic
│   ├── restore/             # File restoration logic
│   ├── diff/                # Snapshot comparison
│   ├── fileutil/            # Hashing, walking, ignore rules
│   └── config/              # config.toml parsing
├── tests/                   # Integration and cross-package tests
├── extension/               # VSCode extension (TypeScript)
│   └── src/
│       ├── extension.ts     # Activation, command registration
│       ├── sidebar.ts       # TreeDataProvider
│       ├── diffViewer.ts    # Webview diff renderer
│       └── cliProxy.ts      # Typed CLI wrappers
├── examples/                # Usage scripts
└── docs/                    # This directory
```

---

## Building the CLI

```bash
# Download dependencies
go mod tidy

# Build the binary
go build -o avc .

# Run directly without building
go run . snapshot "test"
```

Cross-compile for other platforms:
```bash
GOOS=windows GOARCH=amd64 go build -o avc.exe .
GOOS=darwin  GOARCH=arm64 go build -o avc-mac .
GOOS=linux   GOARCH=amd64 go build -o avc-linux .
```

---

## Running tests

```bash
# All tests
go test ./...

# Specific package
go test ./internal/db/...
go test ./tests/... -v

# With race detector
go test -race ./...
```

Tests in `tests/` use `t.TempDir()` for isolation — no setup or teardown needed.

---

## Building the VSCode extension

```bash
cd extension
npm install
npm run compile        # one-off build
npm run watch          # watch mode during development
```

To launch the Extension Development Host in VSCode: open `extension/` and press `F5`.

To package for distribution:
```bash
npm install -g @vscode/vsce
vsce package           # produces avc-<version>.vsix
```

---

## Adding a new CLI command

1. Create `cmd/avc/<name>.go` with a `var <name>Cmd = &cobra.Command{…}`.
2. Register it in `cmd/avc/root.go`: `rootCmd.AddCommand(<name>Cmd)`.
3. Add any new internal logic to the appropriate `internal/` package (or a new one).
4. Add tests in `tests/<name>_test.go`.
5. Document the command in [cli-reference.md](cli-reference.md).

---

## Code conventions

**Go**
- `gofmt` is enforced — run before committing.
- Keep `cmd/` files thin: parse flags, call `internal/`, format output.
- All `internal/` functions return errors; callers in `cmd/` handle them.
- No global state outside `cmd/avc/root.go` (the Cobra flags).

**TypeScript**
- Strict mode is enabled (`tsconfig.json`).
- All CLI calls go through `cliProxy.ts` — never `execFile` directly in other files.
- UI messages follow the pattern `AVC: <What happened>` for discoverability.

---

## Commit style

```
<type>: <short description>

<optional longer explanation>
```

Types: `feat` · `fix` · `refactor` · `test` · `docs` · `chore`

Examples:
```
feat: add avc delete command
fix: exclude .avc/ directory from snapshot walk
docs: add CLI reference for avc diff
```

---

## Reporting issues

Open an issue describing:
1. What you did
2. What you expected
3. What actually happened (include CLI output and error messages)
4. OS and `avc --version` output
