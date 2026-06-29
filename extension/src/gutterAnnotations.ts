/*
 * Copyright (c) 2026 TREVARIX Corp.
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import * as vscode from 'vscode';
import * as path from 'path';
import { annotateFile, resolveProjectPath, AnnotateResult } from './cliProxy';

/** Provides inline gutter annotations showing which snapshot introduced each line. */
export class GutterAnnotationProvider implements vscode.Disposable {
  private decorationType: vscode.TextEditorDecorationType;
  private enabled = false;
  private disposables: vscode.Disposable[] = [];

  constructor() {
    this.decorationType = vscode.window.createTextEditorDecorationType({
      after: {
        margin: '0 0 0 3em',
        color: new vscode.ThemeColor('editorCodeLens.foreground'),
      },
      isWholeLine: true,
    });

    this.disposables.push(
      vscode.window.onDidChangeActiveTextEditor(() => {
        if (this.enabled) this.update();
      }),
      vscode.workspace.onDidSaveTextDocument(() => {
        if (this.enabled) this.update();
      }),
    );
  }

  toggle(): void {
    this.enabled = !this.enabled;
    if (this.enabled) {
      this.update();
      vscode.window.showInformationMessage('AVC: Line annotations enabled');
    } else {
      this.clear();
      vscode.window.showInformationMessage('AVC: Line annotations disabled');
    }
  }

  private clear(): void {
    const editor = vscode.window.activeTextEditor;
    if (editor) {
      editor.setDecorations(this.decorationType, []);
    }
  }

  async update(): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) return;

    const projectPath = resolveProjectPath();
    if (!projectPath) return;

    const filePath = editor.document.uri.fsPath;
    const relative = path.relative(projectPath, filePath).split(path.sep).join('/');

    // Skip files outside the project.
    if (relative.startsWith('..')) return;

    let result: AnnotateResult;
    try {
      result = await annotateFile(projectPath, relative);
    } catch {
      // Annotation failures are non-critical.
      return;
    }

    if (!result.lines || result.lines.length === 0) {
      editor.setDecorations(this.decorationType, []);
      return;
    }

    const decorations: vscode.DecorationOptions[] = result.lines.map((line) => {
      const lineIndex = line.line - 1;
      const range = new vscode.Range(lineIndex, 0, lineIndex, 0);
      const age = formatRelativeTime(line.timestamp);
      const agent = line.agent_name ? ` (${line.agent_name})` : '';
      return {
        range,
        renderOptions: {
          after: {
            contentText: `  ${line.label}${agent} \u00b7 ${age}`,
          },
        },
      };
    });

    editor.setDecorations(this.decorationType, decorations);
  }

  dispose(): void {
    this.decorationType.dispose();
    this.disposables.forEach((d) => d.dispose());
  }
}

function formatRelativeTime(unixTimestamp: number): string {
  const seconds = Math.floor(Date.now() / 1000 - unixTimestamp);
  if (seconds < 60) return 'just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  return `${months}mo ago`;
}
