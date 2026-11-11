package oauth

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/httpx"
)

var keyPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

type providerView struct {
	Key             string `json:"key"`
	Type            string `json:"type"`
	Name            string `json:"name"`
	LoginEnabled    bool   `json:"login_enabled"`
	LinkEnabled     bool   `json:"link_enabled"`
	ClientID        string `json:"client_id"`
	HasClientSecret bool   `json:"has_client_secret"`
	IssuerURL       string `json:"issuer_url"`
}

func viewProvider(p sqlc.LoginProvider) providerView {
	return providerView{
		Key:             p.Key,
		Type:            p.Type,
		Name:            p.Name,
		LoginEnabled:    p.LoginEnabled,
		LinkEnabled:     p.LinkEnabled,
		ClientID:        p.ClientID,
		HasClientSecret: p.ClientSecret != nil,
		IssuerURL:       p.IssuerUrl,
	}
}

func listProviders(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := d.Q().ListLoginProviders(c.Context())
		if err != nil {
			return err
		}
		providers := make([]providerView, 0, len(rows))
		for _, row := range rows {
			providers = append(providers, viewProvider(row))
		}
		return c.JSON(providers)
	}
}

type providerInput struct {
	Type         *string `json:"type"`
	Name         *string `json:"name"`
	LoginEnabled *bool   `json:"login_enabled"`
	LinkEnabled  *bool   `json:"link_enabled"`
	ClientID     *string `json:"client_id"`
	ClientSecret *string `json:"client_secret"`
	IssuerURL    *string `json:"issuer_url"`
}

func validIssuer(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func putProvider(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		key := c.Params("key")
		var in providerInput
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		var saved sqlc.LoginProvider
		err := db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			if err := q.AdminLock(c.Context()); err != nil {
				return err
			}
			fields := map[string]string{}
			row, err := q.GetLoginProvider(c.Context(), key)
			switch {
			case db.IsNotFound(err):
				// Only OIDC providers can be added; the others are built in.
				if !keyPattern.MatchString(key) {
					fields["key"] = "Must be 1 to 32 characters: a-z, 0-9, underscore or dash."
				}
				if in.Type == nil || *in.Type != "oidc" {
					fields["type"] = `New providers must have the type "oidc".`
				}
				row = sqlc.LoginProvider{Key: key, Type: "oidc", Name: key}
			case err != nil:
				return err
			case in.Type != nil && *in.Type != row.Type:
				fields["type"] = "The type of a provider cannot be changed."
			}

			if in.Name != nil {
				row.Name = strings.TrimSpace(*in.Name)
			}
			if n := utf8.RuneCountInString(row.Name); n < 1 || n > 64 {
				fields["name"] = "Must be 1 to 64 characters."
			}
			if in.LoginEnabled != nil {
				row.LoginEnabled = *in.LoginEnabled
			}
			if in.LinkEnabled != nil {
				row.LinkEnabled = *in.LinkEnabled
			}
			if row.Type != "password" {
				if in.ClientID != nil {
					row.ClientID = strings.TrimSpace(*in.ClientID)
				}
				if in.ClientSecret != nil {
					row.ClientSecret = nil
					if *in.ClientSecret != "" {
						row.ClientSecret = d.Box().Seal([]byte(*in.ClientSecret))
					}
				}
				if in.IssuerURL != nil && row.Type == "oidc" {
					row.IssuerUrl = strings.TrimRight(strings.TrimSpace(*in.IssuerURL), "/")
				}
				if row.IssuerUrl != "" && !validIssuer(row.IssuerUrl) {
					fields["issuer_url"] = "Must be an http or https URL."
				}
				if (row.LoginEnabled || row.LinkEnabled) && !Configured(row) {
					fields["client_id"] = "A provider needs its client ID and secret before it can be enabled."
					if row.Type == "oidc" && row.IssuerUrl == "" {
						fields["issuer_url"] = "An OIDC provider needs its issuer URL before it can be enabled."
					}
				}
			}
			if len(fields) > 0 {
				return apierr.Validation(fields)
			}

			saved, err = q.UpsertLoginProvider(c.Context(), sqlc.UpsertLoginProviderParams{
				Key:          row.Key,
				Type:         row.Type,
				Name:         row.Name,
				LoginEnabled: row.LoginEnabled,
				LinkEnabled:  row.LinkEnabled,
				ClientID:     row.ClientID,
				ClientSecret: row.ClientSecret,
				IssuerUrl:    row.IssuerUrl,
			})
			if err != nil {
				return err
			}
			methods, err := q.CountLoginMethods(c.Context())
			if err != nil {
				return err
			}
			if methods == 0 {
				return apierr.Conflict("last_login_method", "At least one way to log in must stay enabled.")
			}
			return nil
		})
		if err != nil {
			return err
		}
		return c.JSON(viewProvider(saved))
	}
}

func deleteProvider(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		key := c.Params("key")
		err := db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			if err := q.AdminLock(c.Context()); err != nil {
				return err
			}
			row, err := q.GetLoginProvider(c.Context(), key)
			if db.IsNotFound(err) {
				return apierr.NotFound()
			}
			if err != nil {
				return err
			}
			if row.Type != "oidc" {
				return apierr.Field("key", "Built-in providers cannot be deleted, only disabled.")
			}
			identities, err := q.CountProviderIdentities(c.Context(), key)
			if err != nil {
				return err
			}
			if identities > 0 {
				return apierr.Conflict("provider_in_use", "Users have accounts linked through this provider. Disable it instead.")
			}
			if err := q.DeleteLoginProvider(c.Context(), key); err != nil {
				return err
			}
			methods, err := q.CountLoginMethods(c.Context())
			if err != nil {
				return err
			}
			if methods == 0 {
				return apierr.Conflict("last_login_method", "At least one way to log in must stay enabled.")
			}
			return nil
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
