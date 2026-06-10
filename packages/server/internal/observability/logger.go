package observability

import (
	"context"
	"errors"
	"log/slog"
	"os"
)

type LoggerSetup struct {
	Logger   *slog.Logger
	Enabled  bool
	Shutdown func(context.Context) error
}

func NewLogger(defaultServiceName string) (*slog.Logger, func(context.Context) error, error) {
	setup, err := SetupLogger(defaultServiceName)
	if err != nil {
		return nil, nil, err
	}
	return setup.Logger, setup.Shutdown, nil
}

func SetupLogger(defaultServiceName string) (LoggerSetup, error) {
	stdout := slog.NewJSONHandler(os.Stdout, nil)
	lokiSetup, err := SetupLoki(defaultServiceName)
	if err != nil {
		return LoggerSetup{}, err
	}
	// Wrap the final handler chain with NewCtxAttrsHandler so every
	// *Context slog call picks up attrs stashed via WithAttrs — the
	// mechanism that lets River middleware attach job_id/job_kind/
	// job_attempt to every log emitted during job processing, and
	// will let HTTP middleware do the same for request_id/user_id.
	if !lokiSetup.Enabled {
		return LoggerSetup{
			Logger:  slog.New(NewCtxAttrsHandler(stdout)),
			Enabled: false,
			Shutdown: func(context.Context) error {
				return nil
			},
		}, nil
	}
	return LoggerSetup{
		Logger:   slog.New(NewCtxAttrsHandler(NewTeeHandler(stdout, lokiSetup.Handler))),
		Enabled:  true,
		Shutdown: lokiSetup.Shutdown,
	}, nil
}

type teeHandler struct {
	handlers []slog.Handler
}

func NewTeeHandler(handlers ...slog.Handler) slog.Handler {
	filtered := make([]slog.Handler, 0, len(handlers))
	for _, handler := range handlers {
		if handler != nil {
			filtered = append(filtered, handler)
		}
	}
	return &teeHandler{handlers: filtered}
}

func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *teeHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, handler := range h.handlers {
		if !handler.Enabled(ctx, record.Level) {
			continue
		}
		if err := handler.Handle(ctx, record.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithAttrs(attrs))
	}
	return &teeHandler{handlers: handlers}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithGroup(name))
	}
	return &teeHandler{handlers: handlers}
}
