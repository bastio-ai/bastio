// Package email is the OSS email-send abstraction. Bastio uses email
// for: welcome on signup, payment receipt, magic-link login (deferred),
// activation reminders for anonymous audit prospects, password reset
// (deferred), webhook-delivery digests for SIEM-less customers.
//
// Two implementations ship today:
//
//   - SendGridClient: HTTPS POST to /v3/mail/send. No SendGrid SDK
//     dependency — the API is one endpoint with a simple JSON payload,
//     and the SDK pulls a long tail of indirect deps for marginal value.
//   - ConsoleClient: prints messages to stdout. Sane default for OSS
//     development and the OSS-self-hosted-without-email path. Operators
//     who really want no-email-at-all can use NoopClient.
//
// Cloud injects a SendGridClient via cfg; OSS dev gets ConsoleClient
// for free. The interface is small enough that customers running OSS
// can write their own implementation in <50 lines if they prefer
// Postmark / SES / Resend / Mailgun.
//
// Threading: callers MUST treat email as best-effort. Send returns an
// error; the auth + billing call sites log the error but never fail
// the user-facing operation. A signup that succeeds with a missed
// welcome email is better than a signup that fails because SendGrid
// is having a bad day.
package email

import (
	"context"
	"errors"
)

// Message is one outbound email. Keep this struct stable — every
// caller fills it directly, and changes ripple through templates and
// tests. Add fields, don't rename.
type Message struct {
	To       string  // primary recipient — exactly one in this version
	ToName   string  // optional display name; empty falls back to email-only
	Subject  string  // already-rendered, no template tokens
	HTML     string  // already-rendered HTML body; required when Text is empty
	Text     string  // plain-text body for clients that don't render HTML
	ReplyTo  string  // optional Reply-To header; empty defaults to From
	Tag      string  // categorical tag for SendGrid analytics — "welcome", "receipt", etc.
}

// Client is the send abstraction. Send returns nil on accepted+queued
// (200/202 from the upstream provider). Errors are network failures,
// invalid messages, or upstream rejections — all logged, none should
// break the calling flow.
type Client interface {
	Send(ctx context.Context, msg Message) error

	// FromAddress is the configured sender for diagnostics + headers.
	// Returning an empty string is allowed but signals misconfiguration.
	FromAddress() string
}

// ErrNotConfigured is returned by Send when a real provider is needed
// but the client has no credentials. Used by the SendGrid client when
// API key is missing — keeps the error space small and lets callers
// distinguish "config issue" from "transient network failure".
var ErrNotConfigured = errors.New("email: provider not configured")

// NoopClient discards every message and reports success. Useful for
// tests, OSS deployments that genuinely want no email, and as a
// "circuit breaker" when SendGrid is down — swap in temporarily
// while operators investigate.
type NoopClient struct {
	From string
}

// Send always returns nil — the message is dropped silently.
func (n NoopClient) Send(_ context.Context, _ Message) error { return nil }

// FromAddress returns whatever From was configured at construction.
func (n NoopClient) FromAddress() string { return n.From }
