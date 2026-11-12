package gateway

import (
	"sync"
	"time"

	"github.com/rayakame/plsremind/hub/internal/auth"
)

type sequenced struct {
	seq   int64
	frame []byte
}

// Session is what a client identified as. It outlives its connection for a
// while so that the client can come back and continue where it left off.
type Session struct {
	id        string
	hub       *Hub
	principal auth.Principal
	intents   []string
	// token and cookie are kept to check again later whether the client may still be here.
	token  string
	cookie string

	mu         sync.Mutex
	conn       *conn
	seq        int64
	buffer     []sequenced
	detachedAt time.Time
}

func (s *Session) wants(intent string) bool {
	for _, have := range s.intents {
		if have == intent {
			return true
		}
	}
	return false
}

// emit sends a dispatch. Sequenced ones get the next number and are kept for resuming.
func (s *Session) emit(event string, numbered bool, data any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var seq *int64
	if numbered {
		s.seq++
		n := s.seq
		seq = &n
	}
	frame := encode(OpDispatch, data, seq, &event)
	if numbered {
		s.buffer = append(s.buffer, sequenced{seq: s.seq, frame: frame})
		if limit := s.hub.d.Gateway.BufferSize; len(s.buffer) > limit {
			s.buffer = s.buffer[len(s.buffer)-limit:]
		}
	}
	if s.conn != nil {
		s.conn.send(frame)
	}
}

// detach is called when the connection ends. The session stays for SessionTTL.
func (s *Session) detach(c *conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == c {
		s.conn = nil
		s.detachedAt = time.Now()
	}
}

// expired reports whether the session has been without a connection for too long.
func (s *Session) expired(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn == nil && now.Sub(s.detachedAt) > s.hub.d.Gateway.SessionTTL
}

// end closes the session's connection, if any, with the given code.
func (s *Session) end(code int, reason string) {
	s.mu.Lock()
	c := s.conn
	s.conn = nil
	s.detachedAt = time.Time{}
	s.mu.Unlock()
	if c != nil {
		c.close(code, reason)
	}
}
