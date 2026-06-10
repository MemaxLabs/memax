package observability

import (
	"context"
	"log/slog"
)

// Context-stashed slog attrs are how we propagate per-request and
// per-job fields (job_id, request_id, user_id) into every log line
// without threading a *slog.Logger through function signatures.
//
// How it works: middleware wraps the incoming ctx with WithAttrs(...),
// storing an immutable attribute slice. Any slog call that passes the
// ctx — e.g. slog.InfoContext(ctx, "msg", ...) — runs through the
// ctxAttrsHandler below, which reads the stashed attrs and prepends
// them to the record. Handlers inside the chain (stdout JSON + Loki
// push) see the enriched record unchanged.
//
// Cost is negligible: one ctx lookup + one copy-with-attrs per log
// call when a ctx is passed; zero overhead for slog.Info (no ctx).
// We accept that slog.Info (without Context suffix) inside a job
// handler misses the enrichment — the fix is to use InfoContext in
// request/job code. A grep for `slog\.(Info|Warn|Error)\(` inside a
// handler with `ctx` in scope is the conventional migration target.

type ctxAttrsKey struct{}

// WithAttrs returns a new context carrying the given attrs.
// Multiple calls accumulate: later WithAttrs on top of a
// WithAttrs-derived ctx produces a ctx with the union of attrs,
// newest-last.
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(attrs) == 0 {
		return ctx
	}
	existing := attrsFromCtx(ctx)
	merged := make([]slog.Attr, 0, len(existing)+len(attrs))
	merged = append(merged, existing...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, ctxAttrsKey{}, merged)
}

func attrsFromCtx(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(ctxAttrsKey{}).([]slog.Attr)
	return v
}

// NewCtxAttrsHandler wraps a slog.Handler so every record that
// carries a ctx with stashed attrs (via WithAttrs) gets those attrs
// prepended before being handed to the inner handler.
func NewCtxAttrsHandler(inner slog.Handler) slog.Handler {
	if inner == nil {
		return nil
	}
	return &ctxAttrsHandler{inner: inner}
}

type ctxAttrsHandler struct {
	inner slog.Handler
}

func (h *ctxAttrsHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *ctxAttrsHandler) Handle(ctx context.Context, record slog.Record) error {
	attrs := attrsFromCtx(ctx)
	if len(attrs) == 0 {
		return h.inner.Handle(ctx, record)
	}
	// Clone so we don't mutate a record that may be shared by other
	// handlers (e.g., teeHandler fans out clones, but the source
	// record is still held by the caller).
	enriched := record.Clone()
	enriched.AddAttrs(attrs...)
	return h.inner.Handle(ctx, enriched)
}

func (h *ctxAttrsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ctxAttrsHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *ctxAttrsHandler) WithGroup(name string) slog.Handler {
	return &ctxAttrsHandler{inner: h.inner.WithGroup(name)}
}
