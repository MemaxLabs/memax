// Package meterctx provides context helpers for the metering system.
// This is a leaf package with no dependencies on handler or meter,
// breaking the import cycle between them.
package meterctx

import (
	"context"
	"sync"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

type contextKey int

const (
	tokenCtxKey contextKey = iota
	limitsCtxKey
	usageEventInfoCtxKey
)

// Token represents a metering reservation that can be committed or rolled back.
// The meter package provides the concrete implementation; handlers interact
// through this interface.
type Token struct {
	mu         sync.Mutex
	committed  bool
	rolledBack bool
	onCommit   func()
	onRollback func()
}

type UsageEventInfo struct {
	mu       sync.Mutex
	Metadata map[string]any
}

func NewUsageEventInfo() *UsageEventInfo {
	return &UsageEventInfo{Metadata: map[string]any{}}
}

func (i *UsageEventInfo) Set(key string, value any) {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.Metadata == nil {
		i.Metadata = map[string]any{}
	}
	i.Metadata[key] = value
}

func (i *UsageEventInfo) Snapshot() map[string]any {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.Metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(i.Metadata))
	for k, v := range i.Metadata {
		out[k] = v
	}
	return out
}

// NewToken creates a Token with commit/rollback callbacks.
func NewToken(onCommit, onRollback func()) *Token {
	return &Token{onCommit: onCommit, onRollback: onRollback}
}

// Commit signals successful operation. Idempotent.
func (t *Token) Commit() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.committed || t.rolledBack {
		return
	}
	t.committed = true
	if t.onCommit != nil {
		t.onCommit()
	}
}

// Rollback releases the reservation. Idempotent.
func (t *Token) Rollback() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.committed || t.rolledBack {
		return
	}
	t.rolledBack = true
	if t.onRollback != nil {
		t.onRollback()
	}
}

// IsCommitted returns whether this token was committed.
func (t *Token) IsCommitted() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.committed
}

// MarkRolledBack sets the token as rolled back without calling the callback.
// Used by Reserve when the request is over quota (already DECR'd).
func (t *Token) MarkRolledBack() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rolledBack = true
}

// --- Context helpers ---

// WithToken injects a Token into the request context.
func WithToken(ctx context.Context, token *Token) context.Context {
	return context.WithValue(ctx, tokenCtxKey, token)
}

// TokenFromContext retrieves the Token from context.
func TokenFromContext(ctx context.Context) *Token {
	t, _ := ctx.Value(tokenCtxKey).(*Token)
	return t
}

// CommitFromContext gets the token from context and commits. No-op if absent.
func CommitFromContext(ctx context.Context) {
	TokenFromContext(ctx).Commit()
}

// RollbackFromContext gets the token from context and rolls back. No-op if absent.
func RollbackFromContext(ctx context.Context) {
	TokenFromContext(ctx).Rollback()
}

// WithUserLimits injects UserLimits into the request context.
func WithUserLimits(ctx context.Context, limits *model.UserLimits) context.Context {
	return context.WithValue(ctx, limitsCtxKey, limits)
}

// UserLimitsFromContext retrieves UserLimits from context. Nil if not set.
func UserLimitsFromContext(ctx context.Context) *model.UserLimits {
	l, _ := ctx.Value(limitsCtxKey).(*model.UserLimits)
	return l
}

func WithUsageEventInfo(ctx context.Context, info *UsageEventInfo) context.Context {
	return context.WithValue(ctx, usageEventInfoCtxKey, info)
}

func UsageEventInfoFromContext(ctx context.Context) *UsageEventInfo {
	info, _ := ctx.Value(usageEventInfoCtxKey).(*UsageEventInfo)
	return info
}

func SetUsageEventField(ctx context.Context, key string, value any) {
	UsageEventInfoFromContext(ctx).Set(key, value)
}
