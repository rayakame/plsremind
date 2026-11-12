package account

import (
	"net/http"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

func registerOAuth(root, user, admin fiber.Router, d *core.Deps, svc *OAuth) {
	root.Get("/auth/oauth/:provider/start", oauthStart(d, svc))
	root.Get("/auth/oauth/:provider/callback", oauthCallback(d, svc))

	user.Delete("/identities/:id", auth.RequireSession(), unlinkIdentity(d, svc))

	admin.Get("/providers", listProviders(d))
	admin.Put("/providers/:key", putProvider(d))
	admin.Delete("/providers/:key", deleteProvider(d))
}

func unlinkIdentity(d *core.Deps, svc *OAuth) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		var removed sqlc.Identity
		err = store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			// The row lock keeps two unlinks from each seeing the other identity as the remaining one.
			if _, err := q.LockUser(c.Context(), userID); err != nil {
				return err
			}
			removed, err = q.GetUserIdentity(c.Context(), sqlc.GetUserIdentityParams{ID: id, UserID: userID})
			if store.IsNotFound(err) {
				return api.NotFound()
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
				return api.Conflict("last_identity", "Without this you could no longer log in. Link another account that allows login first.")
			}
			if err := q.DeleteIdentity(c.Context(), id); err != nil {
				return err
			}
			return events.NotifyUser(c.Context(), q, events.SignalUser, userID)
		})
		if err != nil {
			return err
		}
		svc.RevokeAll(c.Context(), []sqlc.Identity{removed})
		return c.SendStatus(http.StatusNoContent)
	}
}
