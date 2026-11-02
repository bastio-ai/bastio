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

var ErrApprovalExpired = errors.New("mcp approval request has expired or already been resolved")

type Store interface {
	List(context.Context, uuid.UUID) ([]Server, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Server, error)
	Create(context.Context, uuid.UUID, Server, string) (Server, error)
	UpdatePolicy(context.Context, uuid.UUID, uuid.UUID, Policy) error
	UpdateConnection(context.Context, uuid.UUID, uuid.UUID, string, string, string, []byte, bool) (Server, error)
	Rotate(context.Context, uuid.UUID, uuid.UUID, string) error
	Revoke(context.Context, uuid.UUID, uuid.UUID) error
	Events(context.Context, uuid.UUID, uuid.UUID, int) ([]Event, error)
	Ingest(context.Context, string, RuntimeBatch) (Server, error)
	LookupByTokenHash(context.Context, string) (Server, error)
	RecordServerEvents(context.Context, uuid.UUID, uuid.UUID, []Event) error
	CreateApproval(context.Context, uuid.UUID, uuid.UUID, string, []string, []string, time.Duration) (Approval, error)
	GetApproval(context.Context, uuid.UUID, uuid.UUID) (Approval, error)
	ListApprovals(context.Context, uuid.UUID, uuid.UUID, string, int) ([]Approval, error)
	ResolveApproval(context.Context, uuid.UUID, uuid.UUID, string, string, string) (Approval, error)
}

type PGStore struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool} }

func (s *PGStore) List(ctx context.Context, cid uuid.UUID) ([]Server, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.name,s.client,s.mode,s.upstream_url,s.auth_type,s.encrypted_credential IS NOT NULL,s.policy,s.created_at,s.last_seen_at,s.revoked_at,s.dropped_events,
 (SELECT COUNT(*) FROM mcp_events e WHERE e.customer_id=s.customer_id AND e.server_id=s.id AND e.created_at>=NOW()-INTERVAL '30 days' AND e.direction='request'),
 (SELECT COUNT(*) FROM mcp_events e WHERE e.customer_id=s.customer_id AND e.server_id=s.id AND e.created_at>=NOW()-INTERVAL '30 days' AND e.direction='request' AND e.action IN ('blocked','denied')),
 (SELECT COUNT(*) FROM mcp_approvals a WHERE a.customer_id=s.customer_id AND a.server_id=s.id AND a.status='pending' AND a.expires_at>NOW())
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
		if err := rows.Scan(&v.ID, &v.Name, &v.Client, &v.Mode, &v.UpstreamURL, &v.AuthType, &v.HasCredential, &policy, &v.CreatedAt, &v.LastSeenAt, &v.RevokedAt, &v.DroppedEvents, &v.Calls, &v.Blocked, &v.PendingApprovals); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(policy, &v.Policy); err != nil {
			return nil, err
		}
		v.Policy = normalizePolicy(v.Policy)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *PGStore) Get(ctx context.Context, cid, id uuid.UUID) (Server, error) {
	var v Server
	var policy []byte
	v.CustomerID = cid
	err := s.pool.QueryRow(ctx, `SELECT s.id,s.name,s.client,s.mode,s.upstream_url,s.auth_type,s.encrypted_credential,s.encrypted_credential IS NOT NULL,s.policy,s.created_at,s.last_seen_at,s.revoked_at,s.dropped_events,
 (SELECT COUNT(*) FROM mcp_events e WHERE e.customer_id=s.customer_id AND e.server_id=s.id AND e.created_at>=NOW()-INTERVAL '30 days' AND e.direction='request'),
 (SELECT COUNT(*) FROM mcp_events e WHERE e.customer_id=s.customer_id AND e.server_id=s.id AND e.created_at>=NOW()-INTERVAL '30 days' AND e.direction='request' AND e.action IN ('blocked','denied')),
 (SELECT COUNT(*) FROM mcp_approvals a WHERE a.customer_id=s.customer_id AND a.server_id=s.id AND a.status='pending' AND a.expires_at>NOW())
 FROM mcp_servers s WHERE s.customer_id=$1 AND s.id=$2`, cid, id).Scan(
		&v.ID, &v.Name, &v.Client, &v.Mode, &v.UpstreamURL, &v.AuthType, &v.credential, &v.HasCredential, &policy, &v.CreatedAt, &v.LastSeenAt, &v.RevokedAt, &v.DroppedEvents, &v.Calls, &v.Blocked, &v.PendingApprovals,
	)
	if err != nil {
		return Server{}, err
	}
	if err := json.Unmarshal(policy, &v.Policy); err != nil {
		return Server{}, err
	}
	v.Policy = normalizePolicy(v.Policy)
	return v, nil
}

