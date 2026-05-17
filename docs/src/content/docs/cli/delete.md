---
title: avc delete
description: Permanently delete a snapshot and any unreferenced objects.
---

Removes a snapshot from the database. Any object blobs that are no longer referenced by other snapshots are also garbage-collected.

## Usage

```bash
avc delete snap-abc123
avc delete snap-abc123 --json
```

## JSON output

```json
{
  "id": "snap-abc123",
  "success": true
}
```

## Caution

:::caution
This is permanent. There is no undo. Once a snapshot is deleted, the only way to recover those files is from another snapshot that contains them (look up the file with [`avc file-history`](/agentic-vc/cli/file-history/)).
:::

If you only want to retire a snapshot from view but keep its data, you can ignore it instead — AVC has no archive concept, but you can filter it out in the [VSCode extension](/agentic-vc/extension/) or [Web UI](/agentic-vc/web-ui/) sidebar.
