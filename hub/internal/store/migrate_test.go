package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func schemaVersion(t *testing.T, e *testkit.Env) int64 {
	t.Helper()
	var version int64
	require.NoError(t, e.Pool.QueryRow(context.Background(), "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version))
	return version
}

func TestMigrateTwiceIsANoOp(t *testing.T) {
	e := testkit.New(t)
	before := schemaVersion(t, e)
	require.NoError(t, store.Migrate(context.Background(), e.DatabaseURL()))
	require.Equal(t, before, schemaVersion(t, e))
}

func TestAnOlderHubRefusesANewerDatabase(t *testing.T) {
	e := testkit.New(t)
	known := schemaVersion(t, e)
	_, err := e.Pool.Exec(context.Background(), "INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, true)", known+1)
	require.NoError(t, err)

	err = store.Migrate(context.Background(), e.DatabaseURL())
	var tooNew *store.SchemaTooNewError
	require.True(t, errors.As(err, &tooNew), "got %v", err)
	require.Equal(t, known+1, tooNew.Database)
	require.Equal(t, known, tooNew.Known)
	require.Contains(t, err.Error(), "restore the backup")
	require.Equal(t, known+1, schemaVersion(t, e))
}
