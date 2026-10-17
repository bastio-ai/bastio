// Package batchstate owns encrypted, tenant-scoped operational batch state.
package batchstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrMissing = errors.New("batch state unavailable or expired")
var ErrOffset = errors.New("upload offset does not match received bytes")
var ErrCapacity = errors.New("temporary batch upload quota exceeded")

type Key struct {
	Customer        uuid.UUID
	Scope, Kind, ID string
}

func (k Key) context() string { return "gateway-batch/" + k.Scope + "/" + k.Kind + "/" + k.ID }

type Record struct {
	Payload   json.RawMessage
	Received  int64
	Completed bool
}

type Backend interface {
	Put(context.Context, Key, json.RawMessage, time.Duration) error
	Get(context.Context, Key) (Record, error)
	Delete(context.Context, Key) error
	SaveTokens(context.Context, Key, *security.TokenMap) error
	LoadTokens(context.Context, Key) (*security.TokenMap, error)
	Once(context.Context, Key) (bool, error)
	Append(context.Context, Key, int64, []byte) (int64, error)
	Chunk(context.Context, Key, int64) ([]byte, error)
	Lease(context.Context, Key) (bool, error)
	Complete(context.Context, Key, json.RawMessage) error
	CreateUpload(context.Context, Key, json.RawMessage, int64) error
	Release(context.Context, Key) error
}

type Store struct {
	pool   *pgxpool.Pool
	cipher *encryption.Service
	ttl    time.Duration
}

