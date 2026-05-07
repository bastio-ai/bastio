package workspace

import (
	"crypto/rand"
	"encoding/base64"
)

// generateBearerToken returns a URL-safe random base64 string of length
// proportional to n bytes of entropy. Used by branded.go for the
// branded-chat anonymous-session bearer token. Previously lived in
// members.go alongside the invitation flow; the invitation flow moved
// to bastio-cloud during the OSS↔Cloud split, this helper stayed.
func generateBearerToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
