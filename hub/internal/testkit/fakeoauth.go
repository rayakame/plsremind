package testkit

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/deps"
)

type OAuthUser struct {
	ID   string
	Name string
}

// FakeOAuth stands in for GitHub, Discord or an OIDC provider.
type FakeOAuth struct {
	Endpoints deps.OAuthEndpoints
	IssuerURL string
	User      OAuthUser
	// FailWith breaks one step: "token" or "user".
	FailWith string
	// BadSignature makes the OIDC ID token fail verification.
	BadSignature bool

	kind   string
	server *httptest.Server
	key    *rsa.PrivateKey

	mu         sync.Mutex
	challenges map[string]string
	tokens     int
	revoked    []string
}

// Revoked lists the tokens the hub asked the provider to revoke.
func (f *FakeOAuth) Revoked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.revoked...)
}

func NewFakeOAuth(t *testing.T, kind string) *FakeOAuth {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &FakeOAuth{kind: kind, key: key, User: OAuthUser{ID: "1234", Name: "Mia"}, challenges: map[string]string{}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /authorize", f.authorize)
	mux.HandleFunc("POST /token", f.token)
	mux.HandleFunc("GET /user", f.user)
	mux.HandleFunc("POST /revoke", f.revokeForm)
	mux.HandleFunc("DELETE /applications/{client}/grant", f.revokeGitHub)
	mux.HandleFunc("GET /.well-known/openid-configuration", f.discovery)
	mux.HandleFunc("GET /jwks", f.jwks)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)

	base := f.server.URL
	f.IssuerURL = base
	f.Endpoints = deps.OAuthEndpoints{
		AuthURL:   base + "/authorize",
		TokenURL:  base + "/token",
		UserURL:   base + "/user",
		RevokeURL: base + "/revoke",
	}
	if kind == "github" {
		f.Endpoints.RevokeURL = base + "/applications/{client_id}/grant"
	}
	return f
}

func (f *FakeOAuth) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("client_id") == "" {
		http.Error(w, "missing pkce or client", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	code := fmt.Sprintf("code-%d", len(f.challenges)+1)
	f.challenges[code] = q.Get("code_challenge")
	f.mu.Unlock()
	target := q.Get("redirect_uri") + "?" + url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
	http.Redirect(w, r, target, http.StatusFound)
}

func (f *FakeOAuth) token(w http.ResponseWriter, r *http.Request) {
	if f.FailWith == "token" {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	_ = r.ParseForm()
	f.mu.Lock()
	challenge, ok := f.challenges[r.Form.Get("code")]
	delete(f.challenges, r.Form.Get("code"))
	f.tokens++
	n := f.tokens
	f.mu.Unlock()
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}
	body := map[string]any{"access_token": fmt.Sprintf("access-%d", n), "token_type": "bearer"}
	if f.kind != "github" {
		body["refresh_token"] = fmt.Sprintf("refresh-%d", n)
	}
	if f.kind == "oidc" {
		clientID := r.Form.Get("client_id")
		if user, _, ok := r.BasicAuth(); ok {
			clientID, _ = url.QueryUnescape(user)
		}
		body["id_token"] = f.idToken(clientID)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (f *FakeOAuth) idToken(audience string) string {
	key := f.key
	if f.BadSignature {
		key, _ = rsa.GenerateKey(rand.Reader, 2048)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
	if err != nil {
		panic(err)
	}
	now := time.Now()
	token, err := jwt.Signed(signer).Claims(map[string]any{
		"iss":  f.IssuerURL,
		"sub":  f.User.ID,
		"aud":  audience,
		"exp":  now.Add(time.Hour).Unix(),
		"iat":  now.Unix(),
		"name": f.User.Name,
	}).Serialize()
	if err != nil {
		panic(err)
	}
	return token
}

func (f *FakeOAuth) user(w http.ResponseWriter, r *http.Request) {
	if f.FailWith == "user" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer access-") {
		http.Error(w, "boom", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if f.kind == "github" {
		// GitHub user IDs are JSON numbers.
		fmt.Fprintf(w, `{"id": %s, "login": "octo", "name": %q}`, f.User.ID, f.User.Name)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": f.User.ID, "username": "user", "global_name": f.User.Name})
}

func (f *FakeOAuth) revokeForm(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	f.revoked = append(f.revoked, r.Form.Get("token"))
	f.mu.Unlock()
}

func (f *FakeOAuth) revokeGitHub(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if _, _, ok := r.BasicAuth(); !ok {
		http.Error(w, "no basic auth", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	f.revoked = append(f.revoked, body.AccessToken)
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *FakeOAuth) discovery(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                f.IssuerURL,
		"authorization_endpoint":                f.IssuerURL + "/authorize",
		"token_endpoint":                        f.IssuerURL + "/token",
		"jwks_uri":                              f.IssuerURL + "/jwks",
		"revocation_endpoint":                   f.IssuerURL + "/revoke",
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (f *FakeOAuth) jwks(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
		{Key: &f.key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"},
	}})
}

const stateCookie = "plsr_oauth_state"

// OAuthStart begins a flow at the hub and returns where the hub sends the browser.
func (e *Env) OAuthStart(t *testing.T, provider, query string, opts ...ReqOpt) *Resp {
	t.Helper()
	return e.Do(t, "GET", "/auth/oauth/"+provider+"/start?"+query, nil, opts...)
}

// OAuthCallback plays the browser: it follows the hub's redirect to the provider
// and returns the callback request the provider sends the browser back with.
func (e *Env) OAuthCallback(t *testing.T, start *Resp) (path string, opts []ReqOpt) {
	t.Helper()
	require.Equal(t, 302, start.Status, string(start.Body))
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(start.Header.Get("Location"))
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, 302, res.StatusCode, "the provider rejected the authorization request")
	back := res.Header.Get("Location")
	prefix := PublicURL + "/api/v1"
	require.True(t, strings.HasPrefix(back, prefix), back)
	return strings.TrimPrefix(back, prefix), []ReqOpt{Cookie(stateCookie, start.Cookie(stateCookie))}
}

// OAuthFlow runs a complete flow and returns the hub's answer to the callback.
func (e *Env) OAuthFlow(t *testing.T, provider, query string, opts ...ReqOpt) *Resp {
	t.Helper()
	start := e.OAuthStart(t, provider, query, opts...)
	if !strings.Contains(start.Header.Get("Location"), "/authorize") {
		return start
	}
	path, callbackOpts := e.OAuthCallback(t, start)
	return e.Do(t, "GET", path, nil, append(callbackOpts, opts...)...)
}

// OAuthLogin logs in through the provider, creating the account if needed.
func (e *Env) OAuthLogin(t *testing.T, provider string, opts ...ReqOpt) *Resp {
	t.Helper()
	return e.OAuthFlow(t, provider, "intent=login", opts...)
}

// OAuthLink links the provider account to the client's user.
func (c *Client) OAuthLink(t *testing.T, provider string) *Resp {
	t.Helper()
	return c.env.OAuthFlow(t, provider, "intent=link", c.opts...)
}

// AuthError extracts the error code from a failed flow's redirect.
func (r *Resp) AuthError() string {
	u, err := url.Parse(r.Header.Get("Location"))
	if err != nil {
		return ""
	}
	return u.Query().Get("auth_error")
}
