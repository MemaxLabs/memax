package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Every query here runs inside a ledger transaction (role memax_v2,
// scope set), so RLS already limits it to the scope. The explicit
// space_id filters are defence in depth (AGENTS.md: they stay).

// spaceRow is a space as the ledger sees it, through v2.spaces.
type spaceRow struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Kind     policy.SpaceKind
	Name     string
	Rules    policy.Rules
}

func (s spaceRow) policy() policy.Space {
	return policy.Space{Name: s.Name, Kind: s.Kind, Rules: s.Rules}
}

func loadSpace(ctx context.Context, tx pgx.Tx, id uuid.UUID) (spaceRow, error) {
	var s spaceRow
	var rules []byte
	err := tx.QueryRow(ctx, `SELECT id, tenant_id, kind, name, rules FROM v2.spaces WHERE id = $1`, id).
		Scan(&s.ID, &s.TenantID, &s.Kind, &s.Name, &rules)
	if errNoRows(err) {
		return spaceRow{}, ErrNotFound
	}
	if err != nil {
		return spaceRow{}, fmt.Errorf("ledger: load space: %w", err)
	}
	// A rules value that doesn't parse fails the command rather than
	// silently falling back to permissive defaults.
	if err := json.Unmarshal(rules, &s.Rules); err != nil {
		return spaceRow{}, fmt.Errorf("ledger: space %s has unreadable rules: %w", id, err)
	}
	return s, nil
}

// resolveRef turns "M-0219" or a uuid into a memory id within scope.
func resolveRef(ctx context.Context, tx pgx.Tx, scope Scope, ref string) (uuid.UUID, error) {
	ref = strings.TrimSpace(ref)
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	p, n, ok := ParseRef(ref)
	if !ok {
		return uuid.Nil, invalid("memory", "use a display ID like M-0219 or a memory id")
	}
	if p != PrefixMemory {
		return uuid.Nil, invalid("memory", "%s is not a memory; memory IDs start with M-", ref)
	}
	rows, err := tx.Query(ctx,
		`SELECT id FROM v2.memories WHERE seq = $1 AND space_id = ANY($2) LIMIT 2`, n, scope.SpaceIDs())
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

const memorySelect = `
	SELECT m.id, m.tenant_id, m.space_id, m.seq, m.section, m.kind, m.lifecycle, m.flags, m.trust,
	       m.current_version, m.stream_version, m.stale_after, m.conditions, m.decision, m.scope,
	       m.valid_from, m.valid_to, m.created_receipt_id, m.last_receipt_id, m.created_at, m.updated_at,
	       COALESCE(v.statement, '')
	  FROM v2.memories m
	  LEFT JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version`

func scanMemory(row pgx.Row) (*Memory, error) {
	var m Memory
	var flags []string
	var conditions, decision, applies []byte
	if err := row.Scan(&m.ID, &m.TenantID, &m.SpaceID, &m.seq, &m.Section, &m.Kind, &m.Lifecycle, &flags, &m.Trust,
		&m.Version, &m.streamVersion, &m.StaleAfter, &conditions, &decision, &applies,
		&m.ValidFrom, &m.ValidTo, &m.CreatedReceiptID, &m.LastReceiptID, &m.CreatedAt, &m.UpdatedAt,
		&m.Statement); err != nil {
		return nil, err
	}
	fs, err := lifecycle.ParseFlags(flags)
	if err != nil {
		return nil, err
	}
	m.Flags = fs
	m.Ref = FormatRef(PrefixMemory, m.seq)
	m.State = lifecycle.DisplayState(m.Lifecycle, m.Flags)
	m.Conditions = json.RawMessage(conditions)
	m.Applies = json.RawMessage(applies)
	if len(decision) > 0 {
		m.Decision = &DecisionFields{}
		if err := json.Unmarshal(decision, m.Decision); err != nil {
			return nil, fmt.Errorf("ledger: decision fields of %s: %w", m.Ref, err)
		}
	}
	return &m, nil
}

// loadMemory reads one memory in scope; lock takes the row lock that
// serialises commands on it.
func loadMemory(ctx context.Context, tx pgx.Tx, scope Scope, id uuid.UUID, lock bool) (*Memory, error) {
	q := memorySelect + ` WHERE m.id = $1 AND m.space_id = ANY($2)`
	if lock {
		q += ` FOR UPDATE OF m`
	}
	m, err := scanMemory(tx.QueryRow(ctx, q, id, scope.SpaceIDs()))
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load memory: %w", err)
	}
	return m, nil
}

