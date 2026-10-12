package server

import (
	"context"
	"github.com/bastio-ai/bastio/internal/workspace"
)

// TemporaryWorkspaceChat lets provider decorators honor the storage decision.
func TemporaryWorkspaceChat(ctx context.Context) bool { return workspace.TemporaryChat(ctx) }

// WithTemporaryWorkspaceChat tags in-process requests; HTTP clients cannot
// override the workspace policy that the chat handler applies.
func WithTemporaryWorkspaceChat(ctx context.Context, temporary bool) context.Context {
	return workspace.WithTemporaryChat(ctx, temporary)
}
