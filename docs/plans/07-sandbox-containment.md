# Plan 07 — OS-Level Sandbox Containment (optional)

**Covers:** the workspace runner's missing security boundary (review §2.10 context; runner assessment 2026-07-09)
**Goal:** Upgrade `internal/workspace` from a hygiene layer to real containment: commands run in `avc_run_in_workspace`, `avc bisect`, and `avc merge --train --validate` cannot write outside their workspace, reach the network without permission, or exhaust the host — enforced by the OS, not by string inspection.
**Prerequisites:** Plans 01–03 (specifically 03·5j/5k). Recommended before Plan 05's B2/B4 see heavy use on code the user has not reviewed.
**Status: conditional.** Build this when either trigger fires; until then, 03·5k's documented limits are the honest posture.

> **Triggers.** (1) Users run third-party / dependency test suites through the runner (post-`npm install`, anything in `node_modules` or site-packages executes with full user authority today). (2) Fleet automation (B4 merge train with `--validate`, scheduled B2 bisects) runs commands against branches no human has reviewed. Either one means prompt-injected or supply-chain-poisoned code gets a mechanical execution channel — policy layers don't hold there.

**Estimated duration:** ~3–4 weeks (phases land independently; each is useful alone)

---

## Design principles

1. **Containment supersedes classification.** With a real boundary, `classify()`'s first-token blocklist stops being load-bearing and becomes UX (friendly errors for obviously-wrong commands like `sudo`). No more blocklist-expansion whack-a-mole.
2. **Honest degradation, never silent.** Every run reports the containment actually applied. `strict` mode fails closed when the platform can't deliver; `auto` degrades with a warning; `off` restores today's behavior.
3. **One interface, per-platform backends.** All platform code behind build tags in one package; the runner itself stays platform-agnostic.
4. **Workspace-writable, host-readable, network-off.** The default profile on every platform: read the OS and toolchains, write only the workspace (+ its venv/caches + a private tmp), no network unless granted.

### Configuration

```toml
[run]
enabled  = true          # existing human gate — unchanged, still required
sandbox  = "auto"        # auto | strict | off
network  = "deny"        # deny | allow
```

- `auto` — strongest available backend, warn on degradation (default).
- `strict` — refuse to run if full filesystem+network containment is unavailable.
- `network = "deny"` breaks `pip install`/`npm install` by design; the error message must say to set `network = "allow"` (a human config edit — same trust model as `run.enabled`). No per-call override from MCP: an agent must not be able to grant itself network.

---

## Phase 1 — Launcher interface + capability probing (Effort: M)

**Files:** new `avc/internal/sandbox/sandbox.go`, `avc/internal/workspace/runner.go`

```go
// Spec describes the containment contract for one command execution.
type Spec struct {
    WorkspacePath string   // rw root
    ExtraWritable []string // venv, .gomodcache, private tmp
    Network       bool
    TimeoutSeconds int
    MaxMemoryMB   int      // 0 = platform default
    MaxProcesses  int      // fork-bomb guard
}

// Launcher wraps an exec.Cmd with platform containment.
type Launcher interface {
    Name() string                       // "bubblewrap", "sandbox-exec", "jobobject", "none"
    Capabilities() Caps                 // {FSIsolation, NetIsolation, ResourceLimits bool}
    Wrap(spec Spec, cmd *exec.Cmd) (*exec.Cmd, cleanup func(), err error)
}

// Detect probes the host and returns launchers strongest-first.
func Detect() []Launcher
```

