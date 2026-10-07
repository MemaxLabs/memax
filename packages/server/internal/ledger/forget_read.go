package ledger

import (
	"context"
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

// lifecycleNothingToDecline: Keep it instead, on a memory nobody asked to
// forget.
var lifecycleNothingToDecline = lifecycle.TransitionError{
	Message: "Nobody asked to forget it, so there is nothing to keep it instead of.",
}

// Reading tombstones (the Tombstone page) and forget requests.

// GetTombstone reads a forgotten memory's tombstone, by its display ID or
// uuid, with its steps and the copies Memax can't reach.
func (l *Ledger) GetTombstone(ctx context.Context, scope Scope, ref string) (*Tombstone, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	var out *Tombstone
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		id, err := resolveRef(ctx, tx, scope, ref)
		if err != nil {
			return err
		}
		out, err = loadTombstone(ctx, tx, scope, id, l.honesty)
		return err
	})
	return out, err
}

// ForgetPreview is what a Forget of a memory would do, read before anyone
// confirms it: what goes with it, the files it would rewrite, how many
// agents would be told, and whether the actor may.
type ForgetPreview struct {
	Ref     string            `json:"ref"`
	Version int               `json:"version"`
	Carries []Carried         `json:"carries"`
	Files   []TombstoneTarget `json:"files"`
	// Agents is every agent connection that would be told: those that read
	// it (Readers of them) and those connected to the space.
	Agents  int              `json:"agents"`
	Readers int              `json:"readers"`
	Allowed bool             `json:"allowed"`
	Policy  *policy.Decision `json:"policy,omitempty"`
}

// PreviewForget reads what a Forget of ref would do, as actor. A memory
// that can't be forgotten (it is already) is an *lifecycle.TransitionError.
func (l *Ledger) PreviewForget(ctx context.Context, scope Scope, actor Actor, via policy.Via, ref string) (*ForgetPreview, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	var out *ForgetPreview
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		id, err := resolveRef(ctx, tx, scope, ref)
		if err != nil {
			return err
		}
		mem, err := loadMemory(ctx, tx, scope, id, false)
		if err != nil {
			return err
		}
		if _, err := transition(mem, lifecycle.VerbForget); err != nil {
			return err
		}
		sp, err := loadSpace(ctx, tx, mem.SpaceID)
		if err != nil {
			return err
		}
		carried, err := carriedBy(ctx, tx, mem.SpaceID, []*Memory{mem})
		if err != nil {
			return err
		}
		p := &ForgetPreview{Ref: mem.Ref, Version: mem.Version, Carries: nonNilSlice(carried),
			Files: []TombstoneTarget{}, Allowed: true}
		grant, _ := scope.Grant(mem.SpaceID)
		pa := toPolicyActor(actor, via, grant)
		decide := func(m Carried, decision bool) {
			if !p.Allowed {
				return
			}
			d := policy.Decide(pa, policy.ActionForget, policy.Object{Ref: m.Ref, Lifecycle: m.Lifecycle, Decision: decision}, sp.policy())
			if d.Effect == policy.EffectRefuse {
				if m.Ref != mem.Ref {
					d.Message = fmt.Sprintf("%s goes with %s, and you can't forget it: %s", m.Ref, mem.Ref, d.Message)
				}
				p.Allowed, p.Policy = false, &d
			}
		}
		decide(Carried{Ref: mem.Ref, Lifecycle: mem.Lifecycle}, mem.Kind == KindDecision)
		refs := []string{mem.Ref}
		ids := []uuid.UUID{mem.ID}
		for _, c := range carried {
			decide(c, c.Kind == KindDecision)
			refs = append(refs, c.Ref)
			ids = append(ids, c.ID)
		}
		files, err := filesHolding(ctx, tx, mem.SpaceID, refs)
		if err != nil {
			return err
		}
		for _, fid := range files {
			t, err := loadTarget(ctx, tx, scope, fid, false)
			if err != nil {
				return err
			}
			p.Files = append(p.Files, TombstoneTarget{ID: t.ID, Kind: t.Kind, Label: t.Label, Delivery: t.Delivery})
		}
		if err := tx.QueryRow(ctx, `
			WITH readers AS (
			    SELECT r.reader_key AS connection_id, true AS read_it
			      FROM v2.read_rollups r
			     WHERE r.space_id = $1 AND r.reader_kind = 'agent'
			       AND (r.subject_id = ANY ($2) OR r.subject_id IN (
			           SELECT c.id FROM v2.compile_runs c WHERE c.space_id = $1 AND c.refs && $3))
			    UNION ALL
			    SELECT s.connection_id, false FROM v2.agent_connection_spaces s WHERE s.space_id = $1
			), each AS (SELECT connection_id, bool_or(read_it) AS read_it FROM readers GROUP BY connection_id)
			SELECT count(*), count(*) FILTER (WHERE read_it) FROM each`,
			mem.SpaceID, ids, refs).Scan(&p.Agents, &p.Readers); err != nil {
			return fmt.Errorf("ledger: forget preview: %w", err)
		}
		out = p
		return nil
	})
	return out, err
}

