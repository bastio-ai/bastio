package security

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// TokenStyle controls the placeholder shape emitted by reversible
// redaction. Some providers choke on angle brackets inside prompts; curly
// form gives an escape hatch.
type TokenStyle string

const (
	TokenStyleAngle TokenStyle = "angle" // <PII_SSN_1>
	TokenStyleCurly TokenStyle = "curly" // {{PII_SSN_1}}
)

// defaultStyle normalizes an unset or unknown style to the canonical default.
func (s TokenStyle) defaultStyle() TokenStyle {
	if s == TokenStyleCurly {
		return TokenStyleCurly
	}
	return TokenStyleAngle
}

// Prefix returns the token prefix used to detect potential placeholder
// starts at chunk boundaries (e.g. "<PII_" or "{{PII_").
func (s TokenStyle) Prefix() string {
	if s.defaultStyle() == TokenStyleCurly {
		return "{{PII_"
	}
	return "<PII_"
}

// TokenMap is a request-scoped mapping of placeholders to their original
// PII values. The map itself is highly sensitive — callers must never
// serialize it, log values, or persist it beyond the request lifetime.
type TokenMap struct {
	mu        sync.RWMutex
	style     TokenStyle
	entries   map[string]string // placeholder -> original
	originals map[string]string // original -> placeholder (dedupe)
	counts    map[string]int    // piiType -> next index
	restored  atomic.Int64      // cumulative restorations — atomic so Restore can increment under RLock
}

// NewTokenMap returns a new TokenMap configured for the given style.
// A zero style defaults to angle brackets.
func NewTokenMap(style TokenStyle) *TokenMap {
	return &TokenMap{
		style:     style.defaultStyle(),
		entries:   make(map[string]string),
		originals: make(map[string]string),
		counts:    make(map[string]int),
	}
}

// Add returns a stable placeholder for a (piiType, original) pair.
// Repeated calls with the same original return the same placeholder so
// the LLM sees consistent references across message turns.
func (tm *TokenMap) Add(piiType, original string) string {
	if tm == nil || original == "" {
		return original
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if existing, ok := tm.originals[original]; ok {
		return existing
	}
	tm.counts[piiType]++
	ph := formatPlaceholder(tm.style, piiType, tm.counts[piiType])
	tm.entries[ph] = original
	tm.originals[original] = ph
	return ph
}

// Restore replaces every known placeholder in content with its original
// value. Returns the rewritten content and the number of replacements
// performed. Unknown placeholders pass through unchanged.
func (tm *TokenMap) Restore(content string) (string, int) {
	if tm == nil {
		return content, 0
	}
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if len(tm.entries) == 0 {
		return content, 0
	}

	count := 0
	for _, ph := range tm.sortedPlaceholdersLocked() {
		if !strings.Contains(content, ph) {
			continue
		}
		n := strings.Count(content, ph)
		content = strings.ReplaceAll(content, ph, tm.entries[ph])
		count += n
	}
	tm.restored.Add(int64(count))
	return content, count
}

// RestoredCount returns the cumulative number of placeholder-to-original
// substitutions performed by this map. Safe to include in metrics — it
// is a count only, not a value.
func (tm *TokenMap) RestoredCount() int {
	if tm == nil {
		return 0
	}
	return int(tm.restored.Load())
}

// Size returns the number of entries in the map.
func (tm *TokenMap) Size() int {
	if tm == nil {
		return 0
	}
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.entries)
}

// Style returns the token style used to build placeholders in this map.
func (tm *TokenMap) Style() TokenStyle {
	if tm == nil {
		return TokenStyleAngle
	}
	return tm.style
}

// Placeholders returns a sorted copy of all placeholders. Safe to share
// (placeholders are opaque identifiers, not PII).
func (tm *TokenMap) Placeholders() []string {
	if tm == nil {
		return nil
	}
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.sortedPlaceholdersLocked()
}

// sortedPlaceholdersLocked returns placeholders sorted by descending
// length so Restore replaces longer tokens first. That prevents a short
// placeholder from eating into the middle of a longer one (e.g. replacing
// <PII_SSN_1> before <PII_SSN_11>).
func (tm *TokenMap) sortedPlaceholdersLocked() []string {
	phs := make([]string, 0, len(tm.entries))
	for ph := range tm.entries {
		phs = append(phs, ph)
	}
	sort.Slice(phs, func(i, j int) bool { return len(phs[i]) > len(phs[j]) })
	return phs
}

func formatPlaceholder(style TokenStyle, piiType string, n int) string {
	upper := strings.ToUpper(piiType)
	switch style.defaultStyle() {
	case TokenStyleCurly:
		return fmt.Sprintf("{{PII_%s_%d}}", upper, n)
	default:
		return fmt.Sprintf("<PII_%s_%d>", upper, n)
	}
}
