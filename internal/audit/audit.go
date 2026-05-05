// Package audit implements the anonymous Shadow AI Audit wedge: a
// prospect with no Bastio account fills out a form, gets an MDM bundle
// + a 60-day activation link in their inbox, runs the audit for 14
// days, then converts to a paid Workspace by clicking the activation
// link.
//
// This package is OSS — bundle generation is OSS code (governance) and
// the wedge motion is part of what every Bastio deployer should be
// able to run. Cloud reads `pending_audits` directly to verify claim
// tokens; no Go-package dependency from cloud to here.
//
// File layout:
//
//	audit.go       — Service constructor + Routes()
//	types.go       — PendingAudit + request/response shapes + Config
//	store.go       — pgxpool CRUD on pending_audits, customers, installations
//	start.go       — POST /v1/audit/start handler
//	bundle.go      — GET /v1/audit/{id}/bundle.zip handler (one-shot)
//	resend.go      — POST /v1/audit/resend handler (email-keyed claim-token rotation)
//	ready_sweep.go — daily River sweep emails the 14-day audit-ready notice
package audit

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastio-ai/bastio/internal/governance"
	"github.com/bastio-ai/bastio/pkg/email"
)

// Service bundles audit dependencies. Construct once at server
// startup; pass Routes() to server.WithMount.
type Service struct {
	store   *Store
	cfg     Config
	gov     *governance.Handler // bundle generator + installation manager
	emailer email.Client        // optional; nil falls back to URL-in-response
}

// New builds a Service. The governance handler is required because
// bundle generation lives there; cmd/server passes its existing one.
// emailer is optional — when nil the activation URL is returned in
// the response body and the operator does the send manually.
func New(pool *pgxpool.Pool, gov *governance.Handler, emailer email.Client, cfg Config) *Service {
	return &Service{
		store:   NewStore(pool),
		cfg:     cfg.WithDefaults(),
		gov:     gov,
		emailer: emailer,
	}
}

// Routes returns the public /v1/audit subtree. Both endpoints are
// anonymous and rate-limited at the chi router level (the caller
// installs the limiter — see cmd/server wiring). One-shot tokens +
// 60-day expiry are the actual access controls.
func (s *Service) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/start", s.handleStart)
	r.Post("/resend", s.handleResend)
	r.Get("/{id}/bundle.zip", s.handleBundleDownload)
	return r
}
