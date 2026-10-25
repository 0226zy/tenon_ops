// Package app wires configuration, storage, security and HTTP for serve/doctor.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/0226zy/tenon_ops/internal/api"
	"github.com/0226zy/tenon_ops/internal/auth"
	"github.com/0226zy/tenon_ops/internal/config"
	"github.com/0226zy/tenon_ops/internal/security"
	"github.com/0226zy/tenon_ops/internal/services"
	"github.com/0226zy/tenon_ops/internal/store"
)

type App struct {
	Cfg      *config.Config
	Keys     *security.Keys
	Store    *store.Store
	Auth     *auth.Service
	Services *services.Services
	Handler  http.Handler
}

func New(ctx context.Context, cfg *config.Config) (*App, error) {
	keys, err := security.Load(cfg.Security.KeysDir)
	if err != nil {
		return nil, err
	}
	db, err := store.Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := db.CheckTenonSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	a := auth.New(db, keys, cfg)
	svc := services.New(db, keys, cfg, a)
	handler := api.New(cfg, a, svc)
	return &App{Cfg: cfg, Keys: keys, Store: db, Auth: a, Services: svc, Handler: handler}, nil
}

func (a *App) Close() error {
	if a == nil || a.Store == nil {
		return nil
	}
	return a.Store.Close()
}

func (a *App) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", a.Cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", a.Cfg.Listen, err)
	}
	srv := &http.Server{
		Addr:              a.Cfg.Listen,
		Handler:           a.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	slog.Info("tenon-ops listening", "addr", ln.Addr().String(), "mode", a.Cfg.Mode, "exposure", a.Cfg.Exposure, "environment", a.Cfg.Environment)
	cleanupCtx, cancelCleanup := context.WithCancel(ctx)
	defer cancelCleanup()
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				if err := a.Store.CleanupSessions(cleanupCtx); err != nil {
					slog.Warn("cleanup sessions", "error", err)
				}
				if err := a.Store.CleanupOperations(cleanupCtx); err != nil {
					slog.Warn("cleanup operations", "error", err)
				}
			}
		}
	}()
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
