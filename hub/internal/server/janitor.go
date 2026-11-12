package server

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

const (
	eventRetention       = 24 * time.Hour
	reminderLogRetention = 30 * 24 * time.Hour
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
	if err := q.DeleteExpiredInvites(ctx, now); err != nil {
		return err
	}
	// The gateway only needs the log to bridge short gaps; sessions resume from their own buffer.
	if err := q.DeleteOldEvents(ctx, now.Add(-eventRetention)); err != nil {
		return err
	}
	return q.DeleteOldReminderLog(ctx, now.Add(-reminderLogRetention))
}
