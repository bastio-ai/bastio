// Package prompts provides the OSS prompt registry: named, immutable-version
// prompt templates that applications fetch via SDK and that every trace
// carries a pointer to (prompt_name + prompt_version on observations).
//
// Cloud layers deploy-by-label workflows, approvals, and A/B experiments on
// top of the same tables.
package prompts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastio-ai/bastio/internal/llmpipeline"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/clickhouse"
	"github.com/bastio-ai/bastio/pkg/tenant"
)

// DefaultCustomerID is the seeded OSS single-tenant customer. Cloud code
// must not use this — it should resolve the tenant from request context.
var DefaultCustomerID = tenant.DefaultOSSID.String()

// Handler serves the /v1/prompts endpoints.
type Handler struct {
	db *pgxpool.Pool
	ch *clickhouse.CH
	// secEngine + secProfiles are optional; when wired the
	// CreateVersion / Create endpoints scan saved template
	// content the same way KB ingest scans uploaded documents.
	// Templates land at the head of every chat that loads them
	// (workspace assistants, the gateway's playground), so
	// missing this gate would let an admin save attack content
	// once and replay it everywhere.
	secEngine   *security.Engine
	secProfiles security.ProfileLookup
}

// New creates a Handler. The ClickHouse client is optional: when nil the
// usage-by-trace lookups return empty lists.
func New(db *pgxpool.Pool, ch *clickhouse.CH) *Handler {
	return &Handler{db: db, ch: ch}
}

// SetSecurityEngine wires the same engine the gateway uses so saved
// prompt templates pass through the security pipeline before they
// can become the system context of a future chat. Without this
// setter the handler fails open (templates land verbatim) — same
// posture as the rest of the platform's pre-fix behaviour.
func (h *Handler) SetSecurityEngine(e *security.Engine) { h.secEngine = e }

// SetSecurityProfiles pairs with SetSecurityEngine. Both must be
// set for content scanning to fire.
func (h *Handler) SetSecurityProfiles(p security.ProfileLookup) {
	h.secProfiles = p
}

// scanTemplateContent is the prompts-package equivalent of the
// workspace's scanSystemPromptGate. Runs the security engine on
// saved template text; returns the text the caller should
// persist (rewritten when sanitized; verbatim otherwise) plus a
// flag for whether anything was caught. ok=false means a
// structured error has already been written and the caller must
// return.
//
// Empty content short-circuits with allow. Engine not wired =
// fail-open allow + a startup-time warning log. Same posture as
// scanForIngest in the workspace package.
func (h *Handler) scanTemplateContent(
	w http.ResponseWriter,
	ctx context.Context,
	customerID uuid.UUID,
	content string,
) (string, []string, bool) {
	if content == "" || h.secEngine == nil || h.secProfiles == nil {
		return content, nil, true
	}
	profile, err := h.secProfiles.GetDefault(ctx, customerID)
	if err != nil {
		// Profile-lookup hiccup falls open. A production
		// deployment can flip this to fail-closed via a
		// future server option.
		return content, nil, true
	}
	res := llmpipeline.PreflightScan(ctx, llmpipeline.PreflightOptions{
		Engine:           h.secEngine,
		Profile:          profile,
		Content:          content,
		CustomerID:       customerID,
		SkipSanitization: false,
		// Templates always land at the head of a chat — the
		// engine treats RoleSystem content as untrusted
		// context-poisoning candidate, the right hint here.
		Role: security.RoleSystem,
	})
	if res == nil {
		return content, nil, true
	}
	categories := categoriesFromThreatTypes(res.ThreatTypes)
	switch {
	case res.ShouldBlock:
		writeStructuredError(w, http.StatusForbidden,
			"prompt_blocked_by_security",
			"this template was blocked by your workspace's security policy",
			map[string]any{"categories": categories})
		return "", categories, false
	case res.SanitizedContent != "" && res.SanitizedContent != content:
		return res.SanitizedContent, categories, true
	default:
		return content, categories, true
	}
}