func New(pool *pgxpool.Pool, cipher *encryption.Service, ttl time.Duration) *Store {
	if ttl <= 0 || ttl > 90*24*time.Hour {
		ttl = 30 * 24 * time.Hour
	}
	return &Store{pool: pool, cipher: cipher, ttl: ttl}
}
func (s *Store) seal(k Key, payload []byte) ([]byte, error) {
	if s.cipher == nil {
		return nil, fmt.Errorf("batch encryption unavailable")
	}
	enc, err := s.cipher.Encrypt(string(payload), k.Customer.String(), k.context())
	if err != nil {
		return nil, err
	}
	return json.Marshal(enc)
}
func (s *Store) open(k Key, payload []byte) ([]byte, error) {
	var enc encryption.Encrypted
	if s.cipher == nil || json.Unmarshal(payload, &enc) != nil {
		return nil, fmt.Errorf("invalid batch vault")
	}
	plain, err := s.cipher.Decrypt(&enc, k.Customer.String(), k.context())
	return []byte(plain), err
}
func (s *Store) putEncrypted(ctx context.Context, k Key, payload []byte, ttl time.Duration) error {
	if ttl <= 0 || ttl > s.ttl {
		ttl = s.ttl
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO gateway_batch_state(customer_id,scope,kind,resource_id,encrypted_payload,expires_at)
VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(customer_id,scope,kind,resource_id)
DO UPDATE SET encrypted_payload=EXCLUDED.encrypted_payload,expires_at=LEAST(gateway_batch_state.expires_at,EXCLUDED.expires_at)
WHERE gateway_batch_state.customer_id=$1`, k.Customer, k.Scope, k.Kind, k.ID, payload, time.Now().Add(ttl))
	return err
}
func (s *Store) Put(ctx context.Context, k Key, payload json.RawMessage, ttl time.Duration) error {
	enc, err := s.seal(k, payload)
	if err != nil {
		return err
	}
	return s.putEncrypted(ctx, k, enc, ttl)
}
func (s *Store) encrypted(ctx context.Context, k Key) ([]byte, Record, error) {
	var payload []byte
	var record Record
	err := s.pool.QueryRow(ctx, `SELECT encrypted_payload,received_bytes,completed FROM gateway_batch_state
WHERE customer_id=$1 AND scope=$2 AND kind=$3 AND resource_id=$4 AND expires_at>now()`, k.Customer, k.Scope, k.Kind, k.ID).Scan(&payload, &record.Received, &record.Completed)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrMissing
	}
	return payload, record, err
}
func (s *Store) Get(ctx context.Context, k Key) (Record, error) {
	enc, record, err := s.encrypted(ctx, k)
	if err != nil {
		return record, err
	}
	record.Payload, err = s.open(k, enc)
	return record, err
}
func (s *Store) Delete(ctx context.Context, k Key) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if k.Kind == "upload" {
		if _, err = tx.Exec(ctx, `DELETE FROM gateway_batch_chunks WHERE customer_id=$1 AND scope=$2 AND resource_id=$3`, k.Customer, k.Scope, k.ID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM gateway_batch_state WHERE customer_id=$1 AND scope=$2 AND kind=$3 AND resource_id=$4`, k.Customer, k.Scope, k.Kind, k.ID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) SaveTokens(ctx context.Context, k Key, tm *security.TokenMap) error {
	if tm == nil || tm.Size() == 0 {
		return nil
	}
	enc, err := tm.Seal(s.cipher, k.Customer.String(), k.context())
	if err != nil {
		return err
	}
	raw, err := json.Marshal(enc)
	if err != nil {
		return err
	}
	return s.putEncrypted(ctx, k, raw, s.ttl)
}
func (s *Store) LoadTokens(ctx context.Context, k Key) (*security.TokenMap, error) {
	raw, _, err := s.encrypted(ctx, k)
	if errors.Is(err, ErrMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var enc encryption.Encrypted
	if json.Unmarshal(raw, &enc) != nil {
		return nil, fmt.Errorf("invalid token vault")
	}
	return security.OpenTokenMap(s.cipher, &enc, k.Customer.String(), k.context())
}
func (s *Store) Once(ctx context.Context, k Key) (bool, error) {
	enc, err := s.seal(k, []byte(`{}`))
	if err != nil {
		return false, err
	}
	result, err := s.pool.Exec(ctx, `INSERT INTO gateway_batch_state(customer_id,scope,kind,resource_id,encrypted_payload,expires_at)
VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, k.Customer, k.Scope, k.Kind, k.ID, enc, time.Now().Add(s.ttl))
	return result.RowsAffected() == 1, err
}
func (s *Store) Append(ctx context.Context, k Key, offset int64, chunk []byte) (int64, error) {
	if len(chunk) == 0 {
		return offset, nil
	}
	if len(chunk) > 2<<20 {
		return offset, fmt.Errorf("upload chunk exceeds internal limit")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return offset, err
	}
	defer tx.Rollback(ctx)
	var received int64
	var reserved int64
	var complete bool
	var lease *time.Time
	err = tx.QueryRow(ctx, `SELECT received_bytes,reserved_bytes,completed,lease_until FROM gateway_batch_state WHERE customer_id=$1 AND scope=$2 AND kind='upload' AND resource_id=$3 AND expires_at>now() FOR UPDATE`, k.Customer, k.Scope, k.ID).Scan(&received, &reserved, &complete, &lease)
	if err != nil {
		return offset, err
	}
	if received != offset {
		return received, ErrOffset
	}
	if received+int64(len(chunk)) > reserved {
		return received, fmt.Errorf("upload exceeds reserved file size")
	}
	if complete || lease != nil && lease.After(time.Now()) {
		return received, fmt.Errorf("upload is finalizing or complete")
	}
	chunkKey := k
	chunkKey.ID = fmt.Sprintf("%s/chunk/%d", k.ID, offset)
	enc, err := s.seal(chunkKey, chunk)
	if err != nil {
		return received, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO gateway_batch_chunks(customer_id,scope,resource_id,chunk_offset,encrypted_payload,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, k.Customer, k.Scope, k.ID, offset, enc, time.Now().Add(24*time.Hour))
	if err != nil {
		return received, err
	}
	received += int64(len(chunk))
	_, err = tx.Exec(ctx, `UPDATE gateway_batch_state SET received_bytes=$4 WHERE customer_id=$1 AND scope=$2 AND kind='upload' AND resource_id=$3`, k.Customer, k.Scope, k.ID, received)
	if err != nil {
		return offset, err
	}
	return received, tx.Commit(ctx)
}
func (s *Store) Chunk(ctx context.Context, k Key, offset int64) ([]byte, error) {
	var enc []byte
	err := s.pool.QueryRow(ctx, `SELECT encrypted_payload FROM gateway_batch_chunks WHERE customer_id=$1 AND scope=$2 AND resource_id=$3 AND chunk_offset=$4 AND expires_at>now()`, k.Customer, k.Scope, k.ID, offset).Scan(&enc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, io.EOF
	}
	if err != nil {
		return nil, err
	}
	k.ID = fmt.Sprintf("%s/chunk/%d", k.ID, offset)
	return s.open(k, enc)
}
func (s *Store) Lease(ctx context.Context, k Key) (bool, error) {
	result, err := s.pool.Exec(ctx, `UPDATE gateway_batch_state SET lease_until=now()+interval '10 minutes' WHERE customer_id=$1 AND scope=$2 AND kind='upload' AND resource_id=$3 AND expires_at>now() AND NOT completed AND (lease_until IS NULL OR lease_until<now())`, k.Customer, k.Scope, k.ID)
	return result.RowsAffected() == 1, err
}
func (s *Store) Complete(ctx context.Context, k Key, payload json.RawMessage) error {
	enc, err := s.seal(k, payload)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE gateway_batch_state SET encrypted_payload=$4,completed=TRUE,lease_until=NULL WHERE customer_id=$1 AND scope=$2 AND kind='upload' AND resource_id=$3`, k.Customer, k.Scope, k.ID, enc)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM gateway_batch_chunks WHERE customer_id=$1 AND scope=$2 AND resource_id=$3`, k.Customer, k.Scope, k.ID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreateUpload serializes quota checks per tenant, across processes. These are
// temporary quarantine reservations, released on finalize/cancel/expiry.
func (s *Store) CreateUpload(ctx context.Context, k Key, payload json.RawMessage, size int64) error {
	if size < 0 || size > 2<<30 {
		return fmt.Errorf("invalid upload size")
	}
	enc, err := s.seal(k, payload)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,1831))`, k.Customer.String()); err != nil {
		return err
	}
	var count int
	var reserved int64
	err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(reserved_bytes),0) FROM gateway_batch_state WHERE customer_id=$1 AND kind='upload' AND NOT completed AND expires_at>now()`, k.Customer).Scan(&count, &reserved)
	if err != nil {
		return err
	}
	if count >= 8 || reserved+size > 8<<30 {
		return ErrCapacity
	}
	_, err = tx.Exec(ctx, `INSERT INTO gateway_batch_state(customer_id,scope,kind,resource_id,encrypted_payload,reserved_bytes,expires_at) VALUES($1,$2,'upload',$3,$4,$5,$6)`, k.Customer, k.Scope, k.ID, enc, size, time.Now().Add(24*time.Hour))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) Release(ctx context.Context, k Key) error {
	_, err := s.pool.Exec(ctx, `UPDATE gateway_batch_state SET lease_until=NULL WHERE customer_id=$1 AND scope=$2 AND kind='upload' AND resource_id=$3 AND NOT completed`, k.Customer, k.Scope, k.ID)
	return err
}

// Sweep enumerates tenant IDs only; all state/chunk deletion remains scoped.
func (s *Store) Sweep(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT customer_id FROM gateway_batch_state WHERE expires_at<=now() UNION SELECT customer_id FROM gateway_batch_chunks WHERE expires_at<=now() LIMIT 100`)
	if err != nil {
		return err
	}
	var customers []uuid.UUID
	for rows.Next() {
		var cid uuid.UUID
		if err = rows.Scan(&cid); err != nil {
			rows.Close()
			return err
		}
		customers = append(customers, cid)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, cid := range customers {
		if _, err = s.pool.Exec(ctx, `DELETE FROM gateway_batch_chunks c WHERE c.customer_id=$1 AND (c.expires_at<=now() OR NOT EXISTS(SELECT 1 FROM gateway_batch_state s WHERE s.customer_id=$1 AND s.scope=c.scope AND s.resource_id=c.resource_id AND s.kind='upload' AND s.expires_at>now()))`, cid); err != nil {
			return err
		}
		if _, err = s.pool.Exec(ctx, `DELETE FROM gateway_batch_state WHERE customer_id=$1 AND expires_at<=now()`, cid); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RunMaintenance(ctx context.Context) {
	if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
		slog.Error("batch state cleanup failed", "error", err)
	}
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
				slog.Error("batch state cleanup failed", "error", err)
			}
		}
	}
}
