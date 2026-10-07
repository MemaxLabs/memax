package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

const gateSelect = `
	SELECT g.id, g.tenant_id, g.space_id, g.seq, COALESCE(g.question, ''), COALESCE(g.context, ''), g.options,
	       g.status, g.expires_at, g.asked_by, COALESCE(g.agent, ''), COALESCE(g.requesting_session, ''),
	       g.answer_option, g.answer_memory_id, m.seq, g.answered_by, g.answered_at, COALESCE(g.assurance, ''),
	       COALESCE(g.withdrawn_by_kind, ''), g.withdrawn_by, g.withdrawn_at, g.delivered_at,
	       g.stream_version, g.created_receipt_id, g.last_receipt_id, g.created_at, g.updated_at
	  FROM v2.decision_gates g
	  LEFT JOIN v2.memories m ON m.id = g.answer_memory_id AND m.space_id = g.space_id`

// scanGate reads one gate. now decides whether a waiting gate has expired.
func scanGate(row pgx.Row, now time.Time) (*Gate, error) {
	var g Gate
	var options []byte
	var answerOption *int
	var answerMemory, answeredBy, withdrawnBy *uuid.UUID
	var memorySeq *int64
	var answeredAt, withdrawnAt *time.Time
	var assurance, withdrawnKind string
	if err := row.Scan(&g.ID, &g.TenantID, &g.SpaceID, &g.seq, &g.Question, &g.Context, &options,
		&g.stored, &g.ExpiresAt, &g.AskedBy, &g.Agent, &g.SessionRef,
		&answerOption, &answerMemory, &memorySeq, &answeredBy, &answeredAt, &assurance,
		&withdrawnKind, &withdrawnBy, &withdrawnAt, &g.DeliveredAt,
		&g.Version, &g.CreatedReceiptID, &g.LastReceiptID, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	g.Ref = FormatRef(PrefixDecision, g.seq)
	if len(options) > 0 {
		if err := json.Unmarshal(options, &g.Options); err != nil {
			return nil, fmt.Errorf("ledger: options of %s: %w", g.Ref, err)
		}
	}
	if g.Options == nil {
		g.Options = []DecisionOption{} // purged at Forget
	}
	g.Status = g.stored
	if g.stored == GateWaiting && !g.ExpiresAt.After(now) {
		g.Status = GateExpired
	}
	if answerOption != nil && answeredBy != nil && answeredAt != nil {
		a := &GateAnswer{Option: *answerOption, AnsweredBy: *answeredBy, AnsweredAt: *answeredAt, Assurance: policy.Assurance(assurance)}
		if i := *answerOption - 1; i >= 0 && i < len(g.Options) {
			a.Label = g.Options[i].Label
		}
		if answerMemory != nil {
			a.Memory.ID = *answerMemory
			if memorySeq != nil {
				a.Memory.Ref = FormatRef(PrefixMemory, *memorySeq)
			}
		}
		g.Answer = a
	}
	if withdrawnBy != nil && withdrawnAt != nil {
		g.Withdrawn = &GateWithdrawal{ByKind: policy.ActorKind(withdrawnKind), By: *withdrawnBy, At: *withdrawnAt}
	}
	return &g, nil
}

// loadGate reads one gate in scope; lock takes the row lock that
// serialises commands on it (two answers at once: one wins, the other
// finds it answered).
func loadGate(ctx context.Context, tx pgx.Tx, scope Scope, id uuid.UUID, lock bool, now time.Time) (*Gate, error) {
	q := gateSelect + ` WHERE g.id = $1 AND g.space_id = ANY($2)`
	if lock {
		q += ` FOR UPDATE OF g`
	}
	g, err := scanGate(tx.QueryRow(ctx, q, id, scope.SpaceIDs()), now)
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load gate: %w", err)
	}
	return g, nil
}

