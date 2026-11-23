package core_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/core"
)

func environment(t *testing.T) {
	t.Helper()
	t.Setenv("PLSREMIND_DATABASE_URL", "postgres://localhost/plsremind")
	t.Setenv("PLSREMIND_PUBLIC_URL", "https://todo.example.com")
	t.Setenv("PLSREMIND_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
}

func TestMetricsAreOffUnlessAnAddressIsSet(t *testing.T) {
	environment(t)
	t.Setenv("PLSREMIND_METRICS_ADDR", "")
	cfg, err := core.LoadConfig()
	require.NoError(t, err)
	require.Empty(t, cfg.MetricsAddr)

	t.Setenv("PLSREMIND_METRICS_ADDR", ":9090")
	cfg, err = core.LoadConfig()
	require.NoError(t, err)
	require.Equal(t, ":9090", cfg.MetricsAddr)
}
