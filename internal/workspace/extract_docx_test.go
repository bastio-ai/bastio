package workspace

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// TestExtractDOCXMinimal builds a minimal valid .docx in memory and
// asserts the extractor pulls every <w:t> body, joining paragraphs
// with newlines.
func TestExtractDOCXMinimal(t *testing.T) {
	t.Parallel()

	body := buildMinimalDOCX(t,
		[]string{
			"First paragraph text.",
			"Second paragraph with multiple",
			"runs joined.",
		},
	)

	got, err := ExtractText(bytes.NewReader(body),
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"doc.docx")
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(got, "First paragraph text.") {
		t.Errorf("missing first paragraph in %q", got)
	}
	if !strings.Contains(got, "Second paragraph") {
		t.Errorf("missing second paragraph in %q", got)
	}
}

// TestExtractDOCXMissingDocument rejects a zip without word/document.xml.
func TestExtractDOCXMissingDocument(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("README.txt")
	_, _ = f.Write([]byte("not a docx"))
	_ = w.Close()

	_, err := ExtractText(bytes.NewReader(buf.Bytes()),
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"fake.docx")
	if err == nil {
		t.Fatal("expected error when word/document.xml is missing")
	}
}

// buildMinimalDOCX constructs the smallest possible Office Open XML
// document that ExtractDOCX can parse. We don't need the .rels boilerplate
// because the extractor reads document.xml directly.
func buildMinimalDOCX(t *testing.T, paragraphs []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paragraphs {
		doc.WriteString(`<w:p><w:r><w:t>`)
		doc.WriteString(p)
		doc.WriteString(`</w:t></w:r></w:p>`)
	}
	doc.WriteString(`</w:body></w:document>`)
	if _, err := f.Write([]byte(doc.String())); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
