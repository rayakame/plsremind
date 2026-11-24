package account

import (
	"crypto/subtle"
	"errors"
	"golang.org/x/text/language"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

const (
	stateCookie   = "plsr_oauth_state"
	stateLifetime = 10 * time.Minute
)

func redirectURL(d *core.Deps, key string) string {
	return d.Cfg.PublicURL + "/api/v1/auth/oauth/" + url.PathEscape(key) + "/callback"
}

func setStateCookie(c fiber.Ctx, d *core.Deps, value string, expires time.Time) {
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

// oauthFail sends the browser back to the dashboard with an error code it can show.
func oauthFail(c fiber.Ctx, d *core.Deps, code string) error {
	return c.Redirect().Status(http.StatusFound).To(d.Cfg.PublicURL + "/?auth_error=" + url.QueryEscape(code))
}

// linkedPage is where a link flow ends: the dashboard's confirmation, with the linked
// identity or the error code.
func linkedPage(d *core.Deps, key, param, value string) string {
	return d.Cfg.PublicURL + "/linked/" + url.PathEscape(key) + "?" + url.Values{param: {value}}.Encode()
}

func oauthStart(d *core.Deps, svc *OAuth) fiber.Handler {
	return func(c fiber.Ctx) error {
		intent := c.Query("intent", "login")
		if intent != "login" && intent != "link" {
			return api.Field("intent", `Must be "login" or "link".`)
		}
		q := d.Q()
		row, err := q.GetLoginProvider(c.Context(), c.Params("provider"))
		if store.IsNotFound(err) || (err == nil && (row.Type == "password" || !ProviderConfigured(row))) {
			return api.NotFound()
		}
		if err != nil {
			return err
		}

		var userID *uuid.UUID
		if intent == "link" {
			p, ok := auth.From(c)
			if !ok || p.Kind != auth.KindSession {
				return api.Unauthorized()
			}
			if !row.LinkEnabled {
				return c.Redirect().Status(http.StatusFound).To(linkedPage(d, row.Key, "error", "link_disabled"))
			}
			userID = &p.UserID
		} else if !row.LoginEnabled {
			return oauthFail(c, d, "login_disabled")
		}

		provider, err := svc.Build(c.Context(), row)
		if err != nil {
			slog.Error("oauth provider unavailable", "provider", row.Key, "err", err)
			if intent == "link" {
				return c.Redirect().Status(http.StatusFound).To(linkedPage(d, row.Key, "error", "provider_error"))
			}
			return oauthFail(c, d, "provider_error")
		}
		// The invite stays usable until the flow ends, so it is kept encrypted like any other secret.
		var invite []byte
		if code := c.Query("invite_code"); code != "" {
			invite = d.Box().Seal([]byte(code))
		}
		state, hash := core.NewToken("")
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

func oauthCallback(d *core.Deps, svc *OAuth) fiber.Handler {
	return func(c fiber.Ctx) error {
		key := c.Params("provider")
		state, cookie := c.Query("state"), c.Cookies(stateCookie)
		setStateCookie(c, d, "", time.Unix(0, 0))
		if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(cookie)) != 1 {
			return oauthFail(c, d, "invalid_state")
		}
		q := d.Q()
		stored, err := q.ConsumeOAuthState(c.Context(), core.HashToken(state))
		if store.IsNotFound(err) {
			return oauthFail(c, d, "invalid_state")
		}
		if err != nil {
			return err
		}
		fail := func(code string) error {
			if stored.Intent == "link" {
				return c.Redirect().Status(http.StatusFound).To(linkedPage(d, key, "error", code))
			}
			return oauthFail(c, d, code)
		}
		if stored.Provider != key || !stored.ExpiresAt.After(d.Now()) {
			return fail("invalid_state")
		}
		code := c.Query("code")
		if c.Query("error") != "" || code == "" {
			return fail("provider_error")
		}

		row, err := q.GetLoginProvider(c.Context(), key)
		if err != nil {
			return fail("provider_error")
		}
		var linked uuid.UUID
		provider, err := svc.Build(c.Context(), row)
		if err == nil {
			var profile OAuthProfile
			profile, err = provider.Exchange(c.Context(), code, stored.Verifier, redirectURL(d, key))
			if err == nil {
				linked, err = oauthFinish(c, d, row, stored, profile)
			}
		}
		var apiErr *api.Error
		switch {
		case err == nil && stored.Intent == "link":
			return c.Redirect().Status(http.StatusFound).To(linkedPage(d, key, "identity", linked.String()))
		case err == nil:
			return c.Redirect().Status(http.StatusFound).To(d.Cfg.PublicURL + "/")
		case errors.As(err, &apiErr):
			return fail(apiErr.Code)
		default:
			slog.Error("oauth callback failed", "provider", key, "err", err)
			return fail("provider_error")
		}
	}
}

// oauthFinish applies a verified profile: it logs the user in, creates their account or
// links the identity, whose id it returns.
func oauthFinish(c fiber.Ctx, d *core.Deps, used sqlc.LoginProvider, stored sqlc.OauthState, profile OAuthProfile) (uuid.UUID, error) {
	token := d.Box().Seal([]byte(profile.Token))
	var linked uuid.UUID
	err := store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
		// The provider may have been deleted or pointed at another issuer while the
		// user was away at it. The profile is only good for the provider as it was used.
		row, err := q.HoldLoginProvider(c.Context(), used.Key)
		if store.IsNotFound(err) || (err == nil && row.IssuerUrl != used.IssuerUrl) {
			return api.Conflict("provider_error", "The provider changed during the login.")
		}
		if err != nil {
			return err
		}
		now := d.Now()
		identity, err := q.GetIdentity(c.Context(), sqlc.GetIdentityParams{Provider: row.Key, ExternalID: profile.ExternalID})
		known := err == nil
		if err != nil && !store.IsNotFound(err) {
			return err
		}
		link := func(userID uuid.UUID) error {
			params := sqlc.CreateIdentityParams{
				ID:            uuid.Must(uuid.NewV7()),
				UserID:        userID,
				Provider:      row.Key,
				ExternalID:    profile.ExternalID,
				DisplayName:   profile.DisplayName,
				Username:      profile.Username,
				ProviderToken: token,
				CreatedAt:     now,
			}
			if profile.picture != nil {
				params.Avatar, params.AvatarType = profile.picture.data, profile.picture.kind
			}
			created, err := q.CreateIdentity(c.Context(), params)
			if err != nil {
				return err
			}
			linked = created.ID
			// A new identity can bring the user into reach of a connection.
			return events.NotifyUser(c.Context(), q, events.SignalUser, userID)
		}
		refresh := func() error {
			linked = identity.ID
			params := sqlc.RefreshIdentityParams{
				ID:            identity.ID,
				DisplayName:   profile.DisplayName,
				Username:      profile.Username,
				ProviderToken: token,
				SetAvatar:     !profile.keepPicture,
			}
			if profile.picture != nil {
				params.Avatar, params.AvatarType = profile.picture.data, profile.picture.kind
			}
			return q.RefreshIdentity(c.Context(), params)
		}

		if stored.Intent == "link" {
			p, ok := auth.From(c)
			if !ok || stored.UserID == nil || p.UserID != *stored.UserID {
				return api.NewError(http.StatusForbidden, "invalid_state", "The link was started by another session.")
			}
			if !row.LinkEnabled {
				return api.NewError(http.StatusForbidden, "link_disabled", "Linking is turned off for this provider.")
			}
			switch {
			case !known:
				return link(p.UserID)
			case identity.UserID != p.UserID:
				return api.Conflict("identity_taken", "This account is already linked to another user.")
			}
			return refresh()
		}

		if !row.LoginEnabled {
			return api.NewError(http.StatusForbidden, "login_disabled", "Login is turned off for this provider.")
		}
		if known {
			user, err := q.GetUser(c.Context(), identity.UserID)
			if err != nil {
				return err
			}
			if user.Disabled {
				return api.NewError(http.StatusForbidden, "account_disabled", "This account is disabled.")
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
		redeem, err := AdmitNewUser(c.Context(), q, now, invite)
		if err != nil {
			return err
		}
		user, err := CreateUser(c.Context(), q, now, NewUser{DisplayName: profile.DisplayName, Locale: signUpLocale(profile.Locale, c.Get("Accept-Language"))})
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
	return linked, err
}

// signUpLocale picks the language of an account made by signing in elsewhere. Bots cannot see a
// browser, so the account takes the provider's language, or else the one the browser asked for.
func signUpLocale(provider, acceptLanguage string) *string {
	candidates := []string{strings.TrimSpace(provider)}
	// Ordered by weight, without the ones weighted zero; "*" and unknown ones name no language.
	if asked, _, err := language.ParseAcceptLanguage(acceptLanguage); err == nil {
		for _, tag := range asked {
			if base, _ := tag.Base(); base.String() != "und" && base.String() != "mul" {
				candidates = append(candidates, tag.String())
			}
		}
	}
	for _, candidate := range candidates {
		if len(candidate) <= maxLocaleLen && localePattern.MatchString(candidate) {
			return &candidate
		}
	}
	return nil
}
