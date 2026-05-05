package workspace

import (
	"bytes"
	"errors"
	"net/http"
)

// maxUploadBytes caps multipart uploads. Aliased to the security-aware
// constant so we have one source of truth for the cap. Both the upload
// path's parser and the upload-bytes-buffer below honour the same
// limit.
const maxUploadBytes = maxKBUploadBytes

func (h *Handler) listKnowledge(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.ListKnowledgeSources(r.Context(), customerIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": rows})
}

func (h *Handler) getKnowledge(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	k, err := h.store.GetKnowledgeSource(r.Context(), customerIDFromCtx(r.Context()), id)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	writeJSON(w, http.StatusOK, k)
}

func (h *Handler) createKnowledge(w http.ResponseWriter, r *http.Request) {
	var body KnowledgeSource
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if body.Type == "" {
		body.Type = "text"
	}
	if body.Type != "file" && body.Type != "url" && body.Type != "text" {
		writeError(w, http.StatusBadRequest, "type must be file, url, or text")
		return
	}
	cid := customerIDFromCtx(r.Context())
	body.CustomerID = cid

	// Inline-text path runs the security scan synchronously here —
	// there's no worker to defer to. File/URL types fall through to
	// the existing async-ingest flow where the worker scans after
	// extraction. Inline text is small and the request can afford
	// the scan latency in the foreground.
	var scanDecision *IngestScanDecision
	if body.Type == "text" && body.InlineText != nil && *body.InlineText != "" {
		decision, err := h.scanForIngest(r.Context(), cid, *body.InlineText)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "security scan: "+err.Error())
			return
		}
		scanDecision = decision
		switch decision.Action {
		case "block":
			// Don't even create the source row — there's nothing to
			// quarantine when the upload is inline. Surface the
			// rejection with detected categories so the user knows
			// what tripped.
			h.audit(r, "knowledge.scan_blocked",
				AuditTarget{Type: "knowledge", ID: "", Label: body.Name},
				map[string]any{
					"type":       body.Type,
					"categories": decision.Categories,
				})
			writeStructuredError(w, http.StatusForbidden,
				"knowledge_blocked_by_security",
				"this content was blocked by your workspace's security policy",
				map[string]any{"categories": decision.Categories})
			return
		case "sanitize":
			// Substitute the rewritten text. The original never lands
			// in storage. The size_bytes reflects the sanitized form
			// so the dashboard's "X chars" accounting is accurate.
			sanitized := decision.SanitizedContent
			body.InlineText = &sanitized
		}
	}

	if body.Status == "" {
		// Inline text is ready immediately. File/URL go through async ingest.
		if body.Type == "text" && body.InlineText != nil {
			body.Status = "ready"
			body.SizeBytes = int64(len(*body.InlineText))
		} else {
			body.Status = "pending"
		}
	}
	created, err := h.store.CreateKnowledgeSource(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Chunk synchronously for inline-text sources — small, fast, and
	// keeps the KB ready for retrieval immediately. File/URL ingestion
	// runs through the worker. Pass the scan tags so chunks land with
	// `sanitized` + `scan_categories` populated.
	sanitized := scanDecision != nil && scanDecision.Action == "sanitize"
	var categories []string
	if scanDecision != nil {
		categories = scanDecision.Categories
	}
	if err := h.store.chunkAndEmbedWithScan(r.Context(), created, h.embedder, sanitized, categories); err != nil {
		// Surface a 207-style outcome: the source row was created but
		// chunking failed. The frontend will show status='failed' on the
		// next list refresh and the user can retry.
		h.audit(r, "knowledge.created",
			AuditTarget{Type: "knowledge", ID: created.ID.String(), Label: created.Name},
			map[string]any{"type": created.Type, "chunking_error": err.Error()})
		writeJSON(w, http.StatusCreated, map[string]any{
			"source":         created,
			"chunking_error": err.Error(),
		})
		return
	}
	auditMeta := map[string]any{"type": created.Type}
	if sanitized {
		auditMeta["scan_action"] = "sanitize"
		auditMeta["categories"] = categories
	}
	h.audit(r, "knowledge.created",
		AuditTarget{Type: "knowledge", ID: created.ID.String(), Label: created.Name},
		auditMeta)
	writeJSON(w, http.StatusCreated, created)
}

