package account

import (
	"context"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/duration"
	"github.com/rayakame/plsremind/hub/internal/httpx"
	"github.com/rayakame/plsremind/hub/internal/secret"
)

const (
	defaultInviteLifetime = 7 * 24 * time.Hour
	maxInviteLifetime     = 30 * 24 * time.Hour
)

// AdmitNewUser decides whether a new account may be created. The returned
// function marks the invite as used and must run in the same transaction.
func AdmitNewUser(ctx context.Context, q *sqlc.Queries, now time.Time, inviteCode string) (redeem func(userID uuid.UUID) error, err error) {
	settings, err := q.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if inviteCode == "" {
		if settings.Registration == "open" {
			return func(uuid.UUID) error { return nil }, nil
		}
		return nil, apierr.New(http.StatusForbidden, "registration_closed", "Registration needs an invite.")
	}
	invite, err := q.GetUsableInvite(ctx, sqlc.GetUsableInviteParams{CodeHash: secret.Hash(inviteCode), Now: now})
	if db.IsNotFound(err) {
		return nil, apierr.New(http.StatusForbidden, "invalid_invite", "The invite is unknown, used or expired.")
	}
	if err != nil {
		return nil, err
	}
	return func(userID uuid.UUID) error {
		return q.UseInvite(ctx, sqlc.UseInviteParams{ID: invite.ID, Now: &now, UsedBy: &userID})
	}, nil
}

func register(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			credentials
			InviteCode string `json:"invite_code"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		if err := in.normalize(); err != nil {
			return err
		}
		var user sqlc.User
		err := db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			provider, err := q.GetLoginProvider(c.Context(), PasswordProvider)
			if err != nil {
				return err
			}
			if !provider.LoginEnabled {
				return errLoginDisabled()
			}
			now := d.Now()
			redeem, err := AdmitNewUser(c.Context(), q, now, in.InviteCode)
			if err != nil {
				return err
			}
			user, err = CreateUser(c.Context(), q, now, NewUser{DisplayName: in.DisplayName, Timezone: in.Timezone})
			if err != nil {
				return err
			}
			if err := AddPassword(c.Context(), q, now, user.ID, in.Username, in.Password); err != nil {
				return err
			}
			if err := redeem(user.ID); err != nil {
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

type Invite struct {
	ID        uuid.UUID  `json:"id"`
	Code      string     `json:"code,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at"`
	UsedBy    *uuid.UUID `json:"used_by"`
}

func viewInvite(i sqlc.Invite) Invite {
	return Invite{ID: i.ID, CreatedAt: i.CreatedAt.UTC(), ExpiresAt: i.ExpiresAt.UTC(), UsedAt: utc(i.UsedAt), UsedBy: i.UsedBy}
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func createInvite(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			ExpiresIn string `json:"expires_in"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		lifetime := defaultInviteLifetime
		if in.ExpiresIn != "" {
			parsed, err := duration.Parse(in.ExpiresIn)
			if err != nil || parsed > maxInviteLifetime {
				return apierr.Field("expires_in", `Must be a duration of at most 30d, for example "7d".`)
			}
			lifetime = parsed
		}
		admin, _ := auth.From(c)
		now := d.Now()
		plain, hash := secret.NewToken(secret.PrefixInvite)
		invite, err := d.Q().CreateInvite(c.Context(), sqlc.CreateInviteParams{
			ID:        uuid.Must(uuid.NewV7()),
			CodeHash:  hash,
			CreatedBy: &admin.UserID,
			CreatedAt: now,
			ExpiresAt: now.Add(lifetime),
		})
		if err != nil {
			return err
		}
		view := viewInvite(invite)
		view.Code = plain
		return c.Status(http.StatusCreated).JSON(view)
	}
}

func listInvites(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := d.Q().ListInvites(c.Context())
		if err != nil {
			return err
		}
		invites := make([]Invite, 0, len(rows))
		for _, row := range rows {
			invites = append(invites, viewInvite(row))
		}
		return c.JSON(invites)
	}
}

func deleteInvite(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		n, err := d.Q().DeleteInvite(c.Context(), id)
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.NotFound()
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
