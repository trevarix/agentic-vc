package tests

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/statcache"
)

// TestStatCache_Miss_OnEmptyCache verifies that an empty cache returns a miss
// for any file.
func TestStatCache_Miss_OnEmptyCache(t *testing.T) {
	c := statcache.Empty()
	dir := t.TempDir()
	writeFile(t, dir, "file.go", "content")

	info, err := os.Stat(filepath.Join(dir, "file.go"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	_, hit := c.Lookup("file.go", info)
	if hit {
		t.Error("expected cache miss on empty cache, got hit")
	}
}

// TestStatCache_Hit_OnMatchingMtimeAndSize verifies that a cache entry is
// returned when mtime and size match.
func TestStatCache_Hit_OnMatchingMtimeAndSize(t *testing.T) {
	c := statcache.Empty()
	dir := t.TempDir()
	writeFile(t, dir, "file.go", "hello world")

	info, err := os.Stat(filepath.Join(dir, "file.go"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	const fakeHash = "abc123"
	c.Set("file.go", info, fakeHash)

	hash, hit := c.Lookup("file.go", info)
	if !hit {
		t.Error("expected cache hit after Set, got miss")
	}
	if hash != fakeHash {
		t.Errorf("hash = %q, want %q", hash, fakeHash)
	}
}

// TestStatCache_Miss_OnSizeMismatch verifies that a size change causes a miss.
func TestStatCache_Miss_OnSizeMismatch(t *testing.T) {
	c := statcache.Empty()
	dir := t.TempDir()

	writeFile(t, dir, "file.go", "short")
	info, _ := os.Stat(filepath.Join(dir, "file.go"))
	c.Set("file.go", info, "hash1")

	// Overwrite with longer content.
	writeFile(t, dir, "file.go", "much longer content here")
	newInfo, _ := os.Stat(filepath.Join(dir, "file.go"))

	_, hit := c.Lookup("file.go", newInfo)
	if hit {
		t.Error("expected cache miss after size change, got hit")
	}
}

// TestStatCache_Miss_OnMtimeMismatch verifies that an mtime change causes a miss.
func TestStatCache_Miss_OnMtimeMismatch(t *testing.T) {
	c := statcache.Empty()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.go")

	writeFile(t, dir, "file.go", "same content")
	info, _ := os.Stat(path)
	c.Set("file.go", info, "hash1")

	// Set mtime to a different time (1 second in the past).
	oldTime := info.ModTime().Add(-time.Second)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	newInfo, _ := os.Stat(path)

	_, hit := c.Lookup("file.go", newInfo)
	if hit {
		t.Error("expected cache miss after mtime change, got hit")
	}
}

// TestStatCache_SaveAndLoad_RoundTrip verifies that a cache written to disk is
// recovered intact by LoadFromPath.
func TestStatCache_SaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".avc"), 0755); err != nil {
		t.Fatalf("mkdir .avc: %v", err)
	}

	writeFile(t, dir, "app.go", "package main")
	info, _ := os.Stat(filepath.Join(dir, "app.go"))

	c := statcache.Empty()
	c.SnapshotID = "snap-001"
	c.Set("app.go", info, "deadbeef")

	if err := c.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := statcache.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.SnapshotID != "snap-001" {
		t.Errorf("SnapshotID = %q, want %q", loaded.SnapshotID, "snap-001")
	}

	hash, hit := loaded.Lookup("app.go", info)
	if !hit {
		t.Error("expected cache hit after Load, got miss")
	}
	if hash != "deadbeef" {
		t.Errorf("hash = %q, want %q", hash, "deadbeef")
	}
}

// TestStatCache_CorruptFile_ReturnsEmpty verifies that a corrupt cache file is
// silently ignored and an empty cache is returned.
func TestStatCache_CorruptFile_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	avcDir := filepath.Join(dir, ".avc")
	if err := os.MkdirAll(avcDir, 0755); err != nil {
		t.Fatalf("mkdir .avc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(avcDir, "stat-cache.json"), []byte("not-json!!!"), 0644); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}

	c, err := statcache.Load(dir)
	if err != nil {
		t.Fatalf("Load should not error on corrupt cache: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil cache even for corrupt file")
	}
}

// TestStatCache_Invalidate_RemovesCacheFile verifies that Invalidate deletes
// the on-disk cache file.
func TestStatCache_Invalidate_RemovesCacheFile(t *testing.T) {
	dir := t.TempDir()
	avcDir := filepath.Join(dir, ".avc")
	if err := os.MkdirAll(avcDir, 0755); err != nil {
		t.Fatalf("mkdir .avc: %v", err)
	}

	c := statcache.Empty()
	if err := c.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cachePath := filepath.Join(avcDir, "stat-cache.json")
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatal("cache file should exist before Invalidate")
	}

	statcache.Invalidate(dir)

	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Error("cache file should be removed after Invalidate")
	}
}

// TestStatCache_Invalidate_Noop_WhenFileAbsent verifies that Invalidate does
// not error when no cache file exists.
func TestStatCache_Invalidate_Noop_WhenFileAbsent(t *testing.T) {
	dir := t.TempDir()
	// Should not panic or return error.
	statcache.Invalidate(dir)
}

// TestStatCache_Set_OverwritesExistingEntry verifies that Set replaces a
// previously stored entry for the same path.
func TestStatCache_Set_OverwritesExistingEntry(t *testing.T) {
	c := statcache.Empty()
	dir := t.TempDir()
	writeFile(t, dir, "file.go", "v1")
	info1, _ := os.Stat(filepath.Join(dir, "file.go"))
	c.Set("file.go", info1, "hash-v1")

	// Overwrite with new info (simulate file change).
	writeFile(t, dir, "file.go", "v2 longer")
	info2, _ := os.Stat(filepath.Join(dir, "file.go"))
	c.Set("file.go", info2, "hash-v2")

	hash, hit := c.Lookup("file.go", info2)
	if !hit {
		t.Error("expected hit after overwrite Set")
	}
	if hash != "hash-v2" {
		t.Errorf("hash = %q, want %q", hash, "hash-v2")
	}
}

// TestStatCache_AbsentFile_ReturnsEmpty verifies that loading from a
// non-existent path returns a usable empty cache.
func TestStatCache_AbsentFile_ReturnsEmpty(t *testing.T) {
	c, err := statcache.LoadFromPath("/tmp/__nonexistent_avc_cache__.json")
	if err != nil {
		t.Fatalf("LoadFromPath should not error on missing file: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil cache for missing file")
	}
}