func (s *PGStore) Create(ctx context.Context, cid uuid.UUID, srv Server, hash string) (Server, error) {
	srv.Policy = normalizePolicy(srv.Policy)
	raw, err := json.Marshal(srv.Policy)
	if err != nil {
		return Server{}, err
	}
	if srv.ID == uuid.Nil {
		srv.ID, err = uuid.NewV7()
		if err != nil {
			srv.ID = uuid.New()
		}
	}
	if srv.Mode == "" {
		srv.Mode = "stdio"
	}
	if srv.AuthType == "" {
		srv.AuthType = "none"
	}
	var cred any
	if len(srv.credential) > 0 {
		cred = srv.credential
	}
	v := Server{
		ID:            srv.ID,
		CustomerID:    cid,
		Name:          srv.Name,
		Client:        srv.Client,
		Mode:          srv.Mode,
		UpstreamURL:   srv.UpstreamURL,
		AuthType:      srv.AuthType,
		HasCredential: len(srv.credential) > 0,
		Policy:        srv.Policy,
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO mcp_servers(id,customer_id,name,client,mode,upstream_url,auth_type,encrypted_credential,policy,token_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id,created_at`,
		v.ID, cid, v.Name, v.Client, v.Mode, v.UpstreamURL, v.AuthType, cred, raw, hash).Scan(&v.ID, &v.CreatedAt)
	return v, err
}

func (s *PGStore) UpdatePolicy(ctx context.Context, cid, id uuid.UUID, p Policy) error {
	p = normalizePolicy(p)
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

func (s *PGStore) UpdateConnection(ctx context.Context, cid, id uuid.UUID, mode, upstreamURL, authType string, credential []byte, replaceCredential bool) (Server, error) {
	var cred any
	if len(credential) > 0 {
		cred = credential
	}
	var tag interface{ RowsAffected() int64 }
	var err error
	if replaceCredential {
		tag, err = s.pool.Exec(ctx, `UPDATE mcp_servers SET mode=$3,upstream_url=$4,auth_type=$5,encrypted_credential=$6 WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL`,
			cid, id, mode, upstreamURL, authType, cred)
	} else {
		tag, err = s.pool.Exec(ctx, `UPDATE mcp_servers SET mode=$3,upstream_url=$4,auth_type=$5 WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL`,
			cid, id, mode, upstreamURL, authType)
	}
	if err != nil {
		return Server{}, err
	}
	if tag.RowsAffected() == 0 {
		return Server{}, pgx.ErrNoRows
	}
	return s.Get(ctx, cid, id)
}

func (s *PGStore) Rotate(ctx context.Context, cid, id uuid.UUID, hash string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE mcp_servers SET token_hash=$3,last_seen_at=NULL,ingest_window_started_at=NOW(),ingest_window_requests=0 WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL`, cid, id, hash)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

func (s *PGStore) Revoke(ctx context.Context, cid, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE mcp_servers SET revoked_at=COALESCE(revoked_at,NOW()),encrypted_credential=NULL WHERE customer_id=$1 AND id=$2`, cid, id)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if err == nil {
		_, _ = s.pool.Exec(ctx, `UPDATE mcp_approvals SET status='expired' WHERE customer_id=$1 AND server_id=$2 AND status='pending'`, cid, id)
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

func (s *PGStore) LookupByTokenHash(ctx context.Context, hash string) (Server, error) {
	var v Server
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT id,customer_id,name,client,mode,upstream_url,auth_type,encrypted_credential,encrypted_credential IS NOT NULL,policy
 FROM mcp_servers WHERE token_hash=$1 AND revoked_at IS NULL`, hash).Scan(
		&v.ID, &v.CustomerID, &v.Name, &v.Client, &v.Mode, &v.UpstreamURL, &v.AuthType, &v.credential, &v.HasCredential, &raw,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Server{}, ErrEnrollment
	}
	if err != nil {
		return Server{}, err
	}
	if err = json.Unmarshal(raw, &v.Policy); err != nil {
		return Server{}, err
	}
	v.Policy = normalizePolicy(v.Policy)
	return v, nil
}

func (s *PGStore) RecordServerEvents(ctx context.Context, cid, serverID uuid.UUID, events []Event) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, e := range events {
		if e.ID == uuid.Nil {
			e.ID, err = uuid.NewV7()
			if err != nil {
				e.ID = uuid.New()
			}
		}
		if e.Categories == nil {
			e.Categories = []string{}
		}
		if e.OccurredAt.IsZero() {
			e.OccurredAt = time.Now().UTC()
		}
		cat, _ := json.Marshal(e.Categories)
		if _, err = tx.Exec(ctx, `INSERT INTO mcp_events(id,customer_id,server_id,tool_name,direction,action,categories,duration_ms,occurred_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO NOTHING`,
			e.ID, cid, serverID, e.ToolName, e.Direction, e.Action, cat, e.DurationMS, e.OccurredAt); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE mcp_servers SET last_seen_at=NOW() WHERE customer_id=$1 AND id=$2`, cid, serverID); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	err = tx.QueryRow(ctx, `SELECT id,customer_id,name,client,mode,upstream_url,auth_type,policy,ingest_window_started_at,ingest_window_requests FROM mcp_servers WHERE token_hash=$1 AND revoked_at IS NULL FOR UPDATE`, hash).Scan(&v.ID, &v.CustomerID, &v.Name, &v.Client, &v.Mode, &v.UpstreamURL, &v.AuthType, &raw, &window, &requests)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrEnrollment
	}
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(raw, &v.Policy); err != nil {
		return v, err
	}
	v.Policy = normalizePolicy(v.Policy)
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

func (s *PGStore) CreateApproval(ctx context.Context, cid, serverID uuid.UUID, toolName string, argKeys, categories []string, ttl time.Duration) (Approval, error) {
	if argKeys == nil {
		argKeys = []string{}
	}
	if categories == nil {
		categories = []string{}
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	keysJSON, _ := json.Marshal(argKeys)
	catsJSON, _ := json.Marshal(categories)
	expiresAt := time.Now().UTC().Add(ttl)
	var a Approval
	a.CustomerID = cid
	a.ServerID = serverID
	a.ToolName = toolName
	a.ArgumentKeys = argKeys
	a.Categories = categories
	err := s.pool.QueryRow(ctx, `WITH ins AS (
 INSERT INTO mcp_approvals(customer_id,server_id,tool_name,argument_keys,categories,status,expires_at)
 VALUES($1,$2,$3,$4,$5,'pending',$6)
 RETURNING id,customer_id,server_id,tool_name,status,decided_by,decision_reason,requested_at,expires_at,decided_at
)
SELECT ins.id,ins.server_id,srv.name,srv.client,ins.tool_name,ins.status,ins.decided_by,ins.decision_reason,ins.requested_at,ins.expires_at,ins.decided_at
FROM ins JOIN mcp_servers srv ON srv.customer_id=ins.customer_id AND srv.id=ins.server_id`,
		cid, serverID, toolName, keysJSON, catsJSON, expiresAt).Scan(
		&a.ID, &a.ServerID, &a.ServerName, &a.Client, &a.ToolName, &a.Status, &a.DecidedBy, &a.DecisionReason, &a.RequestedAt, &a.ExpiresAt, &a.DecidedAt,
	)
	return a, err
}

func (s *PGStore) GetApproval(ctx context.Context, cid, approvalID uuid.UUID) (Approval, error) {
	_, _ = s.pool.Exec(ctx, `UPDATE mcp_approvals SET status='expired' WHERE customer_id=$1 AND id=$2 AND status='pending' AND expires_at<=NOW()`, cid, approvalID)
	var a Approval
	var keysJSON, catsJSON []byte
	a.CustomerID = cid
	err := s.pool.QueryRow(ctx, `SELECT a.id,a.server_id,srv.name,srv.client,a.tool_name,a.argument_keys,a.categories,a.status,a.decided_by,a.decision_reason,a.requested_at,a.expires_at,a.decided_at
 FROM mcp_approvals a JOIN mcp_servers srv ON srv.customer_id=a.customer_id AND srv.id=a.server_id
 WHERE a.customer_id=$1 AND a.id=$2`, cid, approvalID).Scan(
		&a.ID, &a.ServerID, &a.ServerName, &a.Client, &a.ToolName, &keysJSON, &catsJSON, &a.Status, &a.DecidedBy, &a.DecisionReason, &a.RequestedAt, &a.ExpiresAt, &a.DecidedAt,
	)
	if err != nil {
		return Approval{}, err
	}
	_ = json.Unmarshal(keysJSON, &a.ArgumentKeys)
	_ = json.Unmarshal(catsJSON, &a.Categories)
	if a.ArgumentKeys == nil {
		a.ArgumentKeys = []string{}
	}
	if a.Categories == nil {
		a.Categories = []string{}
	}
	return a, nil
}

func (s *PGStore) ListApprovals(ctx context.Context, cid, serverID uuid.UUID, status string, limit int) ([]Approval, error) {
	_, _ = s.pool.Exec(ctx, `UPDATE mcp_approvals SET status='expired' WHERE customer_id=$1 AND status='pending' AND expires_at<=NOW()`, cid)
	rows, err := s.pool.Query(ctx, `SELECT a.id,a.server_id,srv.name,srv.client,a.tool_name,a.argument_keys,a.categories,a.status,a.decided_by,a.decision_reason,a.requested_at,a.expires_at,a.decided_at
 FROM mcp_approvals a JOIN mcp_servers srv ON srv.customer_id=a.customer_id AND srv.id=a.server_id
 WHERE a.customer_id=$1
   AND ($2::uuid='00000000-0000-0000-0000-000000000000'::uuid OR a.server_id=$2)
   AND ($3='' OR a.status=$3)
 ORDER BY CASE WHEN a.status='pending' THEN 0 ELSE 1 END, a.requested_at DESC, a.id DESC
 LIMIT $4`, cid, serverID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Approval{}
	for rows.Next() {
		var a Approval
		var keysJSON, catsJSON []byte
		a.CustomerID = cid
		if err := rows.Scan(&a.ID, &a.ServerID, &a.ServerName, &a.Client, &a.ToolName, &keysJSON, &catsJSON, &a.Status, &a.DecidedBy, &a.DecisionReason, &a.RequestedAt, &a.ExpiresAt, &a.DecidedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(keysJSON, &a.ArgumentKeys)
		_ = json.Unmarshal(catsJSON, &a.Categories)
		if a.ArgumentKeys == nil {
			a.ArgumentKeys = []string{}
		}
		if a.Categories == nil {
			a.Categories = []string{}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *PGStore) ResolveApproval(ctx context.Context, cid, approvalID uuid.UUID, status, decidedBy, reason string) (Approval, error) {
	_, _ = s.pool.Exec(ctx, `UPDATE mcp_approvals SET status='expired' WHERE customer_id=$1 AND id=$2 AND status='pending' AND expires_at<=NOW()`, cid, approvalID)
	var a Approval
	var keysJSON, catsJSON []byte
	a.CustomerID = cid
	err := s.pool.QueryRow(ctx, `WITH upd AS (
 UPDATE mcp_approvals SET status=$3,decided_by=$4,decision_reason=$5,decided_at=NOW()
 WHERE customer_id=$1 AND id=$2 AND status='pending' AND expires_at>NOW()
 RETURNING id,customer_id,server_id,tool_name,argument_keys,categories,status,decided_by,decision_reason,requested_at,expires_at,decided_at
)
SELECT upd.id,upd.server_id,srv.name,srv.client,upd.tool_name,upd.argument_keys,upd.categories,upd.status,upd.decided_by,upd.decision_reason,upd.requested_at,upd.expires_at,upd.decided_at
FROM upd JOIN mcp_servers srv ON srv.customer_id=upd.customer_id AND srv.id=upd.server_id`,
		cid, approvalID, status, decidedBy, reason).Scan(
		&a.ID, &a.ServerID, &a.ServerName, &a.Client, &a.ToolName, &keysJSON, &catsJSON, &a.Status, &a.DecidedBy, &a.DecisionReason, &a.RequestedAt, &a.ExpiresAt, &a.DecidedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if checkErr := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mcp_approvals WHERE customer_id=$1 AND id=$2)`, cid, approvalID).Scan(&exists); checkErr == nil && exists {
			return Approval{}, ErrApprovalExpired
		}
		return Approval{}, pgx.ErrNoRows
	}
	if err != nil {
		return Approval{}, err
	}
	_ = json.Unmarshal(keysJSON, &a.ArgumentKeys)
	_ = json.Unmarshal(catsJSON, &a.Categories)
	if a.ArgumentKeys == nil {
		a.ArgumentKeys = []string{}
	}
	if a.Categories == nil {
		a.Categories = []string{}
	}
	return a, nil
}

// Prune expires inactive enrollments' metadata too; each delete is tenant-scoped.
func (s *PGStore) Prune(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT customer_id FROM mcp_events WHERE created_at<NOW()-INTERVAL '30 days'
 UNION SELECT DISTINCT customer_id FROM mcp_approvals WHERE (status='pending' AND expires_at<=NOW()) OR requested_at<NOW()-INTERVAL '30 days'`)
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
		if _, err = s.pool.Exec(ctx, `UPDATE mcp_approvals SET status='expired' WHERE customer_id=$1 AND status='pending' AND expires_at<=NOW()`, cid); err != nil {
			return err
		}
		if _, err = s.pool.Exec(ctx, `DELETE FROM mcp_approvals WHERE customer_id=$1 AND requested_at<NOW()-INTERVAL '30 days'`, cid); err != nil {
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
