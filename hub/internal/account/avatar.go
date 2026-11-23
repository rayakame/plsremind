package account

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const (
	pictureLimit   = 1 << 20
	pictureTimeout = 5 * time.Second
)

var (
	pictureTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}
	sharedSpace  = netip.MustParsePrefix("100.64.0.0/10")
)

// avatarPolicy says where a provider's pictures may come from: its own origin, and with
// public also any https address that is not on a private network.
type avatarPolicy struct {
	origin string
	public bool
}

type picture struct {
	data []byte
	kind string
}

func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// fetchPicture fetches an account's picture under the provider's policy.
func fetchPicture(ctx context.Context, base *http.Client, policy avatarPolicy, raw string) (*picture, error) {
	origin := originOf(raw)
	trusted := origin != "" && origin == policy.origin
	if !trusted && (!policy.public || !strings.HasPrefix(origin, "https://")) {
		return nil, fmt.Errorf("a picture from %q is not allowed", origin)
	}
	ctx, cancel := context.WithTimeout(ctx, pictureTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	res, err := guarded(base, trusted).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch picture: status %d", res.StatusCode)
	}
	kind := strings.ToLower(strings.TrimSpace(strings.Split(res.Header.Get("Content-Type"), ";")[0]))
	if !pictureTypes[kind] {
		return nil, fmt.Errorf("fetch picture: type %q", kind)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, pictureLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > pictureLimit {
		return nil, fmt.Errorf("fetch picture: %d bytes", len(data))
	}
	return &picture{data: data, kind: kind}, nil
}

func guarded(base *http.Client, trusted bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if own, ok := base.Transport.(*http.Transport); ok {
		transport = own.Clone()
	}
	if !trusted {
		dialer := &net.Dialer{Timeout: pictureTimeout, Control: publicOnly}
		transport.DialContext = dialer.DialContext
		// A proxy would connect for us, out of sight of the check on the address.
		transport.Proxy = nil
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// publicOnly refuses connections to addresses that are not on the public internet, so a
// picture address cannot make the hub reach into its own network.
func publicOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || sharedSpace.Contains(ip) {
		return errors.New("a picture on a private address is not fetched")
	}
	return nil
}

// pictureOf fetches the picture of a profile. keep is true when fetching failed, so
// that a picture stored earlier stays rather than being thrown away over a hiccup.
func pictureOf(ctx context.Context, base *http.Client, profile OAuthProfile) (pic *picture, keep bool) {
	if profile.AvatarURL == "" {
		return nil, false
	}
	pic, err := fetchPicture(ctx, base, profile.avatar, profile.AvatarURL)
	if err != nil {
		slog.Info("the picture of a linked account was not fetched", "err", err)
		return nil, true
	}
	return pic, false
}
