package detection

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/overlay"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TopicPolicyDetector runs per-customer patterns stored in the
// security_patterns table. It's how operators add company-specific
// deny-lists ("do not mention competitors", "block anything about
// regulated topic X") without shipping new code.
//
// Patterns are loaded from Postgres on construction and refreshed on
// a short interval so dashboard edits take effect quickly without
// hitting the DB on every request. Regex compilation happens once at
// refresh — hot path is pure in-memory matching.
type TopicPolicyDetector struct {
	db      *pgxpool.Pool
	tenant  func(ctx context.Context) uuid.UUID
	refresh time.Duration

	mu       sync.RWMutex
	compiled map[uuid.UUID]*compiledPolicy
	lastLoad time.Time

	// overlayCache holds compiled rules from tenant-policy-overlay
	// snapshots, keyed by overlay version ID. Populated lazily on the
	// first request that carries an overlay via context. Versions are
	// immutable so the cache never needs invalidation — a new version
	// gets a new key and old entries age out naturally if we add an
	// LRU later. For now the map grows bounded by the number of
	// distinct overlay versions the process sees, which is small.
	overlayCache sync.Map // map[uuid.UUID]*compiledPolicy
}

// compiledPolicy is the in-memory form of one customer's active
// patterns: one compiled regex + severity + action per row.
type compiledPolicy struct {
	rules []compiledTopicRule
}

type compiledTopicRule struct {
	name     string
	re       *regexp.Regexp
	severity security.Severity
	action   security.Action
	kind     string // regex | keyword — informational
}

// NewTopicPolicyDetector constructs the detector over a shared pool.
// Patterns are lazy-loaded on first Detect call. refresh controls
// how often we re-query Postgres; 30s is a reasonable default —
// operators rarely need sub-second policy propagation.
func NewTopicPolicyDetector(db *pgxpool.Pool, tenant func(context.Context) uuid.UUID, refresh time.Duration) *TopicPolicyDetector {
	if refresh == 0 {
		refresh = 30 * time.Second
	}
	if tenant == nil {
		// Tests / OSS-default fallback to a zero UUID — DetectHandler
		// overrides with a real resolver before routes are mounted.
		tenant = func(_ context.Context) uuid.UUID { return uuid.Nil }
	}
	return &TopicPolicyDetector{
		db:       db,
		tenant:   tenant,
		refresh:  refresh,
		compiled: map[uuid.UUID]*compiledPolicy{},
	}
}

func (d *TopicPolicyDetector) Name() string { return "topic_policy" }

func (d *TopicPolicyDetector) Detect(ctx context.Context, content string) ([]security.Finding, error) {
	if content == "" {
		return nil, nil
	}

	var findings []security.Finding

	// Core DB-loaded rules — unchanged behaviour, requires a tenant
	// and a DB pool. Nil DB or Nil tenant skips the core load but
	// still lets overlay rules (which come from context, not DB) run.
	if d.db != nil {
		customerID := d.tenant(ctx)
		if customerID != uuid.Nil {
			policy, err := d.load(ctx, customerID)
			if err != nil {
				// Fail open: a DB hiccup must not block production traffic.
				return nil, err
			}
			if policy != nil {
				findings = append(findings, matchRules(policy.rules, content)...)
			}
		}
	}

	// Overlay-provided additional patterns, if an overlay is attached
	// to the request context. Plays the same game: compile once per
	// overlay version, cache, match content.
	if active, ok := overlay.FromContext(ctx); ok {
		if len(active.Snapshot.AdditionalPatterns) > 0 {
			overlayPolicy := d.loadOverlayPatterns(active.Identity, active.Snapshot.AdditionalPatterns)
			findings = append(findings, matchRules(overlayPolicy.rules, content)...)
		}
	}

	return findings, nil
}

// matchRules is extracted so both the DB-loaded rule set and the
// overlay-provided rule set share one matching path.
func matchRules(rules []compiledTopicRule, content string) []security.Finding {
	var out []security.Finding
	for _, r := range rules {
		if !r.re.MatchString(content) {
			continue
		}
		match := r.re.FindString(content)
		if len(match) > 100 {
			match = match[:100] + "..."
		}
		out = append(out, security.Finding{
			ThreatType:     security.ThreatType("topic"),
			DetectorName:   "topic_policy",
			Severity:       r.severity,
			Score:          severityToScore(r.severity),
			Confidence:     0.92,
			MatchedPattern: r.name,
			MatchedContent: match,
			Action:         r.action,
			Message:        "Policy matched: " + r.name,
		})
	}
	return out
}

