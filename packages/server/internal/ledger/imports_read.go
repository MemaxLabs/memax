package ledger

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Reading imports: the summary, every statement's outcome, the memories
// they became with the judge's verdicts, the disagreements found, and
// which proposals can be kept in bulk (Cleanup and ReviewImport, and
// `memax init` waiting for the judge).

// ImportView is one import, in full.
type ImportView struct {
	Import Import           `json:"import"`
	Items  []ImportItemView `json:"items"`
	// Memories are the memories the items became or matched, once each, in
	// the order the items first name them.
	Memories  []ImportMemory   `json:"memories"`
	Conflicts []ImportConflict `json:"conflicts"`
	Progress  ImportProgress   `json:"progress"`
}

// ImportItemView is what became of one statement.
type ImportItemView struct {
	Position         int            `json:"position"`
	Key              string         `json:"key"`
	Ref              string         `json:"ref"`
	Location         ImportLocation `json:"location"`
	Outcome          ImportOutcome  `json:"outcome"`
	Memory           *MemoryPointer `json:"memory,omitempty"`
	FoldedInto       *int           `json:"folded_into,omitempty"`
	Code             string         `json:"code,omitempty"`
	HiddenCharacters int            `json:"hidden_characters"`
}

// ImportMemory is a memory an import proposed or found, and whether it can
// be kept in bulk.
type ImportMemory struct {
	Memory *Memory `json:"memory"`
	// Outcome is proposed (this import proposed it) or existing.
	Outcome ImportOutcome `json:"outcome"`
	// Items counts the import's statements it stands for (folded repeats
	// included): "Three files agree".
	Items int `json:"items"`
	// Bulk: a person can keep it in bulk with the agreeing ones. Held says
	// why not, when it can't.
	Bulk bool   `json:"bulk"`
	Held string `json:"held,omitempty"`
	// Conflict is the import conflict it is in (its n), if any.
	Conflict int `json:"conflict,omitempty"`
}

// Why a proposal can't be kept in bulk.
const (
	HeldDecided    = "decided"           // not a proposal any more: kept, rejected, merged, …
	HeldConflict   = "conflict"          // it disagrees with another statement, or a decision in force
	HeldQuarantine = "quarantined"       // it cites an outside source: keep it on the web
	HeldChecking   = "checking"          // the judge or the conflict check hasn't finished
	HeldHidden     = "hidden_characters" // its line had hidden characters: read it first
	HeldStale      = "stale"
	HeldFailed     = "unchecked" // the judge couldn't reach a verdict on it
)

// ImportHelds lists every reason.
var ImportHelds = []string{HeldDecided, HeldConflict, HeldQuarantine, HeldChecking, HeldHidden, HeldStale, HeldFailed}

// ImportConflict is a disagreement among an import's proposals.
type ImportConflict struct {
	ID uuid.UUID `json:"id"`
	N  int       `json:"n"`
	// Subject is what they disagree about ("Test command"); Rationale says
	// how; Suggestion, when there is one, says both in one statement
	// (scoped, say). All three are the model's words.
	Subject    string          `json:"subject,omitempty"`
	Rationale  string          `json:"rationale,omitempty"`
	Suggestion string          `json:"suggestion,omitempty"`
	Confidence *float64        `json:"confidence,omitempty"`
	Members    []MemoryPointer `json:"members"`
	// State is open or settled; Choice and Chosen say how it was settled.
	State            string         `json:"state"`
	Choice           ImportChoice   `json:"choice,omitempty"`
	Chosen           *MemoryPointer `json:"chosen,omitempty"`
	CreatedReceiptID uuid.UUID      `json:"created_receipt_id"`
	LastReceiptID    uuid.UUID      `json:"last_receipt_id"`
	CreatedAt        time.Time      `json:"created_at"`
	SettledAt        *time.Time     `json:"settled_at,omitempty"`
}

// ImportProgress is how far the judge got with an import's proposals.
type ImportProgress struct {
	// Proposals counts the memories this import proposed.
	Proposals int `json:"proposals"`
	// Working, Judged and Failed count them by the judge's state; a
	// proposal decided meanwhile counts as judged.
	Working int `json:"working"`
	Judged  int `json:"judged"`
	Failed  int `json:"failed"`
	// Ready: nothing is being judged and the conflict check is done.
	Ready bool `json:"ready"`
}

