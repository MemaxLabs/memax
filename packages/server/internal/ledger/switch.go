package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Switch to V2 (plan 25 §10, Phase 2 epic 2.8).
//
// Each space moves from V1 to the V2 record on its own, when its owner
// asks, after a preview of what moves (SwitchStatus, the dry run). The
// switch is one resumable run of steps, each idempotent, each through the
// ledger with its receipts:
//
//  1. space: the kind (a V1 team hub may become a project space, before
//     it has any V2 record) and the repository it compiles for. Roles map
//     as ResolveUserScope reads them (owner → owner; admin → member who can
//     forget; contributor → member; viewer → viewer), so no V1 row changes.
//  2. notes: every V1 memory (but V1's onboarding seeds) is numbered as a
//     note (N-), with its disposition (notes.go): a person's own short
//     memory is a bulk-keep candidate, an agent's is left for Dream to
//     fold, an archived or credential-bearing one stays a note.
//  3. personas (personal spaces): each persona becomes a note for Dream.
//  4. configs: the owner's synced agent files that belong to the space
//     become notes (import sources Dream folds), and the compile targets
//     they stand for are added (AGENTS.md, the CLAUDE.md shim, …). V1's
//     two-way config sync stops for those files while the space is on V2
//     (ConfigBelongs; the /v1 sync handler asks).
//  5. candidates: the person's own V1 memories go up as one import
//     (origin v1; at most MaxImportItems each), through the import
//     conflict check, so ReviewImport "From V1" shows disagreements first
//     and keeps the rest in one go. Nothing is kept by the switch.
//  6. agents: every V1 API key and OAuth grant of the space's members that
//     reaches it is connected to it at Propose (Read when it can't write),
//     as the V1 backfill does, and every agent connected to the space is
//     told, once, in its next MCP response, that its autonomy changed.
//  7. gates: a decision still waiting on V1's board moves to the V2 record,
//     asked by the agent that asked it (when that agent is connected).
//  8. switch: hubs.v2_enabled_at is set, with a switched receipt, and MCP
//     serves the space through the ledger from then on.
//
// V1 Dream runs stay as read-only history (v2.v1_dream_runs), and the V1
// plan is recorded on the switch (grandfathered, D9). No V1 row is changed
// or deleted. SwitchBack clears v2_enabled_at (switched_back receipt), and
// V1 behaves exactly as before; switching again numbers only what V1
// gained meanwhile.
//
// A space with candidates to import switches in the River job
// space_switch; anything smaller switches within the request. A failed step
// leaves the run failed at that step; the job's retries, or the person
// asking again, resume it there.

// The switch's states, as SpaceSwitch reports them.
const (
	SwitchStateV1       = "v1"       // on V1, never switched
	SwitchStateRunning  = "running"  // switching
	SwitchStateSwitched = "switched" // on V2
	SwitchStateFailed   = "failed"   // a step failed; asking again resumes it
	SwitchStateOff      = "off"      // switched back to V1
)

// The switch's steps, in order.
const (
	SwitchStepSpace      = "space"
	SwitchStepNotes      = "notes"
	SwitchStepPersonas   = "personas"
	SwitchStepConfigs    = "configs"
	SwitchStepCandidates = "candidates"
	SwitchStepAgents     = "agents"
	SwitchStepGates      = "gates"
	SwitchStepSwitch     = "switch"
	SwitchStepDone       = "done"
)

// SwitchSteps lists the steps in the order they run.
var SwitchSteps = []string{SwitchStepSpace, SwitchStepNotes, SwitchStepPersonas, SwitchStepConfigs, SwitchStepCandidates, SwitchStepAgents, SwitchStepGates, SwitchStepSwitch}

// The switch's receipt verbs (migration 048), on the space's own stream.
const (
	ActionNoted        Action = "noted"         // the switch numbered V1 content as notes
	ActionSwitched     Action = "switched"      // the space moved to the V2 record
	ActionSwitchedBack Action = "switched_back" // the space went back to V1
)

// ObjectNote is the receipts' object_kind for a note (its Forget).
const ObjectNote = "note"

// QueueSwitch is the River queue the switch runs on.
const QueueSwitch = "switch"

// SpaceSwitchArgs is the River job that runs a space's switch.
type SpaceSwitchArgs struct {
	SpaceID uuid.UUID `json:"space_id"`
}

// Kind implements river.JobArgs.
func (SpaceSwitchArgs) Kind() string { return "space_switch" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (SpaceSwitchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueSwitch,
		MaxAttempts: 5,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
			rivertype.JobStateScheduled, rivertype.JobStateRetryable,
		}},
	}
}

// SwitchOptions are what a person may choose when they switch a space.
type SwitchOptions struct {
	// Kind is the kind the space switches as: a V1 team hub may switch as a
	// project space while it has no V2 record. Empty keeps its kind.
	Kind policy.SpaceKind
	// Repository is the repository it compiles for ("owner/name"); nil
	// keeps it (the preview suggests one from the V1 memories).
	Repository *string
	// Key is the request's Idempotency-Key.
	Key string
}

// SwitchMember is a person in the space, with their V1 role and its V2
// mapping.
type SwitchMember struct {
	PersonID  uuid.UUID   `json:"person_id"`
	Name      string      `json:"name"`
	V1Role    string      `json:"v1_role"`
	Role      policy.Role `json:"role"`
	CanForget bool        `json:"can_forget"`
}

// SwitchNotes counts what the space's V1 content becomes.
type SwitchNotes struct {
	// Total counts the V1 memories that become notes; Person and Agent split
	// them by who wrote them.
	Total  int `json:"total"`
	Person int `json:"person"`
	Agent  int `json:"agent"`
	// Candidates are offered for bulk keep; Fold wait for Dream; Kept stay
	// notes only.
	Candidates int `json:"candidates"`
	Fold       int `json:"fold"`
	Kept       int `json:"kept"`
	// Why a person's notes aren't candidates.
	Long     int `json:"long"`
	Secret   int `json:"secret"`
	Archived int `json:"archived"`
	Format   int `json:"format"`
	External int `json:"external"`
	// Seeds counts V1's onboarding memories, left out.
	Seeds int `json:"seeds"`
}

// SwitchConfig is a V1 agent file that belongs to the space.
type SwitchConfig struct {
	Path    string       `json:"path"`
	Agent   string       `json:"agent"`
	Scope   string       `json:"scope"`
	Targets []TargetKind `json:"targets"`
}

// SwitchAgent is a V1 credential the switch connects (or found connected).
type SwitchAgent struct {
	Credential  CredentialKind  `json:"credential"`
	Name        string          `json:"name"`
	Agent       AgentKind       `json:"agent"`
	PersonID    uuid.UUID       `json:"person_id"`
	Autonomy    policy.Autonomy `json:"autonomy"`
	Connected   bool            `json:"connected"`
	CanWrite    bool            `json:"-"`
	credential  v1Credential
	connection  *Connection
	connectedAt policy.Autonomy
}

