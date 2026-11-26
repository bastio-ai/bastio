package protection

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

const MaxDocumentBytes = 8 << 20

// DocumentText deliberately returns text, never a modified binary document.
// Images, layout, macros, links, and other binary elements are not forwarded.
// Unsupported, encrypted, empty, or oversized documents must be withheld.
func DocumentText(body []byte, mediaType string) (string, error) {
	if len(body) > MaxDocumentBytes {
		return "", fmt.Errorf("document too large")
	}
	var text string
	switch mediaType {
	case "text/plain", "text/markdown", "text/csv":
		if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
			return "", fmt.Errorf("document is not UTF-8 text")
		}
		text = string(body)
	case "application/pdf":
		r, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return "", fmt.Errorf("PDF cannot be read")
		}
		if r.NumPage() > 100 {
			return "", fmt.Errorf("PDF exceeds prototype page limit")
		}
		var out strings.Builder
		for n := 1; n <= r.NumPage(); n++ {
			p := r.Page(n)
			if p.V.IsNull() {
				return "", fmt.Errorf("PDF page cannot be read")
			}
			v, err := p.GetPlainText(nil)
			if err != nil {
				return "", fmt.Errorf("PDF text extraction failed")
			}
			if out.Len()+len(v) > MaxDocumentBytes {
				return "", fmt.Errorf("extracted PDF too large")
			}
			out.WriteString(v)
			out.WriteByte('\n')
		}
		text = out.String()
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		r, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return "", fmt.Errorf("DOCX cannot be read")
		}
		var doc *zip.File
		for _, f := range r.File {
			if f.Name == "word/document.xml" {
				if doc != nil {
					return "", fmt.Errorf("ambiguous DOCX")
				}
				doc = f
			}
		}
		if doc == nil || doc.UncompressedSize64 > MaxDocumentBytes {
			return "", fmt.Errorf("DOCX text missing or too large")
		}
		f, err := doc.Open()
		if err != nil {
			return "", fmt.Errorf("DOCX text cannot be read")
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, MaxDocumentBytes+1))
		if err != nil || len(data) > MaxDocumentBytes {
			return "", fmt.Errorf("DOCX extraction limit exceeded")
		}
		decoder := xml.NewDecoder(bytes.NewReader(data))
		var out strings.Builder
		inText := false
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("DOCX XML is invalid")
			}
			switch v := token.(type) {
			case xml.StartElement:
				if v.Name.Local == "t" {
					inText = true
				}
				if v.Name.Local == "p" || v.Name.Local == "br" {
					out.WriteByte('\n')
				}
			case xml.EndElement:
				if v.Name.Local == "t" {
					inText = false
				}
			case xml.CharData:
				if inText {
					out.Write(v)
				}
			}
		}
		text = out.String()
	default:
		return "", fmt.Errorf("unsupported document media type")
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("document has no readable text")
	}
	return text, nil
}