// resolveGateRef turns "G-0012" or a uuid into a gate id within scope.
func resolveGateRef(ctx context.Context, tx pgx.Tx, scope Scope, ref string) (uuid.UUID, error) {
	ref = strings.TrimSpace(ref)
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	p, n, ok := ParseRef(ref)
	if !ok {
		return uuid.Nil, invalid("gate", "use a display ID like G-0012 or a gate id")
	}
	if p != PrefixDecision {
		return uuid.Nil, invalid("gate", "%s is not a decision gate; gate IDs start with G-", ref)
	}
	rows, err := tx.Query(ctx, `SELECT id FROM v2.decision_gates WHERE seq = $1 AND space_id = ANY($2) LIMIT 2`, n, scope.SpaceIDs())
	if err != nil {
		return uuid.Nil, fmt.Errorf("ledger: resolve %s: %w", ref, err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return uuid.Nil, fmt.Errorf("ledger: resolve %s: %w", ref, err)
	}
	switch len(ids) {
	case 0:
		return uuid.Nil, ErrNotFound
	case 1:
		return ids[0], nil
	}
	return uuid.Nil, ErrAmbiguousRef
}

// needsWeb fills each gate's NeedsWeb from its space's rules.
func needsWeb(ctx context.Context, tx pgx.Tx, gates []*Gate) error {
	rules := map[uuid.UUID]bool{}
	for _, g := range gates {
		need, ok := rules[g.SpaceID]
		if !ok {
			sp, err := loadSpace(ctx, tx, g.SpaceID)
			if err != nil {
				return err
			}
			need = sp.Rules.DecisionsNeedPersonOnWeb(sp.Kind)
			rules[g.SpaceID] = need
		}
		g.NeedsWeb = need
	}
	return nil
}

// GetGate returns one gate, by display ID ("G-0012") or uuid. A display
// ID that exists in more than one tenant of the scope is ErrAmbiguousRef;
// narrow the scope to one space.
func (l *Ledger) GetGate(ctx context.Context, scope Scope, ref string) (*Gate, error) {
	var out *Gate
	now := l.now()
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		id, err := resolveGateRef(ctx, tx, scope, ref)
		if err != nil {
			return err
		}
		g, err := loadGate(ctx, tx, scope, id, false, now)
		if err != nil {
			return err
		}
		out = g
		return needsWeb(ctx, tx, []*Gate{g})
	})
	return out, err
}

// GateQuery filters ListGates.
type GateQuery struct {
	SpaceID uuid.UUID
	// Statuses filters by status, as gates read now (expired is a waiting
	// gate past its time); empty means all.
	Statuses []GateStatus
	Cursor   string
	// Limit defaults to DefaultPageSize and is capped at MaxPageSize.
	Limit int
}

