import * as vscode from 'vscode';
import { DiffResult, FileDiff, getDiff, resolveProjectPath } from './cliProxy';

/** Opens a webview panel showing the diff between two snapshots. */
export async function showDiff(
  fromId: string,
  fromLabel: string,
  toId: string,
  toLabel: string
): Promise<void> {
  const projectPath = resolveProjectPath();
  if (!projectPath) {
    vscode.window.showErrorMessage('AVC: No project path configured.');
    return;
  }

  const panel = vscode.window.createWebviewPanel(
    'avcDiff',
    `AVC Diff: ${fromLabel} → ${toLabel}`,
    vscode.ViewColumn.One,
    { enableScripts: true }
  );

  panel.webview.html = loadingHtml();

  try {
    const result = await getDiff(projectPath, fromId, toId);
    panel.webview.html = buildDiffHtml(result, fromLabel, toLabel);
  } catch (err) {
    panel.webview.html = errorHtml((err as Error).message);
  }
}

function loadingHtml(): string {
  return `<!DOCTYPE html><html><body style="padding:20px;font-family:sans-serif">
    <p>Loading diff...</p></body></html>`;
}

function errorHtml(message: string): string {
  return `<!DOCTYPE html><html><body style="padding:20px;font-family:sans-serif;color:#c00">
    <h3>Error loading diff</h3><pre>${escapeHtml(message)}</pre></body></html>`;
}

function buildDiffHtml(result: DiffResult, fromLabel: string, toLabel: string): string {
  const fileRows = result.files.map(renderFileDiff).join('');
  const totalAdded = result.files.reduce((n, f) => n + f.lines_added, 0);
  const totalRemoved = result.files.reduce((n, f) => n + f.lines_removed, 0);

  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <style>
    body { font-family: var(--vscode-editor-font-family, monospace); font-size: 13px;
           background: var(--vscode-editor-background); color: var(--vscode-editor-foreground);
           padding: 16px; margin: 0; }
    h2 { font-size: 16px; margin-bottom: 4px; }
    .meta { color: var(--vscode-descriptionForeground); margin-bottom: 16px; font-size: 12px; }
    .file { margin-bottom: 20px; border: 1px solid var(--vscode-panel-border); border-radius: 4px; }
    .file-header { padding: 6px 12px; background: var(--vscode-editorGroupHeader-tabsBackground);
                   display: flex; justify-content: space-between; align-items: center; }
    .file-path { font-weight: bold; }
    .badge { border-radius: 3px; padding: 2px 6px; font-size: 11px; }
    .badge-added    { background: #1a7f37; color: #fff; }
    .badge-modified { background: #9a6700; color: #fff; }
    .badge-deleted  { background: #cf222e; color: #fff; }
    .line-stats { font-size: 11px; }
    .added-count   { color: #3fb950; }
    .removed-count { color: #f85149; }
    .diff-body  { border-top: 1px solid var(--vscode-panel-border); overflow-x: auto; }
    .diff-table { border-collapse: collapse; width: 100%; font-size: 12px; }
    .diff-table tr:hover { filter: brightness(1.08); }
    .ln   { width: 1%; white-space: nowrap; text-align: right; padding: 1px 8px;
            color: var(--vscode-editorLineNumber-foreground); user-select: none;
            border-right: 1px solid var(--vscode-panel-border); }
    .sign { width: 1%; white-space: nowrap; padding: 1px 6px; user-select: none; }
    .code { padding: 1px 8px; white-space: pre; }
    .hunk-row td { background: var(--vscode-editor-inactiveSelectionBackground);
                   color: var(--vscode-textLink-foreground); padding: 2px 8px; font-size: 11px; }
    .add-row      { background: rgba(63,185,80,0.15); }
    .add-row .sign { color: #3fb950; }
    .del-row      { background: rgba(248,81,73,0.15); }
    .del-row .sign { color: #f85149; }
    .ctx-row      { color: var(--vscode-descriptionForeground); }
  </style>
</head>
<body>
  <h2>Diff: ${escapeHtml(fromLabel)} → ${escapeHtml(toLabel)}</h2>
  <p class="meta">${result.files.length} file(s) changed &nbsp;
    <span class="added-count">+${totalAdded}</span> &nbsp;
    <span class="removed-count">-${totalRemoved}</span>
  </p>
  ${fileRows}
</body>
</html>`;
}

function renderFileDiff(f: FileDiff): string {
  const badgeClass = `badge-${f.type}`;
  const body = f.diff_preview
    ? `<div class="diff-body">${renderUnifiedDiff(f.diff_preview)}</div>`
    : '';

  return `
  <div class="file">
    <div class="file-header">
      <span class="file-path">${escapeHtml(f.path)}</span>
      <span>
        <span class="badge ${badgeClass}">${f.type}</span>
        <span class="line-stats">
          <span class="added-count">+${f.lines_added}</span>
          <span class="removed-count"> -${f.lines_removed}</span>
        </span>
      </span>
    </div>
    ${body}
  </div>`;
}

function renderUnifiedDiff(preview: string): string {
  let oldLine = 1;
  let newLine = 1;

  const rows = preview
    .split('\n')
    .filter((line) => line.length > 0)
    .map((line) => {
      if (line.startsWith('@@ ')) {
        const match = line.match(/@@ -(\d+)[,\d]* \+(\d+)/);
        if (match) {
          oldLine = parseInt(match[1], 10);
          newLine = parseInt(match[2], 10);
        }
        return `<tr class="hunk-row"><td colspan="3">${escapeHtml(line)}</td></tr>`;
      }

      const prefix = line[0];
      const content = `<td class="code">${escapeHtml(line.slice(1))}</td>`;

      if (prefix === '+') {
        const n = newLine++;
        return `<tr class="add-row"><td class="ln">${n}</td><td class="sign">+</td>${content}</tr>`;
      }
      if (prefix === '-') {
        const n = oldLine++;
        return `<tr class="del-row"><td class="ln">${n}</td><td class="sign">-</td>${content}</tr>`;
      }
      if (prefix === ' ') {
        const n = oldLine++;
        newLine++;
        return `<tr class="ctx-row"><td class="ln">${n}</td><td class="sign"> </td>${content}</tr>`;
      }
      return '';
    })
    .filter(Boolean)
    .join('');

  return `<table class="diff-table">${rows}</table>`;
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}
