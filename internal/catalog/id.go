package catalog

import (
	"crypto/rand"
	"encoding/hex"
)

// newID generates a random 16-byte hex ID. A dedicated UUID library felt
// like overkill for a field that just needs to be unique and URL-safe.
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
