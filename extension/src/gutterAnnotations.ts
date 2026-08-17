/*
 * Copyright (c) 2026 TREVARIX Corp.
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import * as vscode from 'vscode';
import * as path from 'path';
import { annotateFile, resolveProjectPath, AnnotateResult, LineAnnotation } from './cliProxy';

/**
 * Shows inline blame-style annotations for the active file: which snapshot
 * introduced each block of lines, and whether an agent or a human authored it.
 *
 * To stay readable, annotations are collapsed per contiguous block — only the
 * first non-empty line of each run of same-snapshot lines is annotated, not
 * every line — and blank lines are never annotated.
 */
export class GutterAnnotationProvider implements vscode.Disposable {
  private agentDecoration: vscode.TextEditorDecorationType;
  private humanDecoration: vscode.TextEditorDecorationType;
  private enabled = false;
  private disposables: vscode.Disposable[] = [];

  constructor() {
    // Two decoration types so agent- and human-authored blocks read distinctly.
    // Agent blocks are tinted; human blocks use the muted CodeLens colour.
    this.agentDecoration = vscode.window.createTextEditorDecorationType({
      after: { margin: '0 0 0 3em', color: new vscode.ThemeColor('charts.blue') },
      isWholeLine: true,
    });
    this.humanDecoration = vscode.window.createTextEditorDecorationType({
      after: { margin: '0 0 0 3em', color: new vscode.ThemeColor('editorCodeLens.foreground') },
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
      editor.setDecorations(this.agentDecoration, []);
      editor.setDecorations(this.humanDecoration, []);
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
      this.clear();
      return;
    }

    const agentDecorations: vscode.DecorationOptions[] = [];
    const humanDecorations: vscode.DecorationOptions[] = [];

    for (const block of collapseBlocks(result.lines)) {
      // Annotate the first non-empty line of the block; skip all-blank blocks.
      const anchor = firstNonEmptyLine(editor.document, block.startLine, block.endLine);
      if (anchor === null) continue;

      const range = new vscode.Range(anchor, 0, anchor, 0);
      const author = authorOf(block.line);
      const decoration: vscode.DecorationOptions = {
        range,
        renderOptions: {
          after: { contentText: `  ${author.label} · ${formatRelativeTime(block.line.timestamp)}` },
        },
        hoverMessage: buildHover(block.line, author),
      };
      (author.isAgent ? agentDecorations : humanDecorations).push(decoration);
    }

    editor.setDecorations(this.agentDecoration, agentDecorations);
    editor.setDecorations(this.humanDecoration, humanDecorations);
  }

  dispose(): void {
    this.agentDecoration.dispose();
    this.humanDecoration.dispose();
    this.disposables.forEach((d) => d.dispose());
  }
}

/** A contiguous run of lines that share the same originating snapshot. */
interface LineBlock {
  startLine: number; // 1-based, inclusive
  endLine: number; // 1-based, inclusive
  line: LineAnnotation; // the annotation for the block (all lines share snapshot_id)
}

/**
 * collapseBlocks groups the per-line annotations into runs of consecutive lines
 * that share a snapshot_id, so each block is annotated once instead of every
 * line. Assumes `lines` is ordered by line number.
 */
export function collapseBlocks(lines: LineAnnotation[]): LineBlock[] {
  const blocks: LineBlock[] = [];
  for (const line of lines) {
    const last = blocks[blocks.length - 1];
    if (last && last.line.snapshot_id === line.snapshot_id && line.line === last.endLine + 1) {
      last.endLine = line.line;
    } else {
      blocks.push({ startLine: line.line, endLine: line.line, line });
    }
  }
  return blocks;
}

/** Returns the 0-based index of the first non-blank line in [start, end] (1-based), or null. */
function firstNonEmptyLine(doc: vscode.TextDocument, start: number, end: number): number | null {
  for (let ln = start; ln <= end; ln++) {
    const idx = ln - 1;
    if (idx >= 0 && idx < doc.lineCount && doc.lineAt(idx).text.trim().length > 0) {
      return idx;
    }
  }
  return null;
}

interface Author {
  label: string; // "you" for human-authored, otherwise the agent name
  isAgent: boolean;
}

/**
 * authorOf classifies a line's originating snapshot as agent- or human-authored.
 * Human-origin: no agent name, or the extension's automatic save-snapshots
 * ("auto"), which capture a human's own edits. Everything else is a named AI
 * agent (e.g. "claude", "cursor", or the MCP default "agent").
 */
export function authorOf(line: LineAnnotation): Author {
  const name = (line.agent_name ?? '').trim();
  if (name === '' || name.toLowerCase() === 'auto') {
    return { label: 'you', isAgent: false };
  }
  return { label: name, isAgent: true };
}

function buildHover(line: LineAnnotation, author: Author): vscode.MarkdownString {
  const who = author.isAgent ? `agent \`${author.label}\`` : 'you';
  const when = new Date(line.timestamp * 1000).toLocaleString();
  const md = new vscode.MarkdownString(
    `**${line.label}**\n\n` +
    `Introduced by ${who} · ${when}\n\n` +
    `Snapshot \`${line.snapshot_id}\``,
  );
  md.isTrusted = false;
  return md;
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