// GatePage is one page of gates, newest first.
type GatePage struct {
	Gates      []Gate `json:"gates"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// ListGates pages through one space's gates, newest first.
func (l *Ledger) ListGates(ctx context.Context, scope Scope, q GateQuery) (GatePage, error) {
	if _, ok := scope.Grant(q.SpaceID); !ok {
		return GatePage{}, ErrNotFound
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return GatePage{}, err
	}
	after, err := decodeCursor(q.Cursor, 'g')
	if err != nil {
		return GatePage{}, err
	}
	statuses := make([]string, 0, len(q.Statuses))
	for _, s := range q.Statuses {
		if !s.Valid() {
			return GatePage{}, invalid("status", "use waiting, answered, withdrawn or expired")
		}
		if !slices.Contains(statuses, string(s)) {
			statuses = append(statuses, string(s))
		}
	}
	now := l.now()
	var page GatePage
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, gateSelect+`
			 WHERE g.space_id = $1 AND g.space_id = ANY($2)
			   AND ($3::bigint = 0 OR g.seq < $3)
			   AND (cardinality($4::text[]) = 0
			        OR (g.status <> 'waiting' AND g.status = ANY($4))
			        OR (g.status = 'waiting' AND g.expires_at > $5 AND 'waiting' = ANY($4))
			        OR (g.status = 'waiting' AND g.expires_at <= $5 AND 'expired' = ANY($4)))
			 ORDER BY g.seq DESC
			 LIMIT $6`,
			q.SpaceID, scope.SpaceIDs(), after, statuses, now, limit+1)
		if err != nil {
			return fmt.Errorf("ledger: list gates: %w", err)
		}
		gates, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Gate, error) {
			g, err := scanGate(r, now)
			if err != nil {
				return Gate{}, err
			}
			return *g, nil
		})
		if err != nil {
			return fmt.Errorf("ledger: list gates: %w", err)
		}
		page.Gates = gates
		ptrs := make([]*Gate, len(page.Gates))
		for i := range page.Gates {
			ptrs[i] = &page.Gates[i]
		}
		return needsWeb(ctx, tx, ptrs)
	})
	if err != nil {
		return GatePage{}, err
	}
	if len(page.Gates) > limit {
		page.Gates = page.Gates[:limit]
		page.HasMore = true
		page.NextCursor = encodeCursor('g', page.Gates[limit-1].seq)
	}
	return page, nil
}

// GateNews is what an agent connection should hear about the gates it
// asked, on its next recall.
type GateNews struct {
	// Ended are its gates that were answered, withdrawn by someone else, or
	// expired since it was last told: each is returned once.
	Ended []Gate
	// Waiting are its gates still waiting (when asked for).
	Waiting []Gate
}

// MaxGateNews bounds how many ended gates one recall delivers; the rest
// come with the next.
const MaxGateNews = 20

// TakeGateNews returns how the connection's gates in the scope ended, and
// marks them told, so each answer reaches the agent once (plan §5.12: "the
// answer reaches the agent in its next recall"). It writes delivered_at
// only, which is a read, not a change to the record, so it needs no
// receipt (migration 036). Gates another transaction holds (being answered
// right now) are skipped, and come with the next recall. withWaiting also
// lists the gates still waiting, for the session-start digest.
func (l *Ledger) TakeGateNews(ctx context.Context, scope Scope, connection uuid.UUID, withWaiting bool) (GateNews, error) {
	if l == nil {
		return GateNews{}, ErrDisabled
	}
	now := l.now()
	tx, _, err := l.begin(ctx, scope, pgx.ReadWrite)
	if err != nil {
		return GateNews{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var news GateNews
	rows, err := tx.Query(ctx, `
		UPDATE v2.decision_gates SET delivered_at = $3
		 WHERE id IN (SELECT id FROM v2.decision_gates
		               WHERE asked_by = $1 AND space_id = ANY($2) AND delivered_at IS NULL
		                 AND (status <> 'waiting' OR expires_at <= $3)
		               ORDER BY seq
		               LIMIT $4
		               FOR UPDATE SKIP LOCKED)
		RETURNING id`, connection, scope.SpaceIDs(), now, MaxGateNews)
	if err != nil {
		return GateNews{}, mapDBError(fmt.Errorf("ledger: take gate news: %w", err))
	}
	ended, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return GateNews{}, mapDBError(fmt.Errorf("ledger: take gate news: %w", err))
	}
	collect := func(where string, args ...any) ([]Gate, error) {
		rows, err := tx.Query(ctx, gateSelect+` WHERE `+where+` ORDER BY g.seq`, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Gate, error) {
			g, err := scanGate(r, now)
			if err != nil {
				return Gate{}, err
			}
			return *g, nil
		})
	}
	if len(ended) > 0 {
		if news.Ended, err = collect(`g.id = ANY($1) AND g.space_id = ANY($2)`, ended, scope.SpaceIDs()); err != nil {
			return GateNews{}, mapDBError(fmt.Errorf("ledger: read gate news: %w", err))
		}
	}
	if withWaiting {
		if news.Waiting, err = collect(`g.asked_by = $1 AND g.space_id = ANY($2) AND g.status = 'waiting' AND g.expires_at > $3`,
			connection, scope.SpaceIDs(), now); err != nil {
			return GateNews{}, mapDBError(fmt.Errorf("ledger: read waiting gates: %w", err))
		}
	}
	all := make([]*Gate, 0, len(news.Ended)+len(news.Waiting))
	for i := range news.Ended {
		all = append(all, &news.Ended[i])
	}
	for i := range news.Waiting {
		all = append(all, &news.Waiting[i])
	}
	if err := needsWeb(ctx, tx, all); err != nil {
		return GateNews{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GateNews{}, mapDBError(fmt.Errorf("ledger: take gate news: %w", err))
	}
	return news, nil
}
