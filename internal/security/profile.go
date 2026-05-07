package security

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Profile captures the security settings the gateway needs when scanning
// a single request. It's the in-memory shape — a thin projection of
// security_profiles for hot-path use. Add fields as detectors need them;
// keep JSON serialization out of this type (it's loaded fresh per request
// or from a short-lived cache, never persisted outside Postgres).
//
// Input and Output hold the declarative detector-plus-strategy pipeline
// the engine runs via RunSteps. The flat detector fields remain for
// migrations and legacy callers; new code should read the step lists.
type Profile struct {
	CanonicalizeEnabled bool

	// NormalizeUnicode enables the Unicode preprocessing pass before
	// detectors run (NFKC, invisibles, homoglyph fold, whitespace
	// collapse). Default true — turn off only for workloads that
	// legitimately carry exotic Unicode (niche legal/linguistic tools).
	NormalizeUnicode bool
	// NormalizeDecode enables the encoding preprocessing pass
	// (base64/ROT13/leet). Default true; turn off for translation or
	// coding assistants where legitimate base64 is common.
	NormalizeDecode bool

	InjectionEnabled   bool
	InjectionThreshold float64
	JailbreakEnabled   bool
	JailbreakThreshold float64

	PIIEnabled         bool
	PIIAction          Action
	PIIScanResponse    bool
	PIIRestoreResponse bool
	PIITokenStyle      TokenStyle

	SecretsEnabled           bool
	IndirectInjectionEnabled bool
	OutputExfilEnabled       bool
	TopicPolicyEnabled       bool

	// Per-detector strategies — what happens when the step fires.
	// See migration 007 for the allowed values per detector.
	InjectionStrategy         Action
	JailbreakStrategy         Action
	SecretsStrategy           Action
	IndirectInjectionStrategy Action
	OutputExfilStrategy       Action

	Input  []Step
	Output []Step
}

// DefaultProfile returns a safe baseline used when no security_profiles
// row exists for a customer. Mask-by-default + no response scan matches
// the historical behaviour before profiles were loaded per-request.
func DefaultProfile() Profile {
	return Profile{
		CanonicalizeEnabled:      true,
		NormalizeUnicode:         true,
		NormalizeDecode:          true,
		InjectionEnabled:         true,
		InjectionThreshold:       0.72,
		JailbreakEnabled:         true,
		JailbreakThreshold:       0.6,
		PIIEnabled:               true,
		PIIAction:                ActionMask,
		PIIScanResponse:          true,
		PIIRestoreResponse:       true,
		PIITokenStyle:            TokenStyleAngle,
		SecretsEnabled:            true,
		IndirectInjectionEnabled:  true,
		OutputExfilEnabled:        true,
		TopicPolicyEnabled:        false,
		InjectionStrategy:         ActionBlock,
		JailbreakStrategy:         ActionBlock,
		SecretsStrategy:           ActionMask,
		IndirectInjectionStrategy: ActionBlock,
		OutputExfilStrategy:       ActionBlock,
		Input:                     DefaultInputSteps(),
		Output:                    DefaultOutputSteps(),
	}
}

// ProfileLookup resolves the default security profile for a customer.
// Implementations must be concurrency-safe; the gateway calls this on
// every request.
type ProfileLookup interface {
	GetDefault(ctx context.Context, customerID uuid.UUID) (*Profile, error)
}

// NewProfileLookup returns a ProfileLookup backed by the customer's
// Postgres pool. The current implementation queries on every call;
// benchmarks can add a short-lived LRU later without touching callers.
func NewProfileLookup(db *pgxpool.Pool) ProfileLookup {
	return &dbProfileLookup{db: db}
}

type dbProfileLookup struct {
	db *pgxpool.Pool
}

