/*
 * Copyright (c) 2026 TREVARIX Corp.
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import * as vscode from 'vscode';
import { ChildProcess } from 'child_process';
import { spawnWatch, resolveProjectPath } from './cliProxy';

/**
 * Runs the `avc watch` daemon alongside the extension when
 * `avc.watch.enabled` is on: spawned on activation (and when the setting
 * turns on), killed on deactivation (and when it turns off). While the
 * watcher runs, the save-triggered AutoSnapshotManager stands down — the CLI
 * watcher sees every change, including ones made outside the editor.
 */
export class WatchManager implements vscode.Disposable {
  private proc: ChildProcess | undefined;
  private configListener: vscode.Disposable;
  private stopping = false;

  constructor() {
    this.configListener = vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration('avc.watch.enabled')) this.sync();
    });
    this.sync();
  }

  /** Reconciles the child process with the current setting value. */
  private sync(): void {
    const enabled = vscode.workspace
      .getConfiguration('avc.watch')
      .get<boolean>('enabled', false);
    if (enabled && !this.proc) {
      this.start();
    } else if (!enabled && this.proc) {
      this.stop();
    }
  }

  private start(): void {
    const projectPath = resolveProjectPath();
    if (!projectPath) return;

    this.stopping = false;
    const proc = spawnWatch(projectPath);
    this.proc = proc;

    proc.on('error', (err) => {
      this.proc = undefined;
      vscode.window.showErrorMessage(`AVC: Could not start the watcher: ${err.message}`);
    });
    proc.on('exit', (code) => {
      if (this.proc === proc) this.proc = undefined;
      // An exit we did not request means the daemon refused to start (e.g.
      // another watcher already holds the pid file) or died — surface it.
      if (!this.stopping && code !== 0 && code !== null) {
        vscode.window.showWarningMessage(
          'AVC: The watcher stopped unexpectedly. Run `avc watch` in a terminal to see why.'
        );
      }
    });
  }

  private stop(): void {
    if (!this.proc) return;
    this.stopping = true;
    this.proc.kill('SIGINT'); // same clean shutdown as Ctrl+C
    this.proc = undefined;
  }

  dispose(): void {
    this.stop();
    this.configListener.dispose();
  }
}
