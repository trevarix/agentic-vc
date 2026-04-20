import * as vscode from 'vscode';
import { SnapshotFile, getSnapshotInfo, resolveProjectPath } from './cliProxy';
import { makeNonce, escapeHtml, formatBytes, formatTimestamp, buildCsp, BASE_STYLES } from './webviewUtil';

/** Tree node used to build the folder hierarchy. */
interface TreeNode {
  name: string;
  path: string;
  children: Map<string, TreeNode>;
  file?: SnapshotFile;
}

/** Build a folder tree from flat file paths (slash-separated). */
function buildTree(files: SnapshotFile[]): TreeNode {
  const root: TreeNode = { name: '', path: '', children: new Map() };
  for (const f of files) {
    const parts = f.path.split('/').filter(Boolean);
    let current = root;
    for (let i = 0; i < parts.length; i++) {
      const part = parts[i];
      if (!current.children.has(part)) {
        current.children.set(part, {
          name: part,
          path: parts.slice(0, i + 1).join('/'),
          children: new Map(),
        });
      }
      current = current.children.get(part)!;
    }
    current.file = f;
  }
  return root;
}

/** Count total files under a node. */
function countFiles(node: TreeNode): number {
  let count = node.file ? 1 : 0;
  for (const child of node.children.values()) {
    count += countFiles(child);
  }
  return count;
}

/** Pick a Unicode glyph based on file extension. */
function fileIcon(name: string): string {
  const ext = name.split('.').pop()?.toLowerCase() ?? '';
  const map: Record<string, string> = {
    html: '🌐', htm: '🌐',
    css: '🎨', scss: '🎨', sass: '🎨',
    js: '📜', jsx: '📜', ts: '📜', tsx: '📜',
    json: '📋',
    md: '📝',
    py: '🐍',
    go: '🐹',
    png: '🖼️', jpg: '🖼️', jpeg: '🖼️', gif: '🖼️', svg: '🖼️', webp: '🖼️',
    pdf: '📕',
    zip: '📦', tar: '📦', gz: '📦',
    sh: '⚙️', bash: '⚙️',
    yaml: '⚙️', yml: '⚙️', toml: '⚙️',
  };
  return map[ext] ?? '📄';
}

/** Recursively render a tree node and its children as HTML. */
function renderTree(node: TreeNode, snapshotId: string, depth: number): string {
  const sorted = [...node.children.values()].sort((a, b) => {
    const aIsDir = !a.file;
    const bIsDir = !b.file;
    if (aIsDir && !bIsDir) return -1;
    if (!aIsDir && bIsDir) return 1;
    return a.name.localeCompare(b.name);
  });

  let html = '';
  for (const child of sorted) {
    const isFolder = !child.file;
    const indent = depth * 16 + 4;

    if (isFolder) {
      const fileCount = countFiles(child);
      html += `<div class="tree-folder">
        <div class="tree-row folder-row" style="padding-left:${indent}px">
          <span class="tree-chevron">▼</span>
          <span class="folder-icon">📁</span>
          <span class="tree-name">${escapeHtml(child.name)}</span>
          <span class="tree-meta">${fileCount} file${fileCount === 1 ? '' : 's'}</span>
        </div>
        <div class="tree-children">${renderTree(child, snapshotId, depth + 1)}</div>
      </div>`;
    } else if (child.file) {
      const f = child.file;
      html += `<div class="tree-row file-row" style="padding-left:${indent + 14}px">
        <span class="file-icon">${fileIcon(child.name)}</span>
        <span class="tree-name">${escapeHtml(child.name)}</span>
        <span class="tree-size">${formatBytes(f.size)}</span>
        <span class="tree-hash">${escapeHtml(f.hash.slice(0, 8))}</span>
        <button class="restore-btn" data-path="${escapeHtml(f.path)}">Restore</button>
      </div>`;
    }
  }
  return html;
}

