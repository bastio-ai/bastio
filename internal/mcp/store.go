package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrRateLimit = errors.New("mcp reporting rate exceeded")

var ErrEnrollment = errors.New("mcp enrollment is invalid or revoked")

type Store interface {
	List(context.Context, uuid.UUID) ([]Server, error)
	Create(context.Context, uuid.UUID, string, string, Policy, string) (Server, error)
	UpdatePolicy(context.Context, uuid.UUID, uuid.UUID, Policy) error
	Rotate(context.Context, uuid.UUID, uuid.UUID, string) error
	Revoke(context.Context, uuid.UUID, uuid.UUID) error
	Events(context.Context, uuid.UUID, uuid.UUID, int) ([]Event, error)
	Ingest(context.Context, string, RuntimeBatch) (Server, error)
}

type PGStore struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool} }

func (s *PGStore) List(ctx context.Context, cid uuid.UUID) ([]Server, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.name,s.client,s.policy,s.created_at,s.last_seen_at,s.revoked_at,s.dropped_events,
 (SELECT COUNT(*) FROM mcp_events e WHERE e.customer_id=s.customer_id AND e.server_id=s.id AND e.created_at>=NOW()-INTERVAL '30 days' AND e.direction='request'),
 (SELECT COUNT(*) FROM mcp_events e WHERE e.customer_id=s.customer_id AND e.server_id=s.id AND e.created_at>=NOW()-INTERVAL '30 days' AND e.direction='request' AND e.action='blocked')
 FROM mcp_servers s WHERE s.customer_id=$1 ORDER BY s.created_at DESC`, cid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Server{}
	for rows.Next() {
		var v Server
		var policy []byte
		v.CustomerID = cid
		if err := rows.Scan(&v.ID, &v.Name, &v.Client, &policy, &v.CreatedAt, &v.LastSeenAt, &v.RevokedAt, &v.DroppedEvents, &v.Calls, &v.Blocked); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(policy, &v.Policy); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *PGStore) Create(ctx context.Context, cid uuid.UUID, name, client string, p Policy, hash string) (Server, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return Server{}, err
	}
	v := Server{CustomerID: cid, Name: name, Client: client, Policy: p}
	err = s.pool.QueryRow(ctx, `INSERT INTO mcp_servers(customer_id,name,client,policy,token_hash) VALUES($1,$2,$3,$4,$5) RETURNING id,created_at`, cid, name, client, raw, hash).Scan(&v.ID, &v.CreatedAt)
	return v, err
}
func (s *PGStore) UpdatePolicy(ctx context.Context, cid, id uuid.UUID, p Policy) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE mcp_servers SET policy=$3 WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL`, cid, id, raw)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
func (s *PGStore) Rotate(ctx context.Context, cid, id uuid.UUID, hash string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE mcp_servers SET token_hash=$3,last_seen_at=NULL,ingest_window_started_at=NOW(),ingest_window_requests=0 WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL`, cid, id, hash)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
func (s *PGStore) Revoke(ctx context.Context, cid, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE mcp_servers SET revoked_at=COALESCE(revoked_at,NOW()) WHERE customer_id=$1 AND id=$2`, cid, id)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
func (s *PGStore) Events(ctx context.Context, cid, id uuid.UUID, limit int) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `SELECT e.id,e.server_id,s.name,s.client,e.tool_name,e.direction,e.action,e.categories,e.duration_ms,e.occurred_at
 FROM mcp_events e JOIN mcp_servers s ON s.id=e.server_id AND s.customer_id=e.customer_id
 WHERE e.customer_id=$1 AND e.created_at>=NOW()-INTERVAL '30 days' AND ($2::uuid='00000000-0000-0000-0000-000000000000'::uuid OR e.server_id=$2) ORDER BY e.created_at DESC,e.id DESC LIMIT $3`, cid, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		var cat []byte
		if err := rows.Scan(&v.ID, &v.ServerID, &v.ServerName, &v.Client, &v.ToolName, &v.Direction, &v.Action, &cat, &v.DurationMS, &v.OccurredAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(cat, &v.Categories); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *PGStore) Ingest(ctx context.Context, hash string, b RuntimeBatch) (Server, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Server{}, err
	}
	defer tx.Rollback(ctx)
	var v Server
	var raw []byte
	var window time.Time
	var requests int
	// Lock the enrollment row so revocation cannot race a telemetry commit.
	err = tx.QueryRow(ctx, `SELECT id,customer_id,name,client,policy,ingest_window_started_at,ingest_window_requests FROM mcp_servers WHERE token_hash=$1 AND revoked_at IS NULL FOR UPDATE`, hash).Scan(&v.ID, &v.CustomerID, &v.Name, &v.Client, &raw, &window, &requests)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrEnrollment
	}
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(raw, &v.Policy); err != nil {
		return v, err
	}
	if time.Since(window) >= 30*time.Second {
		window = time.Now().UTC()
		requests = 0
	}
	if requests >= 30 {
		return v, ErrRateLimit
	}
	for _, e := range b.Events {
		if e.Categories == nil {
			e.Categories = []string{}
		}
		cat, _ := json.Marshal(e.Categories)
		_, err = tx.Exec(ctx, `INSERT INTO mcp_events(id,customer_id,server_id,tool_name,direction,action,categories,duration_ms,occurred_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO NOTHING`, e.ID, v.CustomerID, v.ID, e.ToolName, e.Direction, e.Action, cat, e.DurationMS, e.OccurredAt)
		if err != nil {
			return v, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE mcp_servers SET last_seen_at=NOW(),dropped_events=dropped_events+$3,ingest_window_started_at=$4,ingest_window_requests=$5 WHERE customer_id=$1 AND id=$2`, v.CustomerID, v.ID, b.DroppedEvents, window, requests+1)
	if err != nil {
		return v, err
	}
	// Server metadata is intentionally bounded to a 30-day window.
	_, err = tx.Exec(ctx, `DELETE FROM mcp_events WHERE customer_id=$1 AND created_at<NOW()-INTERVAL '30 days'`, v.CustomerID)
	if err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}

// Prune expires inactive enrollments' metadata too; each delete is tenant-scoped.
func (s *PGStore) Prune(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT customer_id FROM mcp_events WHERE created_at<NOW()-INTERVAL '30 days'`)
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
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, cid := range customers {
		if _, err = s.pool.Exec(ctx, `DELETE FROM mcp_events WHERE customer_id=$1 AND created_at<NOW()-INTERVAL '30 days'`, cid); err != nil {
			return err
		}
	}
	return nil
}

// RunMaintenance performs an hourly expiry sweep until server shutdown.
func (s *PGStore) RunMaintenance(ctx context.Context) {
	timer := time.NewTicker(time.Hour)
	defer timer.Stop()
	for {
		sweep, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.Prune(sweep)
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.Warn("MCP metadata expiry failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
