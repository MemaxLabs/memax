package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Cloud session starts (cloud warm-start design §3.4). A cloud agent's
// sandbox has no daemon and no warm cache, so its start command names the
// compile each committed file holds (C-0881, from the file's header) and
// asks what changed since. StartSession reads everything that answer needs
// from the record in one round trip: each named compile and its target's
// latest good compile, the tombstones the files or the space's latest
// compiles hold (or forgotten since), the space's waiting gates, the
// agent's notices in the space (taken, so each is told once, here or over
// MCP), and the ids of the latest compiles' memories (for the digest read
// the caller records). The diff of the compiled words is the compile
// service's (compile.Service.Delta), from its stored artifacts.
//
// Each named compile counts as a compile load (a read of every fact in it,
// RecordCompileLoad's read) under the request's Idempotency-Key: its claim
// and its R- numbers are taken in that same first round trip, and the
// reads go out with COMMIT, so a counted start costs two round trips.
// Whether it counts is policy's ActionRead, decided before the transaction
// on the grant (as Ask decides on the grant): a person who can read the
// space, or an agent connected to it. A credential without a connection
// here gets its answer, and nothing is counted, in one read-only round
// trip.

// The bounds of one start.
const (
	MaxStartFiles = 8
	// MaxStartForgets bounds the forgets a start lists that its files don't
	// hold: the latest since the oldest compile named.
	MaxStartForgets = 20
	// MaxStartHeld bounds the forgets a start lists that its files hold.
	MaxStartHeld  = 50
	MaxStartGates = 10
	// MaxStartNotices bounds the notices one start takes; the rest come
	// with the next start or MCP response.
	MaxStartNotices = 20
	// MaxStartQuestion is how much of a gate's question a start carries.
	MaxStartQuestion = 200
)

// StartSource is how a session started, as the agent says.
type StartSource string

// The sources. A compact start loaded no file again, so it counts nothing.
const (
	StartStartup StartSource = "startup"
	StartResume  StartSource = "resume"
	StartClear   StartSource = "clear"
	StartCompact StartSource = "compact"
)

// SessionStart is one cloud session start.
type SessionStart struct {
	// Actor is the agent connection (or the credential, when it has none),
	// or the person whose session it is.
	Actor Actor
	Scope Scope
	Via   policy.Via
	// SpaceID is the space the files are compiled from.
	SpaceID uuid.UUID
	// Connection is the agent's connection, whose notices the start takes;
	// uuid.Nil for a person or a credential with no connection.
	Connection uuid.UUID
	// Agent is the agent whose session starts; an agent connection is
	// always its own agent.
	Agent      string
	SessionRef string
	Source     StartSource
	// LoadedAt is when the session loaded the files: zero means now; at
	// most a day ago (LoadWindow).
	LoadedAt time.Time
	Files    []StartFile
	// IdempotencyKey keeps a retry, or a second hook in the same session,
	// from counting the loads again.
	IdempotencyKey string
}

// StartFile is one compiled file the session loaded.
type StartFile struct {
	Path string
	// Compile is the compile its header names (C-0881).
	Compile string
	// DriftSHA256 is the file's drift hash, when the client sent one.
	DriftSHA256 string
}

// StartedFile is a named file that is one of the space's repository
// files, with the compile it holds and its target's latest.
type StartedFile struct {
	Path string
	// Here is the compile the file holds; Latest is its target's latest
	// good compile (Here itself when nothing compiled since).
	Here, Latest *CompileRun
	// HereRefs are the memories Here's file cites.
	HereRefs []string
	// HandEdited: the file's drift hash isn't what Here wrote (nor a hand
	// edit Memax accepted as the file's baseline).
	HandEdited bool
}

// StartForget is a Forget a start tells: refs and a date, never words.
type StartForget struct {
	Ref  string
	With []string
	At   time.Time
	// InFiles: a named compile holds it, so the files still say it.
	InFiles bool
}

