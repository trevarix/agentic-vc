/**
 * Unit tests for cliProxy.ts.
 *
 * child_process.execFile is mocked so tests never invoke the real avc binary.
 * The vscode module is mocked via __mocks__/vscode.ts.
 */

import { execFile } from 'child_process';
import {
  listSnapshots,
  createSnapshot,
  restoreSnapshot,
  deleteSnapshot,
  getDiff,
  getSnapshotInfo,
  listBranches,
  createBranch,
  switchBranch,
  deleteBranch,
  previewMerge,
  mergeBranch,
  abortMerge,
  getDiffCurrent,
  restoreFile,
  annotateFile,
  getFileHistory,
  catFileFromSnapshot,
  resolveProjectPath,
} from '../cliProxy';
import { __setConfig, __resetConfig, __setWorkspaceFolders, Uri } from '../../__mocks__/vscode';

jest.mock('child_process');

const mockExecFile = execFile as jest.MockedFunction<typeof execFile>;

/** Helper: make execFile call its callback with success + JSON payload. */
function mockSuccess(payload: unknown): void {
  mockExecFile.mockImplementation((...args: unknown[]) => {
    const cb = args[args.length - 1] as (
      err: null,
      stdout: string,
      stderr: string
    ) => void;
    cb(null, JSON.stringify(payload), '');
    return {} as ReturnType<typeof execFile>;
  });
}

/** Helper: make execFile call its callback with an error. */
function mockError(message: string): void {
  mockExecFile.mockImplementation((...args: unknown[]) => {
    const cb = args[args.length - 1] as (
      err: Error,
      stdout: string,
      stderr: string
    ) => void;
    cb(new Error('exit 1'), '', message);
    return {} as ReturnType<typeof execFile>;
  });
}

/** Helper: make execFile return non-JSON stdout. */
function mockBadJson(): void {
  mockExecFile.mockImplementation((...args: unknown[]) => {
    const cb = args[args.length - 1] as (
      err: null,
      stdout: string,
      stderr: string
    ) => void;
    cb(null, 'not-valid-json', '');
    return {} as ReturnType<typeof execFile>;
  });
}

beforeEach(() => {
  jest.clearAllMocks();
  __resetConfig();
});

// ─── resolveProjectPath ────────────────────────────────────────────────────────

describe('resolveProjectPath', () => {
  it('returns configured projectPath when set', () => {
    __setConfig('avc.projectPath', '/configured/path');
    expect(resolveProjectPath()).toBe('/configured/path');
  });

  it('falls back to first workspace folder when projectPath is empty', () => {
    __setConfig('avc.projectPath', '');
    __setWorkspaceFolders([{ uri: Uri.file('/workspace/root') }]);
    expect(resolveProjectPath()).toBe('/workspace/root');
  });

  it('returns undefined when no projectPath and no workspace folders', () => {
    __setConfig('avc.projectPath', '');
    __setWorkspaceFolders([]);
    expect(resolveProjectPath()).toBeUndefined();
  });
});

// ─── listSnapshots ─────────────────────────────────────────────────────────────

describe('listSnapshots', () => {
  it('returns parsed snapshot array on success', async () => {
    const expected = [
      { id: 'snap-1', label: 'first', timestamp: 1000, agent_name: 'claude', files_changed: 3, total_size: 512, notes: '' },
    ];
    mockSuccess(expected);

    const result = await listSnapshots('/proj');
    expect(result).toEqual(expected);
  });

  it('passes --json flag to the CLI', async () => {
    mockSuccess([]);
    await listSnapshots('/proj');

    const call = mockExecFile.mock.calls[0];
    const args = call[1] as string[];
    expect(args).toContain('--json');
    expect(args).toContain('list');
  });

  it('rejects with CLI error message on failure', async () => {
    mockError('project not initialized');
    await expect(listSnapshots('/proj')).rejects.toThrow('project not initialized');
  });

  it('rejects with parse error on invalid JSON', async () => {
    mockBadJson();
    await expect(listSnapshots('/proj')).rejects.toThrow('Invalid JSON response from avc CLI');
  });
});

// ─── createSnapshot ────────────────────────────────────────────────────────────

