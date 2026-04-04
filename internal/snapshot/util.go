package snapshot

import (
	"crypto/rand"
	"encoding/hex"
)

func newSnapID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "snap-" + hex.EncodeToString(b)
}

func newFileID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "file-" + hex.EncodeToString(b)
}