// uploadKnowledge handles POST /v1/workspace/knowledge/upload — a
// multipart/form-data request with a single `file` field plus optional
// `name`. Flow:
//
//  1. Stream the file to the BlobStore (returns a deterministic ref).
//  2. Insert a workspace_knowledge_sources row with type='file',
//     status='pending', source_ref pointing at the blob.
//  3. Enqueue a WorkspaceIngestKnowledgeArgs River job to extract +
//     chunk asynchronously. When River isn't configured, run the
//     worker inline so OSS server-only deployments still work.
//
// Why upload-then-enqueue instead of upload-and-process: the request
// returns as soon as the blob is durable, giving the dashboard a row
// to render with status='pending'. Long extractions (large PDFs) can
// take seconds; we don't make the user watch a spinner.
func (h *Handler) uploadKnowledge(w http.ResponseWriter, r *http.Request) {
	if h.blobs == nil {
		writeError(w, http.StatusServiceUnavailable,
			"file uploads disabled: set BASTIO_DATA_DIR to enable workspace blob storage")
		return
	}

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' field: "+err.Error())
		return
	}
	defer file.Close()

	if header.Size > maxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge,
			"file exceeds maximum upload size")
		return
	}

	name := r.FormValue("name")
	if name == "" {
		name = header.Filename
	}
	if name == "" {
		name = "untitled"
	}
	mimeType := header.Header.Get("Content-Type")

	// Read the upload into memory once. We need the bytes for both
	// (a) the MIME allowlist + hash check and (b) the blob store
	// write. Bounded by maxKBUploadBytes (size cap above + the
	// io.LimitReader inside hashReader); a 25 MB ceiling is the
	// memory cost per concurrent upload, acceptable for the volume.
	hash, body, err := hashReader(file)
	if err != nil {
		switch {
		case errors.Is(err, ErrKBTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		default:
			writeError(w, http.StatusBadRequest, "read upload: "+err.Error())
		}
		return
	}

	// MIME allowlist — refuse anything we can't safely text-extract
	// before it touches the blob store. Cheap reject, big payoff:
	// keeps the worker process from running an extractor over
	// hostile binary formats.
	if _, vErr := validateKBUpload(body, mimeType); vErr != nil {
		switch {
		case errors.Is(vErr, ErrKBMimeNotAllowed):
			writeStructuredError(w, http.StatusUnsupportedMediaType,
				"unsupported_mime_type",
				vErr.Error(),
				map[string]any{"mime_type": mimeType, "allowed": kbAllowedMimeList()})
		case errors.Is(vErr, ErrKBTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, vErr.Error())
		default:
			writeError(w, http.StatusBadRequest, vErr.Error())
		}
		return
	}

	cid := customerIDFromCtx(r.Context())

	// Reserve a UUID up-front so the blob ref + DB row stay in sync.
	// (We can't use INSERT ... RETURNING then upload, because the blob
	// ref is what gets persisted in source_ref.)
	src := KnowledgeSource{
		CustomerID: cid,
		Name:       name,
		Type:       "file",
		Status:     "pending",
		MimeType:   &mimeType,
	}
	created, err := h.store.CreateKnowledgeSource(r.Context(), src)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create source: "+err.Error())
		return
	}

	// Record the hash up-front so dedup queries + post-incident
	// forensics work even if the worker crashes mid-extract.
	if err := h.store.SetKnowledgeSourceHash(r.Context(), cid, created.ID, hash); err != nil {
		// Non-fatal; the row is still usable. Log and proceed.
		// Hashes are forensic-only — they don't gate ingest itself.
		_ = err
	}

	ref, size, err := h.blobs.Put(r.Context(), cid, created.ID, name, bytes.NewReader(body))
	if err != nil {
		// Rollback the row so the dashboard doesn't show a broken
		// pending source the worker can never satisfy.
		_ = h.store.ArchiveKnowledgeSource(r.Context(), cid, created.ID)
		writeError(w, http.StatusBadRequest, "store blob: "+err.Error())
		return
	}

	// Patch the row with the blob ref + size.
	const upd = `UPDATE workspace_knowledge_sources
SET source_ref = $3, size_bytes = $4
WHERE customer_id = $1 AND id = $2`
	if _, err := h.store.pool.Exec(r.Context(), upd, cid, created.ID, ref, size); err != nil {
		writeError(w, http.StatusInternalServerError, "patch source: "+err.Error())
		return
	}
	created.SourceRef = ref
	created.SizeBytes = size

	// Enqueue or run inline. Either way, status will land on 'ready' or
	// 'failed' shortly — dashboard polls.
	if err := EnqueueIngest(r.Context(), h.river, h.store.pool, h.blobs, h.embedder,
		h.secEngine, h.secProfiles,
		WorkspaceIngestKnowledgeArgs{
			SourceID:   created.ID,
			CustomerID: cid,
		}); err != nil {
		// Don't block the upload on enqueue failure — surface the row
		// so the user can manually retry. Mark error for visibility.
		const failQ = `UPDATE workspace_knowledge_sources SET status = 'failed', error = $3
WHERE customer_id = $1 AND id = $2`
		_, _ = h.store.pool.Exec(r.Context(), failQ, cid, created.ID, "enqueue failed: "+err.Error())
	}

	h.audit(r, "knowledge.uploaded",
		AuditTarget{Type: "knowledge", ID: created.ID.String(), Label: created.Name},
		map[string]any{"size_bytes": created.SizeBytes, "mime_type": created.MimeType})
	writeJSON(w, http.StatusCreated, created)
}

