#!/usr/bin/env python3
# Copyright (c) 2026 TREVARIX Corp.
# SPDX-License-Identifier: AGPL-3.0-or-later
"""Point the Claude plugin marketplace at a released tag.

The marketplace lives in a separate repository, so nothing in this one can
notice when its pin goes stale. Left unpinned it tracks the default branch,
which hands plugin users every mid-development commit the moment it lands;
left pinned to an old tag it strands them there forever. Both failure modes
are silent, which is why this runs from the release workflow rather than
living in a checklist.

Requires GH_TOKEN with Contents:write on the marketplace repository.

Usage:
    python scripts/pin-marketplace.py v0.5.0 [--dry-run]
"""

import base64
import json
import subprocess
import sys

MARKETPLACE_REPO = "trevarix/claude-marketplace"
MANIFEST_PATH = ".claude-plugin/marketplace.json"

# The plugin whose source ref this script maintains.
PLUGIN_NAME = "agentic-vc"


def gh_api(*args: str) -> str:
    """Run gh api and return stdout, failing loudly on error."""
    result = subprocess.run(
        ["gh", "api", *args], capture_output=True, text=True,
    )
    if result.returncode != 0:
        raise SystemExit(f"gh api {' '.join(args)} failed:\n{result.stderr.strip()}")
    return result.stdout


def main() -> int:
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    dry_run = "--dry-run" in sys.argv
    if not args:
        print(__doc__, file=sys.stderr)
        return 2
    tag = args[0]
    if not tag.startswith("v"):
        raise SystemExit(f"expected a tag like v0.5.0, got {tag!r}")

    current = json.loads(gh_api(f"repos/{MARKETPLACE_REPO}/contents/{MANIFEST_PATH}"))
    manifest = json.loads(base64.b64decode(current["content"]))

    plugins = [p for p in manifest.get("plugins", []) if p.get("name") == PLUGIN_NAME]
    if not plugins:
        raise SystemExit(f"{PLUGIN_NAME} is not listed in {MARKETPLACE_REPO}")
    plugin = plugins[0]

    # Plain ASCII: this prints to a console that may not be UTF-8.
    previous = plugin.get("source", {}).get("ref", "(unpinned, tracking the default branch)")
    if previous == tag:
        print(f"already pinned to {tag}; nothing to do")
        return 0

    plugin["source"]["ref"] = tag
    # ensure_ascii=False keeps the em dashes in the description literal rather
    # than rewriting them as \u escapes, so the commit diff shows only the ref.
    updated = json.dumps(manifest, indent=2, ensure_ascii=False) + "\n"

    print(f"{PLUGIN_NAME}: {previous} -> {tag}")
    if dry_run:
        print("\n--dry-run, not writing. Resulting manifest:\n")
        print(updated)
        return 0

    message = (
        f"chore: pin {PLUGIN_NAME} to {tag}\n\n"
        "Set by the agentic-vc release workflow. Plugin users move to this "
        "release on their next marketplace update."
    )
    gh_api(
        "--method", "PUT",
        f"repos/{MARKETPLACE_REPO}/contents/{MANIFEST_PATH}",
        "-f", f"message={message}",
        "-f", f"content={base64.b64encode(updated.encode()).decode()}",
        "-f", f"sha={current['sha']}",
    )
    print(f"pinned {MARKETPLACE_REPO} to {tag}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
