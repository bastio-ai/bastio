package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Webhook is a customer-defined HTTP endpoint that fires on high-severity
// governance events. v1 supports Slack, Microsoft Teams, and raw JSON formats.
// Delivery is best-effort with exponential backoff retry; failures are
// audit-logged so IT can spot misconfigured webhooks without blocking ingest.
type Webhook struct {
	ID         uuid.UUID
	CustomerID uuid.UUID
	Name       string
	URL        string
	Format     WebhookFormat
	Trigger    WebhookTrigger
	CreatedAt  time.Time
	LastFiredAt *time.Time
	LastError   string
}

type WebhookFormat string

const (
	FormatSlack   WebhookFormat = "slack"
	FormatTeams   WebhookFormat = "teams"
	FormatRawJSON WebhookFormat = "raw_json"
)

type WebhookTrigger string

const (
	TriggerSeverityHigh WebhookTrigger = "severity:high"
	TriggerOverride     WebhookTrigger = "action:overridden"
	TriggerAny          WebhookTrigger = "any"
)

// WebhookStore persists customer webhook configs in PG.
type WebhookStore struct {
	pool *pgxpool.Pool
}

func NewWebhookStore(pool *pgxpool.Pool) *WebhookStore {
	return &WebhookStore{pool: pool}
}

// List returns active webhooks for a customer.
func (s *WebhookStore) List(ctx context.Context, customerID uuid.UUID) ([]Webhook, error) {
	const q = `
SELECT id, customer_id, name, url, format, trigger, created_at, last_fired_at, last_error
FROM governance_webhooks
WHERE customer_id = $1 AND deleted_at IS NULL
ORDER BY created_at ASC`
	rows, err := s.pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	defer rows.Close()
	out := []Webhook{}
	for rows.Next() {
		var w Webhook
		var fmtStr, trigStr string
		if err := rows.Scan(
			&w.ID, &w.CustomerID, &w.Name, &w.URL, &fmtStr, &trigStr,
			&w.CreatedAt, &w.LastFiredAt, &w.LastError,
		); err != nil {
			return nil, err
		}
		w.Format = WebhookFormat(fmtStr)
		w.Trigger = WebhookTrigger(trigStr)
		out = append(out, w)
	}
	return out, nil
}

// Create inserts a new webhook config.
func (s *WebhookStore) Create(ctx context.Context, customerID uuid.UUID, name, url string, format WebhookFormat, trigger WebhookTrigger) (*Webhook, error) {
	if !validFormat(format) {
		return nil, fmt.Errorf("invalid format: %s", format)
	}
	if !validTrigger(trigger) {
		return nil, fmt.Errorf("invalid trigger: %s", trigger)
	}
	const q = `
INSERT INTO governance_webhooks (customer_id, name, url, format, trigger)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, customer_id, name, url, format, trigger, created_at, last_fired_at, last_error`
	row := s.pool.QueryRow(ctx, q, customerID, name, url, string(format), string(trigger))
	w := &Webhook{}
	var fmtStr, trigStr string
	if err := row.Scan(&w.ID, &w.CustomerID, &w.Name, &w.URL, &fmtStr, &trigStr, &w.CreatedAt, &w.LastFiredAt, &w.LastError); err != nil {
		return nil, fmt.Errorf("insert webhook: %w", err)
	}
	w.Format = WebhookFormat(fmtStr)
	w.Trigger = WebhookTrigger(trigStr)
	return w, nil
}

