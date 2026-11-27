package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/server"
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

func TestTheVersionCommandPrintsTheBuildVersion(t *testing.T) {
	before := server.Version
	t.Cleanup(func() { server.Version = before })
	server.Version = "9.8.7"
	var out bytes.Buffer
	code, handled := command([]string{"plsremind", "version"}, &out)
	require.True(t, handled)
	require.Equal(t, 0, code)
	require.Equal(t, "9.8.7\n", out.String())

	_, handled = command([]string{"plsremind"}, &out)
	require.False(t, handled, "without a command the hub serves")
}