// categoriesFromThreatTypes lowercases + dedupes the engine's
// typed threat list for use in audit metadata + structured
// errors. Mirrors the workspace's dedupeThreatTypes.
func categoriesFromThreatTypes(ts []security.ThreatType) []string {
	if len(ts) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ts))
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		s := strings.ToLower(string(t))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// writeStructuredError mirrors the workspace package's helper —
// returns a JSON body with `error`, `code`, and arbitrary detail
// keys, so the dashboard can surface category lists nicely.
func writeStructuredError(w http.ResponseWriter, status int, code, msg string, detail map[string]any) {
	body := map[string]any{
		"error": msg,
		"code":  code,
	}
	for k, v := range detail {
		body[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Routes returns the Chi router for prompt endpoints.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/{name}", h.Get)
	r.Delete("/{name}", h.Delete)
	r.Get("/{name}/versions", h.ListVersions)
	r.Post("/{name}/versions", h.CreateVersion)
	r.Put("/{name}/versions/{version}/labels", h.SetLabels)
	r.Get("/{name}/usage", h.Usage)
	return r
}

// List returns every prompt for the tenant plus latest-version metadata,
// joined in the database to keep the list page fast.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `
		SELECT
			p.id::text, p.name, p.description, p.created_at, p.updated_at,
			COALESCE(v.version, 0),
			COALESCE(v.content_type, ''),
			COALESCE(v.labels, '{}'::text[]),
			v.created_at
		FROM prompts p
		LEFT JOIN LATERAL (
			SELECT version, content_type, labels, created_at
			FROM prompt_versions
			WHERE prompt_id = p.id
			ORDER BY version DESC
			LIMIT 1
		) v ON TRUE
		WHERE p.customer_id = $1::uuid
		ORDER BY p.updated_at DESC
	`, DefaultCustomerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query prompts", err)
		return
	}
	defer rows.Close()

	out := []map[string]any{}
	for rows.Next() {
		var (
			id, name, description, contentType string
			created, updated                   time.Time
			latestVersion                      int32
			labels                             []string
			latestCreated                      *time.Time
		)
		if err := rows.Scan(&id, &name, &description, &created, &updated,
			&latestVersion, &contentType, &labels, &latestCreated); err != nil {
			slog.Error("scan prompt row", "error", err)
			continue
		}
		row := map[string]any{
			"id":             id,
			"name":           name,
			"description":    description,
			"created_at":     created.UTC().Format(time.RFC3339Nano),
			"updated_at":     updated.UTC().Format(time.RFC3339Nano),
			"latest_version": latestVersion,
			"labels":         labels,
			"content_type":   contentType,
		}
		if latestCreated != nil {
			row["latest_created_at"] = latestCreated.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

type createPromptRequest struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Content       string          `json:"content"`
	ContentType   string          `json:"content_type"`
	Config        json.RawMessage `json:"config"`
	Labels        []string        `json:"labels"`
	CommitMessage string          `json:"commit_message"`
	CreatedBy     string          `json:"created_by"`
}

// Create registers a new prompt and its first version (version = 1).
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createPromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body", err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", nil)
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required", nil)
		return
	}
	if req.ContentType == "" {
		req.ContentType = "text"
	}
	if len(req.Config) == 0 {
		req.Config = []byte("{}")
	}
	if req.Labels == nil {
		req.Labels = []string{}
	}

	// Security gate — saved templates become the system context
	// of every chat that loads them. Same scan engine + action
	// mapping as the workspace assistant system_prompt path.
	scannedContent, _, ok := h.scanTemplateContent(w, r.Context(), tenant.DefaultOSSID, req.Content)
	if !ok {
		return
	}
	req.Content = scannedContent

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "begin tx", err)
		return
	}
	defer tx.Rollback(r.Context())

	var promptID uuid.UUID
	var createdAt, updatedAt time.Time
	err = tx.QueryRow(r.Context(), `
		INSERT INTO prompts (customer_id, name, description)
		VALUES ($1::uuid, $2, $3)
		RETURNING id, created_at, updated_at
	`, DefaultCustomerID, req.Name, req.Description).Scan(&promptID, &createdAt, &updatedAt)
	if err != nil {
		// 23505 = unique_violation — name already taken for this tenant.
		if strings.Contains(err.Error(), "23505") {
			writeError(w, http.StatusConflict, "prompt name already exists", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "insert prompt", err)
		return
	}

	var versionID uuid.UUID
	var versionCreatedAt time.Time
	err = tx.QueryRow(r.Context(), `
		INSERT INTO prompt_versions (
			prompt_id, customer_id, version, content, content_type,
			config, labels, commit_message, created_by
		)
		VALUES ($1, $2::uuid, 1, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at
	`, promptID, DefaultCustomerID, req.Content, req.ContentType,
		string(req.Config), req.Labels, req.CommitMessage, req.CreatedBy).
		Scan(&versionID, &versionCreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "insert version", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "commit", err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":             promptID.String(),
		"name":           req.Name,
		"description":    req.Description,
		"created_at":     createdAt.UTC().Format(time.RFC3339Nano),
		"updated_at":     updatedAt.UTC().Format(time.RFC3339Nano),
		"latest_version": 1,
		"labels":         req.Labels,
		"content_type":   req.ContentType,
		"version": map[string]any{
			"id":             versionID.String(),
			"prompt_id":      promptID.String(),
			"version":        1,
			"content":        req.Content,
			"content_type":   req.ContentType,
			"config":         json.RawMessage(req.Config),
			"labels":         req.Labels,
			"commit_message": req.CommitMessage,
			"created_by":     req.CreatedBy,
			"created_at":     versionCreatedAt.UTC().Format(time.RFC3339Nano),
		},
	})
}

// Get fetches a single prompt by name. With ?version=N or ?label=LABEL the
// response includes that specific version inline (this is the SDK-facing
// path). Without either, the latest version is returned.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	promptID, descr, created, updated, ok := h.resolvePrompt(w, r, name)
	if !ok {
		return
	}

	q := r.URL.Query()
	var versionRow *promptVersionRow
	switch {
	case q.Get("version") != "":
		n, err := strconv.Atoi(q.Get("version"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid version", err)
			return
		}
		versionRow = h.fetchVersion(r, promptID, int32(n))
	case q.Get("label") != "":
		versionRow = h.fetchByLabel(r, promptID, q.Get("label"))
	default:
		versionRow = h.fetchLatest(r, promptID)
	}

	resp := map[string]any{
		"id":          promptID.String(),
		"name":        name,
		"description": descr,
		"created_at":  created.UTC().Format(time.RFC3339Nano),
		"updated_at":  updated.UTC().Format(time.RFC3339Nano),
	}
	if versionRow != nil {
		resp["version"] = versionRow.toMap()
		resp["latest_version"] = versionRow.Version
		resp["labels"] = versionRow.Labels
		resp["content_type"] = versionRow.ContentType
	}
	writeJSON(w, http.StatusOK, resp)
}

// Delete removes a prompt only if it has no versions. The UI is expected
// to confirm and then delete versions first (or we can loosen this later).
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	promptID, _, _, _, ok := h.resolvePrompt(w, r, name)
	if !ok {
		return
	}
	tag, err := h.db.Exec(r.Context(), `
		DELETE FROM prompts p
		WHERE p.id = $1 AND p.customer_id = $2::uuid
		  AND NOT EXISTS (SELECT 1 FROM prompt_versions pv WHERE pv.prompt_id = p.id)
	`, promptID, DefaultCustomerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete prompt", err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "prompt has versions; delete them first", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListVersions returns every version of a prompt, newest first.
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	promptID, _, _, _, ok := h.resolvePrompt(w, r, name)
	if !ok {
		return
	}
	rows, err := h.db.Query(r.Context(), `
		SELECT id::text, version, content, content_type, config::text,
			labels, commit_message, created_by, created_at
		FROM prompt_versions
		WHERE customer_id = $1::uuid AND prompt_id = $2
		ORDER BY version DESC
	`, DefaultCustomerID, promptID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query versions", err)
		return
	}
	defer rows.Close()

	out := []map[string]any{}
	for rows.Next() {
		var v promptVersionRow
		v.PromptID = promptID
		if err := rows.Scan(&v.ID, &v.Version, &v.Content, &v.ContentType, &v.Config,
			&v.Labels, &v.CommitMessage, &v.CreatedBy, &v.CreatedAt); err != nil {
			slog.Error("scan version row", "error", err)
			continue
		}
		out = append(out, v.toMap())
	}
	writeJSON(w, http.StatusOK, out)
}

type createVersionRequest struct {
	Content       string          `json:"content"`
	ContentType   string          `json:"content_type"`
	Config        json.RawMessage `json:"config"`
	Labels        []string        `json:"labels"`
	CommitMessage string          `json:"commit_message"`
	CreatedBy     string          `json:"created_by"`
}

// CreateVersion appends a new immutable version to an existing prompt.
// The version number is assigned atomically via a subquery.
func (h *Handler) CreateVersion(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req createVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body", err)
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required", nil)
		return
	}
	if req.ContentType == "" {
		req.ContentType = "text"
	}
	if len(req.Config) == 0 {
		req.Config = []byte("{}")
	}
	if req.Labels == nil {
		req.Labels = []string{}
	}

	// Security gate — same as Create. New versions of an existing
	// template are the same attack surface: persist once, replay
	// in every chat that loads this prompt + version.
	scannedContent, _, scanOk := h.scanTemplateContent(w, r.Context(), tenant.DefaultOSSID, req.Content)
	if !scanOk {
		return
	}
	req.Content = scannedContent

	promptID, _, _, _, ok := h.resolvePrompt(w, r, name)
	if !ok {
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "begin tx", err)
		return
	}
	defer tx.Rollback(r.Context())

	var version int32
	var id uuid.UUID
	var createdAt time.Time
	err = tx.QueryRow(r.Context(), `
		WITH next AS (
			SELECT COALESCE(MAX(version), 0) + 1 AS v
			FROM prompt_versions
			WHERE prompt_id = $1
		)
		INSERT INTO prompt_versions (
			prompt_id, customer_id, version, content, content_type,
			config, labels, commit_message, created_by
		)
		SELECT $1, $2::uuid, next.v, $3, $4, $5, $6, $7, $8
		FROM next
		RETURNING id, version, created_at
	`, promptID, DefaultCustomerID, req.Content, req.ContentType,
		string(req.Config), req.Labels, req.CommitMessage, req.CreatedBy).
		Scan(&id, &version, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "insert version", err)
		return
	}
	_, _ = tx.Exec(r.Context(), `UPDATE prompts SET updated_at = NOW() WHERE id = $1`, promptID)
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "commit", err)
		return
	}

	writeJSON(w, http.StatusCreated, (&promptVersionRow{
		ID:            id.String(),
		PromptID:      promptID,
		Version:       version,
		Content:       req.Content,
		ContentType:   req.ContentType,
		Config:        string(req.Config),
		Labels:        req.Labels,
		CommitMessage: req.CommitMessage,
		CreatedBy:     req.CreatedBy,
		CreatedAt:     createdAt,
	}).toMap())
}

type setLabelsRequest struct {
	Labels []string `json:"labels"`
	// Exclusive labels (e.g. "production") are cleared from every other
	// version when set. Defaults to every label the caller provides.
	Exclusive []string `json:"exclusive_labels"`
}

// SetLabels replaces the labels on a single version. Labels listed as
// exclusive (or every label if exclusive is omitted) are also removed
// from every other version of the same prompt — this is the OSS primitive
// that cloud uses to implement deploy-by-label.
func (h *Handler) SetLabels(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	versionStr := chi.URLParam(r, "version")
	v, err := strconv.Atoi(versionStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid version", err)
		return
	}
	promptID, _, _, _, ok := h.resolvePrompt(w, r, name)
	if !ok {
		return
	}

	var req setLabelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body", err)
		return
	}
	if req.Labels == nil {
		req.Labels = []string{}
	}
	// When exclusive labels aren't specified, treat every provided label
	// as exclusive — the simplest and most common case.
	exclusive := req.Exclusive
	if exclusive == nil {
		exclusive = req.Labels
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "begin tx", err)
		return
	}
	defer tx.Rollback(r.Context())

	for _, label := range exclusive {
		if label == "" {
			continue
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE prompt_versions
			SET labels = array_remove(labels, $1::text)
			WHERE customer_id = $2::uuid AND prompt_id = $3 AND version != $4 AND $1 = ANY(labels)
		`, label, DefaultCustomerID, promptID, v); err != nil {
			writeError(w, http.StatusInternalServerError, "clear exclusive label", err)
			return
		}
	}

	var row promptVersionRow
	row.PromptID = promptID
	err = tx.QueryRow(r.Context(), `
		UPDATE prompt_versions
		SET labels = $1
		WHERE customer_id = $2::uuid AND prompt_id = $3 AND version = $4
		RETURNING id::text, version, content, content_type, config::text,
			labels, commit_message, created_by, created_at
	`, req.Labels, DefaultCustomerID, promptID, v).Scan(
		&row.ID, &row.Version, &row.Content, &row.ContentType, &row.Config,
		&row.Labels, &row.CommitMessage, &row.CreatedBy, &row.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "version not found", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "update labels", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "commit", err)
		return
	}
	writeJSON(w, http.StatusOK, row.toMap())
}

