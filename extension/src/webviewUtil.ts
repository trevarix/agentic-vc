import * as vscode from 'vscode';
import * as crypto from 'crypto';

const PRISM_CDN = 'https://cdnjs.cloudflare.com/ajax/libs/prism/1.29.0';

/** Generate a cryptographic nonce for CSP inline scripts. */
export function makeNonce(): string {
  return crypto.randomBytes(16).toString('base64');
}

/** Escape HTML special characters. */
export function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

/** Format a byte count as a human-readable string. */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

/** Format a Unix timestamp as a human-readable date string. */
export function formatTimestamp(unix: number): string {
  return new Date(unix * 1000).toLocaleString();
}

/** Build a Content Security Policy string for webview panels. */
export function buildCsp(webview: vscode.Webview, nonce: string): string {
  return [
    `default-src 'none'`,
    `style-src ${webview.cspSource} ${PRISM_CDN} 'unsafe-inline'`,
    `script-src ${PRISM_CDN} 'nonce-${nonce}'`,
    `font-src ${PRISM_CDN}`,
    `connect-src ${PRISM_CDN}`,
  ].join('; ');
}

/** Common base styles that match the VSCode theme. */
export const BASE_STYLES = `
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
  .badge          { border-radius: 3px; padding: 2px 6px; font-size: 11px; }
  .badge-added    { background: #1a7f37; color: #fff; }
  .badge-modified { background: #9a6700; color: #fff; }
  .badge-deleted  { background: #cf222e; color: #fff; }
  .added-count    { color: #3fb950; }
  .removed-count  { color: #f85149; }
  a { color: var(--vscode-textLink-foreground); text-decoration: none; }
  a:hover { text-decoration: underline; }
  button, .btn {
    background: var(--vscode-button-secondaryBackground);
    color: var(--vscode-button-secondaryForeground);
    border: 1px solid var(--vscode-panel-border);
    border-radius: 3px;
    padding: 2px 8px;
    font-size: 11px;
    cursor: pointer;
    white-space: nowrap;
  }
  button:hover, .btn:hover { background: var(--vscode-button-secondaryHoverBackground); }
`;

/** Return the Prism CDN base URL. */
export function prismCdn(): string {
  return PRISM_CDN;
}
