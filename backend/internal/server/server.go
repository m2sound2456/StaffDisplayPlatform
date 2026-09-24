package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
	"github.com/m2sound2456/staffdisplay/backend/internal/database"
	"github.com/m2sound2456/staffdisplay/backend/internal/logger"
	"go.uber.org/zap"
)

// Server owns the HTTP listener lifecycle.
type Server struct {
	cfg        *config.Config
	handler    http.Handler
	httpServer *http.Server
}

// New builds the HTTP server, including the router.
func New(cfg *config.Config, db *database.Database) *Server {
	handler := NewRouter(cfg, db)

	srv := &http.Server{
		Addr:         Addr(cfg.Server),
		Handler:      handler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}
	if cfg.Server.TLS.Enabled {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	return &Server{cfg: cfg, handler: handler, httpServer: srv}
}

// Addr returns the host:port the server listens on.
func Addr(cfg config.ServerConfig) string {
	host := cfg.Host
	if host == "" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, strconv.Itoa(cfg.Port))
}

// Handler exposes the router (used by tests and by embedding deployments).
func (s *Server) Handler() http.Handler { return s.handler }

// Run serves until ctx is cancelled and then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		errCh <- s.listen()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("server_shutdown_started", zap.Duration("grace", s.cfg.App.ShutdownGrace))
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.App.ShutdownGrace)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}

func (s *Server) listen() error {
	if s.cfg.Server.TLS.Enabled {
		if s.cfg.Server.TLS.CertFile == "" || s.cfg.Server.TLS.KeyFile == "" {
			return errors.New("server.tls.cert_file and server.tls.key_file are required when TLS is enabled")
		}
		return s.httpServer.ListenAndServeTLS(s.cfg.Server.TLS.CertFile, s.cfg.Server.TLS.KeyFile)
	}
	return s.httpServer.ListenAndServe()
}
