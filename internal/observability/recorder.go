package observability

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/clickhouse"
)

// TraceRecord is the data written to ClickHouse for each request.
type TraceRecord struct {
	ID             uuid.UUID
	CustomerID     uuid.UUID
	ProxyID        uuid.UUID
	APIKeyID       uuid.UUID
	Method         string
	Path           string
	Provider       string
	Model          string
	StartedAt      time.Time
	CompletedAt    time.Time
	DurationMs     uint32
	InputTokens    uint32
	OutputTokens   uint32
	TotalTokens    uint32
	CostCents      float64
	Status         string
	ErrorMessage   string
	HTTPStatus     uint16
	ThreatDetected bool
	ThreatTypes    []string
	ThreatScore    float32
	SecurityAction string
	EndUserID      string
	SessionID      string
	RequestBody    string
	ResponseBody   string
	// Dimensions for slicing dashboards (Langfuse-style).
	Environment string
	Release     string
	TraceName   string
	Tags        map[string]string
}

// ObservationRecord is a single span within a trace (generation, guardrail,
// tool call, retrieval step, ...). Mirrors the bastio.observations schema
// plus the v1 enrichments (model parameters, prompt versioning, tool I/O,
// per-span cost) the dashboard's SpanTree and SpanDetailTabs render.
type ObservationRecord struct {
	ID           uuid.UUID
	TraceID      uuid.UUID
	ParentID     *uuid.UUID
	CustomerID   uuid.UUID
	Type         string // generation, span, event, tool, retrieval, embedding, guardrail, agent
	Name         string
	Depth        uint8
	StartedAt    time.Time
	CompletedAt  time.Time
	DurationMs   uint32
	InputTokens  uint32
	OutputTokens uint32
	Model        string
	Input        string
	Output       string
	Status       string
	ErrorMessage string

	ModelParameters string // JSON blob: temperature, top_p, max_tokens, ...
	PromptID        string
	PromptName      string
	PromptVersion   uint32
	ToolName        string
	ToolInput       string
	ToolOutput      string
	StatusMessage   string
	CostCents       float64

	Environment string
}

// ThreatEvent is a single threat finding ready for insertion.
type ThreatEvent struct {
	ID             uuid.UUID
	TraceID        uuid.UUID
	CustomerID     uuid.UUID
	ProxyID        uuid.UUID
	ThreatType     string
	ThreatSubtype  string
	Severity       string
	Score          float32
	Action         string
	DetectorName   string
	MatchedPattern string
	MatchedContent string
	Confidence     float32
	EndUserID      string
	IPAddress      string
	UserAgent      string
	Source         string
	DetectedAt     time.Time
}

// RecorderOptions tunes the batcher.
type RecorderOptions struct {
	// BufferSize is the per-channel queue depth. Events beyond this cap are
	// dropped rather than blocking the request path.
	BufferSize int
	// FlushSize triggers a batch flush as soon as this many events accumulate.
	FlushSize int
	// FlushInterval forces a flush after this long even if FlushSize is not
	// reached, so tail traffic still lands in ClickHouse promptly.
	FlushInterval time.Duration
}

// DefaultRecorderOptions returns the options used by NewRecorder.
func DefaultRecorderOptions() RecorderOptions {
	return RecorderOptions{
		BufferSize:    10_000,
		FlushSize:     500,
		FlushInterval: 2 * time.Second,
	}
}

// Recorder buffers trace and threat events in-process and flushes them to
// ClickHouse in batches via PrepareBatch. All Record* methods are non-blocking
// and safe for hot-path use: if the buffer is full, the event is dropped and
// a counter incremented.
type Recorder struct {
	ch   *clickhouse.CH
	opts RecorderOptions

	traceCh       chan *TraceRecord
	threatCh      chan *ThreatEvent
	analyticsCh   chan *TraceRecord
	observationCh chan *ObservationRecord

	tracesDropped       atomic.Uint64
	threatsDropped      atomic.Uint64
	analyticsDropped    atomic.Uint64
	observationsDropped atomic.Uint64

	wg     sync.WaitGroup
	stopCh chan struct{}
	once   sync.Once
}

// NewRecorder creates a Recorder with default batching options.
func NewRecorder(ch *clickhouse.CH) *Recorder {
	return NewRecorderWithOptions(ch, DefaultRecorderOptions())
}

