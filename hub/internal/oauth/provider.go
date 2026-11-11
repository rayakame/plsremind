// Package oauth implements login and account linking through GitHub, Discord and OIDC providers.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/oauth2"

	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
)

// Profile is what the hub keeps of an account at a provider.
type Profile struct {
	ExternalID  string
	DisplayName string
	// Token is the refresh token if the provider issued one, otherwise the access token.
	// It is only kept to revoke the grant when the account is unlinked.
	Token string
}

type Provider interface {
	AuthURL(state, verifier, redirect string) string
	Exchange(ctx context.Context, code, verifier, redirect string) (Profile, error)
	Revoke(ctx context.Context, token string) error
}

// Configured reports whether the provider has everything it needs to run a flow.
func Configured(row sqlc.LoginProvider) bool {
	switch row.Type {
	case "password":
		return true
	case "oidc":
		return row.ClientID != "" && row.ClientSecret != nil && row.IssuerUrl != ""
	default:
		return row.ClientID != "" && row.ClientSecret != nil
	}
}

// Build creates the client for a configured provider.
func Build(ctx context.Context, d *deps.Deps, row sqlc.LoginProvider) (Provider, error) {
	if !Configured(row) || row.Type == "password" {
		return nil, errors.New("provider is not configured for oauth")
	}
	clientSecret, err := d.Box().Open(row.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt client secret: %w", err)
	}
	switch row.Type {
	case "github":
		return newGitHub(d, row.ClientID, string(clientSecret)), nil
	case "discord":
		return newDiscord(d, row.ClientID, string(clientSecret)), nil
	case "oidc":
		return newOIDC(ctx, d, row.IssuerUrl, row.ClientID, string(clientSecret))
	}
	return nil, fmt.Errorf("unknown provider type %q", row.Type)
}

func endpoints(d *deps.Deps, key string, defaults deps.OAuthEndpoints) deps.OAuthEndpoints {
	if override, ok := d.OAuthEndpoints[key]; ok {
		return override
	}
	return defaults
}

// client is an OAuth2 authorization code client with PKCE. The profile and
// revoke functions hold what differs between providers.
type client struct {
	cfg     oauth2.Config
	http    *http.Client
	profile func(ctx context.Context, token *oauth2.Token) (Profile, error)
	revoke  func(ctx context.Context, token string) error
}

func (c *client) config(redirect string) *oauth2.Config {
	cfg := c.cfg
	cfg.RedirectURL = redirect
	return &cfg
}

func (c *client) AuthURL(state, verifier, redirect string) string {
	return c.config(redirect).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

func (c *client) Exchange(ctx context.Context, code, verifier, redirect string) (Profile, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, c.http)
	token, err := c.config(redirect).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Profile{}, fmt.Errorf("exchange code: %w", err)
	}
	profile, err := c.profile(ctx, token)
	if err != nil {
		return Profile{}, err
	}
	if profile.ExternalID == "" {
		return Profile{}, errors.New("provider returned no user id")
	}
	profile.DisplayName = cleanName(profile.DisplayName, profile.ExternalID)
	profile.Token = token.RefreshToken
	if profile.Token == "" {
		profile.Token = token.AccessToken
	}
	return profile, nil
}

func (c *client) Revoke(ctx context.Context, token string) error {
	if c.revoke == nil {
		return nil
	}
	return c.revoke(ctx, token)
}

// cleanName fits a provider's display name into the hub's limits.
func cleanName(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = fallback
	}
	if utf8.RuneCountInString(name) > 64 {
		name = string([]rune(name)[:64])
	}
	return name
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// getJSON fetches a user profile endpoint with the access token.
func getJSON(ctx context.Context, hc *http.Client, endpoint, accessToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch profile: status %d", res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// revokeForm revokes a token the way RFC 7009 describes, which Discord and OIDC providers follow.
func revokeForm(hc *http.Client, endpoint, clientID, clientSecret string) func(ctx context.Context, token string) error {
	return func(ctx context.Context, token string) error {
		form := url.Values{"token": {token}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(clientSecret))
		res, err := hc.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			return fmt.Errorf("revoke token: status %d", res.StatusCode)
		}
		return nil
	}
}
