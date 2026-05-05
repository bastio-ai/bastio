package config

import (
	"encoding/json"
	"net/http"
)

// Handler serves configuration endpoints.
type Handler struct {
	cfg *Config
}

// NewHandler creates a new config handler.
func NewHandler(cfg *Config) *Handler {
	return &Handler{cfg: cfg}
}

// GetConfig returns the current server configuration.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"mode":          h.cfg.Mode,
		"security_mode": h.cfg.SecurityMode,
		"port":          h.cfg.Port,
		"version":       "0.1.0",
		"license":       "FSL-1.1-ALv2",
	})
}