func loadSources(ctx context.Context, tx pgx.Tx, memoryID uuid.UUID) ([]Source, error) {
	rows, err := tx.Query(ctx, `
		SELECT s.id, s.kind, s.ref, COALESCE(s.uri, ''), s.locator, s.external, s.trust_class,
		       COALESCE(s.quote, ''), COALESCE(s.content_hash, ''), s.created_at
		  FROM v2.memory_sources ms
		  JOIN v2.sources s ON s.id = ms.source_id
		 WHERE ms.memory_id = $1
		 ORDER BY s.created_at, s.id`, memoryID)
	if err != nil {
		return nil, fmt.Errorf("ledger: load sources: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Source, error) {
		var s Source
		var locator []byte
		err := r.Scan(&s.ID, &s.Kind, &s.Ref, &s.URI, &locator, &s.External, &s.Trust, &s.Quote, &s.ContentHash, &s.CreatedAt)
		s.Locator = json.RawMessage(locator)
		return s, err
	})
}

const receiptSelect = `
	SELECT id, seq, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id,
	       COALESCE(agent, ''), via, COALESCE(assurance, ''), COALESCE(session_ref, ''), source,
	       COALESCE(reason, ''), occurred_at, recorded_at, stream_id, stream_version
	  FROM v2.receipts`

func scanReceipt(r pgx.CollectableRow) (Receipt, error) {
	var rc Receipt
	var source []byte
	if err := r.Scan(&rc.ID, &rc.Seq, &rc.TenantID, &rc.SpaceID, &rc.ObjectKind, &rc.ObjectID, &rc.ObjectRef,
		&rc.Action, &rc.ActorKind, &rc.ActorID, &rc.Agent, &rc.Via, &rc.Assurance, &rc.SessionRef, &source,
		&rc.Reason, &rc.OccurredAt, &rc.RecordedAt, &rc.StreamID, &rc.StreamVersion); err != nil {
		return Receipt{}, err
	}
	if len(source) > 0 {
		rc.Source = &ReceiptSource{}
		if err := json.Unmarshal(source, rc.Source); err != nil {
			return Receipt{}, err
		}
	}
	return rc, nil
}

func loadReceipts(ctx context.Context, tx pgx.Tx, scope Scope, ids []uuid.UUID) ([]Receipt, error) {
	rows, err := tx.Query(ctx, receiptSelect+` WHERE id = ANY($1) AND space_id = ANY($2) ORDER BY seq`, ids, scope.SpaceIDs())
	if err != nil {
		return nil, fmt.Errorf("ledger: load receipts: %w", err)
	}
	return pgx.CollectRows(rows, scanReceipt)
}

func insertReceipt(ctx context.Context, tx pgx.Tx, rc *Receipt) error {
	var source any
	if rc.Source != nil {
		b, err := json.Marshal(rc.Source)
		if err != nil {
			return err
		}
		source = b
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action,
		                         actor_kind, actor_id, agent, via, assurance, session_ref, source, reason,
		                         occurred_at, stream_id, stream_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING seq, recorded_at`,
		rc.ID, rc.TenantID, rc.SpaceID, rc.ObjectKind, rc.ObjectID, rc.ObjectRef, string(rc.Action),
		string(rc.ActorKind), rc.ActorID, nullText(rc.Agent), string(rc.Via), nullText(string(rc.Assurance)),
		nullText(rc.SessionRef), source, nullText(rc.Reason), rc.OccurredAt, rc.StreamID, rc.StreamVersion,
	).Scan(&rc.Seq, &rc.RecordedAt)
	if err != nil {
		return fmt.Errorf("ledger: write receipt: %w", err)
	}
	return nil
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// searchExpr is the FTS vector of a statement, matching V1's chunk
// search config ('simple' over unaccented lower case).
const searchExpr = `to_tsvector('simple', public.immutable_unaccent(lower(%s)))`
