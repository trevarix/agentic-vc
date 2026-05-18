---
title: avc restore
description: Roll back the entire project to a previous snapshot.
---

Restores every file in the project to the state captured in the snapshot. Files not present in the snapshot are deleted.

## Usage

```bash
avc restore snap-abc123
avc restore snap-abc123 --json
```

## JSON output

```json
{
  "id": "snap-abc123",
  "restored_files": 12,
  "restored_size": 524288,
  "success": true,
  "message": "Successfully restored snapshot snap-abc123"
}
```

## Safety

:::caution[Reversible, but consequential]
Restore overwrites current files. The VSCode extension and Web UI both take an automatic **pre-restore safety snapshot** before calling `avc restore`. From the CLI, you should do the same:

```bash
avc snapshot "Before restore"
avc restore snap-abc123
```

If the restore was wrong, restore the safety snapshot:

```bash
avc list                          # find the safety snapshot ID
avc restore <safety-snapshot-id>  # undo the undo
```
:::

## Branch-aware behaviour

On a non-main branch, restore writes to the **workspace directory** (`.avc/workspaces/<branch>/`), not the real project root. The real project is untouched.

On `main`, restore writes to the project root directly.

## See also

- [`avc restore-file`](/agentic-vc/cli/restore-file/) — restore one file instead of the whole snapshot
- [`avc diff-current`](/agentic-vc/cli/diff-current/) — preview what a restore would change