// SessionStartView is what StartSession read.
type SessionStartView struct {
	SpaceID  uuid.UUID
	TenantID uuid.UUID
	// Files are the request's files that are this space's, in its order.
	Files []StartedFile
	// Forgotten are the Forgets to tell: those the files hold, then the
	// latest others since the oldest compile named.
	Forgotten []StartForget
	// ForgottenRefs is every forgotten memory any named or latest compile
	// cites, told or not: a line citing one is never sent.
	ForgottenRefs map[string]bool
	Gates         []Gate
	Notices       []Notice
	// MemoryIDs maps the latest compiles' refs to their memories, for the
	// digest read of the lines the caller sends.
	MemoryIDs map[string]uuid.UUID
	// Loads is how many compile loads this start recorded (0 on a replay,
	// or when it counts nothing).
	Loads int
	// Counted says whether the start counted at all (the policy's answer).
	Counted bool
}

// startKey is a load's own key in v2.command_keys, from the request's key
// and the compile: bounded, and the same on every retry.
func startKey(key, compile string) string {
	sum := sha256.Sum256([]byte(key + "\x00" + compile))
	return "start-" + hex.EncodeToString(sum[:])
}

// startHash identifies a load for its key: the space, the compile, the
// agent and the session (not the time, which a second hook sends anew).
func startHash(space uuid.UUID, compile, agent, session string) []byte {
	raw, _ := json.Marshal([]string{space.String(), compile, agent, session})
	sum := sha256.Sum256(raw)
	return sum[:]
}

func (s *SessionStart) validate(now time.Time) error {
	if s.SpaceID == uuid.Nil {
		return invalid("space", "say which space")
	}
	if s.Actor.Kind != policy.ActorAgent && s.Actor.Kind != policy.ActorPerson {
		return invalid("actor", "only an agent or a person starts a session")
	}
	if s.Actor.ID == uuid.Nil || s.Scope.PersonID == uuid.Nil {
		return invalid("actor", "is missing")
	}
	if !slices.Contains(ReadVias, s.Via) {
		return invalid("via", "use mcp, api or cli")
	}
	if n := len(s.Files); n < 1 || n > MaxStartFiles {
		return invalid("files", "name 1 to %d compiled files", MaxStartFiles)
	}
	for i := range s.Files {
		f := &s.Files[i]
		f.Path = strings.TrimSpace(f.Path)
		if f.Path == "" || len(f.Path) > 300 || strings.HasPrefix(f.Path, "/") || slices.Contains(strings.Split(f.Path, "/"), "..") {
			return invalid("files", "give each file's path relative to the repository root")
		}
		if p, _, ok := ParseRef(f.Compile); !ok || p != PrefixCompile {
			return invalid("files", "give each file's compile as its header names it, like C-0881")
		}
		_, n, _ := ParseRef(f.Compile)
		f.Compile = FormatRef(PrefixCompile, n)
		if f.DriftSHA256 != "" && !sha256Hex.MatchString(f.DriftSHA256) {
			return invalid("files", "send each file's drift hash as lowercase hex sha256")
		}
	}
	switch s.Source {
	case "":
		s.Source = StartStartup
	case StartStartup, StartResume, StartClear, StartCompact:
	default:
		return invalid("source", "use startup, resume, clear or compact")
	}
	s.SessionRef = strings.TrimSpace(s.SessionRef)
	if len(s.SessionRef) > 255 {
		return invalid("session_ref", "is longer than 255 characters")
	}
	if s.Actor.Kind == policy.ActorAgent {
		s.Agent = s.Actor.Agent
	}
	if s.Agent != "" && !AgentKind(s.Agent).Valid() {
		s.Agent = string(AgentFromV1(s.Agent))
	}
	switch {
	case s.LoadedAt.IsZero():
		s.LoadedAt = now
	case s.LoadedAt.After(now.Add(5 * time.Minute)):
		return invalid("loaded_at", "is in the future; send when the session loaded the files")
	case s.LoadedAt.Before(now.Add(-LoadWindow)):
		return invalid("loaded_at", "is more than a day ago; Memax counts loads reported within a day")
	}
	return nil
}

