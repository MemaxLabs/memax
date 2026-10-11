package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/textsig"
)

// Writing an import (see the package note in imports.go).
//
// Each statement (or group of repeats) is still its own ImportStatement:
// its own receipt, its own idempotency key (from the request's and the
// lead item's), its policy decision and its repeat check, decided in the
// order the statements came, each seeing the proposals the ones before it
// made. What changed is that the whole upload is one transaction, in round
// trips that don't grow with its size (about 13 a statement before, a
// third of a second each on production's network):
//
//  1. One round trip opens the import (or finds the one the key opened),
//     reads the space, the statements' earlier applications and the
//     memories they wrote, every statement's repeat candidates and the
//     memories its sources cite, and takes a block of display IDs, one per
//     statement, from the tenant's counter.
//  2. Go decides each statement in order: a replay, a repeat of what the
//     space has (or of a proposal an earlier statement made), a refusal, or
//     a new proposal with the next ID of the block.
//  3. One round trip writes it all: the receipts, memories, versions,
//     sources, the import's items, the idempotency records and the
//     upload's close, gives back the IDs it didn't use, and reads the
//     summary.
//  4. River's insert (each proposal's judge_proposal and index_memory, and
//     the import's judge_import), then COMMIT.
//
// The counter's row stays locked from the first round trip to COMMIT, so
// the block is the tenant's next numbers in order with no gap: what it
// gives back nobody else could have taken. It is the only lock an import
// waits for besides a duplicate upload's: the import's key serialises two
// uploads with one key (the second waits for the first's import row, then
// replays), and nothing an import writes is a row anyone else holds.
//
// A statement's record is written with its outcome only when it applied (a
// proposal, or a repeat of what the space has): a refused statement is
// decided afresh on a retry, as Apply's rollback leaves it. An upload a
// previous server left half done resumes: its statements' records replay.

// importGroup is one statement of an import: an item, or a group of
// repeats folded into one proposal.
type importGroup struct {
	items []int
	key   string
	nm    NewMemory
	rows  []importItemRow
	hash  string
	bands []int64
}

// repeatCandidate is a memory a statement may repeat (alreadyRecorded's
// candidates).
type repeatCandidate struct {
	id        uuid.UUID
	seq       int64
	lifecycle lifecycle.Lifecycle
	statement string
	// exact is content_sha256 = the statement's: nil when the memory has
	// no fingerprint.
	exact *bool
}

// importCandidatesSQL reads, in one pass over the space, every memory any
// of the import's statements may repeat ($2 their fingerprints, $3 all
// their bands): alreadyRecorded's candidates (the same fingerprint, or a
// MinHash band in common) for every statement at once, without its order
// and limit, which spaceRepeats applies per statement. It costs one scan of
// the space's bands, hashed against the statements': alreadyRecorded's
// query sent once per statement would cost a scan of the space each (or
// rely on the planner choosing the bands' GIN index, which it doesn't when
// it believes a space small), and seconds for an upload of hundreds.
//
// The planner can't size unnest's output, so it may believe both sides of
// the band join tiny and join them by nested loop, one statement's bands
// against every memory's; importNestloopOff (and importNestloopBack after)
// turn nested loops off for this statement alone, with JIT, which the
// resulting cost would otherwise switch on for half a second of compiling.
const importCandidatesSQL = `
	WITH hashes AS MATERIALIZED (SELECT DISTINCT h.hash FROM unnest($2::text[]) AS h(hash)),
	     want AS MATERIALIZED (SELECT DISTINCT b.band FROM unnest($3::bigint[]) AS b(band)),
	     bands AS MATERIALIZED (
	       SELECT m.id, b.band FROM v2.memories m CROSS JOIN LATERAL unnest(m.minhash_bands) AS b(band)
	        WHERE m.space_id = $1 AND m.lifecycle <> 'forgotten'),
	     hit AS MATERIALIZED (
	       SELECT bands.id FROM bands JOIN want ON want.band = bands.band
	        UNION
	       SELECT m.id FROM v2.memories m JOIN hashes ON hashes.hash = m.content_sha256
	        WHERE m.space_id = $1 AND m.lifecycle <> 'forgotten')
	SELECT m.id, m.seq, m.lifecycle, m.content_sha256, m.minhash_bands, COALESCE(v.statement, '')
	  FROM hit
	  JOIN v2.memories m ON m.id = hit.id
	  LEFT JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version
	 WHERE m.space_id = $1`