// TombstoneQuery pages through a space's tombstones, newest first.
type TombstoneQuery struct {
	SpaceID uuid.UUID
	Cursor  string
	Limit   int
}

// TombstonePage is one page of tombstones. Each carries its status, not
// its steps; GetTombstone reads one whole.
type TombstonePage struct {
	Tombstones []Tombstone `json:"tombstones"`
	NextCursor string      `json:"next_cursor,omitempty"`
	HasMore    bool        `json:"has_more"`
}

// ListTombstones lists a space's tombstones, newest first.
func (l *Ledger) ListTombstones(ctx context.Context, scope Scope, q TombstoneQuery) (TombstonePage, error) {
	if l == nil {
		return TombstonePage{}, ErrDisabled
	}
	if _, ok := scope.Grant(q.SpaceID); !ok {
		return TombstonePage{}, ErrNotFound
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return TombstonePage{}, err
	}
	var afterAt time.Time
	var afterID uuid.UUID
	if q.Cursor != "" {
		at, id, ok := decodeTombstoneCursor(q.Cursor)
		if !ok {
			return TombstonePage{}, invalid("cursor", "isn't a cursor this list gave out; start again without it")
		}
		afterAt, afterID = at, id
	}
	var page TombstonePage
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, tombstoneSelect+`
			 WHERE t.space_id = $1 AND t.space_id = ANY ($2)
			   AND ($3::uuid = '00000000-0000-0000-0000-000000000000' OR (t.forgotten_at, t.id) < ($4, $3))
			 ORDER BY t.forgotten_at DESC, t.id DESC
			 LIMIT $5`, q.SpaceID, scope.SpaceIDs(), afterID, afterAt, limit+1)
		if err != nil {
			return fmt.Errorf("ledger: list tombstones: %w", err)
		}
		ts, err := pgx.CollectRows(rows, scanTombstone)
		if err != nil {
			return fmt.Errorf("ledger: list tombstones: %w", err)
		}
		if len(ts) > limit {
			ts, page.HasMore = ts[:limit], true
			last := ts[len(ts)-1]
			page.NextCursor = encodeTombstoneCursor(last.ForgottenAt, last.ID)
		}
		if err := attachWith(ctx, tx, ts); err != nil {
			return err
		}
		page.Tombstones = ts
		return nil
	})
	if page.Tombstones == nil {
		page.Tombstones = []Tombstone{}
	}
	return page, err
}

func encodeTombstoneCursor(at time.Time, id uuid.UUID) string {
	// Postgres keeps microseconds, so this is exact, and "t" + up to 17
	// digits + "_" + 36 fits the spec's 64.
	return "t" + strconv.FormatInt(at.UnixMicro(), 10) + "_" + id.String()
}

func decodeTombstoneCursor(s string) (time.Time, uuid.UUID, bool) {
	if !strings.HasPrefix(s, "t") {
		return time.Time{}, uuid.Nil, false
	}
	at, idText, ok := strings.Cut(s[1:], "_")
	if !ok {
		return time.Time{}, uuid.Nil, false
	}
	micros, err := strconv.ParseInt(at, 10, 64)
	if err != nil || micros < 0 {
		return time.Time{}, uuid.Nil, false
	}
	t := time.UnixMicro(micros).UTC()
	id, err := uuid.Parse(idText)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	return t, id, true
}

