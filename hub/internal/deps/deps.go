package deps

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rayakame/plsremind/hub/internal/config"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/ratelimit"
	"github.com/rayakame/plsremind/hub/internal/secret"
)

type Deps struct {
	Cfg  config.Config
	Pool *pgxpool.Pool
	Now  func() time.Time
	HTTP *http.Client

	// RateAuth limits sessions and personal tokens per credential, RateConn
	// connections per token, RateAnon everyone else per IP.
	RateAuth *ratelimit.Limiter
	RateConn *ratelimit.Limiter
	RateAnon *ratelimit.Limiter

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
func (d *Deps) Box() *secret.Box {
	return secret.NewBox(d.Cfg.SecretKey)
}

func (d *Deps) Q() *sqlc.Queries {
	return sqlc.New(d.Pool)
}