const importColumns = `id, tenant_id, space_id, actor_kind, actor_id, COALESCE(agent, ''), COALESCE(client, ''),
	files, skipped, items_total, uploaded_at, checked_at, COALESCE(check_state, ''), COALESCE(check_tier, ''),
	COALESCE(check_model, ''), created_at, origin`

func scanImport(row pgx.Row) (*Import, error) {
	var imp Import
	var files, skipped []byte
	var state string
	if err := row.Scan(&imp.ID, &imp.TenantID, &imp.SpaceID, &imp.ActorKind, &imp.ActorID, &imp.Agent, &imp.Client,
		&files, &skipped, &imp.Counts.Items, &imp.UploadedAt, &imp.Check.CheckedAt, &state, &imp.Check.Tier,
		&imp.Check.Model, &imp.CreatedAt, &imp.Origin); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(files, &imp.Files); err != nil {
		return nil, fmt.Errorf("ledger: import files: %w", err)
	}
	if err := json.Unmarshal(skipped, &imp.Skipped); err != nil {
		return nil, fmt.Errorf("ledger: import skipped: %w", err)
	}
	imp.Files, imp.Skipped = nonNilSlice(imp.Files), nonNilSlice(imp.Skipped)
	imp.Check.State = state
	if state == "" {
		imp.Check.State = CheckPending
	}
	return &imp, nil
}

// countImport fills an import's counts by outcome and its conflicts.
func countImport(ctx context.Context, tx pgx.Tx, imp *Import) error {
	rows, err := tx.Query(ctx, `
		SELECT outcome, count(*) FROM v2.import_items WHERE import_id = $1 AND space_id = $2 GROUP BY outcome`,
		imp.ID, imp.SpaceID)
	if err != nil {
		return fmt.Errorf("ledger: count import: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var o ImportOutcome
		var n int
		if err := rows.Scan(&o, &n); err != nil {
			return fmt.Errorf("ledger: count import: %w", err)
		}
		switch o {
		case ImportProposed:
			imp.Counts.Proposed = n
		case ImportFolded:
			imp.Counts.Folded = n
		case ImportExisting:
			imp.Counts.Existing = n
		case ImportRefused:
			imp.Counts.Refused = n
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: count import: %w", err)
	}
	return tx.QueryRow(ctx, `SELECT count(*) FROM v2.import_conflicts WHERE import_id = $1 AND space_id = $2`,
		imp.ID, imp.SpaceID).Scan(&imp.Counts.Conflicts)
}

// importSummary reads one import's summary and counts.
func (l *Ledger) importSummary(ctx context.Context, scope Scope, spaceID, id uuid.UUID) (*Import, error) {
	var out *Import
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		imp, err := scanImport(tx.QueryRow(ctx, `SELECT `+importColumns+` FROM v2.imports WHERE id = $1 AND space_id = $2`, id, spaceID))
		if errNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("ledger: read import: %w", err)
		}
		if err := countImport(ctx, tx, imp); err != nil {
			return err
		}
		out = imp
		return nil
	})
	return out, err
}

