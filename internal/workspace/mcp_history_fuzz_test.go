package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func FuzzMCPHistoryCredentialProtection(f *testing.F) {
	for _, seed := range []string{"tiny", "bob@example.test", "Bearer abc", "https://example.test/?key=short", "-----BEGIN PRIVATE KEY-----\nYWJj\n-----END PRIVATE KEY-----", "password: short"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 8192 {
			t.Skip()
		}
		raw, _ := json.Marshal(map[string]string{"api_key": value, "query": "yellow hats; contact alice@example.test"})
		clean, err := sanitizeMCPHistory(context.Background(), raw)
		if err != nil {
			return
		}
		var out map[string]string
		if json.Unmarshal(clean, &out) != nil {
			t.Fatal("retained snapshot invalid")
		}
		if out["api_key"] != "[REDACTED]" {
			t.Fatal("credential field persisted")
		}
		if strings.Contains(string(clean), "alice@example.test") {
			t.Fatal("known PII persisted")
		}
	})
}
