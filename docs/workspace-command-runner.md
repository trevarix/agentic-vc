# Workspace Command Runner — Phase 7

## Problem

When an agent finishes editing files in its workspace, the user must open the workspace
in a new VSCode window, run tests manually, and then paste any errors back to the agent.
This is a broken feedback loop: the agent cannot act on errors it cannot see.

Two friction points:

1. **Manual relay** — the user reads test output and describes it to the agent in prose.
   Precision is lost; round-trips are slow.
2. **Dependency installation** — if the project's test suite requires `pip install` or
   `npm install`, running it in the workspace dir pollutes either the global environment
   or the real project's `node_modules` / `.venv`.

---

## Design

### New MCP tool: `avc_run_in_workspace`

The agent calls this tool to execute a command inside its workspace directory.
The command runs as a sandboxed subprocess; stdout, stderr, and exit code are returned.
The agent sees exactly what a human would see in a terminal.

```
avc_run_in_workspace(branch, command, timeout_seconds?) →
  { exit_code, stdout, stderr, workspace_path, env_info, sandbox_info }
```

**Input fields:**

| Field | Type | Description |
|-------|------|-------------|
| `branch` | string | Branch whose workspace the command runs in. Must not be `"main"`. |
| `command` | string | Shell command to execute. Runs in the workspace directory. |
| `timeout_seconds` | int | Optional. Overrides `default_timeout_seconds` from config. Cannot exceed `max_timeout_seconds`. |

**Output fields:**

| Field | Type | Description |
|-------|------|-------------|
| `exit_code` | int | Process exit code. 0 = success. -1 = blocked or killed by sandbox. |
| `stdout` | string | Captured standard output (truncated at 50 KB). |
| `stderr` | string | Captured standard error (truncated at 50 KB). |
| `workspace_path` | string | Absolute path of the workspace the command ran in. |
| `env_info` | object | What virtual environment was activated. See below. |
| `sandbox_info` | object | Which sandbox layers were applied on this platform. |

---

### User approval requirement

**The agent must ask the user for explicit approval before calling this tool.**

This is enforced through two mechanisms:

1. **Tool description** — the `avc_run_in_workspace` MCP tool description includes:
   > "Always present the full command to the user and obtain explicit approval before
   > calling this tool. Never call it autonomously."

2. **SKILL.md instruction** — the agent skill files written by `avc init --skills`
   include a rule:
   > "Before calling `avc_run_in_workspace`, show the user the exact command you intend
   > to run and wait for their go-ahead. If they say no, do not call the tool."

The server itself does **not** prompt the user — it has no UI access. The obligation
is placed on the agent via its instruction context.

---

### Sandbox model

> **This is a hygiene layer, not a security boundary.** Every layer below reduces
> *accidental* host pollution by a cooperative command — it does not contain an
> adversarial one. The command still runs with the invoking user's full filesystem
> and network access. `classify()` inspects only the first token of the command, so
> a command that chains around it (`env sudo ...`, `bash -c "..."`, a pipe, a `python
> -c "os.system(...)"`) is not stopped. Do not run untrusted or unreviewed code
> through `avc_run_in_workspace` expecting it to be contained — see
> `docs/plans/07-sandbox-containment.md` for what real OS-level containment
> (namespaces, `sandbox-exec`, restricted job objects/tokens) would require, and
> when it's actually worth building.

Commands are executed through two isolation layers and one operational layer.

The **isolation layers** reduce accidental host pollution by the subprocess:
- **Layer 1** — credential leakage prevention (env scrubbing, PATH restriction)
- **Layer 3** — process tree kill (no orphan processes after timeout or teardown)
- **Blocked command list** — obviously wrong commands (system-level installs, `sudo`)
  are rejected before execution with a friendly error, as a courtesy — not as containment

The **operational layer** keeps the tool functional:
- **Layer 2** — timeout and output cap (prevents hangs and context window overflow)

All layers apply on every run across all platforms.

#### Layer 1 — Working directory and environment scrubbing

Applied on all platforms before the process starts.

- `Cmd.Dir` set to workspace path. Process cannot accidentally affect the real project
  root by using relative paths.
- `HOME` overridden to a temp dir inside the workspace so tools that write to `~/`
  (pip cache, npm cache, go env) land inside the workspace tree, not the user's home.
- `PATH` restricted to an allowlist of approved runtimes: `python`, `python3`, `pip`,
  `pip3`, `node`, `npm`, `npx`, `yarn`, `pnpm`, `go`, `cargo`, `ruby`, `gem`,
  `java`, `mvn`, `gradle`, `make`, `cmake`, standard Unix utilities (`ls`, `cat`,
  `echo`, `grep`, `find`, `cp`, `mv`, `rm`, `mkdir`, `touch`, `env`, `sh`, `bash`).
  Anything not on the allowlist is not reachable.
