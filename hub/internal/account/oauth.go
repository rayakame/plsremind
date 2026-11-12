package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/oauth2"

	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

// Profile is what the hub keeps of an account at a provider.
type OAuthProfile struct {
	ExternalID  string
	DisplayName string
	// Token is the refresh token if the provider issued one, otherwise the access token.
	// It is only kept to revoke the grant when the account is unlinked.
	Token string
}

type Provider interface {
	AuthURL(state, verifier, redirect string) string
	Exchange(ctx context.Context, code, verifier, redirect string) (OAuthProfile, error)
	Revoke(ctx context.Context, token string) error
}

// Configured reports whether the provider has everything it needs to run a flow.
func ProviderConfigured(row sqlc.LoginProvider) bool {
	switch row.Type {
	case "password":
		return true
	case "oidc":
		return row.ClientID != "" && row.ClientSecret != nil && row.IssuerUrl != ""
	default:
		return row.ClientID != "" && row.ClientSecret != nil
	}
}

// Service builds provider clients and remembers what it learned about OIDC providers.
type OAuth struct {
	d *core.Deps

	mu        sync.Mutex
	discovery map[string]discovered
}

func newOAuth(d *core.Deps) *OAuth {
	return &OAuth{d: d, discovery: map[string]discovered{}}
}

// Build creates the client for a configured provider.
func (s *OAuth) Build(ctx context.Context, row sqlc.LoginProvider) (Provider, error) {
	if !ProviderConfigured(row) || row.Type == "password" {
		return nil, errors.New("provider is not configured for oauth")
	}
	clientSecret, err := s.d.Box().Open(row.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt client secret: %w", err)
	}
	switch row.Type {
	case "github":
		return newGitHub(s.d, row.ClientID, string(clientSecret)), nil
	case "discord":
		return newDiscord(s.d, row.ClientID, string(clientSecret)), nil
	case "oidc":
		provider, err := s.discover(row)
		if err != nil {
			return nil, err
		}
		return newOIDC(s.d, provider, row.ClientID, string(clientSecret)), nil
	}
	return nil, fmt.Errorf("unknown provider type %q", row.Type)
}

func oauthEndpoints(d *core.Deps, key string, defaults core.OAuthEndpoints) core.OAuthEndpoints {
	if override, ok := d.OAuthEndpoints[key]; ok {
		return override
	}
	return defaults
}

// client is an OAuth2 authorization code client with PKCE. The profile and
// revoke functions hold what differs between providers.
type oauthClient struct {
	cfg     oauth2.Config
	http    *http.Client
	profile func(ctx context.Context, token *oauth2.Token) (OAuthProfile, error)
	revoke  func(ctx context.Context, token string) error
}

func (c *oauthClient) config(redirect string) *oauth2.Config {
	cfg := c.cfg
	cfg.RedirectURL = redirect
	return &cfg
}

func (c *oauthClient) AuthURL(state, verifier, redirect string) string {
	return c.config(redirect).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

func (c *oauthClient) Exchange(ctx context.Context, code, verifier, redirect string) (OAuthProfile, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, c.http)
	token, err := c.config(redirect).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return OAuthProfile{}, fmt.Errorf("exchange code: %w", err)
	}
	profile, err := c.profile(ctx, token)
	if err != nil {
		return OAuthProfile{}, err
	}
	if profile.ExternalID == "" {
		return OAuthProfile{}, errors.New("provider returned no user id")
	}
	profile.DisplayName = cleanProfileName(profile.DisplayName, profile.ExternalID)
	profile.Token = token.RefreshToken
	if profile.Token == "" {
		profile.Token = token.AccessToken
	}
	return profile, nil
}

func (c *oauthClient) Revoke(ctx context.Context, token string) error {
	if c.revoke == nil {
		return nil
	}
	return c.revoke(ctx, token)
}

// cleanName fits a provider's display name into the hub's limits.
func cleanProfileName(name, fallback string) string {
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