// ImportPage is one page of a space's imports, newest first.
type ImportPage struct {
	Items      []Import `json:"items"`
	HasMore    bool     `json:"has_more"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// ListImports lists a space's imports, newest first.
func (l *Ledger) ListImports(ctx context.Context, scope Scope, spaceID uuid.UUID, cursor string, limit int) (ImportPage, error) {
	if l == nil {
		return ImportPage{}, ErrDisabled
	}
	if _, ok := scope.Grant(spaceID); !ok {
		return ImportPage{}, ErrNotFound
	}
	n, err := pageLimit(limit)
	if err != nil {
		return ImportPage{}, err
	}
	at, after, err := decodeImportCursor(cursor)
	if err != nil {
		return ImportPage{}, err
	}
	var page ImportPage
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+importColumns+` FROM v2.imports
			 WHERE space_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
			 ORDER BY created_at DESC, id DESC LIMIT $4`, spaceID, at, after, n+1)
		if err != nil {
			return fmt.Errorf("ledger: list imports: %w", err)
		}
		imps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Import, error) { return scanImport(r) })
		if err != nil {
			return fmt.Errorf("ledger: list imports: %w", err)
		}
		if len(imps) > n {
			imps, page.HasMore = imps[:n], true
		}
		for _, imp := range imps {
			if err := countImport(ctx, tx, imp); err != nil {
				return err
			}
			page.Items = append(page.Items, *imp)
		}
		if page.HasMore {
			last := imps[len(imps)-1]
			page.NextCursor = encodeImportCursor(last.CreatedAt, last.ID)
		}
		return nil
	})
	page.Items = nonNilSlice(page.Items)
	return page, err
}

func encodeImportCursor(at time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte("i:" + strconv.FormatInt(at.UnixMicro(), 10) + ":" + id.String()))
}

func decodeImportCursor(c string) (*time.Time, uuid.UUID, error) {
	if c == "" {
		return nil, uuid.Nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	parts := strings.SplitN(string(raw), ":", 3)
	if err != nil || len(parts) != 3 || parts[0] != "i" {
		return nil, uuid.Nil, invalid("cursor", "isn't one this list issued; start again without it")
	}
	micros, err1 := strconv.ParseInt(parts[1], 10, 64)
	id, err2 := uuid.Parse(parts[2])
	if err1 != nil || err2 != nil {
		return nil, uuid.Nil, invalid("cursor", "isn't one this list issued; start again without it")
	}
	at := time.UnixMicro(micros).UTC()
	return &at, id, nil
}

// GetImport reads one import of a space in full.
func (l *Ledger) GetImport(ctx context.Context, scope Scope, spaceID, id uuid.UUID) (*ImportView, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if _, ok := scope.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	var out *ImportView
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		imp, err := scanImport(tx.QueryRow(ctx, `SELECT `+importColumns+` FROM v2.imports WHERE id = $1 AND space_id = $2`, id, spaceID))
		if errNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("ledger: read import: %w", err)
		}
		if err := countImport(ctx, tx, imp); err != nil {
			return err
		}
		v := &ImportView{Import: *imp}
		if v.Items, err = importItems(ctx, tx, imp); err != nil {
			return err
		}
		if v.Conflicts, err = importConflicts(ctx, tx, imp.ID, imp.SpaceID); err != nil {
			return err
		}
		if v.Memories, err = importMemories(ctx, tx, scope, v); err != nil {
			return err
		}
		v.Progress = importProgress(v)
		out = v
		return nil
	})
	return out, err
}

func importItems(ctx context.Context, tx pgx.Tx, imp *Import) ([]ImportItemView, error) {
	rows, err := tx.Query(ctx, `
		SELECT i.position, i.item_key, i.ref, i.location, i.outcome, i.memory_id, m.seq, i.folded_into,
		       COALESCE(i.code, ''), i.hidden_characters
		  FROM v2.import_items i
		  LEFT JOIN v2.memories m ON m.id = i.memory_id
		 WHERE i.import_id = $1 AND i.space_id = $2
		 ORDER BY i.position`, imp.ID, imp.SpaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: import items: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ImportItemView, error) {
		var it ImportItemView
		var memID *uuid.UUID
		var seq *int64
		err := r.Scan(&it.Position, &it.Key, &it.Ref, &it.Location, &it.Outcome, &memID, &seq, &it.FoldedInto,
			&it.Code, &it.HiddenCharacters)
		if memID != nil && seq != nil {
			it.Memory = &MemoryPointer{ID: *memID, Ref: FormatRef(PrefixMemory, *seq)}
		}
		return it, err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: import items: %w", err)
	}
	return nonNilSlice(items), nil
}

func importConflicts(ctx context.Context, tx pgx.Tx, importID, spaceID uuid.UUID) ([]ImportConflict, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id, c.n, COALESCE(c.subject, ''), COALESCE(c.rationale, ''), COALESCE(c.suggestion, ''), c.confidence,
		       c.members, (SELECT array_agg(m.seq ORDER BY array_position(c.members, m.id))
		                     FROM v2.memories m WHERE m.id = ANY (c.members)),
		       c.state, COALESCE(c.choice, ''), c.chosen_memory_id, (SELECT seq FROM v2.memories WHERE id = c.chosen_memory_id),
		       c.created_receipt_id, c.last_receipt_id, c.created_at, c.settled_at
		  FROM v2.import_conflicts c
		 WHERE c.import_id = $1 AND c.space_id = $2
		 ORDER BY c.n`, importID, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: import conflicts: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ImportConflict, error) {
		var c ImportConflict
		var conf *float32
		var members []uuid.UUID
		var seqs []int64
		var chosen *uuid.UUID
		var chosenSeq *int64
		err := r.Scan(&c.ID, &c.N, &c.Subject, &c.Rationale, &c.Suggestion, &conf, &members, &seqs, &c.State, &c.Choice,
			&chosen, &chosenSeq, &c.CreatedReceiptID, &c.LastReceiptID, &c.CreatedAt, &c.SettledAt)
		if err != nil {
			return c, err
		}
		if conf != nil {
			f := float64(*conf)
			c.Confidence = &f
		}
		for i, id := range members {
			if i < len(seqs) {
				c.Members = append(c.Members, MemoryPointer{ID: id, Ref: FormatRef(PrefixMemory, seqs[i])})
			}
		}
		if chosen != nil && chosenSeq != nil {
			c.Chosen = &MemoryPointer{ID: *chosen, Ref: FormatRef(PrefixMemory, *chosenSeq)}
		}
		return c, nil
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: import conflicts: %w", err)
	}
	return nonNilSlice(out), nil
}

