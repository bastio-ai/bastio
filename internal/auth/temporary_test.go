package auth

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/pkg/cache"
	"github.com/bastio-ai/bastio/pkg/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTemporaryGatewayKeyScopedHiddenAndCleanedOnCancellation(t *testing.T) {
	databaseURL := os.Getenv("MCP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MCP_TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "temporary_keys_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TABLE environments(id UUID PRIMARY KEY,customer_id UUID,name TEXT,kind TEXT,description TEXT,created_at TIMESTAMPTZ DEFAULT now());CREATE UNIQUE INDEX env_name ON environments(customer_id,lower(name));
 CREATE TABLE gateway_api_keys(id UUID PRIMARY KEY,customer_id UUID,name TEXT,key_hash TEXT UNIQUE,key_prefix TEXT,scopes TEXT[],rate_limit_rpm INTEGER,expires_at TIMESTAMPTZ,environment_id UUID NOT NULL REFERENCES environments(id),allow_environment_override BOOLEAN DEFAULT FALSE,is_active BOOLEAN DEFAULT TRUE,last_used_at TIMESTAMPTZ,created_at TIMESTAMPTZ DEFAULT now());`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../pkg/database/migrations/043_ephemeral_gateway_test_keys.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = pool.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	var redisCache *cache.Cache
	if redisURL := os.Getenv("GATEWAY_TEST_REDIS_URL"); redisURL != "" {
		redisCache, err = cache.New(ctx, redisURL)
		if err != nil {
			t.Fatal(err)
		}
		defer redisCache.Close()
	}
	service := NewService(pool, redisCache)
	cid, pid := uuid.New(), uuid.New()
	for _, cancelRequest := range []bool{false, true} {
		caller, cancel := context.WithCancel(ctx)
		var secret string
		err = service.WithTemporaryGatewayKey(caller, cid, pid, func(key string) {
			secret = key
			var scopes []string
			var expires time.Time
			var ephemeral bool
			var environment string
			if err := pool.QueryRow(ctx, `SELECT k.scopes,k.expires_at,k.is_ephemeral,e.name FROM gateway_api_keys k JOIN environments e ON e.id=k.environment_id WHERE key_hash=$1`, HashAPIKey(key)).Scan(&scopes, &expires, &ephemeral, &environment); err != nil {
				t.Fatal(err)
			}
			if len(scopes) != 1 || scopes[0] != "proxy:"+pid.String() || !ephemeral || environment != "production" || time.Until(expires) > 2*time.Minute || time.Until(expires) < time.Minute {
				t.Fatal("temporary credential scope or lifetime incorrect")
			}
			req := httptest.NewRequest("GET", "/", nil)
			req = req.WithContext(tenant.WithID(req.Context(), cid))
			out := httptest.NewRecorder()
			NewAPIKeyHandler(pool).List(out, req)
			if out.Code != 200 || strings.Contains(out.Body.String(), "Dashboard gateway connection test") {
				t.Fatalf("ephemeral key shown in application list: %d %s", out.Code, out.Body.String())
			}
			if redisCache != nil {
				if _, err := service.Authenticate(ctx, key); err != nil {
					t.Fatal(err)
				}
			}
			if cancelRequest {
				cancel()
			}
		})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		var count int
		pool.QueryRow(ctx, "SELECT count(*) FROM gateway_api_keys WHERE key_hash=$1", HashAPIKey(secret)).Scan(&count)
		if count != 0 {
			t.Fatal("temporary key survived cleanup")
		}
		if redisCache != nil {
			if _, err := service.Authenticate(ctx, secret); err == nil {
				t.Fatal("removed key still authenticates from cache")
			}
		}
	}
	if err = service.WithTemporaryGatewayKey(ctx, uuid.Nil, pid, func(string) { t.Fatal("missing tenant accepted") }); err == nil {
		t.Fatal("missing tenant not rejected")
	}
}