// NewRecorderWithOptions creates a Recorder with custom batching options.
// The batcher goroutines are not started until Start is called.
func NewRecorderWithOptions(ch *clickhouse.CH, opts RecorderOptions) *Recorder {
	if opts.BufferSize <= 0 {
		opts.BufferSize = 10_000
	}
	if opts.FlushSize <= 0 {
		opts.FlushSize = 500
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 2 * time.Second
	}
	return &Recorder{
		ch:            ch,
		opts:          opts,
		traceCh:       make(chan *TraceRecord, opts.BufferSize),
		threatCh:      make(chan *ThreatEvent, opts.BufferSize),
		analyticsCh:   make(chan *TraceRecord, opts.BufferSize),
		observationCh: make(chan *ObservationRecord, opts.BufferSize),
		stopCh:        make(chan struct{}),
	}
}

// Start launches the background batcher goroutines. Call once before using
// the Recorder. Subsequent calls are no-ops.
func (r *Recorder) Start(ctx context.Context) {
	r.wg.Add(4)
	go r.drainTraces(ctx)
	go r.drainThreats(ctx)
	go r.drainAnalytics(ctx)
	go r.drainObservations(ctx)
}

// Close stops accepting new events, drains remaining buffers best-effort,
// and waits for flushes to complete or the context to expire.
func (r *Recorder) Close(ctx context.Context) error {
	r.once.Do(func() { close(r.stopCh) })

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("observability recorder: drain timed out",
			"traces_buffered", len(r.traceCh),
			"threats_buffered", len(r.threatCh),
			"analytics_buffered", len(r.analyticsCh),
		)
		return ctx.Err()
	}
	return nil
}

// Dropped returns the number of events dropped because buffers were full.
// Exposed for /metrics and tests.
func (r *Recorder) Dropped() (traces, threats, analytics, observations uint64) {
	return r.tracesDropped.Load(), r.threatsDropped.Load(), r.analyticsDropped.Load(), r.observationsDropped.Load()
}

// RecordTrace enqueues a trace for batched insertion. Non-blocking; drops
// the event if the buffer is full.
func (r *Recorder) RecordTrace(trace *TraceRecord) {
	if trace == nil {
		return
	}
	select {
	case r.traceCh <- trace:
	default:
		r.tracesDropped.Add(1)
	}
	r.RecordAnalytics(trace)
}

// RecordAnalytics enqueues a per-request analytics row. Non-blocking.
func (r *Recorder) RecordAnalytics(trace *TraceRecord) {
	select {
	case r.analyticsCh <- trace:
	default:
		r.analyticsDropped.Add(1)
	}
}

// RecordObservation enqueues a span for batched insertion. Non-blocking;
// drops the event if the buffer is full.
func (r *Recorder) RecordObservation(obs *ObservationRecord) {
	if obs == nil {
		return
	}
	select {
	case r.observationCh <- obs:
	default:
		r.observationsDropped.Add(1)
	}
}

// RecordThreatEvent enqueues a single ThreatEvent. Non-blocking.
func (r *Recorder) RecordThreatEvent(ev *ThreatEvent) {
	if ev == nil {
		return
	}
	select {
	case r.threatCh <- ev:
	default:
		r.threatsDropped.Add(1)
	}
}

// RecordThreats enqueues one ThreatEvent per finding. Non-blocking.
func (r *Recorder) RecordThreats(traceID, customerID, proxyID uuid.UUID, scanResult *security.ScanResult, endUserID, ipAddress, userAgent string) {
	if scanResult == nil {
		return
	}
	now := time.Now().UTC()
	for _, f := range scanResult.Findings {
		ev := &ThreatEvent{
			ID:             uuid.New(),
			TraceID:        traceID,
			CustomerID:     customerID,
			ProxyID:        proxyID,
			ThreatType:     string(f.ThreatType),
			ThreatSubtype:  f.SubCategory,
			Severity:       string(f.Severity),
			Score:          float32(f.Score),
			Action:         string(f.Action),
			DetectorName:   f.DetectorName,
			MatchedPattern: f.MatchedPattern,
			MatchedContent: f.MatchedContent,
			Confidence:     float32(f.Confidence),
			EndUserID:      endUserID,
			IPAddress:      ipAddress,
			UserAgent:      userAgent,
			Source:         f.Source,
			DetectedAt:     now,
		}
		select {
		case r.threatCh <- ev:
		default:
			r.threatsDropped.Add(1)
		}
	}
}

