package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// RuntimeClient reports bounded metadata and refreshes centrally managed tool policy.
// It never receives raw JSON-RPC input or output.
type RuntimeClient struct {
	endpoint  string
	token     string
	http      *http.Client
	events    chan Event
	dropped   atomic.Int64
	mu        sync.RWMutex
	policy    Policy
	validated time.Time
	revoked   bool
	done      chan struct{}
	cancel    context.CancelFunc
}

func NewRuntimeClient(base, token string) (*RuntimeClient, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid MCP control URL")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return nil, errors.New("MCP control URL must use HTTPS (HTTP allowed for local development)")
	}
	if len(token) != 68 || !strings.HasPrefix(token, "mcp_") {
		return nil, errors.New("valid MCP enrollment token required")
	}
	return &RuntimeClient{endpoint: strings.TrimRight(base, "/") + "/v1/mcp/runtime", token: token,
		http: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, events: make(chan Event, 1000), done: make(chan struct{})}, nil
}
func (c *RuntimeClient) Start(ctx context.Context) error {
	if err := c.send(ctx, RuntimeBatch{Events: []Event{}}); err != nil {
		return err
	}
	work, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	go c.loop(work)
	return nil
}
func (c *RuntimeClient) Record(e Event) {
	select {
	case c.events <- e:
	default:
		c.dropped.Add(1)
	}
}
func (c *RuntimeClient) Policy() (Policy, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.policy, !c.revoked && !c.validated.IsZero() && time.Since(c.validated) < 2*time.Minute
}
func (c *RuntimeClient) Close() {
	if c.cancel != nil {
		c.cancel()
		<-c.done
	}
}
func (c *RuntimeClient) loop(ctx context.Context) {
	defer close(c.done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var pending []Event
	flush := func(work context.Context) bool {
		for len(pending) < 100 {
			select {
			case e := <-c.events:
				pending = append(pending, e)
			default:
				goto ready
			}
		}
	ready:
		dropped := c.dropped.Swap(0)
		if err := c.send(work, RuntimeBatch{Events: pending, DroppedEvents: dropped}); err != nil {
			c.dropped.Add(dropped)
			slog.Warn("MCP reporting unavailable; local protection continues with cached policy", "error", err)
			return false
		}
		pending = nil
		return true
	}
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			for {
				if !flush(final) || (len(pending) == 0 && len(c.events) == 0) {
					break
				}
			}
			cancel()
			return
		case <-ticker.C:
			flush(ctx)
		}
	}
}
func (c *RuntimeClient) send(ctx context.Context, b RuntimeBatch) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return errors.New("MCP control connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		c.mu.Lock()
		c.revoked = true
		c.mu.Unlock()
		return errors.New("MCP enrollment revoked or invalid")
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("MCP control returned HTTP %d", res.StatusCode)
	}
	var body struct {
		ServerID uuid.UUID `json:"server_id"`
		Policy   Policy    `json:"policy"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body); err != nil || body.ServerID == uuid.Nil || !validPolicy(body.Policy) {
		return errors.New("MCP control returned an invalid policy")
	}
	c.mu.Lock()
	c.policy = body.Policy
	c.validated = time.Now()
	c.revoked = false
	c.mu.Unlock()
	return nil
}
