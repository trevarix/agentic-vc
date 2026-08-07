/*
 * Copyright (c) 2026 TREVARIX Corp.
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import * as vscode from 'vscode';
import { createSnapshot, resolveProjectPath } from './cliProxy';

/** Watches for file saves and creates snapshots automatically when enabled. */
export class AutoSnapshotManager implements vscode.Disposable {
  private debounceTimer: ReturnType<typeof setTimeout> | undefined;
  private lastSnapshotTime = 0;
  private disposable: vscode.Disposable;

  constructor() {
    this.disposable = vscode.workspace.onDidSaveTextDocument(() => this.onSave());
  }

  private onSave(): void {
    // The CLI watcher supersedes save-triggered snapshots: it sees every
    // change (including ones made outside the editor) and dedupes properly.
    if (vscode.workspace.getConfiguration('avc.watch').get<boolean>('enabled', false)) return;

    const config = vscode.workspace.getConfiguration('avc.autoSnapshot');
    if (!config.get<boolean>('enabled', false)) return;

    const debounceMs = (config.get<number>('debounceSeconds', 30)) * 1000;
    const cooldownMs = (config.get<number>('cooldownMinutes', 5)) * 60 * 1000;

    if (this.debounceTimer) clearTimeout(this.debounceTimer);

    this.debounceTimer = setTimeout(async () => {
      const now = Date.now();
      if (now - this.lastSnapshotTime < cooldownMs) return;

      const projectPath = resolveProjectPath();
      if (!projectPath) return;

      try {
        const agentName = vscode.workspace.getConfiguration('avc').get<string>('defaultAgentName') ?? '';
        const snap = await createSnapshot(
          projectPath,
          'Auto-snapshot',
          agentName || 'auto',
          'Triggered by file save'
        );
        this.lastSnapshotTime = Date.now();
        vscode.window.showInformationMessage(
          `AVC: Auto-snapshot saved (${snap.files_changed} files)`
        );
        vscode.commands.executeCommand('avc.refreshSnapshots');
      } catch {
        // Silent failure — auto-snapshot should not interrupt workflow.
      }
    }, debounceMs);
  }

  dispose(): void {
    if (this.debounceTimer) clearTimeout(this.debounceTimer);
    this.disposable.dispose();
  }
}
