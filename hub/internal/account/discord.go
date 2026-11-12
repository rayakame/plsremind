package account

import (
	"context"

	"golang.org/x/oauth2"

	"github.com/rayakame/plsremind/hub/internal/core"
)

var discordEndpoints = core.OAuthEndpoints{
	AuthURL:   "https://discord.com/oauth2/authorize",
	TokenURL:  "https://discord.com/api/oauth2/token",
	UserURL:   "https://discord.com/api/users/@me",
	RevokeURL: "https://discord.com/api/oauth2/token/revoke",
}

func newDiscord(d *core.Deps, clientID, clientSecret string) Provider {
	ep := oauthEndpoints(d, "discord", discordEndpoints)
	return &oauthClient{
		cfg: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       []string{"identify"},
			Endpoint:     oauth2.Endpoint{AuthURL: ep.AuthURL, TokenURL: ep.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
		},
		http: d.HTTP,
		profile: func(ctx context.Context, token *oauth2.Token) (OAuthProfile, error) {
			var user struct {
				ID         string `json:"id"`
				Username   string `json:"username"`
				GlobalName string `json:"global_name"`
			}
			if err := getJSON(ctx, d.HTTP, ep.UserURL, token.AccessToken, &user); err != nil {
				return OAuthProfile{}, err
			}
			return OAuthProfile{ExternalID: user.ID, DisplayName: firstNonEmpty(user.GlobalName, user.Username)}, nil
		},
		revoke: revokeForm(d.HTTP, ep.RevokeURL, clientID, clientSecret),
	}
}
