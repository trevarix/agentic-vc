// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package db

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func newID(prefix string) string {
	b := make([]byte, 6)
	rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

func nowUnix() int64 {
	return time.Now().Unix()
}
