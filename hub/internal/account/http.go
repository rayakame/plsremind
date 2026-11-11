package account

import (
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/httpx"
)

// Register mounts the public account routes on api and the profile routes on user.
func Register(api, user fiber.Router, d *deps.Deps) {
	api.Post("/setup", setup(d))
	api.Post("/auth/login", login(d))
	api.Post("/auth/logout", logout(d))

	user.Get("", getProfile(d))
	user.Patch("", patchProfile(d))
}

type credentials struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Timezone    string `json:"timezone"`
}

// normalize trims the input and validates it for creating an account.
func (in *credentials) normalize() error {
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
	checkTimezone(fields, in.Timezone)
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
		if err := in.normalize(); err != nil {
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
			checkTimezone(fields, params.Timezone)
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
