package metrics_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
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

func TestTickNotesHowARoundEnded(t *testing.T) {
	m := metrics.New("dev")
	require.NoError(t, m.Tick(context.Background(), func() error { return nil }))
	require.Error(t, m.Tick(context.Background(), func() error { return errors.New("database away") }))
	require.Equal(t, 1.0, testutil.ToFloat64(m.TickerRuns.WithLabelValues("ok")))
	require.Equal(t, 1.0, testutil.ToFloat64(m.TickerRuns.WithLabelValues("error")))
	require.Positive(t, testutil.ToFloat64(m.TickerLastSuccess))
	require.Contains(t, scrape(t, m), "plsremind_ticker_duration_seconds_count 2")
}

func TestTheTickerCountsFromTheStartOfTheProcess(t *testing.T) {
	before := float64(time.Now().Unix())
	require.GreaterOrEqual(t, testutil.ToFloat64(metrics.New("dev").TickerLastSuccess), before)
}

func TestOpenDeliveriesAreCountedWhenAsked(t *testing.T) {
	m := metrics.New("dev")
	m.CountOpenDeliveries(func(context.Context) (int64, error) { return 7, nil })
	require.Contains(t, scrape(t, m), "plsremind_deliveries_open 7")
}

func TestOpenDeliveriesAreLeftOutWhenTheDatabaseCannotSay(t *testing.T) {
	m := metrics.New("dev")
	m.CountOpenDeliveries(func(context.Context) (int64, error) { return 0, errors.New("database away") })
	body := scrape(t, m)
	require.NotContains(t, body, "plsremind_deliveries_open")
	require.Contains(t, body, "plsremind_build_info", "the rest of the scrape still comes")
}

func TestOpenDeliveriesGiveUpWithinTwoSeconds(t *testing.T) {
	m := metrics.New("dev")
	m.CountOpenDeliveries(func(ctx context.Context) (int64, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})
	start := time.Now()
	require.NotContains(t, scrape(t, m), "plsremind_deliveries_open")
	require.Less(t, time.Since(start), 3*time.Second)
}

func TestANilRegistryCountsNothingAndDoesNotFail(t *testing.T) {
	var m *metrics.Metrics
	m.Reminder("due", "fired", time.Second)
	m.Expire("connection", 2)
	m.Acknowledge("delivered")
	require.NoError(t, m.Tick(context.Background(), func() error { return nil }))
}

func TestARoundCutShortByTheShutdownIsNoError(t *testing.T) {
	m := metrics.New("dev")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, m.Tick(ctx, func() error { return ctx.Err() }))
	require.Zero(t, testutil.ToFloat64(m.TickerRuns.WithLabelValues("error")))
}

func TestCountingOpenDeliveriesAgainReplacesTheCount(t *testing.T) {
	m := metrics.New("dev")
	m.CountOpenDeliveries(func(context.Context) (int64, error) { return 1, nil })
	m.CountOpenDeliveries(func(context.Context) (int64, error) { return 2, nil })
	require.Contains(t, scrape(t, m), "plsremind_deliveries_open 2")
}
