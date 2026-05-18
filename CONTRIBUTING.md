# Contributing to AVC

Thanks for your interest in contributing. This is the quick-start. For the full architecture and conventions, see [docs/contributing.md](docs/contributing.md).

---

## Prerequisites

- Go 1.22+
- Node.js 20+ and npm (extension only)
- VSCode with the Extension Development Host (extension work only)

---

## Build and test

```bash
# CLI
cd avc
go mod tidy
go build -o avc .
go test ./...

# Extension
cd extension
npm install
npm run compile
```

> `go test -race` requires CGO. Omit `-race` on Windows builds without CGO enabled.

Run `gofmt` and `go vet ./...` before committing. CI will reject anything that isn't formatted.

---

## Pull request conventions

- **One concern per PR.** Doc changes, refactors, and feature work go in separate PRs.
- **Squash-merge.** Keep the final commit message under 70 characters in the subject; use the body for the why.
- **`--json` parity.** Every new CLI command must support `--json` and emit valid JSON on stdout. See existing commands in [avc/cmd/avc/](avc/cmd/avc/) for the pattern.
- **Tests required.** New commands need an integration test in [avc/tests/](avc/tests/). New `internal/` packages need their own `*_test.go`.
- **Update docs in the same PR.** If you add a command, update [README.md](README.md) and [docs/cli-reference.md](docs/cli-reference.md). If you add an MCP tool, update the README MCP table.

---

## Adding a new CLI command

1. Create `avc/cmd/avc/<name>.go` with `var <name>Cmd = &cobra.Command{…}`.
2. Register it in [avc/cmd/avc/root.go](avc/cmd/avc/root.go) via `rootCmd.AddCommand(<name>Cmd)`.
3. Put the logic in the appropriate package under `avc/internal/` — never in the `cmd/` file.
4. Implement `--json` output (check `jsonOutput` flag from root).
5. Add an integration test in `avc/tests/<name>_test.go`.
6. Document the command in [README.md](README.md) and [docs/cli-reference.md](docs/cli-reference.md).

---

## Adding a new MCP tool

1. Add a `Tool` entry to `AllTools()` in [avc/internal/mcp/tools.go](avc/internal/mcp/tools.go) — name, description, input schema.
2. Add a handler in [avc/internal/mcp/handlers.go](avc/internal/mcp/handlers.go) that calls into the relevant `internal/` package.
3. Register the handler in the dispatch table.
4. Add the tool to the MCP tools table in [README.md](README.md).
5. Add an integration test exercising the tool through the JSON-RPC server.

---

## Commit style

```
<type>: <short description>
```

Types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`.

Examples:
```
feat: add avc annotate command
fix: exclude .avc/ directory from snapshot walk
docs: clarify branch workspace lifecycle
```

---

## Reporting issues

Open an issue with:
1. What you did
2. What you expected
3. What actually happened (include CLI output)
4. OS and `avc --version` output

---

## More

- [docs/contributing.md](docs/contributing.md) — architecture rules, internal package map, extension internals
- [docs/architecture.md](docs/architecture.md) — system design and storage model
- [docs/cli-reference.md](docs/cli-reference.md) — full CLI documentation
