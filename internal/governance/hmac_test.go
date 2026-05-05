package governance

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/hkdf"
)

// TestHMACRoundtrip simulates what the extension does in
// bastio-extension/src/lib/hmac.ts: derive a per-install key from the org
// installation_secret, sign the canonical string, and POST. The server-side
// VerifyHMAC must produce a byte-identical signature against the same body.
func TestHMACRoundtrip(t *testing.T) {
	// Random 32-byte secret matching the extension's expected size.
	rawSecret := make([]byte, 32)
	if _, err := rand.Read(rawSecret); err != nil {
		t.Fatalf("rand: %v", err)
	}
	secretB64URL := base64.RawURLEncoding.EncodeToString(rawSecret)
	installID := "01J0000000000000000000000A"

	// === extension side: derive per-install HMAC key + sign body ===
	// Use the same HKDF parameters the extension does.
	r := hkdf.New(sha256.New, rawSecret, []byte(installID), []byte(hkdfInfo))
	clientKey := make([]byte, 32)
	if _, err := r.Read(clientKey); err != nil {
		t.Fatalf("hkdf: %v", err)
	}

	body := []byte(`{"event_id":"01J","occurred_at":"2026-04-25T22:00:00Z"}`)
	ts := time.Now().UnixMilli()
	method := "POST"
	path := "/v1/governance/events"

	canonical := canonicalString(method, path, ts, installID, body)
	mac := hmac.New(sha256.New, clientKey)
	mac.Write([]byte(canonical))
	sigHex := hex.EncodeToString(mac.Sum(nil))

	// === build the request the way the extension would ===
	req, err := http.NewRequest(method, "https://example.com"+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(
		"Authorization",
		strings.Join([]string{
			authSchemePrefix + "org=01J0000000000000000000000B",
			"install=" + installID,
			"sig=" + sigHex,
			"ts=" + formatInt(ts),
		}, "; "),
	)
	// Note: the leading "Bastio-HMAC " is part of the first segment per the parser's convention.

	// === server side: verify ===
	parsed, err := VerifyHMAC(req, body, secretB64URL)
	if err != nil {
		t.Fatalf("VerifyHMAC failed: %v\ncanonical=%q\nheader=%q", err, canonical, req.Header.Get("Authorization"))
	}
	if parsed.InstallID != installID {
		t.Fatalf("install_id mismatch: got %q want %q", parsed.InstallID, installID)
	}
}

func TestVerifyHMACClockSkewRejection(t *testing.T) {
	rawSecret := make([]byte, 32)
	_, _ = rand.Read(rawSecret)
	secretB64URL := base64.RawURLEncoding.EncodeToString(rawSecret)
	installID := "01J0000000000000000000000A"

	r := hkdf.New(sha256.New, rawSecret, []byte(installID), []byte(hkdfInfo))
	clientKey := make([]byte, 32)
	_, _ = r.Read(clientKey)

	body := []byte(`{}`)
	// 10 minutes in the past — outside the ±5min window.
	ts := time.Now().Add(-10 * time.Minute).UnixMilli()
	canonical := canonicalString("POST", "/v1/governance/events", ts, installID, body)
	mac := hmac.New(sha256.New, clientKey)
	mac.Write([]byte(canonical))
	sigHex := hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest("POST", "https://example.com/v1/governance/events", bytes.NewReader(body))
	req.Header.Set(
		"Authorization",
		strings.Join([]string{
			authSchemePrefix + "org=01J0000000000000000000000B",
			"install=" + installID,
			"sig=" + sigHex,
			"ts=" + formatInt(ts),
		}, "; "),
	)

	if _, err := VerifyHMAC(req, body, secretB64URL); err == nil {
		t.Fatal("expected clock-skew rejection, got nil")
	}
}

func TestVerifyHMACSignatureMismatchRejection(t *testing.T) {
	rawSecret := make([]byte, 32)
	_, _ = rand.Read(rawSecret)
	secretB64URL := base64.RawURLEncoding.EncodeToString(rawSecret)
	installID := "01J0000000000000000000000A"

	body := []byte(`{}`)
	ts := time.Now().UnixMilli()

	// Sign with a different key entirely.
	bogusKey := make([]byte, 32)
	_, _ = rand.Read(bogusKey)
	canonical := canonicalString("POST", "/v1/governance/events", ts, installID, body)
	mac := hmac.New(sha256.New, bogusKey)
	mac.Write([]byte(canonical))
	sigHex := hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest("POST", "https://example.com/v1/governance/events", bytes.NewReader(body))
	req.Header.Set(
		"Authorization",
		strings.Join([]string{
			authSchemePrefix + "org=01J",
			"install=" + installID,
			"sig=" + sigHex,
			"ts=" + formatInt(ts),
		}, "; "),
	)

	if _, err := VerifyHMAC(req, body, secretB64URL); err == nil {
		t.Fatal("expected signature mismatch rejection, got nil")
	}
}

func formatInt(n int64) string { return strconv.FormatInt(n, 10) }