- Sensitive host environment variables removed from subprocess env: `AWS_*`, `GITHUB_*`,
  `SSH_*`, `GPG_*`, `DATABASE_URL`, `SECRET_*`, `TOKEN_*`, `API_KEY*`, `*_PASSWORD`.

#### Layer 2 — Execution limits

Applied on all platforms.

| Limit | Default | Max | Mechanism |
|-------|---------|-----|-----------|
| Wall-clock time | 180s | 600s | `exec.CommandContext` cancel |
| Output per stream | 512 KB | 2 MB | Capped `io.LimitedReader` on stdout/stderr pipes |

Both values are read from `.avc/config.toml` on every call. The user can adjust them per project without touching CLI flags:

```toml
[run]
# Maximum time a workspace command can run before being killed.
default_timeout_seconds = 180
max_timeout_seconds     = 600

# Maximum output captured per stream (stdout/stderr) before truncation.
# Increase for projects with verbose test suites.
max_output_kb = 512
```

When the timeout fires, Layer 3 takes over to kill the process tree. When output is truncated, a trailing note is appended:

```
[... output truncated at 512 KB — 1.2 MB total. Increase max_output_kb in .avc/config.toml to see more.]
```

#### Layer 3 — Process tree kill

Ensures all child processes spawned by the command (e.g. jest workers, webpack) are
killed when the timeout fires or when the runner tears down, not just the direct child.

- **Unix:** `Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` — the process runs
  in its own process group. On timeout or cancellation, the runner sends `SIGKILL` to
  the entire group (`syscall.Kill(-pgid, syscall.SIGKILL)`).
- **Windows:** The process is created inside a Job Object
  (`CreateJobObject` + `AssignProcessToJobObject`). `TerminateJobObject` kills every
  process in the tree atomically.

`sandbox_info` response field:

```json
{
  "platform": "linux",
  "layers": {
    "env_scrubbing":     true,
    "execution_limits":  true,
    "process_tree_kill": true
  }
}
```

---

### Virtual environment isolation

Commands that install packages must not affect the real project root or the user's
global environment. The tool detects the install type from the command prefix and
routes accordingly.

#### Python

| Command prefix | Behaviour |
|---------------|-----------|
| `pip install` / `pip3 install` | Redirected into `.avc/workspaces/<branch>/.venv/` |
| `python` / `python3` / `pytest` / `py.test` | Activated against workspace `.venv` if present |
| `uv add` / `uv sync` / `uv run` | Workspace `.venv` used as `UV_PROJECT_ENVIRONMENT` |
| Other | Run as-is in workspace dir |

**Venv lifecycle:**

- On first `pip install` in a workspace, the tool creates `.avc/workspaces/<branch>/.venv/`
  via `python -m venv`.
- Subsequent `pip install` calls in the same workspace reuse it.
- The venv is removed when the branch is deleted (`branch.RemoveWorkspace`).

`env_info` response: `{ "type": "python-venv", "path": ".../.venv" }`.

#### Node.js

| Command prefix | Behaviour |
|---------------|-----------|
| `npm install` / `yarn` / `pnpm install` | Run in workspace — `node_modules/` lands in workspace |
| `npm test` / `npm run` / `npx` | Workspace `node_modules/.bin` prepended to PATH |
| Other | Run as-is |

Node package managers scope to the nearest `package.json` automatically.
`env_info` response: `{ "type": "node", "node_modules": "<path>" }`.

#### Go

All `go` commands run in the workspace dir. Module cache is read from the host
(`GOPATH` unchanged) but module downloads go to a workspace-local cache
(`GOMODCACHE` set to `.avc/workspaces/<branch>/.gomodcache`).
`env_info` response: `{ "type": "go", "module": "<module name>" }`.

#### System-level installs — blocked

Commands that would modify the host system are rejected before execution:

| Blocked prefix | Reason |
|---------------|--------|
| `brew install` | Modifies host Homebrew |
| `apt install` / `apt-get install` | Modifies system packages |
| `choco install` | Modifies system packages |
| `sudo` | Privilege escalation — never allowed |
| `pip install --user` | Writes to user global site-packages |
| `npm install -g` / `npm install --global` | Writes to global node_modules |

The tool returns `exit_code: -1` and an error in `stderr` explaining why the command
was blocked and what the scoped alternative is.

### Output truncation

Large build logs exceed agent context limits. The tool truncates at 50 KB per stream,
adding a trailing note:

```
[... output truncated at 50 KB — 142 KB total. Run locally to see full output.]
```

---

## Implementation plan

### Step 1 — MCP tool definition

**File:** `avc/internal/mcp/tools.go`

Add `avc_run_in_workspace` to the tool registry:

