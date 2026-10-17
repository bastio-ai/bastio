package gateway

import (
	"bytes"
	"io"
	"net/http"

	"github.com/bastio-ai/bastio/internal/providers"
)

// Inspection middleware is invoked for each actual item, not a job envelope
// or binary multipart upload. Cloud uses its existing semantic classifiers.
// No billing, routing, upstream transport or retries run in this subpipeline.
func (b *batchCall) inspect(raw []byte, response bool) ([]byte, error) {
	middleware := b.h.batchInputMiddleware
	if response {
		middleware = b.h.batchOutputMiddleware
	}
	if middleware == nil {
		return raw, nil
	}
	r := b.r.Clone(b.r.Context())
	r.Method = "POST"
	r.Header = b.r.Header.Clone()
	r.Header.Set("Content-Type", "application/json")
	r.URL.Path = "/v1/openai/chat/completions"
	if b.route.provider == providers.ProviderAnthropic {
		r.URL.Path = "/v1/anthropic/messages"
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	captured := &batchInspectionWriter{header: make(http.Header), status: 200}
	var input []byte
	var readErr error
	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if response {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(raw)
		} else {
			input, readErr = io.ReadAll(req.Body)
			w.WriteHeader(200)
		}
	})
	middleware(next).ServeHTTP(captured, r)
	if captured.status >= 400 {
		return nil, &routeError{captured.status, "batch item withheld by extended security policy"}
	}
	if readErr != nil {
		return nil, readErr
	}
	if response {
		return captured.body.Bytes(), nil
	}
	return input, nil
}

type batchInspectionWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *batchInspectionWriter) Header() http.Header           { return w.header }
func (w *batchInspectionWriter) WriteHeader(status int)        { w.status = status }
func (w *batchInspectionWriter) Write(raw []byte) (int, error) { return w.body.Write(raw) }
