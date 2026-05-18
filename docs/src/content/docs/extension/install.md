---
title: Install & Setup
description: Get the AVC VSCode extension running in three steps.
sidebar:
  order: 1
---

The AVC extension brings snapshot management directly into VSCode — sidebar, Source Control panel, gutter annotations, and more. Pick the install path that matches how you got AVC.

## Prerequisites

- The `avc` CLI must be on your `PATH` (run `avc --version` to confirm) — see [Installation](/agentic-vc/install/)
- Your project must be initialized with `avc init`
- VSCode **1.85+**

## Option A — Marketplace

The simplest install once published:

1. Open VSCode and press `Ctrl+Shift+X` (or `Cmd+Shift+X`) to open the Extensions panel
2. Search for **AVC — Agentic Version Control**
3. Click **Install**, then reload VSCode if prompted

## Option B — Local VSIX

Useful for testing pre-release builds:

1. Run `vsce package` in the `extension/` folder to produce `avc-0.2.0.vsix`
2. In VSCode: `Ctrl+Shift+P` → **Extensions: Install from VSIX…**
3. Pick the generated `.vsix` file

## Option C — Development mode

For contributing to the extension itself:

1. Open the `extension/` folder in VSCode: `code extension/`
2. Run `npm install && npm run compile`
3. Press `F5` — a second window opens labeled **[Extension Development Host]**
4. In that window, open any AVC-initialized folder

## Verify the install

You should see all of the following in your project window:

- A **camera icon** in the activity bar (left edge)
- A **status bar item** like `AVC: 4 snapshots`
- A **branch indicator** next to it: `main`
- An **AVC group** in the Source Control panel (`Ctrl+Shift+G`)

:::tip
If the sidebar shows "Loading…" forever, run `avc --version` in your terminal. If it errors, the extension can't find the CLI — set `avc.cliPath` in [settings](/agentic-vc/extension/settings/).
:::