// SwitchPreview is what a switch would do, read fresh from V1: the dry run.
type SwitchPreview struct {
	Kind                policy.SpaceKind   `json:"kind"`
	Kinds               []policy.SpaceKind `json:"kinds"`
	Repository          string             `json:"repository,omitempty"`
	SuggestedRepository string             `json:"suggested_repository,omitempty"`
	Members             []SwitchMember     `json:"members"`
	Notes               SwitchNotes        `json:"notes"`
	Personas            int                `json:"personas"`
	Configs             []SwitchConfig     `json:"configs"`
	Targets             []TargetKind       `json:"targets"`
	Agents              []SwitchAgent      `json:"agents"`
	Gates               int                `json:"gates"`
	DreamRuns           int                `json:"dream_runs"`
	Plan                string             `json:"plan,omitempty"`
	// Empty: nothing to import, so it switches within the request.
	Empty bool `json:"empty"`
}

// SwitchProgress is what the steps did.
type SwitchProgress struct {
	Notes      int          `json:"notes"`
	Personas   int          `json:"personas"`
	Configs    int          `json:"configs"`
	Targets    []TargetKind `json:"targets"`
	Proposed   int          `json:"proposed"`
	Folded     int          `json:"folded"`
	Existing   int          `json:"existing"`
	Refused    int          `json:"refused"`
	Imports    []uuid.UUID  `json:"imports"`
	Connected  int          `json:"connected"`
	Already    int          `json:"already_connected"`
	Notified   int          `json:"notified"`
	GatesMoved int          `json:"gates_moved"`
	GatesLeft  int          `json:"gates_left"`
}

// SpaceSwitch is where a space's switch stands.
type SpaceSwitch struct {
	Space    Space          `json:"space"`
	State    string         `json:"state"`
	Step     string         `json:"step"`
	Preview  SwitchPreview  `json:"preview"`
	Progress SwitchProgress `json:"progress"`
	// ImportID is the V1 import: ReviewImport "From V1".
	ImportID       *uuid.UUID `json:"import_id,omitempty"`
	Error          string     `json:"error,omitempty"`
	Attempts       int        `json:"attempts"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	SwitchedAt     *time.Time `json:"switched_at,omitempty"`
	SwitchedBackAt *time.Time `json:"switched_back_at,omitempty"`
	// Background: the switch continues in the background; read it again
	// with SwitchStatus.
	Background bool `json:"background"`
}

// switchRun is a v2.space_switches row.
type switchRun struct {
	SpaceID        uuid.UUID
	TenantID       uuid.UUID
	State, Step    string
	RequestedBy    uuid.UUID
	Key            string
	Options        switchRunOptions
	Preview        SwitchPreview
	Progress       SwitchProgress
	ImportID       *uuid.UUID
	Plan           string
	Attempts       int
	Error          string
	StartedAt      time.Time
	SwitchedAt     *time.Time
	SwitchedBackAt *time.Time
}

type switchRunOptions struct {
	Kind       policy.SpaceKind `json:"kind,omitempty"`
	Repository *string          `json:"repository,omitempty"`
	Via        policy.Via       `json:"via,omitempty"`
}

// hubRow is a hub as the switch reads it (login role).
type hubRow struct {
	ID, TenantID, OwnerID uuid.UUID
	Slug, Name            string
	Kind                  policy.SpaceKind
	Repository            string
	V2EnabledAt           *time.Time
}

func readHub(ctx context.Context, db Querier, id uuid.UUID) (*hubRow, error) {
	rows, err := db.Query(ctx, `
		SELECT id, tenant_id, owner_id, slug, name, space_kind, COALESCE(repository, ''), v2_enabled_at
		  FROM public.hubs WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("ledger: read space: %w", err)
	}
	hubs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (hubRow, error) {
		var h hubRow
		err := r.Scan(&h.ID, &h.TenantID, &h.OwnerID, &h.Slug, &h.Name, &h.Kind, &h.Repository, &h.V2EnabledAt)
		return h, err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: read space: %w", err)
	}
	if len(hubs) == 0 {
		return nil, ErrNotFound
	}
	return &hubs[0], nil
}

func (h *hubRow) space(role policy.Role) Space {
	return Space{ID: h.ID, TenantID: h.TenantID, Slug: h.Slug, Name: h.Name, Kind: h.Kind, Role: role,
		Repository: h.Repository, V2EnabledAt: h.V2EnabledAt}
}

// SpaceKindError: the space can't switch as the kind asked.
type SpaceKindError struct {
	Slug    string
	Message string
}

func (e *SpaceKindError) Error() string { return e.Message }

// ---------------------------------------------------------------------
// Reading where a switch stands, and the preview
// ---------------------------------------------------------------------

// SwitchStatus says where a space's switch stands, with a fresh preview of
// what switching (again) moves: the dry run. Any member may read it.
func (l *Ledger) SwitchStatus(ctx context.Context, scope Scope, spaceID uuid.UUID) (*SpaceSwitch, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	g, ok := scope.Grant(spaceID)
	if !ok {
		return nil, ErrNotFound
	}
	hub, err := readHub(ctx, l.pool, spaceID)
	if err != nil {
		return nil, err
	}
	run, err := l.readSwitchRun(ctx, Scope{PersonID: scope.PersonID, Spaces: []SpaceGrant{g}}, spaceID)
	if err != nil {
		return nil, err
	}
	pv, err := l.switchPreview(ctx, hub, run)
	if err != nil {
		return nil, err
	}
	return switchView(hub, g.Role, run, pv), nil
}

func switchView(hub *hubRow, role policy.Role, run *switchRun, pv SwitchPreview) *SpaceSwitch {
	out := &SpaceSwitch{Space: hub.space(role), Preview: pv, Step: SwitchStepSpace, State: SwitchStateV1}
	out.Progress.Targets, out.Progress.Imports = []TargetKind{}, []uuid.UUID{}
	if hub.V2EnabledAt != nil {
		out.State, out.Step = SwitchStateSwitched, SwitchStepDone
	}
	if run != nil {
		out.State, out.Step, out.Progress, out.ImportID = run.State, run.Step, run.Progress, run.ImportID
		out.Error, out.Attempts, out.SwitchedAt, out.SwitchedBackAt = run.Error, run.Attempts, run.SwitchedAt, run.SwitchedBackAt
		started := run.StartedAt
		out.StartedAt = &started
		out.Progress.Targets = nonNilSlice(out.Progress.Targets)
		out.Progress.Imports = nonNilSlice(out.Progress.Imports)
		// A space switched back by hand (cmd/v2-switch-space) reads as on V1.
		if hub.V2EnabledAt == nil && run.State == SwitchStateSwitched {
			out.State = SwitchStateOff
		}
	}
	return out
}

func (l *Ledger) readSwitchRun(ctx context.Context, scope Scope, spaceID uuid.UUID) (*switchRun, error) {
	var run *switchRun
	err := l.Read(ctx, scope.Narrow(spaceID), func(tx pgx.Tx) error {
		var err error
		run, err = loadSwitchRun(ctx, tx, spaceID, false)
		return err
	})
	return run, err
}

