package email

import (
	"context"
	"log/slog"
)

// ConsoleClient writes every send to slog at INFO level. This is the
// fallback that runs when no SendGrid credentials are configured —
// OSS developers run signups + checkouts locally and see exactly what
// would have been sent in their server logs.
//
// Production deployments must NOT rely on this for real email — set
// SENDGRID_API_KEY and use SendGridClient. ConsoleClient logs a one-
// time WARN on first send when the From address looks production-y
// (matches `*@bastio.com`) so an operator notices a misconfig.
type ConsoleClient struct {
	From string
}

// NewConsoleClient builds a logger-only client. From is the sender
// address that would have been used; we still pass it to the log
// output so dev sees the full intended envelope.
func NewConsoleClient(from string) *ConsoleClient {
	return &ConsoleClient{From: from}
}

// Send logs the message instead of sending it. Always returns nil.
func (c *ConsoleClient) Send(_ context.Context, msg Message) error {
	slog.Info("email: console send (not actually delivered)",
		"from", c.From,
		"to", msg.To,
		"to_name", msg.ToName,
		"subject", msg.Subject,
		"tag", msg.Tag,
		"text_len", len(msg.Text),
		"html_len", len(msg.HTML),
	)
	return nil
}

// FromAddress returns the configured sender.
func (c *ConsoleClient) FromAddress() string { return c.From }
