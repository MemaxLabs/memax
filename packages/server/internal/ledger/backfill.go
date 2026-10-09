package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// BackfillOptions scopes BackfillConnections.
type BackfillOptions struct {
	// Users limits the backfill to these people. Empty means everyone.
	Users []uuid.UUID
}

// BackfillReport counts what BackfillConnections did, by credential.
type BackfillReport struct {
	Connected        int `json:"connected"`
	AlreadyConnected int `json:"already_connected"`
	// NoSpaces: the credential reaches none of its person's spaces (a key
	// bound to a hub they left), so there is nothing to connect it to.
	NoSpaces int `json:"no_spaces"`
	Failed   int `json:"failed"`
}

// BackfillConnections connects every active V1 API key and OAuth grant
// that has no agent connection yet (plan 25 §10), each to every space it
// can reach, at Propose or the space's default for new agents if that is
// lower, and at Read when the credential can't write. That is a behaviour
// change for V1 credentials on /v2, on purpose: before connections, an
// agent with write access proposed everywhere; now the person can raise it
// per space. The receipts name Memax as the actor ("connected from a V1
// credential").
//
// It is idempotent: each credential's ConnectAgent carries an
// idempotency key derived from the credential, and a credential that is
// already connected is skipped. Run it with cmd/v2-backfill-agents.
//
// It is not run automatically. Its receipts and connections are V2
// records in the person's spaces, and V2 records block V1's space and
// account deletion until those go through the ledger (plan 25 Phase 0
// log), so run it for the people moving to V2 (Options.Users) until then.
func (l *Ledger) BackfillConnections(ctx context.Context, opts BackfillOptions) (BackfillReport, error) {
	if l == nil {
		return BackfillReport{}, ErrDisabled
	}
	creds, err := v1Credentials(ctx, l.pool, opts.Users)
	if err != nil {
		return BackfillReport{}, err
	}
	var report BackfillReport
	for _, c := range creds {
		scope, err := ResolveUserScope(ctx, l.pool, c.user)
		if err != nil {
			return report, err
		}
		if c.narrow() {
			scope = scope.Narrow(c.hubs...)
		}
		if len(scope.Spaces) == 0 {
			report.NoSpaces++
			continue
		}
		spaces := make([]SpaceAutonomy, len(scope.Spaces))
		for i, g := range scope.Spaces {
			spaces[i] = SpaceAutonomy{SpaceID: g.SpaceID}
		}
		limit := policy.AutonomyPropose
		if !c.canWrite {
			limit = policy.AutonomyRead
		}
		agent := AgentFromV1(c.agent)
		res, err := l.Apply(ctx, &ConnectAgent{
			Meta: Meta{
				Actor: Actor{Kind: policy.ActorMemax, Name: "Memax"}, Scope: scope, Via: policy.ViaSystem,
				IdempotencyKey: "v1-backfill:" + string(c.kind) + ":" + c.id.String(),
				Reason:         "Connected from a V1 credential.",
			},
			Person: c.user, Credential: c.kind, CredentialID: c.id, Agent: agent,
			DisplayName: c.displayName(agent), Spaces: spaces, Cap: limit,
		})
		switch {
		case errors.Is(err, ErrAlreadyConnected), err == nil && res.Replayed:
			report.AlreadyConnected++
		case errors.Is(err, ErrNotFound):
			// Revoked or expired since the list was read.
			report.NoSpaces++
		case err != nil:
			report.Failed++
			l.log.Error("ledger: backfill connection failed", "credential", string(c.kind), "credential_id", c.id.String(), "error", err)
		case res.Outcome == OutcomeRefused:
			report.Failed++
			l.log.Error("ledger: backfill connection refused", "credential", string(c.kind), "credential_id", c.id.String(), "policy", res.Policy.Code)
		default:
			report.Connected++
		}
	}
	l.log.Info("ledger: backfilled agent connections", "connected", report.Connected,
		"already_connected", report.AlreadyConnected, "no_spaces", report.NoSpaces, "failed", report.Failed)
	return report, nil
}

// v1Credential is an active API key or OAuth grant, as V1 stores it.
type v1Credential struct {
	kind     CredentialKind
	id       uuid.UUID
	user     uuid.UUID
	name     string // the key's name, or the OAuth client's
	agent    string // V1 agent slug
	mode     string // hub_scope_mode
	hubs     []uuid.UUID
	canWrite bool
}

// narrow mirrors the /v2 principal: a credential that names hubs (the
// allowlist mode, a hub list, or an old key's single hub) is limited to
// them.
func (c v1Credential) narrow() bool { return c.mode == "hub_allowlist" || len(c.hubs) > 0 }

func (c v1Credential) displayName(agent AgentKind) string {
	if agent != AgentOther {
		return agent.Name()
	}
	for _, n := range []string{c.agent, c.name} {
		if n = strings.TrimSpace(n); n != "" {
			if len([]rune(n)) > MaxDisplayName {
				n = string([]rune(n)[:MaxDisplayName])
			}
			return n
		}
	}
	return agent.Name()
}

// v1Credentials reads the active credentials as the login role: like
// ResolveUserScope, this is identity data the scope is built from.
func v1Credentials(ctx context.Context, db Querier, users []uuid.UUID) ([]v1Credential, error) {
	if users == nil {
		users = []uuid.UUID{}
	}
	rows, err := db.Query(ctx, `
		SELECT 'api_key', k.id, k.user_id, k.name, k.agent_name, k.hub_scope_mode,
		       CASE WHEN cardinality(k.hub_ids) = 0 AND k.hub_id IS NOT NULL THEN ARRAY[k.hub_id] ELSE k.hub_ids END,
		       'memory:write' = ANY (k.default_permissions)
		  FROM public.api_keys k
		 WHERE k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now())
		   AND (cardinality($1::uuid[]) = 0 OR k.user_id = ANY ($1))
		UNION ALL
		SELECT 'oauth_grant', g.id, g.user_id, COALESCE(c.client_name, ''), g.agent_name, g.hub_scope_mode, g.hub_ids,
		       'memory:write' = ANY (g.default_permissions)
		  FROM public.oauth_grants g
		  LEFT JOIN public.oauth_clients c ON c.client_id = g.client_id
		 WHERE g.revoked_at IS NULL AND (g.expires_at IS NULL OR g.expires_at > now())
		   AND (cardinality($1::uuid[]) = 0 OR g.user_id = ANY ($1))
		 ORDER BY 3, 2`, users)
	if err != nil {
		return nil, fmt.Errorf("ledger: list V1 credentials: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (v1Credential, error) {
		var c v1Credential
		err := r.Scan(&c.kind, &c.id, &c.user, &c.name, &c.agent, &c.mode, &c.hubs, &c.canWrite)
		return c, err
	})
}
