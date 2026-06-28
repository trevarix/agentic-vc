/*
 * Copyright (c) 2026 TREVARIX Corp.
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import * as vscode from 'vscode';
import { Snapshot, listSnapshots, listBranches, resolveProjectPath } from './cliProxy';
import { formatBytes } from './webviewUtil';

/** A single snapshot entry shown in the sidebar tree. */
export class SnapshotItem extends vscode.TreeItem {
  constructor(public readonly snapshot: Snapshot) {
    super(snapshot.label, vscode.TreeItemCollapsibleState.Collapsed);

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
    // Note: no this.command — clicking a snapshot row should toggle expand,
    // not auto-trigger an action. Use the action child rows instead.
  }
}

/** A button-like action row that appears beneath an expanded snapshot. */
export class ActionItem extends vscode.TreeItem {
  constructor(
    label: string,
    iconId: string,
    commandId: string,
    parent: SnapshotItem,
    tooltip?: string
  ) {
    super(label, vscode.TreeItemCollapsibleState.None);
    this.iconPath = new vscode.ThemeIcon(iconId);
    this.tooltip = tooltip ?? label;
    this.contextValue = 'snapshotAction';
    this.command = {
      command: commandId,
      title: label,
      arguments: [parent],
    };
  }
}

/** A date-bucket header that groups snapshots in the sidebar tree. */
export class GroupItem extends vscode.TreeItem {
  constructor(public readonly groupLabel: string, public readonly snapshots: Snapshot[]) {
    super(groupLabel, vscode.TreeItemCollapsibleState.Collapsed);
    this.contextValue = 'snapshotGroup';
    this.iconPath = new vscode.ThemeIcon('calendar');
    this.description = `${snapshots.length}`;
  }
}

type SidebarItem = GroupItem | SnapshotItem | ActionItem;

/** Bucket name for a snapshot's timestamp. */
function bucketFor(timestamp: number, now: Date): string {
  const date = new Date(timestamp * 1000);
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const startOfYesterday = startOfToday - 24 * 60 * 60 * 1000;
  const startOfWeek = startOfToday - 7 * 24 * 60 * 60 * 1000;
  const startOfMonth = startOfToday - 30 * 24 * 60 * 60 * 1000;

  const ts = date.getTime();
  if (ts >= startOfToday) return 'Today';
  if (ts >= startOfYesterday) return 'Yesterday';
  if (ts >= startOfWeek) return 'This Week';
  if (ts >= startOfMonth) return 'This Month';
  return 'Older';
}

const BUCKET_ORDER = ['Today', 'Yesterday', 'This Week', 'This Month', 'Older'];

/** Provides snapshot data to the VSCode tree view. */
export class SnapshotProvider implements vscode.TreeDataProvider<SidebarItem> {
  private _onDidChangeTreeData = new vscode.EventEmitter<SidebarItem | undefined>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private snapshots: Snapshot[] = [];
  private _activeBranch = 'main';
  private loading = false;
  private filterText = '';

  get activeBranch(): string {
    return this._activeBranch;
  }

  refresh(): void {
    this._onDidChangeTreeData.fire(undefined);
  }

  setFilter(text: string): void {
    this.filterText = text.toLowerCase();
    this.refresh();
  }

  /** Apply the current text filter to the snapshot list. */
  private filteredSnapshots(): Snapshot[] {
    if (!this.filterText) return this.snapshots;
    return this.snapshots.filter((s) => {
      const searchable = [
        s.label, s.agent_name, s.notes, s.id,
        new Date(s.timestamp * 1000).toLocaleString(),
      ].join(' ').toLowerCase();
      return searchable.includes(this.filterText);
    });
  }

  /** Total visible snapshot count after filters (used by status bar). */
  getVisibleSnapshotCount(): number {
    return this.filteredSnapshots().length;
  }

  /** Latest snapshot (newest first) — used by working-tree diff shortcut. */
  getLatestSnapshot(): Snapshot | undefined {
    return this.snapshots[0];
  }

  /** Flat list of all visible snapshot items (after filters), regardless of grouping. */
  getAllVisibleSnapshotItems(): SnapshotItem[] {
    return this.filteredSnapshots().map((s) => new SnapshotItem(s));
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

  getTreeItem(element: SidebarItem): vscode.TreeItem {
    return element;
  }

  getChildren(element?: SidebarItem): SidebarItem[] {
    // Children of a date-bucket group: its snapshots.
    if (element instanceof GroupItem) {
      return element.snapshots.map((s) => new SnapshotItem(s));
    }

    // Children of a snapshot: action rows (Restore / Diff / Delete / etc.).
    if (element instanceof SnapshotItem) {
      return [
        new ActionItem('View Details', 'info', 'avc.viewInfo', element, 'Open snapshot details panel'),
        new ActionItem('View Diff (vs previous)', 'diff', 'avc.viewDiff', element, 'Compare against previous snapshot'),
        new ActionItem('Diff with Current Files', 'compare-changes', 'avc.diffWithCurrent', element, 'Compare against the working tree'),
        new ActionItem('Restore This Snapshot', 'history', 'avc.restoreSnapshot', element, 'Roll back the project to this snapshot'),
        new ActionItem('Delete Snapshot', 'trash', 'avc.deleteSnapshot', element, 'Permanently delete this snapshot'),
      ];
    }

    // Action items have no children.
    if (element instanceof ActionItem) {
      return [];
    }

    // Root level: build date-bucket groups.
    const filtered = this.filteredSnapshots();
    const now = new Date();
    const buckets = new Map<string, Snapshot[]>();
    for (const s of filtered) {
      const bucket = bucketFor(s.timestamp, now);
      if (!buckets.has(bucket)) buckets.set(bucket, []);
      buckets.get(bucket)!.push(s);
    }
    return BUCKET_ORDER
      .filter((name) => buckets.has(name))
      .map((name) => new GroupItem(name, buckets.get(name)!));
  }
}
