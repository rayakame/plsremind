package httpapi

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
)

// Sweep deletes rows that have expired and can never be used again.
func Sweep(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	q := sqlc.New(pool)
	if err := q.DeleteExpiredSessions(ctx, now); err != nil {
		return err
	}
	if err := q.DeleteExpiredOAuthStates(ctx, now); err != nil {
		return err
	}
	return q.DeleteExpiredInvites(ctx, now)
}
