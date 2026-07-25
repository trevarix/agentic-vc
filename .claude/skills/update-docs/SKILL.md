# /update-docs

Update the AVC documentation site (the separate `avc-docs` repo) to reflect changes made in this repo — new or changed CLI commands, MCP tools, config, or behavior. Use after landing a feature or cutting a release, when the user asks to "update the docs", "sync the docs", or "document <version>".

## Usage

```
/update-docs                 # document the latest release section in CHANGELOG.md
/update-docs <version>       # document a specific version's CHANGELOG section
/update-docs <description>   # document a specific change described in prose
```

Examples: `/update-docs 0.4.0`, `/update-docs the new avc check-ignore command`.

---

## Where the docs live

The docs are a **separate Astro + Starlight repo**, not part of this one. Locate it in this order:

1. `../avc-docs` relative to this repo's root (the default sibling checkout).
2. If not there, ask the user for the path. Confirm it's the docs repo by checking for `astro.config.mjs` and `src/content/docs/`.

All content is Markdown/MDX under `src/content/docs/`. The sidebar in `astro.config.mjs` **auto-generates** the `cli/`, `extension/`, `web-ui/`, and `agents/` sections from their directories — so a new page in `cli/` appears automatically; you do **not** edit `astro.config.mjs` for a new command page. The `Introduction` and `Concepts` groups are hand-listed there, so a brand-new top-level page in those areas does need a sidebar entry.

Page layout:

| Directory | Covers |
|-----------|--------|
| `cli/<command>.md` | One page per `avc` subcommand |
| `agents/` | MCP server, per-framework setup (`claude-code`, `cursor`, `windsurf`), `mcp.md` tool list |
| `concepts/` | `snapshots`, `branches`, `merges`, `storage` — behavior and model |
| `extension/`, `web-ui/` | VSCode extension and web UI |
| `install.md`, `quick-start.md`, `why.md`, `index.mdx` | Top-level intro |

---

## Steps

1. **Determine what changed.**
   - With a `<version>`: read that version's section from this repo's `CHANGELOG.md`.
   - With no argument: read the most recent released section in `CHANGELOG.md`.
   - With a prose `<description>`: use it directly.
   - Cross-check against the actual code when a claim is ambiguous — the docs must match current behavior, not just the changelog wording. Verify command flags, JSON field names, MCP tool args, and file paths against `avc/cmd/avc/`, `avc/internal/mcp/`, and `avc/internal/` before writing them.

2. **Map each change to the pages it touches.** Common mappings:

   | Change | Pages to update |
   |--------|-----------------|
   | New CLI command | New `cli/<command>.md` (auto-sidebar) |
   | Changed flags / JSON output of a command | That command's `cli/*.md` |
   | New or changed MCP tool / tool arg | `agents/mcp.md` (tool table + behavior notes) |
   | MCP config location / setup | `cli/init.md`, `agents/mcp.md`, `agents/<framework>.md`, `concepts/storage.md`, `install.md` |
   | `.avcignore` / snapshot / branch / merge semantics | `concepts/*.md`, and the relevant `cli/*.md` |
   | Install/packaging | `install.md` |

3. **Check the current state before editing.** `grep` the docs for the terms involved (old flag names, old paths, field names). Docs drift — fix stale text you find in the same pages, not only the exact lines the change touches.

4. **Write the updates**, following the conventions below. Prefer editing existing pages; create a new page only for a genuinely new command or concept.

5. **Build to verify.** From the docs repo: `npm run build`. It must complete with no errors. Then confirm:
   - Any new page built (`dist/<path>/index.html` exists) and the page count went up.
   - Every internal link and `#anchor` you added resolves — anchors come from heading text (`## What's excluded` → `#whats-excluded`); grep the built HTML for the `id="..."` you linked to.

6. **Report** the files changed and the build result. Do **not** commit or push the docs repo — leave that to the user (see Rules).

---

## Conventions

- **Frontmatter** on every page:
  ```
  ---
  title: avc <command>
  description: <one sentence, imperative>.
  ---
  ```
  Match the voice of neighbouring pages: short lead paragraph, then `## Usage`, `## Flags`, `## JSON output`, and a `## See also` when cross-referencing.
- **Internal links** are root-relative with a trailing slash: `/cli/snapshot/`, `/concepts/snapshots/#ignoring-never-untracks`. Never link to `.md` files.
- **Match existing structure** — copy the shape of a sibling page (`cli/status.md` is a good small template) rather than inventing a new layout.
- **Only document user-facing surface.** Purely internal changes (a performance win, an internal refactor) belong in the changelog, not a new docs page — unless they change something a user observes. When unsure, ask.
- **Fix, don't duplicate.** If a page already covers the topic with outdated content, correct it in place.

---

## Rules

- Never edit files in this repo (`agentic-vc`) — this skill only writes to the docs repo. Reading this repo's code and `CHANGELOG.md` for accuracy is expected.
- Never `git commit`, `git push`, or `git tag` in the docs repo. Report what changed and let the user commit (their PR/commit workflow is their own).
- Never edit `astro.config.mjs` for an auto-generated section (`cli/`, `agents/`, `extension/`, `web-ui/`); only add a sidebar entry when creating a new page under a hand-listed group (`Introduction`, `Concepts`).
- Never claim a flag, field, path, or tool arg that you have not verified against the current code.
- Always run `npm run build` and confirm it passes before reporting done. If it fails, fix the cause — do not report success on a broken build.
