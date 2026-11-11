package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"

	"github.com/rayakame/plsremind/hub/internal/deps"
)

var githubEndpoints = deps.OAuthEndpoints{
	AuthURL:   "https://github.com/login/oauth/authorize",
	TokenURL:  "https://github.com/login/oauth/access_token",
	UserURL:   "https://api.github.com/user",
	RevokeURL: "https://api.github.com/applications/{client_id}/grant",
}

func newGitHub(d *deps.Deps, clientID, clientSecret string) Provider {
	ep := endpoints(d, "github", githubEndpoints)
	return &client{
		cfg: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       []string{"read:user"},
			Endpoint:     oauth2.Endpoint{AuthURL: ep.AuthURL, TokenURL: ep.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
		},
		http: d.HTTP,
		profile: func(ctx context.Context, token *oauth2.Token) (Profile, error) {
			var user struct {
				ID    json.Number `json:"id"`
				Login string      `json:"login"`
				Name  string      `json:"name"`
			}
			if err := getJSON(ctx, d.HTTP, ep.UserURL, token.AccessToken, &user); err != nil {
				return Profile{}, err
			}
			return Profile{ExternalID: user.ID.String(), DisplayName: firstNonEmpty(user.Name, user.Login)}, nil
		},
		// GitHub has no RFC 7009 endpoint; an app deletes its own grant instead.
		revoke: func(ctx context.Context, token string) error {
			body, _ := json.Marshal(map[string]string{"access_token": token})
			endpoint := strings.Replace(ep.RevokeURL, "{client_id}", clientID, 1)
			req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, bytes.NewReader(body))
			if err != nil {
				return err
			}
			req.SetBasicAuth(clientID, clientSecret)
			req.Header.Set("Accept", "application/vnd.github+json")
			res, err := d.HTTP.Do(req)
			if err != nil {
				return err
			}
			defer res.Body.Close()
			if res.StatusCode >= 300 {
				return fmt.Errorf("revoke github grant: status %d", res.StatusCode)
			}
			return nil
		},
	}
}
