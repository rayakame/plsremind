package httpapi_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

var routeParam = regexp.MustCompile(`:([a-zA-Z_]+)`)

func TestEveryRouteIsDocumented(t *testing.T) {
	raw, err := os.ReadFile(testkit.SpecPath())
	require.NoError(t, err)
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &spec))

	e := testkit.New(t)
	seen := 0
	for _, route := range e.App.GetRoutes(true) {
		if !strings.HasPrefix(route.Path, "/api/v1/") || route.Method == "HEAD" {
			continue
		}
		seen++
		path := routeParam.ReplaceAllString(strings.TrimPrefix(route.Path, "/api/v1"), "{$1}")
		ops, ok := spec.Paths[path]
		require.Truef(t, ok, "path %s is missing from openapi.yaml", path)
		_, ok = ops[strings.ToLower(route.Method)]
		require.Truef(t, ok, "%s %s is missing from openapi.yaml", route.Method, path)
	}
	require.NotZero(t, seen)
}
