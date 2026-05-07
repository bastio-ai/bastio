package workspace

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// ExtractText reads bytes from r and returns plain text suitable for
// chunking. mimeType is a hint — when blank or unknown the function
// falls back to filename-extension sniffing, then to bytes-as-utf8.
//
// MVP support: text/plain, text/markdown, text/csv, application/json.
// PDF + Office docs deferred to Phase 3.5 (requires native deps).
func ExtractText(r io.Reader, mimeType, filename string) (string, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read blob: %w", err)
	}

	switch normalizeMime(mimeType, filename) {
	case "text/plain", "text/markdown", "text/csv", "":
		return decodeUTF8(body)
	case "application/json":
		return prettyJSON(body)
	case "application/pdf":
		return extractPDF(body)
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return extractDOCX(body)
	default:
		// Last-resort: if the bytes are valid UTF-8 text we still
		// accept them. Customers occasionally upload .log or .yaml
		// files that don't get a proper mime header.
		if isLikelyText(body) {
			return decodeUTF8(body)
		}
		return "", fmt.Errorf("unsupported file type: %s", mimeType)
	}
}

// extractPDF pulls plain text from a PDF using ledongthuc/pdf — pure
// Go, no native deps. Loses layout (multi-column docs lose column
// boundaries) but RAG-quality extraction doesn't need pixel-perfect
// reconstruction. Returns an error for encrypted PDFs.
func extractPDF(body []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("read pdf: %w", err)
	}
	var out strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			// Skip pages that fail to extract — encrypted-page error
			// in the middle of an otherwise-readable doc shouldn't
			// nuke the whole upload.
			continue
		}
		if text != "" {
			if out.Len() > 0 {
				out.WriteString("\n\n")
			}
			out.WriteString(text)
		}
	}
	if out.Len() == 0 {
		return "", fmt.Errorf("pdf has no extractable text (may be scanned image or encrypted)")
	}
	return out.String(), nil
}

// extractDOCX unzips a .docx and pulls every `<w:t>` element from
// `word/document.xml`. Stdlib only — no third-party DOCX library.
// Multi-paragraph runs land separated by newlines for chunking.
func extractDOCX(body []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("read docx zip: %w", err)
	}
	var docXML *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			docXML = f
			break
		}
	}
	if docXML == nil {
		return "", fmt.Errorf("docx missing word/document.xml")
	}
	rc, err := docXML.Open()
	if err != nil {
		return "", fmt.Errorf("open docx body: %w", err)
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	var out strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("decode docx xml: %w", err)
		}
		switch v := tok.(type) {
		case xml.StartElement:
			// w:t holds visible text; w:br is a paragraph/line break.
			if v.Name.Local == "t" {
				inText = true
			} else if v.Name.Local == "br" || v.Name.Local == "p" {
				out.WriteByte('\n')
			}
		case xml.EndElement:
			if v.Name.Local == "t" {
				inText = false
			} else if v.Name.Local == "p" {
				out.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				out.Write(v)
			}
		}
	}
	if out.Len() == 0 {
		return "", fmt.Errorf("docx has no extractable text")
	}
	return out.String(), nil
}

// normalizeMime resolves the best mime guess from the explicit header
// and the filename extension.
func normalizeMime(mimeType, filename string) string {
	if mimeType != "" {
		// Strip charset suffix etc.
		if i := strings.Index(mimeType, ";"); i > 0 {
			mimeType = strings.TrimSpace(mimeType[:i])
		}
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		ext := strings.ToLower(filepath.Ext(filename))
		switch ext {
		case ".txt", ".log":
			return "text/plain"
		case ".md", ".markdown":
			return "text/markdown"
		case ".csv":
			return "text/csv"
		case ".json":
			return "application/json"
		case ".pdf":
			return "application/pdf"
		case ".docx":
			return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		}
		if guessed := mime.TypeByExtension(ext); guessed != "" {
			if i := strings.Index(guessed, ";"); i > 0 {
				return strings.TrimSpace(guessed[:i])
			}
			return guessed
		}
	}
	return mimeType
}

func decodeUTF8(body []byte) (string, error) {
	if !utf8.Valid(body) {
		return "", fmt.Errorf("file is not valid UTF-8 text")
	}
	// Strip BOM if present — common in Windows-saved .txt/.csv.
	body = bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})
	return string(body), nil
}

func prettyJSON(body []byte) (string, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		// Don't reject — store the raw text so customers can chunk
		// over malformed JSON if they want.
		return decodeUTF8(body)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return decodeUTF8(body)
	}
	return string(out), nil
}

// isLikelyText returns true when ≤1% of the leading bytes are control
// characters that aren't whitespace. Enough to keep .yaml / .log files
// in scope without smuggling binaries past the type gate.
func isLikelyText(body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	limit := min(len(body), 4096)
	bad := 0
	for i := range limit {
		b := body[i]
		if b == 0 {
			return false
		}
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			bad++
		}
	}
	return bad*100 <= limit // <=1% control chars
}
