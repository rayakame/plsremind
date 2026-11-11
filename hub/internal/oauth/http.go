package oauth

import (
	"net/http"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/httpx"
)

func Register(api, user, admin fiber.Router, d *deps.Deps, revoker *Revoker) {
	api.Get("/auth/oauth/:provider/start", start(d))
	api.Get("/auth/oauth/:provider/callback", callback(d))

	user.Delete("/identities/:id", auth.RequireSession(), unlink(d, revoker))

	admin.Get("/providers", listProviders(d))
	admin.Put("/providers/:key", putProvider(d))
	admin.Delete("/providers/:key", deleteProvider(d))
}

func unlink(d *deps.Deps, revoker *Revoker) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		var removed sqlc.Identity
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			// The row lock keeps two unlinks from each seeing the other identity as the remaining one.
			if _, err := q.LockUser(c.Context(), userID); err != nil {
				return err
			}
			removed, err = q.GetUserIdentity(c.Context(), sqlc.GetUserIdentityParams{ID: id, UserID: userID})
			if db.IsNotFound(err) {
				return apierr.NotFound()
			}
			if err != nil {
				return err
			}
			all, err := q.ListIdentities(c.Context(), userID)
			if err != nil {
				return err
			}
			if len(all) <= 1 {
				return apierr.Conflict("last_identity", "This is your only way to log in.")
			}
			return q.DeleteIdentity(c.Context(), id)
		})
		if err != nil {
			return err
		}
		revoker.RevokeAll(c.Context(), []sqlc.Identity{removed})
		return c.SendStatus(http.StatusNoContent)
	}
}
