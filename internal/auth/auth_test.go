package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestHashAPIKey_Deterministic(t *testing.T) {
	a := HashAPIKey("sk-bastio-abc")
	b := HashAPIKey("sk-bastio-abc")
	if a != b {
		t.Fatalf("hash not stable: %q vs %q", a, b)
	}
	if HashAPIKey("sk-bastio-xyz") == a {
		t.Fatal("different inputs must produce different hashes")
	}
	if strings.ContainsAny(a, "+/=") {
		t.Fatalf("hash should be base64 url-safe: %q", a)
	}
}

func TestGenerateAPIKey_HasPrefix(t *testing.T) {
	k := GenerateAPIKey()
	if !strings.HasPrefix(k, "sk-bastio-") {
		t.Fatalf("missing prefix: %q", k)
	}
	if strings.Contains(strings.TrimPrefix(k, "sk-bastio-"), "-") {
		t.Fatalf("key should not contain dashes after prefix: %q", k)
	}
	if k == GenerateAPIKey() {
		t.Fatal("generated keys must be unique")
	}
}

func TestFromContext_Present(t *testing.T) {
	info := &APIKeyInfo{ID: uuid.New(), Name: "test"}
	ctx := context.WithValue(context.Background(), apiKeyContextKey, info)
	got, ok := FromContext(ctx)
	if !ok || got != info {
		t.Fatalf("FromContext round-trip: ok=%v got=%v", ok, got)
	}
}

func TestFromContext_Absent(t *testing.T) {
	_, ok := FromContext(context.Background())
	if ok {
		t.Fatal("expected ok=false for empty ctx")
	}
}

func TestExtractAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"bearer", map[string]string{"Authorization": "Bearer sk-bastio-abc"}, "sk-bastio-abc"},
		{"x-api-key", map[string]string{"X-API-Key": "sk-bastio-xyz"}, "sk-bastio-xyz"},
		{"bearer takes precedence", map[string]string{
			"Authorization": "Bearer sk-bastio-abc",
			"X-API-Key":     "sk-bastio-xyz",
		}, "sk-bastio-abc"},
		{"wrong scheme", map[string]string{"Authorization": "Basic abc"}, ""},
		{"missing", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			if got := extractAPIKey(r); got != tt.want {
				t.Fatalf("extractAPIKey: want %q got %q", tt.want, got)
			}
		})
	}
}

func TestSafePrefix(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"sk-bastio-abcdefgh", "sk-bastio-ab..."},
		{"short", "***"},
		{"", "***"},
	}
	for _, tt := range tests {
		if got := safePrefix(tt.in); got != tt.want {
			t.Fatalf("safePrefix(%q): want %q got %q", tt.in, tt.want, got)
		}
	}
}