func loadSwitchRun(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, lock bool) (*switchRun, error) {
	q := `SELECT space_id, tenant_id, state, step, requested_by, idempotency_key, options, preview, progress, import_id,
	             COALESCE(plan, ''), attempts, COALESCE(error, ''), started_at, switched_at, switched_back_at
	        FROM v2.space_switches WHERE space_id = $1`
	if lock {
		q += ` FOR UPDATE`
	}
	var r switchRun
	var opts, preview, progress []byte
	err := tx.QueryRow(ctx, q, spaceID).Scan(&r.SpaceID, &r.TenantID, &r.State, &r.Step, &r.RequestedBy, &r.Key, &opts,
		&preview, &progress, &r.ImportID, &r.Plan, &r.Attempts, &r.Error, &r.StartedAt, &r.SwitchedAt, &r.SwitchedBackAt)
	if errNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: read the switch: %w", err)
	}
	if err := json.Unmarshal(opts, &r.Options); err != nil {
		return nil, fmt.Errorf("ledger: read the switch: %w", err)
	}
	if err := json.Unmarshal(preview, &r.Preview); err != nil {
		return nil, fmt.Errorf("ledger: read the switch: %w", err)
	}
	if err := json.Unmarshal(progress, &r.Progress); err != nil {
		return nil, fmt.Errorf("ledger: read the switch: %w", err)
	}
	return &r, nil
}

// switchPreview reads, from V1, what a switch would do.
func (l *Ledger) switchPreview(ctx context.Context, hub *hubRow, run *switchRun) (SwitchPreview, error) {
	pv := SwitchPreview{Kind: hub.Kind, Kinds: []policy.SpaceKind{hub.Kind}, Repository: hub.Repository,
		Members: []SwitchMember{}, Configs: []SwitchConfig{}, Targets: []TargetKind{}, Agents: []SwitchAgent{}}
	if hub.Kind == policy.SpaceTeam && hub.V2EnabledAt == nil {
		pv.Kinds = []policy.SpaceKind{policy.SpaceTeam, policy.SpaceProject}
	}
	if run != nil && run.Options.Kind != "" {
		pv.Kind = run.Options.Kind
	}
	facts, err := readV1HubFacts(ctx, l.pool, hub.ID)
	if err != nil {
		return pv, err
	}
	pv.Plan, pv.DreamRuns, pv.SuggestedRepository = facts.Plan, facts.DreamRuns, facts.Repo
	if pv.Repository == "" {
		pv.Repository = facts.Repo
	}
	if run != nil && run.Options.Repository != nil {
		pv.Repository = *run.Options.Repository
	}
	members, err := readV1Members(ctx, l.pool, hub.ID)
	if err != nil {
		return pv, err
	}
	for _, m := range members {
		pv.Members = append(pv.Members, SwitchMember{PersonID: m.PersonID, Name: m.Name, V1Role: m.V1Role, Role: m.Role, CanForget: m.CanForget})
	}
	mems, err := readV1Memories(ctx, l.pool, hub.ID, nil)
	if err != nil {
		return pv, err
	}
	personal := hub.Kind == policy.SpacePersonal
	for _, m := range mems {
		c := classifyV1(m, personal)
		pv.Notes.count(c.noteRow)
	}
	if err := l.pool.QueryRow(ctx, `
		SELECT count(*) FROM public.memories WHERE hub_id = $1 AND (source_kind = 'onboarding-seed' OR owner_id = $2)`,
		hub.ID, v1SystemUser).Scan(&pv.Notes.Seeds); err != nil {
		return pv, fmt.Errorf("ledger: read V1 memories: %w", err)
	}
	if personal {
		ps, err := readV1Personas(ctx, l.pool, hub.OwnerID)
		if err != nil {
			return pv, err
		}
		pv.Personas = len(ps)
	}
	configs, err := readV1Configs(ctx, l.pool, hub.OwnerID, pv.Kind, pv.Repository)
	if err != nil {
		return pv, err
	}
	for _, c := range configs {
		sc := SwitchConfig{Path: c.Path, Agent: c.Agent, Scope: c.Scope, Targets: []TargetKind{}}
		if pv.Kind != policy.SpacePersonal {
			sc.Targets = nonNilSlice(ConfigTargets(c.Path))
			for _, k := range sc.Targets {
				if !slices.Contains(pv.Targets, k) {
					pv.Targets = append(pv.Targets, k)
				}
			}
		}
		pv.Configs = append(pv.Configs, sc)
	}
	agents, err := l.switchAgents(ctx, hub, members)
	if err != nil {
		return pv, err
	}
	pv.Agents = nonNilSlice(agents)
	gates, err := readV1Gates(ctx, l.pool, hub.ID)
	if err != nil {
		return pv, err
	}
	pv.Gates = len(gates)
	pv.Empty = pv.Notes.Candidates == 0
	return pv, nil
}

func (n *SwitchNotes) count(r noteRow) {
	n.Total++
	if r.AuthorKind == "agent" {
		n.Agent++
	} else {
		n.Person++
	}
	switch r.Disposition {
	case NoteCandidate:
		n.Candidates++
	case NoteFold:
		n.Fold++
	case NoteOnly:
		n.Kept++
	}
	switch r.Hold {
	case HoldLong:
		n.Long++
	case HoldSecret:
		n.Secret++
	case HoldArchived:
		n.Archived++
	case HoldFormat:
		n.Format++
	case HoldExternal:
		n.External++
	}
}

// switchAgents lists the V1 credentials of the space's members that reach
// it, with the connection each has (if any) and the level it gets here.
func (l *Ledger) switchAgents(ctx context.Context, hub *hubRow, members []v1Member) ([]SwitchAgent, error) {
	ids := make([]uuid.UUID, len(members))
	for i, m := range members {
		ids[i] = m.PersonID
	}
	if len(ids) == 0 {
		return nil, nil
	}
	creds, err := v1Credentials(ctx, l.pool, ids)
	if err != nil {
		return nil, err
	}
	scopes := map[uuid.UUID]Scope{}
	var out []SwitchAgent
	for _, c := range creds {
		ms, ok := scopes[c.user]
		if !ok {
			if ms, err = ResolveUserScope(ctx, l.pool, c.user); err != nil {
				return nil, err
			}
			scopes[c.user] = ms
		}
		g, ok := ms.Grant(hub.ID)
		if !ok || (c.narrow() && !slices.Contains(c.hubs, hub.ID)) {
			continue
		}
		agent := AgentFromV1(c.agent)
		a := SwitchAgent{Credential: c.kind, Name: c.displayName(agent), Agent: agent, PersonID: c.user, CanWrite: c.canWrite,
			credential: c}
		a.Autonomy = quietAutonomy(g.Role, hub.Kind, c.canWrite)
		conn, err := l.ConnectionForCredential(ctx, ms, c.kind, c.id)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if conn != nil && conn.State != ConnectionDisconnected {
			a.connection = conn
			if level, here := conn.AutonomyIn(hub.ID); here {
				a.Connected, a.Autonomy, a.connectedAt = true, level, level
			}
		}
		out = append(out, a)
	}
	return out, nil
}

