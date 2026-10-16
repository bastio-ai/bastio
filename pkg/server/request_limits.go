package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func gatewayTimeout(configured time.Duration) time.Duration {
	if configured > 0 {
		return configured
	}
	return 10 * time.Minute
}

// Native SDK resource APIs include slow reasoning calls and multipart uploads.
// Management requests keep their shorter timeout and smaller body limit.
func isProviderAPIPath(path string) bool {
	for _, prefix := range []string{"/v1/typesafe/", "/v1/openrouter/", "/v1/openai/", "/v1/anthropic/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	if strings.HasPrefix(path, "/v1/guard/") && strings.Contains(strings.TrimPrefix(path, "/v1/guard/"), "/v1/") {
		return true
	}
	if IsGenerationGatewayPath(path) {
		return true
	}
	for _, prefix := range []string{"/v1/responses", "/v1/models", "/v1/messages"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// Employee prompts and multipart documents need the same body allowance as
// model calls; management's 1 MiB limit would reject ordinary attachments.
func isWorkspaceContentPath(path string) bool {
	if path == "/v1/workspace/chat-attachments" || path == "/v1/workspace/knowledge/upload" {
		return true
	}
	return strings.HasPrefix(path, "/v1/workspace/conversations/") &&
		(strings.HasSuffix(path, "/messages") || strings.HasSuffix(path, "/messages/stream"))
}

func requestLimitsMiddleware(managementBytes, providerBytes int64, timeout time.Duration) func(http.Handler) http.Handler {
	if providerBytes <= 0 {
		providerBytes = 64 << 20
	}
	return func(next http.Handler) http.Handler {
		management := next
		if managementBytes > 0 {
			management = maxBodyBytesMiddleware(managementBytes)(management)
		}
		management = middleware.Timeout(time.Minute)(management)
		provider := middleware.Timeout(gatewayTimeout(timeout))(maxBodyBytesMiddleware(providerBytes)(next))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isProviderAPIPath(r.URL.Path) || isWorkspaceContentPath(r.URL.Path) {
				provider.ServeHTTP(w, r)
				return
			}
			management.ServeHTTP(w, r)
		})
	}
}
