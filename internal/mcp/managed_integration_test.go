package mcp_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/bastio-ai/bastio/internal/devmode"
	"github.com/bastio-ai/bastio/internal/mcp"
	"github.com/bastio-ai/bastio/internal/workspace"
	"github.com/bastio-ai/bastio/pkg/database"
	"github.com/bastio-ai/bastio/pkg/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func testTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// The integration test uses an isolated schema in an explicitly supplied test DB.
// CI supplies PostgreSQL 18. Nothing is read from a developer's DATABASE_URL.
func TestManagedMCPLifecycle(t *testing.T) {
	url := os.Getenv("MCP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("MCP_TEST_DATABASE_URL is required for PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	schema := "mcp_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = root.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = root.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	}()
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db := &database.DB{Pool: pool}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	cidA, cidB := uuid.New(), uuid.New()
	for _, cid := range []uuid.UUID{cidA, cidB} {
		if _, err = pool.Exec(ctx, `INSERT INTO customers(id,name,slug) VALUES($1,'MCP test',$2)`, cid, cid.String()); err != nil {
			t.Fatal(err)
		}
	}
	store := mcp.NewStore(pool)
	h := mcp.NewHandler(store, devmode.BuildDefaultSecurityEngine())
	router := chi.NewRouter()
	// Same routing shape as pkg/server: runtime bypasses dashboard session auth.
	router.Route("/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					cid, err := uuid.Parse(r.Header.Get("X-Test-Customer"))
					if err != nil {
						http.Error(w, "test session missing", http.StatusUnauthorized)
						return
					}
					role := workspace.RoleOwner
					if r.Header.Get("X-Test-Role") == "member" {
						role = workspace.RoleMember
					}
					next.ServeHTTP(w, r.WithContext(workspace.WithRole(tenant.WithID(r.Context(), cid), role)))
				})
			})
			r.Mount("/mcp", h.Routes(workspace.RequireRole(workspace.RoleAdmin)))
		})
		r.Post("/mcp/runtime", h.Runtime)
	})
	host := httptest.NewServer(router)
	defer host.Close()
	request := func(method, path, body string, cid uuid.UUID, role string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if cid != uuid.Nil {
			req.Header.Set("X-Test-Customer", cid.String())
		}
		req.Header.Set("X-Test-Role", role)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	created := request("POST", "/v1/mcp/servers", `{"name":"Filesystem","client":"terminal","policy":{"allowed_tools":[],"blocked_tools":["delete_file"]}}`, cidA, "")
	if created.Code != 201 {
		t.Fatalf("create HTTP %d: %s", created.Code, created.Body.String())
	}
	var enrolled struct {
		Server mcp.Server `json:"server"`
		Token  string     `json:"enrollment_token"`
	}
	if err = json.Unmarshal(created.Body.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	var storedHash string
	if err = pool.QueryRow(ctx, `SELECT token_hash FROM mcp_servers WHERE customer_id=$1 AND id=$2`, cidA, enrolled.Server.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash != testTokenHash(enrolled.Token) || strings.Contains(storedHash, "mcp_") {
		t.Fatal("enrollment stored a plaintext token")
	}
	for _, tt := range []struct {
		method, path, body string
		cid                uuid.UUID
		role               string
		status             int
	}{
		{"POST", "/v1/mcp/servers", `{}`, cidA, "member", 403},
		{"GET", "/v1/mcp/servers", "", uuid.Nil, "", 401},
		{"PUT", "/v1/mcp/servers/" + enrolled.Server.ID.String() + "/policy", `{}`, cidB, "", 404},
		{"POST", "/v1/mcp/servers/" + enrolled.Server.ID.String() + "/rotate-token", `{}`, cidB, "", 404},
		{"DELETE", "/v1/mcp/servers/" + enrolled.Server.ID.String(), "", cidB, "", 404},
		{"POST", "/v1/mcp/inspect", `{"tool_name":"delete_file","arguments":{},"policy":{"blocked_tools":["delete_file"]}}`, cidA, "member", 403},
		{"POST", "/v1/mcp/servers", `{"name":"x","client":"terminal"} {}`, cidA, "", 400},
	} {
		out := request(tt.method, tt.path, tt.body, tt.cid, tt.role)
		if out.Code != tt.status {
			t.Fatalf("%s %s HTTP %d want %d", tt.method, tt.path, out.Code, tt.status)
		}
	}
	other := request("GET", "/v1/mcp/servers", "", cidB, "")
	if other.Code != 200 || strings.Contains(other.Body.String(), "Filesystem") {
		t.Fatal("cross-tenant server list leak")
	}
	if _, err = store.Ingest(ctx, strings.Repeat("0", 64), mcp.RuntimeBatch{}); err != mcp.ErrEnrollment {
		t.Fatal("invalid enrollment was accepted")
	}

	// Real child process, real runtime endpoint, real DB metadata. The child
	// only echoes a harmless result; destructive arguments must never reach it.
	t.Setenv("BASTIO_MCP_TEST_CHILD", "1")
	t.Setenv("BASTIO_MCP_TOKEN", enrolled.Token)
	runtime, err := mcp.NewRuntimeClient(host.URL, enrolled.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err = runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"README.md"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete_file","arguments":{"path":"README.md"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"execute","arguments":{"command":"rm -rf /tmp/example"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"clock"}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	proxy := mcp.NewStdioProxy(devmode.BuildDefaultSecurityEngine(), "default", os.Args[0], []string{"-test.run=^TestMCPChildProcess$"}, strings.NewReader(input), &output, io.Discard)
	proxy.SetRuntime(runtime)
	if err = proxy.Run(ctx); err != nil {
		runtime.Close()
		t.Fatal(err)
	}
	runtime.Close()
	if !strings.Contains(output.String(), "child:read_file:no_token") || !strings.Contains(output.String(), "child:clock:no_token") || strings.Contains(output.String(), "child:delete_file") || strings.Contains(output.String(), "child:execute") {
		t.Fatalf("unexpected protocol output: %s", output.String())
	}
	events, err := store.Events(ctx, cidA, uuid.Nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 {
		t.Fatalf("events=%d want 4 requests and 2 responses", len(events))
	}
	raw, _ := json.Marshal(events)
	if bytes.Contains(raw, []byte("README.md")) || bytes.Contains(raw, []byte("rm -rf")) || bytes.Contains(raw, []byte("child:")) || bytes.Contains(raw, []byte(enrolled.Token)) {
		t.Fatal("raw content or credential leaked to event store")
	}
	outsiderEvents, err := store.Events(ctx, cidB, enrolled.Server.ID, 100)
	if err != nil || len(outsiderEvents) != 0 {
		t.Fatal("cross-tenant events leaked")
	}

	// Client-supplied server identity cannot write to another enrollment.
	forged := mcp.Event{ID: uuid.New(), ServerID: uuid.New(), ToolName: "read_file", Direction: "request", Action: "allowed", OccurredAt: time.Now()}
	if _, err = store.Ingest(ctx, storedHash, mcp.RuntimeBatch{Events: []mcp.Event{forged}}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Events(ctx, cidA, enrolled.Server.ID, 100)
	if err != nil || got[0].ServerID != enrolled.Server.ID || got[0].Categories == nil {
		t.Fatal("enrollment identity or non-null category contract broken")
	}
	servers, err := store.List(ctx, cidA)
	if err != nil || servers[0].Calls != 5 || servers[0].Blocked != 2 || servers[0].LastSeenAt == nil {
		t.Fatalf("real counts or heartbeat missing: %+v %v", servers, err)
	}
	// Replayed UUIDs do not inflate counts.
	if _, err = store.Ingest(ctx, storedHash, mcp.RuntimeBatch{Events: []mcp.Event{forged}}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE mcp_events SET created_at=NOW()-INTERVAL '31 days' WHERE customer_id=$1 AND id=$2`, cidA, forged.ID); err != nil {
		t.Fatal(err)
	}
	got, err = store.Events(ctx, cidA, enrolled.Server.ID, 100)
	if err != nil || len(got) != 6 {
		t.Fatal("event window did not hide expired metadata")
	}
	if err = store.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM mcp_events WHERE customer_id=$1 AND id=$2`, cidA, forged.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("expired metadata was not physically deleted")
	}
	if _, err = pool.Exec(ctx, `UPDATE mcp_servers SET ingest_window_requests=30 WHERE customer_id=$1 AND id=$2`, cidA, enrolled.Server.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Ingest(ctx, storedHash, mcp.RuntimeBatch{}); err != mcp.ErrRateLimit {
		t.Fatal("reporting rate limit not enforced")
	}
	rotated := request("POST", "/v1/mcp/servers/"+enrolled.Server.ID.String()+"/rotate-token", `{}`, cidA, "")
	if rotated.Code != 200 {
		t.Fatal("rotation failed")
	}
	var replacement struct {
		Token string `json:"enrollment_token"`
	}
	_ = json.Unmarshal(rotated.Body.Bytes(), &replacement)
	if _, err = store.Ingest(ctx, storedHash, mcp.RuntimeBatch{}); err != mcp.ErrEnrollment {
		t.Fatal("old token survived rotation")
	}
	revoked := request("DELETE", "/v1/mcp/servers/"+enrolled.Server.ID.String(), "", cidA, "")
	if revoked.Code != 204 {
		t.Fatal("revocation failed")
	}
	if _, err = store.Ingest(ctx, testTokenHash(replacement.Token), mcp.RuntimeBatch{}); err != mcp.ErrEnrollment {
		t.Fatal("revoked token was accepted")
	}
}
