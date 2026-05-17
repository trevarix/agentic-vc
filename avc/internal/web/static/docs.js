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
avc restore snap-abc123</code></pre>

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
          <pre><code>avc init                         # initialize current directory
avc init /path/to/project        # initialize a specific directory
avc init --skills claude-code    # also write agent integration files</code></pre>

          <h2>Flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--skills &lt;list&gt;</code> <span class="docs-tag optional">optional</span></td><td>Comma-separated agent frameworks to set up. Supported: <code>claude-code</code>, <code>cursor</code>, <code>windsurf</code>, <code>generic</code></td></tr>
            </tbody>
          </table>

          <h2>JSON output</h2>
          <pre><code>{
  "id": "proj-a1b2c3",
  "path": "/path/to/project",
  "name": "project",
  "success": true
}</code></pre>

          <p>Safe to run more than once — re-running on an already-initialized project is a no-op.</p>
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
          <p class="lede">Visual history of snapshots on the current branch.</p>

          <h2>Usage</h2>
          <pre><code>avc log
avc log --json</code></pre>
        `,
      },
      {
        id: 'delete',
        title: 'avc delete',
        synopsis: 'Delete a snapshot and unreferenced objects.',
        body: `
          <p class="lede">Removes a snapshot and any object blobs that are no longer referenced by other snapshots.</p>

          <h2>Usage</h2>
          <pre><code>avc delete snap-abc123
avc delete snap-abc123 --json</code></pre>

          <blockquote>This is permanent. The snapshot cannot be recovered.</blockquote>
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
          <p class="lede">Restores every file in the project to the state captured in the snapshot. Files not in the snapshot are left untouched.</p>

          <h2>Usage</h2>
          <pre><code>avc restore snap-abc123
avc restore snap-abc123 --json</code></pre>

          <blockquote>This overwrites current files. Take a snapshot first if you want to preserve work in progress.</blockquote>
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
          <p class="lede">Outputs the raw bytes of a file as it was stored in a snapshot. Useful for piping into other tools.</p>

          <h2>Usage</h2>
          <pre><code>avc cat snap-abc123 src/auth.go
avc cat snap-abc123 src/auth.go > old-auth.go
avc cat snap-abc123 src/auth.go --json   # base64-encoded for binary safety</code></pre>
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
          <p class="lede">Like <code>git blame</code> but for AVC snapshots. Traces every line in a file back to the snapshot that introduced it.</p>

          <h2>Usage</h2>
          <pre><code>avc annotate src/auth.go
avc annotate src/auth.go --json</code></pre>
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
          <pre><code>avc branch create &lt;name&gt;       # create a branch from the current snapshot
avc branch list                  # list all branches
avc branch switch &lt;name&gt;       # switch active branch
avc branch delete &lt;name&gt;       # delete a branch and its workspace
avc branch diff &lt;name&gt;         # cumulative diff from branch point to HEAD</code></pre>

          <h2>Workflow example</h2>
          <pre><code># Start a new agent task on a branch
avc branch create feature/refactor-auth

# Agent operates in .avc/workspaces/feature/refactor-auth/
# Snapshot as usual — they land on the branch
avc snapshot "WIP refactor"

# Review and merge back when done
avc merge feature/refactor-auth --preview
avc merge feature/refactor-auth</code></pre>
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
          <pre><code>avc merge feature/refactor-auth              # perform the merge
avc merge feature/refactor-auth --preview    # dry-run, show counts only
avc merge --abort                            # restore main from pre-merge snapshot</code></pre>

          <h2>Flags</h2>
          <table>
            <thead><tr><th>Flag</th><th>Description</th></tr></thead>
            <tbody>
              <tr><td><code>--preview</code></td><td>Show counts (clean / conflict / skipped) without modifying files</td></tr>
              <tr><td><code>--abort</code></td><td>Restore main from the auto-snapshot taken before the last merge</td></tr>
            </tbody>
          </table>

          <p>Before every merge, an automatic safety snapshot is taken so <code>--abort</code> can fully reverse a bad merge.</p>
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
          <pre><code>avc mcp serve              # default pretty-printed output
avc mcp serve --compact    # compact JSON for token-sensitive contexts</code></pre>

          <h2>Available tools (14)</h2>
          <ul>
            <li><code>avc_snapshot</code>, <code>avc_list</code>, <code>avc_diff</code>, <code>avc_restore</code>, <code>avc_info</code>, <code>avc_delete</code></li>
            <li><code>avc_branch_create</code>, <code>avc_branch_list</code>, <code>avc_branch_switch</code>, <code>avc_branch_diff</code></li>
            <li><code>avc_merge_preview</code>, <code>avc_merge</code>, <code>avc_merge_abort</code></li>
            <li><code>avc_run_in_workspace</code></li>
          </ul>

          <p>Agent integration files (<code>.claude/skills/</code>, <code>.cursor/rules/</code>, etc.) are written by <code>avc init --skills</code>.</p>
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
              <tr><td>GET</td><td><code>/api/project</code></td><td>Project name, path, active branch</td></tr>
              <tr><td>GET</td><td><code>/api/snapshots</code></td><td>List snapshots on active branch</td></tr>
              <tr><td>POST</td><td><code>/api/snapshots/create</code></td><td>Create a snapshot</td></tr>
              <tr><td>GET</td><td><code>/api/snapshots/&lt;id&gt;</code></td><td>Snapshot detail with file list</td></tr>
              <tr><td>DELETE</td><td><code>/api/snapshots/&lt;id&gt;</code></td><td>Delete a snapshot</td></tr>
              <tr><td>GET</td><td><code>/api/diff?from=&amp;to=</code></td><td>Diff two snapshots</td></tr>
              <tr><td>GET</td><td><code>/api/diff-current?id=</code></td><td>Diff snapshot vs working tree</td></tr>
              <tr><td>POST</td><td><code>/api/restore</code></td><td>Restore full snapshot</td></tr>
              <tr><td>POST</td><td><code>/api/restore-file</code></td><td>Restore single file</td></tr>
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
  sidebar.innerHTML = CATALOG.map(group => `
    <div class="docs-section" data-id="${group.id}">
      <div class="docs-section-header">
        <span class="docs-section-chevron">▼</span>
        <span>${group.section}</span>
      </div>
      <div class="docs-section-items">
        ${group.commands.map(cmd => `
          <a class="docs-link" href="#${cmd.id}" data-id="${cmd.id}">${cmd.title}</a>
        `).join('')}
      </div>
    </div>
  `).join('');

  sidebar.querySelectorAll('.docs-section-header').forEach(header => {
    header.onclick = () => header.parentElement.classList.toggle('collapsed');
  });
}

// ── Content rendering ──────────────────────────────────────────────────────
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
  content.innerHTML = `
    <h1>${escapeHtml(cmd.title)}</h1>
    <p class="lede">${escapeHtml(cmd.synopsis)}</p>
    ${cmd.body}
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
});
