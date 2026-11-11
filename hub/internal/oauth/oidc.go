package oauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/rayakame/plsremind/hub/internal/deps"
)

func newOIDC(ctx context.Context, d *deps.Deps, issuer, clientID, clientSecret string) (Provider, error) {
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, d.HTTP), issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	var metadata struct {
		RevocationEndpoint string `json:"revocation_endpoint"`
	}
	_ = provider.Claims(&metadata)
	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})

	c := &client{
		cfg: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       []string{oidc.ScopeOpenID, "profile"},
			Endpoint:     provider.Endpoint(),
		},
		http: d.HTTP,
		profile: func(ctx context.Context, token *oauth2.Token) (Profile, error) {
			raw, ok := token.Extra("id_token").(string)
			if !ok {
				return Profile{}, errors.New("provider returned no id_token")
			}
			idToken, err := verifier.Verify(oidc.ClientContext(ctx, d.HTTP), raw)
			if err != nil {
				return Profile{}, fmt.Errorf("verify id_token: %w", err)
			}
			var claims struct {
				Name              string `json:"name"`
				PreferredUsername string `json:"preferred_username"`
			}
			if err := idToken.Claims(&claims); err != nil {
				return Profile{}, err
			}
			return Profile{ExternalID: idToken.Subject, DisplayName: firstNonEmpty(claims.Name, claims.PreferredUsername)}, nil
		},
	}
	if metadata.RevocationEndpoint != "" {
		c.revoke = revokeForm(d.HTTP, metadata.RevocationEndpoint, clientID, clientSecret)
	}
	return c, nil
}
