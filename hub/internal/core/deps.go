// Package core holds what the whole hub is built on: configuration, shared
// dependencies and the handling of secrets.
package core

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

type Deps struct {
	Cfg  Config
	Pool *pgxpool.Pool
	Now  func() time.Time
	HTTP *http.Client

	// RateAuth limits sessions and personal tokens per credential, RateConn
	// connections per token, RateAnon everyone else per IP.
	RateAuth *api.Limiter
	RateConn *api.Limiter
	RateAnon *api.Limiter

	Gateway GatewayConfig

	// ReadTimeout and IdleTimeout bound plain HTTP requests. Zero means the defaults.
	ReadTimeout time.Duration
	IdleTimeout time.Duration

	// OAuthEndpoints replaces the built-in addresses of "github" and "discord", for tests.
	OAuthEndpoints map[string]OAuthEndpoints
}

type OAuthEndpoints struct {
	AuthURL   string
	TokenURL  string
	UserURL   string
	RevokeURL string
}

// Box encrypts secrets that are stored in the database.
func (d *Deps) Box() *Box {
	return NewBox(d.Cfg.SecretKey)
}

func (d *Deps) Q() *sqlc.Queries {
	return sqlc.New(d.Pool)
}

// Gateway holds the limits and timers of the WebSocket gateway.
type GatewayConfig struct {
	HeartbeatInterval time.Duration
	IdentifyTimeout   time.Duration
	// SessionTTL is how long a session can be resumed after its connection dropped.
	SessionTTL   time.Duration
	WriteTimeout time.Duration
	// BufferSize is how many events a session keeps for resuming.
	BufferSize int
	// MaxSessions limits the connected sessions of one credential, MaxUserSessions
	// those of one user across all of their tokens and logins.
	MaxSessions       int
	MaxUserSessions   int
	MaxClientMessages int
	// SyncChunk is how many items one part of a user.sync carries.
	SyncChunk int
}

// WithDefaults fills every unset field with its production value.
func (g GatewayConfig) WithDefaults() GatewayConfig {
	if g.HeartbeatInterval == 0 {
		g.HeartbeatInterval = 45 * time.Second
	}
	if g.IdentifyTimeout == 0 {
		g.IdentifyTimeout = 10 * time.Second
	}
	if g.SessionTTL == 0 {
		g.SessionTTL = 5 * time.Minute
	}
	if g.WriteTimeout == 0 {
		g.WriteTimeout = 10 * time.Second
	}
	if g.BufferSize == 0 {
		g.BufferSize = 5000
	}
	if g.MaxSessions == 0 {
		g.MaxSessions = 10
	}
	if g.MaxUserSessions == 0 {
		g.MaxUserSessions = 25
	}
	if g.MaxClientMessages == 0 {
		g.MaxClientMessages = 60
	}
	if g.SyncChunk == 0 {
		g.SyncChunk = 500
	}
	return g
}