describe('createSnapshot', () => {
  const snapshotResponse = {
    id: 'snap-2', label: 'my snap', timestamp: 2000,
    agent_name: 'test', files_changed: 1, total_size: 100, notes: '',
  };

  it('sends label as the first positional argument', async () => {
    mockSuccess(snapshotResponse);
    await createSnapshot('/proj', 'my snap');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('snapshot');
    expect(args).toContain('my snap');
  });

  it('includes --agent flag when agentName is provided', async () => {
    mockSuccess(snapshotResponse);
    await createSnapshot('/proj', 'label', 'my-agent');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('--agent');
    expect(args).toContain('my-agent');
  });

  it('includes --notes flag when notes are provided', async () => {
    mockSuccess(snapshotResponse);
    await createSnapshot('/proj', 'label', undefined, 'some notes');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('--notes');
    expect(args).toContain('some notes');
  });

  it('omits --agent flag when agentName is undefined', async () => {
    mockSuccess(snapshotResponse);
    await createSnapshot('/proj', 'label');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).not.toContain('--agent');
  });

  it('returns the parsed snapshot', async () => {
    mockSuccess(snapshotResponse);
    const result = await createSnapshot('/proj', 'my snap');
    expect(result).toEqual(snapshotResponse);
  });
});

// ─── restoreSnapshot ──────────────────────────────────────────────────────────

describe('restoreSnapshot', () => {
  it('sends restore command with snapshot ID', async () => {
    mockSuccess({ id: 'snap-1', restored_files: 5, restored_size: 1024, success: true, message: 'ok' });
    await restoreSnapshot('/proj', 'snap-1');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('restore');
    expect(args).toContain('snap-1');
  });

  it('rejects on CLI error', async () => {
    mockError('snapshot not found');
    await expect(restoreSnapshot('/proj', 'snap-bad')).rejects.toThrow();
  });
});

// ─── deleteSnapshot ────────────────────────────────────────────────────────────

describe('deleteSnapshot', () => {
  it('sends delete command with snapshot ID', async () => {
    mockSuccess({ success: true });
    await deleteSnapshot('/proj', 'snap-del');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('delete');
    expect(args).toContain('snap-del');
  });
});

// ─── getDiff ──────────────────────────────────────────────────────────────────

describe('getDiff', () => {
  it('sends diff command with both snapshot IDs', async () => {
    mockSuccess({ from_snapshot: 'a', to_snapshot: 'b', files: [] });
    await getDiff('/proj', 'snap-a', 'snap-b');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('diff');
    expect(args).toContain('snap-a');
    expect(args).toContain('snap-b');
  });

  it('returns parsed diff result', async () => {
    const payload = {
      from_snapshot: 'snap-a',
      to_snapshot: 'snap-b',
      files: [{ path: 'main.go', type: 'modified', lines_added: 2, lines_removed: 1 }],
    };
    mockSuccess(payload);
    const result = await getDiff('/proj', 'snap-a', 'snap-b');
    expect(result.files).toHaveLength(1);
    expect(result.files[0].path).toBe('main.go');
  });
});

// ─── getSnapshotInfo ──────────────────────────────────────────────────────────

describe('getSnapshotInfo', () => {
  it('sends info command with snapshot ID', async () => {
    mockSuccess({ id: 'snap-1', label: 'test', timestamp: 0, agent_name: '', files_changed: 0, total_size: 0, notes: '', file_count: 0, files: [] });
    await getSnapshotInfo('/proj', 'snap-1');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('info');
    expect(args).toContain('snap-1');
  });
});

// ─── Branch operations ────────────────────────────────────────────────────────

describe('listBranches', () => {
  it('sends branch list command', async () => {
    mockSuccess([]);
    await listBranches('/proj');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('branch');
    expect(args).toContain('list');
  });
});

describe('createBranch', () => {
  it('sends branch create command with name', async () => {
    mockSuccess({ id: 'b-1', name: 'feat/x', base_snapshot_id: '', created_at: 0, active: false, workspace: '' });
    await createBranch('/proj', 'feat/x');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('create');
    expect(args).toContain('feat/x');
  });

  it('includes --from flag when fromSnapshotId is provided', async () => {
    mockSuccess({ id: 'b-1', name: 'feat/x', base_snapshot_id: 'snap-1', created_at: 0, active: false, workspace: '' });
    await createBranch('/proj', 'feat/x', 'snap-1');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('--from');
    expect(args).toContain('snap-1');
  });
});

