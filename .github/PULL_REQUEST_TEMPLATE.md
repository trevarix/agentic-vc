## Summary

<!-- What problem does this PR solve? Why this approach? -->

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Refactor (no behavior change)
- [ ] Docs / tests only

## What changed

<!-- Brief description of the implementation. Call out any non-obvious decisions. -->

---

## CLI changes

_Skip if no CLI changes._

- [ ] New command added to `avc/cmd/avc/<name>.go` and registered in `root.go`
- [ ] Logic lives in `avc/internal/` — nothing in `cmd/avc/` beyond flag parsing and output
- [ ] `--json` flag supported; output is valid JSON on stdout, errors on stderr with exit code 1
- [ ] Integration test added in `avc/tests/`
- [ ] README.md and `docs/cli-reference.md` updated

## MCP changes

_Skip if no MCP changes._

- [ ] Tool entry added to `AllTools()` in `avc/internal/mcp/tools.go`
- [ ] Handler added and registered in `avc/internal/mcp/handlers.go`
- [ ] MCP tools table in README.md updated
- [ ] Integration test exercises the tool via the JSON-RPC server

## Extension changes

_Skip if no extension changes._

- [ ] All CLI calls go through `cliProxy.ts` — no direct `.avc/` access
- [ ] Extension compiles: `npm run compile` passes
- [ ] Manually tested in Extension Development Host

---

## Tests

- [ ] `go test ./...` passes (run from `avc/`)
- [ ] `go vet ./...` clean
- [ ] `gofmt` applied (CI will reject unformatted code)

## Docs

- [ ] Docs updated in this same PR (not deferred to a follow-up)
- [ ] No new `TODO` or `FIXME` left in code

## Notes for reviewer

<!-- Anything to call attention to: trade-offs, what to stress-test, known gaps -->
