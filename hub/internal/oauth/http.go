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

func Register(api, user, admin fiber.Router, d *deps.Deps, svc *Service) {
	api.Get("/auth/oauth/:provider/start", start(d, svc))
	api.Get("/auth/oauth/:provider/callback", callback(d, svc))

	user.Delete("/identities/:id", auth.RequireSession(), unlink(d, svc))

	admin.Get("/providers", listProviders(d))
	admin.Put("/providers/:key", putProvider(d))
	admin.Delete("/providers/:key", deleteProvider(d))
}

func unlink(d *deps.Deps, svc *Service) fiber.Handler {
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
			// What counts is whether a login is left, not whether an entry is: an
			// account linked at a provider with login turned off cannot get back in.
			usable, err := q.CountUsableLogins(c.Context(), sqlc.CountUsableLoginsParams{UserID: userID, ExceptID: id})
			if err != nil {
				return err
			}
			if usable == 0 {
				return apierr.Conflict("last_identity", "Without this you could no longer log in. Link another account that allows login first.")
			}
			return q.DeleteIdentity(c.Context(), id)
		})
		if err != nil {
			return err
		}
		svc.RevokeAll(c.Context(), []sqlc.Identity{removed})
		return c.SendStatus(http.StatusNoContent)
	}
}