// Usage returns recent traces/observations that referenced this prompt
// (by prompt_name, grouped per version). Populates the "linked traces"
// panel on the prompt detail page.
func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if h.ch == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"summary": []map[string]any{},
			"recent":  []map[string]any{},
		})
		return
	}

	summary := []map[string]any{}
	rows, err := h.ch.Conn.Query(r.Context(), `
		SELECT prompt_version, count() AS trace_count,
			avg(duration_ms) AS avg_duration_ms,
			sum(cost_cents) AS total_cost_cents,
			max(started_at) AS last_used_at
		FROM bastio.observations
		WHERE customer_id = toUUID(?) AND prompt_name = ?
		GROUP BY prompt_version
		ORDER BY prompt_version DESC
	`, DefaultCustomerID, name)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var version uint32
			var traceCount uint64
			var avgDur float64
			var cost float64
			var lastUsed time.Time
			if err := rows.Scan(&version, &traceCount, &avgDur, &cost, &lastUsed); err != nil {
				continue
			}
			summary = append(summary, map[string]any{
				"version":          version,
				"trace_count":      traceCount,
				"avg_duration_ms":  avgDur,
				"total_cost_cents": cost,
				"last_used_at":     lastUsed.UTC().Format(time.RFC3339Nano),
			})
		}
	} else {
		slog.Error("prompt usage summary", "error", err, "name", name)
	}

	recent := []map[string]any{}
	if rrows, err := h.ch.Conn.Query(r.Context(), `
		SELECT toString(trace_id), prompt_version, name, started_at, duration_ms, status
		FROM bastio.observations
		WHERE customer_id = toUUID(?) AND prompt_name = ?
		ORDER BY started_at DESC
		LIMIT 50
	`, DefaultCustomerID, name); err == nil {
		defer rrows.Close()
		for rrows.Next() {
			var traceID, spanName, status string
			var version uint32
			var startedAt time.Time
			var durationMs uint32
			if err := rrows.Scan(&traceID, &version, &spanName, &startedAt, &durationMs, &status); err != nil {
				continue
			}
			recent = append(recent, map[string]any{
				"trace_id":    traceID,
				"version":     version,
				"span_name":   spanName,
				"started_at":  startedAt.UTC().Format(time.RFC3339Nano),
				"duration_ms": durationMs,
				"status":      status,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"summary": summary,
		"recent":  recent,
	})
}

// --- internals ---

type promptVersionRow struct {
	ID            string
	PromptID      uuid.UUID
	Version       int32
	Content       string
	ContentType   string
	Config        string
	Labels        []string
	CommitMessage string
	CreatedBy     string
	CreatedAt     time.Time
}

func (v *promptVersionRow) toMap() map[string]any {
	return map[string]any{
		"id":             v.ID,
		"prompt_id":      v.PromptID.String(),
		"version":        v.Version,
		"content":        v.Content,
		"content_type":   v.ContentType,
		"config":         json.RawMessage(v.Config),
		"labels":         v.Labels,
		"commit_message": v.CommitMessage,
		"created_by":     v.CreatedBy,
		"created_at":     v.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// resolvePrompt loads the prompts row by name and writes a 404 on miss.
// Returns ok=false when the response has already been sent.
func (h *Handler) resolvePrompt(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, string, time.Time, time.Time, bool) {
	var id uuid.UUID
	var desc string
	var created, updated time.Time
	err := h.db.QueryRow(r.Context(), `
		SELECT id, description, created_at, updated_at
		FROM prompts
		WHERE customer_id = $1::uuid AND name = $2
	`, DefaultCustomerID, name).Scan(&id, &desc, &created, &updated)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "prompt not found", nil)
			return uuid.Nil, "", time.Time{}, time.Time{}, false
		}
		writeError(w, http.StatusInternalServerError, "load prompt", err)
		return uuid.Nil, "", time.Time{}, time.Time{}, false
	}
	return id, desc, created, updated, true
}