func (l *dbProfileLookup) GetDefault(ctx context.Context, customerID uuid.UUID) (*Profile, error) {
	if l.db == nil {
		p := DefaultProfile()
		return &p, nil
	}

	var (
		canonicalizeEnabled       bool
		normalizeUnicode          bool
		normalizeDecode           bool
		injectionEnabled          bool
		injectionThreshold        float32
		jailbreakEnabled          bool
		jailbreakThreshold        float32
		piiEnabled                bool
		piiAction                 string
		piiScanResponse           bool
		piiRestoreResponse        bool
		piiTokenStyle             string
		secretsEnabled            bool
		indirectInjectionEnabled  bool
		outputExfilEnabled        bool
		topicPolicyEnabled        bool
		injectionStrategy         string
		jailbreakStrategy         string
		secretsStrategy           string
		indirectInjectionStrategy string
		outputExfilStrategy       string
	)
	err := l.db.QueryRow(ctx, `
		SELECT canonicalize_enabled, normalize_unicode, normalize_decode,
			injection_enabled, injection_threshold,
			jailbreak_enabled, jailbreak_threshold,
			pii_enabled, pii_action, pii_scan_response, pii_restore_response, pii_token_style,
			secrets_enabled, indirect_injection_enabled, output_exfil_enabled, topic_policy_enabled,
			injection_strategy, jailbreak_strategy, secrets_strategy,
			indirect_injection_strategy, output_exfil_strategy
		FROM security_profiles
		WHERE customer_id = $1 AND name = 'default'
		LIMIT 1
	`, customerID).Scan(
		&canonicalizeEnabled, &normalizeUnicode, &normalizeDecode,
		&injectionEnabled, &injectionThreshold,
		&jailbreakEnabled, &jailbreakThreshold,
		&piiEnabled, &piiAction, &piiScanResponse, &piiRestoreResponse, &piiTokenStyle,
		&secretsEnabled, &indirectInjectionEnabled, &outputExfilEnabled, &topicPolicyEnabled,
		&injectionStrategy, &jailbreakStrategy, &secretsStrategy,
		&indirectInjectionStrategy, &outputExfilStrategy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			p := DefaultProfile()
			return &p, nil
		}
		return nil, err
	}

	action := Action(piiAction)
	// Back-compat for rows that predate the 003 migration — 'redact' is
	// the old alias for 'mask'. Migrations should have rewritten these
	// but the normalization here is cheap insurance.
	if action == ActionRedact {
		action = ActionMask
	}
	style := TokenStyle(piiTokenStyle)
	if style != TokenStyleAngle && style != TokenStyleCurly {
		style = TokenStyleAngle
	}

	p := Profile{
		CanonicalizeEnabled:       canonicalizeEnabled,
		NormalizeUnicode:          normalizeUnicode,
		NormalizeDecode:           normalizeDecode,
		InjectionEnabled:          injectionEnabled,
		InjectionThreshold:        float64(injectionThreshold),
		JailbreakEnabled:          jailbreakEnabled,
		JailbreakThreshold:        float64(jailbreakThreshold),
		PIIEnabled:                piiEnabled,
		PIIAction:                 action,
		PIIScanResponse:           piiScanResponse,
		PIIRestoreResponse:        piiRestoreResponse,
		PIITokenStyle:             style,
		SecretsEnabled:            secretsEnabled,
		IndirectInjectionEnabled:  indirectInjectionEnabled,
		OutputExfilEnabled:        outputExfilEnabled,
		TopicPolicyEnabled:        topicPolicyEnabled,
		InjectionStrategy:         normalizeStrategy(injectionStrategy, ActionBlock),
		JailbreakStrategy:         normalizeStrategy(jailbreakStrategy, ActionBlock),
		SecretsStrategy:           normalizeStrategy(secretsStrategy, ActionMask),
		IndirectInjectionStrategy: normalizeStrategy(indirectInjectionStrategy, ActionBlock),
		OutputExfilStrategy:       normalizeStrategy(outputExfilStrategy, ActionBlock),
	}
	// Row predates the step-list columns — synthesize the same pipeline
	// the flat fields would have produced so the gateway's behavior is
	// identical until migrations populate dedicated columns.
	p.Input, p.Output = StepsFromLegacyProfile(p)
	return &p, nil
}

// normalizeStrategy coerces a DB-stored strategy string into a valid
// Action constant, falling back to the detector-specific default when
// the column is empty (pre-migration rows) or carries an unexpected
// value. The CHECK constraint in migration 007 prevents the latter
// from reaching here under normal operation; the guard is defense in
// depth for schema drift during upgrades.
func normalizeStrategy(s string, fallback Action) Action {
	if s == "" {
		return fallback
	}
	a := Action(s)
	switch a {
	case ActionBlock, ActionWarn, ActionMask, ActionTokenize, ActionLogOnly:
		return a
	}
	return fallback
}

type profileCtxKey struct{}

// WithProfile attaches a profile to ctx so the response path (restore,
// response scan) can read the same settings the request path used.
func WithProfile(ctx context.Context, p *Profile) context.Context {
	if p == nil {
		return ctx
	}
	return context.WithValue(ctx, profileCtxKey{}, p)
}

// ProfileFromContext returns the profile stored on ctx. Callers that
// need a value when none was attached should fall back to DefaultProfile.
func ProfileFromContext(ctx context.Context) (*Profile, bool) {
	p, ok := ctx.Value(profileCtxKey{}).(*Profile)
	if !ok || p == nil {
		return nil, false
	}
	return p, true
}
