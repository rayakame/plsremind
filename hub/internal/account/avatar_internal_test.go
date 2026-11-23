package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func pictureServer(t *testing.T, wait time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(wait):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestATrustedOriginMayBeLocal(t *testing.T) {
	srv := pictureServer(t, 0)
	got, err := fetchPicture(context.Background(), srv.Client(), avatarPolicy{origin: srv.URL}, srv.URL+"/me.png")
	require.NoError(t, err)
	require.Equal(t, &picture{data: []byte("png"), kind: "image/png"}, got)
}

func TestAnotherOriginMustNotBeLocal(t *testing.T) {
	srv := pictureServer(t, 0)
	_, err := fetchPicture(context.Background(), srv.Client(), avatarPolicy{origin: "https://idp.example", public: true}, srv.URL+"/me.png")
	require.Error(t, err)
}

func TestWithoutPublicPicturesOnlyTheOriginCounts(t *testing.T) {
	srv := pictureServer(t, 0)
	_, err := fetchPicture(context.Background(), srv.Client(), avatarPolicy{origin: "https://cdn.discordapp.com"}, srv.URL+"/me.png")
	require.Error(t, err)
}

func TestAnotherOriginMustUseHTTPS(t *testing.T) {
	_, err := fetchPicture(context.Background(), http.DefaultClient, avatarPolicy{origin: "https://idp.example", public: true}, "http://pictures.example/me.png")
	require.Error(t, err)
}

func TestASlowPictureIsGivenUp(t *testing.T) {
	srv := pictureServer(t, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := fetchPicture(ctx, srv.Client(), avatarPolicy{origin: srv.URL}, srv.URL+"/me.png")
	require.Error(t, err)
}

func TestOriginOf(t *testing.T) {
	require.Equal(t, "https://cdn.discordapp.com", originOf("https://cdn.discordapp.com/avatars/1/a.png?size=128"))
	require.Equal(t, "http://127.0.0.1:8080", originOf("http://127.0.0.1:8080/x"))
	require.Equal(t, "", originOf("::not a url"))
}

func TestAPictureLeavesNoConnectionBehind(t *testing.T) {
	for _, trusted := range []bool{true, false} {
		transport, ok := guarded(http.DefaultClient, trusted).Transport.(*http.Transport)
		require.True(t, ok)
		require.True(t, transport.DisableKeepAlives)
	}
}
