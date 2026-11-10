package gateway

import (
	"context"
	"maps"
	"sync"

	"github.com/bastio-ai/bastio/internal/llmpipeline"
	"github.com/bastio-ai/bastio/internal/security"
)

// ResponseOutcome lets a middleware that wraps the gateway handlers report
// the final outcome of a response it replaced after the handler produced
// it, so the request's trace row records what the client actually got.
//
// The handler records its trace before it returns, which is before a
// wrapper can decide. With a ResponseOutcome on the request context the
// handler hands its trace to the outcome instead, and the trace is written
// when the wrapper calls Finish, with any outcome it reported applied.
//
// Without a ResponseOutcome on the context nothing changes: traces are
// recorded immediately. With one, every WithResponseOutcome call must be
// matched by exactly one Finish (defer it), or the request's traces are
// never written. A trace the handler records after Finish (a handler still
// running when its wrapper returned) is written at once with the outcome.
//
// Block reports a replaced-with-error response; ReplaceBody reports the
// body actually delivered when the wrapper rewrote (masked, replaced) the
// answer. Both may be used together.
type ResponseOutcome struct {
	mu       sync.Mutex
	holders  int
	finished bool
	blocked  bool
	engine   string
	status   int
	body     []byte
	replaced bool
	pending  []pendingTrace
}

// pendingTrace is a held trace; omitContent records the request's
// OmitContent policy so a replaced body is not stored for it either.
type pendingTrace struct {
	in          llmpipeline.ChatTraceInput
	omitContent bool
}

type responseOutcomeKey struct{}

// WithResponseOutcome attaches a ResponseOutcome to ctx and returns it.
// When ctx already carries one (nested wrappers), that one is shared:
// traces are written when the last holder calls Finish, and a Block from
// any holder applies.
func WithResponseOutcome(ctx context.Context) (context.Context, *ResponseOutcome) {
	if o := responseOutcomeFrom(ctx); o != nil {
		o.mu.Lock()
		o.holders++
		o.mu.Unlock()
		return ctx, o
	}
	o := &ResponseOutcome{holders: 1}
	return context.WithValue(ctx, responseOutcomeKey{}, o), o
}

func responseOutcomeFrom(ctx context.Context) *ResponseOutcome {
	o, _ := ctx.Value(responseOutcomeKey{}).(*ResponseOutcome)
	return o
}

// Block reports that the response was blocked by engine and the client
// received httpStatus (403 when it is not a valid HTTP status). The
// request's trace is then recorded as status blocked, that HTTP status and
// security action block, tagged response_blocked_by=engine. Findings and
// threat rows are not invented; the wrapper records its own. Block after
// the last Finish has no effect.
func (o *ResponseOutcome) Block(engine string, httpStatus int) {
	if o == nil {
		return
	}
	if httpStatus < 100 || httpStatus > 599 {
		httpStatus = 403
	}
	if engine == "" {
		engine = "external"
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.finished {
		return
	}
	o.blocked, o.engine, o.status = true, engine, httpStatus
}

// Finish writes the traces held for this request with the reported outcome
// applied. Calls after the last holder finished are no-ops.
func (o *ResponseOutcome) Finish() {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.finished {
		o.mu.Unlock()
		return
	}
	o.holders--
	if o.holders > 0 {
		o.mu.Unlock()
		return
	}
	o.finished = true
	pending := o.pending
	o.pending = nil
	o.mu.Unlock()
	for _, p := range pending {
		llmpipeline.RecordChatTrace(o.apply(p))
	}
}

// ReplaceBody reports the response body the client actually received when
// the wrapper rewrote the handler's answer (masked or replaced it without
// blocking). The trace then stores that body instead of the handler's,
// with the usual body truncation; requests under the OmitContent policy
// store no body either way. The bytes are copied. ReplaceBody after the
// last Finish has no effect.
func (o *ResponseOutcome) ReplaceBody(body []byte) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.finished {
		return
	}
	o.body, o.replaced = append([]byte(nil), body...), true
}

// record holds a trace until Finish, or writes it now when Finish ran.
func (o *ResponseOutcome) record(in llmpipeline.ChatTraceInput, omitContent bool) {
	p := pendingTrace{in: in, omitContent: omitContent}
	o.mu.Lock()
	if !o.finished {
		o.pending = append(o.pending, p)
		o.mu.Unlock()
		return
	}
	o.mu.Unlock()
	llmpipeline.RecordChatTrace(o.apply(p))
}

func (o *ResponseOutcome) apply(p pendingTrace) llmpipeline.ChatTraceInput {
	o.mu.Lock()
	blocked, engine, status := o.blocked, o.engine, o.status
	body, replaced := o.body, o.replaced
	o.mu.Unlock()
	in := p.in
	if replaced && !p.omitContent {
		in.ResponseBody = string(body)
	}
	if !blocked {
		return in
	}
	in.Status = "blocked"
	in.HTTPStatus = uint16(status)
	scan := security.ScanResult{}
	if in.ScanResult != nil {
		scan = *in.ScanResult
	}
	scan.ShouldBlock = true
	scan.Action = security.ActionBlock
	in.ScanResult = &scan
	tags := make(map[string]string, len(in.Tags)+1)
	maps.Copy(tags, in.Tags)
	tags["response_blocked_by"] = engine
	in.Tags = tags
	return in
}
