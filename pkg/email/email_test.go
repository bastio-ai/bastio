package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNoopClientNeverSends(t *testing.T) {
	t.Parallel()
	c := NoopClient{From: "noreply@bastio.com"}
	if err := c.Send(context.Background(), Message{
		To: "alice@example.com", Subject: "x", Text: "y",
	}); err != nil {
		t.Fatalf("noop should never error: %v", err)
	}
	if c.FromAddress() != "noreply@bastio.com" {
		t.Fatalf("FromAddress mismatch")
	}
}

func TestConsoleClientSendIsNoFail(t *testing.T) {
	t.Parallel()
	c := NewConsoleClient("noreply@bastio.com")
	if err := c.Send(context.Background(), Message{
		To: "alice@example.com", Subject: "Welcome", Text: "Hello",
	}); err != nil {
		t.Fatalf("console send should not error: %v", err)
	}
}

func TestSendGridClientNoKey(t *testing.T) {
	t.Parallel()
	c := NewSendGridClient("", "from@bastio.com", "Bastio")
	err := c.Send(context.Background(), Message{
		To: "alice@example.com", Subject: "x", Text: "y",
	})
	if err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestSendGridClientValidation(t *testing.T) {
	t.Parallel()
	c := NewSendGridClient("sk_test", "from@bastio.com", "Bastio")

	cases := []struct {
		name string
		msg  Message
	}{
		{"missing to", Message{Subject: "x", Text: "y"}},
		{"bad email", Message{To: "no-at-sign", Subject: "x", Text: "y"}},
		{"missing subject", Message{To: "a@b.com", Text: "y"}},
		{"missing both bodies", Message{To: "a@b.com", Subject: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := c.Send(context.Background(), tc.msg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSendGridClientSuccess(t *testing.T) {
	t.Parallel()

	var receivedBody []byte
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		buf, _ := io.ReadAll(r.Body)
		receivedBody = buf
		w.WriteHeader(http.StatusAccepted) // Stripe-style 202
	}))
	defer srv.Close()

	c := NewSendGridClient("sk_secret_123", "from@bastio.com", "Bastio").
		WithBaseURL(srv.URL)

	err := c.Send(context.Background(), Message{
		To:      "alice@example.com",
		ToName:  "Alice",
		Subject: "Welcome",
		Text:    "Hello, world.",
		HTML:    "<p>Hello, world.</p>",
		Tag:     "welcome",
	})
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if receivedAuth != "Bearer sk_secret_123" {
		t.Fatalf("authorization mismatch: %q", receivedAuth)
	}

	var payload map[string]any
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}

	// Spot checks against SendGrid v3 mail-send shape.
	if from, ok := payload["from"].(map[string]any); !ok || from["email"] != "from@bastio.com" {
		t.Fatalf("from mismatch: %v", payload["from"])
	}
	pers, ok := payload["personalizations"].([]any)
	if !ok || len(pers) != 1 {
		t.Fatalf("personalizations malformed")
	}
	cats, ok := payload["categories"].([]any)
	if !ok || len(cats) != 1 || cats[0] != "welcome" {
		t.Fatalf("category tag missing or wrong: %v", payload["categories"])
	}
}

func TestSendGridClientUpstreamError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"message":"bad sender"}]}`))
	}))
	defer srv.Close()

	c := NewSendGridClient("sk_test", "from@bastio.com", "Bastio").WithBaseURL(srv.URL)
	err := c.Send(context.Background(), Message{
		To: "a@b.com", Subject: "x", Text: "y",
	})
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected 400 error, got %v", err)
	}
}

func TestPayloadOmitsHTMLWhenAbsent(t *testing.T) {
	t.Parallel()
	payload := buildSendGridPayload("from@bastio.com", "Bastio", Message{
		To: "a@b.com", Subject: "x", Text: "text only",
	})
	contents, ok := payload["content"].([]map[string]string)
	if !ok || len(contents) != 1 {
		t.Fatalf("expected single content entry, got %v", payload["content"])
	}
	if contents[0]["type"] != "text/plain" {
		t.Fatalf("expected text/plain, got %q", contents[0]["type"])
	}
}

func TestPayloadIncludesBothBodies(t *testing.T) {
	t.Parallel()
	payload := buildSendGridPayload("from@bastio.com", "Bastio", Message{
		To: "a@b.com", Subject: "x", Text: "text", HTML: "<p>html</p>",
	})
	contents := payload["content"].([]map[string]string)
	if len(contents) != 2 {
		t.Fatalf("expected 2 content entries, got %d", len(contents))
	}
	// SendGrid requires text/plain to come BEFORE text/html.
	if contents[0]["type"] != "text/plain" || contents[1]["type"] != "text/html" {
		t.Fatalf("ordering wrong: %v", contents)
	}
}

func TestWelcomeMessageShape(t *testing.T) {
	t.Parallel()
	msg := Welcome("Alice", "https://bastio.com")
	if msg.Subject != "Welcome to Bastio" {
		t.Fatalf("subject: %q", msg.Subject)
	}
	if !strings.Contains(msg.Text, "Hi Alice") {
		t.Fatalf("greeting missing: %q", msg.Text)
	}
	if !strings.Contains(msg.Text, "https://bastio.com") {
		t.Fatalf("dashboard URL missing")
	}
	if msg.Tag != "welcome" {
		t.Fatalf("tag: %q", msg.Tag)
	}
}

func TestWelcomeFallsBackOnEmptyName(t *testing.T) {
	t.Parallel()
	msg := Welcome("", "https://bastio.com")
	if !strings.Contains(msg.Text, "Hi there") {
		t.Fatalf("expected fallback greeting, got %q", msg.Text)
	}
}

func TestSubscriptionReceiptShape(t *testing.T) {
	t.Parallel()
	msg := SubscriptionReceipt("Alice", "Bastio Cloud Pro", 5, "€75.00", "https://bastio.com/portal")
	if !strings.Contains(msg.Text, "5") || !strings.Contains(msg.Text, "€75.00") {
		t.Fatalf("missing seats or total: %q", msg.Text)
	}
	if !strings.Contains(msg.Text, "Bastio Cloud Pro") || !strings.Contains(msg.Subject, "Bastio Cloud Pro") {
		t.Fatalf("product name missing from text/subject: subject=%q text=%q", msg.Subject, msg.Text)
	}
	if msg.Tag != "receipt" {
		t.Fatalf("tag: %q", msg.Tag)
	}
}

func TestRenderTemplate(t *testing.T) {
	t.Parallel()
	out, err := Render("Hi {{.Name}}, you have {{.N}} seats.", map[string]any{
		"Name": "Alice", "N": 5,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out != "Hi Alice, you have 5 seats." {
		t.Fatalf("got %q", out)
	}
}
