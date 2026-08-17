---
name: avc-run
description: Run a build or test command in the AVC workspace — always get user approval first
---

Use **avc_run_in_workspace** to run commands in the branch workspace. You MUST get explicit user approval before every call.

## Required sequence — no exceptions

1. State the exact command you intend to run
2. Explain what it does and why you need to run it
3. Wait for the user to say yes ("yes", "go ahead", "run it", "ok")
4. If the user says no, do not call the tool

## How to call

```json
{
  "branch": "<branch-name>",
  "command": "npm test",
  "timeout_seconds": 120
}
```

## Rules

- System package managers are blocked: `brew install`, `apt install`, `choco install`, `sudo`
- Python installs: use `pip install <pkg>` — a workspace venv is created automatically. NEVER use `--user` or `--system`
- Node installs: use `npm install` — packages go into workspace `node_modules`. NEVER use `-g` or `--global`
- If the command times out, tell the user and suggest increasing `max_timeout_seconds` in `.avc/config.toml`

## After running

- If tests pass: snapshot the workspace, then proceed
- If tests fail: show the full stderr to the user, then fix and re-run (with approval)
