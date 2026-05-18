/**
 * Unit tests for sidebar.ts.
 *
 * Tests the SnapshotProvider's filtering, grouping, and tree-item logic
 * without launching VS Code — all vscode APIs are satisfied by the manual mock.
 */

import { SnapshotProvider, SnapshotItem, ActionItem, GroupItem } from '../sidebar';
import { Snapshot } from '../cliProxy';
import * as cliProxy from '../cliProxy';

jest.mock('../cliProxy');

const mockListSnapshots = cliProxy.listSnapshots as jest.MockedFunction<typeof cliProxy.listSnapshots>;
const mockListBranches = cliProxy.listBranches as jest.MockedFunction<typeof cliProxy.listBranches>;
const mockResolveProjectPath = cliProxy.resolveProjectPath as jest.MockedFunction<typeof cliProxy.resolveProjectPath>;

/** Build a minimal Snapshot fixture. */
function makeSnapshot(overrides: Partial<Snapshot> & { id: string; label: string }): Snapshot {
  return {
    timestamp: Math.floor(Date.now() / 1000),
    agent_name: 'test-agent',
    files_changed: 1,
    total_size: 512,
    notes: '',
    ...overrides,
  };
}

beforeEach(() => {
  jest.clearAllMocks();
  mockResolveProjectPath.mockReturnValue('/test/project');
  mockListBranches.mockResolvedValue([
    { id: 'b-main', name: 'main', base_snapshot_id: '', created_at: 0, active: true, workspace: '' },
  ]);
});

// ─── load ─────────────────────────────────────────────────────────────────────

describe('SnapshotProvider.load()', () => {
  it('populates snapshots from the CLI', async () => {
    const snapshots = [
      makeSnapshot({ id: 's1', label: 'first' }),
      makeSnapshot({ id: 's2', label: 'second' }),
    ];
    mockListSnapshots.mockResolvedValue(snapshots);

    const provider = new SnapshotProvider();
    await provider.load();

    expect(provider.getVisibleSnapshotCount()).toBe(2);
  });

  it('sets snapshots to empty when resolveProjectPath returns undefined', async () => {
    mockResolveProjectPath.mockReturnValue(undefined);

    const provider = new SnapshotProvider();
    await provider.load();

    expect(provider.getVisibleSnapshotCount()).toBe(0);
  });

  it('sets snapshots to empty and does not throw when listSnapshots rejects', async () => {
    mockListSnapshots.mockRejectedValue(new Error('CLI unavailable'));

    const provider = new SnapshotProvider();
    await expect(provider.load()).resolves.not.toThrow();
    expect(provider.getVisibleSnapshotCount()).toBe(0);
  });

  it('is idempotent when called concurrently (guards against duplicate loads)', async () => {
    mockListSnapshots.mockResolvedValue([makeSnapshot({ id: 's1', label: 'only' })]);

    const provider = new SnapshotProvider();
    await Promise.all([provider.load(), provider.load()]);

    expect(mockListSnapshots).toHaveBeenCalledTimes(1);
  });

  it('detects the active branch from the branch list', async () => {
    mockListSnapshots.mockResolvedValue([]);
    mockListBranches.mockResolvedValue([
      { id: 'b-main', name: 'main', base_snapshot_id: '', created_at: 0, active: false, workspace: '' },
      { id: 'b-feat', name: 'feat/x', base_snapshot_id: '', created_at: 0, active: true, workspace: '' },
    ]);

    const provider = new SnapshotProvider();
    await provider.load();

    expect(provider.activeBranch).toBe('feat/x');
  });
});

// ─── setFilter ────────────────────────────────────────────────────────────────

