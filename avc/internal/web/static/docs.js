/* AVC docs page — command catalog with sidebar nav. */
'use strict';

// ── Command catalog ────────────────────────────────────────────────────────
// Each section groups related commands. Keep this in sync with the CLI.
const CATALOG = [
  {
    id: 'overview',
    section: 'Getting Started',
    commands: [
      {
        id: 'overview',
        title: 'Overview',
        synopsis: 'What AVC is and how to use it.',
        body: `
          <p class="lede">AVC (Agentic Version Control) is a local version control system built for AI agents. It gives you four primitives to work safely with code: <strong>snapshot</strong>, <strong>diff</strong>, <strong>branch</strong>, and <strong>merge</strong> — without the complexity of Git.</p>

          <h2>Why AVC</h2>
          <p>When AI agents modify your code, you need to know exactly what changed and roll back instantly if something breaks. AVC gives both you and your agents a safety net: every action is reversible, every change is traceable.</p>

          <h2>Quick start</h2>
          <pre><code># Initialize AVC in your project
avc init

# Create your first snapshot
avc snapshot "Baseline"

# Let an agent work, then snapshot again
avc snapshot "After agent run" --agent "claude" --notes "Refactored auth"

# See what changed
avc diff snap-abc123 snap-def456

# Roll back if needed
avc restore snap-abc123

# Browse snapshots in the web UI
avc ui</code></pre>

          <h2>Global flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--json</code></td><td>Output results as JSON (machine-readable)</td></tr>
              <tr><td><code>--help</code></td><td>Show help for any command</td></tr>
            </tbody>
          </table>

          <h2>Architecture</h2>
          <ul>
            <li><strong>SQLite database</strong> at <code>.avc/avc.db</code> — stores snapshot metadata only</li>
            <li><strong>Object store</strong> at <code>.avc/objects/</code> — content-addressed file blobs (SHA256), automatically deduplicated</li>
            <li><strong>Workspace isolation</strong> — non-main branches operate on <code>.avc/workspaces/&lt;branch&gt;/</code> so the real project stays untouched</li>
          </ul>
        `,
      },
      {
        id: 'init',
        title: 'avc init',
        synopsis: 'Initialize AVC for a project.',
        body: `
          <p class="lede">Initialize AVC in a project directory. Creates <code>.avc/</code> with a SQLite database, default config, and <code>.avcignore</code>.</p>

          <h2>Usage</h2>
          <pre><code>avc init                                    # initialize current directory
avc init my-project                         # creates directory if needed, then init
avc init --skills claude-code               # Claude Code (CLI + VSCode + desktop app)
avc init --skills claude-desktop            # Claude Desktop standalone app
avc init --skills claude-code,cursor        # multiple frameworks at once
avc init --skills cursor,windsurf,generic   # all non-Claude frameworks</code></pre>

          <h2>Flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--skills &lt;list&gt;</code> <span class="docs-tag optional">optional</span></td><td>Comma-separated agent frameworks to configure. See the table below for what each framework writes.</td></tr>
            </tbody>
          </table>

          <h2>What each --skills framework writes</h2>
          <table>
            <thead><tr><th>Framework</th><th>What it writes</th></tr></thead>
            <tbody>
              <tr><td><code>claude-code</code></td><td>MCP entry in <code>~/.claude.json</code> (global, picked up by CLI, VSCode extension, and desktop app); <code>CLAUDE.md</code> block; skill files in <code>.claude/skills/</code></td></tr>
              <tr><td><code>claude-desktop</code></td><td>MCP entry in the Claude Desktop config file with <code>AVC_PROJECT</code> env var set — required because Claude Desktop spawns the server without a project CWD</td></tr>
              <tr><td><code>cursor</code></td><td>MCP entry in <code>~/.cursor/mcp.json</code>; rules file in <code>.cursor/rules/avc.mdc</code></td></tr>
              <tr><td><code>windsurf</code></td><td>MCP entry in <code>~/.codeium/windsurf/mcp_config.json</code>; rules block appended to <code>.windsurfrules</code></td></tr>
              <tr><td><code>generic</code></td><td>Writes <code>AGENT_INSTRUCTIONS.md</code> to the project root — framework-agnostic instructions for any MCP-capable agent</td></tr>
            </tbody>
          </table>

          <h2>JSON output</h2>
          <pre><code>{
  "id": "proj-a1b2c3",
  "path": "/path/to/project",
  "name": "project",
  "already_initialized": false,
  "skills": [],
  "success": true
}</code></pre>

          <p>Safe to run more than once. Re-running on an already-initialized project reports <code>"already_initialized": true</code> and leaves all existing snapshots, branches, and config untouched. If the target directory does not exist, <code>avc init</code> creates it.</p>
        `,
      },
    ],
  },
  {
    id: 'snapshots',
    section: 'Snapshots',
    commands: [
      {
        id: 'snapshot',
        title: 'avc snapshot',
        synopsis: 'Save the current project state.',
        body: `
          <p class="lede">Walks the project, hashes every file, and creates a content-addressed snapshot in the database. Identical files across snapshots share a single object — storage is automatically deduplicated.</p>

          <h2>Usage</h2>
          <pre><code>avc snapshot "Before refactor"
avc snapshot "v1.2.0 release" --agent "claude" --notes "Passed tests"
avc snapshot "WIP" --json</code></pre>

          <h2>Arguments</h2>
          <table>
            <thead><tr><th>Argument</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>&lt;label&gt;</code> <span class="docs-tag required">required</span></td><td>Short human-readable description</td></tr>
            </tbody>
          </table>

          <h2>Flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--agent &lt;name&gt;</code></td><td>Agent or user name creating the snapshot</td></tr>
              <tr><td><code>--notes &lt;text&gt;</code></td><td>Free-form notes attached to the snapshot</td></tr>
            </tbody>
          </table>

          <h2>JSON output</h2>
          <pre><code>{
  "id": "snap-xyz789",
  "label": "v1.2.0 release",
  "timestamp": 1712289600,
  "agent_name": "claude",
  "files_changed": 42,
  "total_size": 1048576,
  "notes": "Passed tests",
  "branch_id": "br-main",
  "success": true
}</code></pre>

          <p>Files matching <code>.avcignore</code> patterns are excluded. The <code>.avc/</code> directory is always excluded.</p>
        `,
      },
      {
        id: 'list',
        title: 'avc list',
        synopsis: 'List snapshots on the active branch.',
        body: `
          <p class="lede">Lists all snapshots on the current branch, newest first.</p>

          <h2>Usage</h2>
          <pre><code>avc list
avc list --json</code></pre>

          <h2>JSON output</h2>
          <pre><code>[
  {
    "id": "snap-def456",
    "label": "Fixed bug in auth",
    "timestamp": 1712282400,
    "agent_name": "claude",
    "files_changed": 3,
    "total_size": 512000,
    "notes": "Security patch",
    "branch_id": "br-main"
  }
]</code></pre>

          <p>Returns an empty array if no snapshots exist.</p>
        `,
      },
      {
        id: 'info',
        title: 'avc info',
        synopsis: 'Show metadata and file list for a snapshot.',
        body: `
          <p class="lede">Detailed view of a single snapshot, including every file with its hash and size.</p>

          <h2>Usage</h2>
          <pre><code>avc info snap-abc123
avc info snap-abc123 --json</code></pre>

          <h2>JSON output</h2>
          <pre><code>{
  "id": "snap-abc123",
  "label": "Before refactor",
  "timestamp": 1712275200,
  "agent_name": "claude",
  "notes": "Stable baseline",
  "file_count": 12,
  "total_size": 524288,
  "files": [
    { "path": "main.go",     "hash": "abc...", "size": 1024 },
    { "path": "src/auth.go", "hash": "def...", "size": 4096 }
  ]
}</code></pre>
        `,
      },
      {
        id: 'log',
        title: 'avc log',
        synopsis: 'Tree-style history of snapshots.',
        body: `
          <p class="lede">Shows a tree-style history of every snapshot on the current branch, newest first, with metadata on each node.</p>

          <h2>Usage</h2>
          <pre><code>avc log
avc log --json</code></pre>

          <h2>Example output</h2>
          <pre><code>  Snapshot history
  ──────────────────────────────────────────────────

  ◆  snap-def456  After agent run
  │  2026-05-18 14:30:00  agent: claude  files: 42
  │
  ◆  snap-abc123  Baseline
  │  2026-05-18 09:00:00  agent: —  files: 40
  │
  ◎  root</code></pre>

          <h2>JSON output</h2>
          <pre><code>[
  {
    "id": "snap-def456",
    "label": "After agent run",
    "timestamp": 1716042600,
    "agent_name": "claude",
    "notes": "Refactored auth",
    "file_count": 42,
    "total_size": 1048576,
    "branch_id": "branch-main"
  }
]</code></pre>
        `,
      },
      {
        id: 'delete',
        title: 'avc delete',
        synopsis: 'Delete a snapshot and unreferenced objects.',
        body: `
          <p class="lede">Permanently removes a snapshot record and its associated file entries. Object blobs shared with other snapshots are preserved; only unreferenced blobs are removed.</p>

          <h2>Usage</h2>
          <pre><code>avc delete snap-abc123
avc delete snap-abc123 --json</code></pre>

          <h2>JSON output</h2>
          <pre><code>{
  "id": "snap-abc123",
  "success": true
}</code></pre>

          <blockquote>This is permanent. The snapshot cannot be recovered. If you might need it, take a note of the ID first.</blockquote>
        `,
      },
    ],
  },
  {
    id: 'diff-restore',
    section: 'Diff & Restore',
    commands: [
      {
        id: 'diff',
        title: 'avc diff',
        synopsis: 'Compare two snapshots file-by-file.',
        body: `
          <p class="lede">Shows added, modified, and deleted files between two snapshots, with line-level counts and unified diff previews.</p>

          <h2>Usage</h2>
          <pre><code>avc diff snap-abc123 snap-def456
avc diff snap-abc123 snap-def456 --json</code></pre>

          <h2>JSON output</h2>
          <pre><code>{
  "from_snapshot": "snap-abc123",
  "to_snapshot": "snap-def456",
  "files": [
    {
      "path": "src/auth.go",
      "type": "modified",
      "old_hash": "abc...",
      "new_hash": "def...",
      "lines_added": 5,
      "lines_removed": 2,
      "diff_preview": "+func NewAuth() Auth {\\n+  return &authImpl{}\\n"
    }
  ]
}</code></pre>

          <p><strong>Change types:</strong> <code>added</code> · <code>modified</code> · <code>deleted</code></p>
        `,
      },
      {
        id: 'diff-current',
        title: 'avc diff-current',
        synopsis: 'Compare a snapshot against the working tree.',
        body: `
          <p class="lede">Shows what's different between a snapshot and the current files on disk. Same JSON shape as <code>avc diff</code>.</p>

          <h2>Usage</h2>
          <pre><code>avc diff-current snap-abc123
avc diff-current snap-abc123 --json</code></pre>
        `,
      },
      {
        id: 'restore',
        title: 'avc restore',
        synopsis: 'Roll back the project to a snapshot.',
        body: `
          <p class="lede">Rolls the entire project back to the exact state captured in a snapshot. Every tracked file is overwritten with its snapshot version, and any files added after the snapshot are deleted.</p>

          <h2>Usage</h2>
          <pre><code>avc restore snap-abc123
avc restore snap-abc123 --json</code></pre>

          <h2>JSON output</h2>
          <pre><code>{
  "id": "snap-abc123",
  "restored_files": 42,
  "restored_size": 1048576,
  "success": true
}</code></pre>

          <blockquote>Files added after the snapshot are <strong>deleted</strong> during restore — not just untouched. Take a snapshot of the current state first if there is any work you want to keep.</blockquote>
        `,
      },
      {
        id: 'restore-file',
        title: 'avc restore-file',
        synopsis: 'Restore a single file from a snapshot.',
        body: `
          <p class="lede">Restores one file rather than the whole snapshot.</p>

          <h2>Usage</h2>
          <pre><code>avc restore-file snap-abc123 src/auth.go
avc restore-file snap-abc123 src/auth.go --json</code></pre>
        `,
      },
      {
        id: 'cat',
        title: 'avc cat',
        synopsis: 'Print file contents from a snapshot.',
        body: `
          <p class="lede">Prints the contents of a file as stored in a snapshot. When run in a terminal, displays a formatted view with line numbers. When piped or redirected, outputs raw bytes — safe for binary files.</p>

          <h2>Usage</h2>
          <pre><code>avc cat snap-abc123 src/auth.go           # formatted view in terminal
avc cat snap-abc123 src/auth.go > out.go  # raw bytes (pipe-safe)
avc cat snap-abc123 src/auth.go --json    # base64-encoded content</code></pre>

          <p>The terminal view strips UTF-8, UTF-16 LE, and UTF-16 BE byte-order marks before display. The piped / redirected path always writes the original bytes unchanged.</p>
        `,
      },
      {
        id: 'file-history',
        title: 'avc file-history',
        synopsis: 'List snapshots that contain a file.',
        body: `
          <p class="lede">Shows every snapshot that contains a given file, with its hash and size in each one.</p>

          <h2>Usage</h2>
          <pre><code>avc file-history src/auth.go
avc file-history src/auth.go --json</code></pre>

          <h2>JSON output</h2>
          <pre><code>[
  {
    "snapshot_id": "snap-def456",
    "label": "Fixed auth bug",
    "timestamp": 1712282400,
    "agent_name": "claude",
    "hash": "def456...",
    "size": 4096
  }
]</code></pre>
        `,
      },
      {
        id: 'annotate',
        title: 'avc annotate',
        synopsis: 'Show which snapshot introduced each line.',
        body: `
          <p class="lede">Like <code>git blame</code> but for AVC snapshots. Traces every line in a file back to the snapshot that first introduced it, using LCS diffing across the snapshot history.</p>

          <h2>Usage</h2>
          <pre><code>avc annotate src/auth.go
avc annotate src/auth.go --json</code></pre>

          <h2>Example output</h2>
          <pre><code>src/auth.go  (12 lines)

   1 │ initial commit
   2 │ initial commit
   3 │ add JWT support
   4 │ add JWT support
   5 │ initial commit</code></pre>

          <p>Each row shows the line number, a separator, and the label of the snapshot that introduced that line. If an agent name was recorded, it appears beside the label.</p>

          <h2>JSON output</h2>
          <pre><code>{
  "file_path": "src/auth.go",
  "total_lines": 12,
  "lines": [
    {
      "line": 1,
      "snapshot_id": "snap-abc123",
      "label": "initial commit",
      "agent_name": "",
      "timestamp": 1716000000
    },
    {
      "line": 3,
      "snapshot_id": "snap-def456",
      "label": "add JWT support",
      "agent_name": "claude",
      "timestamp": 1716042600
    }
  ]
}</code></pre>

          <p>Lines not yet captured in any snapshot are attributed to <code>"(untracked)"</code>.</p>
        `,
      },
    ],
  },
  {
    id: 'branches',
    section: 'Branches',
    commands: [
      {
        id: 'branch',
        title: 'avc branch',
        synopsis: 'Create and manage branches (agent workspaces).',
        body: `
          <p class="lede">Branches isolate agent work in <code>.avc/workspaces/&lt;branch&gt;/</code> so experiments never touch your real project until you merge.</p>

          <h2>Subcommands</h2>
          <pre><code>avc branch create &lt;branch&gt;              # create from current HEAD of main
avc branch create &lt;branch&gt; --from &lt;id&gt;  # create from a specific snapshot
avc branch list                           # list all branches
avc branch switch &lt;branch&gt;              # switch active branch
avc branch delete &lt;branch&gt;              # delete a branch and its workspace
avc branch diff [branch]                  # cumulative diff from branch point to HEAD
                                          # omit branch to diff the active branch</code></pre>

          <h2>branch create flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--from &lt;snapshot-id&gt;</code> <span class="docs-tag optional">optional</span></td><td>Base the new branch on a specific snapshot instead of the current HEAD of main</td></tr>
            </tbody>
          </table>

          <h2>Workflow example</h2>
          <pre><code># Start a new agent task on a branch
avc branch create feature/refactor-auth

# Agent operates in .avc/workspaces/feature/refactor-auth/
# Snapshot as usual — they land on the branch
avc snapshot "WIP refactor"

# Review cumulative changes
avc branch diff feature/refactor-auth

# Merge back when done (performs the merge directly; conflicts get markers)
avc merge feature/refactor-auth

# If the merge went wrong, abort restores main from the auto-snapshot
avc merge --abort</code></pre>
        `,
      },
    ],
  },
  {
    id: 'merge',
    section: 'Merge',
    commands: [
      {
        id: 'merge',
        title: 'avc merge',
        synopsis: 'Merge a branch into main with conflict detection.',
        body: `
          <p class="lede">Three-way merge using the branch point, main HEAD, and branch HEAD. Clean changes auto-apply; conflicting files get standard <code>&lt;&lt;&lt; / === / &gt;&gt;&gt;</code> markers in the working tree.</p>

          <h2>Usage</h2>
          <pre><code>avc merge feature/refactor-auth    # perform the merge (conflicts get markers)
avc merge --abort                  # restore main from the pre-merge safety snapshot</code></pre>

          <h2>Flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--abort</code></td><td>Restore main from the auto-snapshot taken before the last merge</td></tr>
            </tbody>
          </table>

          <p>Before every merge, an automatic safety snapshot is taken so <code>--abort</code> can fully reverse a bad merge. To preview merge counts before committing, use the web UI's <strong>Merge Preview</strong> panel or call <code>GET /api/merge/preview?branch=&lt;name&gt;</code> directly.</p>
        `,
      },
    ],
  },
  {
    id: 'search-status',
    section: 'Search & Status',
    commands: [
      {
        id: 'search',
        title: 'avc search',
        synopsis: 'Search snapshot labels and notes.',
        body: `
          <p class="lede">Searches across all snapshot labels and notes on the active branch. An alias for <code>avc list --search &lt;query&gt;</code>.</p>

          <h2>Usage</h2>
          <pre><code>avc search "auth refactor"
avc search "before deploy" --json</code></pre>

          <h2>Arguments</h2>
          <table>
            <thead><tr><th>Argument</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>&lt;query&gt;</code> <span class="docs-tag required">required</span></td><td>Text to search in labels and notes (case-insensitive)</td></tr>
            </tbody>
          </table>

          <h2>JSON output</h2>
          <p>Same shape as <code>avc list --json</code> — an array of matching snapshot objects.</p>

          <pre><code>[
  {
    "id": "snap-abc123",
    "label": "Before auth refactor",
    "timestamp": 1712275200,
    "agent_name": "claude",
    "files_changed": 8,
    "total_size": 204800,
    "notes": "stable baseline before rewriting auth module",
    "branch_id": "br-main"
  }
]</code></pre>

          <p>Returns an empty array when no snapshots match.</p>
        `,
      },
      {
        id: 'status',
        title: 'avc status',
        synopsis: 'Show working tree changes since the last snapshot.',
        body: `
          <p class="lede">Compares the current working tree against the last snapshot on the active branch. On an agent branch, compares the branch workspace rather than the real project root.</p>

          <h2>Usage</h2>
          <pre><code>avc status
avc status --json</code></pre>

          <h2>Example output</h2>
          <pre><code>  Branch: main  ·  Since: Before refactor

  M  src/auth.go          +12 -3
  A  src/auth_test.go     +45
  D  src/legacy.go        -120</code></pre>

          <p>Output mirrors <code>git status</code> — one line per changed file with an <code>A</code> (added), <code>M</code> (modified), or <code>D</code> (deleted) prefix, followed by line counts.</p>

          <h2>JSON output</h2>
          <pre><code>{
  "branch_name": "main",
  "snapshot_id": "snap-abc123",
  "snapshot_label": "Before refactor",
  "files": [
    { "path": "src/auth.go",      "type": "modified", "lines_added": 12, "lines_removed": 3 },
    { "path": "src/auth_test.go", "type": "added",    "lines_added": 45, "lines_removed": 0 },
    { "path": "src/legacy.go",    "type": "deleted",  "lines_added": 0,  "lines_removed": 120 }
  ]
}</code></pre>

          <p>Returns an empty <code>files</code> array when the working tree is clean.</p>
        `,
      },
    ],
  },
  {
    id: 'portability',
    section: 'Portability',
    commands: [
      {
        id: 'export',
        title: 'avc export',
        synopsis: 'Export snapshots and objects to a portable bundle.',
        body: `
          <p class="lede">Packages the AVC database and all content-addressed blobs into a <code>.avc.tar.gz</code> bundle that can be imported on another machine with <code>avc import</code>.</p>

          <h2>Usage</h2>
          <pre><code>avc export                                    # export all branches
avc export --branch feature-x                 # export one branch only
avc export --output ./backups/project.avc.tar.gz   # custom output path
avc export --json                             # machine-readable manifest</code></pre>

          <h2>Flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--branch &lt;name&gt;</code></td><td>Export only this branch's snapshots and objects</td></tr>
              <tr><td><code>--output &lt;path&gt;</code></td><td>Output path for the bundle (default: <code>&lt;project&gt;-&lt;timestamp&gt;.avc.tar.gz</code> in CWD)</td></tr>
            </tbody>
          </table>

          <h2>Bundle contents</h2>
          <ul>
            <li><code>avc-export.json</code> — manifest with version, counts, and branch list</li>
            <li><code>schema.sql</code> — database rows as <code>INSERT OR IGNORE</code> statements</li>
            <li><code>objects/</code> — all referenced content-addressed blobs</li>
          </ul>

          <h2>JSON output</h2>
          <pre><code>{
  "version": "1",
  "project_name": "my-project",
  "exported_at": 1716042600,
  "branches": ["main", "feature-x"],
  "snapshot_count": 12,
  "object_count": 384
}</code></pre>
        `,
      },
      {
        id: 'import',
        title: 'avc import',
        synopsis: 'Import a bundle into the current project.',
        body: `
          <p class="lede">Merges snapshots, branches, and file objects from a bundle created by <code>avc export</code> into the current project. Safe to run repeatedly — existing snapshots and blobs are never overwritten.</p>

          <h2>Usage</h2>
          <pre><code>avc import --from ./project-20260524.avc.tar.gz
avc import --from ./project-20260524.avc.tar.gz --json</code></pre>

          <h2>Flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--from &lt;path&gt;</code> <span class="docs-tag required">required</span></td><td>Path to the <code>.avc.tar.gz</code> bundle</td></tr>
            </tbody>
          </table>

          <h2>How it works</h2>
          <ul>
            <li>Blobs already present in <code>.avc/objects/</code> are silently skipped — no duplicate writes</li>
            <li>Database rows use <code>INSERT OR IGNORE</code> — snapshots with the same ID are left unchanged</li>
            <li>Foreign key constraints are deferred during replay so import order doesn't matter</li>
            <li>Version mismatch between the bundle and the current AVC installation is detected and rejected</li>
          </ul>

          <h2>JSON output</h2>
          <pre><code>{
  "project_name": "my-project",
  "bundle_path": "./project-20260524.avc.tar.gz",
  "snapshot_count": 12,
  "object_count": 384,
  "skipped_rows": 3
}</code></pre>

          <p><code>skipped_rows</code> is the number of database rows that already existed and were left unchanged.</p>
        `,
      },
    ],
  },
  {
    id: 'maintenance',
    section: 'Maintenance',
    commands: [
      {
        id: 'gc',
        title: 'avc gc',
        synopsis: 'Find and remove orphaned blobs.',
        body: `
          <p class="lede">Scans <code>.avc/objects/</code> and identifies blobs no longer referenced by any snapshot. By default this is a dry run — it reports what would be deleted without removing anything.</p>

          <h2>Usage</h2>
          <pre><code>avc gc          # dry run — list orphaned blobs and total size
avc gc --run    # actually delete them and reclaim disk space
avc gc --json   # machine-readable output</code></pre>

          <h2>Flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--run</code></td><td>Delete the orphaned blobs (default is dry run)</td></tr>
            </tbody>
          </table>

          <h2>JSON output</h2>
          <pre><code>{
  "orphaned_count": 14,
  "orphaned_bytes": 2097152,
  "deleted": false
}</code></pre>

          <p>When <code>--run</code> is passed, <code>"deleted"</code> is <code>true</code> and <code>orphaned_count</code> reflects how many blobs were removed.</p>

          <blockquote>Blobs become orphaned when a snapshot is deleted and the blobs it referenced aren't shared with any other snapshot. Run <code>avc gc</code> periodically to reclaim disk space after heavy snapshot churn.</blockquote>
        `,
      },
      {
        id: 'storage-cmd',
        title: 'avc storage',
        synopsis: 'Show disk usage breakdown for .avc/',
        body: `
          <p class="lede">Reports how much disk space AVC is using, broken down by component.</p>

          <h2>Usage</h2>
          <pre><code>avc storage
avc storage --json</code></pre>

          <h2>Example output</h2>
          <pre><code>  AVC storage usage

  Database      1.2 MB   .avc/avc.db
  Objects      48.7 MB   .avc/objects/  (1,204 blobs)
  Workspaces    3.1 MB   .avc/workspaces/
  ─────────────────────
  Total        53.0 MB</code></pre>

          <h2>JSON output</h2>
          <pre><code>{
  "database_bytes": 1258291,
  "object_bytes": 51068108,
  "object_count": 1204,
  "workspace_bytes": 3250380,
  "total_bytes": 55576779
}</code></pre>
        `,
      },
      {
        id: 'cache',
        title: 'avc cache',
        synopsis: 'Manage the diff cache.',
        body: `
          <p class="lede">AVC caches computed diffs in the database to speed up repeated queries. Use <code>avc cache stats</code> to see cache size, or <code>avc cache clear</code> to reset it.</p>

          <h2>Usage</h2>
          <pre><code>avc cache stats    # show cache entry count and size
avc cache clear    # delete all cached diffs
avc cache --json   # machine-readable stats</code></pre>

          <h2>Subcommands</h2>
          <table>
            <thead><tr><th>Subcommand</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>stats</code></td><td>Show the number of cached diff entries and their total size</td></tr>
              <tr><td><code>clear</code></td><td>Delete all cached diffs (the cache rebuilds automatically on next use)</td></tr>
            </tbody>
          </table>

          <blockquote class="callout-tip">The cache is an optimization — clearing it is always safe. AVC will recompute diffs on the next request and re-populate the cache automatically.</blockquote>
        `,
      },
    ],
  },
  {
    id: 'agents',
    section: 'Agents & MCP',
    commands: [
      {
        id: 'mcp',
        title: 'avc mcp serve',
        synopsis: 'JSON-RPC 2.0 server for AI agents.',
        body: `
          <p class="lede">Starts an MCP (Model Context Protocol) server over stdio, exposing AVC commands as tools that AI agents can call directly.</p>

          <h2>Usage</h2>
          <pre><code>avc mcp serve                      # standard tier (11 tools) — recommended default
avc mcp serve --tools core         # 4 essential tools only (lowest token cost)
avc mcp serve --tools standard     # 11 tools — snapshots, branches, merge, status
avc mcp serve --tools full         # all 24 tools — includes advanced branch/merge/tag ops
avc mcp serve --compact            # compact JSON output for token-sensitive contexts</code></pre>

          <h2>Flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--tools &lt;tier&gt;</code></td><td>Tool tier to expose: <code>core</code>, <code>standard</code> (default), or <code>full</code></td></tr>
              <tr><td><code>--compact</code></td><td>Emit compact JSON instead of pretty-printed (saves tokens)</td></tr>
            </tbody>
          </table>

          <h2>Tool tiers</h2>
          <table>
            <thead><tr><th>Tier</th><th>Count</th><th>Best for</th></tr></thead>
            <tbody>
              <tr><td><code>core</code></td><td>4</td><td>Token-constrained agents — snapshot, list, diff, restore only</td></tr>
              <tr><td><code>standard</code></td><td>11</td><td>Everyday agent use — core + branches, merge, status</td></tr>
              <tr><td><code>full</code></td><td>24</td><td>Power users — adds rename, annotate, tagging, conflict resolution</td></tr>
            </tbody>
          </table>

          <h2>Core tools (4)</h2>
          <ul>
            <li><code>avc_snapshot</code>, <code>avc_list</code>, <code>avc_diff</code>, <code>avc_restore</code></li>
          </ul>

          <h2>Standard tools (11)</h2>
          <ul>
            <li><code>avc_snapshot</code>, <code>avc_list</code>, <code>avc_diff</code>, <code>avc_restore</code>, <code>avc_status</code></li>
            <li><code>avc_branch_create</code>, <code>avc_branch_list</code>, <code>avc_branch_switch</code>, <code>avc_branch_diff</code></li>
            <li><code>avc_merge</code>, <code>avc_merge_abort</code></li>
          </ul>

          <h2>Full tools (24)</h2>
          <ul>
            <li>All standard tools, plus:</li>
            <li><code>avc_info</code>, <code>avc_delete</code>, <code>avc_restore_file</code>, <code>avc_annotate</code></li>
            <li><code>avc_branch_rename</code>, <code>avc_branch_abandon</code>, <code>avc_branch_prune_merged</code></li>
            <li><code>avc_merge_preview</code>, <code>avc_list_conflicts</code>, <code>avc_resolve_conflict</code></li>
            <li><code>avc_tag_snapshot</code>, <code>avc_untag_snapshot</code>, <code>avc_run_in_workspace</code></li>
          </ul>

          <p>Agent integration files (<code>.claude/skills/</code>, <code>.cursor/rules/</code>, etc.) are written by <code>avc init --skills</code>.</p>
        `,
      },
      {
        id: 'run',
        title: 'avc run',
        synopsis: 'Run a command inside a branch workspace.',
        body: `
          <p class="lede">Executes a shell command inside the materialized workspace for a branch. The command runs with environment scrubbing, an execution timeout, and process-tree kill on timeout.</p>

          <h2>Usage</h2>
          <pre><code>avc run --branch feature-x "npm test"
avc run --branch feature-x "go build ./..."
avc run --branch feature-x --timeout 300 "pip install -r requirements.txt"</code></pre>

          <h2>Flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--branch &lt;name&gt;</code> <span class="docs-tag required">required</span></td><td>Branch whose workspace to run the command in</td></tr>
              <tr><td><code>--timeout &lt;seconds&gt;</code></td><td>Override the execution timeout (default and max are set in <code>.avc/config.toml</code>)</td></tr>
            </tbody>
          </table>

          <h2>Sandbox rules</h2>
          <ul>
            <li>System package managers (<code>brew</code>, <code>apt</code>, <code>choco</code>, <code>sudo</code>) are blocked</li>
            <li><strong>Python</strong> — <code>pip install</code> auto-redirects into a workspace-local venv. Never use <code>--user</code> or <code>--system</code></li>
            <li><strong>Node</strong> — <code>npm install</code> installs into workspace <code>node_modules</code>. Never use <code>-g</code> or <code>--global</code></li>
          </ul>

          <blockquote class="callout-warn">Always show the user the exact command before running it. The MCP tool <code>avc_run_in_workspace</code> enforces this by requiring explicit approval before every call.</blockquote>

          <h2>JSON output</h2>
          <pre><code>{
  "exit_code": 0,
  "stdout": "ok  github.com/example/app\\n",
  "stderr": "",
  "workspace_path": ".avc/workspaces/feature-x"
}</code></pre>

          <p>The command's exit code is propagated to the shell. On timeout, the entire process tree is killed before the timeout error is returned.</p>
        `,
      },
    ],
  },
  {
    id: 'web',
    section: 'Web UI',
    commands: [
      {
        id: 'ui',
        title: 'avc ui',
        synopsis: 'Start the standalone web UI server.',
        body: `
          <p class="lede">Serves a graphical interface at <code>http://127.0.0.1:3004</code> for users who don't run VSCode. Auto-opens your browser. Same features as the VSCode extension.</p>

          <h2>Usage</h2>
          <pre><code>avc ui                             # default port 3004, auto-open
avc ui --port 8080                 # custom port
avc ui --no-open                   # don't open browser (headless / SSH)
avc ui --host 0.0.0.0              # bind all interfaces (LAN access)</code></pre>

          <h2>Flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--port &lt;n&gt;</code></td><td>Port to listen on (default <code>3004</code>)</td></tr>
              <tr><td><code>--host &lt;addr&gt;</code></td><td>Bind host (default <code>127.0.0.1</code> — localhost only)</td></tr>
              <tr><td><code>--no-open</code></td><td>Don't open the browser automatically</td></tr>
            </tbody>
          </table>

          <blockquote>Binding to <code>0.0.0.0</code> exposes the UI to your local network with no authentication. Only use on trusted networks.</blockquote>

          <h2>API endpoints</h2>
          <p>The UI is also a REST API. All routes return JSON.</p>
          <table>
            <thead><tr><th>Method</th><th>Route</th><th>Purpose</th></tr></thead>
            <tbody>
              <tr><td colspan="3" style="background:var(--bg-2);font-size:.75rem;letter-spacing:.05em">SNAPSHOTS</td></tr>
              <tr><td>GET</td><td><code>/api/project</code></td><td>Project name, path, active branch</td></tr>
              <tr><td>GET</td><td><code>/api/snapshots</code></td><td>List snapshots on active branch</td></tr>
              <tr><td>POST</td><td><code>/api/snapshots/create</code></td><td>Create a snapshot</td></tr>
              <tr><td>GET</td><td><code>/api/snapshots/&lt;id&gt;</code></td><td>Snapshot detail with file list</td></tr>
              <tr><td>DELETE</td><td><code>/api/snapshots/&lt;id&gt;</code></td><td>Delete a snapshot</td></tr>
              <tr><td>GET</td><td><code>/api/diff?from=&amp;to=</code></td><td>Diff two snapshots</td></tr>
              <tr><td>GET</td><td><code>/api/diff-current?id=</code></td><td>Diff snapshot vs working tree</td></tr>
              <tr><td>POST</td><td><code>/api/restore</code></td><td>Restore full snapshot</td></tr>
              <tr><td>POST</td><td><code>/api/restore-file</code></td><td>Restore single file</td></tr>
              <tr><td colspan="3" style="background:var(--bg-2);font-size:.75rem;letter-spacing:.05em">BRANCHES</td></tr>
              <tr><td>GET</td><td><code>/api/branches</code></td><td>List all branches with active branch name</td></tr>
              <tr><td>POST</td><td><code>/api/branches</code></td><td>Create a new branch</td></tr>
              <tr><td>POST</td><td><code>/api/branches/switch</code></td><td>Switch active branch</td></tr>
              <tr><td>DELETE</td><td><code>/api/branches/&lt;name&gt;</code></td><td>Delete a branch</td></tr>
              <tr><td>GET</td><td><code>/api/branches/&lt;name&gt;/diff</code></td><td>Cumulative diff from branch point to HEAD</td></tr>
              <tr><td colspan="3" style="background:var(--bg-2);font-size:.75rem;letter-spacing:.05em">MERGE</td></tr>
              <tr><td>GET</td><td><code>/api/merge/preview?branch=</code></td><td>Merge dry-run — returns clean/conflict/skipped counts</td></tr>
              <tr><td>POST</td><td><code>/api/merge</code></td><td>Execute the merge</td></tr>
              <tr><td>POST</td><td><code>/api/merge/abort</code></td><td>Restore main from pre-merge safety snapshot</td></tr>
              <tr><td colspan="3" style="background:var(--bg-2);font-size:.75rem;letter-spacing:.05em">STATUS &amp; STORAGE</td></tr>
              <tr><td>GET</td><td><code>/api/status</code></td><td>Working tree changes since last snapshot</td></tr>
              <tr><td>GET</td><td><code>/api/storage</code></td><td>Disk usage breakdown for <code>.avc/</code></td></tr>
            </tbody>
          </table>
        `,
      },
    ],
  },
  {
    id: 'extension',
    section: 'VSCode Extension',
    commands: [
      {
        id: 'ext-install',
        title: 'Install & Setup',
        synopsis: 'Get the AVC VSCode extension running in three steps.',
        body: `
          <p class="lede">The AVC extension brings snapshot management directly into VSCode — sidebar, Source Control panel, gutter annotations, and more. Pick the install path that matches how you got AVC.</p>

          <h2>Prerequisites</h2>
          <ul>
            <li>The <code>avc</code> CLI must be on your <code>PATH</code> (run <code>avc --version</code> to confirm)</li>
            <li>Your project must be initialized with <code>avc init</code></li>
            <li>VSCode <strong>1.85+</strong></li>
          </ul>

          <h2>Option A — Marketplace</h2>
          <p>The simplest install once published:</p>
          <ol class="docs-steps">
            <li>Open VSCode and press <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>X</kbd> to open the Extensions panel</li>
            <li>Search for <strong>AVC — Agentic Version Control</strong></li>
            <li>Click <strong>Install</strong>, then reload VSCode if prompted</li>
          </ol>

          <h2>Option B — Local VSIX</h2>
          <p>Useful for testing pre-release builds:</p>
          <ol class="docs-steps">
            <li>Run <code>vsce package</code> in the <code class="path">extension/</code> folder to produce <code>avc-0.2.0.vsix</code></li>
            <li>In VSCode: <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>P</kbd> → <strong>Extensions: Install from VSIX…</strong></li>
            <li>Pick the generated <code>.vsix</code> file</li>
          </ol>

          <h2>Option C — Development mode</h2>
          <p>For contributing to the extension itself:</p>
          <ol class="docs-steps">
            <li>Open the <code class="path">extension/</code> folder in VSCode: <code>code extension/</code></li>
            <li>Run <code>npm install &amp;&amp; npm run compile</code></li>
            <li>Press <kbd>F5</kbd> — a second window opens labeled <strong>[Extension Development Host]</strong></li>
            <li>In that window, open any AVC-initialized folder</li>
          </ol>

          <h2>Verify the install</h2>
          <p>You should see all of the following in your project window:</p>
          <ul>
            <li>A <strong>camera icon</strong> in the activity bar (left edge)</li>
            <li>A <strong>status bar item</strong> like <code>$(history) AVC: 4 snapshots</code></li>
            <li>A <strong>branch indicator</strong> next to it: <code>$(git-branch) main</code></li>
            <li>An <strong>AVC group</strong> in the Source Control panel (<kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>G</kbd>)</li>
          </ul>

          <blockquote class="callout-tip">If the sidebar shows "Loading…" forever, run <code>avc --version</code> in your terminal. If it errors, the extension can't find the CLI — set <code>avc.cliPath</code> in settings.</blockquote>
        `,
      },
      {
        id: 'ext-sidebar',
        title: 'Sidebar Overview',
        synopsis: 'Navigate snapshots, branches, and changes from the activity bar.',
        body: `
          <p class="lede">Click the camera icon in the activity bar to open the AVC panel. Everything you need is one or two clicks away.</p>

          <h2>Anatomy</h2>
          <pre><code>┌──────────────────────────────────────────────┐
│ AVC SNAPSHOTS                  [+ ↻ ⌕ ⇄ ⏱]  │  ← header buttons
├──────────────────────────────────────────────┤
│ ▼ Today (3)                                  │
│   ▶ Auto-snapshot     4/19/2026 12:09 AM    │  ← collapsed snapshot
│   ▶ Manual baseline   4/19/2026 12:08 AM    │
│ ▶ Yesterday (1)                              │
│ ▶ This Week (5)                              │
│ ▶ Older (12)                                 │
└──────────────────────────────────────────────┘
$(history) AVC: 21 snapshots  $(git-branch) main  $(diff) +0 ~2 -0</code></pre>

          <h2>Header buttons</h2>
          <table>
            <thead><tr><th>Button</th><th>Command</th><th>What it does</th></tr></thead>
            <tbody>
              <tr><td><strong>+</strong></td><td><code>avc.saveSnapshot</code></td><td>Prompts for label and notes, creates a snapshot</td></tr>
              <tr><td><strong>↻</strong></td><td><code>avc.refreshSnapshots</code></td><td>Reload the snapshot list and SCM stats</td></tr>
              <tr><td><strong>⌕</strong></td><td><code>avc.filterSnapshots</code></td><td>Multi-step filter by Agent / Type / Branch</td></tr>
              <tr><td><strong>⇄</strong></td><td><code>avc.compareTwoSnapshots</code></td><td>Pick any two snapshots to diff</td></tr>
              <tr><td><strong>⏱</strong></td><td><code>avc.showTimeline</code></td><td>Open the visual timeline webview</td></tr>
              <tr><td><strong>⊕</strong></td><td><code>avc.createBranch</code></td><td>Create a new branch from current snapshot</td></tr>
              <tr><td><strong>⨉</strong></td><td><code>avc.mergeBranch</code></td><td>Merge a branch into main with preview</td></tr>
            </tbody>
          </table>

          <h2>Date grouping</h2>
          <p>Snapshots automatically bucket into <strong>Today / Yesterday / This Week / This Month / Older</strong>. Groups start collapsed; click any header to expand. The number next to each group is its snapshot count.</p>

          <h2>Status bar (bottom-left)</h2>
          <table>
            <thead><tr><th>Item</th><th>Click action</th></tr></thead>
            <tbody>
              <tr><td><code>$(history) AVC: N snapshots</code></td><td>Refresh the list</td></tr>
              <tr><td><code>$(git-branch) &lt;branch&gt;</code></td><td>QuickPick to switch branches</td></tr>
              <tr><td><code>$(diff) +A ~M -D</code></td><td>Open diff vs the latest snapshot (only shown when there are changes)</td></tr>
            </tbody>
          </table>
        `,
      },
      {
        id: 'ext-actions',
        title: 'Snapshot Actions',
        synopsis: 'Five buttons appear under each expanded snapshot.',
        body: `
          <p class="lede">Click the chevron next to any snapshot to expand it. Five action rows appear directly below — no hover required, no right-clicking.</p>

          <h2>Available actions</h2>
          <table>
            <thead><tr><th>Action</th><th>Icon</th><th>What it does</th></tr></thead>
            <tbody>
              <tr><td><strong>View Details</strong></td><td><code>$(info)</code></td><td>Opens the snapshot detail webview with the full file tree</td></tr>
              <tr><td><strong>View Diff (vs previous)</strong></td><td><code>$(diff)</code></td><td>Compare against the next-older snapshot in the list</td></tr>
              <tr><td><strong>Diff with Current Files</strong></td><td><code>$(compare-changes)</code></td><td>Compare snapshot against your current working tree</td></tr>
              <tr><td><strong>Restore This Snapshot</strong></td><td><code>$(history)</code></td><td>Roll the entire project back to this snapshot</td></tr>
              <tr><td><strong>Delete Snapshot</strong></td><td><code>$(trash)</code></td><td>Permanently delete this snapshot</td></tr>
            </tbody>
          </table>

          <h2>Confirmation modals</h2>
          <p>Destructive actions (<strong>Restore</strong>, <strong>Delete</strong>) always show a confirmation dialog. Cancelling closes the dialog with no side effects.</p>

          <blockquote class="callout-info"><strong>Safety net.</strong> Before any restore, the extension auto-creates a snapshot labeled <code>"Pre-restore safety snapshot"</code>. If the restore was a mistake, just restore that pre-restore snapshot to undo it.</blockquote>

          <h2>Right-click menu</h2>
          <p>The same five actions are also available in the right-click context menu on any snapshot row, plus they appear in the Command Palette under the <strong>AVC:</strong> prefix.</p>
        `,
      },
      {
        id: 'ext-scm',
        title: 'Source Control Panel',
        synopsis: 'AVC integrates with the standard Source Control view.',
        body: `
          <p class="lede">Open the Source Control panel with <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>G</kbd>. AVC appears as its own section alongside Git, listing every file that's changed since the latest snapshot.</p>

          <h2>What you see</h2>
          <ul>
            <li><strong>Changes since last snapshot</strong> — one row per modified, added, or deleted file</li>
            <li><strong>Color-coded icons</strong> — green for added, yellow for modified, red strikethrough for deleted</li>
            <li><strong>Tooltip</strong> on each file shows the line counts (<code>+5 -2</code>)</li>
            <li><strong>Snapshot input box</strong> at the top with placeholder <em>"Snapshot label..."</em></li>
          </ul>

          <h2>Quick snapshot from the SCM panel</h2>
          <ol class="docs-steps">
            <li>Type a label into the input box (e.g. <code>"Fix login bug"</code>)</li>
            <li>Press <kbd>Ctrl</kbd> + <kbd>Enter</kbd> (or click the checkmark)</li>
            <li>The snapshot is created with your label — equivalent to running <code>avc snapshot "Fix login bug"</code></li>
          </ol>

          <h2>Refresh behavior</h2>
          <p>The panel updates automatically:</p>
          <ul>
            <li>On extension activation</li>
            <li>2 seconds after any file save (debounced)</li>
            <li>After any snapshot, restore, or delete operation</li>
          </ul>

          <blockquote class="callout-tip">If a Git-tracked project also has AVC enabled, you'll see <strong>both</strong> sections in the Source Control panel — Git for repository-level changes, AVC for snapshot-level changes since your last save.</blockquote>
        `,
      },
      {
        id: 'ext-file-history',
        title: 'File History',
        synopsis: 'Right-click any file to see every snapshot it appears in.',
        body: `
          <p class="lede">Like <code>git blame</code> but for AVC snapshots — instantly see every snapshot that contains a file, with quick actions to view, diff, or restore old versions.</p>

          <h2>Where to invoke it</h2>
          <ul>
            <li>Right-click a file in the <strong>Explorer</strong> → <strong>AVC: Show File History</strong></li>
            <li>Right-click anywhere in an open <strong>editor</strong> → <strong>AVC: Show File History</strong></li>
            <li>Right-click an editor <strong>tab title</strong> → <strong>AVC: Show File History</strong></li>
            <li>Or run <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>P</kbd> → <strong>AVC: Show File History</strong> (uses the active editor)</li>
          </ul>

          <h2>The flow</h2>
          <ol class="docs-steps">
            <li>A QuickPick appears listing every snapshot that contains the file, newest first. Each row shows the snapshot label, timestamp, agent, file size, and short hash.</li>
            <li>Pick a version — a second QuickPick offers three actions:
              <ul>
                <li><strong>$(eye) Open in editor</strong> — opens the file as it was in that snapshot, read-only</li>
                <li><strong>$(diff) Diff against current</strong> — opens VSCode's native side-by-side diff editor (with code folding, theme, gutter indicators)</li>
                <li><strong>$(history) Restore this version</strong> — overwrites the working file with the snapshot version (confirmation prompt first)</li>
              </ul>
            </li>
            <li>The chosen action runs immediately. For <strong>Restore</strong>, you'll see a confirmation toast.</li>
          </ol>

          <h2>Native diff editor</h2>
          <p>The "Diff against current" action uses a custom URI scheme (<code>avc-snapshot://</code>) so VSCode's <strong>native</strong> diff editor handles the rendering — same UI as Git diffs, with all the editor features (search, fold, gutter blame, etc.).</p>

          <blockquote class="callout-info">If the file isn't found in any snapshot, you'll see <em>"File not found in any snapshot"</em> instead of an empty QuickPick.</blockquote>
        `,
      },
      {
        id: 'ext-settings',
        title: 'Configuration',
        synopsis: 'Settings to customize the extension behavior.',
        body: `
          <p class="lede">All settings live under <code>avc.*</code>. Edit them in the VSCode settings UI (<kbd>Ctrl</kbd> + <kbd>,</kbd>) or directly in your <code class="path">settings.json</code>.</p>

          <h2>Core settings</h2>
          <table>
            <thead><tr><th>Setting</th><th>Default</th><th>Description</th></tr></thead>
            <tbody>
              <tr>
                <td><code>avc.cliPath</code></td>
                <td><code>"avc"</code></td>
                <td>Path to the <code>avc</code> CLI binary. Override if it's not on <code>PATH</code> — e.g. <code>"C:/Users/you/go/bin/avc.exe"</code></td>
              </tr>
              <tr>
                <td><code>avc.projectPath</code></td>
                <td><code>""</code></td>
                <td>Override the project root. Defaults to the first workspace folder.</td>
              </tr>
              <tr>
                <td><code>avc.defaultAgentName</code></td>
                <td><code>""</code></td>
                <td>Auto-fills the agent name when creating a snapshot. Useful if you always want the same identifier (e.g. your name or <code>"manual"</code>).</td>
              </tr>
            </tbody>
          </table>

          <h2>Auto-snapshot settings</h2>
          <p>The extension can automatically create snapshots after you save files. Disabled by default.</p>
          <table>
            <thead><tr><th>Setting</th><th>Default</th><th>Description</th></tr></thead>
            <tbody>
              <tr>
                <td><code>avc.autoSnapshot.enabled</code></td>
                <td><code>false</code></td>
                <td>Master switch. When <code>true</code>, the extension watches file saves and creates snapshots in the background.</td>
              </tr>
              <tr>
                <td><code>avc.autoSnapshot.debounceSeconds</code></td>
                <td><code>30</code></td>
                <td>Wait this long after the last save before snapshotting. Higher values group more changes into one snapshot.</td>
              </tr>
              <tr>
                <td><code>avc.autoSnapshot.cooldownMinutes</code></td>
                <td><code>5</code></td>
                <td>Minimum gap between auto-snapshots. Prevents the snapshot list from growing too quickly during heavy editing.</td>
              </tr>
            </tbody>
          </table>

          <h2>Example settings.json</h2>
          <pre><code>{
  "avc.cliPath": "avc",
  "avc.defaultAgentName": "manual",
  "avc.autoSnapshot.enabled": true,
  "avc.autoSnapshot.debounceSeconds": 60,
  "avc.autoSnapshot.cooldownMinutes": 10
}</code></pre>

          <blockquote class="callout-warn">Auto-snapshots are labeled <code>"Auto-snapshot"</code> with the configured agent name. They participate in the <strong>Type</strong> filter (sidebar &amp; timeline) so you can hide or show them on demand.</blockquote>

          <h2>Toggle line annotations</h2>
          <p>Run <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>P</kbd> → <strong>AVC: Toggle Line Annotations</strong> to show or hide inline gutter annotations indicating which snapshot introduced each line. There's no setting — it's a per-session toggle.</p>
        `,
      },
    ],
  },
  {
    id: 'reference',
    section: 'Reference',
    commands: [
      {
        id: 'avcignore',
        title: '.avcignore',
        synopsis: 'Exclude files from snapshots.',
        body: `
          <p class="lede">A <code>.avcignore</code> file in the project root excludes patterns from all snapshots. Syntax matches <code>.gitignore</code>.</p>

          <h2>Default patterns (written by <code>avc init</code>)</h2>
          <pre><code>node_modules/
vendor/
dist/
build/
.env
.DS_Store
*.log
.avc/</code></pre>

          <p>The <code>.avc/</code> directory is always excluded regardless of <code>.avcignore</code> contents.</p>
        `,
      },
      {
        id: 'exit-codes',
        title: 'Exit codes',
        synopsis: 'Process exit codes for scripting.',
        body: `
          <p class="lede">Every command returns a standard exit code so you can chain them in shell scripts.</p>

          <table>
            <thead><tr><th>Code</th><th>Meaning</th></tr></thead>
            <tbody>
              <tr><td><code>0</code></td><td>Success</td></tr>
              <tr><td><code>1</code></td><td>General error (snapshot not found, project not initialized, I/O failure, etc.)</td></tr>
            </tbody>
          </table>

          <p>Errors are written to <code>stderr</code>; success output goes to <code>stdout</code>.</p>
        `,
      },
      {
        id: 'storage',
        title: 'Storage layout',
        synopsis: 'How AVC organizes data on disk.',
        body: `
          <p class="lede">AVC uses content-addressed storage with a SQLite index. Each project has a self-contained <code>.avc/</code> directory.</p>

          <h2>Directory structure</h2>
          <pre><code>.avc/
├── avc.db                  # SQLite metadata
├── config.toml             # Project config (active branch, etc.)
├── objects/                # Content-addressed blobs
│   ├── ab/cdef...          # SHA256 hash, sharded by first 2 chars
│   └── ef/12ab...
├── workspaces/             # Branch workspaces (non-main only)
│   └── feature-x/
└── stat-cache.json         # Mtime+size cache for fast snapshots</code></pre>

          <h2>Key principles</h2>
          <ul>
            <li><strong>Content-addressed</strong> — file blobs are stored under their SHA256 hash, so identical files share one object</li>
            <li><strong>Write-once</strong> — objects are immutable; restoring an old snapshot reads the original bytes</li>
            <li><strong>SQLite for metadata, files for content</strong> — the database holds only hashes, sizes, and relational data</li>
          </ul>
        `,
      },
    ],
  },
];

