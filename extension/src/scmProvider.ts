import * as vscode from 'vscode';
import * as path from 'path';
import { getDiffCurrent, listSnapshots, resolveProjectPath } from './cliProxy';

/** Registers AVC as a VSCode Source Control provider. */
export class AvcScmProvider implements vscode.Disposable {
  private scm: vscode.SourceControl | undefined;
  private changedGroup: vscode.SourceControlResourceGroup | undefined;
  private disposables: vscode.Disposable[] = [];
  private refreshTimer: ReturnType<typeof setTimeout> | undefined;

  constructor() {
    const projectPath = resolveProjectPath();
    if (!projectPath) return;

    this.scm = vscode.scm.createSourceControl('avc', 'AVC', vscode.Uri.file(projectPath));
    this.scm.acceptInputCommand = { command: 'avc.saveSnapshot', title: 'Snapshot' };
    this.scm.inputBox.placeholder = 'Snapshot label...';

    this.changedGroup = this.scm.createResourceGroup('changes', 'Changes since last snapshot');
    this.changedGroup.hideWhenEmpty = true;

    this.disposables.push(this.scm);

    // Debounced refresh on file save.
    const saveWatcher = vscode.workspace.onDidSaveTextDocument(() => {
      if (this.refreshTimer) clearTimeout(this.refreshTimer);
      this.refreshTimer = setTimeout(() => this.refresh(), 2000);
    });
    this.disposables.push(saveWatcher);

    // Initial refresh.
    this.refresh();
  }

  async refresh(): Promise<void> {
    if (!this.scm || !this.changedGroup) return;

    const projectPath = resolveProjectPath();
    if (!projectPath) return;

    try {
      const snapshots = await listSnapshots(projectPath);
      if (snapshots.length === 0) {
        this.changedGroup.resourceStates = [];
        this.scm.count = 0;
        return;
      }

      const latestId = snapshots[0].id;
      const diff = await getDiffCurrent(projectPath, latestId);

      this.changedGroup.resourceStates = diff.files.map((f) => {
        const uri = vscode.Uri.file(path.join(projectPath, f.path));
        const icon =
          f.type === 'added' ? 'diff-added' :
          f.type === 'deleted' ? 'diff-removed' : 'diff-modified';
        return {
          resourceUri: uri,
          decorations: {
            strikeThrough: f.type === 'deleted',
            tooltip: `${f.type}: +${f.lines_added} -${f.lines_removed}`,
            iconPath: new vscode.ThemeIcon(icon),
          },
        };
      });

      this.scm.count = diff.files.length;
    } catch {
      // SCM refresh failures are non-critical.
    }
  }

  dispose(): void {
    if (this.refreshTimer) clearTimeout(this.refreshTimer);
    this.disposables.forEach((d) => d.dispose());
  }
}