// The statements of a start's first round trip. Unless they say
// otherwise, $1 is the space, $2 the scope's spaces and $3 the named
// compiles' numbers (C-0881 is 881).
const (
	// The named compiles that are good runs of the space's targets.
	startHereSQL = compileSelect + `
		 WHERE r.space_id = $1 AND r.space_id = ANY ($2) AND r.seq = ANY ($3) AND r.status <> 'failed'`
	// Each named compile's target's latest good compile.
	startLatestSQL = compileSelect + `
		 WHERE r.space_id = ANY ($2) AND r.id IN (
		       SELECT (SELECT l.id FROM v2.compile_runs l
		                WHERE l.target_id = h.target_id AND l.status <> 'failed'
		                ORDER BY l.seq DESC LIMIT 1)
		         FROM v2.compile_runs h
		        WHERE h.space_id = $1 AND h.seq = ANY ($3) AND h.status <> 'failed')`
	startTargetsSQL = targetSelect + `
		 WHERE t.space_id = ANY ($2) AND t.id IN (
		       SELECT h.target_id FROM v2.compile_runs h WHERE h.space_id = $1 AND h.seq = ANY ($3))`
	// startRunsCTE names the named compiles (here) and their targets'
	// latest (latest), for the statements below: $1 is the space and $2
	// the named compiles' numbers.
	startRunsCTE = `
		WITH here AS (
		    SELECT r.target_id, r.refs, r.compiled_at FROM v2.compile_runs r
		     WHERE r.space_id = $1 AND r.seq = ANY ($2) AND r.status <> 'failed'),
		latest AS (
		    SELECT l.refs FROM (SELECT DISTINCT target_id FROM here) h
		     CROSS JOIN LATERAL (SELECT r.refs FROM v2.compile_runs r
		                          WHERE r.target_id = h.target_id AND r.status <> 'failed'
		                          ORDER BY r.seq DESC LIMIT 1) l),
		cited AS (SELECT unnest(refs) AS ref FROM here UNION SELECT unnest(refs) FROM latest)`
	// The memories of every Forget whose memories a named or latest
	// compile cites, or forgotten since the oldest compile named. $3 bounds
	// the Forgets.
	startTombstonesSQL = startRunsCTE + `,
		hit AS (
		    SELECT t.op_id, bool_or(t.object_ref IN (SELECT ref FROM cited)) AS cited, max(t.forgotten_at) AS at
		      FROM v2.tombstones t
		     WHERE t.space_id = $1 AND t.object_kind = 'memory'
		       AND (t.object_ref IN (SELECT ref FROM cited) OR t.forgotten_at >= (SELECT min(compiled_at) FROM here))
		     GROUP BY t.op_id
		     ORDER BY 2 DESC, 3 DESC
		     LIMIT $3)
		SELECT t.op_id, t.id, t.object_ref, t.forgotten_at
		  FROM v2.tombstones t JOIN hit ON hit.op_id = t.op_id
		 WHERE t.space_id = $1 AND t.object_kind = 'memory'
		 ORDER BY t.forgotten_at DESC, t.object_ref
		 LIMIT 2000`
	// The ids of the memories the latest compiles cite.
	startMemoriesSQL = startRunsCTE + `
		SELECT m.seq, m.id FROM v2.memories m
		 WHERE m.space_id = $1 AND m.seq IN (
		       SELECT substring(x FROM 3)::bigint FROM latest, unnest(latest.refs) x WHERE x ~ '^M-[0-9]{1,18}$')`
	// The space's waiting gates, newest first (MaxStartGates). $3 is now.
	startGatesSQL = gateSelect + `
		 WHERE g.space_id = $1 AND g.space_id = ANY ($2) AND g.status = 'waiting' AND g.expires_at > $3
		 ORDER BY g.seq DESC
		 LIMIT 10`
	// The connection's untold notices in the space, taken: each is told once.
	startNoticesSQL = `
		WITH taken AS (
		    UPDATE v2.agent_notices n SET delivered_at = now(), delivered_via = 'session_start'
		     WHERE n.id IN (SELECT id FROM v2.agent_notices
		                     WHERE connection_id = $1 AND space_id = $2 AND delivered_at IS NULL
		                     ORDER BY created_at, id LIMIT $3 FOR UPDATE SKIP LOCKED)
		    RETURNING n.id, n.space_id, n.op_id, n.kind, n.refs, n.read_it, n.created_at, n.autonomy
		)
		SELECT t.id, t.space_id, t.op_id, t.kind, t.refs, t.read_it, t.created_at, COALESCE(s.name, ''), COALESCE(t.autonomy, '')
		  FROM taken t LEFT JOIN v2.spaces s ON s.id = t.space_id
		 ORDER BY t.created_at, t.id`
	// The loads' claims, for the named compiles that are good runs of a
	// repository file of the space (with a file at the path named), and
	// their R- numbers, in one statement: a key already used claims
	// nothing, and takes no number. It answers the keys claimed and the
	// first number.
	startClaimSQL = `
		WITH claimed AS (
		    INSERT INTO v2.command_keys (space_id, actor_kind, actor_id, idempotency_key, command, request_hash)
		    SELECT $1, $2, $3, u.key, $4, u.hash
		      FROM unnest($5::text[], $6::bytea[], $7::bigint[], $8::text[]) AS u(key, hash, seq, path)
		     WHERE EXISTS (SELECT 1 FROM v2.compile_runs r
		                     JOIN v2.targets t ON t.id = r.target_id AND t.space_id = r.space_id
		                    WHERE r.space_id = $1 AND r.seq = u.seq AND r.status <> 'failed' AND t.root = 'repo'
		                      AND r.files @> jsonb_build_array(jsonb_build_object('path', u.path)))
		    ON CONFLICT ON CONSTRAINT command_keys_key DO NOTHING
		    RETURNING idempotency_key),
		counted AS (SELECT count(*)::bigint AS n FROM claimed),
		alloc AS (
		    INSERT INTO v2.id_counters AS c (tenant_id, prefix, next)
		    SELECT $9, 'R', n + 1 FROM counted WHERE n > 0
		    ON CONFLICT (tenant_id, prefix) DO UPDATE SET next = c.next + EXCLUDED.next - 1
		    RETURNING next)
		SELECT COALESCE((SELECT array_agg(idempotency_key) FROM claimed), '{}'),
		       COALESCE((SELECT next FROM alloc) - (SELECT n FROM counted), 0)`
)

