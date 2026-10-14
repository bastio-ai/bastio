package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/pkg/clickhouse"
	"github.com/bastio-ai/bastio/pkg/tenant"
	"github.com/google/uuid"
)

// PG-only CI checks that the real handlers return trace IDs. With an isolated
// ClickHouse, also verify those exact IDs and all content-free records persisted.
func workspaceMCPTraceTestRecorder(t *testing.T, h *Handler, cid uuid.UUID) func(*httptest.ResponseRecorder) {
	t.Helper()
	var ch *clickhouse.CH
	if endpoint := os.Getenv("WORKSPACE_TEST_CLICKHOUSE_URL"); endpoint != "" {
		var err error
		ch, err = clickhouse.New(context.Background(), endpoint)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ch.Close() })
	}
	recorder := observability.NewRecorder(ch)
	h.SetObservabilityRecorder(recorder)
	if ch != nil {
		recorder.Start(context.Background())
	}
	var lock sync.Mutex
	ids := map[string]bool{}
	t.Cleanup(func() {
		if len(ids) == 0 {
			t.Error("no MCP trace IDs returned by handlers")
		}
		if ch == nil {
			return
		}
		if err := recorder.Close(context.Background()); err != nil {
			t.Error(err)
			return
		}
		var count, content uint64
		if err := ch.Conn.QueryRow(context.Background(), `SELECT count(),countIf(request_body!='' OR response_body!='' OR total_tokens!=0 OR cost_cents!=0) FROM bastio.traces WHERE customer_id=? AND provider='mcp'`, cid).Scan(&count, &content); err != nil {
			t.Error(err)
			return
		}
		if count != uint64(len(ids)) || content != 0 {
			t.Errorf("persisted MCP traces=%d expected=%d content/usage=%d", count, len(ids), content)
		}
		if err := ch.Conn.QueryRow(context.Background(), `SELECT count(),countIf(input_tokens!=0 OR output_tokens!=0 OR cost_cents!=0) FROM bastio.analytics_request_logs WHERE customer_id=? AND provider='mcp'`, cid).Scan(&count, &content); err != nil {
			t.Error(err)
		}
		if count != uint64(len(ids)) || content != 0 {
			t.Error("tool analytics duplicated or charged model usage")
		}
		observe := observability.NewHandler(ch).Routes()
		for id := range ids {
			var rows, spans uint64
			if err := ch.Conn.QueryRow(context.Background(), `SELECT count() FROM bastio.traces WHERE customer_id=? AND id=toUUID(?)`, cid, id).Scan(&rows); err != nil {
				t.Error(err)
			}
			for _, identity := range []uuid.UUID{cid, uuid.New()} {
				for _, path := range []string{"/traces/" + id, "/traces?search=" + url.QueryEscape(id)} {
					req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(tenant.WithID(context.Background(), identity))
					out := httptest.NewRecorder()
					observe.ServeHTTP(out, req)
					if identity == cid {
						if out.Code != 200 {
							t.Errorf("trace API returned %d: %s", out.Code, out.Body.String())
						}
					} else if path == "/traces/"+id {
						if out.Code != 404 {
							t.Error("foreign tenant could read tool trace")
						}
					} else {
						var result []json.RawMessage
						if json.Unmarshal(out.Body.Bytes(), &result) != nil || len(result) != 0 {
							t.Error("trace search crossed tenant boundary")
						}
					}
				}
			}
			if rows != 1 {
				t.Errorf("returned trace ID persisted %d times", rows)
			}
			if err := ch.Conn.QueryRow(context.Background(), `SELECT count(),countIf(input!='' OR output!='' OR tool_input!='' OR tool_output!='' OR cost_cents!=0) FROM bastio.observations WHERE customer_id=? AND trace_id=toUUID(?)`, cid, id).Scan(&spans, &content); err != nil {
				t.Error(err)
			}
			if spans == 0 || content != 0 {
				t.Errorf("tool spans missing or retained content: spans=%d content=%d", spans, content)
			}
		}
		if err := ch.Conn.QueryRow(context.Background(), `SELECT countIf(matched_content!='' OR matched_pattern!='') FROM bastio.security_threat_logs WHERE customer_id=? AND trace_id IN (SELECT id FROM bastio.traces WHERE customer_id=? AND provider='mcp')`, cid, cid).Scan(&content); err != nil {
			t.Error(err)
		}
		if content != 0 {
			t.Error("MCP findings retained matched content")
		}
	})
	return func(w *httptest.ResponseRecorder) {
		if id := w.Header().Get("X-Bastio-Trace-Id"); id != "" {
			if _, err := uuid.Parse(id); err != nil {
				t.Error("invalid trace ID")
			}
			lock.Lock()
			defer lock.Unlock()
			if ids[id] {
				t.Error("trace ID reused for another request")
			}
			ids[id] = true
		}
	}
}
