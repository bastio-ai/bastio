package workspace

import (
	"regexp"
	"strings"

	"github.com/bastio-ai/bastio/internal/providers"
)

// imageDataURLPattern matches a markdown image referencing a base64
// data URL: `![alt-or-filename](data:image/<mime>;base64,<payload>)`.
//
// The chat-attachments endpoint encodes uploaded images this way and
// composeWithAttachments embeds them inline in the user message
// content. We re-extract them here so the provider client gets a
// proper multimodal request and the persisted message body remains
// the single source of truth (no separate attachments column).
//
// (?s) flag isn't needed — base64 has no newlines and we're matching
// on a single line. Greedy `(.*?)` for alt; non-greedy `([^)]+)` for
// the URL stops at the closing paren.
var imageDataURLPattern = regexp.MustCompile(
	`!\[([^\]]*)\]\(data:(image/[a-zA-Z0-9.+-]+);base64,([A-Za-z0-9+/=]+)\)`,
)

// extractImagesForProvider walks `content`, peels out every markdown
// image with a `data:image/...;base64,...` URL, and returns the text
// stripped of those markers plus a slice of providers.Image blocks
// for the multimodal request.
//
// Why strip from text: most providers' content-parts shape lists the
// images SEPARATELY from text. If we left the markdown image syntax
// in the text part, gpt-4o etc. would see literal "![alt](data:...)"
// which is meaningless to them and would inflate the text token
// count by ~30% of the base64 payload. Better to extract.
//
// The leftover text is trimmed of empty lines that the strip
// produced (markdown images often sit on their own line).
func extractImagesForProvider(content string) (string, []providers.Image) {
	matches := imageDataURLPattern.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, nil
	}
	images := make([]providers.Image, 0, len(matches))
	for _, idx := range matches {
		// idx[0]=full start, idx[1]=full end,
		// idx[2..3]=alt, idx[4..5]=mime, idx[6..7]=data
		alt := content[idx[2]:idx[3]]
		mime := content[idx[4]:idx[5]]
		data := content[idx[6]:idx[7]]
		images = append(images, providers.Image{
			MimeType: mime,
			Data:     data,
			Filename: alt,
		})
	}
	// Replace markers with empty string. Loop in reverse so earlier
	// indexes stay valid as we mutate.
	out := []byte(content)
	for i := len(matches) - 1; i >= 0; i-- {
		idx := matches[i]
		out = append(out[:idx[0]], out[idx[1]:]...)
	}
	cleaned := strings.TrimSpace(collapseBlankLines(string(out)))
	return cleaned, images
}

// recomposeWithImages re-attaches image data URLs to a (potentially
// scan-sanitized) text body. Used by runProvider / streamProvider to
// shield the security engine from the high-entropy base64 payload —
// we extract images out before the scan, scan only the text, then
// re-embed the images afterwards so the persisted message still has
// the data URLs the bubble UI + future-turn extraction need.
//
// Order: text first, then each image on its own line. Mirrors how
// composeWithAttachments writes them in the first place.
func recomposeWithImages(text string, images []providers.Image) string {
	if len(images) == 0 {
		return text
	}
	parts := make([]string, 0, 1+len(images))
	if t := strings.TrimSpace(text); t != "" {
		parts = append(parts, t)
	}
	for _, img := range images {
		alt := img.Filename
		if alt == "" {
			alt = "image"
		}
		parts = append(parts, "!["+alt+"](data:"+img.MimeType+";base64,"+img.Data+")")
	}
	return strings.Join(parts, "\n\n")
}

// collapseBlankLines reduces sequences of 3+ newlines to 2 (one blank
// line). Image extraction often leaves "text\n\n\n\nmore text"; this
// keeps the message readable for the provider and the persisted
// echoed-back log.
func collapseBlankLines(s string) string {
	out := strings.Builder{}
	out.Grow(len(s))
	newlines := 0
	for _, r := range s {
		if r == '\n' {
			newlines++
			if newlines <= 2 {
				out.WriteRune(r)
			}
			continue
		}
		newlines = 0
		out.WriteRune(r)
	}
	return out.String()
}