// ── Sidebar rendering ──────────────────────────────────────────────────────
function renderSidebar() {
  const sidebar = document.getElementById('docs-sidebar');
  sidebar.innerHTML = `
    <div class="docs-search-wrap">
      <input type="text" id="docs-search" class="docs-search"
             placeholder="Filter… (press /)" autocomplete="off" spellcheck="false">
    </div>
    ${CATALOG.map(group => `
      <div class="docs-section" data-id="${group.id}">
        <div class="docs-section-header">
          <span class="docs-section-chevron">▼</span>
          <span>${group.section}</span>
          <span class="docs-section-count">${group.commands.length}</span>
        </div>
        <div class="docs-section-items">
          ${group.commands.map(cmd => `
            <a class="docs-link" href="#${cmd.id}" data-id="${cmd.id}">${cmd.title}</a>
          `).join('')}
        </div>
      </div>
    `).join('')}
  `;

  sidebar.querySelectorAll('.docs-section-header').forEach(header => {
    header.onclick = () => header.parentElement.classList.toggle('collapsed');
  });

  document.getElementById('docs-search').addEventListener('input', (e) => {
    const q = e.target.value.toLowerCase().trim();
    document.querySelectorAll('.docs-link').forEach(link => {
      link.style.display = q === '' || link.textContent.toLowerCase().includes(q) ? '' : 'none';
    });
    document.querySelectorAll('.docs-section').forEach(section => {
      const hasVisible = [...section.querySelectorAll('.docs-link')].some(l => l.style.display !== 'none');
      if (q !== '') {
        section.classList.toggle('collapsed', !hasVisible);
      } else {
        section.classList.remove('collapsed');
      }
    });
  });
}

