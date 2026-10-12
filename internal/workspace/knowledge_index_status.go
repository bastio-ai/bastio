package workspace

import "net/http"

type knowledgeIndexStatus struct {
	Enabled        bool   `json:"enabled"`
	Model          string `json:"model"`
	Dimensions     int    `json:"dimensions"`
	PendingSources int    `json:"pending_sources"`
}

func (h *Handler) knowledgeIndexStatus(w http.ResponseWriter, r *http.Request) {
	status := knowledgeIndexStatus{}
	if h.embedder == nil {
		writeJSON(w, 200, status)
		return
	}
	status.Enabled = true
	status.Model = embeddingModel(h.embedder)
	status.Dimensions = h.embedder.Dimension()
	err := h.store.pool.QueryRow(r.Context(), `SELECT count(*) FROM workspace_knowledge_sources s
WHERE s.customer_id=$1 AND s.archived_at IS NULL AND s.status='ready'
AND s.inline_text IS NOT NULL AND btrim(s.inline_text)<>''
AND (NOT EXISTS(SELECT 1 FROM workspace_knowledge_chunks k WHERE k.customer_id=$1 AND k.knowledge_source_id=s.id)
OR EXISTS(SELECT 1 FROM workspace_knowledge_chunks k WHERE k.customer_id=$1 AND k.knowledge_source_id=s.id
AND (k.embedding IS NULL OR k.embedding_vec IS NULL OR vector_dims(k.embedding_vec)<>$3 OR k.embedding_model<>$2)))`, customerIDFromCtx(r.Context()), status.Model, status.Dimensions).Scan(&status.PendingSources)
	if err != nil {
		writeError(w, 500, "knowledge indexing status unavailable")
		return
	}
	writeJSON(w, 200, status)
}