// quietAutonomy is the level the switch connects a V1 credential at: the
// space's default for new agents, never above Propose (what Memax may
// connect at without a person on the web), and Read for a credential that
// can't write.
func quietAutonomy(role policy.Role, kind policy.SpaceKind, canWrite bool) policy.Autonomy {
	if !canWrite {
		return policy.AutonomyRead
	}
	return policy.MinAutonomy(policy.AutonomyPropose, policy.DefaultAutonomy(role, policy.Space{Kind: kind}))
}

// ---------------------------------------------------------------------
// Starting, running and undoing a switch
// ---------------------------------------------------------------------

// StartSwitch switches a space to the V2 record, or resumes a switch that
// failed. Only the space's owner may, signed in. A space with nothing to
// import switches within the call; otherwise the River job continues it,
// and SpaceSwitch.Background says so. Asking again while it runs, or once
// it switched, changes nothing.
func (l *Ledger) StartSwitch(ctx context.Context, actor Actor, via policy.Via, scope Scope, spaceID uuid.UUID, opts SwitchOptions) (*SpaceSwitch, error) {
	return l.startSwitch(ctx, actor, via, scope, spaceID, opts, false)
}

// StartSwitchInline is StartSwitch that runs the whole switch in the call,
// whatever it imports (cmd/v2-switch-space).
func (l *Ledger) StartSwitchInline(ctx context.Context, actor Actor, via policy.Via, scope Scope, spaceID uuid.UUID, opts SwitchOptions) (*SpaceSwitch, error) {
	return l.startSwitch(ctx, actor, via, scope, spaceID, opts, true)
}