func (h *Handler) fetchVersion(r *http.Request, promptID uuid.UUID, version int32) *promptVersionRow {
	var row promptVersionRow
	row.PromptID = promptID
	err := h.db.QueryRow(r.Context(), `
		SELECT id::text, version, content, content_type, config::text,
			labels, commit_message, created_by, created_at
		FROM prompt_versions
		WHERE customer_id = $1::uuid AND prompt_id = $2 AND version = $3
	`, DefaultCustomerID, promptID, version).Scan(
		&row.ID, &row.Version, &row.Content, &row.ContentType, &row.Config,
		&row.Labels, &row.CommitMessage, &row.CreatedBy, &row.CreatedAt,
	)
	if err != nil {
		return nil
	}
	return &row
}

func (h *Handler) fetchLatest(r *http.Request, promptID uuid.UUID) *promptVersionRow {
	var row promptVersionRow
	row.PromptID = promptID
	err := h.db.QueryRow(r.Context(), `
		SELECT id::text, version, content, content_type, config::text,
			labels, commit_message, created_by, created_at
		FROM prompt_versions
		WHERE customer_id = $1::uuid AND prompt_id = $2
		ORDER BY version DESC
		LIMIT 1
	`, DefaultCustomerID, promptID).Scan(
		&row.ID, &row.Version, &row.Content, &row.ContentType, &row.Config,
		&row.Labels, &row.CommitMessage, &row.CreatedBy, &row.CreatedAt,
	)
	if err != nil {
		return nil
	}
	return &row
}

func (h *Handler) fetchByLabel(r *http.Request, promptID uuid.UUID, label string) *promptVersionRow {
	var row promptVersionRow
	row.PromptID = promptID
	err := h.db.QueryRow(r.Context(), `
		SELECT id::text, version, content, content_type, config::text,
			labels, commit_message, created_by, created_at
		FROM prompt_versions
		WHERE customer_id = $1::uuid AND prompt_id = $2 AND $3 = ANY(labels)
		ORDER BY version DESC
		LIMIT 1
	`, DefaultCustomerID, promptID, label).Scan(
		&row.ID, &row.Version, &row.Content, &row.ContentType, &row.Config,
		&row.Labels, &row.CommitMessage, &row.CreatedBy, &row.CreatedAt,
	)
	if err != nil {
		return nil
	}
	return &row
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err != nil {
		_, _ = fmt.Fprintf(w, `{"error":%q}`, fmt.Sprintf("%s: %s", msg, err.Error()))
	} else {
		_, _ = fmt.Fprintf(w, `{"error":%q}`, msg)
	}
}
