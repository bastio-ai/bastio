package governance

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
)

// cryptoRand is a thin wrapper so test files can monkey-patch.
func cryptoRand(b []byte) (int, error) {
	return rand.Read(b)
}

// base64URLEncode produces an unpadded base64url string suitable for the
// installation_token / installation_secret representations the extension
// expects.
func base64URLEncode(b []byte) string {
	s := base64.URLEncoding.EncodeToString(b)
	return strings.TrimRight(s, "=")
}
