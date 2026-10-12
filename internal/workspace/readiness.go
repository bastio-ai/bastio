package workspace

import (
	"github.com/bastio-ai/bastio/internal/providers"
	"net/http"
)

type ProviderReadiness struct {
	Provider   string `json:"provider"`
	Configured bool   `json:"configured"`
}

func (h *Handler) providerReadiness(w http.ResponseWriter, r *http.Request) {
	out := []ProviderReadiness{}
	for _, name := range []string{"openai", "anthropic", "gemini", "deepseek", "xai", "groq", "openrouter", "bedrock", "ollama"} {
		ready := false
		if h.registry != nil {
			if client, err := h.registry.Get(providers.Provider(name)); err == nil {
				ready = h.resolveAPIKeyCtx(r.Context(), customerIDFromCtx(r.Context()), providers.Provider(name)) != ""
				if local, ok := client.(interface{ Configured() bool }); ok && name == "ollama" {
					ready = local.Configured()
				}
			}
		}
		out = append(out, ProviderReadiness{Provider: name, Configured: ready})
	}
	writeJSON(w, 200, struct {
		Providers []ProviderReadiness `json:"providers"`
	}{out})
}
