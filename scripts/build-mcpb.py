#!/usr/bin/env python3
# Copyright (c) 2026 TREVARIX Corp.
# SPDX-License-Identifier: AGPL-3.0-or-later
"""Build Claude Desktop extension bundles (.mcpb) for AVC.

Claude Desktop chat cannot run a plugin's MCP server — plugins contribute
skills there and nothing else. A Desktop extension is the only way to give
Desktop users the avc_* tools, so each release ships one bundle per platform.

One bundle per platform rather than a single universal one: a Go binary is
~20MB, and five of them in one archive makes a ~100MB download for the four
platforms a given user does not have.

Usage:
    python scripts/build-mcpb.py <version> [output-dir]
"""

import json
import os
import shutil
import subprocess
import sys
import zipfile
from pathlib import Path

# Build targets, matching .goreleaser.yml. Windows arm64 is skipped there too.
# "platform" is the MCPB spec's name for the OS: darwin, linux, or win32.
TARGETS = [
    {"goos": "darwin", "goarch": "arm64", "platform": "darwin"},
    {"goos": "darwin", "goarch": "amd64", "platform": "darwin"},
    {"goos": "linux", "goarch": "amd64", "platform": "linux"},
    {"goos": "linux", "goarch": "arm64", "platform": "linux"},
    {"goos": "windows", "goarch": "amd64", "platform": "win32"},
]

# The MCPB manifest spec version this file targets.
MANIFEST_VERSION = "0.3"

# Unix mode for the bundled binary. Without the executable bit the extension
# installs cleanly and then fails to start, which is the worst failure shape.
BINARY_MODE = 0o755

REPO_ROOT = Path(__file__).resolve().parent.parent
AVC_DIR = REPO_ROOT / "avc"

# The checked-in manifest template. Kept as data rather than built inline so
# avc/cmd/avc/mcpb_test.go can assert its argv still parses.
TEMPLATE = REPO_ROOT / "mcpb" / "manifest.json"


def manifest(version: str, platform: str) -> dict:
    """Load the manifest template and fill in this build's version and platform.

    The template is checked in at mcpb/manifest.json rather than built here, so
    a Go test can validate its argv against the real command tree. Everything
    that does not vary by build lives there; only these two fields are set.
    """
    data = json.loads(TEMPLATE.read_text(encoding="utf-8"))
    if data.get("manifest_version") != MANIFEST_VERSION:
        raise SystemExit(
            f"{TEMPLATE} targets manifest_version "
            f"{data.get('manifest_version')!r}, expected {MANIFEST_VERSION!r}"
        )
    data["version"] = version
    data["compatibility"]["platforms"] = [platform]
    return data


def build_binary(goos: str, goarch: str, out_path: Path, version: str) -> None:
    """Cross-compile the avc binary for one target."""
    env = dict(os.environ)
    env.update({"GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0"})
    subprocess.run(
        [
            "go", "build",
            "-trimpath",
            "-ldflags", f"-s -w -X main.version={version}",
            "-o", str(out_path),
            ".",
        ],
        cwd=AVC_DIR,
        env=env,
        check=True,
    )


def write_bundle(bundle_path: Path, manifest_data: dict, binary: Path, binary_name: str) -> None:
    """Zip the manifest and binary into a .mcpb archive."""
    bundle_path.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(bundle_path, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("manifest.json", json.dumps(manifest_data, indent=2) + "\n")

        # Written through ZipInfo so the executable bit survives; zipfile.write
        # would store the mode of the file as built on the host, which is not
        # executable when the build ran on Windows.
        info = zipfile.ZipInfo(f"server/{binary_name}")
        info.external_attr = (BINARY_MODE | 0o100000) << 16
        info.compress_type = zipfile.ZIP_DEFLATED
        z.writestr(info, binary.read_bytes())


def main() -> int:
    if len(sys.argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2

    version = sys.argv[1].lstrip("v")
    outdir = Path(sys.argv[2]) if len(sys.argv) > 2 else REPO_ROOT / "dist" / "mcpb"
    staging = outdir / ".build"
    staging.mkdir(parents=True, exist_ok=True)

    built = []
    for target in TARGETS:
        goos, goarch = target["goos"], target["goarch"]
        binary_name = "avc.exe" if goos == "windows" else "avc"
        binary = staging / f"{goos}-{goarch}" / binary_name

        binary.parent.mkdir(parents=True, exist_ok=True)
        print(f"building {goos}/{goarch} ...", flush=True)
        build_binary(goos, goarch, binary, version)

        bundle = outdir / f"avc-{version}-{goos}-{goarch}.mcpb"
        write_bundle(bundle, manifest(version, target["platform"]), binary, binary_name)
        built.append(bundle)
        print(f"  -> {bundle.name} ({bundle.stat().st_size // 1024} KiB)")

    shutil.rmtree(staging, ignore_errors=True)
    print(f"\n{len(built)} bundle(s) written to {outdir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
