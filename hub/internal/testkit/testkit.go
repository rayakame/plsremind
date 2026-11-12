// Package testkit provides the HTTP test environment: a real database per test,
// a fake clock and a client that checks every exchange against the OpenAPI spec.
package testkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/gateway"
	"github.com/rayakame/plsremind/hub/internal/server"
	"github.com/rayakame/plsremind/hub/internal/store"
)

type J = map[string]any

// Raw is a request body sent as-is instead of being JSON encoded.
type Raw string

const (
	PublicURL    = "https://hub.test"
	templateName = "plsremind_template"
)

var (
	baseURL   string
	dbCounter atomic.Int64

	specOnce      sync.Once
	specValidator validator.Validator
	specErr       error
)

// Main starts one PostgreSQL container for the whole test binary.
func Main(m *testing.M) {
	ctx := context.Background()
	container, err := postgres.Run(ctx, "postgres:18-alpine",
		postgres.WithUsername("plsremind"),
		postgres.WithPassword("plsremind"),
		postgres.WithDatabase("postgres"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Fatalf("start postgres: %v", err)
	}
	baseURL, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("connection string: %v", err)
	}
	if err := exec(ctx, "CREATE DATABASE "+templateName); err != nil {
		log.Fatalf("create template: %v", err)
	}
	if err := store.Migrate(ctx, urlFor(templateName)); err != nil {
		log.Fatalf("migrate template: %v", err)
	}
	code := m.Run()
	_ = testcontainers.TerminateContainer(container)
	os.Exit(code)
}

func urlFor(database string) string {
	return strings.Replace(baseURL, "/postgres?", "/"+database+"?", 1)
}

func exec(ctx context.Context, sql string) error {
	conn, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, sql)
	return err
}

type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type Env struct {
	App   *fiber.App
	Pool  *pgxpool.Pool
	Deps  *core.Deps
	Clock *FakeClock

	// Hub is the WebSocket gateway, already running.
	Hub *gateway.Hub

	admin   *Client
	dbURL   string
	baseURL string
}

// Deps is the hub's dependency set, for tests that change it before the server is built.
type Deps = core.Deps

// New returns an environment with its own freshly migrated database.
func New(t *testing.T) *Env {
	t.Helper()
	return NewWith(t, nil)
}

// NewWith is New with a chance to change settings that are read when the server is built.
func NewWith(t *testing.T, configure func(d *Deps)) *Env {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("t_%d_%d", os.Getpid(), dbCounter.Add(1))
	require.NoError(t, exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, templateName)))
	pool, err := store.Open(ctx, urlFor(name))
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Close()
		_ = exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
	})

	clock := &FakeClock{now: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)}
	d := &core.Deps{
		Cfg:  core.Config{PublicURL: PublicURL, SecretKey: [32]byte{1, 2, 3}},
		Pool: pool,
		Now:  clock.Now,
		HTTP: &http.Client{Timeout: 5 * time.Second},
	}
	// The gateway runs on real time. Short timers keep its tests fast.
	d.Gateway = core.GatewayConfig{
		HeartbeatInterval: 400 * time.Millisecond,
		IdentifyTimeout:   300 * time.Millisecond,
		SessionTTL:        500 * time.Millisecond,
		WriteTimeout:      2 * time.Second,
		// Test clients send a heartbeat every 100 ms, far more than a real client may.
		// That leaves half a second of slack before a slow machine costs them the connection.
		MaxClientMessages: 100000,
	}
	// Every test client comes from 127.0.0.1; the production limit per address would get in the way.
	d.RateAnon = api.NewLimiter(100000, time.Minute, clock.Now)
	if configure != nil {
		configure(d)
	}
	srv := server.New(d)
	srv.Gateway.Start(ctx)
	t.Cleanup(srv.Gateway.Shutdown)
	return &Env{App: srv.App, Hub: srv.Gateway, Pool: pool, Deps: d, Clock: clock, dbURL: urlFor(name)}
}