// importMemories loads the memories the items name, with their sources,
// links and verdicts, and says which can be kept in bulk.
func importMemories(ctx context.Context, tx pgx.Tx, scope Scope, v *ImportView) ([]ImportMemory, error) {
	var ids []uuid.UUID
	outcome := map[uuid.UUID]ImportOutcome{}
	items := map[uuid.UUID]int{}
	hidden := map[uuid.UUID]int{}
	for _, it := range v.Items {
		if it.Memory == nil {
			continue
		}
		id := it.Memory.ID
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
		items[id]++
		hidden[id] += it.HiddenCharacters
		if it.Outcome == ImportProposed || it.Outcome == ImportFolded {
			outcome[id] = ImportProposed
		} else if _, ok := outcome[id]; !ok {
			outcome[id] = ImportExisting
		}
	}
	if len(ids) == 0 {
		return []ImportMemory{}, nil
	}
	rows, err := tx.Query(ctx, memorySelect+` WHERE m.id = ANY ($1) AND m.space_id = ANY ($2)`, ids, scope.SpaceIDs())
	if err != nil {
		return nil, fmt.Errorf("ledger: import memories: %w", err)
	}
	loaded, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
	if err != nil {
		return nil, fmt.Errorf("ledger: import memories: %w", err)
	}
	if err := attachDetails(ctx, tx, loaded); err != nil {
		return nil, err
	}
	if err := attachSources(ctx, tx, loaded); err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]*Memory{}
	for _, m := range loaded {
		byID[m.ID] = m
	}
	inConflict := map[uuid.UUID]int{}
	for _, c := range v.Conflicts {
		if c.State != "open" {
			continue
		}
		for _, m := range c.Members {
			inConflict[m.ID] = c.N
		}
	}
	checking := v.Import.Check.State == CheckPending
	out := make([]ImportMemory, 0, len(ids))
	for _, id := range ids {
		m := byID[id]
		if m == nil {
			continue
		}
		im := ImportMemory{Memory: m, Outcome: outcome[id], Items: items[id], Conflict: inConflict[id]}
		switch {
		case m.Lifecycle != lifecycle.Proposed:
			im.Held = HeldDecided
		case m.Flags.Has(lifecycle.Conflict) || im.Conflict > 0:
			im.Held = HeldConflict
		case m.Trust.External():
			im.Held = HeldQuarantine
		case checking || (m.Judge != nil && m.Judge.State == JudgeWorking):
			im.Held = HeldChecking
		case m.Judge != nil && m.Judge.State == JudgeFailed:
			im.Held = HeldFailed
		case m.Flags.Has(lifecycle.Stale):
			im.Held = HeldStale
		case hidden[id] > 0:
			im.Held = HeldHidden
		}
		im.Bulk = im.Held == ""
		out = append(out, im)
	}
	return out, nil
}

