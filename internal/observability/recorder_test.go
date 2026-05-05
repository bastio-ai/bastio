package observability

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestDrainLoop_FlushesOnSize confirms the batcher flushes as soon as
// FlushSize events accumulate, not waiting for the interval.
func TestDrainLoop_FlushesOnSize(t *testing.T) {
	src := make(chan *int, 10)
	stop := make(chan struct{})

	var flushed atomic.Int64
	flush := func(_ context.Context, rows []*int) {
		flushed.Add(int64(len(rows)))
	}

	go drainLoop(context.Background(), stop, src, 3, time.Hour, flush)

	for i := range 6 {
		v := i
		src <- &v
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && flushed.Load() < 6 {
		time.Sleep(5 * time.Millisecond)
	}
	close(stop)

	if got := flushed.Load(); got != 6 {
		t.Fatalf("flushed: want 6, got %d", got)
	}
}

// TestDrainLoop_FlushesOnInterval confirms the batcher flushes partial
// batches after the interval elapses, so tail traffic isn't stuck.
func TestDrainLoop_FlushesOnInterval(t *testing.T) {
	src := make(chan *int, 10)
	stop := make(chan struct{})

	var flushed atomic.Int64
	flush := func(_ context.Context, rows []*int) {
		flushed.Add(int64(len(rows)))
	}

	go drainLoop(context.Background(), stop, src, 1000, 50*time.Millisecond, flush)

	v := 1
	src <- &v

	time.Sleep(150 * time.Millisecond)
	close(stop)

	if got := flushed.Load(); got != 1 {
		t.Fatalf("flushed: want 1, got %d", got)
	}
}

// TestDrainLoop_FlushesOnStop confirms remaining events are drained when
// the stop channel closes.
func TestDrainLoop_FlushesOnStop(t *testing.T) {
	src := make(chan *int, 10)
	stop := make(chan struct{})

	var flushed atomic.Int64
	flush := func(_ context.Context, rows []*int) {
		flushed.Add(int64(len(rows)))
	}

	done := make(chan struct{})
	go func() {
		drainLoop(context.Background(), stop, src, 1000, time.Hour, flush)
		close(done)
	}()

	for i := range 4 {
		v := i
		src <- &v
	}

	close(stop)
	<-done

	if got := flushed.Load(); got != 4 {
		t.Fatalf("flushed: want 4, got %d", got)
	}
}

// TestRecorder_NonBlockingDrop confirms Record* never blocks and drops to
// a counter when the buffer is full. This is the safety property that
// protects the request hot path.
func TestRecorder_NonBlockingDrop(t *testing.T) {
	r := &Recorder{
		traceCh:       make(chan *TraceRecord, 2),
		analyticsCh:   make(chan *TraceRecord, 2),
		threatCh:      make(chan *ThreatEvent, 2),
		observationCh: make(chan *ObservationRecord, 2),
	}

	for range 10 {
		r.RecordTrace(&TraceRecord{})
		r.RecordAnalytics(&TraceRecord{})
		r.RecordObservation(&ObservationRecord{})
	}

	traces, _, analytics, observations := r.Dropped()
	if traces == 0 {
		t.Fatalf("expected traces to be dropped when buffer full")
	}
	if analytics == 0 {
		t.Fatalf("expected analytics to be dropped when buffer full")
	}
	if observations == 0 {
		t.Fatalf("expected observations to be dropped when buffer full")
	}
}
