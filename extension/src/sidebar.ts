import * as vscode from 'vscode';
import { Snapshot, listSnapshots, listBranches, resolveProjectPath } from './cliProxy';

/** A single snapshot entry shown in the sidebar tree. */
export class SnapshotItem extends vscode.TreeItem {
  constructor(public readonly snapshot: Snapshot) {
    super(snapshot.label, vscode.TreeItemCollapsibleState.None);

    const date = new Date(snapshot.timestamp * 1000).toLocaleString();
    this.description = date;
    this.tooltip = [
      `ID: ${snapshot.id}`,
      `Agent: ${snapshot.agent_name || '—'}`,
      `Files: ${snapshot.files_changed}`,
      `Size: ${formatBytes(snapshot.total_size)}`,
      snapshot.notes ? `Notes: ${snapshot.notes}` : '',
    ]
      .filter(Boolean)
      .join('\n');

    this.contextValue = 'snapshot';
    this.iconPath = new vscode.ThemeIcon('history');
    this.command = {
      command: 'avc.viewDiff',
      title: 'View Changes',
      arguments: [this],
    };
  }
}

/** Provides snapshot data to the VSCode tree view. */
export class SnapshotProvider implements vscode.TreeDataProvider<SnapshotItem> {
  private _onDidChangeTreeData = new vscode.EventEmitter<SnapshotItem | undefined>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private snapshots: Snapshot[] = [];
  private _activeBranch = 'main';
  private loading = false;

  get activeBranch(): string {
    return this._activeBranch;
  }

  refresh(): void {
    this._onDidChangeTreeData.fire(undefined);
  }

  async load(): Promise<void> {
    if (this.loading) return;
    this.loading = true;
    try {
      const projectPath = resolveProjectPath();
      if (!projectPath) {
        this.snapshots = [];
        return;
      }
      const [snapshots, branches] = await Promise.all([
        listSnapshots(projectPath),
        listBranches(projectPath).catch(() => []),
      ]);
      this.snapshots = snapshots;
      const active = branches.find((b) => b.active);
      if (active) this._activeBranch = active.name;
    } catch (err) {
      vscode.window.showErrorMessage(`AVC: Failed to load snapshots — ${(err as Error).message}`);
      this.snapshots = [];
    } finally {
      this.loading = false;
      this.refresh();
    }
  }

  getTreeItem(element: SnapshotItem): vscode.TreeItem {
    return element;
  }

  getChildren(): SnapshotItem[] {
    return this.snapshots.map((s) => new SnapshotItem(s));
  }
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
