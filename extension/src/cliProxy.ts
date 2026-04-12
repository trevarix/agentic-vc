import { execFile } from 'child_process';
import * as vscode from 'vscode';

export interface Snapshot {
  id: string;
  label: string;
  timestamp: number;
  agent_name: string;
  files_changed: number;
  total_size: number;
  notes: string;
}

export interface SnapshotDetail extends Snapshot {
  file_count: number;
  files: SnapshotFile[];
}

export interface SnapshotFile {
  path: string;
  hash: string;
  size: number;
}

export interface RestoreResult {
  id: string;
  restored_files: number;
  restored_size: number;
  success: boolean;
  message: string;
}

export interface DiffResult {
  from_snapshot: string;
  to_snapshot: string;
  files: FileDiff[];
}

export interface Branch {
  id: string;
  name: string;
  base_snapshot_id: string;
  created_at: number;
  active: boolean;
  workspace: string;
}

export interface FileDiff {
  path: string;
  type: 'added' | 'modified' | 'deleted';
  old_hash?: string;
  new_hash?: string;
  lines_added: number;
  lines_removed: number;
  diff_preview?: string;
}

/** Executes an avc CLI command and returns the parsed JSON response. */
function runAvcCommand<T>(args: string[], projectPath?: string): Promise<T> {
  return new Promise((resolve, reject) => {
    const config = vscode.workspace.getConfiguration('avc');
    const cliPath: string = config.get('cliPath') ?? 'avc';

    const env = projectPath
      ? { ...process.env, AVC_PROJECT: projectPath }
      : process.env;

    execFile(cliPath, [...args, '--json'], { env, cwd: projectPath }, (error, stdout, stderr) => {
      if (error) {
        reject(new Error(stderr?.trim() || error.message));
        return;
      }
      try {
        resolve(JSON.parse(stdout) as T);
      } catch {
        reject(new Error('Invalid JSON response from avc CLI'));
      }
    });
  });
}

/** Returns the configured project path or falls back to the first workspace folder. */
export function resolveProjectPath(): string | undefined {
  const config = vscode.workspace.getConfiguration('avc');
  const configured: string = config.get('projectPath') ?? '';
  if (configured) return configured;
  return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
}

export async function listSnapshots(projectPath: string): Promise<Snapshot[]> {
  return runAvcCommand<Snapshot[]>(['list'], projectPath);
}

export async function createSnapshot(
  projectPath: string,
  label: string,
  agentName?: string,
  notes?: string
): Promise<Snapshot> {
  const args = ['snapshot', label];
  if (agentName) args.push('--agent', agentName);
  if (notes) args.push('--notes', notes);
  return runAvcCommand<Snapshot>(args, projectPath);
}

export async function restoreSnapshot(
  projectPath: string,
  snapshotId: string
): Promise<RestoreResult> {
  return runAvcCommand<RestoreResult>(['restore', snapshotId], projectPath);
}

export async function getDiff(
  projectPath: string,
  fromId: string,
  toId: string
): Promise<DiffResult> {
  return runAvcCommand<DiffResult>(['diff', fromId, toId], projectPath);
}

export async function getSnapshotInfo(
  projectPath: string,
  snapshotId: string
): Promise<SnapshotDetail> {
  return runAvcCommand<SnapshotDetail>(['info', snapshotId], projectPath);
}

export async function deleteSnapshot(
  projectPath: string,
  snapshotId: string
): Promise<void> {
  await runAvcCommand<{ success: boolean }>(['delete', snapshotId], projectPath);
}

export async function listBranches(projectPath: string): Promise<Branch[]> {
  return runAvcCommand<Branch[]>(['branch', 'list'], projectPath);
}

export async function createBranch(
  projectPath: string,
  name: string,
  fromSnapshotId?: string
): Promise<Branch> {
  const args = ['branch', 'create', name];
  if (fromSnapshotId) args.push('--from', fromSnapshotId);
  return runAvcCommand<Branch>(args, projectPath);
}

export async function switchBranch(
  projectPath: string,
  name: string
): Promise<{ name: string; success: boolean }> {
  return runAvcCommand<{ name: string; success: boolean }>(['branch', 'switch', name], projectPath);
}

export async function deleteBranch(
  projectPath: string,
  name: string
): Promise<void> {
  await runAvcCommand<{ success: boolean }>(['branch', 'delete', name], projectPath);
}

export interface MergeFileResult {
  path: string;
  decision: 'clean' | 'conflict' | 'skip';
}

export interface MergeResult {
  merge_id: string;
  branch: string;
  preview: boolean;
  clean: number;
  conflicts: number;
  skipped: number;
  files: MergeFileResult[];
}

export async function previewMerge(
  projectPath: string,
  branchName: string
): Promise<MergeResult> {
  return runAvcCommand<MergeResult>(['merge', branchName, '--preview'], projectPath);
}

export async function mergeBranch(
  projectPath: string,
  branchName: string
): Promise<MergeResult> {
  return runAvcCommand<MergeResult>(['merge', branchName], projectPath);
}

export async function abortMerge(projectPath: string): Promise<void> {
  await runAvcCommand<{ aborted: boolean }>(['merge', '--abort'], projectPath);
}