describe('switchBranch', () => {
  it('sends branch switch command', async () => {
    mockSuccess({ name: 'feat/x', success: true });
    await switchBranch('/proj', 'feat/x');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('switch');
    expect(args).toContain('feat/x');
  });
});

describe('deleteBranch', () => {
  it('sends branch delete command', async () => {
    mockSuccess({ success: true });
    await deleteBranch('/proj', 'feat/old');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('delete');
    expect(args).toContain('feat/old');
  });
});

// ─── Merge operations ─────────────────────────────────────────────────────────

describe('previewMerge', () => {
  it('sends merge command with --preview flag', async () => {
    mockSuccess({ merge_id: '', branch: 'feat/x', preview: true, clean: 1, conflicts: 0, skipped: 0, files: [] });
    await previewMerge('/proj', 'feat/x');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('merge');
    expect(args).toContain('feat/x');
    expect(args).toContain('--preview');
  });
});

describe('mergeBranch', () => {
  it('sends merge command without --preview', async () => {
    mockSuccess({ merge_id: 'm-1', branch: 'feat/x', preview: false, clean: 2, conflicts: 0, skipped: 0, files: [] });
    await mergeBranch('/proj', 'feat/x');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('merge');
    expect(args).toContain('feat/x');
    expect(args).not.toContain('--preview');
  });
});

describe('abortMerge', () => {
  it('sends merge --abort command', async () => {
    mockSuccess({ aborted: true });
    await abortMerge('/proj');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('merge');
    expect(args).toContain('--abort');
  });
});

// ─── File operations ──────────────────────────────────────────────────────────

describe('getDiffCurrent', () => {
  it('sends diff-current command', async () => {
    mockSuccess({ from_snapshot: 'snap-1', to_snapshot: '', files: [] });
    await getDiffCurrent('/proj', 'snap-1');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('diff-current');
    expect(args).toContain('snap-1');
  });
});

describe('restoreFile', () => {
  it('sends restore-file command with snapshot ID and path', async () => {
    mockSuccess({ id: 'snap-1', file_path: 'src/app.go', size: 512, success: true, message: 'ok' });
    await restoreFile('/proj', 'snap-1', 'src/app.go');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('restore-file');
    expect(args).toContain('snap-1');
    expect(args).toContain('src/app.go');
  });
});

describe('annotateFile', () => {
  it('sends annotate command with file path', async () => {
    mockSuccess({ file_path: 'main.go', total_lines: 10, lines: [] });
    await annotateFile('/proj', 'main.go');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('annotate');
    expect(args).toContain('main.go');
  });
});

describe('getFileHistory', () => {
  it('sends file-history command', async () => {
    mockSuccess([]);
    await getFileHistory('/proj', 'src/service.go');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('file-history');
    expect(args).toContain('src/service.go');
  });
});

describe('catFileFromSnapshot', () => {
  it('decodes base64 content to UTF-8 string', async () => {
    const content = 'hello from snapshot';
    const base64 = Buffer.from(content, 'utf8').toString('base64');
    mockSuccess({ snapshot_id: 'snap-1', file_path: 'file.go', size: content.length, content_base64: base64 });

    const result = await catFileFromSnapshot('/proj', 'snap-1', 'file.go');
    expect(result).toBe(content);
  });

  it('sends cat command with snapshot ID and file path', async () => {
    mockSuccess({ snapshot_id: 'snap-1', file_path: 'file.go', size: 0, content_base64: '' });
    await catFileFromSnapshot('/proj', 'snap-1', 'file.go');

    const args = mockExecFile.mock.calls[0][1] as string[];
    expect(args).toContain('cat');
    expect(args).toContain('snap-1');
    expect(args).toContain('file.go');
  });
});

// ─── CLI path configuration ───────────────────────────────────────────────────

describe('CLI path', () => {
  it('uses custom cliPath from configuration', async () => {
    __setConfig('avc.cliPath', '/usr/local/bin/avc-custom');
    mockSuccess([]);
    await listSnapshots('/proj');

    const calledBinary = mockExecFile.mock.calls[0][0] as string;
    expect(calledBinary).toBe('/usr/local/bin/avc-custom');
  });

  it('defaults to "avc" when cliPath is not configured', async () => {
    mockSuccess([]);
    await listSnapshots('/proj');

    const calledBinary = mockExecFile.mock.calls[0][0] as string;
    expect(calledBinary).toBe('avc');
  });
});
