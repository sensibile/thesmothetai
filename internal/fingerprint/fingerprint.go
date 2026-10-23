// Package fingerprint computes content hashes for license evidence.
//
// Hash is the authoritative identity of a license text: any byte change,
// including whitespace, yields a different hash. NormalizedHash is an
// auxiliary comparison that ignores formatting-only differences and must
// never be used on its own to decide that two texts are legally equivalent.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const prefix = "sha256:"

// Hash returns the exact content hash of text.
func Hash(text []byte) string {
	sum := sha256.Sum256(text)
	return prefix + hex.EncodeToString(sum[:])
}

// NormalizedHash returns a hash that ignores line endings and runs of
// whitespace. Letter case and punctuation are preserved.
func NormalizedHash(text []byte) string {
	s := strings.ReplaceAll(string(text), "\r\n", "\n")
	return Hash([]byte(strings.Join(strings.Fields(s), " ")))
}
