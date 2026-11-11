package oauth

import (
	"context"

	"golang.org/x/oauth2"

	"github.com/rayakame/plsremind/hub/internal/deps"
)

var discordEndpoints = deps.OAuthEndpoints{
	AuthURL:   "https://discord.com/oauth2/authorize",
	TokenURL:  "https://discord.com/api/oauth2/token",
	UserURL:   "https://discord.com/api/users/@me",
	RevokeURL: "https://discord.com/api/oauth2/token/revoke",
}

func newDiscord(d *deps.Deps, clientID, clientSecret string) Provider {
	ep := endpoints(d, "discord", discordEndpoints)
	return &client{
		cfg: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       []string{"identify"},
			Endpoint:     oauth2.Endpoint{AuthURL: ep.AuthURL, TokenURL: ep.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
		},
		http: d.HTTP,
		profile: func(ctx context.Context, token *oauth2.Token) (Profile, error) {
			var user struct {
				ID         string `json:"id"`
				Username   string `json:"username"`
				GlobalName string `json:"global_name"`
			}
			if err := getJSON(ctx, d.HTTP, ep.UserURL, token.AccessToken, &user); err != nil {
				return Profile{}, err
			}
			return Profile{ExternalID: user.ID, DisplayName: firstNonEmpty(user.GlobalName, user.Username)}, nil
		},
		revoke: revokeForm(d.HTTP, ep.RevokeURL, clientID, clientSecret),
	}
}
