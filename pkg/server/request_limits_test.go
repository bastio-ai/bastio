package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNativeRequestLimitsKeepManagementCaps(t *testing.T) {
	for _, tc := range []struct {
		path    string
		status  int
		timeout time.Duration
	}{
		{"/v1/proxies", 413, time.Minute},
		{"/v1/workspace/settings", 413, time.Minute},
		{"/v1/workspace/chat-attachments", 200, 10 * time.Minute},
		{"/v1/workspace/knowledge/upload", 200, 10 * time.Minute},
		{"/v1/workspace/conversations/thread/messages/stream", 200, 10 * time.Minute},
		{"/v1/openrouter/chat/completions", 200, 10 * time.Minute},
		{"/v1/openai/files", 200, 10 * time.Minute},
		{"/v1/guard/proxy-id/v1/messages", 200, 10 * time.Minute},
		{"/v1/responses/response-id", 200, 10 * time.Minute},
	} {
		t.Run(tc.path, func(t *testing.T) {
			h := requestLimitsMiddleware(8, 32, 0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) < tc.timeout-time.Second || time.Until(deadline) > tc.timeout {
					t.Errorf("unexpected deadline: %v", deadline)
				}
				if _, err := io.ReadAll(r.Body); err != nil {
					w.WriteHeader(413)
					return
				}
				w.WriteHeader(200)
			}))
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader("1234567890123456")))
			if rr.Code != tc.status {
				t.Fatalf("status %d, expected %d", rr.Code, tc.status)
			}
		})
	}
}
