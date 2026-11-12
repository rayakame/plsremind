package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/gateway"
)

// Frame is one message from the gateway.
type Frame struct {
	Op int
	T  string
	S  int64
	D  gjson.Result
}

func (f Frame) Get(path string) gjson.Result {
	return f.D.Get(path)
}

// GatewayClient is a WebSocket client for tests.
type GatewayClient struct {
	env  *Env
	conn *websocket.Conn

	// Ready is the READY dispatch, for clients made by Gateway.
	Ready     Frame
	SessionID string
	// LastSeq is the highest sequence number seen so far.
	LastSeq int64
	// Seen holds every dispatch read so far, in order.
	Seen []Frame

	frames    chan Frame
	closeCode chan int
	writeMu   sync.Mutex
	stopBeat  chan struct{}
	beatOnce  sync.Once
	pause     chan struct{}
	pauseOnce sync.Once
}

// Pause makes the client stop reading, the way a hung program would.
func (g *GatewayClient) Pause() {
	g.StopHeartbeat()
	g.pauseOnce.Do(func() { close(g.pause) })
}

// ExpectEnded requires the connection to end, however it ends.
func (g *GatewayClient) ExpectEnded(t *testing.T) {
	t.Helper()
	select {
	case <-g.pause:
		// A paused reader never sees the end; ask the socket directly.
		_ = g.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		for {
			if _, _, err := g.conn.ReadMessage(); err != nil {
				var netErr interface{ Timeout() bool }
				require.False(t, errors.As(err, &netErr) && netErr.Timeout(), "the gateway kept the connection open")
				return
			}
		}
	default:
	}
	select {
	case <-g.closeCode:
	case <-time.After(5 * time.Second):
		t.Fatal("the gateway kept the connection open")
	}
}

// RestartGateway stops the gateway and starts it again, as a restart of the hub would.
func (e *Env) RestartGateway(t *testing.T) {
	t.Helper()
	e.Hub.Shutdown()
	e.Hub.Start(context.Background())
}

func (e *Env) dial(t *testing.T, opts ...ReqOpt) *GatewayClient {
	t.Helper()
	cfg := reqConfig{headers: http.Header{}}
	for _, opt := range opts {
		opt(&cfg)
	}
	for _, c := range cfg.cookies {
		cfg.headers.Add("Cookie", c.String())
	}
	url := "ws" + strings.TrimPrefix(e.Listen(t), "http") + "/api/v1/gateway"
	conn, res, err := websocket.DefaultDialer.Dial(url, cfg.headers)
	if res != nil && res.Body != nil {
		res.Body.Close()
	}
	require.NoError(t, err)
	g := &GatewayClient{env: e, conn: conn, frames: make(chan Frame, 4096), closeCode: make(chan int, 1), stopBeat: make(chan struct{}), pause: make(chan struct{})}
	t.Cleanup(g.Close)
	go g.read()
	return g
}

func (g *GatewayClient) read() {
	defer close(g.frames)
	for {
		select {
		case <-g.pause:
			return
		default:
		}
		_, data, err := g.conn.ReadMessage()
		if err != nil {
			code := -1
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				code = closeErr.Code
			}
			g.closeCode <- code
			return
		}
		parsed := gjson.ParseBytes(data)
		g.frames <- Frame{Op: int(parsed.Get("op").Int()), T: parsed.Get("t").String(), S: parsed.Get("s").Int(), D: parsed.Get("d")}
	}
}

// GatewayRaw connects without identifying and without sending heartbeats.
func (e *Env) GatewayRaw(t *testing.T, opts ...ReqOpt) *GatewayClient {
	t.Helper()
	return e.dial(t, opts...)
}

// Gateway connects, identifies with the token and intents, and keeps the
// connection alive with heartbeats in the background.
func (e *Env) Gateway(t *testing.T, token string, intents ...string) *GatewayClient {
	t.Helper()
	g := e.dial(t)
	require.Equal(t, gateway.OpHello, g.Next(t).Op)
	g.Send(t, gateway.OpIdentify, J{"token": token, "intents": intents})
	g.Ready = g.Expect(t, gateway.EventReady)
	g.SessionID = g.Ready.Get("session_id").String()
	g.beat()
	return g
}

