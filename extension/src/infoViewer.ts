import * as vscode from 'vscode';
import { getSnapshotInfo, resolveProjectPath } from './cliProxy';
import { makeNonce, escapeHtml, formatBytes, formatTimestamp, buildCsp, BASE_STYLES } from './webviewUtil';

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

    const filesHtml = detail.files
      .map(
        (f) => `<tr>
          <td class="file-path-cell">${escapeHtml(f.path)}</td>
          <td class="size-cell">${formatBytes(f.size)}</td>
          <td class="hash-cell">${escapeHtml(f.hash.slice(0, 8))}</td>
          <td><button class="restore-btn" data-path="${escapeHtml(f.path)}">Restore</button></td>
        </tr>`
      )
      .join('');

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

    .file-table {
      width: 100%;
      border-collapse: collapse;
      font-size: 12px;
      margin-top: 8px;
    }
    .file-table th {
      text-align: left;
      padding: 4px 8px;
      border-bottom: 2px solid var(--vscode-panel-border);
      color: var(--vscode-descriptionForeground);
      font-size: 11px;
      text-transform: uppercase;
    }
    .file-table td {
      padding: 3px 8px;
      border-bottom: 1px solid var(--vscode-panel-border);
    }
    .file-table tr:hover { background: var(--vscode-list-hoverBackground); }
    .file-path-cell { word-break: break-all; }
    .size-cell { white-space: nowrap; text-align: right; color: var(--vscode-descriptionForeground); }
    .hash-cell { font-family: monospace; color: var(--vscode-descriptionForeground); }
    .restore-btn {
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
  <table class="file-table">
    <thead>
      <tr><th>Path</th><th>Size</th><th>Hash</th><th></th></tr>
    </thead>
    <tbody>
      ${filesHtml}
    </tbody>
  </table>

  <script nonce="${n}">
    const vscode = acquireVsCodeApi();
    document.querySelectorAll('.restore-btn').forEach(function (btn) {
      btn.addEventListener('click', function () {
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

    // Handle messages from webview.
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