```json
{
  "branch":          { "type": "string",  "description": "Branch name (not main)" },
  "command":         { "type": "string",  "description": "Shell command to run" },
  "timeout_seconds": { "type": "integer", "description": "Timeout in seconds (default 60, max 300)" }
}
```

Tool description must include the user-approval requirement verbatim.

### Step 2 — Core types

**File:** `avc/internal/workspace/runner.go` (new package)

```go
package workspace

type RunRequest struct {
    ProjectRoot    string
    BranchName     string
    Command        string
    TimeoutSeconds int
}

type EnvInfo struct {
    Type       string // "python-venv" | "node" | "go" | "none"
    Path       string // venv or node_modules path
    ModuleName string // go module name
}

type SandboxInfo struct {
    Platform        string
    EnvScrubbing    bool
    ExecutionLimits bool
    ProcessTreeKill bool
}

type RunResult struct {
    ExitCode      int
    Stdout        string
    Stderr        string
    WorkspacePath string
    EnvInfo       EnvInfo
    SandboxInfo   SandboxInfo
}

func Run(req RunRequest) (*RunResult, error)
```

### Step 3 — Command classification

**File:** `avc/internal/workspace/classify.go`

```go
type commandClass int

const (
    classBlocked    commandClass = iota
    classPipInstall
    classPython
    classNode
    classGo
    classGeneric
)

func classify(command string) commandClass
func isBlockedGlobalFlag(command string) bool // detects --user, -g, --global flags
```

Classification is by first token. `isBlockedGlobalFlag` scans all tokens for flags
that would cause writes outside the workspace.

### Step 4 — Layer 1: environment builder

**File:** `avc/internal/workspace/env.go`

```go
// buildEnv returns a scrubbed environment for the subprocess.
// HOME is redirected to a temp dir inside the workspace.
// PATH is restricted to the approved runtime allowlist.
// Sensitive variables matching known patterns are stripped.
func buildEnv(workspacePath string, class commandClass) []string

var sensitiveVarPrefixes = []string{
    "AWS_", "GITHUB_", "SSH_", "GPG_", "SECRET_",
    "TOKEN_", "API_KEY", "DATABASE_URL",
}

var pathAllowlist = []string{
    "python", "python3", "pip", "pip3",
    "node", "npm", "npx", "yarn", "pnpm",
    "go", "cargo", "ruby", "gem",
    "java", "mvn", "gradle",
    "make", "cmake",
    "sh", "bash", "env",
    "ls", "cat", "echo", "grep", "find",
    "cp", "mv", "rm", "mkdir", "touch",
}
```

### Step 5 — Layer 2: execution limits

**File:** `avc/internal/workspace/runner.go`

Timeout via `exec.CommandContext`, duration resolved as:

```go
timeout := req.TimeoutSeconds
if timeout <= 0 {
    timeout = cfg.Run.DefaultTimeoutSeconds  // 180
}
if timeout > cfg.Run.MaxTimeoutSeconds {
    timeout = cfg.Run.MaxTimeoutSeconds      // 600
}
```

Output capping via `io.LimitedReader` wrapping `cmd.StdoutPipe()` and
`cmd.StderrPipe()`, limit read from `cfg.Run.MaxOutputKB * 1024`. No platform
split needed.

**File:** `avc/internal/config/config.go`

```go
type RunConfig struct {
    DefaultTimeoutSeconds int `toml:"default_timeout_seconds"`
    MaxTimeoutSeconds     int `toml:"max_timeout_seconds"`
    MaxOutputKB           int `toml:"max_output_kb"`
}

type Config struct {
    Branch BranchConfig `toml:"branch"`
    Run    RunConfig    `toml:"run"`
}
```

**File:** `avc/cmd/avc/init.go`

Write `[run]` block to `.avc/config.toml` during `avc init` with default values
and comments shown above.

### Step 6 — Layer 3: process tree kill

**File:** `avc/internal/workspace/processtree_unix.go`

```go
func setupProcessGroup(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(cmd *exec.Cmd) {
    if cmd.Process != nil {
        syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
    }
}
```

**File:** `avc/internal/workspace/processtree_windows.go`

```go
func setupProcessGroup(cmd *exec.Cmd)  // attaches Job Object handle
func killProcessTree(cmd *exec.Cmd)    // calls TerminateJobObject
```

### Step 7 — Python venv management

**File:** `avc/internal/workspace/venv.go`

```go
func ensurePythonVenv(workspacePath string) (venvPath string, err error)
func pipArgs(venvPath, originalCommand string) []string
func pythonArgs(venvPath, originalCommand string) []string
```

`ensurePythonVenv` creates `.venv/` in the workspace if absent via `python -m venv .venv`.
`pipArgs` replaces `pip` with `<venv>/bin/pip` (Unix) or `<venv>\Scripts\pip.exe` (Windows).
`pythonArgs` prepends venv `bin/` to PATH so workspace Python is used.