// StartSession reads what a cloud session start answers, and counts its
// compile loads. A space outside the scope is ErrNotFound; a named file
// that isn't one of the space's repository files is left out.
func (l *Ledger) StartSession(ctx context.Context, s SessionStart) (*SessionStartView, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	now := l.now()
	if err := s.validate(now); err != nil {
		return nil, err
	}
	grant, ok := s.Scope.Grant(s.SpaceID)
	if !ok {
		return nil, ErrNotFound
	}
	// Whether this start counts: ActionRead on the grant, as Ask decides
	// before its transaction (the space's rules don't bear on a read).
	counts := s.Source != StartCompact &&
		policy.Decide(toPolicyActor(s.Actor, s.Via, grant, l.plans), policy.ActionRead, policy.Object{},
			policy.Space{Name: grant.Name, Kind: grant.Kind, OnV2: grant.OnV2}).Effect == policy.EffectApply
	if counts {
		s.IdempotencyKey = strings.TrimSpace(s.IdempotencyKey)
		if n := len(s.IdempotencyKey); n < 1 || n > 255 {
			return nil, invalid("idempotency_key", "send an Idempotency-Key of 1 to 255 characters")
		}
		if err := l.ensureReadMonth(ctx, s.LoadedAt); err != nil {
			return nil, err
		}
	}
	takes := s.Actor.Kind == policy.ActorAgent && s.Connection != uuid.Nil
	scope := s.Scope.Narrow(s.SpaceID)

	seqs := make([]int64, 0, len(s.Files))
	for _, f := range s.Files {
		_, n, _ := ParseRef(f.Compile)
		if !slices.Contains(seqs, n) {
			seqs = append(seqs, n)
		}
	}
	v := &SessionStartView{SpaceID: s.SpaceID, TenantID: grant.TenantID, ForgottenRefs: map[string]bool{},
		MemoryIDs: map[string]uuid.UUID{}, Counted: counts}
	var here, latest []CompileRun
	var targets []*Target
	var forgets []startTombstone
	var claimed []string
	var firstSeq int64
	b := &pgx.Batch{}
	spaces := scope.SpaceIDs()
	b.Queue(startHereSQL, s.SpaceID, spaces, seqs).Query(func(rows pgx.Rows) error {
		var err error
		here, err = pgx.CollectRows(rows, scanCompileRun)
		return err
	})
	b.Queue(startLatestSQL, s.SpaceID, spaces, seqs).Query(func(rows pgx.Rows) error {
		var err error
		latest, err = pgx.CollectRows(rows, scanCompileRun)
		return err
	})
	b.Queue(startTargetsSQL, s.SpaceID, spaces, seqs).Query(func(rows pgx.Rows) error {
		var err error
		targets, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Target, error) { return scanTarget(r) })
		return err
	})
	b.Queue(startTombstonesSQL, s.SpaceID, seqs, MaxStartHeld+MaxStartForgets).Query(func(rows pgx.Rows) error {
		var err error
		forgets, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (startTombstone, error) {
			var t startTombstone
			err := r.Scan(&t.op, &t.id, &t.ref, &t.at)
			return t, err
		})
		return err
	})
	b.Queue(startMemoriesSQL, s.SpaceID, seqs).Query(func(rows pgx.Rows) error {
		for rows.Next() {
			var seq int64
			var id uuid.UUID
			if err := rows.Scan(&seq, &id); err != nil {
				return err
			}
			v.MemoryIDs[FormatRef(PrefixMemory, seq)] = id
		}
		return rows.Err()
	})
	b.Queue(startGatesSQL, s.SpaceID, spaces, now).Query(func(rows pgx.Rows) error {
		var err error
		v.Gates, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Gate, error) {
			g, err := scanGate(r, now)
			if err != nil {
				return Gate{}, err
			}
			return *g, nil
		})
		return err
	})
	if takes {
		b.Queue(startNoticesSQL, s.Connection, s.SpaceID, MaxStartNotices).Query(func(rows pgx.Rows) error {
			var err error
			v.Notices, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notice, error) {
				var n Notice
				err := r.Scan(&n.ID, &n.SpaceID, &n.OpID, &n.Kind, &n.Refs, &n.ReadIt, &n.At, &n.Space, &n.Autonomy)
				return n, err
			})
			return err
		})
	}
	type load struct {
		key  string
		file StartFile
	}
	var loads []load
	if counts {
		keys, hashes, lseqs, paths := []string{}, [][]byte{}, []int64{}, []string{}
		for _, f := range s.Files {
			key := startKey(s.IdempotencyKey, f.Compile)
			if slices.ContainsFunc(loads, func(x load) bool { return x.key == key }) {
				continue // the same compile in two files counts once
			}
			_, n, _ := ParseRef(f.Compile)
			loads = append(loads, load{key: key, file: f})
			keys, lseqs, paths = append(keys, key), append(lseqs, n), append(paths, f.Path)
			hashes = append(hashes, startHash(s.SpaceID, f.Compile, s.Agent, s.SessionRef))
		}
		w := &writer{meta: &Meta{Actor: s.Actor}}
		b.Queue(startClaimSQL, s.SpaceID, string(s.Actor.Kind), w.actorID(), string(CommandLoadCompile),
			keys, hashes, lseqs, paths, grant.TenantID).QueryRow(func(row pgx.Row) error {
			return row.Scan(&claimed, &firstSeq)
		})
	}

	// A start that writes nothing is one read-only round trip.
	if !counts && !takes {
		if err := l.ReadBatch(ctx, scope, b); err != nil {
			return nil, fmt.Errorf("ledger: session start: %w", err)
		}
		v.fill(s, here, latest, targets, forgets)
		return v, nil
	}
	tx, _, err := l.begin(ctx, scope, pgx.ReadWrite)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return nil, mapDBError(fmt.Errorf("ledger: session start: %w", err))
	}
	v.fill(s, here, latest, targets, forgets)

	// The loads the claims took go out with COMMIT.
	if len(claimed) > 0 {
		byCompile := map[string]*CompileRun{}
		for i := range here {
			byCompile[here[i].Ref] = &here[i]
		}
		var reads []pendingRead
		dec := policy.Decision{Effect: policy.EffectApply}
		w := &writer{tx: tx, meta: &Meta{Actor: s.Actor, Scope: scope, Via: s.Via}, command: CommandLoadCompile, now: now}
		for _, ld := range loads {
			run := byCompile[ld.file.Compile]
			if run == nil || !slices.Contains(claimed, ld.key) {
				continue
			}
			e := ReadEvent{SpaceID: s.SpaceID, TenantID: grant.TenantID, PersonID: s.Scope.PersonID, Agent: s.Agent,
				Kind: ReadCompileLoad, Via: s.Via, SessionRef: s.SessionRef, CompileID: run.ID, At: s.LoadedAt}
			if s.Actor.Kind == policy.ActorAgent {
				e.Reader, e.ConnectionID = ReaderAgent, s.Actor.ID
			} else {
				e.Reader, e.PersonID = ReaderPerson, s.Actor.ID
			}
			e, ok := normalizeRead(e, now)
			if !ok {
				return nil, invalid("files", "this start's loads can't be recorded")
			}
			p := pendingRead{event: e, id: newID(), seq: firstSeq + int64(len(reads)),
				compile: &readCompile{id: run.ID, space: run.SpaceID, seq: run.seq, facts: len(run.Refs)}}
			reads = append(reads, p)
			w.meta.IdempotencyKey = ld.key
			w.space = s.SpaceID
			if err := w.record(ctx, Result{Outcome: OutcomeApplied, Policy: dec}, p.id); err != nil {
				return nil, mapDBError(err)
			}
		}
		if len(reads) != len(claimed) {
			// A claim whose compile the reads above didn't see: the run
			// changed between the two statements. Count nothing.
			return nil, fmt.Errorf("ledger: session start: %d loads claimed, %d found", len(claimed), len(reads))
		}
		if err := insertReads(ctx, tx, reads); err != nil {
			return nil, mapDBError(err)
		}
		v.Loads = len(reads)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapDBError(fmt.Errorf("ledger: session start: commit: %w", err))
	}
	if v.Loads > 0 {
		l.log.Info("ledger: session start", "space_id", s.SpaceID.String(), "agent", s.Agent, "loads", v.Loads,
			"files", len(v.Files), "forgotten", len(v.Forgotten), "gates", len(v.Gates), "notices", len(v.Notices))
	}
	return v, nil
}

