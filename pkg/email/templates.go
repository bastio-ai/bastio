package email

import (
	"bytes"
	"fmt"
	"html"
	"strings"
	"text/template"
	"time"
)

// AuditStarted is sent immediately after POST /v1/audit/start. It
// gives the prospect their bundle download URL (one-shot, 7-day
// expiry) and the activation URL they'll click in 14 days. The
// expires field comes from the claim-token TTL (default 60 days).
func AuditStarted(toName, bundleURL, activationURL string, expires time.Time) Message {
	body := fmt.Sprintf(`Hi %s,

Your free 14-day Shadow AI Audit is set up. Two links to keep handy:

  Download your MDM bundle (one-shot, expires in 7 days):
    %s

  Activation link (works any time within %d days):
    %s

What happens next:

  1. Push the MDM bundle through your IT system (Chrome Enterprise, Intune, or Jamf).
  2. Sit tight for 14 days. The Bastio extension reports metadata only — never prompt content.
  3. We'll email your audit report when the window closes. Or click the activation link above whenever you want to claim the audit and convert to a paid Workspace.

Reply to this email if you get stuck — a real person will read it.

— The Bastio team`,
		safeName(toName), bundleURL, daysUntil(expires), activationURL)

	return Message{
		Subject: "Your Bastio Shadow AI Audit is set up",
		ToName:  toName,
		Text:    body,
		Tag:     "audit-started",
	}
}

// daysUntil rounds up to the nearest day. Used in AuditStarted's
// expiry copy ("works any time within 60 days").
func daysUntil(t time.Time) int {
	d := time.Until(t)
	if d <= 0 {
		return 0
	}
	return int((d + 23*time.Hour) / (24 * time.Hour))
}

// Welcome renders the post-signup welcome email. Plain-text only for
// MVP — HTML email design is its own polish session.
func Welcome(toName, dashboardURL string) Message {
	body := strings.Join([]string{
		"Hi " + safeName(toName) + ",",
		"",
		"Your Bastio workspace is ready. Sign in any time at " + dashboardURL + ".",
		"",
		"What happens next:",
		"  1. Generate a Shadow AI Audit MDM bundle from the Governance tab.",
		"  2. Deploy the browser extension to your team via Chrome Enterprise / Intune / Jamf.",
		"  3. After 14 days, your audit report tells you exactly which AI tools your team uses.",
		"  4. Click \"Activate Workspace\" on the report to redirect that traffic into a controlled chat surface.",
		"",
		"Reply to this email if you get stuck — a real person will read it.",
		"",
		"— The Bastio team",
	}, "\n")

	return Message{
		Subject: "Welcome to Bastio",
		ToName:  toName,
		Text:    body,
		Tag:     "welcome",
	}
}

// SubscriptionReceipt renders the post-checkout receipt. Sent on the
// `customer.subscription.created` Stripe webhook so the user gets
// confirmation independent of Stripe's own receipt mail. Includes a
// link to the customer portal.
//
// productName is the public tier display name ("Bastio Cloud Core",
// "Bastio Cloud Pro", "Bastio Workspace") so the subject + opening
// line match the plan the customer actually bought. Empty falls back
// to "Bastio" for legacy / unknown-tier rows that pre-date BAS-29.
//
// monthlyTotalLabel is a fully-formatted price string ("€29.00",
// "€75.00", "$125.00") so the caller owns currency/locale formatting
// — keeps the template currency-agnostic and lets the bastio-cloud
// caller pull the real number off the Stripe subscription.
func SubscriptionReceipt(toName, productName string, seats int, monthlyTotalLabel, portalURL string) Message {
	if productName == "" {
		productName = "Bastio"
	}
	body := fmt.Sprintf(`Hi %s,

Your %s subscription is active.

  Seats:           %d
  Monthly total:   %s
  Manage / cancel: %s

The audit data from your governance pilot has been carried over —
you'll see it the next time you visit the Governance tab.

— The Bastio team`,
		safeName(toName), productName, seats, monthlyTotalLabel, portalURL)

	return Message{
		Subject: "Your " + productName + " subscription is active",
		ToName:  toName,
		Text:    body,
		Tag:     "receipt",
	}
}

