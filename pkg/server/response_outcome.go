package server

import (
	"context"

	"github.com/bastio-ai/bastio/internal/gateway"
)

// ResponseOutcome is the extension point for a middleware that wraps the
// gateway handlers and can replace a response after the handler produced
// it. Attach one with WithResponseOutcome before calling the handler; the
// handler then holds the request's trace until Finish, and a Block reported
// in between is recorded on it (status blocked, the reported HTTP status,
// security action block, tag response_blocked_by=<engine>).
//
//	ctx, outcome := server.WithResponseOutcome(r.Context())
//	defer outcome.Finish()
//	next.ServeHTTP(captured, r.WithContext(ctx))
//	if replaced { outcome.Block("my-engine", http.StatusForbidden) }
//	if masked { outcome.ReplaceBody(deliveredBody) } // trace stores what was sent
//
// Block(engine, httpStatus) records the request as blocked; ReplaceBody(b)
// makes the trace store the body actually delivered (truncated as usual,
// never stored under the OmitContent policy). Both may be combined, and
// both are ignored after the last Finish.
//
// Every WithResponseOutcome must be matched by exactly one Finish, or the
// request's traces are not written. See gateway.ResponseOutcome.
type ResponseOutcome = gateway.ResponseOutcome

// WithResponseOutcome attaches a ResponseOutcome to ctx (or joins the one
// already there) and returns it.
func WithResponseOutcome(ctx context.Context) (context.Context, *ResponseOutcome) {
	return gateway.WithResponseOutcome(ctx)
}
