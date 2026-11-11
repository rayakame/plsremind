package deps

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rayakame/plsremind/hub/internal/config"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
)

type Deps struct {
	Cfg  config.Config
	Pool *pgxpool.Pool
	Now  func() time.Time
	HTTP *http.Client
}

func (d *Deps) Q() *sqlc.Queries {
	return sqlc.New(d.Pool)
}