func (r *Recorder) drainTraces(ctx context.Context) {
	defer r.wg.Done()
	drainLoop(ctx, r.stopCh, r.traceCh, r.opts.FlushSize, r.opts.FlushInterval, r.flushTraces)
}

func (r *Recorder) drainAnalytics(ctx context.Context) {
	defer r.wg.Done()
	drainLoop(ctx, r.stopCh, r.analyticsCh, r.opts.FlushSize, r.opts.FlushInterval, r.flushAnalytics)
}

func (r *Recorder) drainThreats(ctx context.Context) {
	defer r.wg.Done()
	drainLoop(ctx, r.stopCh, r.threatCh, r.opts.FlushSize, r.opts.FlushInterval, r.flushThreats)
}

func (r *Recorder) drainObservations(ctx context.Context) {
	defer r.wg.Done()
	drainLoop(ctx, r.stopCh, r.observationCh, r.opts.FlushSize, r.opts.FlushInterval, r.flushObservations)
}

func (r *Recorder) flushObservations(ctx context.Context, rows []*ObservationRecord) {
	batch, err := r.ch.Conn.PrepareBatch(ctx, `
		INSERT INTO bastio.observations (
			id, trace_id, parent_id, customer_id,
			type, name, depth,
			started_at, completed_at, duration_ms,
			input_tokens, output_tokens, model,
			input, output, status, error_message,
			model_parameters, prompt_id, prompt_name, prompt_version,
			tool_name, tool_input, tool_output, status_message, cost_cents,
			environment
		)`)
	if err != nil {
		slog.Error("prepare observations batch failed", "error", err, "rows", len(rows))
		return
	}
	for _, o := range rows {
		var parent any
		if o.ParentID != nil {
			parent = o.ParentID.String()
		}
		if err := batch.Append(
			o.ID.String(), o.TraceID.String(), parent, o.CustomerID.String(),
			o.Type, o.Name, o.Depth,
			o.StartedAt, o.CompletedAt, o.DurationMs,
			o.InputTokens, o.OutputTokens, o.Model,
			o.Input, o.Output, o.Status, o.ErrorMessage,
			o.ModelParameters, o.PromptID, o.PromptName, o.PromptVersion,
			o.ToolName, o.ToolInput, o.ToolOutput, o.StatusMessage, o.CostCents,
			o.Environment,
		); err != nil {
			slog.Error("append observation row failed", "error", err, "trace_id", o.TraceID)
		}
	}
	if err := batch.Send(); err != nil {
		slog.Error("send observations batch failed", "error", err, "rows", len(rows))
		return
	}
	slog.Debug("flushed observations batch", "rows", len(rows))
}

// drain is the generic batcher loop used by each channel. It accumulates up
// to FlushSize events or waits up to FlushInterval before flushing.
func drainLoop[T any](ctx context.Context, stop <-chan struct{}, src <-chan *T, flushSize int, interval time.Duration, flush func(context.Context, []*T)) {
	buf := make([]*T, 0, flushSize)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	flushNow := func() {
		if len(buf) == 0 {
			return
		}
		// Use a detached context with a timeout so a cancelled request context
		// doesn't abort the batch write.
		fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		flush(fctx, buf)
		cancel()
		buf = buf[:0]
	}

	for {
		select {
		case <-stop:
			// Drain any remaining events, then flush.
		drainRemaining:
			for {
				select {
				case ev := <-src:
					buf = append(buf, ev)
					if len(buf) >= flushSize {
						flushNow()
					}
				default:
					break drainRemaining
				}
			}
			flushNow()
			return
		case <-ctx.Done():
			flushNow()
			return
		case ev := <-src:
			buf = append(buf, ev)
			if len(buf) >= flushSize {
				flushNow()
			}
		case <-ticker.C:
			flushNow()
		}
	}
}

