package observability

import (
	"time"

	"github.com/google/uuid"
)

// SpanBuilder is a tiny fluent helper the gateway uses to construct
// ObservationRecord values without sprinkling uuid.New() and time.Now()
// calls through the hot path. Usage:
//
//	span := NewSpan(traceID, customerID, "guardrail", "security.scan").
//	    WithEnv(env).Start()
//	// ... do the work ...
//	span.Finish("ok", "")
//	recorder.RecordObservation(span.Record())
//
// The type is deliberately flat — no lifecycle bookkeeping beyond start
// and finish timestamps. Anything richer (nested children, error wrapping)
// can be added by setting fields on the returned Record.
type SpanBuilder struct {
	rec *ObservationRecord
}

// NewSpan starts a new child span builder for the given trace/customer.
func NewSpan(traceID, customerID uuid.UUID, spanType, name string) *SpanBuilder {
	return &SpanBuilder{
		rec: &ObservationRecord{
			ID:         uuid.New(),
			TraceID:    traceID,
			CustomerID: customerID,
			Type:       spanType,
			Name:       name,
			Status:     "ok",
		},
	}
}

// WithEnv attaches the environment dimension so span-level filters stay
// consistent with trace-level filters.
func (b *SpanBuilder) WithEnv(env string) *SpanBuilder {
	b.rec.Environment = env
	return b
}

// WithParent links the span to a parent observation and sets its depth.
func (b *SpanBuilder) WithParent(parent *uuid.UUID, depth uint8) *SpanBuilder {
	b.rec.ParentID = parent
	b.rec.Depth = depth
	return b
}

// Start records the span's start time and returns the builder so the
// caller can chain or capture it.
func (b *SpanBuilder) Start() *SpanBuilder {
	b.rec.StartedAt = time.Now().UTC()
	return b
}

// Finish records the completion time and status. Safe to call once only;
// subsequent calls overwrite the previous finish.
func (b *SpanBuilder) Finish(status, errMessage string) {
	b.rec.CompletedAt = time.Now().UTC()
	b.rec.DurationMs = uint32(b.rec.CompletedAt.Sub(b.rec.StartedAt).Milliseconds())
	b.rec.Status = status
	if errMessage != "" {
		b.rec.ErrorMessage = errMessage
		b.rec.StatusMessage = errMessage
	}
}

// Record returns the underlying observation so the caller can enrich it
// further (tokens, model_parameters, tool_* fields) before handing it to
// the recorder.
func (b *SpanBuilder) Record() *ObservationRecord {
	return b.rec
}
