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
