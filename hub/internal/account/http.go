// Package account manages users: sign-up, login with a password or through GitHub,
// Discord and OIDC providers, profiles and what an admin may do with them.
package account

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/delivery"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/reminder"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
	"github.com/rayakame/plsremind/hub/internal/todo"
)

// Register mounts the public account routes on api and the profile routes on user.
func Register(root, user, admin fiber.Router, d *core.Deps) {
	svc := newOAuth(d)
	root.Post("/setup", setup(d))
	root.Post("/auth/register", register(d))
	root.Post("/auth/login", login(d))
	root.Post("/auth/logout", logout(d))

	user.Get("", getProfile(d))
	user.Patch("", patchProfile(d))
	user.Delete("", auth.RequireSession(), deleteProfile(d, svc))
	user.Get("/identities", listIdentities(d))
	user.Put("/password", auth.RequireSession(), setPassword(d))

	admin.Get("/settings", getSettings(d))
	admin.Patch("/settings", patchSettings(d))
	admin.Get("/invites", listInvites(d))
	admin.Post("/invites", createInvite(d))
	admin.Delete("/invites/:id", deleteInvite(d))
	admin.Get("/users", listUsers(d))
	admin.Patch("/users/:id", patchUserAccess(d))
	admin.Delete("/users/:id", adminDeleteUser(d, svc))
	admin.Put("/users/:id/password", resetPassword(d))

	registerOAuth(root, user, admin, d, svc)
}

func errLoginDisabled() error {
	return api.NewError(http.StatusForbidden, "login_disabled", "Password login is turned off on this instance.")
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
		return api.Validation(fields)
	}
	return nil
}

