package server

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func startTestHTTP(t *testing.T, h http.Handler) (*http.Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return srv, "http://" + ln.Addr().String()
}

// An in-flight request must complete with 200 and an uncancelled context
// while the server is draining.
func TestDrainHTTP_InFlightRequestCompletes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	ctxErr := make(chan error, 1)
	srv, url := startTestHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		ctxErr <- r.Context().Err()
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))

	type result struct {
		status int
		err    error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get(url)
		if err != nil {
			got <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, resp.Body)
		got <- result{status: resp.StatusCode}
	}()

	<-started
	drained := make(chan error, 1)
	go func() { drained <- drainHTTP(srv, 5*time.Second) }()

	// Shutdown is in progress and must wait for the handler.
	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-drained:
		t.Fatalf("drain returned before the in-flight request finished: %v", err)
	default:
	}
	close(release)

	r := <-got
	if r.err != nil || r.status != http.StatusOK {
		t.Fatalf("in-flight request: status=%d err=%v", r.status, r.err)
	}
	if err := <-ctxErr; err != nil {
		t.Fatalf("request context cancelled during drain: %v", err)
	}
	if err := <-drained; err != nil {
		t.Fatalf("drain: %v", err)
	}
}

// A request that outlives the drain bound (a long stream) is cut so
// shutdown finishes within the bound.
func TestDrainHTTP_BoundCutsLongStreams(t *testing.T) {
	started := make(chan struct{})
	handlerDone := make(chan struct{})
	srv, url := startTestHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	go func() {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}()
	<-started

	begin := time.Now()
	if err := drainHTTP(srv, 200*time.Millisecond); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if elapsed := time.Since(begin); elapsed > 2*time.Second {
		t.Fatalf("drain took %v, want about the 200ms bound", elapsed)
	}
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler was not released after force close")
	}
}
