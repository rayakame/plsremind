package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTheHealthCommandAsksTheLocalListener(t *testing.T) {
	for listen, want := range map[string]string{
		":8080":          "http://127.0.0.1:8080/readyz",
		"0.0.0.0:9000":   "http://127.0.0.1:9000/readyz",
		"[::]:8080":      "http://127.0.0.1:8080/readyz",
		"10.0.0.5:8080":  "http://10.0.0.5:8080/readyz",
		"localhost:8081": "http://localhost:8081/readyz",
	} {
		require.Equal(t, want, readyURL(listen), listen)
	}
}

func TestTheHealthCommandSaysWhetherTheHubIsReady(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	defer srv.Close()
	require.True(t, probe(srv.URL))
	status = http.StatusServiceUnavailable
	require.False(t, probe(srv.URL))
	srv.Close()
	require.False(t, probe(srv.URL), "nobody listening")
}