// releaseKnowledge takes a quarantined source back to status='pending'
// so the next ingest run picks it up. Admin-only — gated at the route
// level. The release action is heavily audited because it's the
// override path that bypasses the security gate, and a careless
// admin clicking "release" on a malicious upload is exactly the
// situation a future incident-response review will care about.
//
// The scan_result snapshot on the row is left in place — the audit
// trail shows what was caught + that an admin chose to release.
func (h *Handler) releaseKnowledge(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	cid := customerIDFromCtx(r.Context())
	prev, _ := h.store.GetKnowledgeSource(r.Context(), cid, id)
	if prev != nil && prev.Status != "quarantined" {
		writeStructuredError(w, http.StatusConflict,
			"not_quarantined",
			"only quarantined sources can be released",
			map[string]any{"current_status": prev.Status})
		return
	}
	if err := h.store.ReleaseKnowledgeSource(r.Context(), cid, id); err != nil {
		notFoundOr500(w, err)
		return
	}
	label := ""
	var prevCategories []string
	if prev != nil {
		label = prev.Name
		prevCategories = scanCategoriesFromSourceRow(prev)
	}
	h.audit(r, "knowledge.released_from_quarantine",
		AuditTarget{Type: "knowledge", ID: id.String(), Label: label},
		map[string]any{"categories_at_quarantine": prevCategories})

	// Re-enqueue the ingest job — same path as a fresh upload. The
	// worker will re-scan; if the policy changed and the scan now
	// passes, the source flips ready. If it still fails, status goes
	// back to quarantined and the admin sees they need to fix the
	// content, not the policy.
	if err := EnqueueIngest(r.Context(), h.river, h.store.pool, h.blobs, h.embedder,
		h.secEngine, h.secProfiles,
		WorkspaceIngestKnowledgeArgs{
			SourceID:   id,
			CustomerID: cid,
		}); err != nil {
		// Don't unwind the release — the row is back to pending and
		// the admin can manually re-trigger via the dashboard. Log
		// for visibility.
		_ = err
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     id,
		"status": "pending",
	})
}

func (h *Handler) archiveKnowledge(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	cid := customerIDFromCtx(r.Context())
	prev, _ := h.store.GetKnowledgeSource(r.Context(), cid, id)
	if err := h.store.ArchiveKnowledgeSource(r.Context(), cid, id); err != nil {
		notFoundOr500(w, err)
		return
	}
	label := ""
	if prev != nil {
		label = prev.Name
	}
	h.audit(r, "knowledge.archived",
		AuditTarget{Type: "knowledge", ID: id.String(), Label: label}, nil)
	w.WriteHeader(http.StatusNoContent)
}
