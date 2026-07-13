# Changelog

All notable changes to AVC will be documented here.

Format: [Keep a Changelog](https://keepachangelog.com/en/1.0.0/)  
Versioning: [Semantic Versioning](https://semver.org/spec/v2.0.0.html)

## [Unreleased]

## [0.3.0] - 2026-07-12

### Added

- `avc watch` — a continuous checkpointing daemon that watches your project and every active branch workspace, automatically snapshotting real changes while skipping ignored churn and idle periods. Checkpoints are debounced and rate-limited, with their own retention cap, and the VSCode extension can start and stop the daemon alongside the editor.
- `avc timeline` — a session-based activity report showing what agents did while you were away: snapshots grouped by session and task, each with a short change summary, interleaved with restores, merges, and undos.
- `avc bisect` — automated regression hunting via binary search between a known-good and known-bad snapshot, running your test command in a freshly materialized workspace at each step. Uses git-compatible exit codes and is also available as an MCP tool for agents.
- Stacked branches (`avc branch create --from-branch <parent>`) and `avc branch diff a..b` for comparing two branches directly, enabling merge-queue-style workflows across fleets of agents.
- Session and task attribution: `avc snapshot --session/--task` groups related snapshots together and records what an agent was working on, feeding directly into `avc timeline`.
- Line-level three-way merge: files edited on both main and a branch now merge automatically when the edits don't overlap, instead of always producing a whole-file conflict. Overlapping edits still get clear, hunk-level conflict markers. This is a major improvement for multi-agent workflows where several branches touch the same file.
- Protected paths: a new `[protect]` config setting blocks or warns on merges that touch sensitive files, enforced automatically at merge time and surfaced early in `avc status`.
- Universal undo: `avc undo` reverses the most recent restore or merge with no arguments needed — running it twice acts as redo. `avc trash restore` brings back quarantined files without ever overwriting live ones.
- `avc verify` (renamed from `avc fsck`) checks the integrity of every stored file, and can repair or quarantine anything corrupted. Stored files are now compressed automatically where it saves space, and `avc storage` reports the resulting savings.

### Fixed

- Restoring the working tree no longer deletes ignored files — anything that would have been removed is safely quarantined instead of destroyed.
- Snapshotting and file storage are now crash-safe: writes are atomic, and a crash partway through a snapshot can no longer be mistaken for "delete everything" on restore.
- Multiple tools — the CLI, MCP server, extension, and web UI — can now read and write at the same time without "database is locked" errors.
- Workspace creation no longer risks silently mutating your real project files.
- Merging a branch that deleted a file no longer crashes; deletions and edit conflicts are now handled and clearly reported.
- `avc merge --abort` now actually works, and a failed merge can no longer get stuck in a permanent "in progress" state.
- Uncommitted workspace changes are now captured automatically before a merge or restore, instead of being silently lost.
- Snapshots that are still in use — an active branch's base, a tagged snapshot, or part of the latest merge — can no longer be deleted or garbage-collected by accident.
- Diffing very large files no longer risks hanging, and binary files are now detected instead of producing meaningless line counts.
- `.avcignore` now correctly supports `**` and negation (`!`) patterns at any directory depth.
- The web UI now requires authentication and validates request origin, closing a gap that let any browser tab call the local API.
- The MCP server no longer drops the whole session on a single oversized request, and sandboxed commands no longer hang after producing more output than the configured limit.
- A malformed `config.toml` no longer crashes every command, and no longer silently disables protected-path enforcement.
- Database migrations no longer silently swallow errors, and no longer re-run on every startup.

### Changed
- Snapshot and restore now preserve the Unix executable bit and clean up empty directories left behind after a restore.
- Documentation refreshed to cover the new commands and updated project links.

## [0.2.1] - 2026-06-28

### Fixed

- `avc init` now asks for confirmation before bootstrapping a brand-new AVC project at a path that doesn't already have one, preventing accidental project creation.

### Changed

- AVC is now licensed under the GNU Affero General Public License v3.0 (AGPL-3.0), with Homebrew and Scoop package metadata updated to match.
- Fixed release pipeline issues with GoReleaser hook paths and Homebrew tap references, and renamed the Scoop bucket to `scoop-bucket` to support multiple published tools going forward.

## [0.2.0] - 2026-05-31

### Added

- `avc status` shows which files have changed since the last snapshot, giving agents and users an at-a-glance view of unstaged work before deciding whether to snapshot or restore.
- Storage management: `avc gc` reclaims space occupied by unreferenced objects (dry-run by default, `--run` to apply), and `avc storage` shows a disk-usage breakdown by branch or snapshot.
- Snapshot tags: label any snapshot with `avc snapshot tag` / `avc snapshot untag` for easy retrieval; `avc_tag` and `avc_untag` MCP tools expose the same capability to agents.
- `avc export` and `avc import` bundle an entire AVC repository — snapshots, history, and object store — into a portable `.tar.gz` for backup or migration between machines.
- `avc search` as a convenient shorthand for `avc list --search`, with new `--agent` and `--changed` filters for targeted snapshot discovery.
- `avc branch rename` renames an existing branch without disturbing its workspace or snapshot history.
- `avc diff --stat` prints a compact changed-file summary instead of the full unified diff, useful for a quick size check before reviewing details.
- Three new MCP tools for agent workflows: `avc_status` (working-tree state), `avc_restore_file` (single-file restore without touching the rest of the workspace), and `avc_annotate` (line-by-line snapshot attribution).
- Conflict resolution MCP tools: `avc_list_conflicts` and `avc_resolve_conflict` let agents resolve merge conflicts programmatically without leaving the agent loop.
- Pre- and post-hook support for snapshot and restore operations, fired with `AVC_*` environment variables so external scripts can react to AVC lifecycle events.
- Snapshot retention policy to automatically prune snapshots beyond a configurable limit, keeping the object store from growing unbounded on long-running projects.
- MCP tool tiers (`--tools core/standard/full`) give operators control over which tools are exposed to agents, enabling minimal surface-area deployments.
- Cross-platform release pipeline: GoReleaser configuration, cosign artifact signing, Homebrew tap and Scoop bucket support, and `avc --version` populated via build-time ldflags.
- Web UI gains a branch selector, merge panel, and status view; branch creation and merge operations are now accessible directly from the browser.

### Fixed

- Branch mapping, snapshot search scoping, and post-merge active-branch state are now correctly handled when importing an exported repository bundle.
- Annotate (line-blame) queries are now O(1) instead of O(N), eliminating slowdowns on histories with many snapshots.
- Conflict markers upgraded to diff3 style, providing additional surrounding context that makes manual and agent-driven conflict resolution more reliable.

### Changed

- SQLite now runs in WAL mode with query indexes, improving concurrent read performance and reducing lock contention during multi-agent workloads.
- Branch names are validated against a strict pattern, preventing names that would be ambiguous or unsafe as workspace directory paths.
- Branch workspace creation uses hardlinks where the filesystem supports them, making materialisation significantly faster for large projects.
- GitHub organization renamed from SkillMythOrg to trevarix; documentation site migrated to trevarix/avc-docs.

## [0.1.0] - 2026-05-19

### Added

- Initial release of AVC (Agentic Version Control) — a local version control system built for the agent era, delivering snapshot, diff, branch, and merge primitives that make agent-driven changes safe and reviewable without requiring Git.
- Snapshot engine that captures the complete state of a project directory into a content-addressed object store. Each snapshot is labelled and queryable; identical files across snapshots share a single stored object. A stat cache (mtime + size) skips re-hashing unchanged files for fast incremental captures.
- Diff command to compare any two snapshots with a unified diff preview and per-file line counts. Snapshot file trees now annotate each entry with A, M, or D indicators showing whether a file was added, modified, or deleted relative to the previous snapshot.
- Branch workspaces that give each agent an isolated copy of the working tree in `.avc/workspaces/<branch>/`. Snapshots, diffs, and restores all operate within the branch boundary — the real project root is never touched while a branch is active.
- Three-way merge engine to integrate a branch back into main. Conflicts are detected and reported before any file is written; clean merges apply in a single step.
- MCP server (`avc mcp serve`) that exposes all AVC primitives as JSON-RPC 2.0 tools over stdio, allowing any MCP-compatible agent (Claude Code, Cursor, etc.) to create snapshots, manage branches, inspect diffs, and trigger merges without leaving the agent loop.
- Agent skills integration via `avc init --skills <framework>`, which writes framework-specific MCP configuration and agent instruction files so agents know when and how to use AVC safely by default.
- Workspace command runner (`avc run`) for executing build or test commands sandboxed inside a branch workspace, with full process-tree termination and venv isolation to prevent agent-driven runs from polluting the host environment.
- VSCode extension with a sidebar panel listing all snapshots grouped by date, diff-status indicators, category filters, and a folder-tree view. Includes a diff viewer webview for side-by-side snapshot comparison and one-click restore. The extension communicates exclusively with the CLI (`avc --json`) and never accesses `.avc/` directly.
- Standalone web UI (`avc ui`) for browsing snapshots, inspecting diffs, and managing branches from a browser, with toast notifications, status badges, and human-readable timestamps.
- Structured CLI output with coloured text, organised help pages, and `--json` support on every command for machine-readable integration with scripts and agents.
- Astro Starlight documentation site deployed to GitHub Pages, covering the CLI reference, architecture, contributing guide, code of conduct, and security policy.
- GitHub Actions CI/CD pipeline for building the Go CLI binary and VSCode extension, running the test suite, and deploying documentation and release artefacts.