// Delete soft-deletes a webhook (sets deleted_at).
func (s *WebhookStore) Delete(ctx context.Context, customerID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE governance_webhooks SET deleted_at = NOW() WHERE id = $1 AND customer_id = $2 AND deleted_at IS NULL`, id, customerID)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("webhook not found")
	}
	return nil
}

// recordFire updates last_fired_at and last_error after a delivery attempt.
func (s *WebhookStore) recordFire(ctx context.Context, id uuid.UUID, lastErr string) {
	if _, err := s.pool.Exec(ctx, `UPDATE governance_webhooks SET last_fired_at = NOW(), last_error = $2 WHERE id = $1`, id, lastErr); err != nil {
		slog.Warn("update webhook last_fired_at", "id", id, "err", err)
	}
}

func validFormat(f WebhookFormat) bool {
	switch f {
	case FormatSlack, FormatTeams, FormatRawJSON:
		return true
	}
	return false
}

func validTrigger(t WebhookTrigger) bool {
	switch t {
	case TriggerSeverityHigh, TriggerOverride, TriggerAny:
		return true
	}
	return false
}

// LookupByID fetches a single webhook (used by delivery worker).
func (s *WebhookStore) LookupByID(ctx context.Context, id uuid.UUID) (*Webhook, error) {
	const q = `
SELECT id, customer_id, name, url, format, trigger, created_at, last_fired_at, last_error
FROM governance_webhooks
WHERE id = $1 AND deleted_at IS NULL`
	row := s.pool.QueryRow(ctx, q, id)
	w := &Webhook{}
	var fmtStr, trigStr string
	if err := row.Scan(&w.ID, &w.CustomerID, &w.Name, &w.URL, &fmtStr, &trigStr, &w.CreatedAt, &w.LastFiredAt, &w.LastError); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("webhook not found")
		}
		return nil, err
	}
	w.Format = WebhookFormat(fmtStr)
	w.Trigger = WebhookTrigger(trigStr)
	return w, nil
}

// ============================================================
// Delivery
// ============================================================

// WebhookDeliverer fires webhooks asynchronously. v1 uses a goroutine with
// retry+backoff. v1.1 will move to River for transactional enqueue + durable
// retry across server restarts.
type WebhookDeliverer struct {
	store *WebhookStore
	httpc *http.Client
}

func NewWebhookDeliverer(store *WebhookStore) *WebhookDeliverer {
	return &WebhookDeliverer{
		store: store,
		httpc: &http.Client{Timeout: 10 * time.Second},
	}
}

// FireForEvent enqueues delivery for every webhook that matches the trigger.
// Non-blocking — returns immediately, deliveries happen on goroutines.
func (d *WebhookDeliverer) FireForEvent(ctx context.Context, customerID uuid.UUID, ev EventPayload) {
	hooks, err := d.store.List(ctx, customerID)
	if err != nil {
		slog.Warn("list webhooks for delivery", "err", err)
		return
	}
	for _, w := range hooks {
		if !triggerMatches(w.Trigger, ev) {
			continue
		}
		go d.deliverWithRetry(w, ev)
	}
}

func triggerMatches(t WebhookTrigger, ev EventPayload) bool {
	switch t {
	case TriggerAny:
		return true
	case TriggerSeverityHigh:
		return ev.Severity == SeverityHigh
	case TriggerOverride:
		return ev.Action == ActionOverridden
	}
	return false
}

func (d *WebhookDeliverer) deliverWithRetry(w Webhook, ev EventPayload) {
	body, err := formatPayload(w.Format, ev)
	if err != nil {
		d.store.recordFire(context.Background(), w.ID, "format error: "+err.Error())
		return
	}

	backoffs := []time.Duration{0, 2 * time.Second, 8 * time.Second}
	var lastErr error
	for _, b := range backoffs {
		if b > 0 {
			time.Sleep(b)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		err := d.deliverOnce(ctx, w, body)
		cancel()
		if err == nil {
			d.store.recordFire(context.Background(), w.ID, "")
			return
		}
		lastErr = err
	}
	if lastErr != nil {
		d.store.recordFire(context.Background(), w.ID, lastErr.Error())
		slog.Warn("webhook delivery failed after retries", "id", w.ID, "err", lastErr)
	}
}

// deliverOnce performs a single HTTP POST attempt. Returns nil on 2xx, an
// error on transport failure or non-2xx status. Used by both the goroutine
// retry loop and the River worker.
func (d *WebhookDeliverer) deliverOnce(ctx context.Context, w Webhook, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Bastio-Governance/0.1")
	resp, err := d.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("http %d", resp.StatusCode)
}

func formatPayload(format WebhookFormat, ev EventPayload) ([]byte, error) {
	switch format {
	case FormatSlack:
		return json.Marshal(slackPayload(ev))
	case FormatTeams:
		return json.Marshal(teamsPayload(ev))
	case FormatRawJSON:
		return json.Marshal(ev)
	}
	return nil, fmt.Errorf("unknown format: %s", format)
}

func slackPayload(ev EventPayload) map[string]any {
	emoji := ":warning:"
	if ev.Severity == SeverityHigh {
		emoji = ":no_entry:"
	}
	rules := "—"
	if len(ev.RuleIDs) > 0 {
		rules = ev.RuleIDs[0]
		if len(ev.RuleIDs) > 1 {
			rules = fmt.Sprintf("%s (+%d more)", rules, len(ev.RuleIDs)-1)
		}
	}
	return map[string]any{
		"text": fmt.Sprintf("%s *Bastio Governance — %s severity*\nDomain: `%s`\nUser: `%s`\nRule: `%s`\nAction: `%s`",
			emoji, ev.Severity, ev.SourceDomain, truncateID(ev.UserID), rules, ev.Action),
	}
}

func teamsPayload(ev EventPayload) map[string]any {
	color := "FFAA00"
	if ev.Severity == SeverityHigh {
		color = "D70015"
	}
	rules := "—"
	if len(ev.RuleIDs) > 0 {
		rules = ev.RuleIDs[0]
	}
	return map[string]any{
		"@type":      "MessageCard",
		"@context":   "https://schema.org/extensions",
		"themeColor": color,
		"summary":    "Bastio Governance event",
		"title":      fmt.Sprintf("Bastio Governance — %s severity", ev.Severity),
		"sections": []map[string]any{
			{
				"facts": []map[string]string{
					{"name": "Domain", "value": ev.SourceDomain},
					{"name": "User", "value": truncateID(ev.UserID)},
					{"name": "Rule", "value": rules},
					{"name": "Action", "value": string(ev.Action)},
				},
			},
		},
	}
}

func truncateID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "…"
}
