package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// sendgridEndpoint is the only API call the SendGrid v3 mail-send
// integration makes. Override via WithBaseURL for tests.
const sendgridEndpoint = "https://api.sendgrid.com/v3/mail/send"

// SendGridClient sends mail via SendGrid's v3 mail-send endpoint with
// no SDK dependency. The API surface is small enough that maintaining
// our own client is cheaper than tracking SDK version churn.
type SendGridClient struct {
	apiKey   string
	from     string
	fromName string
	baseURL  string
	http     *http.Client
}

// NewSendGridClient builds a client. apiKey is required; missing key
// → returned client returns ErrNotConfigured on every Send so callers
// can degrade to console logging without restructuring.
func NewSendGridClient(apiKey, fromAddress, fromName string) *SendGridClient {
	return &SendGridClient{
		apiKey:   apiKey,
		from:     fromAddress,
		fromName: fromName,
		baseURL:  sendgridEndpoint,
		http:     &http.Client{Timeout: 15 * time.Second},
	}
}

// WithBaseURL is for tests — points the client at an httptest server.
// Returns the same *SendGridClient for chaining.
func (c *SendGridClient) WithBaseURL(url string) *SendGridClient {
	c.baseURL = url
	return c
}

// Send POSTs the message to SendGrid. Errors:
//
//   - ErrNotConfigured: no API key (caller can fall back to ConsoleClient)
//   - network: wrap of underlying HTTP error
//   - upstream: 4xx/5xx with the response body included for log forensics
//
// Success criterion: 200/202 with empty body. SendGrid returns 202 in
// normal operation; 200 surfaces during sandbox-mode testing.
func (c *SendGridClient) Send(ctx context.Context, msg Message) error {
	if c.apiKey == "" {
		return ErrNotConfigured
	}
	if c.from == "" {
		return fmt.Errorf("email: SendGridClient missing sender address")
	}
	if err := validateMessage(msg); err != nil {
		return err
	}

	payload := buildSendGridPayload(c.from, c.fromName, msg)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode sendgrid payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build sendgrid request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sendgrid http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("sendgrid status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}
	return nil
}

// FromAddress returns the configured sender.
func (c *SendGridClient) FromAddress() string { return c.from }

// =============================================================================
// payload + validation
// =============================================================================

// validateMessage rejects messages that would surely fail upstream —
// catching the obvious errors locally beats waiting on SendGrid's
// response and logs.
func validateMessage(msg Message) error {
	if msg.To == "" || !strings.Contains(msg.To, "@") {
		return fmt.Errorf("email: invalid recipient %q", msg.To)
	}
	if msg.Subject == "" {
		return fmt.Errorf("email: missing subject")
	}
	if msg.HTML == "" && msg.Text == "" {
		return fmt.Errorf("email: at least one of HTML or Text is required")
	}
	return nil
}

// buildSendGridPayload formats the v3 mail-send body. Two `content`
// entries (text + html) are added when both are present; SendGrid
// requires text/plain to come BEFORE text/html in the array.
func buildSendGridPayload(from, fromName string, msg Message) map[string]any {
	to := map[string]any{"email": msg.To}
	if msg.ToName != "" {
		to["name"] = msg.ToName
	}
	personalization := map[string]any{
		"to":      []any{to},
		"subject": msg.Subject,
	}

	contents := []map[string]string{}
	if msg.Text != "" {
		contents = append(contents, map[string]string{"type": "text/plain", "value": msg.Text})
	}
	if msg.HTML != "" {
		contents = append(contents, map[string]string{"type": "text/html", "value": msg.HTML})
	}

	fromObj := map[string]any{"email": from}
	if fromName != "" {
		fromObj["name"] = fromName
	}

	body := map[string]any{
		"personalizations": []any{personalization},
		"from":             fromObj,
		"content":          contents,
	}
	if msg.ReplyTo != "" {
		body["reply_to"] = map[string]any{"email": msg.ReplyTo}
	}
	if msg.Tag != "" {
		body["categories"] = []string{msg.Tag}
	}
	return body
}
