# Changelog

All notable changes to AVC will be documented here.

Format: [Keep a Changelog](https://keepachangelog.com/en/1.0.0/)  
Versioning: [Semantic Versioning](https://semver.org/spec/v2.0.0.html)

## [Unreleased]

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
