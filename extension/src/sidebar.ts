import * as vscode from 'vscode';
import { Snapshot, listSnapshots, resolveProjectPath } from './cliProxy';
import { formatBytes } from './webviewUtil';

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
      command: 'avc.viewInfo',
      title: 'View Details',
      arguments: [this],
    };
  }
}

/** Provides snapshot data to the VSCode tree view. */
export class SnapshotProvider implements vscode.TreeDataProvider<SnapshotItem> {
  private _onDidChangeTreeData = new vscode.EventEmitter<SnapshotItem | undefined>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private snapshots: Snapshot[] = [];
  private loading = false;
  private filterText = '';

  refresh(): void {
    this._onDidChangeTreeData.fire(undefined);
  }

  setFilter(text: string): void {
    this.filterText = text.toLowerCase();
    this.refresh();
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
      this.snapshots = await listSnapshots(projectPath);
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
    const items = this.snapshots.map((s) => new SnapshotItem(s));
    if (!this.filterText) return items;
    return items.filter((item) => {
      const s = item.snapshot;
      const searchable = [
        s.label, s.agent_name, s.notes, s.id,
        new Date(s.timestamp * 1000).toLocaleString(),
      ].join(' ').toLowerCase();
      return searchable.includes(this.filterText);
    });
  }
}

