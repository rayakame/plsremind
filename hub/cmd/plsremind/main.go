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

	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/delivery"
	"github.com/rayakame/plsremind/hub/internal/server"
	"github.com/rayakame/plsremind/hub/internal/store"
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

	cfg, err := core.LoadConfig()
	if err != nil {
		return err
	}
	if err := store.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	d := &core.Deps{
		Cfg:  cfg,
		Pool: pool,
		Now:  time.Now,
		HTTP: &http.Client{Timeout: 10 * time.Second},
	}
	srv := server.New(d)
	app := srv.App
	srv.Gateway.Start(ctx)

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
			if err := server.Sweep(ctx, pool, time.Now()); err != nil && ctx.Err() == nil {
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
		srv.Gateway.Shutdown()
		if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
			slog.Error("shutdown", "err", err)
		}
	}()
	slog.Info("listening", "addr", cfg.ListenAddr, "version", server.Version)
	return app.Listen(cfg.ListenAddr, fiber.ListenConfig{DisableStartupMessage: true})
}