// ResumeGateway connects and resumes an earlier session from the given sequence number.
func (e *Env) ResumeGateway(t *testing.T, token, sessionID string, seq int64) *GatewayClient {
	t.Helper()
	g := e.dial(t)
	require.Equal(t, gateway.OpHello, g.Next(t).Op)
	g.Send(t, gateway.OpResume, J{"token": token, "session_id": sessionID, "seq": seq})
	g.SessionID = sessionID
	g.LastSeq = seq
	g.beat()
	return g
}

func (g *GatewayClient) beat() {
	interval := g.env.Deps.Gateway.HeartbeatInterval / 2
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-g.stopBeat:
				return
			case <-ticker.C:
				if g.write(J{"op": gateway.OpHeartbeat, "d": nil}) != nil {
					return
				}
			}
		}
	}()
}

// StopHeartbeat ends the background heartbeats, for tests that send their own.
func (g *GatewayClient) StopHeartbeat() {
	g.beatOnce.Do(func() { close(g.stopBeat) })
}

func (g *GatewayClient) write(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	return g.conn.WriteMessage(websocket.TextMessage, raw)
}

func (g *GatewayClient) Send(t *testing.T, op int, d any) {
	t.Helper()
	require.NoError(t, g.write(J{"op": op, "d": d}))
}

func (g *GatewayClient) SendRaw(t *testing.T, text string) {
	t.Helper()
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	require.NoError(t, g.conn.WriteMessage(websocket.TextMessage, []byte(text)))
}

func (g *GatewayClient) next(timeout time.Duration, skipACK bool) (Frame, bool) {
	deadline := time.After(timeout)
	for {
		select {
		case f, ok := <-g.frames:
			if !ok {
				return Frame{}, false
			}
			if skipACK && f.Op == gateway.OpHeartbeatACK {
				continue
			}
			if f.Op == gateway.OpDispatch {
				g.Seen = append(g.Seen, f)
				if f.S > g.LastSeq {
					g.LastSeq = f.S
				}
			}
			return f, true
		case <-deadline:
			return Frame{}, false
		}
	}
}

// Next returns the next message, including heartbeat acknowledgements.
func (g *GatewayClient) Next(t *testing.T) Frame {
	t.Helper()
	f, ok := g.next(3*time.Second, false)
	require.True(t, ok, "no gateway message arrived")
	return f
}

// Expect returns the next dispatch and requires it to be of the given type.
func (g *GatewayClient) Expect(t *testing.T, event string) Frame {
	t.Helper()
	f, ok := g.next(3*time.Second, true)
	require.True(t, ok, "expected %s, but nothing arrived", event)
	require.Equal(t, gateway.OpDispatch, f.Op, "expected %s, got op %d: %s", event, f.Op, f.D.Raw)
	require.Equal(t, event, f.T, f.D.Raw)
	return f
}

// NextEvent returns the next dispatch of any type.
func (g *GatewayClient) NextEvent(t *testing.T) Frame {
	t.Helper()
	f, ok := g.next(3*time.Second, true)
	require.True(t, ok, "no dispatch arrived")
	require.Equal(t, gateway.OpDispatch, f.Op, f.D.Raw)
	return f
}

// NextOp returns the next message that is not a heartbeat acknowledgement.
func (g *GatewayClient) NextOp(t *testing.T) Frame {
	t.Helper()
	f, ok := g.next(3*time.Second, true)
	require.True(t, ok, "no gateway message arrived")
	return f
}

// ExpectQuiet requires that no dispatch arrives for a while.
func (g *GatewayClient) ExpectQuiet(t *testing.T) {
	t.Helper()
	f, ok := g.next(250*time.Millisecond, true)
	require.False(t, ok, "unexpected %s: %s", f.T, f.D.Raw)
}

// ExpectClose requires the hub to close the connection with the given code.
func (g *GatewayClient) ExpectClose(t *testing.T, code int) {
	t.Helper()
	select {
	case got := <-g.closeCode:
		require.Equal(t, code, got)
	case <-time.After(3 * time.Second):
		t.Fatalf("the gateway did not close the connection, expected code %d", code)
	}
}

func (g *GatewayClient) Close() {
	g.StopHeartbeat()
	_ = g.conn.Close()
}
