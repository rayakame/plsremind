package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	// Time zone names must resolve even where the system ships no zone database.
	_ "time/tzdata"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/config"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/delivery"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/httpapi"
)

// tickEvery is how often the hub looks for reminders whose time has come.
const tickEvery = 5 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("plsremind stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	d := &deps.Deps{
		Cfg:  cfg,
		Pool: pool,
		Now:  time.Now,
		HTTP: &http.Client{Timeout: 10 * time.Second},
	}
	server := httpapi.NewServer(d)
	app := server.App
	server.Gateway.Start(ctx)

	go func() {
		ticker := time.NewTicker(tickEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := delivery.RunOnce(ctx, d); err != nil && ctx.Err() == nil {
					slog.Error("fire reminders", "err", err)
				}
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			if err := httpapi.Sweep(ctx, pool, time.Now()); err != nil && ctx.Err() == nil {
				slog.Error("sweep expired rows", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	go func() {
		<-ctx.Done()
		// Gateway clients are told to reconnect before the listener goes away.
		server.Gateway.Shutdown()
		if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
			slog.Error("shutdown", "err", err)
		}
	}()
	slog.Info("listening", "addr", cfg.ListenAddr, "version", httpapi.Version)
	return app.Listen(cfg.ListenAddr, fiber.ListenConfig{DisableStartupMessage: true})
}
