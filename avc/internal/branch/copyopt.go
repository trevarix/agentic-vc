package branch

// copyFileOptimized copies src to dst, trying platform-optimised methods first.
// It tries (in order):
//  1. Hardlink — zero extra disk until one side mutates; fails across devices
//  2. Regular byte-copy — always works
//
// Reflink (copy-on-write) is listed in the plan but requires platform-specific
// syscalls (FICLONE on Linux, clonefile on macOS, FSCTL_DUPLICATE_EXTENTS on
// Windows ReFS) and a supported filesystem. The fallback to a byte-copy is
// transparent and correct. Reflink support can be added per-platform as a
// build-tag file without changing this function's signature.
func copyFileOptimized(src, dst string) error {
	if err := tryHardlink(src, dst); err == nil {
		return nil
	}
	return regularCopy(src, dst)
}
