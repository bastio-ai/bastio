package governance

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	hkdfInfo        = "bastio-governance-hmac-v1"
	replayWindow    = 5 * time.Minute
	authHeaderName  = "Authorization"
	authSchemePrefix = "Bastio-HMAC "
)

// AuthHeader holds the parsed pieces of `Authorization: Bastio-HMAC ...`.
type AuthHeader struct {
	OrgID       string
	InstallID   string
	Signature   string
	TimestampMS int64
}

var (
	errAuthMissing      = errors.New("missing authorization header")
	errAuthScheme       = errors.New("auth scheme must be Bastio-HMAC")
	errAuthMalformed    = errors.New("malformed authorization header")
	errAuthClockSkew    = errors.New("timestamp outside replay window")
	errAuthSignature    = errors.New("signature mismatch")
	errAuthBodyHashFail = errors.New("body hash failure")
)

// parseAuthHeader extracts org/install/sig/ts from the Authorization value.
// Format: `Bastio-HMAC org=<id>; install=<id>; sig=<hex>; ts=<unix_ms>`.
func parseAuthHeader(value string) (AuthHeader, error) {
	if value == "" {
		return AuthHeader{}, errAuthMissing
	}
	if !strings.HasPrefix(value, authSchemePrefix) {
		return AuthHeader{}, errAuthScheme
	}
	body := strings.TrimPrefix(value, authSchemePrefix)
	parts := strings.Split(body, ";")
	out := AuthHeader{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		eq := strings.IndexByte(p, '=')
		if eq < 0 {
			return AuthHeader{}, errAuthMalformed
		}
		k, v := p[:eq], p[eq+1:]
		switch k {
		case "org":
			out.OrgID = v
		case "install":
			out.InstallID = v
		case "sig":
			out.Signature = v
		case "ts":
			ms, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return AuthHeader{}, errAuthMalformed
			}
			out.TimestampMS = ms
		}
	}
	if out.OrgID == "" || out.InstallID == "" || out.Signature == "" || out.TimestampMS == 0 {
		return AuthHeader{}, errAuthMalformed
	}
	return out, nil
}

// deriveKey produces the per-install HMAC key. Mirrors the extension's HKDF
// at bastio-extension/src/lib/hmac.ts.
func deriveKey(installationSecretB64URL, installID string) ([]byte, error) {
	ikm, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(installationSecretB64URL, "="))
	if err != nil {
		// Allow standard encoding too in case an operator pastes one.
		ikm, err = base64.URLEncoding.DecodeString(installationSecretB64URL)
		if err != nil {
			return nil, fmt.Errorf("decode installation_secret: %w", err)
		}
	}
	salt := []byte(installID)
	r := hkdf.New(sha256.New, ikm, salt, []byte(hkdfInfo))
	out := make([]byte, 32)
	if _, err := r.Read(out); err != nil {
		return nil, fmt.Errorf("hkdf read: %w", err)
	}
	return out, nil
}

// canonicalString is the byte string the extension and server sign over.
// Format: METHOD\npath\nts_unix_ms\ninstall_id\nsha256_hex(body)
func canonicalString(method, path string, ts int64, installID string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	return fmt.Sprintf("%s\n%s\n%d\n%s\n%x", strings.ToUpper(method), path, ts, installID, bodyHash[:])
}

// VerifyHMAC checks a request's signature against the supplied per-install
// secret. Returns nil on success or one of the err* sentinels.
func VerifyHMAC(r *http.Request, body []byte, installationSecret string) (AuthHeader, error) {
	hdr, err := parseAuthHeader(r.Header.Get(authHeaderName))
	if err != nil {
		return AuthHeader{}, err
	}

	now := time.Now()
	delta := time.Duration(now.UnixMilli()-hdr.TimestampMS) * time.Millisecond
	if delta < 0 {
		delta = -delta
	}
	if delta > replayWindow {
		return AuthHeader{}, errAuthClockSkew
	}

	key, err := deriveKey(installationSecret, hdr.InstallID)
	if err != nil {
		return AuthHeader{}, errAuthBodyHashFail
	}

	canonical := canonicalString(r.Method, r.URL.Path, hdr.TimestampMS, hdr.InstallID, body)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(canonical))
	expected := mac.Sum(nil)

	got, err := hex.DecodeString(hdr.Signature)
	if err != nil || subtle.ConstantTimeCompare(expected, got) != 1 {
		return AuthHeader{}, errAuthSignature
	}
	return hdr, nil
}
