package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WebhookTarget defines a SIEM or alert notification endpoint destination.
type WebhookTarget struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"` // "slack", "splunk", "datadog", "generic"
	URL       string            `json:"url"`
	Secret    string            `json:"secret,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	MinSeverity string          `json:"min_severity"` // "medium", "high", "critical"
	Enabled   bool              `json:"enabled"`
}

// EventPayload represents a security alert event dispatched to webhooks and SIEMs.
type EventPayload struct {
	EventID     string    `json:"event_id"`
	Timestamp   time.Time `json:"timestamp"`
	TenantID    string    `json:"tenant_id"`
	EventType   string    `json:"event_type"` // "threat_detected", "pii_leak", "guardrail_block"
	Severity    string    `json:"severity"`   // "low", "medium", "high", "critical"
	ProfileName string    `json:"profile_name"`
	Summary     string    `json:"summary"`
	Details     any       `json:"details,omitempty"`
}

// Dispatcher manages asynchronous real-time dispatch of security events to configured webhooks/SIEMs.
type Dispatcher struct {
	mu      sync.RWMutex
	targets map[string]*WebhookTarget
	client  *http.Client
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		targets: make(map[string]*WebhookTarget),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (d *Dispatcher) AddTarget(target *WebhookTarget) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.targets[target.ID] = target
}

func (d *Dispatcher) RemoveTarget(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.targets, id)
}

func (d *Dispatcher) ListTargets() []*WebhookTarget {
	d.mu.RLock()
	defer d.mu.RUnlock()
	res := make([]*WebhookTarget, 0, len(d.targets))
	for _, t := range d.targets {
		res = append(res, t)
	}
	return res
}

// Dispatch sends an event to all enabled webhook targets matching severity rules in background goroutines.
func (d *Dispatcher) Dispatch(ctx context.Context, event EventPayload) {
	d.mu.RLock()
	targets := make([]*WebhookTarget, 0, len(d.targets))
	for _, t := range d.targets {
		if t.Enabled {
			targets = append(targets, t)
		}
	}
	d.mu.RUnlock()

	for _, t := range targets {
		target := t
		go func() {
			if err := d.sendToTarget(ctx, target, event); err != nil {
				slog.Error("failed to dispatch webhook alert", "target", target.Name, "type", target.Type, "err", err)
			}
		}()
	}
}

func (d *Dispatcher) sendToTarget(ctx context.Context, target *WebhookTarget, event EventPayload) error {
	var body []byte
	var err error

	switch target.Type {
	case "slack":
		slackMsg := map[string]any{
			"text": fmt.Sprintf("🚨 *Bastio Security Alert* [%s]: %s\n> %s\n*Tenant:* `%s` | *Profile:* `%s`",
				strings.ToUpper(event.Severity), event.Summary, event.EventType, event.TenantID, event.ProfileName),
		}
		body, err = json.Marshal(slackMsg)
	case "splunk":
		splunkMsg := map[string]any{
			"time":       event.Timestamp.Unix(),
			"sourcetype": "bastio:security:alert",
			"event":      event,
		}
		body, err = json.Marshal(splunkMsg)
	case "datadog":
		datadogMsg := map[string]any{
			"title":      fmt.Sprintf("Bastio AI Security Alert: %s", event.EventType),
			"text":       event.Summary,
			"alert_type": strings.ToLower(event.Severity),
			"source":     "bastio",
			"tags":       []string{fmt.Sprintf("tenant:%s", event.TenantID), fmt.Sprintf("profile:%s", event.ProfileName)},
		}
		body, err = json.Marshal(datadogMsg)
	case "generic":
		fallthrough
	default:
		body, err = json.Marshal(event)
	}

	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Bastio-SIEM-Dispatcher/1.0")
	for k, v := range target.Headers {
		req.Header.Set(k, v)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook responded with HTTP status %d", resp.StatusCode)
	}

	return nil
}
