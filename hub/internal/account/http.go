package account

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/httpx"
	"github.com/rayakame/plsremind/hub/internal/secret"
)

// Register mounts the public account routes on api and the profile routes on user.
func Register(api, user, admin fiber.Router, d *deps.Deps, revoker Revoker) {
	api.Post("/setup", setup(d))
	api.Post("/auth/register", register(d))
	api.Post("/auth/login", login(d))
	api.Post("/auth/logout", logout(d))

	user.Get("", getProfile(d))
	user.Patch("", patchProfile(d))
	user.Delete("", auth.RequireSession(), deleteProfile(d, revoker))
	user.Get("/identities", listIdentities(d))
	user.Put("/password", auth.RequireSession(), setPassword(d))

	admin.Get("/settings", getSettings(d))
	admin.Patch("/settings", patchSettings(d))
	admin.Get("/invites", listInvites(d))
	admin.Post("/invites", createInvite(d))
	admin.Delete("/invites/:id", deleteInvite(d))
	admin.Get("/users", listUsers(d))
	admin.Patch("/users/:id", patchUserAccess(d))
	admin.Delete("/users/:id", adminDeleteUser(d, revoker))
	admin.Put("/users/:id/password", resetPassword(d))
}

func errLoginDisabled() error {
	return apierr.New(http.StatusForbidden, "login_disabled", "Password login is turned off on this instance.")
}

type credentials struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Timezone    string `json:"timezone"`
}

// normalize trims the input and validates it for creating an account.
func (in *credentials) normalize(ctx context.Context, q *sqlc.Queries) error {
	in.Username = NormalizeUsername(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	fields := map[string]string{}
	checkUsername(fields, in.Username)
	checkPassword(fields, "password", in.Password)
	checkDisplayName(fields, in.DisplayName)
	checkTimezone(ctx, q, fields, in.Timezone)
	if len(fields) > 0 {
		return apierr.Validation(fields)
	}
	return nil
}

func setup(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in credentials
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		if err := in.normalize(c.Context(), d.Q()); err != nil {
			return err
		}
		var user sqlc.User
		err := db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			if err := q.SetupLock(c.Context()); err != nil {
				return err
			}
			count, err := q.CountUsers(c.Context())
			if err != nil {
				return err
			}
			if count > 0 {
				return apierr.Conflict("already_set_up", "This instance is already set up.")
			}
			now := d.Now()
			user, err = CreateUser(c.Context(), q, now, NewUser{DisplayName: in.DisplayName, Timezone: in.Timezone, Role: "admin"})
			if err != nil {
				return err
			}
			if err := AddPassword(c.Context(), q, now, user.ID, in.Username, in.Password); err != nil {
				return err
			}
			return auth.StartSession(c, d, q, user.ID)
		})
		if err != nil {
			return err
		}
		return c.Status(http.StatusCreated).JSON(View(user))
	}
}

func login(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in credentials
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		q := d.Q()
		provider, err := q.GetLoginProvider(c.Context(), PasswordProvider)
		if err != nil {
			return err
		}
		if !provider.LoginEnabled {
			return errLoginDisabled()
		}
		user, err := Authenticate(c.Context(), q, in.Username, in.Password)
		if err != nil {
			return err
		}
		if user.Disabled {
			return apierr.New(http.StatusForbidden, "account_disabled", "This account is disabled.")
		}
		if err := auth.StartSession(c, d, q, user.ID); err != nil {
			return err
		}
		return c.JSON(View(user))
	}
}

func logout(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := auth.EndSession(c, d); err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

func getProfile(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		user, err := d.Q().GetUser(c.Context(), auth.Target(c))
		if err != nil {
			return err
		}
		// A connection gets what it needs to talk to the user at the right time, nothing more.
		if p, _ := auth.From(c); p.Kind == auth.KindConnection {
			full := View(user)
			return c.JSON(fiber.Map{
				"id":           full.ID,
				"display_name": full.DisplayName,
				"timezone":     full.Timezone,
				"quiet_hours":  full.QuietHours,
			})
		}
		return c.JSON(View(user))
	}
}