func importProgress(v *ImportView) ImportProgress {
	var p ImportProgress
	for _, im := range v.Memories {
		if im.Outcome != ImportProposed {
			continue
		}
		p.Proposals++
		switch {
		case im.Memory.Lifecycle == lifecycle.Proposed && im.Memory.Judge != nil && im.Memory.Judge.State == JudgeWorking:
			p.Working++
		case im.Memory.Judge != nil && im.Memory.Judge.State == JudgeFailed:
			p.Failed++
		default:
			p.Judged++
		}
	}
	p.Ready = p.Working == 0 && v.Import.Check.State != CheckPending
	return p
}

// attachSources fills Sources on each memory in one query.
func attachSources(ctx context.Context, tx pgx.Tx, ms []*Memory) error {
	if len(ms) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(ms))
	byID := map[uuid.UUID]*Memory{}
	for i, m := range ms {
		ids[i] = m.ID
		byID[m.ID] = m
	}
	rows, err := tx.Query(ctx, `
		SELECT ms.memory_id, s.id, s.kind, s.ref, COALESCE(s.uri, ''), s.locator, s.external, s.trust_class,
		       COALESCE(s.quote, ''), COALESCE(s.content_hash, ''), s.created_at
		  FROM v2.memory_sources ms
		  JOIN v2.sources s ON s.id = ms.source_id
		 WHERE ms.memory_id = ANY ($1)
		 ORDER BY s.created_at, s.id`, ids)
	if err != nil {
		return fmt.Errorf("ledger: load sources: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var mid uuid.UUID
		var s Source
		var locator []byte
		if err := rows.Scan(&mid, &s.ID, &s.Kind, &s.Ref, &s.URI, &locator, &s.External, &s.Trust, &s.Quote,
			&s.ContentHash, &s.CreatedAt); err != nil {
			return fmt.Errorf("ledger: load sources: %w", err)
		}
		s.Locator = json.RawMessage(locator)
		if m := byID[mid]; m != nil {
			m.Sources = append(m.Sources, s)
		}
	}
	return rows.Err()
}

// ImportCandidate is one of an import's proposals as the conflict check
// sees it.
type ImportCandidate struct {
	ID        uuid.UUID
	Ref       string
	Statement string
	Kind      Kind
	Section   Section
	// Sources are where it came from ("CLAUDE.md:12"), and Paths where it
	// applies (a scoped rule's globs).
	Sources []string
	Paths   []string
	Trust   policy.Trust
}

// ImportCheckInput is what the conflict check compares.
type ImportCheckInput struct {
	Import  *Import
	Scope   Scope
	Pending []ImportCandidate
}

// ImportCheckSnapshot reads an import's open proposals for its conflict
// check, as Memax acting on the space. Done reports a check already
// recorded.
func (l *Ledger) ImportCheckSnapshot(ctx context.Context, args JudgeImportArgs) (*ImportCheckInput, bool, error) {
	if l == nil {
		return nil, false, ErrDisabled
	}
	scope, err := l.SpaceScope(ctx, args.SpaceID)
	if err != nil {
		return nil, false, err
	}
	in := &ImportCheckInput{Scope: scope}
	done := false
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		imp, err := scanImport(tx.QueryRow(ctx, `SELECT `+importColumns+` FROM v2.imports WHERE id = $1 AND space_id = $2`,
			args.ImportID, args.SpaceID))
		if errNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("ledger: read import: %w", err)
		}
		in.Import, done = imp, imp.Check.CheckedAt != nil
		rows, err := tx.Query(ctx, memorySelect+`
			 WHERE m.space_id = $1 AND m.lifecycle = 'proposed'
			   AND m.id IN (SELECT memory_id FROM v2.import_items WHERE import_id = $2 AND space_id = $1 AND outcome = 'proposed')
			 ORDER BY m.seq`, args.SpaceID, args.ImportID)
		if err != nil {
			return fmt.Errorf("ledger: import snapshot: %w", err)
		}
		ms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
		if err != nil {
			return fmt.Errorf("ledger: import snapshot: %w", err)
		}
		if err := attachSources(ctx, tx, ms); err != nil {
			return err
		}
		for _, m := range ms {
			c := ImportCandidate{ID: m.ID, Ref: m.Ref, Statement: m.Statement, Kind: m.Kind, Section: m.Section,
				Paths: scopePaths(m.Applies), Trust: m.Trust}
			for _, s := range m.Sources {
				c.Sources = append(c.Sources, s.Ref)
			}
			in.Pending = append(in.Pending, c)
		}
		return nil
	})
	return in, done, err
}