// loadOverlayPatterns returns a compiled policy for the overlay
// version. Cached so regex compilation happens at most once per
// version ID per process.
func (d *TopicPolicyDetector) loadOverlayPatterns(ident overlay.Identity, patterns []overlay.PatternRule) *compiledPolicy {
	// Nil version ID (no active overlay identity) is possible if a
	// caller attaches an ad-hoc snapshot without loader-provided
	// identity. Don't cache those — recompile on each call. In
	// practice the loader always provides a real UUID.
	if ident.VersionID != uuid.Nil {
		if cached, ok := d.overlayCache.Load(ident.VersionID); ok {
			return cached.(*compiledPolicy)
		}
	}

	rules := make([]compiledTopicRule, 0, len(patterns))
	for _, p := range patterns {
		re, err := compileRule(p.PatternType, p.Pattern)
		if err != nil {
			continue
		}
		rules = append(rules, compiledTopicRule{
			name:     p.Name,
			re:       re,
			severity: security.Severity(defaultSeverity(p.Severity)),
			action:   normalizeTopicAction(p.Action),
			kind:     p.PatternType,
		})
	}
	cp := &compiledPolicy{rules: rules}
	if ident.VersionID != uuid.Nil {
		d.overlayCache.Store(ident.VersionID, cp)
	}
	return cp
}

// defaultSeverity maps an empty severity string to "medium" so overlay
// patterns authored without a severity still have one.
func defaultSeverity(s string) string {
	if s == "" {
		return "medium"
	}
	return s
}

// load returns the compiled patterns for customerID, refreshing from
// the DB if the cache is stale. Read path uses a shared lock so
// concurrent Detect calls don't serialize behind each other.
func (d *TopicPolicyDetector) load(ctx context.Context, customerID uuid.UUID) (*compiledPolicy, error) {
	d.mu.RLock()
	p, ok := d.compiled[customerID]
	stale := time.Since(d.lastLoad) > d.refresh
	d.mu.RUnlock()
	if ok && !stale {
		return p, nil
	}

	rows, err := d.db.Query(ctx, `
		SELECT name, pattern_type, pattern, action, severity
		FROM security_patterns
		WHERE customer_id = $1 AND is_active = true
	`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := make([]compiledTopicRule, 0, 8)
	for rows.Next() {
		var (
			name, kind, pattern, action, severity string
		)
		if err := rows.Scan(&name, &kind, &pattern, &action, &severity); err != nil {
			continue
		}
		re, err := compileRule(kind, pattern)
		if err != nil {
			// Skip bad rules rather than crash the detector. The
			// dashboard validates on write, so this only fires when
			// migrations or manual edits produce broken regex.
			continue
		}
		rules = append(rules, compiledTopicRule{
			name:     name,
			re:       re,
			severity: security.Severity(severity),
			action:   normalizeTopicAction(action),
			kind:     kind,
		})
	}

	compiled := &compiledPolicy{rules: rules}

	d.mu.Lock()
	d.compiled[customerID] = compiled
	d.lastLoad = time.Now()
	d.mu.Unlock()
	return compiled, nil
}

// Invalidate drops the compiled cache for customerID so the next Detect
// reloads from Postgres. Called after dashboard pattern writes.
func (d *TopicPolicyDetector) Invalidate(customerID uuid.UUID) {
	if d == nil || customerID == uuid.Nil {
		return
	}
	d.mu.Lock()
	if d.compiled != nil {
		delete(d.compiled, customerID)
	}
	d.lastLoad = time.Time{}
	d.mu.Unlock()
}

// compileRule translates a (kind, pattern) row into a compiled regex.
// keyword patterns become case-insensitive word-boundary matches;
// regex patterns are compiled with case-insensitive flag only —
// authors can still opt into case sensitivity via (?-i) inline.
func compileRule(kind, pattern string) (*regexp.Regexp, error) {
	switch strings.ToLower(kind) {
	case "regex":
		return regexp.Compile("(?i)" + pattern)
	case "keyword":
		// Split on whitespace so the pattern "foo bar" becomes
		// two alternatives, matched anywhere as whole words.
		words := strings.Fields(pattern)
		if len(words) == 0 {
			return nil, errEmptyPattern
		}
		esc := make([]string, 0, len(words))
		for _, w := range words {
			esc = append(esc, regexp.QuoteMeta(w))
		}
		return regexp.Compile(`(?i)\b(?:` + strings.Join(esc, "|") + `)\b`)
	case "semantic":
		// Semantic matching requires embeddings — cloud-only. Fall back
		// to literal-string matching so OSS users still get *some*
		// value from a semantic pattern configured in the dashboard.
		return regexp.Compile(`(?i)` + regexp.QuoteMeta(pattern))
	}
	return regexp.Compile("(?i)" + regexp.QuoteMeta(pattern))
}

var errEmptyPattern = &topicErr{msg: "empty pattern"}

type topicErr struct{ msg string }

func (e *topicErr) Error() string { return e.msg }

// normalizeTopicAction maps the DB's legacy action vocabulary to the
// engine's canonical Action constants. `redact` predates the mask/
// tokenize split and is treated as mask.
func normalizeTopicAction(a string) security.Action {
	switch strings.ToLower(a) {
	case "block":
		return security.ActionBlock
	case "warn":
		return security.ActionWarn
	case "log", "log_only":
		return security.ActionLogOnly
	case "redact", "mask":
		return security.ActionMask
	}
	return security.ActionWarn
}
