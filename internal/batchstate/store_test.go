package batchstate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/database"
	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func batchTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("MCP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable MCP_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(root.Close)
	name := "batch_vault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ident := pgx.Identifier{name}.Sanitize()
	if _, err = root.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.Exec(context.Background(), "DROP DATABASE "+ident+" WITH (FORCE)") })
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = (&database.DB{Pool: pool}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	cipher, err := encryption.New("")
	if err != nil {
		t.Fatal(err)
	}
	return New(pool, cipher, time.Hour)
}

func TestBatchVaultIsolationRestartExpiryAndQuarantine(t *testing.T) {
	store := batchTestStore(t)
	ctx := context.Background()
	cid, other := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{cid, other} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO customers(id,name,slug) VALUES($1,'batch test',$2)`, id, id.String()); err != nil {
			t.Fatal(err)
		}
	}
	key := Key{Customer: cid, Scope: "provider-credential-scope", Kind: "tokens", ID: "manifest/item"}
	tm := security.NewRequestTokenMap(security.TokenStyleAngle)
	token := tm.Add("email", "person@example.test")
	if err := store.SaveTokens(ctx, key, tm); err != nil {
		t.Fatal(err)
	}
	var encrypted string
	if err := store.pool.QueryRow(ctx, `SELECT encrypted_payload::text FROM gateway_batch_state WHERE customer_id=$1 AND scope=$2 AND kind=$3 AND resource_id=$4`, cid, key.Scope, key.Kind, key.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "person@example.test") || strings.Contains(encrypted, token) {
		t.Fatal("plaintext token vault persisted")
	}
	restarted := New(store.pool, store.cipher, time.Hour)
	restored, err := restarted.LoadTokens(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if value, n := restored.Restore(token); value != "person@example.test" || n != 1 {
		t.Fatal("restart lost reversible state")
	}
	for _, wrong := range []Key{{Customer: other, Scope: key.Scope, Kind: key.Kind, ID: key.ID}, {Customer: cid, Scope: "other-credential", Kind: key.Kind, ID: key.ID}, {Customer: cid, Scope: key.Scope, Kind: key.Kind, ID: "other/item"}} {
		value, err := restarted.LoadTokens(ctx, wrong)
		if err != nil || value != nil {
			t.Fatal("vault crossed isolation boundary")
		}
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, e := restarted.Once(ctx, Key{Customer: cid, Scope: key.Scope, Kind: "recorded", ID: "job/item"})
			if e != nil {
				t.Error(e)
			}
			if won {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("result analytics not idempotent")
	}
	upload := Key{Customer: cid, Scope: key.Scope, Kind: "upload", ID: uuid.NewString()}
	if err = store.CreateUpload(ctx, upload, json.RawMessage(`{"size":6}`), 6); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Append(ctx, upload, 2, []byte("bad")); !errors.Is(err, ErrOffset) {
		t.Fatal("wrong offset accepted")
	}
	if _, err = store.Append(ctx, upload, 0, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	data, err := restarted.Chunk(ctx, upload, 0)
	if err != nil || string(data) != "secret" {
		t.Fatal("chunk restart failed")
	}
	if err = store.pool.QueryRow(ctx, `SELECT encrypted_payload::text FROM gateway_batch_chunks WHERE customer_id=$1 AND scope=$2 AND resource_id=$3 AND chunk_offset=0`, cid, key.Scope, upload.ID).Scan(&encrypted); err != nil || strings.Contains(encrypted, "secret") {
		t.Fatal("upload quarantine not encrypted")
	}
	if leased, e := store.Lease(ctx, upload); e != nil || !leased {
		t.Fatal("lease unavailable")
	}
	if leased, e := restarted.Lease(ctx, upload); e != nil || leased {
		t.Fatal("concurrent finalize accepted")
	}
	if err = store.Complete(ctx, upload, json.RawMessage(`{"response":{"ok":true}}`)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM gateway_batch_chunks WHERE customer_id=$1 AND scope=$2 AND resource_id=$3`, cid, key.Scope, upload.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("finalized quarantine not deleted")
	}
	if _, err = store.pool.Exec(ctx, `UPDATE gateway_batch_state SET expires_at=now()-interval '1 second' WHERE customer_id=$1`, cid); err != nil {
		t.Fatal(err)
	}
	if restored, err = restarted.LoadTokens(ctx, key); err != nil || restored != nil {
		t.Fatal("expired vault readable")
	}
	if err = restarted.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Get(ctx, key); !errors.Is(err, ErrMissing) {
		t.Fatal("expired state not purged")
	}
	var quotaWins atomic.Int32
	var activeMu sync.Mutex
	var active []Key
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			upload := Key{Customer: cid, Scope: uuid.NewString(), Kind: "upload", ID: uuid.NewString()}
			err := store.CreateUpload(ctx, upload, json.RawMessage(`{}`), 1)
			if err == nil {
				quotaWins.Add(1)
				activeMu.Lock()
				active = append(active, upload)
				activeMu.Unlock()
			} else if !errors.Is(err, ErrCapacity) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if quotaWins.Load() != 8 {
		t.Fatalf("tenant upload quota raced: %d", quotaWins.Load())
	}
	for _, upload := range active {
		if err = store.Delete(ctx, upload); err != nil {
			t.Fatal(err)
		}
	}
	orphan := Key{Customer: cid, Scope: key.Scope, Kind: "upload", ID: uuid.NewString()}
	if err = store.CreateUpload(ctx, orphan, json.RawMessage(`{}`), 6); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Append(ctx, orphan, 0, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE gateway_batch_state SET expires_at=now()-interval '1 second' WHERE customer_id=$1 AND scope=$2 AND kind='upload' AND resource_id=$3`, cid, orphan.Scope, orphan.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM gateway_batch_chunks WHERE customer_id=$1 AND scope=$2 AND resource_id=$3`, cid, orphan.Scope, orphan.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired session left newer orphan chunks")
	}
}
