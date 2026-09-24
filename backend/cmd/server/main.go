// Command server runs the Staff Display Platform API.
//
// Usage:
//
//	cd backend && go run ./cmd/server
//	cd backend && go run ./cmd/server --check   # validate configuration and exit
//
// Configuration comes from configs/config.yaml plus the APP_ENV profile
// (configs/config.<APP_ENV>.yaml) and the environment variables / .env — see
// backend/.env.example, README.md §5 and docs/DEPLOYMENT.md §1–§2.
package main

import (
	"context"
	"flag"
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

// Process exit codes (also used by the --check deployment gate).
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("staffdisplay-server", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	check := flags.Bool("check", false, "validate the effective configuration, print a redacted summary and exit")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	cfg, err := config.Load()
	if err != nil {
		return fail(err)
	}

	// --check is the deployment gate: it proves a (production) configuration is
	// valid without opening a listener or a database connection. Every secret is
	// masked in the output (config.Config.Summary).
	if *check {
		fmt.Printf("configuration is valid: %s (tier %s)\n", cfg.App.Environment, config.ActiveTier(cfg))
		for _, line := range cfg.Summary() {
			fmt.Printf("  %s\n", line)
		}
		return exitOK
	}

	if err := serve(cfg); err != nil {
		return fail(err)
	}
	return exitOK
}

// serve starts the logger, the database pool and the HTTP server, then waits for
// a shutdown signal.
func serve(cfg *config.Config) error {
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
		zap.String("tier", string(config.ActiveTier(cfg))),
		zap.Strings("config_files", cfg.Files),
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

// fail reports an error the way the operator sees it on stderr.
func fail(err error) int {
	_, _ = fmt.Fprintf(os.Stderr, "staffdisplay-server: %v\n", err)
	return exitError
}
