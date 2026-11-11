package oauth

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/rayakame/plsremind/hub/internal/account"
	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/secret"
)

const (
	stateCookie   = "plsr_oauth_state"
	stateLifetime = 10 * time.Minute
)

func redirectURL(d *deps.Deps, key string) string {
	return d.Cfg.PublicURL + "/api/v1/auth/oauth/" + url.PathEscape(key) + "/callback"
}

func setStateCookie(c fiber.Ctx, d *deps.Deps, value string, expires time.Time) {
	c.Cookie(&fiber.Cookie{
		Name:     stateCookie,
		Value:    value,
		Path:     "/api/v1/auth/oauth",
		Expires:  expires,
		HTTPOnly: true,
		Secure:   strings.HasPrefix(d.Cfg.PublicURL, "https://"),
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}

// fail sends the browser back to the dashboard with an error code it can show.
func fail(c fiber.Ctx, d *deps.Deps, code string) error {
	return c.Redirect().Status(http.StatusFound).To(d.Cfg.PublicURL + "/?auth_error=" + url.QueryEscape(code))
}

func start(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		intent := c.Query("intent", "login")
		if intent != "login" && intent != "link" {
			return apierr.Field("intent", `Must be "login" or "link".`)
		}
		q := d.Q()
		row, err := q.GetLoginProvider(c.Context(), c.Params("provider"))
		if db.IsNotFound(err) || (err == nil && (row.Type == "password" || !Configured(row))) {
			return apierr.NotFound()
		}
		if err != nil {
			return err
		}

		var userID *uuid.UUID
		if intent == "link" {
			p, ok := auth.From(c)
			if !ok || p.Kind != auth.KindSession {
				return apierr.Unauthorized()
			}
			if !row.LinkEnabled {
				return fail(c, d, "link_disabled")
			}
			userID = &p.UserID
		} else if !row.LoginEnabled {
			return fail(c, d, "login_disabled")
		}

		provider, err := Build(c.Context(), d, row)
		if err != nil {
			slog.Error("oauth provider unavailable", "provider", row.Key, "err", err)
			return fail(c, d, "provider_error")
		}
		// The invite stays usable until the flow ends, so it is kept encrypted like any other secret.
		var invite []byte
		if code := c.Query("invite_code"); code != "" {
			invite = d.Box().Seal([]byte(code))
		}
		state, hash := secret.NewToken("")
		verifier := oauth2.GenerateVerifier()
		expires := d.Now().Add(stateLifetime)
		err = q.CreateOAuthState(c.Context(), sqlc.CreateOAuthStateParams{
			StateHash:  hash,
			Provider:   row.Key,
			Intent:     intent,
			UserID:     userID,
			InviteCode: invite,
			Verifier:   verifier,
			ExpiresAt:  expires,
		})
		if err != nil {
			return err
		}
		// The cookie ties the state to this browser, so a callback URL made in
		// one browser cannot be finished in another.
		setStateCookie(c, d, state, expires)
		return c.Redirect().Status(http.StatusFound).To(provider.AuthURL(state, verifier, redirectURL(d, row.Key)))
	}
}

func callback(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		key := c.Params("provider")
		state, cookie := c.Query("state"), c.Cookies(stateCookie)
		setStateCookie(c, d, "", time.Unix(0, 0))
		if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(cookie)) != 1 {
			return fail(c, d, "invalid_state")
		}
		q := d.Q()
		stored, err := q.ConsumeOAuthState(c.Context(), secret.Hash(state))
		if db.IsNotFound(err) {
			return fail(c, d, "invalid_state")
		}
		if err != nil {
			return err
		}
		if stored.Provider != key || !stored.ExpiresAt.After(d.Now()) {
			return fail(c, d, "invalid_state")
		}
		code := c.Query("code")
		if c.Query("error") != "" || code == "" {
			return fail(c, d, "provider_error")
		}

		row, err := q.GetLoginProvider(c.Context(), key)
		if err != nil {
			return fail(c, d, "provider_error")
		}
		provider, err := Build(c.Context(), d, row)
		if err == nil {
			var profile Profile
			profile, err = provider.Exchange(c.Context(), code, stored.Verifier, redirectURL(d, key))
			if err == nil {
				err = finish(c, d, row, stored, profile)
			}
		}
		var apiErr *apierr.Error
		switch {
		case err == nil:
			return c.Redirect().Status(http.StatusFound).To(d.Cfg.PublicURL + "/")
		case errors.As(err, &apiErr):
			return fail(c, d, apiErr.Code)
		default:
			slog.Error("oauth callback failed", "provider", key, "err", err)
			return fail(c, d, "provider_error")
		}
	}
}

// finish applies a verified profile: it logs the user in, creates their account or links the identity.
func finish(c fiber.Ctx, d *deps.Deps, row sqlc.LoginProvider, stored sqlc.OauthState, profile Profile) error {
	token := d.Box().Seal([]byte(profile.Token))
	return db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
		now := d.Now()
		identity, err := q.GetIdentity(c.Context(), sqlc.GetIdentityParams{Provider: row.Key, ExternalID: profile.ExternalID})
		known := err == nil
		if err != nil && !db.IsNotFound(err) {
			return err
		}
		link := func(userID uuid.UUID) error {
			_, err := q.CreateIdentity(c.Context(), sqlc.CreateIdentityParams{
				ID:            uuid.Must(uuid.NewV7()),
				UserID:        userID,
				Provider:      row.Key,
				ExternalID:    profile.ExternalID,
				DisplayName:   profile.DisplayName,
				ProviderToken: token,
				CreatedAt:     now,
			})
			return err
		}
		refresh := func() error {
			return q.RefreshIdentity(c.Context(), sqlc.RefreshIdentityParams{ID: identity.ID, DisplayName: profile.DisplayName, ProviderToken: token})
		}

		if stored.Intent == "link" {
			p, ok := auth.From(c)
			if !ok || stored.UserID == nil || p.UserID != *stored.UserID {
				return apierr.New(http.StatusForbidden, "invalid_state", "The link was started by another session.")
			}
			if !row.LinkEnabled {
				return apierr.New(http.StatusForbidden, "link_disabled", "Linking is turned off for this provider.")
			}
			switch {
			case !known:
				return link(p.UserID)
			case identity.UserID != p.UserID:
				return apierr.Conflict("identity_taken", "This account is already linked to another user.")
			}
			return refresh()
		}

		if !row.LoginEnabled {
			return apierr.New(http.StatusForbidden, "login_disabled", "Login is turned off for this provider.")
		}
		if known {
			user, err := q.GetUser(c.Context(), identity.UserID)
			if err != nil {
				return err
			}
			if user.Disabled {
				return apierr.New(http.StatusForbidden, "account_disabled", "This account is disabled.")
			}
			if err := refresh(); err != nil {
				return err
			}
			return auth.StartSession(c, d, q, user.ID)
		}

		var invite string
		if stored.InviteCode != nil {
			plain, err := d.Box().Open(stored.InviteCode)
			if err != nil {
				return err
			}
			invite = string(plain)
		}
		redeem, err := account.AdmitNewUser(c.Context(), q, now, invite)
		if err != nil {
			return err
		}
		user, err := account.CreateUser(c.Context(), q, now, account.NewUser{DisplayName: profile.DisplayName})
		if err != nil {
			return err
		}
		if err := link(user.ID); err != nil {
			return err
		}
		if err := redeem(user.ID); err != nil {
			return err
		}
		return auth.StartSession(c, d, q, user.ID)
	})
}
