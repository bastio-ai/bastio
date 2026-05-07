package workspace

import (
	"net/http"
)

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	cid := customerIDFromCtx(r.Context())
	st, err := h.store.EnsureSettings(r.Context(), cid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// EffectiveModelsResponse is the body of GET /v1/workspace/me/effective-models.
// Always returns a non-null array (possibly empty) so callers don't
// have to special-case JSON null. An empty array means "no whitelist
// — show the full catalog" (consistent with the rest of the workspace).
type EffectiveModelsResponse struct {
	AllowedModels []AllowedModel `json:"allowed_models"`
}

// getEffectiveModels returns the merged allowed_models list for the
// caller — workspace_members override beats workspace_settings, which
// beats "open" (empty list = full catalog). The workspace-app fetches
// this on chat boot so its model picker reflects the per-user filter.
func (h *Handler) getEffectiveModels(w http.ResponseWriter, r *http.Request) {
	cid := customerIDFromCtx(r.Context())
	uid := userIDFromCtx(r.Context())
	allowed, err := h.store.EffectiveAllowedModels(r.Context(), cid, uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if allowed == nil {
		allowed = []AllowedModel{}
	}
	writeJSON(w, http.StatusOK, EffectiveModelsResponse{AllowedModels: allowed})
}

func (h *Handler) patchSettings(w http.ResponseWriter, r *http.Request) {
	cid := customerIDFromCtx(r.Context())
	if _, err := h.store.EnsureSettings(r.Context(), cid); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var patch SettingsPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if patch.BillingMode != nil && *patch.BillingMode != "platform_keys" && *patch.BillingMode != "byo_keys" {
		writeError(w, http.StatusBadRequest, "billing_mode must be platform_keys or byo_keys")
		return
	}
	st, err := h.store.UpdateSettings(r.Context(), cid, patch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit(r, "settings.updated",
		AuditTarget{Type: "settings", ID: cid.String(), Label: ""},
		map[string]any{"fields_changed": fieldsFromSettingsPatch(patch)})
	writeJSON(w, http.StatusOK, st)
}

// fieldsFromSettingsPatch reports which SettingsPatch fields the
// caller set. Same shape as fieldsFromPatch in assistants.go —
// audit gets a list of changed names without dumping full values
// (some fields like AI persona prompt could be long).
func fieldsFromSettingsPatch(p SettingsPatch) []string {
	out := []string{}
	if p.Branding != nil {
		out = append(out, "branding")
	}
	if p.SeatLimit != nil {
		out = append(out, "seat_limit")
	}
	if p.RetentionDays != nil {
		out = append(out, "retention_days")
	}
	if p.SpendCapCents != nil {
		out = append(out, "spend_cap_cents")
	}
	if p.BillingMode != nil {
		out = append(out, "billing_mode")
	}
	if p.AllowedModels != nil {
		out = append(out, "allowed_models")
	}
	if p.AIPersonaName != nil {
		out = append(out, "ai_persona_name")
	}
	if p.AIPersonaPersonality != nil {
		out = append(out, "ai_persona_personality")
	}
	if p.AIPersonaTone != nil {
		out = append(out, "ai_persona_tone")
	}
	if p.DisableImageAttachments != nil {
		out = append(out, "disable_image_attachments")
	}
	return out
}