const tombstoneSelect = `
	SELECT t.id, t.op_id, t.object_kind, t.object_id, t.object_ref, t.space_id, t.tenant_id, COALESCE(t.carried, ''),
	       COALESCE(p.object_ref, ''), COALESCE(t.note, ''), t.by_kind, t.by_id, t.requested_by, t.via, t.receipt_id,
	       t.forgotten_at, t.kept_at, t.reads_before, t.gone, t.status, t.completed_at, t.reapplied_at,
	       (SELECT count(*) FROM v2.agent_notices n WHERE n.op_id = t.op_id)
	  FROM v2.tombstones t
	  JOIN v2.tombstones p ON p.id = t.op_id`

func scanTombstone(row pgx.CollectableRow) (Tombstone, error) {
	var t Tombstone
	var by *uuid.UUID
	var request *uuid.UUID
	var gone []byte
	var primary string
	if err := row.Scan(&t.ID, &t.OpID, &t.Kind, &t.ObjectID, &t.Ref, &t.SpaceID, &t.TenantID, &t.Carried,
		&primary, &t.Note, &t.By.Kind, &by, &request, &t.Via, &t.ReceiptID,
		&t.ForgottenAt, &t.KeptAt, &t.ReadsBefore, &gone, &t.Status, &t.CompletedAt, &t.ReappliedAt, &t.Agents); err != nil {
		return Tombstone{}, err
	}
	t.By.ID = by
	if request != nil {
		t.RequestedBy = &TombstoneAgent{ConnectionID: *request}
	}
	if t.Carried != "" {
		t.Primary = primary
	}
	if err := json.Unmarshal(gone, &t.Gone); err != nil {
		return Tombstone{}, fmt.Errorf("ledger: tombstone %s: %w", t.Ref, err)
	}
	if t.CompletedAt != nil {
		d := t.CompletedAt.Sub(t.ForgottenAt).Milliseconds()
		t.DurationMS = &d
	}
	t.With, t.Steps, t.Unreachable = []string{}, []TombstoneStep{}, []UnreachableCopy{}
	return t, nil
}

// attachWith fills each tombstone's With: the other refs of its op.
func attachWith(ctx context.Context, tx pgx.Tx, ts []Tombstone) error {
	if len(ts) == 0 {
		return nil
	}
	ops := make([]uuid.UUID, 0, len(ts))
	for _, t := range ts {
		ops = append(ops, t.OpID)
	}
	rows, err := tx.Query(ctx, `SELECT op_id, id, object_ref FROM v2.tombstones WHERE op_id = ANY ($1) ORDER BY forgotten_at, object_ref`, ops)
	if err != nil {
		return fmt.Errorf("ledger: tombstones: %w", err)
	}
	defer rows.Close()
	type member struct {
		id  uuid.UUID
		ref string
	}
	byOp := map[uuid.UUID][]member{}
	for rows.Next() {
		var op, id uuid.UUID
		var ref string
		if err := rows.Scan(&op, &id, &ref); err != nil {
			return fmt.Errorf("ledger: tombstones: %w", err)
		}
		byOp[op] = append(byOp[op], member{id, ref})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: tombstones: %w", err)
	}
	for i := range ts {
		for _, m := range byOp[ts[i].OpID] {
			if m.id != ts[i].ID && m.ref != SpaceObjectRef {
				ts[i].With = append(ts[i].With, m.ref)
			}
		}
		if len(ts[i].With) > 1000 {
			ts[i].With = ts[i].With[:1000]
		}
	}
	return nil
}

// loadTombstone reads an object's tombstone with its steps and the copies
// Memax can't reach. A carried tombstone shows its op's steps (the files
// and agents are the op's).
func loadTombstone(ctx context.Context, tx pgx.Tx, scope Scope, objectID uuid.UUID, h ForgetHonesty) (*Tombstone, error) {
	rows, err := tx.Query(ctx, tombstoneSelect+`
		 WHERE t.object_id = $1 AND t.space_id = ANY ($2) ORDER BY t.forgotten_at DESC, t.id DESC LIMIT 1`, objectID, scope.SpaceIDs())
	if err != nil {
		return nil, fmt.Errorf("ledger: tombstone: %w", err)
	}
	ts, err := pgx.CollectRows(rows, scanTombstone)
	if err != nil {
		return nil, fmt.Errorf("ledger: tombstone: %w", err)
	}
	if len(ts) == 0 {
		return nil, ErrNotFound
	}
	t := &ts[0]
	if err := attachWith(ctx, tx, ts); err != nil {
		return nil, err
	}
	if err := fillTombstone(ctx, tx, t, h); err != nil {
		return nil, err
	}
	return t, nil
}