// ── Content rendering ──────────────────────────────────────────────────────
function getAllCommands() {
  return CATALOG.flatMap(g => g.commands);
}

function findCommand(id) {
  for (const group of CATALOG) {
    for (const cmd of group.commands) {
      if (cmd.id === id) return { cmd, group };
    }
  }
  return null;
}

function renderCommand(id) {
  const found = findCommand(id);
  const content = document.getElementById('docs-content');

  if (!found) {
    content.innerHTML = '<div class="docs-empty">Page not found.</div>';
    return;
  }

  const { cmd, group } = found;

  // Prev / next within the flat command list.
  const all = getAllCommands();
  const idx = all.findIndex(c => c.id === id);
  const prev = idx > 0 ? all[idx - 1] : null;
  const next = idx < all.length - 1 ? all[idx + 1] : null;
  const pager = `
    <div class="docs-pager">
      <div class="docs-pager-side">
        ${prev ? `<a href="#${prev.id}" class="docs-pager-btn">← ${escapeHtml(prev.title)}</a>` : ''}
      </div>
      <div class="docs-pager-side right">
        ${next ? `<a href="#${next.id}" class="docs-pager-btn">${escapeHtml(next.title)} →</a>` : ''}
      </div>
    </div>`;

  // Render: title → body (body already opens with its own lede paragraph).
  content.innerHTML = `
    <h1>${escapeHtml(cmd.title)}</h1>
    ${cmd.body}
    ${pager}
  `;

  // Highlight active link, expand parent section.
  document.querySelectorAll('.docs-link').forEach(link => {
    link.classList.toggle('active', link.dataset.id === id);
  });
  document.querySelectorAll('.docs-section').forEach(section => {
    if (section.dataset.id === group.id) {
      section.classList.remove('collapsed');
    }
  });

  // Scroll content to top on navigation.
  content.parentElement.scrollTop = 0;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  }[c]));
}

// ── Hash routing ───────────────────────────────────────────────────────────
function routeFromHash() {
  const hash = window.location.hash.slice(1);
  const id = hash || 'overview';
  renderCommand(id);
}

window.addEventListener('hashchange', routeFromHash);
window.addEventListener('DOMContentLoaded', () => {
  renderSidebar();
  routeFromHash();

  // Press / to focus the search box.
  document.addEventListener('keydown', (e) => {
    if (e.key === '/' && e.target.tagName !== 'INPUT' && e.target.tagName !== 'TEXTAREA') {
      e.preventDefault();
      document.getElementById('docs-search').focus();
    }
    if (e.key === 'Escape') {
      const search = document.getElementById('docs-search');
      if (document.activeElement === search) {
        search.value = '';
        search.dispatchEvent(new Event('input'));
        search.blur();
      }
    }
  });
});
