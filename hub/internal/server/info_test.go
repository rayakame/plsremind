package server_test

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestInfo(t *testing.T) {
	e := testkit.New(t)
	r := e.Do(t, "GET", "/info", nil)
	require.Equal(t, 200, r.Status)
	require.Equal(t, "plsremind", r.Get("name").String())
	require.Equal(t, "dev", r.Get("version").String())
}

func TestUnknownRouteIsJSON404(t *testing.T) {
	r := testkit.New(t).Do(t, "GET", "/nope", nil, testkit.NoContract())
	require.Equal(t, 404, r.Status)
	require.Equal(t, "not_found", r.Get("error.code").String())
}

func TestOversizedBodyIs413(t *testing.T) {
	base := testkit.New(t).Listen(t)
	conn, err := net.Dial("tcp", strings.TrimPrefix(base, "http://"))
	require.NoError(t, err)
	defer conn.Close()
	// Only the headers are sent: the server must refuse by the announced length alone.
	_, err = fmt.Fprintf(conn, "POST /api/v1/info HTTP/1.1\r\nHost: hub.test\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", 2<<20)
	require.NoError(t, err)
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, 413, res.StatusCode)
	require.Equal(t, "payload_too_large", gjson.GetBytes(raw, "error.code").String())
}
