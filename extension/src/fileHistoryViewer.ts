/*
 * Copyright (c) 2026 TREVARIX Corp.
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import * as vscode from 'vscode';
import { FileHistoryEntry, getFileHistory, resolveProjectPath } from './cliProxy';
import { formatBytes, formatTimestamp } from './webviewUtil';

interface HistoryQuickPickItem extends vscode.QuickPickItem {
  entry: FileHistoryEntry;
}

interface ActionItem extends vscode.QuickPickItem {
  action: 'open' | 'restore' | 'diff';
}

/** Show snapshots that contain filePath, then offer Open / Restore / Diff actions. */
export async function showFileHistory(filePath: string): Promise<void> {
  const projectPath = resolveProjectPath();
  if (!projectPath) {
    vscode.window.showErrorMessage('AVC: No project path configured.');
    return;
  }

  let history: FileHistoryEntry[];
  try {
    history = await getFileHistory(projectPath, filePath);
  } catch (err) {
    vscode.window.showErrorMessage(`AVC: Failed to load file history — ${(err as Error).message}`);
    return;
  }

  if (history.length === 0) {
    vscode.window.showInformationMessage(`AVC: '${filePath}' was not found in any snapshot.`);
    return;
  }

  const items: HistoryQuickPickItem[] = history.map((e) => ({
    label: e.label || '(untitled)',
    description: `${formatTimestamp(e.timestamp)} · ${e.hash.slice(0, 8)}`,
    detail: `${e.agent_name || 'no agent'} · ${formatBytes(e.size)} · ${e.snapshot_id}`,
    entry: e,
  }));

  const picked = await vscode.window.showQuickPick(items, {
    placeHolder: `${filePath} — pick a snapshot version`,
    matchOnDescription: true,
    matchOnDetail: true,
  });
  if (!picked) return;

  const actions: ActionItem[] = [
    { label: '$(eye) Open in editor', description: 'View this version (read-only)', action: 'open' },
    { label: '$(diff) Diff against current', description: 'Compare snapshot version with working tree', action: 'diff' },
    { label: '$(history) Restore this version', description: 'Overwrite the working file with this version', action: 'restore' },
  ];

  const action = await vscode.window.showQuickPick(actions, {
    placeHolder: `${picked.entry.label} (${picked.entry.snapshot_id.slice(0, 12)})`,
  });
  if (!action) return;

  switch (action.action) {
    case 'open':
      await vscode.commands.executeCommand(
        'avc.openFileFromSnapshot',
        picked.entry.snapshot_id,
        filePath,
        picked.entry.label
      );
      break;
    case 'diff':
      await vscode.commands.executeCommand(
        'avc.diffFileWithCurrent',
        picked.entry.snapshot_id,
        filePath,
        picked.entry.label
      );
      break;
    case 'restore':
      await vscode.commands.executeCommand(
        'avc.restoreFile',
        picked.entry.snapshot_id,
        filePath
      );
      break;
  }
}
