import * as vscode from 'vscode';
import { SnapshotProvider, SnapshotItem } from './sidebar';
import { showDiff, showDiffResult } from './diffViewer';
import { showInfo } from './infoViewer';
import { showTimeline } from './timelineViewer';
import { AutoSnapshotManager } from './autoSnapshot';
import { AvcScmProvider } from './scmProvider';
import { GutterAnnotationProvider } from './gutterAnnotations';
import {
  createSnapshot,
  restoreSnapshot,
  deleteSnapshot,
  getDiffCurrent,
  restoreFile,
  resolveProjectPath,
} from './cliProxy';

export function activate(context: vscode.ExtensionContext): void {
  const provider = new SnapshotProvider();

  const statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 10);
  statusBar.command = 'avc.refreshSnapshots';
  statusBar.show();
  context.subscriptions.push(statusBar);

  function updateStatusBar(): void {
    const count = provider.getChildren().length;
    statusBar.text = `$(history) AVC: ${count} snapshot${count === 1 ? '' : 's'}`;
  }

  context.subscriptions.push(
    vscode.window.registerTreeDataProvider('avcSnapshots', provider)
  );

  // Load snapshot list on activation.
  provider.load().then(updateStatusBar);

  // ─── Auto-snapshot on save ──────────────────────────────────────────────────
  context.subscriptions.push(new AutoSnapshotManager());

  // ─── SCM integration ───────────────────────────────────────────────────────
  const scmProvider = new AvcScmProvider();
  context.subscriptions.push(scmProvider);

  // ─── Gutter annotations ────────────────────────────────────────────────────
  const gutterProvider = new GutterAnnotationProvider();
  context.subscriptions.push(gutterProvider);

  /** Refresh sidebar + SCM together. */
  async function refreshAll(): Promise<void> {
    await provider.load();
    updateStatusBar();
    scmProvider.refresh();
  }

  // ─── Commands ────────────────────────────────────────────────────────────────

  context.subscriptions.push(
    vscode.commands.registerCommand('avc.refreshSnapshots', () => refreshAll()),

    // ── Save snapshot ──────────────────────────────────────────────────────────
    vscode.commands.registerCommand('avc.saveSnapshot', async () => {
      const label = await vscode.window.showInputBox({
        prompt: 'Snapshot label',
        placeHolder: 'e.g. Before refactor',
      });
      if (!label) return;

      const notes = await vscode.window.showInputBox({
        prompt: 'Notes (optional)',
        placeHolder: 'What changed?',
      });

      const config = vscode.workspace.getConfiguration('avc');
      const agentName: string = config.get('defaultAgentName') ?? '';
      const projectPath = resolveProjectPath();

      if (!projectPath) {
        vscode.window.showErrorMessage('AVC: No project path configured.');
        return;
      }

      try {
        const snap = await createSnapshot(projectPath, label, agentName || undefined, notes || undefined);
        vscode.window.showInformationMessage(
          `AVC: Snapshot "${snap.label}" saved (${snap.files_changed} files)`
        );
        await refreshAll();
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Snapshot failed — ${(err as Error).message}`);
      }
    }),

    // ── Restore snapshot (with safety snapshot) ────────────────────────────────
    vscode.commands.registerCommand('avc.restoreSnapshot', async (item: SnapshotItem) => {
      const confirmed = await vscode.window.showWarningMessage(
        `Restore to "${item.snapshot.label}"? Current files will be overwritten.`,
        { modal: true },
        'Restore'
      );
      if (confirmed !== 'Restore') return;

      const projectPath = resolveProjectPath();
      if (!projectPath) {
        vscode.window.showErrorMessage('AVC: No project path configured.');
        return;
      }

      // Auto-snapshot before restore so the user can undo.
      try {
        await createSnapshot(
          projectPath,
          'Pre-restore safety snapshot',
          'avc-extension',
          `Auto-saved before restoring to "${item.snapshot.label}"`
        );
      } catch (safetyErr) {
        const proceed = await vscode.window.showWarningMessage(
          'AVC: Failed to create safety snapshot. Continue with restore?',
          { modal: true },
          'Continue'
        );
        if (proceed !== 'Continue') return;
      }

      try {
        const result = await restoreSnapshot(projectPath, item.snapshot.id);
        vscode.window.showInformationMessage(
          `AVC: Restored ${result.restored_files} files from "${item.snapshot.label}"`
        );
        await refreshAll();
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Restore failed — ${(err as Error).message}`);
      }
    }),

    // ── View diff (adjacent snapshots) ─────────────────────────────────────────
    vscode.commands.registerCommand('avc.viewDiff', async (item: SnapshotItem) => {
      const snapshots = provider.getChildren();
      const idx = snapshots.findIndex((s) => s.snapshot.id === item.snapshot.id);
      const prev = snapshots[idx + 1];

      if (!prev) {
        vscode.window.showInformationMessage('AVC: No earlier snapshot to compare against.');
        return;
      }

      await showDiff(
        prev.snapshot.id,
        prev.snapshot.label,
        item.snapshot.id,
        item.snapshot.label
      );
    }),

    // ── View snapshot details ──────────────────────────────────────────────────
    vscode.commands.registerCommand('avc.viewInfo', async (item: SnapshotItem) => {
      await showInfo(item.snapshot.id, item.snapshot.label);
    }),

    // ── Delete snapshot ────────────────────────────────────────────────────────
    vscode.commands.registerCommand('avc.deleteSnapshot', async (item: SnapshotItem) => {
      const confirmed = await vscode.window.showWarningMessage(
        `Delete snapshot "${item.snapshot.label}"? This cannot be undone.`,
        { modal: true },
        'Delete'
      );
      if (confirmed !== 'Delete') return;

      const projectPath = resolveProjectPath();
      if (!projectPath) return;

      try {
        await deleteSnapshot(projectPath, item.snapshot.id);
        await refreshAll();
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Delete failed — ${(err as Error).message}`);
      }
    }),

    // ── Filter snapshots ───────────────────────────────────────────────────────
    vscode.commands.registerCommand('avc.filterSnapshots', async () => {
      const input = await vscode.window.showInputBox({
        prompt: 'Filter snapshots (label, agent, date). Leave empty to clear.',
        placeHolder: 'e.g. claude, refactor, 2026-04',
      });
      provider.setFilter(input ?? '');
      updateStatusBar();
    }),

    // ── Compare two snapshots ──────────────────────────────────────────────────
    vscode.commands.registerCommand('avc.compareTwoSnapshots', async () => {
      const snapshots = provider.getChildren();
      if (snapshots.length < 2) {
        vscode.window.showInformationMessage('AVC: Need at least 2 snapshots to compare.');
        return;
      }

      const pickItems = snapshots.map((s) => ({
        label: s.snapshot.label,
        description: new Date(s.snapshot.timestamp * 1000).toLocaleString(),
        detail: s.snapshot.id,
        snapshot: s.snapshot,
      }));

      const from = await vscode.window.showQuickPick(pickItems, {
        placeHolder: 'Select "from" snapshot (older)',
      });
      if (!from) return;

      const toItems = pickItems.filter((p) => p.snapshot.id !== from.snapshot.id);
      const to = await vscode.window.showQuickPick(toItems, {
        placeHolder: 'Select "to" snapshot (newer)',
      });
      if (!to) return;

      await showDiff(from.snapshot.id, from.snapshot.label, to.snapshot.id, to.snapshot.label);
    }),

    // ── Diff with current working tree ─────────────────────────────────────────
    vscode.commands.registerCommand('avc.diffWithCurrent', async (item: SnapshotItem) => {
      const projectPath = resolveProjectPath();
      if (!projectPath) {
        vscode.window.showErrorMessage('AVC: No project path configured.');
        return;
      }

      try {
        const result = await getDiffCurrent(projectPath, item.snapshot.id);
        showDiffResult(result, item.snapshot.label, 'Working Tree');
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Diff failed — ${(err as Error).message}`);
      }
    }),

    // ── Show timeline ──────────────────────────────────────────────────────────
    vscode.commands.registerCommand('avc.showTimeline', () => showTimeline()),

    // ── Restore single file ────────────────────────────────────────────────────
    vscode.commands.registerCommand('avc.restoreFile', async (snapshotId: string, filePath: string) => {
      const projectPath = resolveProjectPath();
      if (!projectPath) {
        vscode.window.showErrorMessage('AVC: No project path configured.');
        return;
      }

      const confirmed = await vscode.window.showWarningMessage(
        `Restore "${filePath}" from this snapshot? The current version will be overwritten.`,
        { modal: true },
        'Restore'
      );
      if (confirmed !== 'Restore') return;

      try {
        const result = await restoreFile(projectPath, snapshotId, filePath);
        vscode.window.showInformationMessage(`AVC: Restored ${result.file_path}`);
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: File restore failed — ${(err as Error).message}`);
      }
    }),

    // ── Toggle gutter annotations ──────────────────────────────────────────────
    vscode.commands.registerCommand('avc.toggleAnnotations', () => gutterProvider.toggle()),
  );
}

export function deactivate(): void {
  // No persistent resources to clean up.
}
