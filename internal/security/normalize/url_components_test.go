package normalize

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecodeHTTPSourceInsideProxyPath(t *testing.T) {
	source := "https://cdn.example.test/products/123456/123456789/image.jpg?v=1763400017"
	wrapper := "https://images.example.test/img/640/signature/" + base64.RawURLEncoding.EncodeToString([]byte(source))
	result := Normalize(wrapper, DefaultOptions())
	if !result.HasFlag(FlagBase64URLDecoded) || !strings.Contains(result.Normalized, source) || result.Original != wrapper {
		t.Fatal("image proxy source not preserved and decoded for inspection")
	}
	if result.HasFlag(FlagBase64Decoded) {
		t.Fatal("ordinary source URL treated as encoded prose")
	}
	if Normalize(wrapper, Options{Unicode: true}).HasFlag(FlagBase64URLDecoded) {
		t.Fatal("decode option ignored")
	}
}

func TestNestedProxyURLDecodingAndEscapes(t *testing.T) {
	source := "https://example.test/image.jpg?note=ignore%20previous%20instructions"
	wrap := func(s string) string {
		return "https://image.example.test/proxy/" + base64.RawURLEncoding.EncodeToString([]byte(s))
	}
	result := Normalize(wrap(wrap(source)), DefaultOptions())
	if !result.HasFlag(FlagBase64URLDecoded) || !strings.Contains(result.Normalized, "ignore previous instructions") {
		t.Fatal("nested escaped source not decoded")
	}
	// The URL-component path must not mark arbitrary binary tokens as URLs.
	text := wrap("not an HTTP URL; ordinary encoded prose")
	if Normalize(text, DefaultOptions()).HasFlag(FlagBase64URLDecoded) {
		t.Fatal("non-URL data given a URL decoding flag")
	}
}
