package v2api

import (
	"net/http"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Principal is the caller as the ledger sees them, for surfaces outside
// /v2 that act on the record: the MCP server (internal/mcpv2). It comes
// from principalFor, so a credential maps onto an actor in one place.
type Principal struct {
	Actor ledger.Actor
	// Scope is every space the caller may touch, narrowed to the
	// credential's hubs, with an agent's autonomy per space.
	Scope ledger.Scope
	// Connection is an agent's connection, when it has one.
	Connection *ledger.Connection
	// Impersonated: an operator's impersonation session, which may read
	// but not change the record.
	Impersonated bool
}

// PrincipalError is why a request has no principal, with the HTTP status
// /v2 would answer and a sentence that says what to do.
type PrincipalError struct {
	Status  int
	Code    string
	Message string
}

func (e *PrincipalError) Error() string { return e.Message }

// Principal resolves the request's credential the way /v2 does. via is the
// surface the caller is on; MCP passes policy.ViaMCP.
func (h *Handler) Principal(r *http.Request, via policy.Via) (*Principal, *PrincipalError) {
	if h == nil || h.ledger == nil {
		return nil, &PrincipalError{Status: http.StatusServiceUnavailable, Code: codeUnavailable,
			Message: "The V2 record isn't configured on this server."}
	}
	p, e := h.principalFor(r)
	if e != nil {
		return nil, &PrincipalError{Status: e.status, Code: e.code, Message: e.message}
	}
	p.via = via
	return &Principal{Actor: p.actor, Scope: p.scope, Connection: p.connection, Impersonated: p.impersonated}, nil
}

// Ledger is the ledger this handler serves, for the surfaces that share
// its principal mapping.
func (h *Handler) Ledger() *ledger.Ledger {
	if h == nil {
		return nil
	}
	return h.ledger
}
