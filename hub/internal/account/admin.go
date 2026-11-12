package account

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

func errLastAdmin() error {
	return api.Conflict("last_admin", "The instance needs at least one active admin.")
}

func isActiveAdmin(u sqlc.User) bool {
	return u.Role == "admin" && !u.Disabled
}

// lockedUser takes the lock that serializes every change to who is an admin and
// only then reads the user. Reading first would let a change that is still in
// flight slip past the last-admin check.
func lockedUser(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (sqlc.User, error) {
	if err := q.AdminLock(ctx); err != nil {
		return sqlc.User{}, err
	}
	return getUser(ctx, q, id)
}

// guardLastAdmin fails when user is the only active admin. The caller holds the admin lock.
func guardLastAdmin(ctx context.Context, q *sqlc.Queries, user sqlc.User) error {
	if !isActiveAdmin(user) {
		return nil
	}
	admins, err := q.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}
	if admins <= 1 {
		return errLastAdmin()
	}
	return nil
}

func getUser(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (sqlc.User, error) {
	user, err := q.GetUser(ctx, id)
	if store.IsNotFound(err) {
		return sqlc.User{}, api.NotFound()
	}
	return user, err
}

// DeleteUser removes a user and everything that belongs to them, then revokes
// the tokens the hub held for their linked accounts.
func DeleteUser(ctx context.Context, d *core.Deps, svc *OAuth, id uuid.UUID) error {
	var identities []sqlc.Identity
	remove := func(q *sqlc.Queries) error {
		user, err := lockedUser(ctx, q, id)
		if err != nil {
			return err
		}
		if err := guardLastAdmin(ctx, q, user); err != nil {
			return err
		}
		identities, err = q.ListIdentities(ctx, id)
		if err != nil {
			return err
		}
		if _, err = q.DeleteUser(ctx, id); err != nil {
			return err
		}
		return events.NotifyUser(ctx, q, events.SignalUser, id)
	}
	// Deleting a user touches rows the reminder ticker may hold at that moment.
	// PostgreSQL then stops one of the two; trying again is all it takes.
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = store.Tx(ctx, d.Pool, remove); !store.IsDeadlock(err) {
			break
		}
	}
	if err != nil {
		return err
	}
	svc.RevokeAll(ctx, identities)
	return nil
}

type AdminUser struct {
	User
	Disabled bool `json:"disabled"`
}

func adminView(u sqlc.User) AdminUser {
	return AdminUser{User: View(u), Disabled: u.Disabled}
}

func getSettings(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		settings, err := d.Q().GetSettings(c.Context())
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"name": settings.Name, "registration": settings.Registration})
	}
}

func patchSettings(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Name         *string `json:"name"`
			Registration *string `json:"registration"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		var out sqlc.UpdateSettingsRow
		err := store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			current, err := q.GetSettings(c.Context())
			if err != nil {
				return err
			}
			params := sqlc.UpdateSettingsParams{Name: current.Name, Registration: current.Registration}
			fields := map[string]string{}
			if in.Name != nil {
				params.Name = strings.TrimSpace(*in.Name)
				if n := utf8.RuneCountInString(params.Name); n < 1 || n > 64 {
					fields["name"] = "Must be 1 to 64 characters."
				}
			}
			if in.Registration != nil {
				params.Registration = *in.Registration
				if params.Registration != "open" && params.Registration != "invite" {
					fields["registration"] = `Must be "open" or "invite".`
				}
			}
			if len(fields) > 0 {
				return api.Validation(fields)
			}
			out, err = q.UpdateSettings(c.Context(), params)
			return err
		})
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"name": out.Name, "registration": out.Registration})
	}
}

func listUsers(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		page, err := api.ParsePage(c)
		if err != nil {
			return err
		}
		var after uuid.UUID
		if page.Cursor != "" {
			if err := api.DecodeCursor(page.Cursor, &after); err != nil {
				return err
			}
		}
		rows, err := d.Q().ListUsers(c.Context(), sqlc.ListUsersParams{After: after, PageSize: int32(page.Limit + 1)})
		if err != nil {
			return err
		}
		users := make([]AdminUser, 0, len(rows))
		for _, row := range rows {
			users = append(users, adminView(row))
		}
		return c.JSON(api.NewList(users, page.Limit, func(last AdminUser) any { return last.ID }))
	}
}

func patchUserAccess(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			Role     *string `json:"role"`
			Disabled *bool   `json:"disabled"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		if in.Role != nil && *in.Role != "user" && *in.Role != "admin" {
			return api.Field("role", `Must be "user" or "admin".`)
		}
		var updated sqlc.User
		err = store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			user, err := lockedUser(c.Context(), q, id)
			if err != nil {
				return err
			}
			params := sqlc.UpdateUserAccessParams{ID: id, Role: user.Role, Disabled: user.Disabled}
			if in.Role != nil {
				params.Role = *in.Role
			}
			if in.Disabled != nil {
				params.Disabled = *in.Disabled
			}
			if params.Role != "admin" || params.Disabled {
				if err := guardLastAdmin(c.Context(), q, user); err != nil {
					return err
				}
			}
			if params.Disabled {
				if err := q.DeleteUserSessions(c.Context(), id); err != nil {
					return err
				}
			}
			updated, err = q.UpdateUserAccess(c.Context(), params)
			if err != nil {
				return err
			}
			// The user's own clients show the role, so they are told like about any other profile change.
			admin, _ := auth.From(c)
			if err := events.Append(c.Context(), q, d.Now(), id, auth.ActorFrom(admin), "user.updated", View(updated)); err != nil {
				return err
			}
			return events.NotifyUser(c.Context(), q, events.SignalUser, id)
		})
		if err != nil {
			return err
		}
		return c.JSON(adminView(updated))
	}
}

func resetPassword(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			NewPassword string `json:"new_password"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		fields := map[string]string{}
		checkPassword(fields, "new_password", in.NewPassword)
		if len(fields) > 0 {
			return api.Validation(fields)
		}
		hash, err := core.HashPassword(in.NewPassword)
		if err != nil {
			return err
		}
		err = store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			if _, err := getUser(c.Context(), q, id); err != nil {
				return err
			}
			identity, err := q.GetPasswordIdentity(c.Context(), id)
			if store.IsNotFound(err) {
				return api.Conflict("no_password_identity", "This user has no password to reset.")
			}
			if err != nil {
				return err
			}
			if err := q.UpdatePasswordHash(c.Context(), sqlc.UpdatePasswordHashParams{ID: identity.ID, PasswordHash: &hash}); err != nil {
				return err
			}
			if err := q.DeleteUserPersonalTokens(c.Context(), id); err != nil {
				return err
			}
			if err := q.DeleteUserSessions(c.Context(), id); err != nil {
				return err
			}
			return events.NotifyUser(c.Context(), q, events.SignalUser, id)
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

func adminDeleteUser(d *core.Deps, svc *OAuth) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		if err := DeleteUser(c.Context(), d, svc, id); err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
