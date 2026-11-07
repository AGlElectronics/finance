// Package app wires configuration, the database pool and HTTP into a runnable
// server. main.go calls Run.
//
// IMPORTANT: the server does NOT run migrations on boot. cmd/migrate is a
// separate one-shot container (and binary). This keeps API startup fast,
// avoids racey leader-election, and makes rollbacks predictable.
package app

import (
	"context"
	"errors"
	"fmt"
	nethttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/finance/backend/internal/auth"
	"github.com/AGlElectronics/finance/backend/internal/config"
	"github.com/AGlElectronics/finance/backend/internal/db"
	"github.com/AGlElectronics/finance/backend/internal/denlink"
	httpx "github.com/AGlElectronics/finance/backend/internal/http"
	"github.com/AGlElectronics/finance/backend/internal/tlscert"
)

// Version is set via -ldflags at build time.
var Version = "dev"

// Run boots the server. It blocks until a shutdown signal is received.
func Run(ctx context.Context, logger zerolog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger.Info().
		Str("env", cfg.AppEnv).
		Str("addr", cfg.HTTPAddr).
		Str("version", Version).
		Str("summary", cfg.String()).
		Msg("starting finance backend")

	dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pool, err := db.Open(dbCtx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer pool.Close()
	logger.Info().Msg("database pool ready")

	var oidcVerifier *auth.OIDCVerifier
	if cfg.AuthMode == config.AuthModeOIDC {
		v, err := auth.NewOIDCVerifier(ctx, cfg)
		if err != nil {
			return fmt.Errorf("oidc verifier: %w", err)
		}
		oidcVerifier = v
		logger.Info().Str("issuer", cfg.OIDCIssuerURL).Msg("oidc verifier ready")
	}

	if cfg.S3Configured() {
		logger.Info().
			Str("endpoint", cfg.S3Endpoint).
			Str("bucket", cfg.S3Bucket).
			Msg("object storage configured (LOCAL DEV ONLY; MinIO must never be used in production)")
	}

	den := denlink.New(pool, denlink.Config{Slug: "finance", Version: Version, InternalURL: cfg.DenInternalURL, Mode: cfg.DenMode}, logger)
	if err := den.Restore(ctx); err != nil {
		logger.Warn().Err(err).Msg("den link: load failed")
	}
	go den.Run(ctx)

	router := httpx.NewRouter(httpx.Deps{
		Config:       cfg,
		Pool:         pool,
		Logger:       logger,
		OIDCVerifier: oidcVerifier,
		Den:          den,
	}, Version)

	server := &nethttp.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if _, err := tlscert.Configure(ctx, server, logger); err != nil {
		return fmt.Errorf("tls: %w", err)
	}

	return runServer(ctx, server, pool, cfg.ShutdownTimeout, logger)
}

func runServer(ctx context.Context, server *nethttp.Server, pool *pgxpool.Pool, shutdownTimeout time.Duration, logger zerolog.Logger) error {
	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info().Str("addr", server.Addr).Msg("http listening")
		if err := tlscert.ListenAndServe(server); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-signalCtx.Done():
		logger.Info().Msg("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("http shutdown error")
	}
	pool.Close()
	logger.Info().Msg("shutdown complete")
	return nil
}
