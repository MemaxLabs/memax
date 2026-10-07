package mcpv2

import (
	"context"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// The compiled digest (plan 25 §5.11): at session start an agent gets the
// space's precompiled context, its latest good compile of the target
// delivered over MCP (else AGENTS.md, else the ChatGPT copy-out), plus
// what changed since it was last seen, what waits in Review and what was
// forgotten. A space with no compiled target yet falls back to the
// lexical digest.

// Previewer reads a target's latest good compile (compile.Service).
type Previewer interface {
	Preview(ctx context.Context, scope ledger.Scope, targetID uuid.UUID) (*compile.Preview, error)
}

// maxCompiled bounds the compiled content a digest carries (Codex's
// AGENTS.md cap is 32 KiB).
const maxCompiled = 32 << 10

type compiledDigest struct {
	ledger  *ledger.Ledger
	preview Previewer
	lexical Digester
	log     *slog.Logger

	// cache holds artifacts by compile ref. A ref's output never changes,
	// and the latest good ref is read from the record on every call, so a
	// re-render (after Forget, say) is a new ref and never served stale.
	mu    sync.Mutex
	cache map[string]cachedArtifact
}

type cachedArtifact struct {
	compiled handler.MCPCompiled
	space    uuid.UUID
	at       time.Time
}

const artifactTTL = 10 * time.Minute

// isNilPreviewer catches a typed nil (a nil *compile.Service in the
// interface): the compile service is nil without a compile URL.
func isNilPreviewer(p Previewer) bool {
	s, ok := p.(*compile.Service)
	return ok && s == nil
}

func newCompiledDigest(l *ledger.Ledger, p Previewer, lexical Digester, log *slog.Logger) *compiledDigest {
	return &compiledDigest{ledger: l, preview: p, lexical: lexical, log: log, cache: map[string]cachedArtifact{}}
}

func (d *compiledDigest) Digest(ctx context.Context, scope ledger.Scope, spaces []SpaceRef, since *time.Time) (Digest, error) {
	out, err := d.lexical.Digest(ctx, scope, spaces, since)
	if err != nil {
		return out, err
	}
	for i := range out.Spaces {
		c, err := d.compiled(ctx, scope, spaces[i].ID)
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			d.log.WarnContext(ctx, "mcp: no compiled digest; serving the lexical one", "space_id", spaces[i].ID.String(), "error", err)
			continue
		}
		if c != nil {
			out.Spaces[i].Compiled = c
			out.Spaces[i].Sections = []handler.MCPDigestSection{}
		}
	}
	return out, nil
}

// compiled is the space's latest compiled file, or nil before its first
// compile.
func (d *compiledDigest) compiled(ctx context.Context, scope ledger.Scope, spaceID uuid.UUID) (*handler.MCPCompiled, error) {
	targets, err := d.ledger.ListTargets(ctx, scope, spaceID)
	if err != nil {
		return nil, err
	}
	t := pickTarget(targets)
	if t == nil || t.LastCompile == nil {
		return nil, nil
	}
	if c, ok := d.cached(t.LastCompile.Ref); ok && t.LastCompile.Status != ledger.CompileFailed {
		return &c, nil
	}
	p, err := d.preview.Preview(ctx, scope, t.ID)
	if err != nil || p.Compile == nil {
		return nil, err
	}
	var content string
	switch {
	case len(p.Files) > 0:
		content = p.Files[0].Content
	case len(p.Copies) > 0:
		content = p.Copies[0].Content
	default:
		return nil, nil
	}
	c := handler.MCPCompiled{Ref: p.Compile.Ref, Target: t.Label, CompiledAt: p.Compile.CompiledAt.UTC().Format(time.RFC3339)}
	c.Content, c.Truncated = truncateUTF8(content, maxCompiled)
	d.store(spaceID, p.Compile.Ref, c)
	return &c, nil
}

// pickTarget prefers the target agents read over MCP, then AGENTS.md (the
// canonical file), then the ChatGPT copy-out.
func pickTarget(targets []ledger.Target) *ledger.Target {
	for _, want := range []func(ledger.Target) bool{
		func(t ledger.Target) bool { return t.Delivery == ledger.DeliveryMCP },
		func(t ledger.Target) bool { return t.Kind == ledger.TargetAgentsMD },
		func(t ledger.Target) bool { return t.Kind == ledger.TargetChatGPT },
	} {
		for i := range targets {
			if want(targets[i]) && targets[i].SyncState != ledger.SyncOff {
				return &targets[i]
			}
		}
	}
	return nil
}

func (d *compiledDigest) cached(ref string) (handler.MCPCompiled, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.cache[ref]
	if !ok || time.Since(c.at) > artifactTTL {
		return handler.MCPCompiled{}, false
	}
	return c.compiled, true
}

func (d *compiledDigest) store(space uuid.UUID, ref string, c handler.MCPCompiled) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.cache) >= 512 {
		d.cache = map[string]cachedArtifact{}
	}
	// A compile ref's output never changes, but a newer one replaces it:
	// drop the space's older copies, which may hold a forgotten line.
	for k, a := range d.cache {
		if a.space == space && k != ref {
			delete(d.cache, k)
		}
	}
	d.cache[ref] = cachedArtifact{compiled: c, space: space, at: time.Now()}
}

// purgeSpace drops a space's cached compiled files (Forget's propagation).
func (d *compiledDigest) purgeSpace(space uuid.UUID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, a := range d.cache {
		if a.space == space {
			delete(d.cache, k)
		}
	}
}

func truncateUTF8(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}
