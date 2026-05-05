package workspace

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) analyticsSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := h.store.AnalyticsSummary(r.Context(), customerIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (h *Handler) analyticsDaily(w http.ResponseWriter, r *http.Request) {
	days := intQuery(r, "days", 14)
	rows, err := h.store.AnalyticsDaily(r.Context(), customerIDFromCtx(r.Context()), days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": rows})
}

func (h *Handler) analyticsByModel(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.AnalyticsByModel(r.Context(), customerIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"by_model": rows})
}

// analyticsTopUsers returns the top-N users by cost in the current
// month. ?limit=N (default 10, capped at 100). Used by the per-user
// drill-down table on the analytics page.
func (h *Handler) analyticsTopUsers(w http.ResponseWriter, r *http.Request) {
	limit := intQuery(r, "limit", 10)
	rows, err := h.store.AnalyticsTopUsers(r.Context(), customerIDFromCtx(r.Context()), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": rows})
}

// analyticsForecast returns current-month spend, daily average,
// projected month-end, and the % delta vs the previous month total.
func (h *Handler) analyticsForecast(w http.ResponseWriter, r *http.Request) {
	f, err := h.store.AnalyticsForecast(r.Context(), customerIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// analyticsCompare returns this-week vs last-week stats. Rolling
// 7-day windows so the answer is stable regardless of weekday.
func (h *Handler) analyticsCompare(w http.ResponseWriter, r *http.Request) {
	c, err := h.store.AnalyticsCompare(r.Context(), customerIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// analyticsUserDetail returns one user's last-30-day breakdown.
// Path: /v1/workspace/analytics/users/{userID}.
func (h *Handler) analyticsUserDetail(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, "userID is required")
		return
	}
	d, err := h.store.AnalyticsUserDetail(r.Context(), customerIDFromCtx(r.Context()), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}
