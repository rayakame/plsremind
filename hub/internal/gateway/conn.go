package gateway

import (
	"sync"
	"time"

	"github.com/gofiber/contrib/v3/websocket"
)

const outQueue = 256

// conn is one WebSocket connection. Everything is written by its own goroutine
// with a deadline, so a client that stops reading can only stall itself.
type conn struct {
	ws           *websocket.Conn
	out          chan []byte
	done         chan struct{}
	once         sync.Once
	writeTimeout time.Duration
}

func newConn(ws *websocket.Conn, writeTimeout time.Duration) *conn {
	c := &conn{ws: ws, out: make(chan []byte, outQueue), done: make(chan struct{}), writeTimeout: writeTimeout}
	go c.write()
	return c
}

func (c *conn) write() {
	for {
		select {
		case <-c.done:
			return
		case frame := <-c.out:
			_ = c.ws.SetWriteDeadline(time.Now().Add(c.writeTimeout))
			if err := c.ws.WriteMessage(websocket.TextMessage, frame); err != nil {
				c.close(websocket.CloseAbnormalClosure, "")
				return
			}
		}
	}
}

// send queues a frame. A connection whose queue is full is too slow to keep and is closed.
func (c *conn) send(frame []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.out <- frame:
		return true
	default:
		c.close(CloseRateLimited, "too slow")
		return false
	}
}

func (c *conn) closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// close tells the client why and drops the connection. Safe to call more than once.
func (c *conn) close(code int, reason string) {
	c.once.Do(func() {
		close(c.done)
		deadline := time.Now().Add(time.Second)
		_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), deadline)
		_ = c.ws.Close()
	})
}
