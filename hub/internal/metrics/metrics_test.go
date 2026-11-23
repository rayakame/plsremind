package metrics_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/metrics"
)

func scrape(t *testing.T, m *metrics.Metrics) string {
	t.Helper()
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	res, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return string(body)
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func TestBuildInfoCarriesTheVersion(t *testing.T) {
	body := scrape(t, metrics.New("1.2.3"))
	require.Contains(t, body, `plsremind_build_info{version="1.2.3"} 1`)
	require.Contains(t, body, "go_goroutines")
	require.Contains(t, body, "process_start_time_seconds")
}

func TestStartRefusesAnAddressItCannotBind(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer taken.Close()
	require.Error(t, metrics.Start(context.Background(), taken.Addr().String(), metrics.New("dev")))
	require.Error(t, metrics.Start(context.Background(), "not an address", metrics.New("dev")))
}

func TestStartServesMetricsUntilItsContextEnds(t *testing.T) {
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, metrics.Start(ctx, addr, metrics.New("dev")))

	res, err := http.Get("http://" + addr + "/metrics")
	require.NoError(t, err)
	res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	res, err = http.Get("http://" + addr + "/somewhere")
	require.NoError(t, err)
	res.Body.Close()
	require.Equal(t, http.StatusNotFound, res.StatusCode)

	cancel()
	require.Eventually(t, func() bool {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return false
		}
		ln.Close()
		return true
	}, 6*time.Second, 50*time.Millisecond, "the address is free again")
}
