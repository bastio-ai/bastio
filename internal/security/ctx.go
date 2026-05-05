package security

import "context"

// ctxKeyRole tags the message role on a context so role-aware detectors
// (indirect-injection, output-exfiltration) can apply different
// thresholds to user input vs. tool output vs. retrieved documents
// without changing the base Detector interface.
type ctxKeyRole struct{}

// Recognized role constants. Matches DetectMessage.Role on the wire.
// `user` is the operator's typed input — the default when role is
// absent. `tool`, `retrieval`, and `memory` all originate outside
// the operator and are treated as less trusted.
const (
	RoleUser      = "user"
	RoleSystem    = "system"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleRetrieval = "retrieval"
	RoleMemory    = "memory"
)

// WithRole attaches the calling message's role to ctx.
func WithRole(ctx context.Context, role string) context.Context {
	if role == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyRole{}, role)
}

// RoleFromContext returns the role stored on ctx. Empty string when
// the context carries no role — detectors should treat that as
// equivalent to "user" (the conservative default).
func RoleFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRole{}).(string)
	return v
}

// IsUntrustedRole reports whether the role originates outside the
// operator's direct input. Indirect-injection / exfil detectors use
// this to tighten thresholds for content the user didn't type.
func IsUntrustedRole(role string) bool {
	switch role {
	case RoleTool, RoleRetrieval, RoleMemory:
		return true
	}
	return false
}
