package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
)

// maxFrame is plenty for the three small messages a client may send.
const maxFrame = 4096

func (h *Hub) Register(api fiber.Router) {
	api.Get("/gateway", func(c fiber.Ctx) error {
		if !websocket.IsWebSocketUpgrade(c) {
			return apierr.New(http.StatusUpgradeRequired, "upgrade_required", "This address only speaks WebSocket.")
		}
		return c.Next()
	}, websocket.New(h.serve))
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// serve runs one connection from Hello until it ends.
func (h *Hub) serve(ws *websocket.Conn) {
	cfg := h.d.Gateway
	c := newConn(ws, cfg.WriteTimeout)
	cookie := ws.Cookies(auth.SessionCookie)
	sameOrigin := ws.Headers(fiber.HeaderOrigin) == h.d.Cfg.PublicURL
	ctx := context.Background()

	var session *Session
	defer func() {
		if session != nil {
			session.detach(c)
		}
		c.close(websocket.CloseNormalClosure, "")
	}()

	ws.SetReadLimit(maxFrame)
	c.send(encode(OpHello, fiber.Map{"heartbeat_interval": cfg.HeartbeatInterval.Milliseconds()}, nil, nil))
	// Until the client has identified, the clock runs from the moment it connected.
	_ = ws.SetReadDeadline(time.Now().Add(cfg.IdentifyTimeout))

	windowStart, messages := time.Now(), 0
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			if isTimeout(err) {
				c.close(CloseTimeout, "no heartbeat")
			}
			return
		}
		if time.Since(windowStart) > time.Minute {
			windowStart, messages = time.Now(), 0
		}
		if messages++; messages > h.d.Gateway.MaxClientMessages {
			c.close(CloseRateLimited, "too many messages")
			return
		}
		var in inbound
		if err := json.Unmarshal(data, &in); err != nil {
			c.close(CloseDecode, "not a gateway message")
			return
		}

		switch in.Op {
		case OpHeartbeat:
			c.send(encode(OpHeartbeatACK, nil, nil, nil))
			if session != nil {
				_ = ws.SetReadDeadline(time.Now().Add(cfg.HeartbeatInterval * 3 / 2))
			}
		case OpIdentify:
			if session != nil {
				c.close(CloseAlreadyAuthenticated, "already identified")
				return
			}
			var code int
			if session, code = h.identify(ctx, c, in.D, cookie, sameOrigin); code != 0 {
				c.close(code, "identify refused")
				return
			}
			_ = ws.SetReadDeadline(time.Now().Add(cfg.HeartbeatInterval * 3 / 2))
		default:
			c.close(CloseUnknownOp, "unknown opcode")
			return
		}
	}
}
