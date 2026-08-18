#!/usr/bin/env python3
# Copyright (c) 2026 TREVARIX Corp.
# SPDX-License-Identifier: AGPL-3.0-or-later
"""Verify Claude Desktop extension bundles (.mcpb) built by build-mcpb.py.

These checks cover the parts of a bundle that only fail on a user's machine:
an executable missing its permission bit installs cleanly and then refuses to
launch, and a build-matrix slip that packs the same binary into all five
bundles is invisible until someone on the wrong architecture installs one.

Reads every .mcpb in a directory and exits non-zero on the first problem.

Usage:
    python scripts/verify-mcpb.py <bundle-dir>
"""

import json
import struct
import sys
import zipfile
from pathlib import Path

# Minimum plausible size for a real avc binary. A stub or an empty file would
# satisfy every structural check while being useless.
MIN_BINARY_BYTES = 1_000_000

# Entries must claim a Unix creator or their stored modes are ignored.
UNIX_CREATE_SYSTEM = 3

# Expected (executable format, machine) per "<goos>-<goarch>" build target,
# read from the binary's own header so a mismatched build is caught.
EXPECTED = {
    "darwin-amd64": ("macho", 0x01000007),  # CPU_TYPE_X86_64
    "darwin-arm64": ("macho", 0x0100000C),  # CPU_TYPE_ARM64
    "linux-amd64": ("elf", 0x3E),           # EM_X86_64
    "linux-arm64": ("elf", 0xB7),           # EM_AARCH64
    "windows-amd64": ("pe", 0x8664),        # IMAGE_FILE_MACHINE_AMD64
}

# MCPB platform name per GOOS.
PLATFORM = {"darwin": "darwin", "linux": "linux", "windows": "win32"}


def identify(data: bytes):
    """Return (format, machine) read from an executable header, or (None, None)."""
    if data[:4] == b"\xcf\xfa\xed\xfe":  # Mach-O 64-bit, little-endian
        return "macho", struct.unpack_from("<I", data, 4)[0]
    if data[:4] == b"\x7fELF":
        return "elf", struct.unpack_from("<H", data, 18)[0]
    if data[:2] == b"MZ":
        pe_offset = struct.unpack_from("<I", data, 0x3C)[0]
        if data[pe_offset:pe_offset + 4] == b"PE\0\0":
            return "pe", struct.unpack_from("<H", data, pe_offset + 4)[0]
    return None, None


def verify(path: Path) -> list:
    """Check one bundle, returning a list of problem strings."""
    problems = []

    # avc-<version>-<goos>-<goarch>.mcpb
    parts = path.stem.split("-")
    if len(parts) < 4:
        return [f"{path.name}: filename does not encode a build target"]
    goos, goarch = parts[-2], parts[-1]
    target = f"{goos}-{goarch}"
    if target not in EXPECTED:
        return [f"{path.name}: unknown build target {target!r}"]

    with zipfile.ZipFile(path) as z:
        names = z.namelist()

        if "manifest.json" not in names:
            return [f"{path.name}: no manifest.json at the archive root"]
        manifest = json.loads(z.read("manifest.json"))

        # The manifest must claim the platform the filename promises, or a user
        # downloads a bundle Desktop then refuses on the wrong OS.
        want_platform = PLATFORM[goos]
        got_platforms = manifest.get("compatibility", {}).get("platforms", [])
        if got_platforms != [want_platform]:
            problems.append(
                f"{path.name}: compatibility.platforms is {got_platforms}, want ['{want_platform}']")

        # entry_point must resolve to a file actually in the archive. Desktop
        # appends .exe on Windows, so accept either spelling.
        entry = manifest["server"]["entry_point"]
        member = next((n for n in (entry, entry + ".exe") if n in names), None)
        if member is None:
            return problems + [f"{path.name}: entry_point {entry!r} is not in the archive"]

        info = z.getinfo(member)
        mode = (info.external_attr >> 16) & 0o777
        if not mode & 0o111:
            problems.append(
                f"{path.name}: {member} is stored mode {oct(mode)} with no executable bit; "
                "the extension would install and then fail to launch")
        # A mode is only read when the entry claims a Unix creator. Stored under
        # the default creator, the bits above are present but ignored, and the
        # binary extracts unexecutable on macOS and Linux.
        if info.create_system != UNIX_CREATE_SYSTEM:
            problems.append(
                f"{path.name}: {member} declares create_system={info.create_system}, not "
                f"{UNIX_CREATE_SYSTEM} (Unix); its {oct(mode)} mode would be ignored on extraction")

        manifest_info = z.getinfo("manifest.json")
        if manifest_info.create_system != UNIX_CREATE_SYSTEM:
            problems.append(
                f"{path.name}: manifest.json declares create_system={manifest_info.create_system}, "
                f"not {UNIX_CREATE_SYSTEM} (Unix)")
        manifest_mode = (manifest_info.external_attr >> 16) & 0o777
        if not manifest_mode & 0o044:
            problems.append(
                f"{path.name}: manifest.json is stored mode {oct(manifest_mode)}; the host must "
                "read it to install or uninstall the extension")

        data = z.read(member)
        if len(data) < MIN_BINARY_BYTES:
            problems.append(f"{path.name}: {member} is only {len(data)} bytes; not a real binary")

        want_fmt, want_machine = EXPECTED[target]
        got_fmt, got_machine = identify(data)
        if got_fmt != want_fmt:
            problems.append(
                f"{path.name}: {member} is {got_fmt or 'unrecognised'} format, want {want_fmt}")
        elif got_machine != want_machine:
            problems.append(
                f"{path.name}: {member} targets machine {hex(got_machine)}, "
                f"want {hex(want_machine)} for {target}")

        # Nothing but the manifest and the server directory should ship.
        stray = [n for n in names if n != "manifest.json" and not n.startswith("server/")]
        if stray:
            problems.append(f"{path.name}: unexpected archive entries: {stray}")

    return problems


def main() -> int:
    if len(sys.argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2

    bundle_dir = Path(sys.argv[1])
    bundles = sorted(bundle_dir.glob("*.mcpb"))
    if not bundles:
        print(f"no .mcpb bundles found in {bundle_dir}", file=sys.stderr)
        return 1

    missing = set(EXPECTED) - {"-".join(b.stem.split("-")[-2:]) for b in bundles}
    problems = [f"no bundle built for {t}" for t in sorted(missing)]

    for bundle in bundles:
        found = verify(bundle)
        problems.extend(found)
        if not found:
            print(f"ok  {bundle.name} ({bundle.stat().st_size // 1024} KiB)")

    if problems:
        print("\nFAILED:", file=sys.stderr)
        for p in problems:
            print(f"  {p}", file=sys.stderr)
        return 1

    print(f"\n{len(bundles)} bundle(s) verified")
    return 0


if __name__ == "__main__":
    sys.exit(main())
