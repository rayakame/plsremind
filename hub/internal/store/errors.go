package store

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// IsUnstorableJSON reports whether a value was valid JSON to Go but not to PostgreSQL:
// a lone surrogate in a string, or a number beyond what numeric holds.
func IsUnstorableJSON(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "22P02" || pgErr.Code == "22003" || pgErr.Code == "22P05")
}

// IsDeadlock reports whether PostgreSQL aborted the statement to break a deadlock.
func IsDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

// ViolatedConstraint names the constraint or unique index a failed statement ran into.
func ViolatedConstraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}
