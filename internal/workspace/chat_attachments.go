package workspace

import (
	"encoding/base64"
	"io"
	"net/http"
	"strings"
)

// chatAttachmentMaxBytes caps the per-file upload size for chat
// attachments. 25 MB matches the Knowledge Base ingest cap. Anything
// larger should land in the Knowledge Base — chat attachments are for
// one-shot context, not durable corpora.
const chatAttachmentMaxBytes = 25 * 1024 * 1024

// chatAttachmentResponse is the shape the workspace-app expects back.
// `text` is the extracted plain text — the chat client inlines it as
// a fenced code block in the outgoing user message. `text` is empty
// for unsupported types (e.g. images), and the caller treats that as
// "binary placeholder, content not extracted".
type chatAttachmentResponse struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
	Text     string `json:"text"`
	// Extracted is false for binary types we couldn't extract (e.g.
	// images). The frontend uses this to render a clear placeholder
	// instead of an empty content block.
	Extracted bool `json:"extracted"`
	// ExtractError, if non-empty, surfaces a per-file extraction
	// failure (encrypted PDF, malformed docx) without failing the
	// whole upload — multi-file picks would otherwise lose all the
	// good extractions because one file blew up.
	ExtractError string `json:"extract_error,omitempty"`
	// IsImage marks image uploads. Frontend uses it to know whether
	// to embed the file as a markdown image (multimodal-capable
	// model can see it) vs. an extraction placeholder.
	IsImage bool `json:"is_image"`
	// DataURL is populated for images: a `data:<mime>;base64,<data>`
	// string the frontend embeds inline in the message as a markdown
	// image. Backend re-extracts on send and forwards as content
	// parts to the provider.
	DataURL string `json:"data_url,omitempty"`
}

// uploadChatAttachment handles POST /v1/workspace/chat-attachments —
// a one-shot multipart upload. The server runs the same ExtractText
// path the Knowledge Base ingest uses, then returns the plain text
// for the frontend to inline into the next chat message. Nothing is
// persisted: chat attachments are ephemeral context, not searchable
// corpora. (For durable, retrievable docs, use the Knowledge Base.)
//
// Image types are accepted but not extracted — the response carries
// extracted=false and the frontend renders a placeholder. Multimodal
// image support is gated on the provider clients learning the
// content-parts shape, which is a separate refactor.
func (h *Handler) uploadChatAttachment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(chatAttachmentMaxBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' field: "+err.Error())
		return
	}
	defer file.Close()

	if header.Size > chatAttachmentMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge,
			"file exceeds maximum upload size of 25 MB")
		return
	}

	mime := header.Header.Get("Content-Type")

	// Workspace setting: hard-block image attachments. The
	// admin-facing toggle exists because image pixel content
	// bypasses the security engine — we strip base64 before
	// scanning the surrounding text and recompose afterwards,
	// so only the model provider's safety layer inspects the
	// pixels. Regulated workspaces flip this on to refuse
	// images entirely. Other attachment types (PDFs treated as
	// context, text) still work — the toggle is image-specific.
	if strings.HasPrefix(strings.ToLower(mime), "image/") {
		settings, _ := h.store.EnsureSettings(r.Context(), customerIDFromCtx(r.Context()))
		if settings != nil && settings.DisableImageAttachments {
			writeStructuredError(w, http.StatusForbidden,
				"image_attachments_disabled",
				"this workspace's admin has disabled image attachments",
				map[string]any{"mime_type": mime})
			return
		}
	}

	resp := chatAttachmentResponse{
		Name:     header.Filename,
		MimeType: mime,
		Size:     header.Size,
	}

	// Images: skip text extraction; instead, base64-encode the raw
	// bytes and return a data: URL the frontend embeds inline.
	// Capped per chatAttachmentMaxBytes (25MB) — way above what
	// gpt-4o / claude-3 actually accept (typically 5MB before
	// resize), but we let the provider reject oversized images
	// with a clearer error than ours.
	if isImageMime(mime, header.Filename) {
		raw, err := io.ReadAll(file)
		if err != nil {
			resp.ExtractError = "read file: " + err.Error()
			writeJSON(w, http.StatusOK, resp)
			return
		}
		if mime == "" {
			mime = "image/*"
		}
		resp.IsImage = true
		resp.MimeType = mime
		resp.DataURL = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
		writeJSON(w, http.StatusOK, resp)
		return
	}

	text, extractErr := ExtractText(file, mime, header.Filename)
	if extractErr != nil {
		resp.ExtractError = extractErr.Error()
		// Still return 200 so the frontend can render an attachment
		// chip with a clear "couldn't read this file" hint, instead
		// of swallowing the upload entirely.
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Text = text
	resp.Extracted = true
	writeJSON(w, http.StatusOK, resp)
}

// isImageMime returns true for the image types we deliberately skip
// extraction for. Filename-based fallback handles uploads that don't
// carry a proper Content-Type header (drag-and-drop from some
// browsers does this).
func isImageMime(mime, filename string) bool {
	if len(mime) >= 6 && mime[:6] == "image/" {
		return true
	}
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic", ".heif", ".bmp", ".tiff"} {
		if hasSuffixCaseInsensitive(filename, ext) {
			return true
		}
	}
	return false
}

func hasSuffixCaseInsensitive(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	tail := s[len(s)-len(suffix):]
	if len(tail) != len(suffix) {
		return false
	}
	for i := 0; i < len(tail); i++ {
		a := tail[i]
		b := suffix[i]
		if a >= 'A' && a <= 'Z' {
			a += 32
		}
		if b >= 'A' && b <= 'Z' {
			b += 32
		}
		if a != b {
			return false
		}
	}
	return true
}