// startTombstone is one forgotten memory of a Forget a start read.
type startTombstone struct {
	op, id uuid.UUID
	ref    string
	at     time.Time
}

// fill matches the request's files to the runs read, and works out the
// Forgets to tell.
func (v *SessionStartView) fill(s SessionStart, here, latest []CompileRun, targets []*Target, tombs []startTombstone) {
	hereBy := map[string]*CompileRun{}
	for i := range here {
		hereBy[here[i].Ref] = &here[i]
	}
	latestBy := map[uuid.UUID]*CompileRun{}
	for i := range latest {
		latestBy[latest[i].TargetID] = &latest[i]
	}
	targetBy := map[uuid.UUID]*Target{}
	for _, t := range targets {
		targetBy[t.ID] = t
	}
	held := map[string]bool{}
	for _, f := range s.Files {
		run := hereBy[f.Compile]
		if run == nil {
			continue
		}
		t := targetBy[run.TargetID]
		if t == nil || t.Root != RootRepo {
			continue
		}
		out := slices.IndexFunc(run.Files, func(o CompiledOutput) bool { return o.Path == f.Path })
		if out < 0 || slices.ContainsFunc(v.Files, func(x StartedFile) bool { return x.Path == f.Path }) {
			continue
		}
		file := run.Files[out]
		sf := StartedFile{Path: f.Path, Here: run, Latest: latestBy[run.TargetID], HereRefs: file.Refs}
		if sf.Latest == nil {
			sf.Latest = run
		}
		if sf.HereRefs == nil {
			sf.HereRefs = run.Refs
		}
		if f.DriftSHA256 != "" && f.DriftSHA256 != file.DriftSHA256 {
			sf.HandEdited = !acceptedEdit(t, run.ID, f.Path, f.DriftSHA256)
		}
		for _, r := range sf.HereRefs {
			held[r] = true
		}
		v.Files = append(v.Files, sf)
	}

	// One entry per Forget: its primary memory (or its first, when the
	// primary was a note), with the rest it took. Those the files hold
	// first, then the latest others.
	type op struct {
		primary string
		with    []string
		at      time.Time
		held    bool
	}
	ops := map[uuid.UUID]*op{}
	var order []uuid.UUID
	for _, t := range tombs {
		v.ForgottenRefs[t.ref] = true
		o, ok := ops[t.op]
		if !ok {
			o = &op{at: t.at}
			ops[t.op] = o
			order = append(order, t.op)
		}
		if t.id == t.op {
			o.primary, o.at = t.ref, t.at
		} else {
			o.with = append(o.with, t.ref)
		}
		o.held = o.held || held[t.ref]
	}
	var inFiles, since []StartForget
	for _, id := range order {
		o := ops[id]
		if o.primary == "" && len(o.with) > 0 {
			slices.Sort(o.with)
			o.primary, o.with = o.with[0], o.with[1:]
		}
		slices.Sort(o.with)
		f := StartForget{Ref: o.primary, With: o.with, At: o.at, InFiles: o.held}
		if f.With == nil {
			f.With = []string{}
		}
		if o.held {
			inFiles = append(inFiles, f)
		} else {
			since = append(since, f)
		}
	}
	// Only those since the oldest compile named are news; a Forget a
	// latest compile still cites (before its recompile) is filtered, not told.
	oldest := time.Time{}
	for _, f := range v.Files {
		if oldest.IsZero() || f.Here.CompiledAt.Before(oldest) {
			oldest = f.Here.CompiledAt
		}
	}
	news := since[:0]
	for _, f := range since {
		if !oldest.IsZero() && !f.At.Before(oldest) {
			news = append(news, f)
		}
	}
	byAt := func(a, b StartForget) int { return b.At.Compare(a.At) }
	slices.SortStableFunc(inFiles, byAt)
	slices.SortStableFunc(news, byAt)
	if len(inFiles) > MaxStartHeld {
		inFiles = inFiles[:MaxStartHeld]
	}
	if len(news) > MaxStartForgets {
		news = news[:MaxStartForgets]
	}
	v.Forgotten = append(inFiles, news...)

	for i := range v.Gates {
		v.Gates[i].Question = shorten(v.Gates[i].Question, MaxStartQuestion)
	}
}

// acceptedEdit reports whether hash is a hand edit Memax accepted as the
// file's baseline after run delivered it (a pull or an overwrite).
func acceptedEdit(t *Target, run uuid.UUID, path, hash string) bool {
	if t.Delivered == nil || t.Delivered.CompileID == nil || *t.Delivered.CompileID != run {
		return false
	}
	return slices.ContainsFunc(t.Delivered.Files, func(f DeliveredFile) bool { return f.Path == path && f.SHA256 == hash })
}

// shorten cuts s to at most n characters, with an ellipsis.
func shorten(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
