// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package workspace executes commands inside agent branch workspaces with
// environment scrubbing, execution limits, and process tree kill.
package workspace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/config"
)

// RunRequest describes a command to execute in a branch workspace.
type RunRequest struct {
	ProjectRoot    string
	BranchName     string
	Command        string
	TimeoutSeconds int
}

// EnvInfo reports what virtual environment was activated for the command.
type EnvInfo struct {
	Type       string `json:"type"`        // "python-venv" | "node" | "go" | "none"
	Path       string `json:"path"`        // venv or node_modules path
	ModuleName string `json:"module_name"` // go module name if applicable
}

// SandboxInfo reports which isolation layers were applied.
type SandboxInfo struct {
	Platform        string `json:"platform"`
	EnvScrubbing    bool   `json:"env_scrubbing"`
	ExecutionLimits bool   `json:"execution_limits"`
	ProcessTreeKill bool   `json:"process_tree_kill"`
}

// RunResult is the output of a workspace command execution.
type RunResult struct {
	ExitCode      int
	Stdout        string
	Stderr        string
	WorkspacePath string
	EnvInfo       EnvInfo
	SandboxInfo   SandboxInfo
}

const (
	defaultTimeoutSeconds = 180
	maxTimeoutSeconds     = 600
	defaultMaxOutputKB    = 512
	maxOutputKB           = 2048
)

// Run executes req.Command in the branch workspace with all sandbox layers applied.
func Run(req RunRequest) (*RunResult, error) {
	// Step 1: validate.
	if req.BranchName == "" || req.BranchName == "main" {
		return nil, fmt.Errorf("avc_run_in_workspace requires a non-main agent branch; got %q", req.BranchName)
	}
	workspacePath := filepath.Join(req.ProjectRoot, ".avc", "workspaces", req.BranchName)
	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("workspace for branch %q not found — run `avc branch create %s` first", req.BranchName, req.BranchName)
	}
	return runSandboxed(req.ProjectRoot, workspacePath, req.Command, req.TimeoutSeconds)
}

// RunInDir executes command in an arbitrary directory with the same sandbox
// layers as Run (env scrubbing, command blocklist, timeout, output caps,
// process-tree kill). Used by the merge train's --validate step, which must
// run against the real project root (post-merge main) rather than a branch
// workspace. Callers are responsible for the [run] enabled gate.
func RunInDir(projectRoot, dir, command string, timeoutSeconds int) (*RunResult, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("run directory %q not accessible: %w", dir, err)
	}
	return runSandboxed(projectRoot, dir, command, timeoutSeconds)
}

