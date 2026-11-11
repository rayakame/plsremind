package httpapi_test

import (
	"io"
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
	body := `{"x":"` + strings.Repeat("a", 2<<20) + `"}`
	res, err := http.Post(base+"/api/v1/info", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, 413, res.StatusCode)
	require.Equal(t, "payload_too_large", gjson.GetBytes(raw, "error.code").String())
}
