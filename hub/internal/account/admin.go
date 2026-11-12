package account

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/httpx"
	"github.com/rayakame/plsremind/hub/internal/secret"
	"github.com/rayakame/plsremind/hub/internal/signal"
)

func errLastAdmin() error {
	return apierr.Conflict("last_admin", "The instance needs at least one active admin.")
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
	if db.IsNotFound(err) {
		return sqlc.User{}, apierr.NotFound()
	}
	return user, err
}

// Revoker withdraws the hub's grants at login providers.
type Revoker interface {
	RevokeAll(ctx context.Context, identities []sqlc.Identity)
}

// DeleteUser removes a user and everything that belongs to them, then revokes
// the tokens the hub held for their linked accounts.
func DeleteUser(ctx context.Context, d *deps.Deps, revoker Revoker, id uuid.UUID) error {
	var identities []sqlc.Identity
	err := db.Tx(ctx, d.Pool, func(q *sqlc.Queries) error {
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
		return signal.ForUser(ctx, q, signal.User, id)
	})
	if err != nil {
		return err
	}
	revoker.RevokeAll(ctx, identities)
	return nil
}

type AdminUser struct {
	User
	Disabled bool `json:"disabled"`
}

func adminView(u sqlc.User) AdminUser {
	return AdminUser{User: View(u), Disabled: u.Disabled}
}

func getSettings(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		settings, err := d.Q().GetSettings(c.Context())
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"name": settings.Name, "registration": settings.Registration})
	}
}

func patchSettings(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Name         *string `json:"name"`
			Registration *string `json:"registration"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		var out sqlc.UpdateSettingsRow
		err := db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
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
				return apierr.Validation(fields)
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

func listUsers(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		page, err := httpx.ParsePage(c)
		if err != nil {
			return err
		}
		var after uuid.UUID
		if page.Cursor != "" {
			if err := httpx.DecodeCursor(page.Cursor, &after); err != nil {
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
		return c.JSON(httpx.NewList(users, page.Limit, func(last AdminUser) any { return last.ID }))
	}
}

func patchUserAccess(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			Role     *string `json:"role"`
			Disabled *bool   `json:"disabled"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		if in.Role != nil && *in.Role != "user" && *in.Role != "admin" {
			return apierr.Field("role", `Must be "user" or "admin".`)
		}
		var updated sqlc.User
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
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
			return signal.ForUser(c.Context(), q, signal.User, id)
		})
		if err != nil {
			return err
		}
		return c.JSON(adminView(updated))
	}
}

func resetPassword(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			NewPassword string `json:"new_password"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		fields := map[string]string{}
		checkPassword(fields, "new_password", in.NewPassword)
		if len(fields) > 0 {
			return apierr.Validation(fields)
		}
		hash, err := secret.HashPassword(in.NewPassword)
		if err != nil {
			return err
		}
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			if _, err := getUser(c.Context(), q, id); err != nil {
				return err
			}
			identity, err := q.GetPasswordIdentity(c.Context(), id)
			if db.IsNotFound(err) {
				return apierr.Conflict("no_password_identity", "This user has no password to reset.")
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
			return signal.ForUser(c.Context(), q, signal.User, id)
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

func adminDeleteUser(d *deps.Deps, revoker Revoker) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		if err := DeleteUser(c.Context(), d, revoker, id); err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