// importNestloopOff and importNestloopBack bracket importCandidatesSQL in
// its pipeline, keeping the transaction's own settings for what follows.
const (
	importNestloopOff = `SELECT set_config('memax.import_nestloop', current_setting('enable_nestloop'), true),
	                            set_config('memax.import_jit', current_setting('jit'), true),
	                            set_config('enable_nestloop', 'off', true), set_config('jit', 'off', true)`
	importNestloopBack = `SELECT set_config('enable_nestloop', current_setting('memax.import_nestloop'), true),
	                             set_config('jit', current_setting('memax.import_jit'), true)`
)

// spaceCandidate is a row of importCandidatesSQL.
type spaceCandidate struct {
	repeatCandidate
	hash  *string
	bands []int64
}

// spaceRepeats indexes the space's candidates by fingerprint and band.
type spaceRepeats struct {
	all    []spaceCandidate
	byHash map[string][]int
	byBand map[int64][]int
}

func newSpaceRepeats() *spaceRepeats {
	return &spaceRepeats{byHash: map[string][]int{}, byBand: map[int64][]int{}}
}

func (s *spaceRepeats) add(c spaceCandidate) {
	i := len(s.all)
	s.all = append(s.all, c)
	if c.hash != nil {
		s.byHash[*c.hash] = append(s.byHash[*c.hash], i)
	}
	for _, b := range c.bands {
		s.byBand[b] = append(s.byBand[b], i)
	}
}

// of is alreadyRecorded's query for one statement: the space's memories
// with its fingerprint or one of its bands, in alreadyRecorded's order, at
// most 50.
func (s *spaceRepeats) of(g *importGroup) []repeatCandidate {
	idx := slices.Clone(s.byHash[g.hash])
	for _, b := range g.bands {
		idx = append(idx, s.byBand[b]...)
	}
	slices.Sort(idx)
	idx = slices.Compact(idx)
	out := make([]repeatCandidate, 0, len(idx))
	for _, i := range idx {
		c := s.all[i].repeatCandidate
		if h := s.all[i].hash; h != nil {
			same := *h == g.hash
			c.exact = &same
		}
		out = append(out, c)
	}
	slices.SortStableFunc(out, compareRepeats)
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}

// repeatRank is alreadyRecorded's preference among lifecycles.
func repeatRank(l lifecycle.Lifecycle) int {
	switch l {
	case lifecycle.Kept:
		return 0
	case lifecycle.Proposed:
		return 1
	case lifecycle.Faded:
		return 2
	case lifecycle.Merged:
		return 3
	}
	return 4
}

// compareRepeats orders candidates as alreadyRecorded's ORDER BY does:
// exact first (DESC puts NULL before true), then by lifecycle, then seq.
func compareRepeats(a, b repeatCandidate) int {
	exactKey := func(c repeatCandidate) int {
		switch {
		case c.exact == nil:
			return 0
		case *c.exact:
			return 1
		}
		return 2
	}
	if d := exactKey(a) - exactKey(b); d != 0 {
		return d
	}
	if d := repeatRank(a.lifecycle) - repeatRank(b.lifecycle); d != 0 {
		return d
	}
	switch {
	case a.seq < b.seq:
		return -1
	case a.seq > b.seq:
		return 1
	}
	return 0
}

// findRepeat is alreadyRecorded for one statement of the batch: among the
// space's candidates as the transaction began and the proposals the
// statements before it made (which the same query would have found had
// each been its own transaction), the first by alreadyRecorded's order
// that it repeats.
func findRepeat(g *importGroup, space []repeatCandidate, made []*madeProposal) *repeatCandidate {
	cands := slices.Clone(space)
	for _, p := range made {
		same := p.hash == g.hash
		if !same && !bandsOverlap(p.bands, g.bands) {
			continue
		}
		cands = append(cands, repeatCandidate{id: p.id, seq: p.seq, lifecycle: lifecycle.Proposed, statement: p.statement, exact: &same})
	}
	slices.SortStableFunc(cands, compareRepeats)
	if len(cands) > 50 {
		cands = cands[:50]
	}
	for i, c := range cands {
		if textsig.ContentSHA256(c.statement) == g.hash || textsig.NearDuplicate(g.nm.Statement, c.statement) {
			return &cands[i]
		}
	}
	return nil
}

func bandsOverlap(a, b []int64) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// madeProposal is a proposal an earlier statement of the batch made.
type madeProposal struct {
	id        uuid.UUID
	seq       int64
	statement string
	hash      string
	bands     []int64
}

