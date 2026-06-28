// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

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
