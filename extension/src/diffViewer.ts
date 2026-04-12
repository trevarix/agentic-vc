import * as vscode from 'vscode';
import { DiffResult, FileDiff, getDiff, resolveProjectPath } from './cliProxy';
import { makeNonce, escapeHtml, buildCsp, prismCdn } from './webviewUtil';

const PRISM_CDN = prismCdn();

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
    panel.webview.html = buildDiffHtml(result, fromLabel, toLabel, panel.webview);
  } catch (err) {
    panel.webview.html = errorHtml((err as Error).message);
  }
}

/** Opens a webview panel from a pre-fetched DiffResult. */
export function showDiffResult(
  result: DiffResult,
  fromLabel: string,
  toLabel: string
): void {
  const panel = vscode.window.createWebviewPanel(
    'avcDiff',
    `AVC Diff: ${fromLabel} → ${toLabel}`,
    vscode.ViewColumn.One,
    { enableScripts: true }
  );

  panel.webview.html = buildDiffHtml(result, fromLabel, toLabel, panel.webview);
}

function loadingHtml(): string {
  return `<!DOCTYPE html><html><body style="padding:20px;font-family:sans-serif">
    <p>Loading diff...</p></body></html>`;
}

function errorHtml(message: string): string {
  return `<!DOCTYPE html><html><body style="padding:20px;font-family:sans-serif;color:#c00">
    <h3>Error loading diff</h3><pre>${escapeHtml(message)}</pre></body></html>`;
}