// importItems is Import once the request is valid: open the import, decide
// and write every statement (or group of repeats), close it and queue its
// conflict check, all in one transaction.
func (l *Ledger) importItems(ctx context.Context, meta Meta, req ImportRequest, origin importOrigin) (*ImportResult, error) {
	if _, ok := meta.Scope.Grant(req.SpaceID); !ok {
		return nil, ErrNotFound
	}
	meta.Via = policy.ViaImport
	now := l.now()
	if err := validateMeta(&meta, now); err != nil {
		return nil, err
	}
	for i := range req.Items {
		req.Items[i].SpaceID = req.SpaceID
		if err := req.Items[i].NewMemory.validate(); err != nil {
			return nil, err
		}
	}
	meta.Scope = meta.Scope.Narrow(req.SpaceID)
	hash, err := req.hash()
	if err != nil {
		return nil, err
	}
	files, err := json.Marshal(nonNilSlice(req.Files))
	if err != nil {
		return nil, err
	}
	skipped, err := json.Marshal(nonNilSlice(req.Skipped))
	if err != nil {
		return nil, err
	}

	prefix := itemKeyPrefix(meta.IdempotencyKey)
	var groups []*importGroup
	var keys, hashes []string
	var bands []int64
	var srcIDs []uuid.UUID
	var srcSeqs []int64
	for _, g := range foldImport(req.Items) {
		lead := req.Items[g[0]]
		st := &importGroup{items: g, key: prefix + lead.Key, nm: mergeGroup(req.Items, g)}
		for _, p := range g {
			it := req.Items[p]
			st.rows = append(st.rows, importItemRow{Position: p, Key: it.Key, Ref: it.Ref, Location: it.Location,
				HiddenCharacters: it.HiddenCharacters})
		}
		st.hash, st.bands = signature(st.nm.Statement)
		for _, s := range st.nm.Sources {
			if s.Kind != SourceMemory {
				continue
			}
			if id, seq, err := memorySourceRef(s.Ref); err == nil {
				if id != uuid.Nil {
					srcIDs = append(srcIDs, id)
				} else {
					srcSeqs = append(srcSeqs, seq)
				}
			}
		}
		groups = append(groups, st)
		keys, hashes, bands = append(keys, st.key), append(hashes, st.hash), append(bands, st.bands...)
	}

	tx, loginRole, err := l.begin(ctx, meta.Scope, pgx.ReadWrite)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	w := &writer{tx: tx, meta: &meta, command: CommandImportStatement, inserter: l.inserter, loginRole: loginRole,
		space: req.SpaceID, indexJobs: l.indexJobs, now: now, undoWindow: l.undoWindow, judgeUndoWindow: l.judgeUndoWindow,
		returnWindow: l.returnWindow, forgetHonesty: l.honesty, dreamUndoWindow: l.dreamUndoWindow}
	actorKind, actorID := string(meta.Actor.Kind), w.actorID()
	scopeIDs := meta.Scope.SpaceIDs()

	// 1. Open the import and read everything, in one round trip.
	var (
		sp            spaceRow
		spaceErr      error
		read          bool
		importID      uuid.UUID
		storedHash    []byte
		claims        map[string]*storedClaim
		replayedMems  = map[uuid.UUID]*repeatCandidate{}
		space         = newSpaceRepeats()
		sourceMems    = map[uuid.UUID]sourceMemory{}
		sourceBySeq   = map[int64]sourceMemory{}
		first         int64
		allocated     bool
		newImportID   = newID()
		claimedSubSQL = `SELECT object_id FROM v2.command_keys
		                  WHERE space_id = $1 AND actor_kind = $2 AND actor_id IS NOT DISTINCT FROM $3 AND idempotency_key = ANY ($4)`
	)
	b := &pgx.Batch{}
	b.Queue(spaceSQL, req.SpaceID).QueryRow(func(row pgx.Row) error {
		read = true
		sp, spaceErr = scanSpace(ctx, row, req.SpaceID)
		return nil
	})
	b.Queue(`
		INSERT INTO v2.imports (id, tenant_id, space_id, actor_kind, actor_id, agent, idempotency_key, request_sha256,
		                        client, files, skipped, items_total, origin)
		SELECT $1, s.tenant_id, s.id, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12 FROM v2.spaces s WHERE s.id = $2
		ON CONFLICT ON CONSTRAINT imports_key DO NOTHING`,
		newImportID, req.SpaceID, actorKind, actorID, nullText(meta.Actor.Agent), meta.IdempotencyKey, hash,
		nullText(req.Client), files, skipped, len(req.Items), string(origin))
	b.Queue(`
		SELECT id, request_sha256 FROM v2.imports
		 WHERE space_id = $1 AND actor_kind = $2 AND actor_id IS NOT DISTINCT FROM $3 AND idempotency_key = $4`,
		req.SpaceID, actorKind, actorID, meta.IdempotencyKey).Query(func(rows pgx.Rows) error {
		defer rows.Close()
		for rows.Next() {
			if err := rows.Scan(&importID, &storedHash); err != nil {
				return fmt.Errorf("ledger: open import: %w", err)
			}
		}
		return rows.Err()
	})
	b.Queue(claimsSQL, req.SpaceID, actorKind, actorID, keys).Query(func(rows pgx.Rows) error {
		claims, err = scanClaims(rows)
		return err
	})
	b.Queue(`SELECT m.id, m.seq, m.lifecycle FROM v2.memories m WHERE m.id IN (`+claimedSubSQL+`) AND m.space_id = ANY ($5)`,
		req.SpaceID, actorKind, actorID, keys, scopeIDs).Query(func(rows pgx.Rows) error {
		defer rows.Close()
		for rows.Next() {
			var c repeatCandidate
			if err := rows.Scan(&c.id, &c.seq, &c.lifecycle); err != nil {
				return fmt.Errorf("ledger: load memories: %w", err)
			}
			replayedMems[c.id] = &c
		}
		return rows.Err()
	})
	if len(groups) > 0 {
		b.Queue(importNestloopOff)
		b.Queue(importCandidatesSQL, req.SpaceID, hashes, bands).Query(func(rows pgx.Rows) error {
			defer rows.Close()
			for rows.Next() {
				var c spaceCandidate
				if err := rows.Scan(&c.id, &c.seq, &c.lifecycle, &c.hash, &c.bands, &c.statement); err != nil {
					return fmt.Errorf("ledger: find repeats: %w", err)
				}
				space.add(c)
			}
			return rows.Err()
		})
		b.Queue(importNestloopBack)
		b.Queue(`
			INSERT INTO v2.id_counters AS c (tenant_id, prefix, next)
			SELECT s.tenant_id, $2, 1 + $3::bigint FROM v2.spaces s WHERE s.id = $1
			ON CONFLICT (tenant_id, prefix) DO UPDATE SET next = c.next + $3::bigint
			RETURNING next - $3::bigint`, req.SpaceID, string(PrefixMemory), len(groups)).Query(func(rows pgx.Rows) error {
			defer rows.Close()
			for rows.Next() {
				if err := rows.Scan(&first); err != nil {
					return fmt.Errorf("ledger: allocate M- numbers: %w", err)
				}
				allocated = true
			}
			return rows.Err()
		})
	}
	if len(srcIDs)+len(srcSeqs) > 0 {
		b.Queue(`SELECT id, seq, lifecycle, trust FROM v2.memories WHERE space_id = $1 AND (id = ANY ($2) OR seq = ANY ($3))`,
			req.SpaceID, srcIDs, srcSeqs).Query(func(rows pgx.Rows) error {
			defer rows.Close()
			for rows.Next() {
				var m sourceMemory
				if err := rows.Scan(&m.id, &m.seq, &m.lifecycle, &m.trust); err != nil {
					return fmt.Errorf("ledger: resolve memory source: %w", err)
				}
				sourceMems[m.id], sourceBySeq[m.seq] = m, m
			}
			return rows.Err()
		})
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return nil, mapDBError(fmt.Errorf("ledger: import: %w", err))
	}
	switch {
	case !read:
		return nil, fmt.Errorf("ledger: import: the space wasn't read")
	case spaceErr != nil:
		return nil, spaceErr
	}
	grant, _ := meta.Scope.Grant(sp.ID)
	if d := policy.Decide(toPolicyActor(meta.Actor, meta.Via, grant), policy.ActionPropose, policy.Object{}, sp.policy()); d.Effect == policy.EffectRefuse {
		return &ImportResult{Refused: &d}, nil
	}
	if importID == uuid.Nil || (len(groups) > 0 && !allocated) {
		return nil, fmt.Errorf("ledger: open import: the import wasn't opened")
	}
	replayed := importID != newImportID
	if replayed && !bytes.Equal(storedHash, hash) {
		return nil, fmt.Errorf("%w: key %q opened another import; send a new key for a new import",
			ErrIdempotencyKeyReused, meta.IdempotencyKey)
	}
	imp := &Import{ID: importID, SpaceID: sp.ID, TenantID: sp.TenantID}
	findSource := func(id uuid.UUID, seq int64) (sourceMemory, error) {
		if id != uuid.Nil {
			return sourceMems[id], nil
		}
		return sourceBySeq[seq], nil
	}

	// 2. Decide each statement in order.
	results := make([]ImportItemResult, len(req.Items))
	var (
		writes  importWrites
		records []claimRow
		made    []*madeProposal
		applied []string // refs for the log
	)
	for _, st := range groups {
		cmd := &ImportStatement{Meta: meta, Import: imp.ID, NewMemory: st.nm, Items: st.rows}
		cmd.IdempotencyKey = st.key
		if err := validateMeta(&cmd.Meta, now); err != nil {
			return nil, err
		}
		if err := cmd.validate(); err != nil {
			return nil, err
		}
		cmdHash, err := requestHash(cmd)
		if err != nil {
			return nil, err
		}
		leadKey := req.Items[st.items[0]].Key
		item := func(i int) ImportItemResult {
			p := st.items[i]
			return ImportItemResult{Key: req.Items[p].Key, Position: p, Ref: req.Items[p].Ref}
		}

		if stored := claims[st.key]; stored != nil {
			c, err := matchClaim(st.key, CommandImportStatement, cmdHash, stored)
			if err != nil {
				return nil, err
			}
			var m *repeatCandidate
			if c.objectID != nil {
				m = replayedMems[*c.objectID]
			}
			if m == nil {
				return nil, ErrNotFound
			}
			ptr := &MemoryPointer{ID: m.id, Ref: FormatRef(PrefixMemory, m.seq)}
			for i, p := range st.items {
				r := item(i)
				switch {
				case len(c.receiptIDs) == 0:
					r.Outcome, r.Memory, r.Lifecycle = ImportExisting, ptr, string(m.lifecycle)
				case i == 0:
					r.Outcome, r.Memory = ImportProposed, ptr
				default:
					r.Outcome, r.FoldedInto, r.Memory = ImportFolded, leadKey, ptr
				}
				results[p] = r
			}
			continue
		}

		rows := slices.Clone(st.rows)
		if existing := findRepeat(st, space.of(st), made); existing != nil {
			ptr := &MemoryPointer{ID: existing.id, Ref: FormatRef(PrefixMemory, existing.seq)}
			for i := range rows {
				rows[i].Outcome, rows[i].MemoryID = ImportExisting, &existing.id
				r := item(i)
				r.Outcome, r.Memory, r.Lifecycle = ImportExisting, ptr, string(existing.lifecycle)
				results[st.items[i]] = r
			}
			writes.items = append(writes.items, rows...)
			raw, err := policyJSON(policy.Decision{Effect: policy.EffectApply})
			if err != nil {
				return nil, err
			}
			id := existing.id
			records = append(records, claimRow{key: st.key, hash: cmdHash, outcome: OutcomeApplied, policy: raw, object: &id,
				receipts: []uuid.UUID{}})
			applied = append(applied, ptr.Ref)
			continue
		}

		// writeMemory, as a proposal.
		actorTrust := policy.ActorTrust(meta.Actor.Kind, meta.Via)
		srcs, err := settleMemorySources(resolveSources(cmd.Sources, actorTrust), actorTrust, findSource)
		if err != nil {
			return nil, err
		}
		trusts := []policy.Trust{actorTrust}
		texts := []string{cmd.Statement, meta.Reason}
		for _, s := range srcs {
			trusts = append(trusts, s.trust)
			texts = append(texts, s.Quote)
		}
		trust := policy.MinTrust(trusts...)
		obj := policy.Object{
			Decision:            cmd.Kind == KindDecision,
			External:            trust.External(),
			ContradictsDecision: cmd.ContradictsDecision,
			Secrets:             findSecrets(texts...),
		}
		dec := policy.Decide(w.policyActor(grant), policy.ActionPropose, obj, sp.policy())
		switch dec.Effect {
		case policy.EffectRefuse:
			d := dec
			for i := range rows {
				rows[i].Outcome, rows[i].Code = ImportRefused, d.Code
				r := item(i)
				r.Outcome, r.Policy = ImportRefused, &d
				results[st.items[i]] = r
			}
			writes.items = append(writes.items, rows...)
			continue
		case policy.EffectPropose:
		default:
			// Policy sends everything through the import surface to Review
			// (policy.CodeImport); an import has no path that keeps.
			return nil, fmt.Errorf("ledger: import: policy decided %s for %s; an import only proposes", dec.Effect, st.key)
		}
		state, err := lifecycle.Transition(lifecycle.State{}, lifecycle.VerbPropose)
		if err != nil {
			return nil, err
		}
		seq := first + int64(len(made))
		id := newID()
		ref := FormatRef(PrefixMemory, seq)
		rc := w.receipt(sp, id, ref, ActionProposed, 1, meta.Reason)
		if len(srcs) > 0 {
			rc.Source = &ReceiptSource{Kind: string(srcs[0].Kind), Ref: srcs[0].Ref}
		}
		if err := writes.proposal(id, seq, &rc, memoryRow{
			Statement: cmd.Statement, Section: cmd.Section, Kind: cmd.Kind, Decision: cmd.Decision, Trust: trust,
			StaleAfter: cmd.StaleAfter, Conditions: cmd.Conditions, Applies: cmd.Applies,
			ValidFrom: cmd.ValidFrom, ValidTo: cmd.ValidTo,
		}, st.hash, st.bands, state, srcs); err != nil {
			return nil, err
		}
		// Each proposal is judged on its own and indexed (insertMemory's
		// jobs); every id is new, so none repeats another.
		w.jobs = append(w.jobs, river.InsertManyParams{Args: JudgeArgs{MemoryID: id, SpaceID: sp.ID, Version: 1, Mode: JudgeProposal}})
		if w.indexJobs {
			w.jobs = append(w.jobs, river.InsertManyParams{Args: IndexArgs{MemoryID: id, SpaceID: sp.ID, Version: 1}})
		}
		lead := rows[0].Position
		ptr := &MemoryPointer{ID: id, Ref: ref}
		for i := range rows {
			rows[i].Outcome, rows[i].MemoryID = ImportProposed, &id
			r := item(i)
			r.Outcome, r.Memory = ImportProposed, ptr
			if i > 0 {
				rows[i].Outcome, rows[i].FoldedInto = ImportFolded, &lead
				r.Outcome, r.FoldedInto = ImportFolded, leadKey
			}
			results[st.items[i]] = r
		}
		writes.items = append(writes.items, rows...)
		raw, err := policyJSON(dec)
		if err != nil {
			return nil, err
		}
		records = append(records, claimRow{key: st.key, hash: cmdHash, outcome: outcomeFor(dec.Effect), policy: raw,
			object: &id, receipts: []uuid.UUID{rc.ID}})
		made = append(made, &madeProposal{id: id, seq: seq, statement: cmd.Statement, hash: st.hash, bands: st.bands})
		applied = append(applied, ref)
	}

	// 3. Every write, the upload's close and its summary, in one round trip.
	b = &pgx.Batch{}
	if allocated && len(made) < len(groups) {
		// Give back the numbers no statement took: the row has been
		// locked since they were taken, so they are still the last.
		b.Queue(`UPDATE v2.id_counters SET next = $3 WHERE tenant_id = $1 AND prefix = $2`,
			sp.TenantID, string(PrefixMemory), first+int64(len(made)))
	}
	if err := writes.queue(b, sp); err != nil {
		return nil, err
	}
	insertImportItemRows(b, imp.ID, sp.ID, writes.items)
	w.queueClaims(b, sp.ID, CommandImportStatement, records)
	b.Queue(`UPDATE v2.imports SET uploaded_at = COALESCE(uploaded_at, now()) WHERE id = $1 AND space_id = $2`, imp.ID, sp.ID)
	summary := queueImportSummary(b, imp.ID, sp.ID)
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return nil, mapDBError(fmt.Errorf("ledger: import: %w", err))
	}
	if summary.imp == nil {
		return nil, ErrNotFound
	}

	// 4. The jobs (the import's conflict check last), then COMMIT.
	w.jobs = append(w.jobs, river.InsertManyParams{Args: JudgeImportArgs{ImportID: imp.ID, SpaceID: imp.SpaceID}})
	if err := w.flush(ctx); err != nil {
		return nil, mapDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapDBError(fmt.Errorf("ledger: commit import: %w", err))
	}
	l.log.Info("ledger: imported", "import", imp.ID.String(), "space_id", sp.ID.String(), "origin", string(origin),
		"statements", len(groups), "written", len(applied), "proposed", len(made), "replayed", replayed,
		"actor_kind", string(meta.Actor.Kind), "jobs", len(w.jobs))
	return &ImportResult{Import: summary.imp, Items: results, Replayed: replayed}, nil
}

