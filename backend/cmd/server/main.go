// Command server runs the Staff Display Platform API.
//
// Usage:
//
//	cd backend && go run ./cmd/server
//
// Configuration comes from configs/config[.<APP_ENV>].yaml plus environment
// variables / .env — see backend/.env.example and README.md §5.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
	"github.com/m2sound2456/staffdisplay/backend/internal/database"
	"github.com/m2sound2456/staffdisplay/backend/internal/logger"
	"github.com/m2sound2456/staffdisplay/backend/internal/server"
	"github.com/m2sound2456/staffdisplay/backend/internal/version"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "staffdisplay-server: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := logger.Init(logger.Options{
		Development: cfg.Logging.Development,
		Level:       cfg.Logging.Level,
		Encoding:    cfg.Logging.Encoding,
	}); err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()

	dbOptions := database.OptionsFromConfig(cfg.Database).WithDebug(cfg.Logging.Development)
	logger.Info("server_starting",
		zap.String("version", version.String()),
		zap.String("environment", cfg.App.Environment),
		zap.String("addr", server.Addr(cfg.Server)),
	)
	logger.Info("database_connecting", zap.String("target", dbOptions.RedactedDSN()))

	db, err := database.Open(dbOptions)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.New(cfg, db).Run(ctx); err != nil {
		return err
	}

	logger.Info("server_stopped")
	return nil
}
