package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// WithTemporaryGatewayKey runs a server-owned request with a normal, scoped
// gateway credential. The plaintext never leaves this callback. Keys are hidden
// from application-key listings, expire in two minutes, and are removed even
// when the caller cancels. Authentication caches are invalidated on cleanup.
func (s *Service) WithTemporaryGatewayKey(ctx context.Context, customerID, proxyID uuid.UUID, run func(string)) error {
	if customerID == uuid.Nil || proxyID == uuid.Nil {
		return fmt.Errorf("missing gateway test identity")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `DELETE FROM gateway_api_keys WHERE customer_id=$1 AND is_ephemeral AND expires_at <= now()`, customerID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO environments (id,customer_id,name,kind,description)
 SELECT $1,$2,'production','production','Default production boundary'
 WHERE NOT EXISTS (SELECT 1 FROM environments WHERE customer_id=$2)
 ON CONFLICT DO NOTHING`, uuid.New(), customerID)
	if err != nil {
		return err
	}
	id, secret := uuid.New(), GenerateAPIKey()
	inserted, err := tx.Exec(ctx, `INSERT INTO gateway_api_keys (id,customer_id,name,key_hash,key_prefix,scopes,rate_limit_rpm,expires_at,environment_id,allow_environment_override,is_ephemeral)
 SELECT $1,$2,'Dashboard gateway connection test',$3,$4,$5,6,$6,e.id,FALSE,TRUE
 FROM environments e WHERE e.customer_id=$2 ORDER BY (lower(e.name)='production') DESC,e.created_at,e.id LIMIT 1`,
		id, customerID, HashAPIKey(secret), secret[:12], []string{"proxy:" + proxyID.String()}, time.Now().Add(2*time.Minute))
	if err != nil {
		return err
	}
	if inserted.RowsAffected() != 1 {
		return fmt.Errorf("workspace environment missing")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.db.Exec(cleanup, `DELETE FROM gateway_api_keys WHERE id=$1 AND customer_id=$2 AND is_ephemeral`, id, customerID); err != nil {
			slog.Warn("temporary gateway key cleanup failed", "key_id", id, "error", err)
		}
		if s.cache != nil {
			if err := s.cache.Del(cleanup, "apikey:"+HashAPIKey(secret)); err != nil {
				slog.Warn("temporary gateway key cache cleanup failed", "key_id", id, "error", err)
			}
		}
	}()
	run(secret)
	return nil
}
