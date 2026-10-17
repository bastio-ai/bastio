package security

import (
	"encoding/json"
	"fmt"

	"github.com/bastio-ai/bastio/pkg/encryption"
)

// Seal is the only persistence boundary for asynchronous batch token maps.
// It never exposes plaintext entries to a caller. The owning batch store must
// bind context to tenant, credential scope, manifest and item, and enforce TTL.
func (tm *TokenMap) Seal(cipher *encryption.Service, customer, context string) (*encryption.Encrypted, error) {
	if tm == nil || cipher == nil {
		return nil, fmt.Errorf("token vault unavailable")
	}
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	payload, err := json.Marshal(struct {
		Style     TokenStyle
		Namespace string
		Entries   map[string]string
		Counts    map[string]int
	}{tm.style, tm.namespace, tm.entries, tm.counts})
	if err != nil {
		return nil, fmt.Errorf("encode token vault: %w", err)
	}
	return cipher.Encrypt(string(payload), customer, context)
}

// OpenTokenMap restores only a vault authenticated for the supplied context.
func OpenTokenMap(cipher *encryption.Service, encrypted *encryption.Encrypted, customer, context string) (*TokenMap, error) {
	if cipher == nil || encrypted == nil {
		return nil, fmt.Errorf("token vault unavailable")
	}
	plain, err := cipher.Decrypt(encrypted, customer, context)
	if err != nil {
		return nil, fmt.Errorf("open token vault: %w", err)
	}
	var payload struct {
		Style     TokenStyle
		Namespace string
		Entries   map[string]string
		Counts    map[string]int
	}
	if json.Unmarshal([]byte(plain), &payload) != nil {
		return nil, fmt.Errorf("invalid token vault")
	}
	tm := NewTokenMap(payload.Style)
	tm.namespace = payload.Namespace
	if payload.Counts != nil {
		tm.counts = payload.Counts
	}
	for placeholder, original := range payload.Entries {
		tm.entries[placeholder] = original
		tm.originals[original] = placeholder
	}
	return tm, nil
}
