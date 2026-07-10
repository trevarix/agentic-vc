package branch

// copyFileOptimized copies src to dst. Hardlinks are deliberately NOT used:
// workspace files must never share inodes with project-root files, because
// an in-place write in the workspace (append, sed -i, an editor's save) would
// then mutate the original file instead of just the copy — silently breaking
// workspace isolation.
//
// Reflink (true copy-on-write) is safe and can be added per-platform later as
// a build-tag file without changing this function's signature: it requires
// platform-specific syscalls (FICLONE on Linux, clonefile on macOS,
// FSCTL_DUPLICATE_EXTENTS on Windows ReFS) and a supported filesystem, and the
// OS breaks the share on first write — unlike a hardlink, a written-to reflink
// never mutates its source.
func copyFileOptimized(src, dst string) error {
	return regularCopy(src, dst)
}