// fillTombstone builds the steps and the copies Memax can't reach, from
// the op's propagation rows, its targets as they stand now, and its
// notices.
func fillTombstone(ctx context.Context, tx pgx.Tx, t *Tombstone, h ForgetHonesty) error {
	at := t.ForgottenAt
	asked := TombstoneStep{Kind: StepAsked, Status: StepDone, At: &at}
	if t.RequestedBy != nil {
		agent, err := connectionNames(ctx, tx, []uuid.UUID{t.RequestedBy.ConnectionID})
		if err != nil {
			return err
		}
		if a, ok := agent[t.RequestedBy.ConnectionID]; ok {
			t.RequestedBy = &a
		}
		asked.Agent = t.RequestedBy
	}
	t.Steps = append(t.Steps, asked, TombstoneStep{Kind: StepRemoved, Status: StepDone, At: &at})

	// The op's destinations.
	rows, err := tx.Query(ctx, `
		SELECT p.destination_kind, p.destination_id, COALESCE(p.label, ''), p.status, p.detail, p.done_at,
		       t.kind, t.delivery, t.sync_state, t.delivered_at, dc.seq, COALESCE(t.path, '')
		  FROM v2.propagations p
		  LEFT JOIN v2.targets t ON t.id = p.destination_id
		  LEFT JOIN v2.compile_runs dc ON dc.id = t.delivered_compile_id
		 WHERE p.op_id = $1
		 ORDER BY CASE p.destination_kind WHEN 'target' THEN 0 WHEN 'artifacts' THEN 1 WHEN 'caches' THEN 2 ELSE 3 END,
		          p.created_at, p.id`, t.OpID)
	if err != nil {
		return fmt.Errorf("ledger: tombstone steps: %w", err)
	}
	type dest struct {
		kind, label, status string
		id                  *uuid.UUID
		detail              []byte
		doneAt              *time.Time
		tKind               *TargetKind
		delivery            *Delivery
		state               *SyncState
		deliveredAt         *time.Time
		deliveredSeq        *int64
		path                string
	}
	var dests []dest
	for rows.Next() {
		var d dest
		if err := rows.Scan(&d.kind, &d.id, &d.label, &d.status, &d.detail, &d.doneAt, &d.tKind, &d.delivery, &d.state,
			&d.deliveredAt, &d.deliveredSeq, &d.path); err != nil {
			rows.Close()
			return fmt.Errorf("ledger: tombstone steps: %w", err)
		}
		dests = append(dests, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: tombstone steps: %w", err)
	}
	var committed, handEdits, copies []string
	for _, d := range dests {
		var detail struct {
			Generation int64      `json:"generation"`
			Kind       TargetKind `json:"kind"`
			Delivery   Delivery   `json:"delivery"`
			Compile    string     `json:"compile"`
			Count      *int       `json:"count"`
		}
		_ = json.Unmarshal(d.detail, &detail)
		s := TombstoneStep{Kind: d.kind, At: d.doneAt, Compile: detail.Compile, Count: detail.Count}
		switch d.status {
		case "done":
			s.Status = StepDone
		case "held":
			s.Status, s.Reason = StepHeld, StepReasonHandEdit
		case "stopped":
			s.Status, s.Reason = StepUnreachable, StepReasonStopped
		case "failed":
			s.Status = StepFailed
		default:
			s.Status = StepWaiting
			if d.kind == "target" {
				s.Reason = StepReasonCompiling
			}
		}
		if d.kind != "target" {
			t.Steps = append(t.Steps, s)
			continue
		}
		kind, delivery := detail.Kind, detail.Delivery
		if d.tKind != nil {
			kind, delivery = *d.tKind, *d.delivery
		}
		s.Target = &TombstoneTarget{Kind: kind, Label: d.label, Delivery: delivery}
		if d.id != nil {
			s.Target.ID = *d.id
		}
		// A compiled file is rewritten when the daemon (or a pull request)
		// delivers a run at least as new as the one Forget's compile made.
		if s.Status == StepDone && delivery.writesFiles() {
			_, runSeq, _ := ParseRef(detail.Compile)
			switch {
			case d.state != nil && *d.state == SyncDrifted:
				s.Status, s.Reason, s.At = StepHeld, StepReasonHandEdit, nil
			case d.deliveredSeq != nil && runSeq > 0 && *d.deliveredSeq >= runSeq:
				s.At = d.deliveredAt
			case detail.Compile == "":
				// Nothing new to deliver: the file never held a newer copy.
			default:
				s.Status, s.Reason, s.At = StepWaiting, StepReasonDelivery, nil
			}
		}
		if s.Status == StepHeld {
			handEdits = append(handEdits, d.label)
		}
		if delivery.writesFiles() {
			committed = append(committed, d.label)
		}
		if delivery == DeliveryCopy {
			copies = append(copies, d.label)
			if s.Status == StepDone {
				s.Reason = StepReasonCopy
			}
		}
		t.Steps = append(t.Steps, s)
	}

	// The agents.
	rows, err = tx.Query(ctx, `
		SELECT n.connection_id, n.read_it, n.delivered_at
		  FROM v2.agent_notices n
		 WHERE n.op_id = $1
		 ORDER BY n.delivered_at NULLS LAST, n.created_at, n.connection_id`, t.OpID)
	if err != nil {
		return fmt.Errorf("ledger: tombstone notices: %w", err)
	}
	type notice struct {
		id          uuid.UUID
		readIt      bool
		deliveredAt *time.Time
	}
	var notices []notice
	for rows.Next() {
		var n notice
		if err := rows.Scan(&n.id, &n.readIt, &n.deliveredAt); err != nil {
			rows.Close()
			return fmt.Errorf("ledger: tombstone notices: %w", err)
		}
		notices = append(notices, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: tombstone notices: %w", err)
	}
	ids := make([]uuid.UUID, len(notices))
	for i, n := range notices {
		ids[i] = n.id
	}
	names, err := connectionNames(ctx, tx, ids)
	if err != nil {
		return err
	}
	var readers []string
	for _, n := range notices {
		a, ok := names[n.id]
		if !ok {
			a = TombstoneAgent{ConnectionID: n.id}
		}
		s := TombstoneStep{Kind: StepAgent, Agent: &a, ReadIt: n.readIt, At: n.deliveredAt}
		switch {
		case n.deliveredAt != nil:
			s.Status = StepDone
		case a.State == string(ConnectionDisconnected):
			s.Status, s.Reason = StepUnreachable, StepReasonDisconnected
		case a.State == string(ConnectionPaused):
			s.Status, s.Reason = StepWaiting, StepReasonPaused
		default:
			s.Status, s.Reason = StepWaiting, StepReasonNotYet
		}
		t.Steps = append(t.Steps, s)
		if n.readIt {
			name := a.DisplayName
			if name == "" {
				name = a.Agent
			}
			if name != "" && !slices.Contains(readers, name) {
				readers = append(readers, name)
			}
		}
	}

	// What Memax can't reach.
	if len(committed) > 0 {
		var repo string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(repository, '') FROM v2.spaces WHERE id = $1`, t.SpaceID).Scan(&repo); err != nil && !errNoRows(err) {
			return fmt.Errorf("ledger: tombstone: %w", err)
		}
		u := UnreachableCopy{Kind: UnreachableGitHistory, Files: committed}
		if repo != "" {
			u.Repositories = []string{repo}
		}
		t.Unreachable = append(t.Unreachable, u)
	}
	if len(handEdits) > 0 {
		t.Unreachable = append(t.Unreachable, UnreachableCopy{Kind: UnreachableHandEdits, Files: handEdits})
	}
	if len(copies) > 0 {
		t.Unreachable = append(t.Unreachable, UnreachableCopy{Kind: UnreachableCopies, Targets: copies})
	}
	agents := readers
	if agents == nil {
		agents = []string{}
	}
	t.Unreachable = append(t.Unreachable, UnreachableCopy{Kind: UnreachableAgentMemory, Agents: agents})
	days := h.BackupDays
	if days <= 0 {
		days = DefaultBackupDays
	}
	t.Unreachable = append(t.Unreachable, UnreachableCopy{Kind: UnreachableBackups, Days: days})
	var processors []Processor
	if h.Embeddings.Name != "" && t.Gone.Embeddings > 0 {
		processors = append(processors, h.Embeddings)
	}
	if h.Judge.Name != "" && t.Gone.ModelVerdicts > 0 {
		processors = append(processors, h.Judge)
	}
	if h.Ask.Name != "" {
		processors = append(processors, h.Ask)
	}
	if len(processors) > 0 {
		t.Unreachable = append(t.Unreachable, UnreachableCopy{Kind: UnreachableLLM, Processors: processors})
	}
	return nil
}

// connectionNames names agent connections the scope can see (others are
// absent from the map).
func connectionNames(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]TombstoneAgent, error) {
	out := map[uuid.UUID]TombstoneAgent{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT id, agent, display_name, state FROM v2.agent_connections WHERE id = ANY ($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("ledger: agent names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a TombstoneAgent
		if err := rows.Scan(&a.ConnectionID, &a.Agent, &a.DisplayName, &a.State); err != nil {
			return nil, fmt.Errorf("ledger: agent names: %w", err)
		}
		out[a.ConnectionID] = a
	}
	return out, rows.Err()
}

// requestForget is RequestForget: an agent's memax_forget.
func (w *writer) requestForget(ctx context.Context, c *RequestForget) (Result, error) {
	mem, grant, sp, replay, err := w.open(ctx, c.Memory)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		res := *replay
		if res.Memory != nil {
			res.ForgetRequest, err = latestForgetRequest(ctx, w.tx, res.Memory.ID, w.meta.Actor.ID)
		}
		return res, err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionRequestForget, policy.Object{Ref: mem.Ref, Lifecycle: mem.Lifecycle,
		Secrets: findSecrets(w.meta.Reason)}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if _, err := transition(mem, lifecycle.VerbForget); err != nil {
		return Result{}, err
	}
	// Asking again is the same request.
	if existing, err := latestForgetRequest(ctx, w.tx, mem.ID, w.meta.Actor.ID); err != nil {
		return Result{}, err
	} else if existing != nil && existing.Status == ForgetRequestWaiting {
		res := Result{Outcome: OutcomeApplied, Policy: dec, Memory: mem, ForgetRequest: existing, Unchanged: true}
		return res, nil
	}
	var waiting int
	if err := w.tx.QueryRow(ctx, `
		SELECT count(*) FROM v2.forget_requests WHERE space_id = $1 AND requested_by = $2 AND status = 'waiting'`,
		sp.ID, w.meta.Actor.ID).Scan(&waiting); err != nil {
		return Result{}, fmt.Errorf("ledger: forget requests: %w", err)
	}
	if waiting >= MaxWaitingForgetRequests {
		return refused(policy.Decision{Effect: policy.EffectRefuse, Code: policy.CodeGateLimit, Message: fmt.Sprintf(
			"%d memories already wait for a person to forget them in %s. They're answered on the web.",
			MaxWaitingForgetRequests, sp.Name)}), nil
	}
	rc := w.receipt(sp, mem.ID, mem.Ref, ActionForgetRequested, mem.streamVersion+1, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.memories SET stream_version = $2, last_receipt_id = $3, updated_at = now() WHERE id = $1 AND space_id = $4`,
		mem.ID, rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: request forget: %w", err)
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.forget_requests (id, tenant_id, space_id, memory_id, requested_by, agent, session_ref,
		                                created_receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
		newID(), sp.TenantID, sp.ID, mem.ID, w.meta.Actor.ID, nullText(w.meta.Actor.Agent), nullText(w.meta.SessionRef), rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: request forget: %w", err)
	}
	res, err := w.finish(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}, mem.ID)
	if err != nil {
		return Result{}, err
	}
	res.ForgetRequest, err = latestForgetRequest(ctx, w.tx, mem.ID, w.meta.Actor.ID)
	return res, err
}

// declineForget is DeclineForget: a person keeps the memory.
func (w *writer) declineForget(ctx context.Context, c *DeclineForget) (Result, error) {
	mem, grant, sp, replay, err := w.open(ctx, c.Memory)
	if err != nil || replay != nil {
		return deref(replay), err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionDeclineForget, policy.Object{Ref: mem.Ref, Lifecycle: mem.Lifecycle}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	var waiting int
	if err := w.tx.QueryRow(ctx, `SELECT count(*) FROM v2.forget_requests WHERE memory_id = $1 AND status = 'waiting'`, mem.ID).Scan(&waiting); err != nil {
		return Result{}, fmt.Errorf("ledger: forget requests: %w", err)
	}
	if waiting == 0 {
		return Result{}, &TransitionError{Ref: mem.Ref, Err: &lifecycleNothingToDecline}
	}
	rc := w.receipt(sp, mem.ID, mem.Ref, ActionForgetDeclined, mem.streamVersion+1, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.memories SET stream_version = $2, last_receipt_id = $3, updated_at = now() WHERE id = $1 AND space_id = $4`,
		mem.ID, rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: keep it: %w", err)
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.forget_requests SET status = 'declined', decided_by = $2, decided_at = now(), last_receipt_id = $3, updated_at = now()
		 WHERE memory_id = $1 AND space_id = $4 AND status = 'waiting'`, mem.ID, w.meta.Actor.ID, rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: keep it: %w", err)
	}
	return w.finish(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}, mem.ID)
}

const forgetRequestSelect = `
	SELECT f.id, f.memory_id, m.seq, f.space_id, f.requested_by, COALESCE(f.agent, ''), COALESCE(c.display_name, ''),
	       COALESCE(f.session_ref, ''), COALESCE(r.reason, ''), f.status, f.decided_by, f.decided_at, f.created_receipt_id, f.created_at
	  FROM v2.forget_requests f
	  JOIN v2.memories m ON m.id = f.memory_id
	  JOIN v2.receipts r ON r.id = f.created_receipt_id
	  LEFT JOIN v2.agent_connections c ON c.id = f.requested_by`

func scanForgetRequest(row pgx.CollectableRow) (ForgetRequest, error) {
	var f ForgetRequest
	var seq int64
	err := row.Scan(&f.ID, &f.MemoryID, &seq, &f.SpaceID, &f.Agent.ConnectionID, &f.Agent.Agent, &f.Agent.DisplayName,
		&f.SessionRef, &f.Reason, &f.Status, &f.DecidedBy, &f.DecidedAt, &f.ReceiptID, &f.RequestedAt)
	f.Ref = FormatRef(PrefixMemory, seq)
	return f, err
}

func latestForgetRequest(ctx context.Context, tx pgx.Tx, memoryID, connection uuid.UUID) (*ForgetRequest, error) {
	rows, err := tx.Query(ctx, forgetRequestSelect+` WHERE f.memory_id = $1 AND f.requested_by = $2 ORDER BY f.created_at DESC LIMIT 1`,
		memoryID, connection)
	if err != nil {
		return nil, fmt.Errorf("ledger: forget request: %w", err)
	}
	fs, err := pgx.CollectRows(rows, scanForgetRequest)
	if err != nil || len(fs) == 0 {
		return nil, err
	}
	return &fs[0], nil
}

// ForgetRequests lists the requests waiting on a memory, oldest first.
func (l *Ledger) ForgetRequests(ctx context.Context, scope Scope, memoryID uuid.UUID) ([]ForgetRequest, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	var out []ForgetRequest
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, forgetRequestSelect+`
			 WHERE f.memory_id = $1 AND f.space_id = ANY ($2) AND f.status = 'waiting' ORDER BY f.created_at`, memoryID, scope.SpaceIDs())
		if err != nil {
			return fmt.Errorf("ledger: forget requests: %w", err)
		}
		out, err = pgx.CollectRows(rows, scanForgetRequest)
		return err
	})
	if out == nil {
		out = []ForgetRequest{}
	}
	return out, err
}
