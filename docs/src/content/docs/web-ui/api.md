---
title: REST API
description: HTTP endpoints exposed by `avc ui`. Use these to build custom integrations.
sidebar:
  order: 2
---

The `avc ui` server is also a small REST API. Every endpoint returns JSON. The web UI itself is built on top of these endpoints — if a feature exists in the browser, you can call it from `curl`, a Python script, or any HTTP client.

## Base URL

```
http://127.0.0.1:3004
```

Add `--port` to change the port (see [`avc ui`](/agentic-vc/cli/ui/) flags).

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/project` | Project name, path, active branch |
| `GET` | `/api/snapshots` | List snapshots on the active branch |
| `POST` | `/api/snapshots/create` | Create a snapshot |
| `GET` | `/api/snapshots/<id>` | Snapshot detail with file list |
| `DELETE` | `/api/snapshots/<id>` | Delete a snapshot |
| `GET` | `/api/diff?from=&to=` | Diff two snapshots |
| `GET` | `/api/diff-current?id=` | Diff snapshot vs working tree |
| `POST` | `/api/restore` | Restore full snapshot |
| `POST` | `/api/restore-file` | Restore single file |

## Examples

### List snapshots

```bash
curl http://127.0.0.1:3004/api/snapshots
```

```json
[
  { "id": "snap-abc", "label": "Baseline", "timestamp": 1712282400, ... },
  ...
]
```

### Create a snapshot

```bash
curl -X POST http://127.0.0.1:3004/api/snapshots/create \
  -H "Content-Type: application/json" \
  -d '{"label":"Via API","agent":"my-script","notes":"From curl"}'
```

```json
{
  "id": "snap-xyz",
  "label": "Via API",
  "success": true
}
```

### Restore

```bash
curl -X POST http://127.0.0.1:3004/api/restore \
  -H "Content-Type: application/json" \
  -d '{"snapshot_id":"snap-abc"}'
```

### Diff two snapshots

```bash
curl "http://127.0.0.1:3004/api/diff?from=snap-abc&to=snap-def"
```

Returns the same JSON shape as [`avc diff`](/agentic-vc/cli/diff/).

## Error responses

Errors return an HTTP status code and a JSON error body:

```json
{ "error": "snapshot 'snap-foo' not found" }
```

## CORS

The server does **not** send CORS headers. The web UI is served from the same origin as the API, so it works seamlessly. If you want to call the API from a browser app on a different origin, you'll need to proxy through your own backend.

## Stability

This API is considered stable for the v1 web UI surface. Future versions may add endpoints; they won't remove or rename existing ones without a major version bump.