type profilePatch struct {
	DisplayName *string     `json:"display_name"`
	Timezone    *string     `json:"timezone"`
	QuietHours  *QuietHours `json:"quiet_hours"`
}

func patchProfile(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in profilePatch
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		q := d.Q()
		user, err := q.GetUser(c.Context(), auth.Target(c))
		if err != nil {
			return err
		}
		params := sqlc.UpdateUserProfileParams{
			ID:          user.ID,
			DisplayName: user.DisplayName,
			Timezone:    user.Timezone,
			QuietStart:  user.QuietStart,
			QuietEnd:    user.QuietEnd,
		}
		fields := map[string]string{}
		if in.DisplayName != nil {
			params.DisplayName = strings.TrimSpace(*in.DisplayName)
			checkDisplayName(fields, params.DisplayName)
		}
		if in.Timezone != nil {
			params.Timezone = *in.Timezone
			checkTimezone(c.Context(), q, fields, params.Timezone)
		}
		if in.QuietHours != nil {
			start, okStart := parseClock(in.QuietHours.Start)
			end, okEnd := parseClock(in.QuietHours.End)
			if !okStart || !okEnd {
				fields["quiet_hours"] = "Start and end must be times in HH:MM format."
			}
			params.QuietStart, params.QuietEnd = start, end
		}
		if len(fields) > 0 {
			return apierr.Validation(fields)
		}
		user, err = q.UpdateUserProfile(c.Context(), params)
		if err != nil {
			return err
		}
		return c.JSON(View(user))
	}
}

func deleteProfile(d *deps.Deps, revoker Revoker) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := DeleteUser(c.Context(), d, revoker, auth.Target(c)); err != nil {
			return err
		}
		if err := auth.EndSession(c, d); err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

type Identity struct {
	ID          uuid.UUID `json:"id"`
	Provider    string    `json:"provider"`
	ExternalID  string    `json:"external_id"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

func listIdentities(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := d.Q().ListIdentities(c.Context(), auth.Target(c))
		if err != nil {
			return err
		}
		identities := make([]Identity, 0, len(rows))
		for _, row := range rows {
			identities = append(identities, Identity{
				ID:          row.ID,
				Provider:    row.Provider,
				ExternalID:  row.ExternalID,
				DisplayName: row.DisplayName,
				CreatedAt:   row.CreatedAt.UTC(),
			})
		}
		return c.JSON(identities)
	}
}

func setPassword(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Username        string `json:"username"`
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		userID := auth.Target(c)
		err := db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			identity, err := q.GetPasswordIdentity(c.Context(), userID)
			hasPassword := err == nil
			if err != nil && !db.IsNotFound(err) {
				return err
			}
			fields := map[string]string{}
			checkPassword(fields, "new_password", in.NewPassword)
			username := NormalizeUsername(in.Username)
			if !hasPassword {
				checkUsername(fields, username)
			}
			if len(fields) > 0 {
				return apierr.Validation(fields)
			}
			if !hasPassword {
				provider, err := q.GetLoginProvider(c.Context(), PasswordProvider)
				if err != nil {
					return err
				}
				if !provider.LinkEnabled {
					return apierr.New(http.StatusForbidden, "link_disabled", "Adding a password is turned off on this instance.")
				}
				return AddPassword(c.Context(), q, d.Now(), userID, username, in.NewPassword)
			}
			if !secret.VerifyPassword(*identity.PasswordHash, in.CurrentPassword) {
				return apierr.New(http.StatusForbidden, "invalid_credentials", "The current password is wrong.")
			}
			hash, err := secret.HashPassword(in.NewPassword)
			if err != nil {
				return err
			}
			if err := q.UpdatePasswordHash(c.Context(), sqlc.UpdatePasswordHashParams{ID: identity.ID, PasswordHash: &hash}); err != nil {
				return err
			}
			// Whoever changes a password after a scare expects other devices to be logged out.
			p, _ := auth.From(c)
			return q.DeleteOtherSessions(c.Context(), sqlc.DeleteOtherSessionsParams{UserID: userID, KeepID: p.CredentialID})
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
