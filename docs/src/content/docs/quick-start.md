---
title: Quick Start
description: A 5-minute walkthrough of AVC's core workflow — snapshot, diff, restore.
---

This guide walks you through AVC's core loop: initialize a project, take a baseline snapshot, make changes, see the diff, and restore. By the end you'll know enough to use AVC daily.

:::tip[Prerequisites]
Install the AVC CLI first — see the [Installation guide](/agentic-vc/install/). Verify with `avc --version`.
:::

## 1. Initialize a project

Pick any project directory:

```bash
cd ~/my-project
avc init
```

This creates a `.avc/` directory with a SQLite database and a default `.avcignore` file. Re-running is safe; it's a no-op on already-initialized projects.

If you want the agent integration files set up at the same time:

```bash
avc init --skills claude-code,cursor,windsurf
```

## 2. Take a baseline snapshot

```bash
avc snapshot "Baseline"
```

Output:

```
Snapshot created: snap-a1b2c3d4
  Label:    Baseline
  Branch:   main
  Files:    42
  Size:     1.2 MB
```

This snapshot is your "known good" state. You can return to it at any time.

## 3. Make changes (or let an agent make them)

Edit some files. For this walkthrough, just modify any file in the project — change a line in your README, add a new file, whatever.

If you're using an AI agent, point it at the project and ask it to do something. Examples:
- "Refactor the auth module to use bcrypt"
- "Add a unit test for the calculator function"
- "Fix the bug in the payment handler"

## 4. Snapshot the result

```bash
avc snapshot "After changes" --agent claude --notes "Refactored auth"
```

The `--agent` and `--notes` flags are optional but useful — they help you remember what each snapshot represents when scrolling through a long list.

## 5. See what changed

```bash
avc diff snap-a1b2c3d4 snap-e5f6g7h8
```

You'll see a per-file summary:

```
M src/auth/login.go    (+12 -8)
M src/auth/session.go  (+5 -2)
A src/auth/bcrypt.go   (+34 -0)
```

For richer output:

```bash
avc diff snap-a1b2c3d4 snap-e5f6g7h8 --json
```

You'll get the line-level unified diff for every changed file as JSON — feed it into another tool, or render it in the [Web UI](/agentic-vc/web-ui/).

## 6. Roll back if needed

If the changes broke something, restore the baseline:

```bash
avc restore snap-a1b2c3d4
```

:::caution
Restore overwrites current files. AVC auto-creates a "Pre-restore safety snapshot" before doing this, so you can undo the undo by restoring *that* snapshot.
:::

## What's next?

You now know the core loop. From here:

- **List all snapshots** → `avc list`
- **See snapshot details + file tree** → `avc info snap-a1b2c3d4`
- **Open a graphical browser** → `avc ui` then visit `http://localhost:3004/`
- **Use branches for parallel agent runs** → see [Concepts → Branches](/agentic-vc/concepts/branches/)
- **Browse from VSCode** → install the [VSCode Extension](/agentic-vc/extension/)
- **Wire it up to your AI agent** → see [Agent Integration](/agentic-vc/agents/)