func (l *Ledger) startSwitch(ctx context.Context, actor Actor, via policy.Via, scope Scope, spaceID uuid.UUID, opts SwitchOptions, inline bool) (*SpaceSwitch, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	g, ok := scope.Grant(spaceID)
	if !ok {
		return nil, ErrNotFound
	}
	if d := policy.DecideSwitchSpace(policy.Actor{Kind: actor.Kind, Credential: actor.Credential, Role: g.Role}); d.Effect == policy.EffectRefuse {
		return nil, &SpaceRefusedError{Decision: d}
	}
	if err := checkText("idempotency_key", opts.Key, MaxKeyLength, true); err != nil {
		return nil, err
	}
	if opts.Kind != "" && opts.Kind != policy.SpaceProject && opts.Kind != policy.SpaceTeam && opts.Kind != policy.SpacePersonal {
		return nil, invalid("kind", "use project or team")
	}
	if opts.Repository != nil {
		r := strings.TrimSpace(*opts.Repository)
		if err := checkText("repository", r, MaxSpaceRepository, false); err != nil {
			return nil, err
		}
		opts.Repository = &r
	}
	hub, err := readHub(ctx, l.pool, spaceID)
	if err != nil {
		return nil, err
	}
	run, err := l.readSwitchRun(ctx, scope, spaceID)
	if err != nil {
		return nil, err
	}
	switch {
	case hub.V2EnabledAt != nil:
		pv, err := l.switchPreview(ctx, hub, run)
		if err != nil {
			return nil, err
		}
		return switchView(hub, g.Role, run, pv), nil
	case run != nil && run.State == SwitchStateRunning:
		// One switch at a time: the one running answers, whoever asked.
		pv, err := l.switchPreview(ctx, hub, run)
		if err != nil {
			return nil, err
		}
		out := switchView(hub, g.Role, run, pv)
		out.Background = true
		return out, nil
	}

	// Step 1, the space: its kind and repository, before it has a record.
	if err := l.setSpaceColumns(ctx, hub, opts); err != nil {
		return nil, err
	}
	if hub, err = readHub(ctx, l.pool, spaceID); err != nil {
		return nil, err
	}
	// The tenant may have changed with the kind: resolve the scope again.
	ps, err := ResolveUserScope(ctx, l.pool, actor.ID)
	if err != nil {
		return nil, err
	}
	if g, ok = ps.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	pv, err := l.switchPreview(ctx, hub, nil)
	if err != nil {
		return nil, err
	}
	background := !inline && l.inserter != nil && !pv.Empty
	resume := run != nil && run.State == SwitchStateFailed
	ro := switchRunOptions{Kind: hub.Kind, Repository: opts.Repository, Via: via}

	tx, loginRole, err := l.begin(ctx, ps.Narrow(spaceID), pgx.ReadWrite)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Two requests at once: the second waits here, then finds it running.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('v2.space_switch:' || $1::text, 0))`, spaceID); err != nil {
		return nil, fmt.Errorf("ledger: start the switch: %w", err)
	}
	cur, err := loadSwitchRun(ctx, tx, spaceID, true)
	if err != nil {
		return nil, err
	}
	if cur != nil && cur.State == SwitchStateRunning {
		_ = tx.Rollback(ctx)
		out := switchView(hub, g.Role, cur, pv)
		out.Background = true
		return out, nil
	}
	optsJSON, _ := json.Marshal(ro)
	pvJSON, err := json.Marshal(pv)
	if err != nil {
		return nil, err
	}
	step := SwitchStepNotes
	if resume && cur != nil {
		step = cur.Step
	}
	if cur == nil {
		_, err = tx.Exec(ctx, `
			INSERT INTO v2.space_switches (space_id, tenant_id, state, step, requested_by, idempotency_key, options, preview,
			                               plan, attempts)
			VALUES ($1, $2, 'running', $3, $4, $5, $6, $7, $8, 1)`,
			spaceID, hub.TenantID, step, actor.ID, opts.Key, optsJSON, pvJSON, nullText(pv.Plan))
	} else {
		// Again after a failure (resume at its step) or after switching back
		// (from the start: numbering and the import skip what is done).
		progress := cur.Progress
		if !resume {
			progress = SwitchProgress{}
		}
		progressJSON, _ := json.Marshal(progress)
		_, err = tx.Exec(ctx, `
			UPDATE v2.space_switches
			   SET tenant_id = $2, state = 'running', step = $3, requested_by = $4, idempotency_key = $5, options = $6,
			       preview = $7, progress = $8, plan = $9, attempts = attempts + 1, error = NULL,
			       started_at = CASE WHEN $10 THEN started_at ELSE now() END, updated_at = now()
			 WHERE space_id = $1`,
			spaceID, hub.TenantID, step, actor.ID, opts.Key, optsJSON, pvJSON, progressJSON, nullText(pv.Plan), resume)
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: start the switch: %w", err)
	}
	if background {
		w := &writer{tx: tx, meta: &Meta{Actor: actor, Scope: ps.Narrow(spaceID), Via: via}, inserter: l.inserter, loginRole: loginRole}
		w.jobs = append(w.jobs, river.InsertManyParams{Args: SpaceSwitchArgs{SpaceID: spaceID}})
		if err := w.flush(ctx); err != nil {
			return nil, mapDBError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapDBError(fmt.Errorf("ledger: start the switch: %w", err))
	}
	l.log.Info("ledger: switch started", "space_id", spaceID.String(), "slug", hub.Slug, "background", background,
		"resume", resume, "candidates", pv.Notes.Candidates, "notes", pv.Notes.Total)
	if background {
		out, err := l.SwitchStatus(ctx, ps, spaceID)
		if out != nil {
			out.Background = true
		}
		return out, err
	}
	if _, err := l.RunSwitch(ctx, spaceID); err != nil {
		return nil, err
	}
	return l.SwitchStatus(ctx, ps, spaceID)
}

// setSpaceColumns applies the kind and repository a switch asked for, as
// the login role (public.hubs is V1's table). A team hub becomes a project
// space only while it has no V2 record: the foreign keys to (space_id,
// tenant_id) refuse it otherwise.
func (l *Ledger) setSpaceColumns(ctx context.Context, hub *hubRow, opts SwitchOptions) error {
	kind := hub.Kind
	if opts.Kind != "" && opts.Kind != hub.Kind {
		if hub.Kind != policy.SpaceTeam || opts.Kind != policy.SpaceProject {
			return &SpaceKindError{Slug: hub.Slug, Message: fmt.Sprintf(
				"%s is a %s space and switches as one; only a V1 team hub can become a project space.", hub.Slug, hub.Kind)}
		}
		kind = opts.Kind
	}
	repo := hub.Repository
	if opts.Repository != nil {
		repo = *opts.Repository
	}
	if kind == hub.Kind && repo == hub.Repository {
		return nil
	}
	_, err := l.pool.Exec(ctx, `UPDATE public.hubs SET space_kind = $2, repository = $3, updated_at = now() WHERE id = $1`,
		hub.ID, string(kind), nullText(repo))
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23503" {
		return &SpaceKindError{Slug: hub.Slug, Message: fmt.Sprintf(
			"%s already has a V2 record, so it keeps its kind. Switch it as a team space.", hub.Slug)}
	}
	if err != nil {
		return fmt.Errorf("ledger: set the space's kind and repository: %w", err)
	}
	return nil
}

// RunSwitch runs a space's switch from the step it stands at to the end
// (the space_switch job, or StartSwitch for a small space). It acts as the
// person who asked, who must still own the space. A step that fails leaves
// the run failed there, and the error is returned (the job retries).
func (l *Ledger) RunSwitch(ctx context.Context, spaceID uuid.UUID) (*SpaceSwitch, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	sys, err := l.SpaceScope(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	run, err := l.readSwitchRun(ctx, sys, spaceID)
	if err != nil || run == nil {
		return nil, err
	}
	if run.State != SwitchStateRunning && run.State != SwitchStateFailed {
		return nil, nil
	}
	ps, err := ResolveUserScope(ctx, l.pool, run.RequestedBy)
	if err != nil {
		return nil, err
	}
	g, ok := ps.Grant(spaceID)
	if !ok || g.Role != policy.RoleOwner {
		l.failSwitch(ctx, sys, spaceID, run.Step, policy.CodeSwitchByOwner)
		return nil, &SpaceRefusedError{Decision: policy.DecideSwitchSpace(policy.Actor{Kind: policy.ActorPerson, Role: g.Role})}
	}
	r := &switchRunner{l: l, run: run, scope: ps.Narrow(spaceID), grant: g,
		actor: Actor{Kind: policy.ActorPerson, ID: run.RequestedBy, Credential: policy.CredentialSession},
		via:   run.Options.Via}
	if !r.via.Valid() {
		r.via = policy.ViaWeb
	}
	if r.hub, err = readHub(ctx, l.pool, spaceID); err != nil {
		return nil, err
	}
	if run.State == SwitchStateFailed {
		if err := l.updateSwitchRun(ctx, r.scope, spaceID, `state = 'running', error = NULL`); err != nil {
			return nil, err
		}
	}
	start := slices.Index(SwitchSteps, run.Step)
	if start < 0 {
		start = len(SwitchSteps)
	}
	for _, step := range SwitchSteps[start:] {
		if step == SwitchStepSpace {
			continue // StartSwitch set the kind and repository
		}
		if err := r.do(ctx, step); err != nil {
			code := "step_failed"
			var refused *SpaceRefusedError
			if errors.As(err, &refused) {
				code = refused.Decision.Code
			}
			l.failSwitch(ctx, r.scope, spaceID, step, code)
			l.log.Error("ledger: switch step failed", "space_id", spaceID.String(), "step", step, "error", err)
			return nil, fmt.Errorf("ledger: switch %s: %w", step, err)
		}
		next := SwitchStepDone
		if i := slices.Index(SwitchSteps, step); i+1 < len(SwitchSteps) {
			next = SwitchSteps[i+1]
		}
		if step != SwitchStepSwitch {
			progress, _ := json.Marshal(r.run.Progress)
			if err := l.updateSwitchRun(ctx, r.scope, spaceID, `step = $2, progress = $3, import_id = $4`, next, progress,
				r.run.ImportID); err != nil {
				return nil, err
			}
		}
	}
	l.log.Info("ledger: space switched to V2", "space_id", spaceID.String(), "slug", r.hub.Slug,
		"notes", r.run.Progress.Notes, "proposed", r.run.Progress.Proposed, "connected", r.run.Progress.Connected)
	return l.SwitchStatus(ctx, ps, spaceID)
}

// updateSwitchRun changes the run's bookkeeping (no receipt: the run is
// not the record).
func (l *Ledger) updateSwitchRun(ctx context.Context, scope Scope, spaceID uuid.UUID, set string, args ...any) error {
	return l.meter(ctx, scope, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE v2.space_switches SET `+set+`, updated_at = now() WHERE space_id = $1`,
			append([]any{spaceID}, args...)...)
		return err
	})
}

func (l *Ledger) failSwitch(ctx context.Context, scope Scope, spaceID uuid.UUID, step, code string) {
	if err := l.updateSwitchRun(ctx, scope, spaceID, `state = 'failed', step = $2, error = $3`, step, truncateRunes(code, 64)); err != nil {
		l.log.Error("ledger: record the switch's failure", "space_id", spaceID.String(), "error", err)
	}
}

// SwitchBack puts a space back on V1 (its owner, signed in): MCP and every
// surface serve it as V1 again, and its V1 rows, never changed by the
// switch, answer exactly as before. Its V2 record stays, receipted, for a
// later switch.
func (l *Ledger) SwitchBack(ctx context.Context, actor Actor, via policy.Via, scope Scope, spaceID uuid.UUID, key string) (*SpaceSwitch, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	g, ok := scope.Grant(spaceID)
	if !ok {
		return nil, ErrNotFound
	}
	if d := policy.DecideSwitchSpace(policy.Actor{Kind: actor.Kind, Credential: actor.Credential, Role: g.Role}); d.Effect == policy.EffectRefuse {
		return nil, &SpaceRefusedError{Decision: d}
	}
	if err := checkText("idempotency_key", key, MaxKeyLength, true); err != nil {
		return nil, err
	}
	hub, err := readHub(ctx, l.pool, spaceID)
	if err != nil {
		return nil, err
	}
	if hub.V2EnabledAt != nil {
		if err := l.switchBack(ctx, actor, via, scope.Narrow(spaceID), hub, key); err != nil {
			return nil, err
		}
	}
	return l.SwitchStatus(ctx, scope, spaceID)
}