describe('SnapshotProvider.setFilter()', () => {
  async function loadedProvider(snapshots: Snapshot[]): Promise<SnapshotProvider> {
    mockListSnapshots.mockResolvedValue(snapshots);
    const p = new SnapshotProvider();
    await p.load();
    return p;
  }

  it('filters by label (case-insensitive)', async () => {
    const provider = await loadedProvider([
      makeSnapshot({ id: 's1', label: 'Feature A' }),
      makeSnapshot({ id: 's2', label: 'Bug fix B' }),
    ]);

    provider.setFilter('feature');
    expect(provider.getVisibleSnapshotCount()).toBe(1);
  });

  it('filters by agent name', async () => {
    const provider = await loadedProvider([
      makeSnapshot({ id: 's1', label: 'snap', agent_name: 'claude' }),
      makeSnapshot({ id: 's2', label: 'snap', agent_name: 'gpt' }),
    ]);

    provider.setFilter('claude');
    expect(provider.getVisibleSnapshotCount()).toBe(1);
  });

  it('filters by snapshot ID', async () => {
    const provider = await loadedProvider([
      makeSnapshot({ id: 'snap-abc-123', label: 'first' }),
      makeSnapshot({ id: 'snap-xyz-456', label: 'second' }),
    ]);

    provider.setFilter('abc');
    expect(provider.getVisibleSnapshotCount()).toBe(1);
  });

  it('returns all snapshots when filter is cleared', async () => {
    const provider = await loadedProvider([
      makeSnapshot({ id: 's1', label: 'alpha' }),
      makeSnapshot({ id: 's2', label: 'beta' }),
    ]);

    provider.setFilter('alpha');
    expect(provider.getVisibleSnapshotCount()).toBe(1);

    provider.setFilter('');
    expect(provider.getVisibleSnapshotCount()).toBe(2);
  });

  it('returns 0 when no snapshots match the filter', async () => {
    const provider = await loadedProvider([makeSnapshot({ id: 's1', label: 'hello' })]);

    provider.setFilter('zzz-no-match');
    expect(provider.getVisibleSnapshotCount()).toBe(0);
  });
});

// ─── getLatestSnapshot ────────────────────────────────────────────────────────

describe('SnapshotProvider.getLatestSnapshot()', () => {
  it('returns the first snapshot in the list (newest first)', async () => {
    const snapshots = [
      makeSnapshot({ id: 's1', label: 'newest' }),
      makeSnapshot({ id: 's2', label: 'older' }),
    ];
    mockListSnapshots.mockResolvedValue(snapshots);
    const provider = new SnapshotProvider();
    await provider.load();

    expect(provider.getLatestSnapshot()?.id).toBe('s1');
  });

  it('returns undefined when no snapshots are loaded', async () => {
    mockListSnapshots.mockResolvedValue([]);
    const provider = new SnapshotProvider();
    await provider.load();

    expect(provider.getLatestSnapshot()).toBeUndefined();
  });
});

// ─── getAllVisibleSnapshotItems ────────────────────────────────────────────────

describe('SnapshotProvider.getAllVisibleSnapshotItems()', () => {
  it('returns SnapshotItem instances for each visible snapshot', async () => {
    const snapshots = [
      makeSnapshot({ id: 's1', label: 'first' }),
      makeSnapshot({ id: 's2', label: 'second' }),
    ];
    mockListSnapshots.mockResolvedValue(snapshots);
    const provider = new SnapshotProvider();
    await provider.load();

    const items = provider.getAllVisibleSnapshotItems();
    expect(items).toHaveLength(2);
    expect(items[0]).toBeInstanceOf(SnapshotItem);
    expect(items[0].snapshot.id).toBe('s1');
  });

  it('respects the active filter', async () => {
    const snapshots = [
      makeSnapshot({ id: 's1', label: 'keep this' }),
      makeSnapshot({ id: 's2', label: 'remove this' }),
    ];
    mockListSnapshots.mockResolvedValue(snapshots);
    const provider = new SnapshotProvider();
    await provider.load();
    provider.setFilter('keep');

    const items = provider.getAllVisibleSnapshotItems();
    expect(items).toHaveLength(1);
    expect(items[0].snapshot.id).toBe('s1');
  });
});

// ─── getChildren ──────────────────────────────────────────────────────────────