func setup(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in credentials
		if err := api.Body(c, &in); err != nil {
			return err
		}
		if err := in.normalize(c.Context(), d.Q()); err != nil {
			return err
		}
		var user sqlc.User
		err := store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			if err := q.SetupLock(c.Context()); err != nil {
				return err
			}
			count, err := q.CountUsers(c.Context())
			if err != nil {
				return err
			}
			if count > 0 {
				return api.Conflict("already_set_up", "This instance is already set up.")
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

func login(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in credentials
		if err := api.Body(c, &in); err != nil {
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
			return api.NewError(http.StatusForbidden, "account_disabled", "This account is disabled.")
		}
		if err := auth.StartSession(c, d, q, user.ID); err != nil {
			return err
		}
		return c.JSON(View(user))
	}
}

func logout(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := auth.EndSession(c, d); err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

func getProfile(d *core.Deps) fiber.Handler {
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
	DisplayName *string             `json:"display_name"`
	Timezone    *string             `json:"timezone"`
	QuietHours  api.Opt[QuietHours] `json:"quiet_hours"`
}

func patchProfile(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in profilePatch
		if err := api.Body(c, &in); err != nil {
			return err
		}
		// What can be checked without the stored profile is checked up front; the
		// time zone probe in particular must not run inside the transaction.
		fields := map[string]string{}
		var displayName string
		if in.DisplayName != nil {
			displayName = strings.TrimSpace(*in.DisplayName)
			checkDisplayName(fields, displayName)
		}
		if in.Timezone != nil {
			checkTimezone(c.Context(), d.Q(), fields, *in.Timezone)
		}
		var quietStart, quietEnd pgtype.Time
		if in.QuietHours.HasValue() {
			var okStart, okEnd bool
			quietStart, okStart = parseClock(in.QuietHours.Val.Start)
			quietEnd, okEnd = parseClock(in.QuietHours.Val.End)
			if !okStart || !okEnd || quietStart == quietEnd {
				fields["quiet_hours"] = "Start and end must be two different times in HH:MM format."
			}
		}
		if len(fields) > 0 {
			return api.Validation(fields)
		}

		var user sqlc.User
		err := store.UserTx(c.Context(), d.Pool, auth.Target(c), func(q *sqlc.Queries) error {
			before, err := q.GetUser(c.Context(), auth.Target(c))
			if err != nil {
				return err
			}
			params := sqlc.UpdateUserProfileParams{
				ID:          before.ID,
				DisplayName: before.DisplayName,
				Timezone:    before.Timezone,
				QuietStart:  before.QuietStart,
				QuietEnd:    before.QuietEnd,
			}
			if in.DisplayName != nil {
				params.DisplayName = displayName
			}
			if in.Timezone != nil {
				params.Timezone = *in.Timezone
			}
			if in.QuietHours.Set {
				params.QuietStart, params.QuietEnd = quietStart, quietEnd
			}
			user, err = q.UpdateUserProfile(c.Context(), params)
			if err != nil {
				return err
			}
			now := d.Now()
			p, _ := auth.From(c)
			actor := auth.ActorFrom(p)
			if err := events.Append(c.Context(), q, now, user.ID, actor, "user.updated", View(user)); err != nil {
				return err
			}
			rezoned := user.Timezone != before.Timezone
			requieted := user.QuietStart != before.QuietStart || user.QuietEnd != before.QuietEnd
			if rezoned {
				// An all-day deadline ends with the day in the user's zone, so its due
				// time just moved. Clients that keep items in memory need to hear that.
				moved, err := todo.OpenAllDayItems(c.Context(), q, user.ID)
				if err != nil {
					return err
				}
				for _, it := range moved {
					if err := events.Append(c.Context(), q, now, user.ID, actor, "item.updated", it); err != nil {
						return err
					}
				}
			}
			if requieted {
				// What the old quiet hours were holding back is looked at again.
				if err := delivery.ReleaseHeldFor(c.Context(), q, now, user.ID); err != nil {
					return err
				}
			}
			if !rezoned && !requieted {
				return nil
			}
			// All-day deadlines and quiet hours are reckoned in the user's own time.
			return reminder.SyncUser(c.Context(), q, now, user.ID)
		})
		if err != nil {
			return err
		}
		return c.JSON(View(user))
	}
}

func deleteProfile(d *core.Deps, svc *OAuth) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := DeleteUser(c.Context(), d, svc, auth.Target(c)); err != nil {
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

func listIdentities(d *core.Deps) fiber.Handler {
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

func setPassword(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Username        string `json:"username"`
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		userID := auth.Target(c)
		err := store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			identity, err := q.GetPasswordIdentity(c.Context(), userID)
			hasPassword := err == nil
			if err != nil && !store.IsNotFound(err) {
				return err
			}
			fields := map[string]string{}
			checkPassword(fields, "new_password", in.NewPassword)
			username := NormalizeUsername(in.Username)
			if !hasPassword {
				checkUsername(fields, username)
			}
			if len(fields) > 0 {
				return api.Validation(fields)
			}
			if !hasPassword {
				provider, err := q.GetLoginProvider(c.Context(), PasswordProvider)
				if err != nil {
					return err
				}
				if !provider.LinkEnabled {
					return api.NewError(http.StatusForbidden, "link_disabled", "Adding a password is turned off on this instance.")
				}
				return AddPassword(c.Context(), q, d.Now(), userID, username, in.NewPassword)
			}
			if !core.VerifyPassword(*identity.PasswordHash, in.CurrentPassword) {
				return api.NewError(http.StatusForbidden, "invalid_credentials", "The current password is wrong.")
			}
			hash, err := core.HashPassword(in.NewPassword)
			if err != nil {
				return err
			}
			if err := q.UpdatePasswordHash(c.Context(), sqlc.UpdatePasswordHashParams{ID: identity.ID, PasswordHash: &hash}); err != nil {
				return err
			}
			// Whoever changes a password after a scare expects everything else to be
			// logged out. That includes personal tokens: someone with a stolen session
			// could have made one to keep their access.
			p, _ := auth.From(c)
			if err := q.DeleteUserPersonalTokens(c.Context(), userID); err != nil {
				return err
			}
			if err := q.DeleteOtherSessions(c.Context(), sqlc.DeleteOtherSessionsParams{UserID: userID, KeepID: p.CredentialID}); err != nil {
				return err
			}
			return events.NotifyUser(c.Context(), q, events.SignalUser, userID)
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