// importWrites are the rows an import's new proposals write, as arrays.
type importWrites struct {
	receipts []*Receipt
	// memories
	ids                                        []uuid.UUID
	seqs                                       []int64
	sections, kinds, trusts, lifecycles, flags []string
	staleAfter, validFrom, validTo             []*time.Time
	conditions, scopes                         []string
	decisions                                  []*string
	statements, hashes, bands                  []string
	receiptIDs                                 []uuid.UUID
	// sources
	srcIDs, srcMemories, srcReceipts   []uuid.UUID
	srcKinds, srcRefs, srcLocs, srcTru []string
	srcURIs, srcQuotes, srcHashes      []*string
	// the import's items
	items []importItemRow
}

// proposal adds a new memory: its receipt, row, first version and sources
// (insertMemory and insertSources).
func (iw *importWrites) proposal(id uuid.UUID, seq int64, rc *Receipt, row memoryRow, hash string, bands []int64, state lifecycle.State, srcs []resolvedSource) error {
	iw.receipts = append(iw.receipts, rc)
	var decision *string
	if row.Decision != nil {
		b, err := json.Marshal(row.Decision)
		if err != nil {
			return err
		}
		s := string(b)
		decision = &s
	}
	iw.ids, iw.seqs = append(iw.ids, id), append(iw.seqs, seq)
	iw.sections, iw.kinds, iw.trusts = append(iw.sections, string(row.Section)), append(iw.kinds, string(row.Kind)), append(iw.trusts, string(row.Trust))
	iw.lifecycles, iw.flags = append(iw.lifecycles, string(state.Lifecycle)), append(iw.flags, strings.Join(state.Flags.Strings(), ","))
	iw.staleAfter, iw.validFrom, iw.validTo = append(iw.staleAfter, row.StaleAfter), append(iw.validFrom, row.ValidFrom), append(iw.validTo, row.ValidTo)
	iw.conditions, iw.scopes, iw.decisions = append(iw.conditions, string(row.Conditions)), append(iw.scopes, string(row.Applies)), append(iw.decisions, decision)
	iw.statements, iw.hashes, iw.bands = append(iw.statements, row.Statement), append(iw.hashes, hash), append(iw.bands, bandsLiteral(bands))
	iw.receiptIDs = append(iw.receiptIDs, rc.ID)
	for _, s := range srcs {
		iw.srcIDs, iw.srcMemories, iw.srcReceipts = append(iw.srcIDs, newID()), append(iw.srcMemories, id), append(iw.srcReceipts, rc.ID)
		iw.srcKinds, iw.srcRefs, iw.srcLocs, iw.srcTru = append(iw.srcKinds, string(s.Kind)), append(iw.srcRefs, s.Ref),
			append(iw.srcLocs, string(s.Locator)), append(iw.srcTru, string(s.trust))
		iw.srcURIs, iw.srcQuotes, iw.srcHashes = append(iw.srcURIs, textOrNil(s.URI)), append(iw.srcQuotes, textOrNil(s.Quote)),
			append(iw.srcHashes, textOrNil(s.ContentHash))
	}
	return nil
}

