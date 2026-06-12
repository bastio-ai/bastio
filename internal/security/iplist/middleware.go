package iplist

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/netip"

	"github.com/bastio-ai/bastio/internal/security"
)

// MiddlewareConfig controls what happens on a threat-list hit.
type MiddlewareConfig struct {
	// Block returns 403 to listed clients. Default false: annotate
	// only — the verdict rides the request context and the gateway
	// security scan folds it into the trace as a warn finding.
	Block bool
}

// Middleware returns a chi-compatible middleware that checks the
// client IP against the manager's active threat lists.
//
// It expects to run after chi's RealIP middleware (mounted on the root
// router), which rewrites r.RemoteAddr from X-Forwarded-For /
// X-Real-IP — so RemoteAddr is the true client address by the time we
// parse it. In the server wiring it is mounted on the gateway proxy
// group only, after API-key auth and rate limiting, matching the
// documented ordering (RequestID → RealIP → ... → Auth → RateLimit →
// context middleware → handler). Health endpoints live outside that
// group; the path guard below is defense in depth so a future remount
// can never block them either.
func Middleware(m *Manager, cfg MiddlewareConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m == nil || isHealthPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			addr, ok := clientAddr(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			verdict := m.Lookup(addr)
			if !verdict.Listed {
				next.ServeHTTP(w, r)
				return
			}
			if cfg.Block {
				slog.Info("iplist: blocked listed client ip",
					"ip", addr.String(),
					"lists", verdict.Lists,
					"path", r.URL.Path,
				)
				writeBlocked(w, verdict)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithVerdict(r.Context(), verdict)))
		})
	}
}

// isHealthPath guards liveness/readiness/metrics endpoints from ever
// being blocked, regardless of where the middleware is mounted.
func isHealthPath(p string) bool {
	switch p {
	case "/health", "/healthz", "/ready", "/readyz", "/metrics":
		return true
	}
	return false
}

// clientAddr parses the client IP from r.RemoteAddr. After chi's
// RealIP middleware the value may be either host:port (direct
// connections) or a bare IP (rewritten from proxy headers); both forms
// are handled. IPv4-mapped IPv6 is unmapped to match the compiled sets.
func clientAddr(r *http.Request) (netip.Addr, bool) {
	if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		return ap.Addr().Unmap(), true
	}
	if a, err := netip.ParseAddr(r.RemoteAddr); err == nil {
		return a.Unmap(), true
	}
	return netip.Addr{}, false
}

// writeBlocked emits the 403 body for block mode. Mirrors the gateway's
// existing blocked-response shape (top-level "error" + threat_types) so
// SDKs don't need a separate error path for IP blocks.
func writeBlocked(w http.ResponseWriter, v Verdict) {
	body, _ := json.Marshal(struct {
		Error       string   `json:"error"`
		ThreatTypes []string `json:"threat_types"`
		Lists       []string `json:"lists"`
	}{
		Error:       "request blocked by security policy",
		ThreatTypes: []string{string(security.ThreatIPReputation)},
		Lists:       v.Lists,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write(body)
}
