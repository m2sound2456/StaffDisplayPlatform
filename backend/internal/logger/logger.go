// Package logger provides the process-wide structured logger (zap).
//
// The package is intentionally independent from config so any layer can log
// without importing configuration types.
package logger

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Options configures the global logger instance.
type Options struct {
	// Development enables the human friendly console encoder.
	Development bool
	// Level is one of: debug, info, warn, warning, error, fatal.
	Level string
	// Encoding is "console" or "json" (json is recommended in production).
	Encoding string
}

var (
	mu     sync.RWMutex
	base   *zap.Logger
	inited bool
)

// Init installs the global logger. It must be called before the first log call
// to get structured output; the package is safe to use without it (no-op logger).
func Init(opts Options) error {
	level, err := parseLevel(opts.Level)
	if err != nil {
		return err
	}

	core := zapcore.NewCore(buildEncoder(opts), zapcore.AddSync(os.Stdout), level)
	zapOpts := []zap.Option{
		zap.AddCaller(),
		zap.AddCallerSkip(1),
		zap.AddStacktrace(zapcore.ErrorLevel),
	}
	if opts.Development {
		zapOpts = append(zapOpts, zap.Development())
	}

	l := zap.New(core, zapOpts...)

	mu.Lock()
	defer mu.Unlock()
	base = l
	inited = true
	return nil
}

// Sync flushes buffered log entries.
func Sync() error {
	mu.RLock()
	l, ok := base, inited
	mu.RUnlock()
	if !ok || l == nil {
		return nil
	}
	return l.Sync()
}

// L returns the active logger, or a no-op logger before Init.
func L() *zap.Logger {
	mu.RLock()
	l, ok := base, inited
	mu.RUnlock()
	if ok && l != nil {
		return l
	}
	return zap.NewNop()
}

// With returns a logger with additional fields.
func With(fields ...zap.Field) *zap.Logger { return L().With(fields...) }

// WithRequestID returns a logger bound to the given HTTP request id.
func WithRequestID(requestID string) *zap.Logger {
	l := L()
	if requestID == "" {
		return l
	}
	return l.With(zap.String("request_id", requestID))
}

// Debug logs at debug level.
func Debug(msg string, fields ...zap.Field) { L().Debug(msg, fields...) }

// Info logs at info level.
func Info(msg string, fields ...zap.Field) { L().Info(msg, fields...) }

// Warn logs at warn level.
func Warn(msg string, fields ...zap.Field) { L().Warn(msg, fields...) }

// Error logs at error level.
func Error(msg string, fields ...zap.Field) { L().Error(msg, fields...) }

// Fatal logs at fatal level and exits.
func Fatal(msg string, fields ...zap.Field) { L().Fatal(msg, fields...) }

func buildEncoder(opts Options) zapcore.Encoder {
	var cfg zapcore.EncoderConfig
	if opts.Development {
		cfg = zap.NewDevelopmentEncoderConfig()
		cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		cfg = zap.NewProductionEncoderConfig()
		cfg.EncodeLevel = zapcore.LowercaseLevelEncoder
	}
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder

	if strings.EqualFold(strings.TrimSpace(opts.Encoding), "console") {
		return zapcore.NewConsoleEncoder(cfg)
	}
	return zapcore.NewJSONEncoder(cfg)
}

func parseLevel(value string) (zapcore.LevelEnabler, error) {
	level := strings.ToLower(strings.TrimSpace(value))
	if level == "" {
		level = "info"
	}
	if level == "warning" {
		level = "warn"
	}
	var parsed zapcore.Level
	if err := parsed.Set(level); err != nil {
		return nil, fmt.Errorf("parse logging level: %w", err)
	}
	return parsed, nil
}
