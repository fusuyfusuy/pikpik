package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewID generates a time-sortable unique identifier prefixed with the given resource tag.
// e.g. NewID("usr") -> "usr_01953f4a..."
func NewID(prefix string) string {
	var buf [16]byte
	// 48-bit timestamp (milliseconds)
	now := time.Now().UnixMilli()
	buf[0] = byte(now >> 40)
	buf[1] = byte(now >> 32)
	buf[2] = byte(now >> 24)
	buf[3] = byte(now >> 16)
	buf[4] = byte(now >> 8)
	buf[5] = byte(now)

	// 80-bit crypto random entropy
	_, _ = rand.Read(buf[6:])

	encoded := hex.EncodeToString(buf[:])
	if prefix == "" {
		return encoded
	}
	return fmt.Sprintf("%s_%s", prefix, encoded)
}
