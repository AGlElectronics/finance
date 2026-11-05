// Package main is the Finance API server entrypoint. Migrations are a separate
// one-shot (cmd/migrate); the API MUST NOT auto-migrate.
package main

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/finance/backend/internal/app"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	zerolog.TimeFieldFormat = time.RFC3339

	if os.Getenv("APP_ENV") == "development" {
		logger = logger.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339})
	}

	ctx := context.Background()
	if err := app.Run(ctx, logger); err != nil {
		logger.Fatal().Err(err).Msg("server failed")
	}
}
