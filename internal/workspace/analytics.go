package workspace

import "net/http"

// Per-user analytics (analyticsTopUsers, analyticsUserDetail) are
// cloud-only — OSS is single-user, so there's nothing to rank or
// drill into. Those handlers live in bastio-cloud/internal/workspace.

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
