package account

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

const discoveryLifetime = 10 * time.Minute

// discovered is an OIDC provider's published configuration together with the
// settings it was fetched for.
type discovered struct {
	provider  *oidc.Provider
	issuer    string
	clientID  string
	secret    []byte
	fetchedAt time.Time
}

func (d discovered) fits(row sqlc.LoginProvider, now time.Time) bool {
	return d.provider != nil &&
		d.issuer == row.IssuerUrl &&
		d.clientID == row.ClientID &&
		bytes.Equal(d.secret, row.ClientSecret) &&
		now.Sub(d.fetchedAt) < discoveryLifetime
}

// discover returns the provider's configuration, fetching it at most once per
// discoveryLifetime. An entry made for other settings is never reused, so a
// change by the admin takes effect at once.
func (s *OAuth) discover(row sqlc.LoginProvider) (*oidc.Provider, error) {
	now := s.d.Now()
	s.mu.Lock()
	cached := s.discovery[row.Key]
	s.mu.Unlock()
	if cached.fits(row, now) {
		return cached.provider, nil
	}
	// The provider keeps this context to fetch signing keys later, so it must outlive the request.
	provider, err := oidc.NewProvider(oidc.ClientContext(context.Background(), s.d.HTTP), row.IssuerUrl)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	s.mu.Lock()
	s.discovery[row.Key] = discovered{
		provider:  provider,
		issuer:    row.IssuerUrl,
		clientID:  row.ClientID,
		secret:    row.ClientSecret,
		fetchedAt: now,
	}
	s.mu.Unlock()
	return provider, nil
}

func newOIDC(d *core.Deps, provider *oidc.Provider, clientID, clientSecret string) Provider {
	var metadata struct {
		RevocationEndpoint string `json:"revocation_endpoint"`
	}
	_ = provider.Claims(&metadata)
	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})

	c := &oauthClient{
		cfg: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       []string{oidc.ScopeOpenID, "profile"},
			Endpoint:     provider.Endpoint(),
		},
		http: d.HTTP,
		profile: func(ctx context.Context, token *oauth2.Token) (OAuthProfile, error) {
			raw, ok := token.Extra("id_token").(string)
			if !ok {
				return OAuthProfile{}, errors.New("provider returned no id_token")
			}
			idToken, err := verifier.Verify(oidc.ClientContext(ctx, d.HTTP), raw)
			if err != nil {
				return OAuthProfile{}, fmt.Errorf("verify id_token: %w", err)
			}
			var claims struct {
				Name              string `json:"name"`
				PreferredUsername string `json:"preferred_username"`
			}
			if err := idToken.Claims(&claims); err != nil {
				return OAuthProfile{}, err
			}
			return OAuthProfile{ExternalID: idToken.Subject, DisplayName: firstNonEmpty(claims.Name, claims.PreferredUsername)}, nil
		},
	}
	if metadata.RevocationEndpoint != "" {
		c.revoke = revokeForm(d.HTTP, metadata.RevocationEndpoint, clientID, clientSecret)
	}
	return c
}
