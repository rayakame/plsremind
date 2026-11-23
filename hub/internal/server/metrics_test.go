package server_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func requests(t *testing.T, e *testkit.Env) map[string]float64 {
	t.Helper()
	families, err := e.Deps.Metrics.Registry().Gather()
	require.NoError(t, err)
	counts := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "plsremind_http_requests_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, pair := range metric.GetLabel() {
				labels[pair.GetName()] = pair.GetValue()
			}
			counts[labels["method"]+" "+labels["route"]+" "+labels["status"]] = metric.GetCounter().GetValue()
		}
	}
	return counts
}

func TestRequestsAreCountedByRoutePattern(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	item := mia.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
	mia.Do(t, "GET", "/users/@me/items/"+item, nil)
	mia.Do(t, "GET", "/users/@me/items/"+item, nil)
	counts := requests(t, e)
	require.Equal(t, 2.0, counts["GET /api/v1/users/:user/items/:id 200"], counts)
	for key := range counts {
		require.NotContains(t, key, item)
		require.NotContains(t, key, "@me")
	}
}

func TestPathsNoRouteKnowsShareOneLabel(t *testing.T) {
	e := testkit.New(t)
	get(t, e, "/api/v1/nope/1")
	get(t, e, "/api/v1/x/y/z")
	get(t, e, "/api/elsewhere")
	counts := requests(t, e)
	require.Equal(t, 3.0, counts["GET unmatched 404"], counts)
	get(t, e, "/wp-login.php")
	require.Equal(t, 1.0, requests(t, e)["GET web 200"], "the dashboard answers every other path")
	for key := range requests(t, e) {
		require.False(t, strings.Contains(key, "nope") || strings.Contains(key, "wp-login"), key)
	}
}

func TestAnErrorIsCountedWithItsStatusUnderItsRoute(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	missing := mia.Do(t, "GET", "/users/@me/items/01930000-0000-7000-8000-000000000999", nil)
	require.Equal(t, 404, missing.Status)
	require.Equal(t, 1.0, requests(t, e)["GET /api/v1/users/:user/items/:id 404"], requests(t, e))
}

func TestTheDatabasePoolIsReported(t *testing.T) {
	e := testkit.New(t)
	families, err := e.Deps.Metrics.Registry().Gather()
	require.NoError(t, err)
	found := map[string]float64{}
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			key := family.GetName()
			for _, pair := range metric.GetLabel() {
				key += " " + pair.GetValue()
			}
			found[key] = metric.GetGauge().GetValue() + metric.GetCounter().GetValue()
		}
	}
	require.Equal(t, float64(e.Pool.Stat().MaxConns()), found["plsremind_db_pool_connections max"])
	for _, key := range []string{"plsremind_db_pool_connections acquired", "plsremind_db_pool_connections idle",
		"plsremind_db_pool_connections total", "plsremind_db_pool_acquire_wait_seconds_total", "plsremind_db_pool_empty_acquires_total"} {
		_, ok := found[key]
		require.True(t, ok, key)
	}
}
