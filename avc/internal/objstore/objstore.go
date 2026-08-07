// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package objstore is the single owner of AVC's content-addressed object
// store at .avc/objects/<2-hex-shard>/<62-hex-rest>. Object read/write used
// to live in package restore (with a duplicate reader in diff); extracting
// it here contains the blast radius of format changes.
//
// # On-disk format
//
// Objects are written in one of two forms:
//
//   - v2 compressed: a 13-byte header — magic "AVCO", format byte 0x01,
//     8-byte little-endian raw (uncompressed) size — followed by one zstd
//     frame. Written only when compression actually saves space.
//   - raw: the exact original bytes, headerless. Every object written before
//     compression existed is this form, and so is content that doesn't
//     compress (already-compressed media, archives, tiny files).
//
// A reader distinguishes the two by the magic prefix. The pathological case
// — a user file whose own content begins with the magic — is handled by
// falling back to raw bytes whenever the header or zstd frame fails to
// parse, so no legacy object is ever misread.
package objstore

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
)

// magic prefixes every v2 (compressed) object. Chosen to be nothing any real
// file format starts with.
var magic = []byte("AVCO")

const (
	formatZstd     = 0x01
	headerLen      = 4 + 1 + 8 // magic + format byte + raw size
	minCompressLen = 128       // don't bother compressing tiny objects
)

var (
	// EncodeAll/DecodeAll on shared instances are safe for concurrent use.
	encoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	decoder, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
)

// Path returns the on-disk path of an object. Returns "" for hashes too
// short to shard (defense against the empty-hash panics of review 1.2).
func Path(projectRoot, hash string) string {
	if len(hash) < 3 {
		return ""
	}
	return filepath.Join(projectRoot, ".avc", "objects", hash[:2], hash[2:])
}

// Exists reports whether an object is already stored.
func Exists(projectRoot, hash string) bool {
	if len(hash) < 3 {
		return false
	}
	_, err := os.Stat(Path(projectRoot, hash))
	return err == nil
}

// Store writes data under its hash, compressed when that saves space.
// Content-addressed deduplication: if the object already exists, Store is a
// no-op. Writes are atomic (unique temp file + rename) so a crash can never
// leave a truncated blob on the final path, and concurrent Stores of
// identical content tolerate each other's rename winning.
func Store(projectRoot, hash string, data []byte) error {
	if len(hash) < 3 {
		return fmt.Errorf("invalid object hash %q", hash)
	}
	path := Path(projectRoot, hash)
	if _, err := os.Stat(path); err == nil {
		return nil // already stored
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	payload := encode(data)

	// The suffix must be unique per call, not just per process: concurrent
	// goroutines share a PID, and Windows cannot open a file mid-rename.
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	tmp := fmt.Sprintf("%s.%d.%s.tmp", path, os.Getpid(), hex.EncodeToString(suffix))
	if err := os.WriteFile(tmp, payload, 0644); err != nil {
		os.Remove(tmp)
		return err
	}

	// Windows can surface "access denied" when two renames race onto the
	// same content-addressed destination, even though either outcome is
	// byte-identical. Retry briefly; accept a concurrent winner.
	var renameErr error
	for attempt := 0; attempt < 5; attempt++ {
		if renameErr = os.Rename(tmp, path); renameErr == nil {
			return nil
		}
		if _, statErr := os.Stat(path); statErr == nil {
			os.Remove(tmp)
			return nil // a concurrent writer already stored identical content
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	os.Remove(tmp)
	return renameErr
}

// Read returns an object's original (decompressed) bytes.
func Read(projectRoot, hash string) ([]byte, error) {
	if len(hash) < 3 {
		return nil, fmt.Errorf("invalid object hash %q", hash)
	}
	raw, err := os.ReadFile(Path(projectRoot, hash))
	if err != nil {
		return nil, fmt.Errorf("object %q not found: %w", hash, err)
	}
	return decode(raw), nil
}

// ReadSafe is Read returning nil on any error — for callers that treat a
// missing or unreadable object as "no content" (diff previews).
func ReadSafe(projectRoot, hash string) []byte {
	data, err := Read(projectRoot, hash)
	if err != nil {
		return nil
	}
	return data
}

// encode returns the on-disk form of data: v2-compressed when that saves
// space, raw bytes otherwise.
func encode(data []byte) []byte {
	if len(data) < minCompressLen {
		return data
	}
	compressed := encoder.EncodeAll(data, make([]byte, 0, len(data)/2))
	if headerLen+len(compressed) >= len(data) {
		return data // compression doesn't pay — store raw
	}
	out := make([]byte, headerLen, headerLen+len(compressed))
	copy(out, magic)
	out[4] = formatZstd
	binary.LittleEndian.PutUint64(out[5:13], uint64(len(data)))
	return append(out, compressed...)
}

// decode returns an object's original bytes from its on-disk form. Anything
// that doesn't parse as a well-formed v2 object — wrong magic, unknown
// format byte, corrupt zstd frame, raw-size mismatch — is returned as raw
// bytes, which is exactly right for legacy objects and for the pathological
// legacy file whose content merely starts with the magic.
func decode(raw []byte) []byte {
	rawSize, frame, ok := parseHeader(raw)
	if !ok {
		return raw
	}
	// The declared size may be garbage when a legacy raw file merely starts
	// with the magic bytes — clamp the pre-allocation so a bogus header can
	// never panic make(); DecodeAll grows the buffer as needed, and the
	// size check below still rejects any mismatch.
	allocHint := rawSize
	if allocHint > 64<<20 {
		allocHint = 64 << 20
	}
	out, err := decoder.DecodeAll(frame, make([]byte, 0, allocHint))
	if err != nil || uint64(len(out)) != rawSize {
		return raw
	}
	return out
}

// parseHeader validates a v2 header, returning the declared raw size and the
// zstd frame that follows it.
func parseHeader(raw []byte) (rawSize uint64, frame []byte, ok bool) {
	if len(raw) < headerLen || string(raw[:4]) != string(magic) || raw[4] != formatZstd {
		return 0, nil, false
	}
	return binary.LittleEndian.Uint64(raw[5:13]), raw[headerLen:], true
}

// Info describes one stored object's on-disk footprint.
type Info struct {
	Compressed bool
	DiskSize   int64  // bytes on disk (compressed form when applicable)
	RawSize    uint64 // original content size (== DiskSize for raw objects)
}

// Stat reports an object's storage form without decompressing it — the v2
// header carries the raw size precisely so storage accounting stays cheap.
func Stat(path string, diskSize int64) Info {
	f, err := os.Open(path)
	if err != nil {
		return Info{DiskSize: diskSize, RawSize: uint64(diskSize)}
	}
	defer f.Close()
	header := make([]byte, headerLen)
	if n, _ := f.Read(header); n == headerLen {
		if rawSize, _, ok := parseHeader(header); ok {
			return Info{Compressed: true, DiskSize: diskSize, RawSize: rawSize}
		}
	}
	return Info{DiskSize: diskSize, RawSize: uint64(diskSize)}
}
