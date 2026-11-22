package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewID returns a random opaque identifier for tasks and audit records.
// It is intentionally dependency-free; callers only need uniqueness.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Identifiers are not secrets. Keep the process usable if the
		// platform entropy source is temporarily unavailable.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}

	return hex.EncodeToString(b[:])
}
