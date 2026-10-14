package workspace

import (
	"context"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/google/uuid"
	"mime"
	"net/http"
)

type callOptionsKey struct{}
type providerCallOptions struct {
	reservation     uuid.UUID
	hasReservation  bool
	toolPlan        *assistantToolPlan
	tools           []providers.ToolDefinition
	maxOutput       int
	reasoningEffort string
	resolvedKey     string
	keyResolved     bool
}

func callOptions(ctx context.Context) providerCallOptions {
	value, _ := ctx.Value(callOptionsKey{}).(providerCallOptions)
	return value
}
func callMaxOutput(ctx context.Context) int {
	if maximum := callOptions(ctx).maxOutput; maximum > 0 && maximum <= workspaceMaxOutputTokens {
		return maximum
	}
	return workspaceMaxOutputTokens
}

// JSON-only writes require a CORS preflight when sent from another origin.
func requireJSONContent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeError(w, 400, "JSON content type required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