### Step 8 — Subprocess executor

**File:** `avc/internal/workspace/runner.go`

`Run` orchestrates all layers in order:

1. Validate branch is not `"main"`, workspace dir exists
2. Classify command — return blocked error immediately if `classBlocked`
3. Load config — resolve timeout and output cap values
4. Prepare environment via `buildEnv` (Layer 1)
5. Build `exec.CommandContext` with resolved timeout (Layer 2)
6. `setupProcessGroup` (Layer 3)
7. Set output pipes with `io.LimitedReader` caps (Layer 2)
8. `cmd.Start()`
9. `cmd.Wait()` — on context cancellation, call `killProcessTree`
10. Collect stdout/stderr, append truncation note if capped
11. Return `RunResult` with `SandboxInfo` reporting which layers were applied

### Step 9 — MCP handler

**File:** `avc/internal/mcp/handlers.go`

```go
case "avc_run_in_workspace":
    return toolRunInWorkspace(projectRoot, params)
```

```go
func toolRunInWorkspace(projectRoot string, params map[string]any) (any, error) {
    branch  := stringParam(params, "branch")
    command := stringParam(params, "command")
    timeout := intParamDefault(params, "timeout_seconds", 60)
    result, err := workspace.Run(workspace.RunRequest{
        ProjectRoot:    projectRoot,
        BranchName:     branch,
        Command:        command,
        TimeoutSeconds: timeout,
    })
    if err != nil {
        return nil, err
    }
    return map[string]any{
        "exit_code":      result.ExitCode,
        "stdout":         result.Stdout,
        "stderr":         result.Stderr,
        "workspace_path": result.WorkspacePath,
        "env_info":       result.EnvInfo,
        "sandbox_info":   result.SandboxInfo,
    }, nil
}
```

### Step 10 — SKILL.md updates

**File:** `avc/internal/skills/skills.go`

Add to each framework's instruction content:

```
## Running commands in the workspace

Before calling `avc_run_in_workspace`, you MUST:
1. Show the user the exact command you intend to run.
2. Explain what the command does and why.
3. Wait for explicit user approval ("yes", "go ahead", "run it").

If the user declines, do not call the tool.

Commands run in a sandboxed environment:
- System package managers (brew, apt, choco, sudo) are blocked on all platforms.
- For Python: use `pip install` — a venv is created inside the workspace automatically.
  Do not use --user or --system flags.
- For Node: use `npm install` — packages install into workspace node_modules.
  Do not use -g or --global flags.
```

### Step 11 — CLI command

**File:** `avc/cmd/avc/run.go`

```
avc run --branch <name> <command...>
```

Thin wrapper: resolves project root, calls `workspace.Run`, writes stdout/stderr to
os.Stdout/os.Stderr, exits with the subprocess exit code. Useful for testing the
runner without MCP.

### Step 12 — Tests

**File:** `avc/tests/runner_test.go`

- `TestRunner_EchoCommand` — echo returns correct stdout, exit 0
- `TestRunner_ExitCode` — command exiting 1 returns exit code 1
- `TestRunner_Timeout` — `sleep 300` killed after 1s; exit code non-zero; no orphan process
- `TestRunner_ProcessTreeKill` — command spawning children; all children dead after timeout
- `TestRunner_EnvScrubbing` — `echo $AWS_SECRET_ACCESS_KEY` returns empty string
- `TestRunner_PathRestriction` — `git status` returns not-found (git not on allowlist)
- `TestRunner_PipInstall_CreatesVenv` — `pip install requests` creates `.venv/` in workspace
- `TestRunner_PipInstall_Idempotent` — second `pip install` reuses existing `.venv/`
- `TestRunner_BlockedCommand` — `sudo apt install curl` returns exit -1 with error message
- `TestRunner_BlockedGlobalFlag` — `npm install -g typescript` returns exit -1 with error
- `TestRunner_MainBranchRejected` — branch `"main"` returns error, no command run
- `TestRunner_OutputTruncation` — command producing > 512 KB output is truncated with trailer referencing config key

---

## What this does not change

- The workspace directory layout is unchanged — `.avc/workspaces/<branch>/` is still the
  root the agent edits.
- The object store and DB are untouched by command execution.
- Snapshot, restore, diff, and merge are unaffected.
- The user can still open the workspace in a new VSCode window for manual inspection.
  The command runner is an alternative path, not a replacement.

---

## Platform support matrix

| Layer | Linux | macOS | Windows |
|-------|-------|-------|---------|
| Env scrubbing + PATH restriction | ✅ | ✅ | ✅ |
| Execution limits (timeout + output cap) | ✅ | ✅ | ✅ |
| Process tree kill | ✅ process group | ✅ process group | ✅ Job Object |