func (l *Ledger) switchBack(ctx context.Context, actor Actor, via policy.Via, scope Scope, hub *hubRow, key string) error {
	tx, loginRole, err := l.begin(ctx, scope, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sp, err := loadSpace(ctx, tx, hub.ID)
	if err != nil {
		return err
	}
	w := &writer{tx: tx, meta: &Meta{Actor: actor, Scope: scope, Via: via, OccurredAt: l.now().UTC().Truncate(time.Microsecond)}}
	stream, err := nextSpaceStreamVersion(ctx, tx, sp.ID)
	if err != nil {
		return err
	}
	rc := w.objectReceipt(sp, ObjectSpace, sp.ID, SpaceObjectRef, ActionSwitchedBack, stream, "")
	if err := insertReceipt(ctx, tx, &rc); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO v2.space_switches (space_id, tenant_id, state, step, requested_by, idempotency_key, switched_at, switched_back_at)
		VALUES ($1, $2, 'off', 'done', $3, $4, $5, now())
		ON CONFLICT (space_id) DO UPDATE SET state = 'off', switched_back_at = now(), error = NULL, updated_at = now()`,
		sp.ID, sp.TenantID, actor.ID, key, hub.V2EnabledAt); err != nil {
		return fmt.Errorf("ledger: switch back: %w", err)
	}
	if err := asLoginRole(ctx, tx, loginRole, func() error {
		_, err := tx.Exec(ctx, `UPDATE public.hubs SET v2_enabled_at = NULL WHERE id = $1`, sp.ID)
		return err
	}); err != nil {
		return fmt.Errorf("ledger: switch back: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBError(fmt.Errorf("ledger: switch back: %w", err))
	}
	l.log.Info("ledger: space switched back to V1", "space_id", sp.ID.String(), "slug", hub.Slug)
	return nil
}

// ---------------------------------------------------------------------
// The steps
// ---------------------------------------------------------------------

type switchRunner struct {
	l     *Ledger
	run   *switchRun
	hub   *hubRow
	scope Scope
	grant SpaceGrant
	actor Actor
	via   policy.Via
}

func (r *switchRunner) do(ctx context.Context, step string) error {
	switch step {
	case SwitchStepNotes:
		return r.notes(ctx)
	case SwitchStepPersonas:
		return r.personas(ctx)
	case SwitchStepConfigs:
		return r.configs(ctx)
	case SwitchStepCandidates:
		return r.candidates(ctx)
	case SwitchStepAgents:
		return r.agents(ctx)
	case SwitchStepGates:
		return r.gates(ctx)
	case SwitchStepSwitch:
		return r.switchOn(ctx)
	}
	return nil
}

// meta is the person's command envelope for one step's command.
func (r *switchRunner) meta(key, reason string) Meta {
	return Meta{Actor: r.actor, Scope: r.scope, Via: r.via, IdempotencyKey: key, Reason: reason}
}

// numbered lists the V1 rows of an origin already numbered in the space.
func (r *switchRunner) numbered(ctx context.Context, origin NoteOrigin) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	err := r.l.Read(ctx, r.scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT v1_id FROM v2.note_refs WHERE space_id = $1 AND origin = $2`, r.hub.ID, string(origin))
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		for _, id := range ids {
			out[id] = true
		}
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: read numbered notes: %w", err)
	}
	return out, nil
}

// number writes rows as notes beside one noted receipt on the space's
// stream, in one transaction.
func (r *switchRunner) number(ctx context.Context, rows []noteRow, what string) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	tx, _, err := r.l.begin(ctx, r.scope, pgx.ReadWrite)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sp, err := loadSpace(ctx, tx, r.hub.ID)
	if err != nil {
		return 0, err
	}
	w := &writer{tx: tx, meta: &Meta{Actor: r.actor, Scope: r.scope, Via: r.via, OccurredAt: r.l.now().UTC().Truncate(time.Microsecond)}}
	stream, err := nextSpaceStreamVersion(ctx, tx, sp.ID)
	if err != nil {
		return 0, err
	}
	rc := w.objectReceipt(sp, ObjectSpace, sp.ID, SpaceObjectRef, ActionNoted, stream,
		fmt.Sprintf("Switching to V2: %d %s became notes.", len(rows), what))
	if err := insertReceipt(ctx, tx, &rc); err != nil {
		return 0, err
	}
	n, err := numberNotes(ctx, tx, sp, rc.ID, rows)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, mapDBError(fmt.Errorf("ledger: number notes: %w", err))
	}
	return n, nil
}

// notes numbers the space's V1 memories.
func (r *switchRunner) notes(ctx context.Context) error {
	done, err := r.numbered(ctx, NoteFromMemory)
	if err != nil {
		return err
	}
	mems, err := readV1Memories(ctx, r.l.pool, r.hub.ID, done)
	if err != nil {
		return err
	}
	rows := make([]noteRow, len(mems))
	for i, m := range mems {
		rows[i] = classifyV1(m, r.hub.Kind == policy.SpacePersonal).noteRow
	}
	n, err := r.number(ctx, rows, "V1 memories")
	r.run.Progress.Notes += n
	return err
}

// personas numbers the owner's V1 personas, in the personal space.
func (r *switchRunner) personas(ctx context.Context) error {
	if r.hub.Kind != policy.SpacePersonal {
		return nil
	}
	done, err := r.numbered(ctx, NoteFromPersona)
	if err != nil {
		return err
	}
	ps, err := readV1Personas(ctx, r.l.pool, r.hub.OwnerID)
	if err != nil {
		return err
	}
	var rows []noteRow
	for _, p := range ps {
		if !done[p.ID] {
			rows = append(rows, personaNote(p))
		}
	}
	n, err := r.number(ctx, rows, "personas")
	r.run.Progress.Personas += n
	return err
}