func (r *Recorder) flushTraces(ctx context.Context, rows []*TraceRecord) {
	batch, err := r.ch.Conn.PrepareBatch(ctx, `
		INSERT INTO bastio.traces (
			id, customer_id, proxy_id, api_key_id,
			method, path, provider, model,
			started_at, completed_at, duration_ms,
			input_tokens, output_tokens, total_tokens, cost_cents,
			status, error_message, http_status,
			threat_detected, threat_types, threat_score, security_action,
			end_user_id, session_id,
			request_body, response_body,
			environment, release, trace_name, tags
		)`)
	if err != nil {
		slog.Error("prepare traces batch failed", "error", err, "rows", len(rows))
		return
	}
	for _, t := range rows {
		types := t.ThreatTypes
		if types == nil {
			types = []string{}
		}
		tags := t.Tags
		if tags == nil {
			tags = map[string]string{}
		}
		if err := batch.Append(
			t.ID.String(), t.CustomerID.String(), t.ProxyID.String(), t.APIKeyID.String(),
			t.Method, t.Path, t.Provider, t.Model,
			t.StartedAt, t.CompletedAt, t.DurationMs,
			t.InputTokens, t.OutputTokens, t.TotalTokens, t.CostCents,
			t.Status, t.ErrorMessage, t.HTTPStatus,
			t.ThreatDetected, types, t.ThreatScore, t.SecurityAction,
			t.EndUserID, t.SessionID,
			t.RequestBody, t.ResponseBody,
			t.Environment, t.Release, t.TraceName, tags,
		); err != nil {
			slog.Error("append traces row failed", "error", err, "trace_id", t.ID)
		}
	}
	if err := batch.Send(); err != nil {
		slog.Error("send traces batch failed", "error", err, "rows", len(rows))
		return
	}
	slog.Debug("flushed traces batch", "rows", len(rows))
}

func (r *Recorder) flushAnalytics(ctx context.Context, rows []*TraceRecord) {
	batch, err := r.ch.Conn.PrepareBatch(ctx, `
		INSERT INTO bastio.analytics_request_logs (
			customer_id, proxy_id, timestamp,
			provider, model,
			input_tokens, output_tokens, cost_cents, duration_ms,
			status, threat_detected, end_user_id, environment
		)`)
	if err != nil {
		slog.Error("prepare analytics batch failed", "error", err, "rows", len(rows))
		return
	}
	for _, t := range rows {
		if err := batch.Append(
			t.CustomerID.String(), t.ProxyID.String(), t.StartedAt,
			t.Provider, t.Model,
			t.InputTokens, t.OutputTokens, t.CostCents, t.DurationMs,
			t.Status, t.ThreatDetected, t.EndUserID, t.Environment,
		); err != nil {
			slog.Error("append analytics row failed", "error", err)
		}
	}
	if err := batch.Send(); err != nil {
		slog.Error("send analytics batch failed", "error", err, "rows", len(rows))
		return
	}
	slog.Debug("flushed analytics batch", "rows", len(rows))
}

func (r *Recorder) flushThreats(ctx context.Context, rows []*ThreatEvent) {
	batch, err := r.ch.Conn.PrepareBatch(ctx, `
		INSERT INTO bastio.security_threat_logs (
			id, trace_id, customer_id, proxy_id,
			threat_type, threat_subtype, severity, score, action_taken,
			detector_name, matched_pattern, matched_content, confidence,
			end_user_id, ip_address, user_agent, source,
			detected_at
		)`)
	if err != nil {
		slog.Error("prepare threats batch failed", "error", err, "rows", len(rows))
		return
	}
	for _, t := range rows {
		if err := batch.Append(
			t.ID.String(), t.TraceID.String(), t.CustomerID.String(), t.ProxyID.String(),
			t.ThreatType, t.ThreatSubtype, t.Severity, t.Score, t.Action,
			t.DetectorName, t.MatchedPattern, t.MatchedContent, t.Confidence,
			t.EndUserID, t.IPAddress, t.UserAgent, t.Source,
			t.DetectedAt,
		); err != nil {
			slog.Error("append threats row failed", "error", err, "trace_id", t.TraceID)
		}
	}
	if err := batch.Send(); err != nil {
		slog.Error("send threats batch failed", "error", err, "rows", len(rows))
		return
	}
	slog.Debug("flushed threats batch", "rows", len(rows))
}
