package account

import (
	"context"
	"net/url"

	"golang.org/x/oauth2"

	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

var discordEndpoints = core.OAuthEndpoints{
	AuthURL:    "https://discord.com/oauth2/authorize",
	TokenURL:   "https://discord.com/api/oauth2/token",
	UserURL:    "https://discord.com/api/users/@me",
	RevokeURL:  "https://discord.com/api/oauth2/token/revoke",
	AvatarBase: "https://cdn.discordapp.com",
}

func newDiscord(d *core.Deps, clientID, clientSecret string, bot bool) Provider {
	ep := oauthEndpoints(d, "discord", discordEndpoints)
	scopes := []string{"identify"}
	var params []oauth2.AuthCodeOption
	if bot {
		// Discord keeps only the newest grant of an application: asking for less would take the bot out.
		scopes = append(scopes, "applications.commands")
		params = append(params, oauth2.SetAuthURLParam("integration_type", "1"))
	}
	return &oauthClient{
		cfg: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       scopes,
			Endpoint:     oauth2.Endpoint{AuthURL: ep.AuthURL, TokenURL: ep.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
		},
		params: params,
		http:   d.HTTP,
		profile: func(ctx context.Context, token *oauth2.Token) (OAuthProfile, error) {
			var user struct {
				ID         string `json:"id"`
				Username   string `json:"username"`
				GlobalName string `json:"global_name"`
				Avatar     string `json:"avatar"`
			}
			if err := getJSON(ctx, d.HTTP, ep.UserURL, token.AccessToken, &user); err != nil {
				return OAuthProfile{}, err
			}
			profile := OAuthProfile{
				ExternalID:  user.ID,
				DisplayName: firstNonEmpty(user.GlobalName, user.Username),
				Username:    user.Username,
				avatar:      avatarPolicy{origin: originOf(ep.AvatarBase)},
			}
			if user.Avatar != "" {
				profile.AvatarURL = ep.AvatarBase + "/avatars/" + url.PathEscape(user.ID) + "/" + url.PathEscape(user.Avatar) + ".png?size=128"
			}
			return profile, nil
		},
		revoke: revokeForm(d.HTTP, ep.RevokeURL, clientID, clientSecret),
	}
}

// InstallsWithLink reports whether linking at a sign-in also adds an app of it: the Discord
// sign-in does for a bot of its own application, which the app's install link names.
func InstallsWithLink(signIn sqlc.LoginProvider, installURL string) bool {
	if signIn.Type != "discord" || !ProviderConfigured(signIn) || installURL == "" {
		return false
	}
	link, err := url.Parse(installURL)
	return err == nil && link.Query().Get("client_id") == signIn.ClientID
}

func hasBot(ctx context.Context, q *sqlc.Queries, signIn sqlc.LoginProvider) (bool, error) {
	apps, err := q.ListConnections(ctx)
	if err != nil {
		return false, err
	}
	for _, app := range apps {
		if app.Provider == signIn.Key && InstallsWithLink(signIn, app.InstallUrl) {
			return true, nil
		}
	}
	return false, nil
}