/** Opens a webview panel showing detailed info for a snapshot. */
export async function showInfo(snapshotId: string, label: string): Promise<void> {
  const projectPath = resolveProjectPath();
  if (!projectPath) {
    vscode.window.showErrorMessage('AVC: No project path configured.');
    return;
  }

  const panel = vscode.window.createWebviewPanel(
    'avcInfo',
    `AVC Info: ${label}`,
    vscode.ViewColumn.One,
    { enableScripts: true }
  );

  panel.webview.html = `<!DOCTYPE html><html><body style="padding:20px;font-family:sans-serif">
    <p>Loading snapshot details...</p></body></html>`;

  try {
    const detail = await getSnapshotInfo(projectPath, snapshotId);
    const n = makeNonce();
    const csp = buildCsp(panel.webview, n);

    const tree = buildTree(detail.files);
    const treeHtml = renderTree(tree, detail.id, 0);

    panel.webview.html = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta http-equiv="Content-Security-Policy" content="${csp}">
  <style>
    ${BASE_STYLES}

    .info-grid {
      display: grid;
      grid-template-columns: auto 1fr;
      gap: 4px 16px;
      margin-bottom: 16px;
      font-size: 13px;
    }
    .info-label {
      color: var(--vscode-descriptionForeground);
      font-weight: bold;
      white-space: nowrap;
    }
    .info-value {
      word-break: break-all;
    }
    .notes-block {
      background: var(--vscode-editorWidget-background);
      border: 1px solid var(--vscode-panel-border);
      border-radius: 4px;
      padding: 8px 12px;
      margin-bottom: 16px;
      font-size: 12px;
      white-space: pre-wrap;
    }

    /* File tree */
    .file-tree {
      border: 1px solid var(--vscode-panel-border);
      border-radius: 4px;
      margin-top: 8px;
      overflow: hidden;
      font-size: 12px;
    }
    .tree-row {
      display: flex;
      align-items: center;
      gap: 6px;
      padding: 3px 8px;
      min-height: 24px;
      border-bottom: 1px solid var(--vscode-panel-border);
    }
    .tree-row:last-child { border-bottom: none; }
    .tree-row:hover { background: var(--vscode-list-hoverBackground); }

    .folder-row {
      cursor: pointer;
      user-select: none;
    }
    .folder-row .tree-name { font-weight: bold; }

    .tree-chevron {
      width: 12px;
      flex-shrink: 0;
      text-align: center;
      font-size: 9px;
      color: var(--vscode-descriptionForeground);
      transition: transform 0.1s ease-in-out;
      display: inline-block;
    }
    .folder-icon, .file-icon {
      width: 16px;
      flex-shrink: 0;
      text-align: center;
    }
    .tree-name {
      flex: 1;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .tree-size {
      flex-shrink: 0;
      width: 70px;
      text-align: right;
      color: var(--vscode-descriptionForeground);
      font-size: 11px;
    }
    .tree-hash {
      flex-shrink: 0;
      width: 70px;
      font-family: monospace;
      color: var(--vscode-descriptionForeground);
      font-size: 11px;
    }
    .tree-meta {
      color: var(--vscode-descriptionForeground);
      font-size: 11px;
      margin-left: auto;
    }

    /* Collapsed state — children hidden, chevron rotated */
    .tree-folder.collapsed > .tree-children { display: none; }
    .tree-folder.collapsed .tree-chevron { transform: rotate(-90deg); }

    .restore-btn {
      flex-shrink: 0;
      background: var(--vscode-button-secondaryBackground);
      color: var(--vscode-button-secondaryForeground);
      border: 1px solid var(--vscode-panel-border);
      border-radius: 3px;
      padding: 1px 6px;
      font-size: 10px;
      cursor: pointer;
    }
    .restore-btn:hover { background: var(--vscode-button-secondaryHoverBackground); }
  </style>
</head>
<body>
  <h2>${escapeHtml(detail.label)}</h2>

  <div class="info-grid">
    <span class="info-label">ID</span>
    <span class="info-value">${escapeHtml(detail.id)}</span>

    <span class="info-label">Timestamp</span>
    <span class="info-value">${formatTimestamp(detail.timestamp)}</span>

    <span class="info-label">Agent</span>
    <span class="info-value">${detail.agent_name ? escapeHtml(detail.agent_name) : '<em>none</em>'}</span>

    <span class="info-label">Files</span>
    <span class="info-value">${detail.file_count}</span>

    <span class="info-label">Total Size</span>
    <span class="info-value">${formatBytes(detail.total_size)}</span>
  </div>

  ${detail.notes ? `<div class="notes-block">${escapeHtml(detail.notes)}</div>` : ''}

  <h3>Files (${detail.files.length})</h3>
  <div class="file-tree">${treeHtml}</div>

  <script nonce="${n}">
    var vscode = acquireVsCodeApi();

    // Attach click handlers to all folder rows for collapse/expand.
    document.querySelectorAll('.folder-row').forEach(function (row) {
      row.addEventListener('click', function () {
        row.parentElement.classList.toggle('collapsed');
      });
    });

    document.querySelectorAll('.restore-btn').forEach(function (btn) {
      btn.addEventListener('click', function (e) {
        e.stopPropagation();
        vscode.postMessage({
          command: 'restoreFile',
          snapshotId: '${escapeHtml(detail.id)}',
          filePath: btn.getAttribute('data-path'),
        });
      });
    });
  </script>
</body>
</html>`;

    panel.webview.onDidReceiveMessage(
      (message) => {
        if (message.command === 'restoreFile') {
          vscode.commands.executeCommand(
            'avc.restoreFile',
            message.snapshotId,
            message.filePath
          );
        }
      },
      undefined,
      []
    );
  } catch (err) {
    panel.webview.html = `<!DOCTYPE html><html><body style="padding:20px;font-family:sans-serif;color:#c00">
      <h3>Error loading snapshot</h3><pre>${escapeHtml((err as Error).message)}</pre></body></html>`;
  }
}