function buildDiffHtml(
  result: DiffResult,
  fromLabel: string,
  toLabel: string,
  webview: vscode.Webview
): string {
  const n = makeNonce();
  const totalAdded = result.files.reduce((sum, f) => sum + f.lines_added, 0);
  const totalRemoved = result.files.reduce((sum, f) => sum + f.lines_removed, 0);

  const fileBlocks = result.files.map((f, i) => renderFileDiff(f, i)).join('');

  // File navigation — only shown when diff spans multiple files
  const nav =
    result.files.length > 1
      ? `<nav class="file-nav">
          <span class="nav-label">Jump to file:</span>
          ${result.files
            .map(
              (f, i) =>
                `<a class="nav-link" href="#file-${i}">${escapeHtml(f.path)}</a>`
            )
            .join('')}
        </nav>`
      : '';

  // Raw diff text per file — stored in JS for copy-to-clipboard.
  // Escape </script> so the JSON can safely sit inside a <script> block.
  const rawDiffsJson = JSON.stringify(result.files.map((f) => f.diff_preview ?? '')).replace(
    /<\//g,
    '<\\/'
  );

  const csp = buildCsp(webview, n);

  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta http-equiv="Content-Security-Policy" content="${csp}">
  <link rel="stylesheet" href="${PRISM_CDN}/themes/prism-tomorrow.min.css">
  <style>
    /* Prism: transparent bg so row add/del colours show through */
    code[class*="language-"], pre[class*="language-"] {
      background: transparent !important;
      text-shadow: none !important;
      font-size: inherit !important;
      font-family: inherit !important;
      line-height: inherit !important;
      padding: 0 !important;
      margin: 0 !important;
    }
    body {
      font-family: var(--vscode-editor-font-family, monospace);
      font-size: 13px;
      background: var(--vscode-editor-background);
      color: var(--vscode-editor-foreground);
      padding: 16px;
      margin: 0;
    }
    h2   { font-size: 16px; margin-bottom: 4px; }
    .meta { color: var(--vscode-descriptionForeground); margin-bottom: 12px; font-size: 12px; }

    /* File navigator */
    .file-nav {
      margin-bottom: 16px;
      background: var(--vscode-editorWidget-background);
      border: 1px solid var(--vscode-panel-border);
      border-radius: 4px;
      padding: 8px 12px;
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
      align-items: center;
    }
    .nav-label { font-size: 11px; color: var(--vscode-descriptionForeground); margin-right: 4px; }
    .nav-link  { color: var(--vscode-textLink-foreground); text-decoration: none; font-size: 12px; }
    .nav-link:hover { text-decoration: underline; }

    /* File block */
    .file { margin-bottom: 20px; border: 1px solid var(--vscode-panel-border); border-radius: 4px; }
    .file-header {
      padding: 6px 12px;
      background: var(--vscode-editorGroupHeader-tabsBackground);
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 8px;
    }
    .file-path { font-weight: bold; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .file-header-right { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
    .badge          { border-radius: 3px; padding: 2px 6px; font-size: 11px; }
    .badge-added    { background: #1a7f37; color: #fff; }
    .badge-modified { background: #9a6700; color: #fff; }
    .badge-deleted  { background: #cf222e; color: #fff; }
    .line-stats     { font-size: 11px; }
    .added-count    { color: #3fb950; }
    .removed-count  { color: #f85149; }

    /* Copy button */
    .copy-btn {
      background: var(--vscode-button-secondaryBackground);
      color: var(--vscode-button-secondaryForeground);
      border: 1px solid var(--vscode-panel-border);
      border-radius: 3px;
      padding: 2px 8px;
      font-size: 11px;
      cursor: pointer;
      white-space: nowrap;
    }
    .copy-btn:hover  { background: var(--vscode-button-secondaryHoverBackground); }
    .copy-btn.copied { background: #1a7f37; color: #fff; border-color: #1a7f37; }

    /* Diff table */
    .diff-body  { border-top: 1px solid var(--vscode-panel-border); overflow-x: auto; }
    .diff-table { border-collapse: collapse; width: 100%; font-size: 12px; }
    .diff-table tr:hover { filter: brightness(1.08); }
    .ln   {
      width: 1%; white-space: nowrap; text-align: right; padding: 1px 8px;
      color: var(--vscode-editorLineNumber-foreground); user-select: none;
      border-right: 1px solid var(--vscode-panel-border);
    }
    .sign { width: 1%; white-space: nowrap; padding: 1px 6px; user-select: none; }
    .code { padding: 1px 8px; white-space: pre; }
    .hunk-row td {
      background: var(--vscode-editor-inactiveSelectionBackground);
      color: var(--vscode-textLink-foreground);
      padding: 2px 8px;
      font-size: 11px;
    }
    .add-row       { background: rgba(63,185,80,0.15); }
    .add-row .sign { color: #3fb950; }
    .del-row       { background: rgba(248,81,73,0.15); }
    .del-row .sign { color: #f85149; }
    .ctx-row       { color: var(--vscode-descriptionForeground); }
  </style>
</head>
<body>
  <h2>Diff: ${escapeHtml(fromLabel)} → ${escapeHtml(toLabel)}</h2>
  <p class="meta">${result.files.length} file(s) changed &nbsp;
    <span class="added-count">+${totalAdded}</span> &nbsp;
    <span class="removed-count">-${totalRemoved}</span>
  </p>
  ${nav}
  ${fileBlocks}

  <script nonce="${n}" src="${PRISM_CDN}/components/prism-core.min.js"></script>
  <script nonce="${n}" src="${PRISM_CDN}/plugins/autoloader/prism-autoloader.min.js"></script>
  <script nonce="${n}">
    if (typeof Prism !== 'undefined' && Prism.plugins && Prism.plugins.autoloader) {
      Prism.plugins.autoloader.languages_path = '${PRISM_CDN}/components/';
      Prism.highlightAll();
    }

    var rawDiffs = ${rawDiffsJson};

    function copyDiff(index) {
      var text = rawDiffs[index] || '';
      navigator.clipboard.writeText(text).then(function () {
        var btn = document.querySelector('[data-copy-index="' + index + '"]');
        if (btn) {
          btn.textContent = 'Copied!';
          btn.classList.add('copied');
          setTimeout(function () {
            btn.textContent = 'Copy';
            btn.classList.remove('copied');
          }, 2000);
        }
      });
    }
  </script>
</body>
</html>`;
}

/** Map file extension → Prism language identifier. */
function langFromPath(filePath: string): string {
  const ext = filePath.split('.').pop()?.toLowerCase() ?? '';
  const map: Record<string, string> = {
    go: 'go',
    ts: 'typescript', tsx: 'typescript',
    js: 'javascript', jsx: 'javascript',
    py: 'python',
    rs: 'rust',
    java: 'java',
    c: 'c', h: 'c',
    cpp: 'cpp', cc: 'cpp', hpp: 'cpp',
    cs: 'csharp',
    json: 'json',
    yaml: 'yaml', yml: 'yaml',
    md: 'markdown',
    html: 'html',
    css: 'css',
    sh: 'bash', bash: 'bash',
    toml: 'toml',
    sql: 'sql',
    rb: 'ruby',
    php: 'php',
    swift: 'swift',
    kt: 'kotlin',
    scala: 'scala',
  };
  return map[ext] ?? '';
}

function renderFileDiff(f: FileDiff, index: number): string {
  const badgeClass = `badge-${f.type}`;
  const lang = langFromPath(f.path);
  const body = f.diff_preview
    ? `<div class="diff-body">${renderUnifiedDiff(f.diff_preview, lang)}</div>`
    : '';

  return `
  <div class="file" id="file-${index}">
    <div class="file-header">
      <span class="file-path">${escapeHtml(f.path)}</span>
      <span class="file-header-right">
        <span class="badge ${badgeClass}">${f.type}</span>
        <span class="line-stats">
          <span class="added-count">+${f.lines_added}</span>
          <span class="removed-count"> -${f.lines_removed}</span>
        </span>
        <button class="copy-btn" data-copy-index="${index}" onclick="copyDiff(${index})">Copy</button>
      </span>
    </div>
    ${body}
  </div>`;
}

function renderUnifiedDiff(preview: string, lang: string): string {
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
      const rawCode = line.slice(1);
      const codeCell = lang
        ? `<td class="code"><code class="language-${lang}">${escapeHtml(rawCode)}</code></td>`
        : `<td class="code">${escapeHtml(rawCode)}</td>`;

      if (prefix === '+') {
        const n = newLine++;
        return `<tr class="add-row"><td class="ln">${n}</td><td class="sign">+</td>${codeCell}</tr>`;
      }
      if (prefix === '-') {
        const n = oldLine++;
        return `<tr class="del-row"><td class="ln">${n}</td><td class="sign">-</td>${codeCell}</tr>`;
      }
      if (prefix === ' ') {
        const n = oldLine++;
        newLine++;
        return `<tr class="ctx-row"><td class="ln">${n}</td><td class="sign"> </td>${codeCell}</tr>`;
      }
      return '';
    })
    .filter(Boolean)
    .join('');

  return `<table class="diff-table">${rows}</table>`;
}
