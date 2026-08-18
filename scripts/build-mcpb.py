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

# The manifest only needs to be readable. writestr defaults a bare filename
# to 0o600, owner-only, for a file the host must read to install anything.
MANIFEST_MODE = 0o644

# Unix host marker, so the modes above are honoured rather than ignored.
UNIX_CREATE_SYSTEM = 3

REPO_ROOT = Path(__file__).resolve().parent.parent
AVC_DIR = REPO_ROOT / "avc"

# The checked-in manifest template. Kept as data rather than built inline so
# avc/cmd/avc/mcpb_test.go can assert its argv still parses.
TEMPLATE = REPO_ROOT / "mcpb" / "manifest.json"


def manifest(version: str, platform: str, binary_name: str) -> dict:
    """Load the manifest template and fill in what varies per build.

    The template is checked in at mcpb/manifest.json rather than built here, so
    a Go test can validate its argv against the real command tree.

    The executable path is set here rather than in the template because bundles
    are per-platform: the Windows bundle can name avc.exe outright instead of
    relying on the host to append the extension. It also must be absolute —
    ${__dirname} resolves to the installed bundle directory, whereas a bare
    relative path is spawned against the host's working directory and fails
    with ENOENT.
    """
    data = json.loads(TEMPLATE.read_text(encoding="utf-8"))
    if data.get("manifest_version") != MANIFEST_VERSION:
        raise SystemExit(
            f"{TEMPLATE} targets manifest_version "
            f"{data.get('manifest_version')!r}, expected {MANIFEST_VERSION!r}"
        )
    data["version"] = version
    data["compatibility"]["platforms"] = [platform]
    data["server"]["entry_point"] = f"server/{binary_name}"
    data["server"]["mcp_config"]["command"] = f"${{__dirname}}/server/{binary_name}"
    return data


def build_binary(goos: str, goarch: str, out_path: Path, version: str) -> None:
    """Cross-compile the avc binary for one target.

    out_path must be absolute: this runs with cwd=avc/, so a relative -o would
    be written under avc/ rather than where the caller asked for it.
    """
    if not out_path.is_absolute():
        raise SystemExit(f"internal error: build output path {out_path} is not absolute")

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

    # go build can exit 0 without producing the file the caller expected — a
    # relative -o resolved against the wrong directory did exactly that.
    if not out_path.is_file():
        raise SystemExit(f"go build reported success but {out_path} does not exist")


def zip_entry(name: str, mode: int) -> zipfile.ZipInfo:
    """Build a ZipInfo carrying a Unix mode that extractors will honour.

    Permission bits live in the upper half of external_attr, but an extractor
    only reads them when the entry claims a Unix creator. Left at the default,
    a bundle built on Windows stores modes that every extractor on macOS and
    Linux ignores, and the binary lands without its executable bit.
    """
    info = zipfile.ZipInfo(name)
    info.create_system = UNIX_CREATE_SYSTEM
    info.external_attr = (mode | 0o100000) << 16
    info.compress_type = zipfile.ZIP_DEFLATED
    return info


def write_bundle(bundle_path: Path, manifest_data: dict, binary: Path, binary_name: str) -> None:
    """Zip the manifest and binary into a .mcpb archive.

    Both entries go through zip_entry so the archive is identical whichever
    platform built it. Handing writestr a bare filename instead stores mode
    0o600 and the building host's creator id, which makes a locally built
    bundle differ from the one CI ships.
    """
    bundle_path.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(bundle_path, "w", zipfile.ZIP_DEFLATED) as z:
        payload = json.dumps(manifest_data, indent=2) + chr(10)
        z.writestr(zip_entry("manifest.json", MANIFEST_MODE), payload)
        z.writestr(zip_entry("server/" + binary_name, BINARY_MODE), binary.read_bytes())


def main() -> int:
    if len(sys.argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2

    version = sys.argv[1].lstrip("v")
    # Resolved before use: the go build below runs with cwd=avc/, so a relative
    # output path would put the binary under avc/ while everything here looks
    # for it relative to the caller's directory.
    outdir = Path(sys.argv[2]).resolve() if len(sys.argv) > 2 else REPO_ROOT / "dist" / "mcpb"
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
        write_bundle(
            bundle,
            manifest(version, target["platform"], binary_name),
            binary,
            binary_name,
        )
        built.append(bundle)
        print(f"  -> {bundle.name} ({bundle.stat().st_size // 1024} KiB)")

    shutil.rmtree(staging, ignore_errors=True)
    print(f"\n{len(built)} bundle(s) written to {outdir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
