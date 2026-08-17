/**
 * Unit tests for the annotation grouping/classification logic.
 * vscode is mocked via __mocks__/vscode.ts.
 */

import { collapseBlocks, authorOf } from '../gutterAnnotations';
import { LineAnnotation } from '../cliProxy';

function line(n: number, snap: string, agent = ''): LineAnnotation {
  return { line: n, snapshot_id: snap, label: `snap ${snap}`, agent_name: agent, timestamp: 1000 };
}

describe('collapseBlocks', () => {
  it('groups consecutive lines sharing a snapshot into one block', () => {
    const blocks = collapseBlocks([
      line(1, 'A'), line(2, 'A'), line(3, 'A'),
      line(4, 'B'),
      line(5, 'A'), line(6, 'A'),
    ]);
    expect(blocks.map((b) => [b.startLine, b.endLine, b.line.snapshot_id])).toEqual([
      [1, 3, 'A'],
      [4, 4, 'B'],
      [5, 6, 'A'], // a later run of A is its own block, like git blame
    ]);
  });

  it('breaks a block when line numbers are non-contiguous', () => {
    const blocks = collapseBlocks([line(1, 'A'), line(2, 'A'), line(9, 'A')]);
    expect(blocks).toHaveLength(2);
    expect(blocks[1].startLine).toBe(9);
  });

  it('returns nothing for no lines', () => {
    expect(collapseBlocks([])).toEqual([]);
  });
});

describe('authorOf', () => {
  it('classifies an empty agent name as human ("you")', () => {
    expect(authorOf(line(1, 'A', ''))).toEqual({ label: 'you', isAgent: false });
  });

  it('classifies the auto-save agent as human (it captures a human\'s edits)', () => {
    expect(authorOf(line(1, 'A', 'auto'))).toEqual({ label: 'you', isAgent: false });
    expect(authorOf(line(1, 'A', 'AUTO'))).toEqual({ label: 'you', isAgent: false });
  });

  it('classifies a named AI agent as agent-authored', () => {
    expect(authorOf(line(1, 'A', 'claude'))).toEqual({ label: 'claude', isAgent: true });
    expect(authorOf(line(1, 'A', 'agent'))).toEqual({ label: 'agent', isAgent: true });
    expect(authorOf(line(1, 'A', 'cursor'))).toEqual({ label: 'cursor', isAgent: true });
  });
});