// configs numbers the owner's agent files that belong to the space, and
// adds the targets they stand for.
func (r *switchRunner) configs(ctx context.Context) error {
	done, err := r.numbered(ctx, NoteFromAgentConfig)
	if err != nil {
		return err
	}
	cs, err := readV1Configs(ctx, r.l.pool, r.hub.OwnerID, r.hub.Kind, r.hub.Repository)
	if err != nil {
		return err
	}
	var rows []noteRow
	var kinds []TargetKind
	for _, c := range cs {
		if !done[c.ID] {
			rows = append(rows, configNote(c))
		}
		if r.hub.Kind != policy.SpacePersonal {
			for _, k := range ConfigTargets(c.Path) {
				if !slices.Contains(kinds, k) {
					kinds = append(kinds, k)
				}
			}
		}
	}
	n, err := r.number(ctx, rows, "agent files")
	r.run.Progress.Configs += n
	if err != nil {
		return err
	}
	if len(kinds) == 0 {
		return nil
	}
	have := map[TargetKind]bool{}
	brief := false
	if err := r.l.Read(ctx, r.scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT kind FROM v2.targets WHERE space_id = $1`, r.hub.ID)
		if err != nil {
			return err
		}
		ks, err := pgx.CollectRows(rows, pgx.RowTo[TargetKind])
		for _, k := range ks {
			have[k] = true
		}
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM v2.briefs WHERE space_id = $1)`, r.hub.ID).Scan(&brief)
	}); err != nil {
		return fmt.Errorf("ledger: read targets: %w", err)
	}
	// A target compiles a Brief: a space without one starts an empty Brief,
	// as Brief's "Start the Brief" does. What a person keeps from the V1
	// import compiles into it.
	if !brief {
		summary := "What every agent working here should know."
		if r.hub.Repository != "" {
			summary = "What every agent working on " + r.hub.Repository + " should know."
		}
		res, err := r.l.Apply(ctx, &ReviseBrief{
			Meta:    r.meta("v1-switch:brief:"+r.hub.ID.String(), "The first Brief, when the space switched to V2."),
			SpaceID: r.hub.ID, Title: truncateRunes(r.hub.Name, MaxBriefTitleRunes), Summary: summary,
		})
		if err != nil {
			return err
		}
		if res.Outcome == OutcomeRefused {
			return &SpaceRefusedError{Decision: res.Policy}
		}
	}
	for _, k := range kinds {
		if have[k] {
			continue
		}
		res, err := r.l.Apply(ctx, &ConfigureTarget{
			Meta:    r.meta("v1-switch:target:"+r.hub.ID.String()+":"+string(k), "From the V1 agent files this space synced."),
			SpaceID: r.hub.ID, Kind: k,
		})
		if err != nil {
			return err
		}
		if res.Outcome == OutcomeRefused {
			return &SpaceRefusedError{Decision: res.Policy}
		}
		if !res.Replayed {
			r.run.Progress.Targets = append(r.run.Progress.Targets, k)
		}
	}
	return nil
}

// candidates imports the person's own V1 memories as one import (origin
// v1) per MaxImportItems, for bulk keep after the import conflict check.
func (r *switchRunner) candidates(ctx context.Context) error {
	type cand struct {
		id  uuid.UUID
		ref string
	}
	var cands []cand
	imported := map[string]bool{}
	if err := r.l.Read(ctx, r.scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT note_id, seq FROM v2.note_refs
			 WHERE space_id = $1 AND origin = 'memory' AND disposition = 'candidate' AND forgotten_at IS NULL
			 ORDER BY seq`, r.hub.ID)
		if err != nil {
			return err
		}
		cands, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (cand, error) {
			var c cand
			var seq int64
			err := row.Scan(&c.id, &seq)
			c.ref = FormatRef(PrefixNote, seq)
			return c, err
		})
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `
			SELECT i.item_key FROM v2.import_items i JOIN v2.imports m ON m.id = i.import_id AND m.space_id = i.space_id
			 WHERE i.space_id = $1 AND m.origin = 'v1'`, r.hub.ID)
		if err != nil {
			return err
		}
		keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, k := range keys {
			imported[k] = true
		}
		return err
	}); err != nil {
		return fmt.Errorf("ledger: read candidates: %w", err)
	}
	var todo []cand
	for _, c := range cands {
		if !imported[c.ref] {
			todo = append(todo, c)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(todo))
	for i, c := range todo {
		ids[i] = c.id
	}
	mems, err := readV1MemoriesByID(ctx, r.l.pool, r.hub.ID, ids)
	if err != nil {
		return err
	}
	personal := r.hub.Kind == policy.SpacePersonal
	var items []ImportItem
	for _, c := range todo {
		m, ok := mems[c.id]
		if !ok {
			continue // deleted in V1 meanwhile
		}
		cl := classifyV1(m, personal)
		if cl.Disposition != NoteCandidate {
			continue // changed in V1 meanwhile; Dream folds it
		}
		items = append(items, ImportItem{Key: c.ref, Ref: c.ref, Location: ImportV1,
			NewMemory: NewMemory{SpaceID: r.hub.ID, Statement: cl.Statement, Section: cl.Section, Kind: KindFact,
				Sources: []SourceInput{{Kind: SourceNote, Ref: c.ref, Locator: noteLocator(c.id), Trust: cl.Trust}}}})
	}
	for start, batch := 0, 0; start < len(items); start, batch = start+MaxImportItems, batch+1 {
		part := items[start:min(start+MaxImportItems, len(items))]
		key := fmt.Sprintf("v1-switch:%s:%d", r.run.Key, batch)
		if len(key) > MaxKeyLength {
			key = key[:MaxKeyLength]
		}
		meta := r.meta(key, "")
		res, err := r.l.importItems(ctx, meta, ImportRequest{Meta: meta, SpaceID: r.hub.ID, Client: "Memax V1", Items: part}, importOriginV1)
		if err != nil {
			return err
		}
		if res.Refused != nil {
			return &SpaceRefusedError{Decision: *res.Refused}
		}
		if !slices.Contains(r.run.Progress.Imports, res.Import.ID) {
			r.run.Progress.Imports = append(r.run.Progress.Imports, res.Import.ID)
		}
		if r.run.ImportID == nil {
			id := res.Import.ID
			r.run.ImportID = &id
		}
		for _, it := range res.Items {
			switch it.Outcome {
			case ImportProposed:
				r.run.Progress.Proposed++
			case ImportFolded:
				r.run.Progress.Folded++
			case ImportExisting:
				r.run.Progress.Existing++
			case ImportRefused:
				r.run.Progress.Refused++
			}
		}
	}
	return nil
}

// readV1MemoriesByID reads some of a hub's V1 memories (the candidates'
// words, for the import).
func readV1MemoriesByID(ctx context.Context, db Querier, hubID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]v1Memory, error) {
	all, err := readV1Memories(ctx, db, hubID, nil)
	if err != nil {
		return nil, err
	}
	want := map[uuid.UUID]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := map[uuid.UUID]v1Memory{}
	for _, m := range all {
		if want[m.ID] {
			out[m.ID] = m
		}
	}
	return out, nil
}

// agents connects the members' V1 credentials that reach the space, at
// Propose (Read when they can't write), and tells every agent connected to
// the space that it now proposes here.
func (r *switchRunner) agents(ctx context.Context) error {
	members, err := readV1Members(ctx, r.l.pool, r.hub.ID)
	if err != nil {
		return err
	}
	agents, err := r.l.switchAgents(ctx, r.hub, members)
	if err != nil {
		return err
	}
	scopes := map[uuid.UUID]Scope{}
	for _, a := range agents {
		if a.Connected {
			r.run.Progress.Already++
			continue
		}
		ms, ok := scopes[a.PersonID]
		if !ok {
			if ms, err = ResolveUserScope(ctx, r.l.pool, a.PersonID); err != nil {
				return err
			}
			ms = ms.Narrow(r.hub.ID)
			scopes[a.PersonID] = ms
		}
		meta := Meta{Actor: Actor{Kind: policy.ActorMemax, Name: "Memax"}, Scope: ms, Via: policy.ViaSystem,
			Reason: "Connected when the space switched to V2."}
		var res Result
		if a.connection == nil {
			meta.IdempotencyKey = "v1-switch:connect:" + string(a.Credential) + ":" + a.credential.id.String()
			res, err = r.l.Apply(ctx, &ConnectAgent{Meta: meta, Person: a.PersonID, Credential: a.Credential,
				CredentialID: a.credential.id, Agent: a.Agent, DisplayName: a.Name,
				Spaces: []SpaceAutonomy{{SpaceID: r.hub.ID}}, Cap: a.Autonomy})
		} else {
			meta.IdempotencyKey = "v1-switch:space:" + a.connection.ID.String()
			res, err = r.l.Apply(ctx, &SetAutonomy{Meta: meta, Connection: a.connection.ID, SpaceID: r.hub.ID, Autonomy: a.Autonomy})
		}
		switch {
		case errors.Is(err, ErrAlreadyConnected), errors.Is(err, ErrNotFound):
			// Connected meanwhile, or revoked since it was listed.
			continue
		case err != nil:
			return err
		case res.Outcome == OutcomeRefused:
			r.l.log.Warn("ledger: switch: agent not connected", "space_id", r.hub.ID.String(), "credential", string(a.Credential),
				"policy", res.Policy.Code)
			continue
		}
		r.run.Progress.Connected++
	}
	n, err := r.l.queueSwitchNotices(ctx, r.scope, r.hub.ID)
	r.run.Progress.Notified += n
	return err
}

// queueSwitchNotices tells every agent connected to the space, once, that
// the space is on V2 and what it may do there.
func (l *Ledger) queueSwitchNotices(ctx context.Context, scope Scope, spaceID uuid.UUID) (int, error) {
	var n int64
	err := l.meter(ctx, scope, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO v2.agent_notices (id, tenant_id, space_id, connection_id, person_id, kind, refs, read_it, autonomy)
			SELECT gen_random_uuid(), s.tenant_id, s.space_id, c.id, c.person_id, 'switched', '{}', false, s.autonomy
			  FROM v2.agent_connection_spaces s JOIN v2.agent_connections c ON c.id = s.connection_id
			 WHERE s.space_id = $1 AND c.state <> 'disconnected'
			ON CONFLICT (connection_id, space_id) WHERE kind = 'switched' AND delivered_at IS NULL DO NOTHING`, spaceID)
		n = tag.RowsAffected()
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("ledger: switch notices: %w", err)
	}
	return int(n), nil
}

