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
  listBranches,
  createBranch,
  switchBranch,
  deleteBranch,
  previewMerge,
  mergeBranch,
  abortMerge,
  getDiffCurrent,
  restoreFile,
  resolveProjectPath,
} from './cliProxy';

export function activate(context: vscode.ExtensionContext): void {
  const provider = new SnapshotProvider();

  // Snapshot count status bar (left side).
  const snapshotBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 10);
  snapshotBar.command = 'avc.refreshSnapshots';
  snapshotBar.show();
  context.subscriptions.push(snapshotBar);

  // Branch status bar (left side, slightly lower priority).
  const branchBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 9);
  branchBar.command = 'avc.switchBranch';
  branchBar.tooltip = 'AVC: Switch branch';
  branchBar.show();
  context.subscriptions.push(branchBar);

  function updateStatusBar(): void {
    const count = provider.getChildren().length;
    snapshotBar.text = `$(history) AVC: ${count} snapshot${count === 1 ? '' : 's'}`;
    branchBar.text = `$(git-branch) ${provider.activeBranch}`;
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

    vscode.commands.registerCommand('avc.createBranch', async () => {
      const projectPath = resolveProjectPath();
      if (!projectPath) {
        vscode.window.showErrorMessage('AVC: No project path configured.');
        return;
      }

      const name = await vscode.window.showInputBox({
        prompt: 'Branch name',
        placeHolder: 'e.g. feature/my-experiment',
      });
      if (!name) return;

      try {
        const b = await createBranch(projectPath, name);
        // Branch create auto-switches — just reload the UI.
        await provider.load();
        updateStatusBar();
        const msg = b.workspace
          ? `AVC: Branch "${b.name}" created. Direct your agent to: ${b.workspace}`
          : `AVC: Branch "${b.name}" created.`;
        vscode.window.showInformationMessage(msg);
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Create branch failed — ${(err as Error).message}`);
      }
    }),

    vscode.commands.registerCommand('avc.switchBranch', async () => {
      const projectPath = resolveProjectPath();
      if (!projectPath) {
        vscode.window.showErrorMessage('AVC: No project path configured.');
        return;
      }

      try {
        const branches = await listBranches(projectPath);
        if (branches.length === 0) {
          vscode.window.showInformationMessage('AVC: No branches found.');
          return;
        }
        const items = branches.map((b) => ({
          label: b.active ? `$(check) ${b.name}` : b.name,
          description: b.active ? 'active' : '',
          branchName: b.name,
        }));
        const picked = await vscode.window.showQuickPick(items, {
          placeHolder: 'Select a branch to switch to',
        });
        if (!picked || picked.description === 'active') return;

        await switchBranch(projectPath, picked.branchName);
        await provider.load();
        updateStatusBar();
        vscode.window.showInformationMessage(`AVC: Switched to branch "${picked.branchName}"`);
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Switch branch failed — ${(err as Error).message}`);
      }
    }),

    vscode.commands.registerCommand('avc.deleteBranch', async () => {
      const projectPath = resolveProjectPath();
      if (!projectPath) {
        vscode.window.showErrorMessage('AVC: No project path configured.');
        return;
      }

      try {
        const branches = await listBranches(projectPath);
        const deletable = branches.filter((b) => b.name !== 'main' && !b.active);
        if (deletable.length === 0) {
          vscode.window.showInformationMessage('AVC: No deletable branches (cannot delete main or active branch).');
          return;
        }
        const picked = await vscode.window.showQuickPick(
          deletable.map((b) => ({ label: b.name, branchName: b.name })),
          { placeHolder: 'Select a branch to delete' }
        );
        if (!picked) return;

        const confirmed = await vscode.window.showWarningMessage(
          `Delete branch "${picked.branchName}"? This cannot be undone.`,
          { modal: true },
          'Delete'
        );
        if (confirmed !== 'Delete') return;

        await deleteBranch(projectPath, picked.branchName);
        await provider.load();
        updateStatusBar();
        vscode.window.showInformationMessage(`AVC: Branch "${picked.branchName}" deleted.`);
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Delete branch failed — ${(err as Error).message}`);
      }
    }),

    // ── Merge branch to main ───────────────────────────────────────────────────
    vscode.commands.registerCommand('avc.mergeBranch', async () => {
      const projectPath = resolveProjectPath();
      if (!projectPath) {
        vscode.window.showErrorMessage('AVC: No project path configured.');
        return;
      }

      try {
        const branches = await listBranches(projectPath);
        const mergeable = branches.filter((b) => b.name !== 'main');
        if (mergeable.length === 0) {
          vscode.window.showInformationMessage('AVC: No branches available to merge.');
          return;
        }

        const picked = await vscode.window.showQuickPick(
          mergeable.map((b) => ({ label: b.name, branchName: b.name })),
          { placeHolder: 'Select a branch to merge into main' }
        );
        if (!picked) return;

        const preview = await previewMerge(projectPath, picked.branchName);
        const previewMsg = [
          `Preview: merge "${picked.branchName}" → main`,
          `  Clean:     ${preview.clean} file(s)`,
          `  Conflicts: ${preview.conflicts} file(s)`,
          `  Skipped:   ${preview.skipped} file(s)`,
        ].join('\n');

        const action = await vscode.window.showInformationMessage(
          previewMsg,
          { modal: true },
          'Merge',
          'Cancel'
        );
        if (action !== 'Merge') return;

        const result = await mergeBranch(projectPath, picked.branchName);
        await refreshAll();

        if (result.conflicts > 0) {
          vscode.window.showWarningMessage(
            `AVC: Merge complete with ${result.conflicts} conflict(s). ` +
            `Resolve the markers, then snapshot. Or run "AVC: Abort Merge" to undo.`
          );
        } else {
          vscode.window.showInformationMessage(
            `AVC: Merged "${picked.branchName}" into main — ${result.clean} file(s) applied cleanly.`
          );
        }
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Merge failed — ${(err as Error).message}`);
      }
    }),

    // ── Abort merge ────────────────────────────────────────────────────────────
    vscode.commands.registerCommand('avc.abortMerge', async () => {
      const projectPath = resolveProjectPath();
      if (!projectPath) {
        vscode.window.showErrorMessage('AVC: No project path configured.');
        return;
      }

      const confirmed = await vscode.window.showWarningMessage(
        'Abort merge? Main will be restored from the pre-merge snapshot.',
        { modal: true },
        'Abort'
      );
      if (confirmed !== 'Abort') return;

      try {
        await abortMerge(projectPath);
        await refreshAll();
        vscode.window.showInformationMessage('AVC: Merge aborted. Main restored.');
      } catch (err) {
        vscode.window.showErrorMessage(`AVC: Abort failed — ${(err as Error).message}`);
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