// Listen serves the app on a real socket and returns its base URL.
func (e *Env) Listen(t *testing.T) string {
	t.Helper()
	if e.baseURL != "" {
		return e.baseURL
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() {
		_ = e.App.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true})
	}()
	t.Cleanup(func() { _ = e.App.Shutdown() })
	e.baseURL = "http://" + ln.Addr().String()
	return e.baseURL
}

type reqConfig struct {
	contract bool
	headers  http.Header
	cookies  []*http.Cookie
}

type ReqOpt func(*reqConfig)

// NoContract skips the OpenAPI check, for requests that are deliberately outside the spec.
func NoContract() ReqOpt {
	return func(c *reqConfig) { c.contract = false }
}

func Header(key, value string) ReqOpt {
	return func(c *reqConfig) { c.headers.Set(key, value) }
}

func Cookie(name, value string) ReqOpt {
	return func(c *reqConfig) { c.cookies = append(c.cookies, &http.Cookie{Name: name, Value: value}) }
}

func Bearer(token string) ReqOpt {
	return Header("Authorization", "Bearer "+token)
}

type Resp struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r *Resp) Get(path string) gjson.Result {
	return gjson.GetBytes(r.Body, path)
}

// Cookie returns the value of a cookie set by the response, or "".
func (r *Resp) Cookie(name string) string {
	for _, c := range (&http.Response{Header: r.Header}).Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func isWrite(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

// Do sends a request to /api/v1 + path and checks the exchange against the OpenAPI spec.
func (e *Env) Do(t *testing.T, method, path string, body any, opts ...ReqOpt) *Resp {
	t.Helper()
	cfg := reqConfig{contract: true, headers: http.Header{}}
	if isWrite(method) {
		cfg.headers.Set("Origin", PublicURL)
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	var payload []byte
	switch b := body.(type) {
	case nil:
	case Raw:
		payload = []byte(b)
	default:
		var err error
		payload, err = json.Marshal(b)
		require.NoError(t, err)
	}

	build := func() *http.Request {
		req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(payload))
		req.Header = cfg.headers.Clone()
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for _, c := range cfg.cookies {
			req.AddCookie(c)
		}
		return req
	}

	res, err := e.App.Test(build(), fiber.TestConfig{Timeout: 30 * time.Second})
	require.NoError(t, err)
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	resp := &Resp{Status: res.StatusCode, Header: res.Header, Body: raw}

	if cfg.contract {
		checkContract(t, build(), resp)
	}
	return resp
}

// SpecPath is the location of the OpenAPI document in the repository.
func SpecPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "api", "openapi.yaml")
}

func loadSpec() (validator.Validator, error) {
	specOnce.Do(func() {
		raw, err := os.ReadFile(SpecPath())
		if err != nil {
			specErr = err
			return
		}
		doc, err := libopenapi.NewDocument(raw)
		if err != nil {
			specErr = err
			return
		}
		v, errs := validator.NewValidator(doc)
		if len(errs) > 0 {
			specErr = fmt.Errorf("openapi: %v", errs)
			return
		}
		specValidator = v
	})
	return specValidator, specErr
}

var contractMu sync.Mutex

func checkContract(t *testing.T, req *http.Request, resp *Resp) {
	t.Helper()
	v, err := loadSpec()
	require.NoError(t, err)

	res := &http.Response{
		StatusCode: resp.Status,
		Header:     resp.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(resp.Body)),
		Request:    req,
	}
	contractMu.Lock()
	defer contractMu.Unlock()
	var problems []string
	// A rejected request is allowed to be malformed, so only its response is checked.
	if resp.Status < 400 {
		if ok, errs := v.ValidateHttpRequestSync(req); !ok {
			for _, e := range errs {
				problems = append(problems, "request: "+e.Error())
			}
		}
	}
	if ok, errs := v.ValidateHttpResponse(req, res); !ok {
		for _, e := range errs {
			problems = append(problems, "response: "+e.Error())
		}
	}
	require.Emptyf(t, problems, "%s %s -> %d does not match openapi.yaml\nbody: %s", req.Method, req.URL.Path, resp.Status, resp.Body)
}