// queue queues the receipts, memories, versions and sources into b, in
// that order (each row's receipt goes first).
func (iw *importWrites) queue(b *pgx.Batch, sp spaceRow) error {
	if len(iw.ids) == 0 {
		return nil
	}
	if err := queueReceipts(b, iw.receipts); err != nil {
		return err
	}
	b.Queue(fmt.Sprintf(`
		INSERT INTO v2.memories (id, tenant_id, space_id, seq, section, kind, lifecycle, flags, trust,
		                         current_version, stream_version, stale_after, conditions, decision, scope,
		                         valid_from, valid_to, search, content_sha256, minhash_bands, created_receipt_id, last_receipt_id)
		SELECT r.id, $1, $2, r.seq, r.section, r.kind, r.lifecycle, string_to_array(r.flags, ','), r.trust,
		       1, 1, r.stale_after, r.conditions::jsonb, r.decision::jsonb, r.scope::jsonb,
		       r.valid_from, r.valid_to, %s, r.hash, r.bands::bigint[], r.receipt_id, r.receipt_id
		  FROM unnest($3::uuid[], $4::bigint[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[],
		              $10::timestamptz[], $11::text[], $12::text[], $13::text[], $14::timestamptz[], $15::timestamptz[],
		              $16::text[], $17::text[], $18::text[], $19::uuid[])
		       WITH ORDINALITY AS r(id, seq, section, kind, lifecycle, flags, trust, stale_after, conditions, decision, scope,
		                            valid_from, valid_to, statement, hash, bands, receipt_id, ord)
		 ORDER BY r.ord`, fmt.Sprintf(searchExpr, "r.statement")),
		sp.TenantID, sp.ID, iw.ids, iw.seqs, iw.sections, iw.kinds, iw.lifecycles, iw.flags, iw.trusts,
		iw.staleAfter, iw.conditions, iw.decisions, iw.scopes, iw.validFrom, iw.validTo,
		iw.statements, iw.hashes, iw.bands, iw.receiptIDs)
	b.Queue(`
		INSERT INTO v2.memory_versions (memory_id, version, space_id, statement, receipt_id, last_receipt_id)
		SELECT v.id, 1, $1, v.statement, v.receipt_id, v.receipt_id
		  FROM unnest($2::uuid[], $3::text[], $4::uuid[]) AS v(id, statement, receipt_id)`,
		sp.ID, iw.ids, iw.statements, iw.receiptIDs)
	if len(iw.srcIDs) == 0 {
		return nil
	}
	b.Queue(`
		INSERT INTO v2.sources (id, space_id, kind, uri, ref, locator, external, trust_class, quote, content_hash,
		                        created_receipt_id, last_receipt_id)
		SELECT s.id, $1, s.kind, s.uri, s.ref, s.locator::jsonb, s.trust = 'external', s.trust, s.quote, s.content_hash,
		       s.receipt_id, s.receipt_id
		  FROM unnest($2::uuid[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::uuid[])
		       WITH ORDINALITY AS s(id, kind, uri, ref, locator, trust, quote, content_hash, receipt_id, ord)
		 ORDER BY s.ord`,
		sp.ID, iw.srcIDs, iw.srcKinds, iw.srcURIs, iw.srcRefs, iw.srcLocs, iw.srcTru, iw.srcQuotes, iw.srcHashes, iw.srcReceipts)
	b.Queue(`
		INSERT INTO v2.memory_sources (memory_id, source_id, space_id, receipt_id)
		SELECT ms.memory_id, ms.source_id, $1, ms.receipt_id
		  FROM unnest($2::uuid[], $3::uuid[], $4::uuid[]) AS ms(memory_id, source_id, receipt_id)`,
		sp.ID, iw.srcMemories, iw.srcIDs, iw.srcReceipts)
	return nil
}

