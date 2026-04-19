import * as vscode from 'vscode';
import { catFileFromSnapshot, resolveProjectPath } from './cliProxy';

export const SNAPSHOT_SCHEME = 'avc-snapshot';

/** Build a URI that the content provider can resolve.
 *  Format: avc-snapshot:/<snapshot_id>/<file_path>?label=<label>
 *  The query string is purely cosmetic — it shows up in the editor tab title. */
export function snapshotUri(snapshotId: string, filePath: string, label?: string): vscode.Uri {
  // Use vscode.Uri.from so path stays clean even on Windows.
  const path = `/${snapshotId}/${filePath}`;
  return vscode.Uri.from({
    scheme: SNAPSHOT_SCHEME,
    path,
    query: label ? `label=${encodeURIComponent(label)}` : '',
  });
}

/** Provides read-only snapshot file content via the avc-snapshot:// scheme. */
export class SnapshotContentProvider implements vscode.TextDocumentContentProvider {
  private _onDidChange = new vscode.EventEmitter<vscode.Uri>();
  readonly onDidChange = this._onDidChange.event;

  async provideTextDocumentContent(uri: vscode.Uri): Promise<string> {
    const projectPath = resolveProjectPath();
    if (!projectPath) {
      return '// AVC: No project path configured.';
    }

    // path looks like: /snap-abc123/path/to/file.ts
    // Strip leading slash, then split off the snapshot id.
    const trimmed = uri.path.replace(/^\/+/, '');
    const slash = trimmed.indexOf('/');
    if (slash === -1) {
      return '// AVC: Invalid snapshot URI.';
    }
    const snapshotId = trimmed.slice(0, slash);
    const filePath = trimmed.slice(slash + 1);

    try {
      return await catFileFromSnapshot(projectPath, snapshotId, filePath);
    } catch (err) {
      return `// AVC: Failed to load file from snapshot — ${(err as Error).message}`;
    }
  }
}
