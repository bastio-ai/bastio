package server

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metrics is the single process-wide instrumentation instance. Exposed via
// the /metrics endpoint; wrap handlers with metrics.Middleware.
var metrics = newMetricsOnce()

var newMetricsOnce = sync.OnceValue(func() *metricsInstance {
	m := &metricsInstance{
		requestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "bastio",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total HTTP requests handled, by route and status.",
		}, []string{"route", "method", "status"}),
		requestDurationSeconds: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "bastio",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency, by route and method.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"route", "method"}),
		inFlight: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "bastio",
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "In-flight HTTP requests.",
		}),
	}

	// Go runtime and process collectors are already registered on the
	// default registerer at package init; don't re-register them here.
	return m
})

type metricsInstance struct {
	requestsTotal          *prometheus.CounterVec
	requestDurationSeconds *prometheus.HistogramVec
	inFlight               prometheus.Gauge
}

// Middleware records request counts, latencies, and in-flight gauge for
// every request. Route label uses the matched chi pattern (e.g. /v1/proxies)
// so cardinality stays bounded.
func (m *metricsInstance) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.inFlight.Inc()
		defer m.inFlight.Dec()

		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		route := "unmatched"
		if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
			route = rctx.RoutePattern()
		}
		status := strconv.Itoa(ww.Status())
		m.requestsTotal.WithLabelValues(route, r.Method, status).Inc()
		m.requestDurationSeconds.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
	})
}
