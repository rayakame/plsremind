package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL string
	PublicURL   string
	ListenAddr  string
	// ProxyHeader names the header a reverse proxy puts the client address in.
	ProxyHeader string
	SecretKey   [32]byte
}

// Load reads the configuration from PLSREMIND_* environment variables.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("PLSREMIND_DATABASE_URL"),
		PublicURL:   strings.TrimRight(os.Getenv("PLSREMIND_PUBLIC_URL"), "/"),
		ListenAddr:  os.Getenv("PLSREMIND_LISTEN_ADDR"),
		ProxyHeader: os.Getenv("PLSREMIND_PROXY_HEADER"),
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("PLSREMIND_DATABASE_URL is required")
	}
	u, err := url.Parse(cfg.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Config{}, errors.New("PLSREMIND_PUBLIC_URL must be an http or https URL")
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("PLSREMIND_SECRET_KEY"))
	if err != nil || len(key) != len(cfg.SecretKey) {
		return Config{}, fmt.Errorf("PLSREMIND_SECRET_KEY must be %d bytes, base64 encoded", len(cfg.SecretKey))
	}
	copy(cfg.SecretKey[:], key)
	return cfg, nil
}