The runner picks the first launcher satisfying the config mode; `RunResult.SandboxInfo` is extended to report `backend`, `fs_isolation`, `net_isolation`, `resource_limits` truthfully (replacing today's always-`true` booleans, which 03·5k already flags as misleading). Add `avc run doctor` printing what `Detect()` finds and why (`bwrap: not found`, `userns: disabled by sysctl`, …) — this makes support tractable.

Ship with only the `none` launcher wired so the interface, config plumbing, doctor output, and strict-mode refusal are testable on every platform before any OS code lands.

---

## Phase 2 — Linux backend (Effort: L)

**Files:** `avc/internal/sandbox/linux_bwrap.go`, `linux_ns.go` (build tag `linux`)

**Primary: bubblewrap** (`bwrap`), if on PATH — battle-tested (Flatpak), unprivileged, exactly this use case:

```
bwrap --ro-bind / / \
      --bind <workspace> <workspace> \
      --bind <extraWritable…> … \
      --tmpfs /tmp --tmpfs /home \
      --dev /dev --proc /proc \
      --unshare-pid --unshare-ipc [--unshare-net unless Network] \
      --die-with-parent \
      -- sh -c <command>
```

`--die-with-parent` also solves the daemonized-child leak the group-kill misses today. Env scrubbing (Plan 03-era `buildEnv`) stays — defense in depth; `--tmpfs /home` makes the HOME redirect real instead of advisory.

**Fallback: raw namespaces** via self-re-exec (`avc __sandbox-helper`, hidden command): `CLONE_NEWNS|NEWPID|NEWIPC` (+`NEWNET` for deny) with unprivileged user namespaces where the distro allows. Mount setup mirrors the bwrap profile. If userns is disabled → this launcher reports unavailable; `auto` degrades, `strict` refuses.

**Resource limits:** cgroups v2 (`memory.max`, `pids.max`) when a delegated slice is writable; else `RLIMIT_AS`/`RLIMIT_NPROC`/`RLIMIT_FSIZE` via the helper. Fork-bomb and runaway-memory tests included.

---

## Phase 3 — Windows backend (Effort: L)

**Files:** `avc/internal/sandbox/windows_job.go`, `windows_token.go`, optional `windows_appcontainer.go` (build tag `windows`; `golang.org/x/sys/windows`)

Layered, in ascending effort — each sub-phase ships alone:

1. **Job object** (M): `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` + `ACTIVE_PROCESS` (process cap) + `JOB_MEMORY` (memory cap) + `DIE_ON_UNHANDLED_EXCEPTION`. Replaces the `taskkill` race (a process spawning between enumeration and kill escapes `taskkill /T`; kill-on-close cannot be escaped). Resource limits: done. *This sub-phase is worth doing even if Plan 07 otherwise never fires.*
2. **Restricted token, low integrity** (M): create the child with a low-IL token — Windows then denies writes to all medium-IL objects, i.e. essentially the whole user profile. Grant the workspace explicit low-IL write access (`SetNamedSecurityInfo` label ACE on the workspace root; removed by `cleanup`). Read access to the profile remains (matches the "host-readable" profile; full read-denial needs AppContainer).
3. **AppContainer** (L, optional): full capability-based isolation; with no `internetClient` capability, network is denied at the object-manager level — the only clean `network = deny` on Windows. Significant toolchain friction (many dev tools misbehave in AppContainers); gate behind explicit `sandbox = "strict"` demand and mark experimental. Without it, Windows `Capabilities().NetIsolation = false` and strict+deny refuses on Windows — honest, per principle 2.

---

## Phase 4 — macOS backend (Effort: M)

**Files:** `avc/internal/sandbox/darwin_sbx.go` (build tag `darwin`)

`sandbox-exec` with a generated profile (deprecated in name but load-bearing across macOS and used by comparable dev tools; the underlying `sandbox_init` API is stable):

```scheme
(version 1)
(deny default)
(allow process-exec* process-fork)
(allow file-read*)                          ; host-readable
(allow file-write* (subpath "<workspace>") (subpath "<tmp>") …)
(deny network*)                              ; unless Network
(allow sysctl-read mach-lookup …)            ; minimum for toolchains to run
```

Profile written to a temp file, passed via `sandbox-exec -f`, removed by `cleanup`. Iterate the allow-list against real toolchains (go test, pytest, npm test) in CI. Resource limits via `setrlimit` in a fork/exec shim (macOS has no unprivileged cgroup equivalent). If `sandbox-exec` disappears in a future macOS, `Detect()` reports it and `auto` degrades — principle 2 again.

---

## Phase 5 — Integration & policy cleanup (Effort: M)

- `avc_run_in_workspace`, `avc bisect` (B2), and `avc merge --train --validate` (B4) all route through the launcher; their MCP descriptions and `docs/workspace-command-runner.md` are updated from 03·5k's "policy, not containment" to describe the per-platform guarantees and the honest `sandbox_info`.
- `classify()` demoted to UX: keep `sudo`/system-package-manager errors (they're *better* messages than a sandbox EPERM), delete any implication that it is the safety mechanism.
- Re-scope `buildFilteredPath`: with FS isolation the allowlist's job is reproducibility, not security — simplify or drop per platform.
- `strict` mode documented as the recommended setting for fleet automation (the Plan 05 features that triggered this plan).

---

## Testing

Per-platform integration tests behind build tags, run in a 3-OS CI matrix; each asserts the *boundary*, not the implementation:

- **Escape:** write to project root / `$HOME` / `%USERPROFILE%` from inside a run → fails in `strict`+FS-isolation; the file provably does not exist afterwards.
- **Network:** `curl`/socket connect under `network = deny` → fails; under `allow` → succeeds.
- **Resources:** fork bomb capped by pids/process limit; allocation bomb killed by memory cap; both report a distinguishable error, not a hang.
- **Orphans:** command spawning a detached child → no process from the tree survives run completion (job object / `--die-with-parent`).
- **Degradation:** backend forcibly unavailable (bwrap removed from PATH) → `auto` runs with warning + truthful `sandbox_info`; `strict` refuses with actionable error.
- **Toolchain smoke:** `go test`, `pytest`, `npm test` on fixture projects pass *inside* the sandbox on all three platforms — containment that breaks legitimate use is a regression.

---

## Exit criteria

- [ ] `Launcher` interface + `Detect()` + `avc run doctor`; `sandbox_info` reports real capabilities per run
- [ ] `sandbox = strict` fails closed everywhere containment is unavailable; `auto` degrades loudly; `off` preserves current behavior
- [ ] Linux: bwrap and namespace fallback pass escape/network/resource/orphan tests
- [ ] Windows: job-object kill-on-close + limits; restricted-token write containment passes escape tests; network posture honestly reported
- [ ] macOS: sandbox-exec profile passes escape/network tests; toolchain smoke green
- [ ] B2/B4 runs are contained by default under `auto`; MCP cannot grant itself network
- [ ] `classify()` reduced to UX messaging; docs describe guarantees per platform
- [ ] 3-OS CI matrix green, including toolchain smoke tests