// runSandboxed is the shared core of Run and RunInDir. workspacePath is the
// directory the command executes in (and the anchor for venv/node_modules/
// GOMODCACHE isolation).
func runSandboxed(projectRoot, workspacePath, reqCommand string, timeoutSeconds int) (*RunResult, error) {
	sandboxInfo := SandboxInfo{
		Platform:        runtime.GOOS,
		EnvScrubbing:    true,
		ExecutionLimits: true,
		ProcessTreeKill: true,
	}

	// Step 2: classify — block immediately.
	class := classify(reqCommand)
	if class == classBlocked {
		return &RunResult{
			ExitCode:      -1,
			Stderr:        blockedMessage(reqCommand),
			WorkspacePath: workspacePath,
			SandboxInfo:   sandboxInfo,
		}, nil
	}

	// Step 3: resolve config values.
	cfg, _ := config.Load(projectRoot)
	timeout := resolveTimeout(timeoutSeconds, cfg)
	maxOut := resolveMaxOutput(cfg)

	// Step 4: prepare environment (Layer 1).
	envVars := buildEnv(workspacePath)
	envInfo := EnvInfo{Type: "none"}

	command := reqCommand

	switch class {
	case classPipInstall:
		venvPath, err := ensurePythonVenv(workspacePath)
		if err != nil {
			return &RunResult{ExitCode: -1, Stderr: err.Error(), WorkspacePath: workspacePath, SandboxInfo: sandboxInfo}, nil
		}
		command = replacePipWithVenv(venvPath, command)
		envInfo = EnvInfo{Type: "python-venv", Path: venvPath}

	case classPython:
		venvPath, err := ensurePythonVenv(workspacePath)
		if err != nil {
			return &RunResult{ExitCode: -1, Stderr: err.Error(), WorkspacePath: workspacePath, SandboxInfo: sandboxInfo}, nil
		}
		envVars = prependToPath(envVars, venvBinDir(venvPath))
		envInfo = EnvInfo{Type: "python-venv", Path: venvPath}

	case classNode:
		nodeModulesBin := filepath.Join(workspacePath, "node_modules", ".bin")
		envVars = prependToPath(envVars, nodeModulesBin)
		envInfo = EnvInfo{Type: "node", Path: filepath.Join(workspacePath, "node_modules")}

	case classGo:
		gomodcache := filepath.Join(workspacePath, ".gomodcache")
		envVars = append(envVars, "GOMODCACHE="+gomodcache)
		envInfo = EnvInfo{Type: "go"}
	}

	// Step 5: build exec.CommandContext with timeout (Layer 2).
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = workspacePath
	cmd.Env = envVars

	// Step 6: setup process group (Layer 3).
	setupProcessGroup(cmd)

	// Step 7: output pipes with LimitedReader caps (Layer 2).
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	stdoutReader := &io.LimitedReader{R: stdoutPipe, N: maxOut}
	stderrReader := &io.LimitedReader{R: stderrPipe, N: maxOut}

	// Step 8: start.
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	// Step 9: drain output concurrently, then wait.
	//
	// Once the LimitedReader budget is exhausted, io.Copy into the buffer
	// stops — but if nothing keeps reading the underlying pipe past that
	// point, a still-writing child fills the OS pipe buffer and blocks on
	// its next write forever. A chatty-but-passing command would then hang
	// until the context timeout and get reported as killed (-1) instead of
	// its real, successful exit code. Draining the raw pipe to io.Discard
	// after the cap keeps the child able to finish normally.
	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(&stdoutBuf, stdoutReader) //nolint:errcheck
		io.Copy(io.Discard, stdoutPipe)   //nolint:errcheck
	}()
	go func() {
		defer wg.Done()
		io.Copy(&stderrBuf, stderrReader) //nolint:errcheck
		io.Copy(io.Discard, stderrPipe)   //nolint:errcheck
	}()

	waitErr := cmd.Wait()

	// Kill tree if the context fired before the process exited.
	if ctx.Err() != nil {
		killProcessTree(cmd)
	}

	wg.Wait()

	// Step 10: resolve exit code.
	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			if exitCode < 0 {
				exitCode = 1
			}
		} else if ctx.Err() != nil {
			exitCode = -1
		} else {
			exitCode = 1
		}
	}

	// Step 11: append truncation note if output was capped.
	stdoutStr := appendTruncationNote(stdoutBuf.String(), stdoutReader.N, maxOut)
	stderrStr := appendTruncationNote(stderrBuf.String(), stderrReader.N, maxOut)

	return &RunResult{
		ExitCode:      exitCode,
		Stdout:        stdoutStr,
		Stderr:        stderrStr,
		WorkspacePath: workspacePath,
		EnvInfo:       envInfo,
		SandboxInfo:   sandboxInfo,
	}, nil
}

func resolveTimeout(requested int, cfg *config.Config) int {
	t := requested
	if t <= 0 {
		if cfg != nil && cfg.Run.DefaultTimeoutSeconds > 0 {
			t = cfg.Run.DefaultTimeoutSeconds
		} else {
			t = defaultTimeoutSeconds
		}
	}
	max := maxTimeoutSeconds
	if cfg != nil && cfg.Run.MaxTimeoutSeconds > 0 {
		max = cfg.Run.MaxTimeoutSeconds
	}
	if t > max {
		t = max
	}
	return t
}

func resolveMaxOutput(cfg *config.Config) int64 {
	kb := defaultMaxOutputKB
	if cfg != nil && cfg.Run.MaxOutputKB > 0 {
		kb = cfg.Run.MaxOutputKB
	}
	if kb > maxOutputKB {
		kb = maxOutputKB
	}
	return int64(kb) * 1024
}

func appendTruncationNote(output string, remaining, limit int64) string {
	if remaining > 0 {
		return output
	}
	totalEstimate := int64(len(output)) + 1 // at least one more byte existed
	_ = totalEstimate
	return output + fmt.Sprintf(
		"\n[... output truncated at %d KB. Increase max_output_kb in .avc/config.toml to see more.]",
		limit/1024,
	)
}

// goModuleName reads the module name from go.mod in the workspace if present.
func goModuleName(workspacePath string) string {
	data, err := os.ReadFile(filepath.Join(workspacePath, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}
