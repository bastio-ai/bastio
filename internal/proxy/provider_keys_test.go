package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/pkg/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProviderKeyName(t *testing.T) {
	for _, tc := range []struct {
		value    string
		required bool
		valid    bool
	}{
		{"  Production EU  ", true, true}, {"", false, true}, {"  ", true, false}, {strings.Repeat("界", 120), true, true}, {strings.Repeat("界", 121), true, false},
	} {
		_, err := providerKeyName(tc.value, tc.required)
		if (err == nil) != tc.valid {
			t.Fatalf("label validity: length=%d required=%v err=%v", len(tc.value), tc.required, err)
		}
	}
}

func TestProviderKeyLabelsTenantIsolation(t *testing.T) {
	url := os.Getenv("MCP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("MCP_TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "key_labels_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TABLE proxy_provider_keys (id UUID PRIMARY KEY,customer_id UUID NOT NULL,proxy_id UUID,provider TEXT,encrypted_key JSONB,is_default BOOLEAN,created_at TIMESTAMPTZ DEFAULT now())`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../pkg/database/migrations/042_provider_key_labels.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = pool.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewProviderKeyHandler(pool)
	owner, other := uuid.New(), uuid.New()
	call := func(method, path, body string, customer uuid.UUID) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		request = request.WithContext(tenant.WithID(request.Context(), customer))
		response := httptest.NewRecorder()
		handler.Routes().ServeHTTP(response, request)
		return response
	}
	const secret = "sk-test-only-provider-secret"
	created := call(http.MethodPost, "/", `{"provider":"openai","name":" Production EU ","key":"`+secret+`"}`, owner)
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var key ProviderKey
	if err = json.Unmarshal(created.Body.Bytes(), &key); err != nil {
		t.Fatal(err)
	}
	if key.Name != "Production EU" || strings.Contains(created.Body.String(), secret) {
		t.Fatal("create label or secret masking incorrect")
	}
	if got := call(http.MethodPatch, "/"+key.ID, `{"name":"Other owner"}`, other); got.Code != 404 {
		t.Fatalf("cross-tenant rename: %d", got.Code)
	}
	if got := call(http.MethodPatch, "/"+key.ID, `{"name":"Renamed","key":"replacement"}`, owner); got.Code != 400 {
		t.Fatalf("credential mutation accepted: %d", got.Code)
	}
	if got := call(http.MethodPatch, "/"+key.ID, `{"name":"Renamed EU"}`, owner); got.Code != 200 {
		t.Fatalf("rename: %d %s", got.Code, got.Body.String())
	}
	listed := call(http.MethodGet, "/", "", owner)
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), "Renamed EU") || strings.Contains(listed.Body.String(), secret) {
		t.Fatalf("list label or mask incorrect: %s", listed.Body.String())
	}
	var stored string
	if err = pool.QueryRow(ctx, `SELECT encrypted_key->>'key' FROM proxy_provider_keys WHERE id=$1 AND customer_id=$2`, key.ID, owner).Scan(&stored); err != nil || stored != secret {
		t.Fatal("rename changed secret")
	}
	otherList := call(http.MethodGet, "/", "", other)
	if otherList.Body.String() != "[]\n" {
		t.Fatal("provider key leaked to another tenant")
	}
}