// insertImportItemRows queues the import's items into b (insertImportItems
// in one statement).
func insertImportItemRows(b *pgx.Batch, importID, spaceID uuid.UUID, rows []importItemRow) {
	if len(rows) == 0 {
		return
	}
	n := len(rows)
	positions, hidden := make([]int32, n), make([]int32, n)
	keys, refs, locations, outcomes := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	memories, folded, codes := make([]*uuid.UUID, n), make([]*int32, n), make([]*string, n)
	for i, r := range rows {
		positions[i], hidden[i] = int32(r.Position), int32(r.HiddenCharacters)
		keys[i], refs[i], locations[i], outcomes[i] = r.Key, r.Ref, string(r.Location), string(r.Outcome)
		memories[i], codes[i] = r.MemoryID, textOrNil(r.Code)
		if r.FoldedInto != nil {
			f := int32(*r.FoldedInto)
			folded[i] = &f
		}
	}
	b.Queue(`
		INSERT INTO v2.import_items (import_id, position, space_id, item_key, ref, location, outcome, memory_id,
		                             folded_into, code, hidden_characters)
		SELECT $1, i.position, $2, i.item_key, i.ref, i.location, i.outcome, i.memory_id, i.folded_into, i.code, i.hidden
		  FROM unnest($3::int[], $4::text[], $5::text[], $6::text[], $7::text[], $8::uuid[], $9::int[], $10::text[], $11::int[])
		       AS i(position, item_key, ref, location, outcome, memory_id, folded_into, code, hidden)
		ON CONFLICT (import_id, position) DO NOTHING`,
		importID, spaceID, positions, keys, refs, locations, outcomes, memories, folded, codes, hidden)
}

