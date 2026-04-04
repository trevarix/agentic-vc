import * as vscode from 'vscode';
import { SnapshotProvider, SnapshotItem } from './sidebar';
import { showDiff } from './diffViewer';
import {
  createSnapshot,
  restoreSnapshot,
  deleteSnapshot,
  resolveProjectPath,
} from './cliProxy';

export function activate(context: vscode.ExtensionContext): void {
  const provider = new SnapshotProvider();

  context.subscriptions.push(
    vscode.window.registerTreeDataProvider('avcSnapshots', provider)
  );

  // Load snapshot list on activation.
  provider.load();

  // ─── Commands ────────────────────────────────────────────────────────────────

  context.subscriptions.push(
    vscode.commands.registerCommand('avc.refreshSnapshots', () => provider.load()),

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
        provider.load();
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Snapshot failed — ${(err as Error).message}`);
      }
    }),

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

      try {
        const result = await restoreSnapshot(projectPath, item.snapshot.id);
        vscode.window.showInformationMessage(
          `AVC: Restored ${result.restored_files} files from "${item.snapshot.label}"`
        );
        provider.load();
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Restore failed — ${(err as Error).message}`);
      }
    }),

    vscode.commands.registerCommand('avc.viewDiff', async (item: SnapshotItem) => {
      // Diff this snapshot against the previous one in the list.
      // The tree provides items newest-first so the "previous" snapshot is index+1.
      // For simplicity we show changes relative to the immediately preceding snapshot.
      // A future version could let users pick both endpoints.
      const snapshots = provider.getChildren();
      const idx = snapshots.findIndex((s) => s.snapshot.id === item.snapshot.id);
      const prev = snapshots[idx + 1];

      if (!prev) {
        vscode.window.showInformationMessage('AVC: No earlier snapshot to compare against.');
        return;
      }

      await showDiff(
        context,
        prev.snapshot.id,
        prev.snapshot.label,
        item.snapshot.id,
        item.snapshot.label
      );
    }),

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
        provider.load();
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Delete failed — ${(err as Error).message}`);
      }
    })
  );
}

export function deactivate(): void {
  // No persistent resources to clean up.
}