describe('SnapshotProvider.getChildren()', () => {
  async function providerWith(snapshots: Snapshot[]): Promise<SnapshotProvider> {
    mockListSnapshots.mockResolvedValue(snapshots);
    const p = new SnapshotProvider();
    await p.load();
    return p;
  }

  it('returns GroupItem array at root level when snapshots exist', async () => {
    const provider = await providerWith([makeSnapshot({ id: 's1', label: 'test' })]);

    const children = provider.getChildren(undefined);
    expect(children.length).toBeGreaterThan(0);
    expect(children[0]).toBeInstanceOf(GroupItem);
  });

  it('returns empty array at root level when no snapshots', async () => {
    const provider = await providerWith([]);
    const children = provider.getChildren(undefined);
    expect(children).toHaveLength(0);
  });

  it('returns SnapshotItems as children of a GroupItem', async () => {
    const provider = await providerWith([makeSnapshot({ id: 's1', label: 'test' })]);

    const groups = provider.getChildren(undefined) as GroupItem[];
    const groupChildren = provider.getChildren(groups[0]);
    expect(groupChildren[0]).toBeInstanceOf(SnapshotItem);
  });

  it('returns ActionItems as children of a SnapshotItem', async () => {
    const snapshot = makeSnapshot({ id: 's1', label: 'test' });
    const provider = await providerWith([snapshot]);

    const snapshotItem = new SnapshotItem(snapshot);
    const children = provider.getChildren(snapshotItem);

    expect(children.length).toBeGreaterThan(0);
    expect(children[0]).toBeInstanceOf(ActionItem);
  });

  it('returns empty array as children of an ActionItem', async () => {
    const snapshot = makeSnapshot({ id: 's1', label: 'test' });
    const provider = await providerWith([snapshot]);
    const snapshotItem = new SnapshotItem(snapshot);

    // Get first action item.
    const [actionItem] = provider.getChildren(snapshotItem) as ActionItem[];
    const grandchildren = provider.getChildren(actionItem);

    expect(grandchildren).toHaveLength(0);
  });

  it('groups snapshots by date bucket (Today appears first)', async () => {
    const nowSec = Math.floor(Date.now() / 1000);
    const provider = await providerWith([
      makeSnapshot({ id: 's1', label: 'recent', timestamp: nowSec }),
    ]);

    const groups = provider.getChildren(undefined) as GroupItem[];
    expect(groups[0].groupLabel).toBe('Today');
  });

  it('filters affect getChildren at root level', async () => {
    const provider = await providerWith([
      makeSnapshot({ id: 's1', label: 'match me' }),
      makeSnapshot({ id: 's2', label: 'not this' }),
    ]);

    provider.setFilter('match');
    const groups = provider.getChildren(undefined) as GroupItem[];
    const totalSnaps = groups.reduce((sum, g) => sum + g.snapshots.length, 0);
    expect(totalSnaps).toBe(1);
  });
});

// ─── SnapshotItem ─────────────────────────────────────────────────────────────

describe('SnapshotItem', () => {
  it('uses snapshot label as the tree item label', () => {
    const snap = makeSnapshot({ id: 's1', label: 'My Snapshot' });
    const item = new SnapshotItem(snap);
    expect(item.label).toBe('My Snapshot');
  });

  it('sets contextValue to "snapshot"', () => {
    const item = new SnapshotItem(makeSnapshot({ id: 's1', label: 'snap' }));
    expect(item.contextValue).toBe('snapshot');
  });

  it('includes snapshot ID in tooltip', () => {
    const snap = makeSnapshot({ id: 'snap-unique-id', label: 'snap' });
    const item = new SnapshotItem(snap);
    expect(item.tooltip).toContain('snap-unique-id');
  });
});

// ─── GroupItem ────────────────────────────────────────────────────────────────

describe('GroupItem', () => {
  it('uses description to show snapshot count', () => {
    const snaps = [
      makeSnapshot({ id: 's1', label: 'a' }),
      makeSnapshot({ id: 's2', label: 'b' }),
    ];
    const group = new GroupItem('Today', snaps);
    expect(group.description).toBe('2');
  });

  it('stores the group label', () => {
    const group = new GroupItem('This Week', []);
    expect(group.groupLabel).toBe('This Week');
  });
});
