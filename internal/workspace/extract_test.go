package workspace

import (
	"strings"
	"testing"
)

func TestExtractTextPlain(t *testing.T) {
	t.Parallel()
	got, err := ExtractText(strings.NewReader("Hello, world.\n"), "text/plain", "notes.txt")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "Hello, world.\n" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractTextStripsBOM(t *testing.T) {
	t.Parallel()
	bom := []byte{0xEF, 0xBB, 0xBF}
	got, err := ExtractText(strings.NewReader(string(bom)+"BOM-prefixed."), "text/plain", "x.txt")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "BOM-prefixed." {
		t.Fatalf("BOM not stripped: %q", got)
	}
}

func TestExtractTextRejectsBinary(t *testing.T) {
	t.Parallel()
	binary := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	_, err := ExtractText(strings.NewReader(string(binary)), "application/octet-stream", "blob.bin")
	if err == nil {
		t.Fatal("expected rejection for binary content")
	}
}

func TestExtractTextSniffsExtension(t *testing.T) {
	t.Parallel()
	// No mime; only extension is .md → markdown should be accepted.
	got, err := ExtractText(strings.NewReader("# Title\n\nBody."), "", "doc.md")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got, "# Title") {
		t.Fatalf("got %q", got)
	}
}

func TestExtractTextJSONIsPrettified(t *testing.T) {
	t.Parallel()
	got, err := ExtractText(strings.NewReader(`{"a":1,"b":[2,3]}`), "application/json", "x.json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got, "\n  ") {
		t.Fatalf("expected indented JSON, got %q", got)
	}
}

func TestExtractTextAcceptsLogYAML(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		body     string
		mime     string
		filename string
	}{
		{"yaml", "key: value\nlist:\n  - one\n", "", "config.yaml"},
		{"log",  "[INFO] starting\n[ERROR] panic\n", "", "app.log"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ExtractText(strings.NewReader(c.body), c.mime, c.filename)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got != c.body {
				t.Fatalf("got %q want %q", got, c.body)
			}
		})
	}
}
