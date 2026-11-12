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
	fin          chan closing
	once         sync.Once
	writeTimeout time.Duration
}

type closing struct {
	code   int
	reason string
}

func newConn(ws *websocket.Conn, writeTimeout time.Duration) *conn {
	c := &conn{ws: ws, out: make(chan []byte, outQueue), done: make(chan struct{}), fin: make(chan closing, 1), writeTimeout: writeTimeout}
	go c.write()
	return c
}

func (c *conn) write() {
	for {
		select {
		case <-c.done:
			return
		case frame := <-c.out:
			if !c.writeFrame(frame) {
				return
			}
		case end := <-c.fin:
			// Send what is already queued, then say goodbye.
			for {
				select {
				case frame := <-c.out:
					if !c.writeFrame(frame) {
						return
					}
					continue
				default:
				}
				break
			}
			c.close(end.code, end.reason)
			return
		}
	}
}

func (c *conn) writeFrame(frame []byte) bool {
	_ = c.ws.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	if err := c.ws.WriteMessage(websocket.TextMessage, frame); err != nil {
		c.close(websocket.CloseAbnormalClosure, "")
		return false
	}
	return true
}

// finish closes the connection after everything queued so far has been written.
func (c *conn) finish(code int, reason string) {
	select {
	case c.fin <- closing{code, reason}:
	default:
	}
}

// send queues a frame. When the queue is full it waits for the writer to catch
// up, which slows down only the session that is sending. A client that does not
// take anything for a whole write timeout is too slow to keep.
func (c *conn) send(frame []byte) bool {
	select {
	case <-c.done:
		return false
	case c.out <- frame:
		return true
	default:
	}
	timer := time.NewTimer(c.writeTimeout)
	defer timer.Stop()
	select {
	case <-c.done:
		return false
	case c.out <- frame:
		return true
	case <-timer.C:
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
