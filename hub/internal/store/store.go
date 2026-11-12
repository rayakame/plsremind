// Package store opens the database, migrates it and runs transactions.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Migrate applies all pending migrations.
func Migrate(ctx context.Context, url string) error {
	conn, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer conn.Close()
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, conn, files)
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	_, err = provider.Up(ctx)
	return err
}

// UserTx is Tx for a change to one user's data. It takes the user's row lock
// before anything else, so that all changes to one user happen one after the
// other. Without that, two requests that lock an item and the user in opposite
// order can wait on each other until PostgreSQL aborts one of them.
//
// The lock is FOR NO KEY UPDATE: it does not block inserts that merely refer to the user.
func UserTx(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, fn func(q *sqlc.Queries) error) error {
	return Tx(ctx, pool, func(q *sqlc.Queries) error {
		if _, err := q.LockUser(ctx, userID); err != nil {
			if IsNotFound(err) {
				return api.NotFound()
			}
			return err
		}
		return fn(q)
	})
}

// Tx runs fn in a transaction and commits when fn returns nil.
func Tx(ctx context.Context, pool *pgxpool.Pool, fn func(q *sqlc.Queries) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(sqlc.New(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
