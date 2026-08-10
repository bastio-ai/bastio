package notify

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	dispatcher *Dispatcher
}

func NewHandler(dispatcher *Dispatcher) *Handler {
	return &Handler{
		dispatcher: dispatcher,
	}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.ListWebhooks)
	r.Post("/", h.CreateWebhook)
	r.Delete("/{id}", h.DeleteWebhook)
	return r
}

func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	targets := h.dispatcher.ListTargets()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": targets})
}

func (h *Handler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req WebhookTarget
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if req.URL == "" || req.Name == "" {
		http.Error(w, `{"error":"name and url are required"}`, http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		req.ID = uuid.New().String()
	}
	if req.Type == "" {
		req.Type = "generic"
	}
	if req.MinSeverity == "" {
		req.MinSeverity = "high"
	}
	req.Enabled = true

	h.dispatcher.AddTarget(&req)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(req)
}

func (h *Handler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"id is required"}`, http.StatusBadRequest)
		return
	}

	h.dispatcher.RemoveTarget(id)
	w.WriteHeader(http.StatusNoContent)
}
