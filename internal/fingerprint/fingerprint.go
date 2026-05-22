// Package fingerprint computes opaque hex fingerprints used to invalidate
// cached artifacts when their inputs, prompts, or models change.
//
// A fingerprint is a SHA-256 over the NUL-joined parts. Order matters; pass
// parts in the same order every time you want the same fingerprint.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Compute returns the hex SHA-256 of parts joined with a NUL byte. Empty
// parts are preserved as empty slots so a missing field doesn't collide with
// a shifted ordering.
func Compute(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
