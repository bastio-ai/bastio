// Package iplist provides IP threat-list support for the gateway:
// public reputation feeds (FireHOL level1, Tor exit nodes) compiled
// into in-memory range sets with O(log n) lookups, refreshed on a
// jittered background cadence and swapped atomically.
//
// OSS scope is public feeds only — GeoIP/MaxMind enrichment is a
// cloud-side concern and deliberately absent here.
package iplist

import (
	"context"
	"net/netip"
	"strings"

	"github.com/bastio-ai/bastio/internal/security"
)

// Verdict is the result of looking up a client address against the
// active threat lists.
type Verdict struct {
	// Listed is true when the address appears on at least one list.
	Listed bool
	// Lists holds the names of every list containing the address,
	// e.g. "firehol_level1", "tor_exits".
	Lists []string
}

// Finding renders the verdict as a synthetic security finding so the
// gateway can fold it into the same ScanResult that content detectors
// populate — one threat row per finding through the existing
// recordTrace path. Action is warn: in annotate mode the request
// proceeds; in block mode the middleware already returned 403 before
// any scan ran, so this finding never needs to flip a block decision.
func (v Verdict) Finding() security.Finding {
	lists := strings.Join(v.Lists, ", ")
	return security.Finding{
		ThreatType:     security.ThreatIPReputation,
		DetectorName:   "iplist",
		Severity:       security.SeverityMedium,
		Score:          0.6,
		Confidence:     1.0, // list membership is binary
		SubCategory:    "reputation.listed",
		MatchedPattern: strings.Join(v.Lists, ","),
		Action:         security.ActionWarn,
		Message:        "client ip listed on threat feeds: " + lists,
		Source:         "public ip threat feeds",
	}
}

// Provider fetches one threat list. Implementations parse their feed
// into prefixes; single addresses are represented as /32 or /128.
type Provider interface {
	// Name is the canonical list name surfaced in Verdict.Lists
	// (e.g. "firehol_level1").
	Name() string
	// Fetch downloads and parses the feed.
	Fetch(ctx context.Context) ([]netip.Prefix, error)
}

// ctxKey tags the verdict the middleware attaches to a request context.
type ctxKey struct{}

// WithVerdict attaches a lookup verdict to ctx. Called by the
// middleware in annotate mode; downstream code (the gateway security
// scan) reads it back via VerdictFrom.
func WithVerdict(ctx context.Context, v Verdict) context.Context {
	return context.WithValue(ctx, ctxKey{}, v)
}

// VerdictFrom returns the verdict stored on ctx by the middleware.
// ok is false when the request never passed the middleware or the
// client IP was not listed.
func VerdictFrom(ctx context.Context) (Verdict, bool) {
	v, ok := ctx.Value(ctxKey{}).(Verdict)
	return v, ok
}
