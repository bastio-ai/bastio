package governance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PolicyStore reads and writes per-customer governance policy rows.
// Defaults are baked into the handler; rows here override per customer.
type PolicyStore struct {
	pool *pgxpool.Pool
}

func NewPolicyStore(pool *pgxpool.Pool) *PolicyStore {
	return &PolicyStore{pool: pool}
}

// CustomerPolicy mirrors the governance_policies table shape.
type CustomerPolicy struct {
	CustomerID         uuid.UUID
	SeverityLow        string
	SeverityMedium     string
	SeverityHigh       string
	CustomKeywords     []string
	CustomRegexPacks   []RegexPack
	RedirectTarget     *RedirectTargetPG
	OverrideEnabled    bool
	PseudonymizePII    bool
}

// RegexPack is a customer-defined named regex for Layer 3 detection.
// Server validates the pattern compiles + is non-catastrophic before push.
type RegexPack struct {
	ID       string `json:"id"`
	Pattern  string `json:"pattern"`
	Severity string `json:"severity"`
	Label    string `json:"label"`
}

// RedirectTargetPG is the JSON shape stored in PG.
type RedirectTargetPG struct {
	URL              string `json:"url"`
	Label            string `json:"label"`
	OpenInNewTab     bool   `json:"open_in_new_tab"`
	CarryOverSupport bool   `json:"carry_over_supported"`
}

// Get returns the customer's policy or a default-zero row if none exists.
func (s *PolicyStore) Get(ctx context.Context, customerID uuid.UUID) (*CustomerPolicy, error) {
	const q = `
SELECT customer_id, severity_low, severity_medium, severity_high,
       custom_keywords, custom_regex_packs, redirect_target,
       override_enabled, pseudonymize_pii
FROM governance_policies
WHERE customer_id = $1`
	row := s.pool.QueryRow(ctx, q, customerID)
	p := &CustomerPolicy{}
	var keywordsJSON, regexJSON, redirectJSON []byte
	if err := row.Scan(
		&p.CustomerID, &p.SeverityLow, &p.SeverityMedium, &p.SeverityHigh,
		&keywordsJSON, &regexJSON, &redirectJSON,
		&p.OverrideEnabled, &p.PseudonymizePII,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return defaultPolicy(customerID), nil
		}
		return nil, fmt.Errorf("get policy: %w", err)
	}
	if len(keywordsJSON) > 0 {
		_ = json.Unmarshal(keywordsJSON, &p.CustomKeywords)
	}
	if len(regexJSON) > 0 {
		_ = json.Unmarshal(regexJSON, &p.CustomRegexPacks)
	}
	if len(redirectJSON) > 0 && string(redirectJSON) != "null" {
		p.RedirectTarget = &RedirectTargetPG{}
		_ = json.Unmarshal(redirectJSON, p.RedirectTarget)
	}
	return p, nil
}

// Upsert inserts or updates the customer's policy row.
func (s *PolicyStore) Upsert(ctx context.Context, p *CustomerPolicy) error {
	if p.CustomerID == uuid.Nil {
		return errors.New("customer_id required")
	}
	if !validAction(p.SeverityLow) || !validAction(p.SeverityMedium) || !validAction(p.SeverityHigh) {
		return errors.New("invalid severity action")
	}
	if p.CustomKeywords == nil {
		p.CustomKeywords = []string{}
	}
	if p.CustomRegexPacks == nil {
		p.CustomRegexPacks = []RegexPack{}
	}
	keywordsJSON, _ := json.Marshal(p.CustomKeywords)
	regexJSON, _ := json.Marshal(p.CustomRegexPacks)
	var redirectJSON any
	if p.RedirectTarget != nil {
		redirectJSON, _ = json.Marshal(p.RedirectTarget)
	}

	const q = `
INSERT INTO governance_policies (customer_id, severity_low, severity_medium, severity_high,
                                 custom_keywords, custom_regex_packs, redirect_target,
                                 override_enabled, pseudonymize_pii, updated_at)
VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, NOW())
ON CONFLICT (customer_id) DO UPDATE SET
    severity_low = EXCLUDED.severity_low,
    severity_medium = EXCLUDED.severity_medium,
    severity_high = EXCLUDED.severity_high,
    custom_keywords = EXCLUDED.custom_keywords,
    custom_regex_packs = EXCLUDED.custom_regex_packs,
    redirect_target = EXCLUDED.redirect_target,
    override_enabled = EXCLUDED.override_enabled,
    pseudonymize_pii = EXCLUDED.pseudonymize_pii,
    updated_at = NOW()`
	_, err := s.pool.Exec(ctx, q, p.CustomerID,
		p.SeverityLow, p.SeverityMedium, p.SeverityHigh,
		string(keywordsJSON), string(regexJSON), redirectJSON,
		p.OverrideEnabled, p.PseudonymizePII)
	if err != nil {
		return fmt.Errorf("upsert policy: %w", err)
	}
	return nil
}

func defaultPolicy(customerID uuid.UUID) *CustomerPolicy {
	return &CustomerPolicy{
		CustomerID:       customerID,
		SeverityLow:      "log",
		SeverityMedium:   "warn",
		SeverityHigh:     "block_redirect",
		CustomKeywords:   []string{},
		CustomRegexPacks: []RegexPack{},
		OverrideEnabled:  true,
		PseudonymizePII:  false,
	}
}

func validAction(a string) bool {
	switch a {
	case "log", "warn", "block_redirect":
		return true
	}
	return false
}

// ============================================================
// Domain overrides (per-customer additions to the tracked AI list)
// ============================================================

type DomainOverride struct {
	ID         uuid.UUID
	CustomerID uuid.UUID
	Domain     string
	Label      string
}

type DomainStore struct {
	pool *pgxpool.Pool
}

func NewDomainStore(pool *pgxpool.Pool) *DomainStore {
	return &DomainStore{pool: pool}
}

func (s *DomainStore) List(ctx context.Context, customerID uuid.UUID) ([]DomainOverride, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, customer_id, domain, label
FROM governance_domain_overrides
WHERE customer_id = $1
ORDER BY domain ASC`, customerID)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	defer rows.Close()
	out := []DomainOverride{}
	for rows.Next() {
		var d DomainOverride
		if err := rows.Scan(&d.ID, &d.CustomerID, &d.Domain, &d.Label); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (s *DomainStore) Add(ctx context.Context, customerID uuid.UUID, domain, label string) (*DomainOverride, error) {
	const q = `
INSERT INTO governance_domain_overrides (customer_id, domain, label)
VALUES ($1, $2, $3)
ON CONFLICT (customer_id, domain) DO UPDATE SET label = EXCLUDED.label
RETURNING id, customer_id, domain, label`
	row := s.pool.QueryRow(ctx, q, customerID, domain, label)
	d := &DomainOverride{}
	if err := row.Scan(&d.ID, &d.CustomerID, &d.Domain, &d.Label); err != nil {
		return nil, fmt.Errorf("add domain: %w", err)
	}
	return d, nil
}

func (s *DomainStore) Delete(ctx context.Context, customerID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM governance_domain_overrides WHERE id = $1 AND customer_id = $2`, id, customerID)
	if err != nil {
		return fmt.Errorf("delete domain: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("not found")
	}
	return nil
}
