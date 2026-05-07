package workspace

import (
	"net/http"

	"github.com/google/uuid"
)

// systemPromptScanLabel identifies the scan target in audit rows
// for assistants. Assistant system prompts are workspace-wide
// attack surface — every chat with the assistant inherits them —
// so audit visibility for blocked / sanitized prompts is critical.
const systemPromptScanLabel = "assistant"

func (h *Handler) listAssistants(w http.ResponseWriter, r *http.Request) {
	cid := customerIDFromCtx(r.Context())
	rows, err := h.store.ListAssistants(r.Context(), cid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assistants": rows})
}

func (h *Handler) getAssistant(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	a, err := h.store.GetAssistant(r.Context(), customerIDFromCtx(r.Context()), id)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) createAssistant(w http.ResponseWriter, r *http.Request) {
	var body Assistant
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	body.CustomerID = customerIDFromCtx(r.Context())
	if body.DefaultProvider == "" {
		body.DefaultProvider = "openai"
	}
	if body.DefaultModel == "" {
		body.DefaultModel = "gpt-4o-mini"
	}
	// Security gate — the assistant's system prompt is replayed
	// at the head of every chat with this assistant. A malicious
	// prompt is a workspace-wide attack vector. Same scan engine
	// + action mapping as KB ingest: block rejects (admin sees
	// detected categories), sanitize rewrites the persisted
	// prompt, allow proceeds verbatim. Empty system prompt skips
	// the scan (no content to inspect).
	scanned, scan, ok := h.scanSystemPromptGate(w, r, body.CustomerID,
		systemPromptScanLabel, "", body.Name, body.SystemPrompt)
	if !ok {
		return
	}
	body.SystemPrompt = scanned
	// body.Language nil/empty = auto-detect from user input. No
	// default — users can opt into "always English" by passing "en".
	created, err := h.store.CreateAssistant(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	auditMeta := map[string]any{
		"default_provider": created.DefaultProvider,
		"default_model":    created.DefaultModel,
	}
	if scan != nil && scan.Action == "sanitize" {
		auditMeta["scan_action"] = "sanitize"
		auditMeta["scan_categories"] = scan.Categories
	}
	h.audit(r, "assistant.created",
		AuditTarget{Type: "assistant", ID: created.ID.String(), Label: created.Name},
		auditMeta)
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) updateAssistant(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var patch AssistantPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	cid := customerIDFromCtx(r.Context())
	// Security gate on patches that touch system_prompt. Patches
	// that don't change the prompt skip the scan — there's nothing
	// new to inspect.
	var scan *IngestScanDecision
	if patch.SystemPrompt != nil {
		// Look up the existing row's name for the audit row label
		// (the patch may not include name).
		prev, _ := h.store.GetAssistant(r.Context(), cid, id)
		var label string
		if prev != nil {
			label = prev.Name
		}
		scanned, decision, ok := h.scanSystemPromptGate(w, r, cid,
			systemPromptScanLabel, id.String(), label, *patch.SystemPrompt)
		if !ok {
			return
		}
		patch.SystemPrompt = &scanned
		scan = decision
	}
	a, err := h.store.UpdateAssistant(r.Context(), cid, id, patch)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	auditMeta := map[string]any{"fields_changed": fieldsFromPatch(patch)}
	if scan != nil && scan.Action == "sanitize" {
		auditMeta["scan_action"] = "sanitize"
		auditMeta["scan_categories"] = scan.Categories
	}
	h.audit(r, "assistant.updated",
		AuditTarget{Type: "assistant", ID: a.ID.String(), Label: a.Name},
		auditMeta)
	writeJSON(w, http.StatusOK, a)
}

// scanSystemPromptGate runs the standard ingest scan on a saved
// system-prompt-style text field (assistant system_prompt, prompt
// template content). Returns the text the caller should persist
// (rewritten when the scan sanitized; verbatim otherwise) plus the
// decision for audit metadata. ok=false means a structured error
// has already been written and the caller must return.
//
// Empty input short-circuits with action="allow" — there's no
// content to scan. Mirrors the workspace's existing fail-open posture
// when the engine isn't wired (OSS-standalone).
func (h *Handler) scanSystemPromptGate(
	w http.ResponseWriter,
	r *http.Request,
	customerID uuid.UUID,
	targetType, targetID, targetLabel, content string,
) (string, *IngestScanDecision, bool) {
	if content == "" {
		return content, &IngestScanDecision{Action: "allow"}, true
	}
	decision, err := h.scanForIngest(r.Context(), customerID, content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "security scan: "+err.Error())
		return "", nil, false
	}
	switch decision.Action {
	case "block":
		// Audit BEFORE the response so the row lands even when the
		// caller's request times out reading the error.
		h.audit(r, targetType+".scan_blocked",
			AuditTarget{Type: targetType, ID: targetID, Label: targetLabel},
			map[string]any{"categories": decision.Categories})
		writeStructuredError(w, http.StatusForbidden,
			targetType+"_blocked_by_security",
			"this content was blocked by your workspace's security policy",
			map[string]any{"categories": decision.Categories})
		return "", decision, false
	case "sanitize":
		return decision.SanitizedContent, decision, true
	default:
		return content, decision, true
	}
}

func (h *Handler) archiveAssistant(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	cid := customerIDFromCtx(r.Context())
	// Snapshot name before archive so the audit row stays useful.
	prev, _ := h.store.GetAssistant(r.Context(), cid, id)
	if err := h.store.ArchiveAssistant(r.Context(), cid, id); err != nil {
		notFoundOr500(w, err)
		return
	}
	label := ""
	if prev != nil {
		label = prev.Name
	}
	h.audit(r, "assistant.archived",
		AuditTarget{Type: "assistant", ID: id.String(), Label: label}, nil)
	w.WriteHeader(http.StatusNoContent)
}

// fieldsFromPatch lists which AssistantPatch fields the caller set,
// so the audit row says "name + system_prompt changed" instead of
// dumping the full new value (avoids logging the entire prompt body
// every time someone tweaks a comma).
func fieldsFromPatch(p AssistantPatch) []string {
	out := []string{}
	if p.Name != nil {
		out = append(out, "name")
	}
	if p.Description != nil {
		out = append(out, "description")
	}
	if p.SystemPrompt != nil {
		out = append(out, "system_prompt")
	}
	if p.DefaultProvider != nil {
		out = append(out, "default_provider")
	}
	if p.DefaultModel != nil {
		out = append(out, "default_model")
	}
	if p.Language != nil {
		out = append(out, "language")
	}
	if p.IsDefault != nil {
		out = append(out, "is_default")
	}
	if p.KnowledgeSourceIDs != nil {
		out = append(out, "knowledge_source_ids")
	}
	return out
}
