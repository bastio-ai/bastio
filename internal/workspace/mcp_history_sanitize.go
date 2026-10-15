package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
)

const mcpHistoryLimit = 64 << 10

var mcpHistorySanitizer = security.NewEngine(detection.NewPIIDetector(), detection.NewSecretsDetector())
var historySensitiveKey = regexp.MustCompile(`(?i)^(?:authorization|proxy[-_]?authorization|cookies?|set[-_]?cookie|password|passwd|pwd|(?:api|access|refresh|bearer|auth|id|session)[_-]?(?:key|token)|token|secret|client[_-]?secret|credentials?|private[_-]?key|connection[_-]?string)$`)
var historyCredentialLine = regexp.MustCompile(`(?im)\b(?:authorization|proxy-authorization|cookie|set-cookie|password|passwd|pwd|api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|secret)["']?\s*[:=]\s*[^\r\n]+`)
var historyBearer = regexp.MustCompile(`(?i)\b(?:Bearer|Basic)\s+[A-Za-z0-9/+=_.-]+`)
var historyOpaque = regexp.MustCompile(`^[A-Za-z0-9+/=_-]{128,}$`)
var historyURL = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)
var errHistorySanitize = errors.New("history content could not be sanitized")

// Logging protection is independent of traffic detector enablement/thresholds.
// PII placeholders are irreversible here: token maps are never persisted.
func sanitizeMCPHistory(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) > mcpHistoryLimit || !json.Valid(raw) {
		return nil, errHistorySanitize
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return nil, errHistorySanitize
	}
	protected, err := sanitizeHistoryValue(ctx, value, 0)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(protected)
	if err != nil || len(out) > mcpHistoryLimit {
		return nil, errHistorySanitize
	}
	return out, nil
}
func sanitizeHistoryText(ctx context.Context, text string) (string, error) {
	if ctx.Err() != nil {
		return "", errHistorySanitize
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "data:") || historyOpaque.MatchString(text) {
		return "[OMITTED_BINARY_OR_OPAQUE_VALUE]", nil
	}
	text = historyCredentialLine.ReplaceAllString(text, "[REDACTED_CREDENTIAL]")
	text = historyBearer.ReplaceAllString(text, "[REDACTED_CREDENTIAL]")
	// URL query/fragment and userinfo are omitted even when their credential
	// values have low entropy. Paths still pass through PII/secrets inspection.
	text = historyURL.ReplaceAllStringFunc(text, func(s string) string {
		u, err := url.Parse(s)
		if err != nil {
			return "[REDACTED_URL]"
		}
		u.User = nil
		u.RawQuery = ""
		u.ForceQuery = false
		u.Fragment = ""
		return u.String()
	})
	scan := mcpHistorySanitizer.Scan(ctx, &security.ScanRequest{Content: text, Canonicalize: true, Steps: []security.Step{{Detector: "pii", Strategy: security.ActionTokenize}, {Detector: "secrets", Strategy: security.ActionMask}}})
	if scan == nil || scan.RewriteUnavailable || scan.ShouldBlock {
		return "", errHistorySanitize
	}
	// Verify the final snapshot independently. Canonicalization can expose a
	// recognized value that a surface-form rewrite did not replace; never
	// retain that value just because the first scan attempted a transformation.
	verified := mcpHistorySanitizer.Scan(ctx, &security.ScanRequest{Content: scan.SanitizedContent, Canonicalize: true, Steps: []security.Step{{Detector: "pii", Strategy: security.ActionBlock}, {Detector: "secrets", Strategy: security.ActionBlock}}})
	if ctx.Err() != nil || verified == nil || verified.ShouldBlock || verified.RewriteUnavailable {
		return "", errHistorySanitize
	}
	// Never retain either the original token map or normalized side-channel text.
	return scan.SanitizedContent, nil
}
func sanitizeHistoryValue(ctx context.Context, value any, depth int) (any, error) {
	if depth > 24 {
		return nil, errHistorySanitize
	}
	switch v := value.(type) {
	case string:
		return sanitizeHistoryText(ctx, v)
	case json.Number:
		sanitized, err := sanitizeHistoryText(ctx, string(v))
		if err != nil {
			return nil, err
		}
		if sanitized != string(v) {
			return sanitized, nil
		}
		return v, nil
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			cleanKey, err := sanitizeHistoryText(ctx, key)
			if err != nil {
				return nil, err
			}
			if _, duplicate := out[cleanKey]; duplicate {
				return nil, errHistorySanitize
			}
			// No binary/resource blobs or authentication metadata in retained history.
			compact := strings.Map(func(r rune) rune {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					return unicode.ToLower(r)
				}
				return -1
			}, security.Canonicalize(key).Canonical)
			sensitive := historySensitiveKey.MatchString(key) || historySensitiveKey.MatchString(compact) || historySensitiveKey.MatchString(strings.TrimPrefix(compact, "x")) || compact == "key"
			for _, part := range []string{"authorization", "cookie", "password", "passwd", "credential", "secret", "privatekey", "apikey", "accesskey", "signingkey", "encryptionkey", "connectionstring"} {
				sensitive = sensitive || strings.Contains(compact, part)
			}
			if sensitive {
				out[cleanKey] = "[REDACTED]"
				continue
			}
			out[cleanKey], err = sanitizeHistoryValue(ctx, item, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			clean, err := sanitizeHistoryValue(ctx, item, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = clean
		}
		return out, nil
	case nil, bool:
		return v, nil
	default:
		return nil, errHistorySanitize
	}
}