// PaymentFailed renders the dunning notice sent on the
// `invoice.payment_failed` Stripe webhook. Stripe retries the charge
// on its own schedule (Smart Retries); this email's job is to get the
// customer to fix the card before the subscription slides to past_due
// — at which point the billing gate pauses workspace writes.
//
// productName follows the SubscriptionReceipt convention (empty falls
// back to "Bastio"). amountLabel is the caller-formatted amount due
// ("€29.00"). invoiceURL is Stripe's hosted invoice page where the
// customer can pay the open invoice directly; empty omits that line
// (Stripe occasionally sends payment_failed without a hosted URL,
// e.g. when the invoice is already voided). portalURL points at the
// customer portal for updating the payment method.
func PaymentFailed(toName, productName, amountLabel, invoiceURL, portalURL string) Message {
	if productName == "" {
		productName = "Bastio"
	}
	lines := []string{
		"Hi " + safeName(toName) + ",",
		"",
		fmt.Sprintf("We couldn't collect the %s payment for your %s subscription.", amountLabel, productName),
		"",
		"We'll retry the charge automatically over the next few days. To fix",
		"it right away:",
		"",
	}
	if invoiceURL != "" {
		lines = append(lines, "  Pay the open invoice:      "+invoiceURL)
	}
	lines = append(lines,
		"  Update your payment method: "+portalURL,
		"",
		"If the payment keeps failing, your subscription moves to past-due",
		"and workspace writes pause until it's resolved. Your data and",
		"conversations stay exactly where they are either way.",
		"",
		"Reply to this email if something looks wrong — a real person will",
		"read it.",
		"",
		"— The Bastio team",
	)

	return Message{
		Subject: "Payment failed for your " + productName + " subscription",
		ToName:  toName,
		Text:    strings.Join(lines, "\n"),
		Tag:     "payment-failed",
	}
}

// AuditReady renders the email sent after the 14-day audit completes.
// activationURL is one-time-use, expires in 30 days; for the MVP
// authenticated-customer flow this is just the workspace URL.
func AuditReady(toName, activationURL string, totalEvents int, uniqueUsers int) Message {
	body := fmt.Sprintf(`Hi %s,

Your 14-day Shadow AI Audit is complete.

  Total events:  %d
  Unique users:  %d

View the full report and convert to a paid Workspace here:
%s

The activation link is valid for 30 days.

— The Bastio team`,
		safeName(toName), totalEvents, uniqueUsers, activationURL)

	return Message{
		Subject: "Your Shadow AI Audit is ready",
		ToName:  toName,
		Text:    body,
		Tag:     "audit-ready",
	}
}

// WorkspaceInvitation is sent when a workspace owner invites a new
// member. The bearer token in acceptURL is one-shot — accepting consumes
// the invitation; expired or revoked invitations 410 from the server.
// inviterEmail and companyName let the recipient confirm the invite is
// from someone they expect.
func WorkspaceInvitation(toName, inviterEmail, companyName, role, acceptURL string, expires time.Time) Message {
	from := strings.TrimSpace(inviterEmail)
	if from == "" {
		from = "the workspace owner"
	}
	company := strings.TrimSpace(companyName)
	if company == "" {
		company = "a Bastio workspace"
	}
	body := fmt.Sprintf(`Hi %s,

%s invited you to join %s on Bastio as a %s.

Accept the invitation:
%s

This link is single-use and expires in %d days. If you weren't expecting
this invitation, you can ignore this email — nothing happens until you
click the link.

— The Bastio team`,
		safeName(toName), from, company, role, acceptURL, daysUntil(expires))

	return Message{
		Subject: fmt.Sprintf("You've been invited to %s on Bastio", company),
		ToName:  toName,
		Text:    body,
		Tag:     "workspace-invitation",
	}
}

// AuditResend is sent in response to POST /v1/audit/resend. The
// activationURL embeds a freshly-rotated claim token (the original
// token's raw value is not stored, only its hash, so resends always
// rotate). Subject line is the same as AuditReady so the prospect's
// thread stays continuous if they used resend after losing the ready
// email.
func AuditResend(toName, activationURL string) Message {
	body := fmt.Sprintf(`Hi %s,

Here's a fresh activation link for your Bastio Shadow AI Audit:

%s

This link replaces any earlier activation URL — if you saved the old
one, it no longer works. Click here to claim the audit and convert to
a paid Workspace.

Reply to this email if you get stuck — a real person will read it.

— The Bastio team`,
		safeName(toName), activationURL)

	return Message{
		Subject: "Your Shadow AI Audit is ready",
		ToName:  toName,
		Text:    body,
		Tag:     "audit-resend",
	}
}

// safeName returns the name if usable, else falls back to "there".
// Avoids "Hi ," / "Hi  ," opening lines.
func safeName(n string) string {
	n = strings.TrimSpace(n)
	if n == "" {
		return "there"
	}
	return n
}

// Render is a minimal helper for callers that want template-based
// custom emails. Most internal flows use the typed builders above;
// Render is for ad-hoc admin notifications.
func Render(tmpl string, data any) (string, error) {
	t, err := template.New("email").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("exec template: %w", err)
	}
	return buf.String(), nil
}

// EscapeHTML is exposed for callers building HTML bodies inline.
// Not used by the typed builders (which are plain text), but the
// future welcome-HTML rewrite will lean on this.
func EscapeHTML(s string) string { return html.EscapeString(s) }