// importSummaryRead is an import's summary, filled by the batch that read
// it (queueImportSummary).
type importSummaryRead struct{ imp *Import }

// queueImportSummary queues importSummary's reads into b.
func queueImportSummary(b *pgx.Batch, id, spaceID uuid.UUID) *importSummaryRead {
	out := &importSummaryRead{}
	b.Queue(`SELECT `+importColumns+` FROM v2.imports WHERE id = $1 AND space_id = $2`, id, spaceID).Query(func(rows pgx.Rows) error {
		imps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Import, error) { return scanImport(r) })
		if err != nil {
			return fmt.Errorf("ledger: read import: %w", err)
		}
		if len(imps) == 1 {
			out.imp = imps[0]
		}
		return nil
	})
	var counts ImportCounts
	b.Queue(importCountsSQL, id, spaceID).Query(func(rows pgx.Rows) error { return scanImportCounts(rows, &counts) })
	b.Queue(importConflictsSQL, id, spaceID).QueryRow(func(row pgx.Row) error {
		if err := row.Scan(&counts.Conflicts); err != nil {
			return fmt.Errorf("ledger: count import: %w", err)
		}
		if out.imp != nil {
			counts.Items = out.imp.Counts.Items
			out.imp.Counts = counts
		}
		return nil
	})
	return out
}

// bandsLiteral is a memory's MinHash bands as an array literal.
func bandsLiteral(bands []int64) string {
	var s strings.Builder
	s.WriteByte('{')
	for i, b := range bands {
		if i > 0 {
			s.WriteByte(',')
		}
		fmt.Fprint(&s, b)
	}
	s.WriteByte('}')
	return s.String()
}
