import * as vscode from 'vscode';
import { Snapshot, listSnapshots, resolveProjectPath } from './cliProxy';
import { makeNonce, escapeHtml, formatBytes, formatTimestamp, buildCsp, BASE_STYLES } from './webviewUtil';

/** Deterministic colour for an agent name. */
function agentColor(name: string): string {
  const palette = [
    '#3fb950', '#58a6ff', '#d2a8ff', '#f0883e',
    '#db61a2', '#79c0ff', '#7ee787', '#ffa657',
  ];
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = ((hash << 5) - hash + name.charCodeAt(i)) | 0;
  }
  return palette[Math.abs(hash) % palette.length];
}

/** Opens a webview panel showing a vertical timeline of all snapshots. */
export async function showTimeline(): Promise<void> {
  const projectPath = resolveProjectPath();
  if (!projectPath) {
    vscode.window.showErrorMessage('AVC: No project path configured.');
    return;
  }

  const panel = vscode.window.createWebviewPanel(
    'avcTimeline',
    'AVC Timeline',
    vscode.ViewColumn.One,
    { enableScripts: true }
  );

  let snapshots: Snapshot[];
  try {
    snapshots = await listSnapshots(projectPath);
  } catch (err) {
    panel.webview.html = `<!DOCTYPE html><html><body style="padding:20px;color:#c00">
      <h3>Error</h3><pre>${escapeHtml((err as Error).message)}</pre></body></html>`;
    return;
  }

  const n = makeNonce();
  const csp = buildCsp(panel.webview, n);

  const nodes = snapshots
    .map(
      (s) => {
        const agentBadge = s.agent_name
          ? `<span class="agent-badge" style="background:${agentColor(s.agent_name)}">${escapeHtml(s.agent_name)}</span>`
          : '';
        return `<div class="tl-node" data-id="${escapeHtml(s.id)}">
        <div class="tl-dot"></div>
        <div class="tl-content">
          <span class="tl-label">${escapeHtml(s.label)}</span>
          ${agentBadge}
          <span class="tl-meta">${formatTimestamp(s.timestamp)} &middot; ${s.files_changed} files &middot; ${formatBytes(s.total_size)}</span>
          ${s.notes ? `<span class="tl-notes">${escapeHtml(s.notes)}</span>` : ''}
        </div>
      </div>`;
      }
    )
    .join('');

  panel.webview.html = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta http-equiv="Content-Security-Policy" content="${csp}">
  <style>
    ${BASE_STYLES}

    .timeline {
      position: relative;
      padding-left: 28px;
      margin-top: 16px;
    }
    .timeline::before {
      content: '';
      position: absolute;
      left: 8px;
      top: 0;
      bottom: 0;
      width: 2px;
      background: var(--vscode-panel-border);
    }
    .tl-node {
      position: relative;
      margin-bottom: 20px;
      cursor: pointer;
      padding: 8px 12px;
      border-radius: 4px;
      transition: background 0.15s;
    }
    .tl-node:hover {
      background: var(--vscode-list-hoverBackground);
    }
    .tl-dot {
      position: absolute;
      left: -24px;
      top: 12px;
      width: 12px;
      height: 12px;
      border-radius: 50%;
      background: var(--vscode-textLink-foreground);
      border: 2px solid var(--vscode-editor-background);
    }
    .tl-content {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }
    .tl-label {
      font-weight: bold;
      font-size: 13px;
    }
    .agent-badge {
      display: inline-block;
      align-self: flex-start;
      border-radius: 3px;
      padding: 1px 6px;
      font-size: 10px;
      color: #fff;
      font-weight: bold;
    }
    .tl-meta {
      font-size: 11px;
      color: var(--vscode-descriptionForeground);
    }
    .tl-notes {
      font-size: 11px;
      color: var(--vscode-descriptionForeground);
      font-style: italic;
      margin-top: 2px;
    }
    .empty-msg {
      color: var(--vscode-descriptionForeground);
      font-style: italic;
      margin-top: 24px;
    }
  </style>
</head>
<body>
  <h2>Snapshot Timeline</h2>
  <p class="meta">${snapshots.length} snapshot${snapshots.length === 1 ? '' : 's'}</p>

  ${snapshots.length === 0
      ? '<p class="empty-msg">No snapshots yet. Create one with the + button in the sidebar.</p>'
      : `<div class="timeline">${nodes}</div>`}

  <script nonce="${n}">
    var vscode = acquireVsCodeApi();
    document.querySelectorAll('.tl-node').forEach(function (node) {
      node.addEventListener('click', function () {
        vscode.postMessage({ command: 'viewInfo', snapshotId: node.getAttribute('data-id') });
      });
    });
  </script>
</body>
</html>`;

  panel.webview.onDidReceiveMessage(
    (message) => {
      if (message.command === 'viewInfo') {
        // Find the snapshot label for the info viewer title.
        const snap = snapshots.find((s) => s.id === message.snapshotId);
        if (snap) {
          vscode.commands.executeCommand('avc.viewInfo', { snapshot: snap });
        }
      }
    },
    undefined,
    []
  );
}