// gates moves the decisions waiting on V1's board to the V2 record, asked
// by the agent that asked them, when it is connected to the space. The V1
// card stays (no V1 row changes); a decision whose agent isn't connected
// stays on V1's board.
func (r *switchRunner) gates(ctx context.Context) error {
	gates, err := readV1Gates(ctx, r.l.pool, r.hub.ID)
	if err != nil || len(gates) == 0 {
		return err
	}
	var conns []Connection
	if err := r.l.Read(ctx, r.scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT c.id FROM v2.agent_connection_spaces s JOIN v2.agent_connections c ON c.id = s.connection_id
			 WHERE s.space_id = $1 AND c.state = 'active' AND s.autonomy <> 'read'
			 ORDER BY (c.person_id = $2) DESC, c.created_at`, r.hub.ID, r.hub.OwnerID)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, id := range ids {
			c, err := loadConnection(ctx, tx, r.scope, id)
			if err != nil {
				return err
			}
			conns = append(conns, *c)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("ledger: read connections: %w", err)
	}
	for _, g := range gates {
		agent := AgentFromV1(g.Agent)
		i := slices.IndexFunc(conns, func(c Connection) bool { return c.Agent == agent })
		if i < 0 || g.Question == "" || len(g.Options) < 2 || len(g.Options) > 4 {
			r.run.Progress.GatesLeft++
			continue
		}
		conn := &conns[i]
		ms, err := ResolveUserScope(ctx, r.l.pool, conn.PersonID)
		if err != nil {
			return err
		}
		scope := ms.Narrow(r.hub.ID).WithConnection(conn, conn.MaxAutonomy)
		opts := make([]DecisionOption, len(g.Options))
		for j, o := range g.Options {
			opts[j] = DecisionOption{Label: truncateRunes(o, MaxSourceRefRunes)}
		}
		res, err := r.l.Apply(ctx, &RequestDecision{
			Meta: Meta{Actor: Actor{Kind: policy.ActorAgent, ID: conn.ID, Name: conn.DisplayName, Agent: string(conn.Agent),
				Credential: conn.Credential.Kind.Policy()}, Scope: scope, Via: policy.ViaSystem,
				IdempotencyKey: "v1-switch:gate:" + g.SlotID.String(), Reason: "Asked on V1's board; moved when the space switched to V2."},
			SpaceID: r.hub.ID, Question: truncateRunes(g.Question, 300), Context: truncateRunes(g.Context, 2000), Options: opts,
		})
		if err != nil && !errors.Is(err, ErrInvalid) {
			return err
		}
		if err != nil || res.Outcome == OutcomeRefused {
			r.run.Progress.GatesLeft++
			continue
		}
		r.run.Progress.GatesMoved++
	}
	return nil
}

// switchOn is the last step: the space moves to the V2 record, with a
// switched receipt, in one transaction.
func (r *switchRunner) switchOn(ctx context.Context) error {
	tx, loginRole, err := r.l.begin(ctx, r.scope, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sp, err := loadSpace(ctx, tx, r.hub.ID)
	if err != nil {
		return err
	}
	now := r.l.now().UTC().Truncate(time.Microsecond)
	w := &writer{tx: tx, meta: &Meta{Actor: r.actor, Scope: r.scope, Via: r.via, OccurredAt: now}}
	stream, err := nextSpaceStreamVersion(ctx, tx, sp.ID)
	if err != nil {
		return err
	}
	p := r.run.Progress
	rc := w.objectReceipt(sp, ObjectSpace, sp.ID, SpaceObjectRef, ActionSwitched, stream,
		fmt.Sprintf("Switched to V2: %d notes, %d proposals for Review, %d agents connected.", p.Notes+p.Personas+p.Configs,
			p.Proposed, p.Connected))
	if err := insertReceipt(ctx, tx, &rc); err != nil {
		return err
	}
	progress, _ := json.Marshal(p)
	if _, err := tx.Exec(ctx, `
		UPDATE v2.space_switches
		   SET state = 'switched', step = 'done', switched_at = $2, progress = $3, import_id = $4, error = NULL, updated_at = now()
		 WHERE space_id = $1`, sp.ID, now, progress, r.run.ImportID); err != nil {
		return fmt.Errorf("ledger: switch: %w", err)
	}
	if err := asLoginRole(ctx, tx, loginRole, func() error {
		_, err := tx.Exec(ctx, `UPDATE public.hubs SET v2_enabled_at = $2 WHERE id = $1`, sp.ID, now)
		return err
	}); err != nil {
		return fmt.Errorf("ledger: switch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBError(fmt.Errorf("ledger: switch: %w", err))
	}
	return nil
}
