package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// holdBackKeepAlive keeps a held-back stream alive behind proxy idle
// timeouts (Cloudflare cuts a response with no bytes for ~100 s). If the
// upstream stream is still running after the grace period it commits the
// response headers and sends SSE comment keep-alives at an interval, while
// the caller keeps buffering and scanning. Until halt returns, this
// goroutine is the only writer to the ResponseWriter; the caller must not
// touch it.
type holdBackKeepAlive struct {
	stop      chan struct{}
	done      chan struct{}
	committed bool // read only after halt
}

const sseKeepAlive = ": keepalive\n\n"

func (h *Handler) startHoldBackKeepAlive(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, status int) *holdBackKeepAlive {
	k := &holdBackKeepAlive{stop: make(chan struct{}), done: make(chan struct{})}
	if h.holdBackGrace <= 0 {
		close(k.done)
		return k
	}
	go func() {
		defer close(k.done)
		grace := time.NewTimer(h.holdBackGrace)
		defer grace.Stop()
		select {
		case <-k.stop:
			return
		case <-ctx.Done():
			return
		case <-grace.C:
		}
		w.WriteHeader(status)
		k.committed = true
		tick := time.NewTicker(h.holdBackInterval)
		defer tick.Stop()
		for {
			if _, err := io.WriteString(w, sseKeepAlive); err != nil {
				return
			}
			flusher.Flush()
			select {
			case <-k.stop:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	return k
}

// halt stops the keep-alives, waits for the writer to finish and reports
// whether the response headers were committed. Call it exactly once.
func (k *holdBackKeepAlive) halt() bool {
	close(k.stop)
	<-k.done
	return k.committed
}

// inStreamErrorEvent is the provider-shaped SSE error event a committed
// stream ends with when it is blocked or fails after keep-alives: the
// official SDKs raise it as an error. endpoint is the provider endpoint
// (ProviderEndpoint), code a short machine code, message the same text the
// uncommitted 403/502 body carries.
//
//	chat completions  data: {"error":{"message","type","code"}}
//	Claude messages   event: error + data: {"type":"error","error":{"type","message"}}
//	Responses         event: error + data: {"type":"error","code","message","param",
//	                  "error":{...}} (the SDKs raise on the "error" object)
//	Gemini            data: {"error":{"code","message","status"}}
func inStreamErrorEvent(endpoint, code, message string) []byte {
	type errBody struct {
		Type    string `json:"type,omitempty"`
		Code    string `json:"code,omitempty"`
		Message string `json:"message"`
	}
	var event string
	var payload []byte
	switch {
	case endpoint == "/messages":
		event = "error"
		payload, _ = json.Marshal(struct {
			Type  string  `json:"type"`
			Error errBody `json:"error"`
		}{"error", errBody{Type: "permission_error", Message: message}})
	case endpoint == "/responses" || strings.HasPrefix(endpoint, "/responses/"):
		event = "error"
		payload, _ = json.Marshal(struct {
			Type    string          `json:"type"`
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Param   json.RawMessage `json:"param"`
			Error   struct {
				Type    string `json:"type"`
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}{Type: "error", Code: code, Message: message, Param: json.RawMessage("null"), Error: struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}{"gateway_error", code, message}})
	case strings.Contains(endpoint, "GenerateContent"):
		payload, _ = json.Marshal(struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}{Error: struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		}{403, message, "PERMISSION_DENIED"}})
	default:
		payload, _ = json.Marshal(struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			} `json:"error"`
		}{Error: struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		}{message, "gateway_error", code}})
	}
	var b strings.Builder
	if event != "" {
		b.WriteString("event: " + event + "\n")
	}
	b.WriteString("data: ")
	b.Write(payload)
	b.WriteString("\n\n")
	return []byte(b.String())
}
