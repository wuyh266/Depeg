package logging

import (
	"context"
	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/port"
	"log/slog"
	"os"
)

type logger struct{ output *slog.Logger }

func New(settings config.Log) port.Logger {
	level := slog.LevelInfo
	_ = level.UnmarshalText([]byte(settings.Level))
	return &logger{output: slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))}
}
func (l *logger) Info(ctx context.Context, message string, fields ...any) {
	l.output.InfoContext(ctx, message, fields...)
}
func (l *logger) Error(ctx context.Context, message string, fields ...any) {
	l.output.ErrorContext(ctx, message, fields...)
}
