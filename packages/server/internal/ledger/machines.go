package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Machines (personal-layer design §5, migration 056): the computers a
// person links to their Personal space. A personal space compiles to home
// targets, one set per machine, so each machine has its own baseline,
// holds, drift and Forget steps, through the same code as a repository's
// targets.
//
// LinkMachine is one command: the machine with its linked receipt, the
// space's first Brief when it has none, and one target per file the person
// approved on that machine, each with its configured receipt and its
// compile job, in one transaction, so a half-linked machine never exists.
// UnlinkMachine stops every target of the machine; its daemon then takes
// the layer off. Only the space's owner, as a person on a session, does
// either (policy.DecideMachine).

// The machine commands.
const (
	CommandLinkMachine   CommandName = "link_machine"
	CommandUnlinkMachine CommandName = "unlink_machine"
	CommandRenameMachine CommandName = "rename_machine"
)

// ObjectMachine is receipts.object_kind for a machine. Its object_ref is
// the machine's label (machine-2), never its name: a host name is
// personal data.
const ObjectMachine = "machine"

// ActionUnlinked is the receipt of UnlinkMachine. LinkMachine writes
// linked (ActionLinked, which the judge's links already use).
const ActionUnlinked Action = "unlinked"

// Limits on a machine.
const (
	MaxMachineNameRunes = 80
	// MaxMachineFiles bounds the files one link approves: one per agent at
	// most, and the canonical copy.
	MaxMachineFiles = 10
)

// Machine is a computer linked to a personal space.
type Machine struct {
	ID      uuid.UUID `json:"id"`
	SpaceID uuid.UUID `json:"space_id"`
	// Label is what receipts call it: machine-2.
	Label string `json:"label"`
	// Name is what its person calls it; the short host name by default.
	Name     string `json:"name"`
	Platform string `json:"platform"`
	// DeviceID is the CLI's device id on that machine.
	DeviceID uuid.UUID `json:"device_id"`
	// SessionID is the CLI session that linked it last.
	SessionID  *uuid.UUID `json:"session_id,omitempty"`
	Linked     bool       `json:"linked"`
	LinkedAt   time.Time  `json:"linked_at"`
	UnlinkedAt *time.Time `json:"unlinked_at,omitempty"`
	// LastSeenAt is when its daemon last read its targets (to five minutes).
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	// Version is the machine's version (its ETag): send it back as If-Match.
	Version          int       `json:"version"`
	CreatedReceiptID uuid.UUID `json:"created_receipt_id"`
	LastReceiptID    uuid.UUID `json:"last_receipt_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	seq int
}

// machineLabel is a machine's label from its number.
func machineLabel(seq int) string { return fmt.Sprintf("machine-%d", seq) }

// MachineFile is one file a link approves on the machine.
type MachineFile struct {
	Kind TargetKind `json:"kind"`
	// Path is relative to the machine's home folder; empty is the kind's
	// own (.claude/rules/memax-personal.md, .codex/AGENTS.md, the
	// canonical .memax/personal/AGENTS.md).
	Path string `json:"path,omitempty"`
	// UserOwned, when sent, must agree with the path: ~/.codex/AGENTS.md
	// and ~/.claude/CLAUDE.md are the person's, Memax manages one block in
	// them.
	UserOwned *bool `json:"user_owned,omitempty"`
	// ReadBy are the agents that load the file on this machine (default:
	// the kind's own agent; nothing for the canonical copy).
	ReadBy []string `json:"read_by,omitempty"`
}

// LinkMachine links a machine to a personal space (or links it again),
// with the files its person approved there.
type LinkMachine struct {
	Meta
	SpaceID uuid.UUID
	// DeviceKey is the CLI's device id; the same machine links again by it.
	DeviceKey   uuid.UUID
	MachineName string
	Platform    string
	// SessionID is the session the command comes from, from its token,
	// never from a request body (uuid.Nil when it names none).
	SessionID uuid.UUID
	Files     []MachineFile
}

// Name implements Command.
func (*LinkMachine) Name() CommandName { return CommandLinkMachine }

// UnlinkMachine takes the personal space off a machine: every target of it
// stops, and its daemon removes what Memax wrote there (rule 6: never a
// file someone edited).
type UnlinkMachine struct {
	Meta
	Machine uuid.UUID
}

// Name implements Command.
func (*UnlinkMachine) Name() CommandName { return CommandUnlinkMachine }

// RenameMachine changes what a machine is called.
type RenameMachine struct {
	Meta
	Machine uuid.UUID
	// ExpectedVersion is the machine's version (If-Match); 0 skips the check.
	ExpectedVersion int
	NewName         string
}

// Name implements Command.
func (*RenameMachine) Name() CommandName { return CommandRenameMachine }

var platformPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func checkMachineName(name string) error {
	return checkLine("name", name, MaxMachineNameRunes, true)
}

func (c *LinkMachine) validate() error {
	switch {
	case c.SpaceID == uuid.Nil:
		return invalid("space_id", "say which personal space to link the machine to")
	case c.DeviceKey == uuid.Nil:
		return invalid("device_id", "send the CLI's device id (~/.memax/config.json)")
	}
	c.MachineName = strings.TrimSpace(c.MachineName)
	if err := checkMachineName(c.MachineName); err != nil {
		return err
	}
	c.Platform = strings.ToLower(strings.TrimSpace(c.Platform))
	if !platformPattern.MatchString(c.Platform) {
		return invalid("platform", "use the platform's lowercase name, such as darwin or linux")
	}
	if len(c.Files) == 0 {
		return invalid("files", "approve at least one file to write on this machine")
	}
	if len(c.Files) > MaxMachineFiles {
		return invalid("files", "at most %d files on one machine", MaxMachineFiles)
	}
	kinds := map[TargetKind]bool{}
	for i := range c.Files {
		f := &c.Files[i]
		if !f.Kind.AtHome() {
			return invalid("files.kind", "%s isn't written into a home folder; use claude_user, codex_user, gemini_user, copilot_user or agents_md", f.Kind)
		}
		if kinds[f.Kind] {
			return invalid("files.kind", "%s is listed twice; a machine has one file per agent", f.Kind)
		}
		kinds[f.Kind] = true
		if f.Path == "" {
			f.Path = f.Kind.defaultPathAt(RootHome)
		}
		if err := checkHomePath(f.Kind, f.Path); err != nil {
			return err
		}
		if f.UserOwned != nil && *f.UserOwned != homeOwned(f.Kind, f.Path) {
			return userOwnedClash(f.Path, homeOwned(f.Kind, f.Path))
		}
		readBy, err := normalizeReadBy(f.ReadBy)
		if err != nil {
			return err
		}
		f.ReadBy = readBy
	}
	return nil
}

func (c *UnlinkMachine) validate() error {
	if c.Machine == uuid.Nil {
		return invalid("machine", "say which machine to unlink")
	}
	return nil
}

func (c *RenameMachine) validate() error {
	if c.Machine == uuid.Nil {
		return invalid("machine", "say which machine to rename")
	}
	if c.ExpectedVersion < 0 {
		return invalid("expected_version", "send the machine's version (If-Match)")
	}
	c.NewName = strings.TrimSpace(c.NewName)
	return checkMachineName(c.NewName)
}

// ---------------------------------------------------------------------
// Rows
// ---------------------------------------------------------------------

const machineSelect = `
	SELECT m.id, m.space_id, m.seq, m.device_key, m.name, m.platform, m.session_id, m.linked_at, m.unlinked_at,
	       m.last_seen_at, m.stream_version, m.created_receipt_id, m.last_receipt_id, m.created_at, m.updated_at
	  FROM v2.machines m`

func scanMachine(row pgx.Row) (*Machine, error) {
	var m Machine
	if err := row.Scan(&m.ID, &m.SpaceID, &m.seq, &m.DeviceID, &m.Name, &m.Platform, &m.SessionID, &m.LinkedAt,
		&m.UnlinkedAt, &m.LastSeenAt, &m.Version, &m.CreatedReceiptID, &m.LastReceiptID, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	m.Label = machineLabel(m.seq)
	m.Linked = m.UnlinkedAt == nil
	return &m, nil
}

// loadMachine reads one machine in scope; lock takes its row lock.
func loadMachine(ctx context.Context, tx pgx.Tx, scope Scope, id uuid.UUID, lock bool) (*Machine, error) {
	q := machineSelect + ` WHERE m.id = $1 AND m.space_id = ANY($2)`
	if lock {
		q += ` FOR UPDATE OF m`
	}
	m, err := scanMachine(tx.QueryRow(ctx, q, id, scope.SpaceIDs()))
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load machine: %w", err)
	}
	return m, nil
}

// linkedMachine is a machine of the space that is linked now, for a home
// target to be added to or turned back on.
func (w *writer) linkedMachine(ctx context.Context, spaceID, id uuid.UUID) (*Machine, error) {
	if id == uuid.Nil {
		return nil, invalid("machine", "say which machine the home file is on")
	}
	m, err := scanMachine(w.tx.QueryRow(ctx, machineSelect+` WHERE m.id = $1 AND m.space_id = $2 FOR SHARE OF m`, id, spaceID))
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load machine: %w", err)
	}
	if !m.Linked {
		return nil, &TargetStateError{Ref: m.Label,
			Message: "isn't linked any more; run memax link --home on it to link it again"}
	}
	return m, nil
}

// decideTarget is who may add or change a target: people who may keep, or
// for a home target, the machine's person (policy.DecideMachine).
func (w *writer) decideTarget(g SpaceGrant, sp spaceRow, root TargetRoot) policy.Decision {
	if root == RootHome {
		return policy.DecideMachine(w.policyActor(g), policy.MachineConfigure, sp.policy())
	}
	return policy.Decide(w.policyActor(g), policy.ActionConfigureTarget, policy.Object{}, sp.policy())
}

// decideReport is who may report a target's delivery or hand edit: a
// device or the repository, or for a home target only its person (design
// §5.2: an agent's key never reports a person's home files).
func (w *writer) decideReport(g SpaceGrant, sp spaceRow, t *Target) policy.Decision {
	if t.Root == RootHome {
		return policy.DecideMachine(w.policyActor(g), policy.MachineReport, sp.policy())
	}
	return policy.Decide(w.policyActor(g), policy.ActionReport, policy.Object{}, sp.policy())
}

// ---------------------------------------------------------------------
// LinkMachine
// ---------------------------------------------------------------------

// The first Personal Brief's sections: what the first Brief of a space
// linked to a machine holds, before anything is placed. A kept memory the
// Brief doesn't place compiles at the end of its own section, most-read
// first, so every kept fact reaches the files at once.
var firstPersonalSections = []BriefSection{
	{Key: "preferences", Heading: "Preferences", Items: []BriefItem{}},
	{Key: "conventions", Heading: "Conventions", Items: []BriefItem{}},
	{Key: "decisions", Heading: "Decisions", Items: []BriefItem{}},
	{Key: "open", Heading: "Open questions", Items: []BriefItem{}},
}

// FirstPersonalBriefReason is the first Personal Brief's receipt reason.
const FirstPersonalBriefReason = "The first Brief, written when you linked a machine"

// linkReads is what LinkMachine reads in one round trip.
type linkReads struct {
	machine *Machine
	brief   *briefRow
	// briefSeq is the first Brief's B- number, taken in the same round
	// trip when the space has no Brief (0 when it has one).
	briefSeq int64
	nextSeq  int
	targets  []*Target
}

func (w *writer) linkMachine(ctx context.Context, c *LinkMachine) (Result, error) {
	grant, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, replay, err := w.spaceAndClaim(ctx, c.SpaceID)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	dec := policy.DecideMachine(w.policyActor(grant), policy.MachineLink, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	r, err := w.linkReads(ctx, sp, c.DeviceKey)
	if err != nil {
		return Result{}, err
	}

	// The machine: new, linked again, or linked already.
	var session *uuid.UUID
	if c.SessionID != uuid.Nil {
		s := c.SessionID
		session = &s
	}
	m := r.machine
	newMachine := m == nil
	if newMachine {
		m = &Machine{ID: newID(), SpaceID: sp.ID, seq: r.nextSeq, DeviceID: c.DeviceKey, Linked: true}
		m.Label = machineLabel(m.seq)
	}
	machineChanged := newMachine || !m.Linked || m.Name != c.MachineName || m.Platform != c.Platform ||
		!sameUUID(m.SessionID, session)

	// The targets: one per approved file, new or turned back on. Another
	// file of the same agent on this machine (Codex moved to
	// AGENTS.override.md) stops, so each agent reads one copy.
	type addTarget struct {
		t  *Target
		rc Receipt
	}
	var adds []addTarget
	var reenable, stop []*Target
	approved := map[TargetKind]string{}
	for _, f := range c.Files {
		approved[f.Kind] = f.Path
		existing := slices.IndexFunc(r.targets, func(t *Target) bool { return t.Kind == f.Kind && t.Path == f.Path })
		if existing >= 0 {
			t := r.targets[existing]
			if t.SyncState == SyncOff {
				reenable = append(reenable, t)
			}
			continue
		}
		t := &Target{ID: newID(), SpaceID: sp.ID, TenantID: sp.TenantID, Kind: f.Kind, Root: RootHome,
			Machine: &TargetMachine{ID: m.ID, Label: m.Label, Name: c.MachineName}, Path: f.Path,
			Label: targetLabelAt(RootHome, f.Kind, f.Path), Settings: defaultSettingsAt(f.Kind, RootHome, f.Path),
			Delivery: DeliveryLocal, SyncState: SyncCompiling, Version: 1, DirtyGen: 1, CompiledGen: 0, OpenDrift: 0}
		if len(f.ReadBy) > 0 {
			t.Settings.ReadBy = f.ReadBy
		}
		adds = append(adds, addTarget{t: t})
	}
	for _, t := range r.targets {
		if p, ok := approved[t.Kind]; ok && p != t.Path && t.SyncState != SyncOff {
			stop = append(stop, t)
		}
	}
	if !machineChanged && len(adds) == 0 && len(reenable) == 0 && len(stop) == 0 {
		// Linked already, with every approved file compiling: nothing to write.
		targets, err := w.machineTargets(ctx, sp.ID, m.ID)
		if err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeApplied, Policy: dec, Machine: m, Targets: targets, Unchanged: true}, nil
	}

	// The first Brief, when the space has none, with the B- number the
	// reads took for it.
	var briefID, briefVersionID uuid.UUID
	briefSeq := r.briefSeq
	if r.brief == nil {
		if briefSeq == 0 {
			// A Brief was written between the two reads: link again.
			return Result{}, fmt.Errorf("%w: the space's Brief was being written", ErrBusy)
		}
		briefID, briefVersionID = newID(), newID()
	}

	// Every receipt, then every row, in one round trip. The receipts go
	// first: the rows name them, and the deferred trigger finds them.
	b := &pgx.Batch{}
	var receipts []*Receipt
	queue := func(rc Receipt) *Receipt {
		p := &rc
		receipts = append(receipts, p)
		queueReceipt(b, p)
		return p
	}
	var mrc *Receipt
	if machineChanged {
		version := 1
		if !newMachine {
			version = m.Version + 1
		}
		mrc = queue(w.objectReceipt(sp, ObjectMachine, m.ID, m.Label, ActionLinked, version, w.meta.Reason))
	}
	var brc *Receipt
	if r.brief == nil {
		brc = queue(w.objectReceipt(sp, ObjectBrief, briefID, FormatRef(PrefixBrief, briefSeq), ActionRevised, 1,
			FirstPersonalBriefReason))
	}
	for i := range adds {
		a := &adds[i]
		a.rc = *queue(w.objectReceipt(sp, ObjectTarget, a.t.ID, targetRef(a.t), ActionConfigured, 1, w.meta.Reason))
	}
	reenableRC := make([]*Receipt, len(reenable))
	for i, t := range reenable {
		reenableRC[i] = queue(w.objectReceipt(sp, ObjectTarget, t.ID, targetRef(t), ActionConfigured, t.Version+1, w.meta.Reason))
	}
	stopRC := make([]*Receipt, len(stop))
	for i, t := range stop {
		stopRC[i] = queue(w.objectReceipt(sp, ObjectTarget, t.ID, targetRef(t), ActionStopped, t.Version+1, w.meta.Reason))
	}

	if mrc != nil {
		if newMachine {
			b.Queue(`
				INSERT INTO v2.machines (id, tenant_id, space_id, seq, device_key, name, platform, session_id, linked_at,
				                         stream_version, created_receipt_id, last_receipt_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), 1, $9, $9)
				RETURNING linked_at, created_at, updated_at`,
				m.ID, sp.TenantID, sp.ID, m.seq, c.DeviceKey, c.MachineName, c.Platform, session, mrc.ID,
			).QueryRow(func(row pgx.Row) error {
				return row.Scan(&m.LinkedAt, &m.CreatedAt, &m.UpdatedAt)
			})
		} else {
			b.Queue(`
				UPDATE v2.machines
				   SET name = $3, platform = $4, session_id = $5, unlinked_at = NULL, stream_version = $6,
				       linked_at = CASE WHEN unlinked_at IS NULL THEN linked_at ELSE now() END,
				       last_receipt_id = $7, updated_at = now()
				 WHERE id = $1 AND space_id = $2
				RETURNING linked_at, updated_at`,
				m.ID, sp.ID, c.MachineName, c.Platform, session, mrc.StreamVersion, mrc.ID,
			).QueryRow(func(row pgx.Row) error {
				return row.Scan(&m.LinkedAt, &m.UpdatedAt)
			})
		}
		m.Name, m.Platform, m.SessionID, m.Linked, m.UnlinkedAt = c.MachineName, c.Platform, session, true, nil
		m.Version, m.LastReceiptID = mrc.StreamVersion, mrc.ID
		if newMachine {
			m.CreatedReceiptID = mrc.ID
		}
	}
	if brc != nil {
		structure, err := json.Marshal(briefStructure{Sections: firstPersonalSections})
		if err != nil {
			return Result{}, err
		}
		b.Queue(`
			INSERT INTO v2.briefs (id, tenant_id, space_id, current_version, stream_version, created_receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, 1, 1, $4, $4)`, briefID, sp.TenantID, sp.ID, brc.ID)
		b.Queue(`
			INSERT INTO v2.brief_versions (id, brief_id, version, tenant_id, space_id, seq, title, structure,
			                               receipt_id, last_receipt_id)
			VALUES ($1, $2, 1, $3, $4, $5, $6, $7, $8, $8)`,
			briefVersionID, briefID, sp.TenantID, sp.ID, briefSeq, personalBriefTitle(sp.Name), structure, brc.ID)
	}
	for i := range adds {
		a := &adds[i]
		settingsJSON, err := json.Marshal(a.t.Settings)
		if err != nil {
			return Result{}, err
		}
		a.t.CreatedReceiptID, a.t.LastReceiptID = a.rc.ID, a.rc.ID
		t := a.t
		b.Queue(`
			INSERT INTO v2.targets (id, tenant_id, space_id, kind, root, machine_id, path, settings, delivery, sync_state,
			                        dirty_gen, compiled_gen, stream_version, created_receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, $4, 'home', $5, $6, $7, 'local', 'compiling', 1, 0, 1, $8, $8)
			RETURNING dirty_at, created_at, updated_at`,
			t.ID, sp.TenantID, sp.ID, string(t.Kind), m.ID, t.Path, settingsJSON, a.rc.ID,
		).QueryRow(func(row pgx.Row) error {
			return row.Scan(&t.DirtyAt, &t.CreatedAt, &t.UpdatedAt)
		})
	}
	for i, t := range reenable {
		b.Queue(`
			UPDATE v2.targets
			   SET sync_state = 'compiling', dirty_gen = dirty_gen + 1, dirty_at = now(), stream_version = $3,
			       last_receipt_id = $4, updated_at = now()
			 WHERE id = $1 AND space_id = $2`, t.ID, sp.ID, reenableRC[i].StreamVersion, reenableRC[i].ID)
	}
	for i, t := range stop {
		b.Queue(`
			UPDATE v2.targets SET sync_state = 'off', stream_version = $3, last_receipt_id = $4, updated_at = now()
			 WHERE id = $1 AND space_id = $2`, t.ID, sp.ID, stopRC[i].StreamVersion, stopRC[i].ID)
	}
	if err := w.tx.SendBatch(ctx, b).Close(); err != nil {
		return Result{}, fmt.Errorf("ledger: link machine: %w", err)
	}
	for i, t := range stop {
		if err := w.closeObservations(ctx, t.ID, sp.ID, obsFilter{}, ObservationStopped, DriftStop, stopRC[i].ID, nil, nil); err != nil {
			return Result{}, err
		}
	}
	for _, a := range adds {
		w.enqueueCompile(a.t.ID, sp.ID)
	}
	for _, t := range reenable {
		w.enqueueCompile(t.ID, sp.ID)
	}

	res := Result{Outcome: OutcomeApplied, Policy: dec, Machine: m}
	for _, rc := range receipts {
		res.Receipts = append(res.Receipts, *rc)
	}
	if err := w.record(ctx, res, m.ID); err != nil {
		return Result{}, err
	}
	if len(reenable) == 0 && len(stop) == 0 && len(r.targets) == 0 {
		// Every target is new: its projection is what was just written.
		for _, a := range adds {
			res.Targets = append(res.Targets, *a.t)
		}
		sortTargets(res.Targets)
		return res, nil
	}
	if res.Targets, err = w.machineTargets(ctx, sp.ID, m.ID); err != nil {
		return Result{}, err
	}
	return res, nil
}

// linkReads reads, in one round trip, what LinkMachine decides from. The
// advisory lock goes first, so two links into one space (two machines at
// once) take their turns: the next machine number and the first Brief are
// decided by one of them at a time.
func (w *writer) linkReads(ctx context.Context, sp spaceRow, deviceKey uuid.UUID) (*linkReads, error) {
	spaceID := sp.ID
	r := &linkReads{}
	b := &pgx.Batch{}
	b.Queue(`SELECT pg_advisory_xact_lock(hashtextextended('v2.machines:' || $1::text, 0))`, spaceID)
	b.Queue(machineSelect+` WHERE m.space_id = $1 AND m.device_key = $2 FOR UPDATE OF m`, spaceID, deviceKey).
		QueryRow(func(row pgx.Row) error {
			m, err := scanMachine(row)
			if errNoRows(err) {
				return nil
			}
			r.machine = m
			return err
		})
	b.Queue(`
		SELECT b.id, b.current_version, b.stream_version, v.seq
		  FROM v2.briefs b
		  JOIN v2.brief_versions v ON v.brief_id = b.id AND v.version = b.current_version
		 WHERE b.space_id = $1
		   FOR NO KEY UPDATE OF b`, spaceID).QueryRow(func(row pgx.Row) error {
		var br briefRow
		err := row.Scan(&br.id, &br.version, &br.streamVersion, &br.seq)
		if errNoRows(err) {
			return nil
		}
		r.brief = &br
		return err
	})
	// The first Brief's B- number, taken only when the space has no Brief
	// (allocateRef's statement, behind the same lock); a number taken for
	// a Brief written meanwhile is a gap, which counters allow.
	b.Queue(`
		INSERT INTO v2.id_counters AS c (tenant_id, prefix, next)
		SELECT $1, $2, 2 WHERE NOT EXISTS (SELECT 1 FROM v2.briefs WHERE space_id = $3)
		ON CONFLICT (tenant_id, prefix) DO UPDATE SET next = c.next + 1
		RETURNING next - 1`, sp.TenantID, string(PrefixBrief), spaceID).QueryRow(func(row pgx.Row) error {
		err := row.Scan(&r.briefSeq)
		if errNoRows(err) {
			return nil
		}
		return err
	})
	b.Queue(`SELECT COALESCE(max(seq), 0) + 1 FROM v2.machines WHERE space_id = $1`, spaceID).
		QueryRow(func(row pgx.Row) error { return row.Scan(&r.nextSeq) })
	b.Queue(targetSelect+`
		 WHERE t.space_id = $1 AND t.machine_id = (SELECT id FROM v2.machines WHERE space_id = $1 AND device_key = $2)
		 ORDER BY t.id FOR NO KEY UPDATE OF t`, spaceID, deviceKey).Query(func(rows pgx.Rows) error {
		var err error
		r.targets, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Target, error) { return scanTarget(row) })
		return err
	})
	if err := w.tx.SendBatch(ctx, b).Close(); err != nil {
		return nil, fmt.Errorf("ledger: link machine: %w", err)
	}
	return r, nil
}

// personalBriefTitle is the first Personal Brief's title: the space's name.
func personalBriefTitle(name string) string {
	if name = strings.TrimSpace(name); name == "" || len([]rune(name)) > MaxBriefTitleRunes {
		return "Personal"
	}
	return name
}

// machineTargets reads a machine's targets, with their runs.
func (w *writer) machineTargets(ctx context.Context, spaceID, machineID uuid.UUID) ([]Target, error) {
	return listTargetsWhere(ctx, w.tx, w.meta.Scope, `t.space_id = $1 AND t.machine_id = $2`, spaceID, machineID)
}

// ---------------------------------------------------------------------
// UnlinkMachine and RenameMachine
// ---------------------------------------------------------------------

// openMachine loads and locks a machine, then claims the command's key in
// its space.
func (w *writer) openMachine(ctx context.Context, id uuid.UUID) (*Machine, SpaceGrant, spaceRow, *Result, error) {
	m, err := loadMachine(ctx, w.tx, w.meta.Scope, id, true)
	if err != nil {
		return nil, SpaceGrant{}, spaceRow{}, nil, err
	}
	grant, ok := w.meta.Scope.Grant(m.SpaceID)
	if !ok {
		return nil, SpaceGrant{}, spaceRow{}, nil, ErrNotFound
	}
	sp, replay, err := w.spaceAndClaim(ctx, m.SpaceID)
	return m, grant, sp, replay, err
}

func (w *writer) unlinkMachine(ctx context.Context, c *UnlinkMachine) (Result, error) {
	m, grant, sp, replay, err := w.openMachine(ctx, c.Machine)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	dec := policy.DecideMachine(w.policyActor(grant), policy.MachineUnlink, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if !m.Linked {
		targets, err := w.machineTargets(ctx, sp.ID, m.ID)
		if err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeApplied, Policy: dec, Machine: m, Targets: targets, Unchanged: true}, nil
	}
	receipts, err := w.unlink(ctx, sp, m, w.meta.Reason)
	if err != nil {
		return Result{}, err
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}
	if err := w.record(ctx, res, m.ID); err != nil {
		return Result{}, err
	}
	if res.Machine, err = loadMachine(ctx, w.tx, w.meta.Scope, m.ID, false); err != nil {
		return Result{}, err
	}
	if res.Targets, err = w.machineTargets(ctx, sp.ID, m.ID); err != nil {
		return Result{}, err
	}
	return res, nil
}

// unlink writes a machine's unlinked receipt and stops every target of it
// that compiles, each with its stopped receipt (both with reason), closing
// their open hand edits: nothing will compile over them now.
func (w *writer) unlink(ctx context.Context, sp spaceRow, m *Machine, reason string) ([]Receipt, error) {
	rows, err := w.tx.Query(ctx, `
		SELECT id, kind, path, stream_version FROM v2.targets
		 WHERE space_id = $1 AND machine_id = $2 AND sync_state <> 'off'
		 ORDER BY id FOR NO KEY UPDATE`, sp.ID, m.ID)
	if err != nil {
		return nil, fmt.Errorf("ledger: unlink machine: %w", err)
	}
	type row struct {
		id      uuid.UUID
		kind    TargetKind
		path    string
		version int
	}
	targets, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.id, &x.kind, &x.path, &x.version)
		return x, err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: unlink machine: %w", err)
	}
	b := &pgx.Batch{}
	var queued []*Receipt
	queue := func(rc Receipt) *Receipt {
		p := &rc
		queued = append(queued, p)
		queueReceipt(b, p)
		return p
	}
	mrc := queue(w.objectReceipt(sp, ObjectMachine, m.ID, m.Label, ActionUnlinked, m.Version+1, reason))
	stops := make([]*Receipt, len(targets))
	for i, t := range targets {
		stops[i] = queue(w.objectReceipt(sp, ObjectTarget, t.id, targetRef(&Target{Kind: t.kind, Root: RootHome, Path: t.path}),
			ActionStopped, t.version+1, reason))
	}
	b.Queue(`
		UPDATE v2.machines SET unlinked_at = now(), stream_version = $3, last_receipt_id = $4, updated_at = now()
		 WHERE id = $1 AND space_id = $2`, m.ID, sp.ID, mrc.StreamVersion, mrc.ID)
	for i, t := range targets {
		b.Queue(`
			UPDATE v2.targets SET sync_state = 'off', stream_version = $3, last_receipt_id = $4, updated_at = now()
			 WHERE id = $1 AND space_id = $2`, t.id, sp.ID, stops[i].StreamVersion, stops[i].ID)
	}
	if err := w.tx.SendBatch(ctx, b).Close(); err != nil {
		return nil, fmt.Errorf("ledger: unlink machine: %w", err)
	}
	for i, t := range targets {
		if err := w.closeObservations(ctx, t.id, sp.ID, obsFilter{}, ObservationStopped, DriftStop, stops[i].ID, nil, nil); err != nil {
			return nil, err
		}
	}
	out := make([]Receipt, len(queued))
	for i, rc := range queued {
		out[i] = *rc
	}
	return out, nil
}

// unlinkAll unlinks every machine of a personal space still linked: a
// Forget of the whole space (and an account's) takes the layer off every
// machine (design §5.4). Its receipts carry no reason: the space's reasons
// are redacted already, and the Forget's own may hold the words it took.
func (w *writer) unlinkAll(ctx context.Context, sp spaceRow) ([]Receipt, error) {
	if sp.Kind != policy.SpacePersonal {
		return nil, nil
	}
	rows, err := w.tx.Query(ctx, machineSelect+` WHERE m.space_id = $1 AND m.unlinked_at IS NULL ORDER BY m.seq FOR UPDATE OF m`, sp.ID)
	if err != nil {
		return nil, fmt.Errorf("ledger: unlink machines: %w", err)
	}
	machines, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Machine, error) { return scanMachine(r) })
	if err != nil {
		return nil, fmt.Errorf("ledger: unlink machines: %w", err)
	}
	var out []Receipt
	for _, m := range machines {
		rcs, err := w.unlink(ctx, sp, m, "")
		if err != nil {
			return nil, err
		}
		out = append(out, rcs...)
	}
	return out, nil
}

func (w *writer) renameMachine(ctx context.Context, c *RenameMachine) (Result, error) {
	m, grant, sp, replay, err := w.openMachine(ctx, c.Machine)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	if c.ExpectedVersion != 0 && c.ExpectedVersion != m.Version {
		return Result{}, &EditClashError{Ref: m.Label, Expected: c.ExpectedVersion, Current: m.Version}
	}
	dec := policy.DecideMachine(w.policyActor(grant), policy.MachineConfigure, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if c.NewName == m.Name {
		targets, err := w.machineTargets(ctx, sp.ID, m.ID)
		if err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeApplied, Policy: dec, Machine: m, Targets: targets, Unchanged: true}, nil
	}
	// The receipt names the machine by its label: the new name is personal
	// data and stays out of it.
	rc := w.objectReceipt(sp, ObjectMachine, m.ID, m.Label, ActionConfigured, m.Version+1, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	if err := execDeferred(ctx, w.tx, `
		UPDATE v2.machines SET name = $3, stream_version = $4, last_receipt_id = $5, updated_at = now()
		 WHERE id = $1 AND space_id = $2`, m.ID, sp.ID, c.NewName, rc.StreamVersion, rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: rename machine: %w", err)
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}
	if err := w.record(ctx, res, m.ID); err != nil {
		return Result{}, err
	}
	if res.Machine, err = loadMachine(ctx, w.tx, w.meta.Scope, m.ID, false); err != nil {
		return Result{}, err
	}
	if res.Targets, err = w.machineTargets(ctx, sp.ID, m.ID); err != nil {
		return Result{}, err
	}
	return res, nil
}

// ---------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------

// ListMachines returns the space's machines, linked or not, by number.
func (l *Ledger) ListMachines(ctx context.Context, scope Scope, spaceID uuid.UUID) ([]Machine, error) {
	if _, ok := scope.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	var out []Machine
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, machineSelect+` WHERE m.space_id = $1 AND m.space_id = ANY($2) ORDER BY m.seq`,
			spaceID, scope.SpaceIDs())
		if err != nil {
			return fmt.Errorf("ledger: list machines: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Machine, error) {
			m, err := scanMachine(r)
			if err != nil {
				return Machine{}, err
			}
			return *m, nil
		})
		return err
	})
	return out, err
}

// GetMachine returns one machine in scope.
func (l *Ledger) GetMachine(ctx context.Context, scope Scope, id uuid.UUID) (*Machine, error) {
	var out *Machine
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		m, err := loadMachine(ctx, tx, scope, id, false)
		out = m
		return err
	})
	return out, err
}

// SeenEvery is how often a machine's last_seen_at moves as its daemon polls.
const SeenEvery = 5 * time.Minute

// ListMachineTargets returns one machine's targets, each with its latest
// run: what that machine's daemon polls. It notes the machine as seen, at
// most once every SeenEvery, in the same round trip as the read (the
// bookkeeping column needs no receipt). ErrNotFound when the machine isn't
// the space's.
func (l *Ledger) ListMachineTargets(ctx context.Context, scope Scope, spaceID, machineID uuid.UUID) ([]Target, error) {
	if _, ok := scope.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	var out []Target
	err := l.writeBookkeeping(ctx, scope, func(tx pgx.Tx, _ string) error {
		var found bool
		var targets []*Target
		b := &pgx.Batch{}
		b.Queue(`
			UPDATE v2.machines SET last_seen_at = now()
			 WHERE id = $1 AND space_id = $2
			   AND (last_seen_at IS NULL OR last_seen_at < now() - $3::interval)`,
			machineID, spaceID, fmt.Sprintf("%d seconds", int(SeenEvery.Seconds())))
		b.Queue(`SELECT EXISTS (SELECT 1 FROM v2.machines WHERE id = $1 AND space_id = $2)`, machineID, spaceID).
			QueryRow(func(row pgx.Row) error { return row.Scan(&found) })
		b.Queue(targetSelect+`
			 WHERE t.space_id = $1 AND t.machine_id = $2 AND t.space_id = ANY($3)
			 ORDER BY array_position($4::text[], t.kind), t.path NULLS LAST, t.id`,
			spaceID, machineID, scope.SpaceIDs(), kindsOrder()).Query(func(rows pgx.Rows) error {
			var err error
			targets, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Target, error) { return scanTarget(r) })
			return err
		})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("ledger: list machine targets: %w", err)
		}
		if !found {
			return ErrNotFound
		}
		if err := fillTargets(ctx, tx, scope, targets); err != nil {
			return err
		}
		out = derefTargets(targets)
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------

// isMachineCommand reports whether a command's object is a machine: its
// replay loads the machine and its targets.
func isMachineCommand(n CommandName) bool {
	switch n {
	case CommandLinkMachine, CommandUnlinkMachine, CommandRenameMachine:
		return true
	}
	return false
}

// queueReceipt queues a receipt's insert on a batch, reading back what the
// database stamps.
func queueReceipt(b *pgx.Batch, rc *Receipt) {
	b.Queue(receiptInsertSQL, receiptArgs(rc)...).QueryRow(func(row pgx.Row) error {
		if err := row.Scan(&rc.Seq, &rc.RecordedAt); err != nil {
			return fmt.Errorf("ledger: write receipt: %w", err)
		}
		return nil
	})
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func derefTargets(ts []*Target) []Target {
	out := make([]Target, len(ts))
	for i, t := range ts {
		out[i] = *t
	}
	return out
}

// sortTargets orders targets as ListTargets does: in the compiler's order,
// then by path.
func sortTargets(ts []Target) {
	order := func(k TargetKind) int { return slices.Index(TargetKinds, k) }
	slices.SortStableFunc(ts, func(a, b Target) int {
		if d := order(a.Kind) - order(b.Kind); d != 0 {
			return d
		}
		return strings.Compare(a.Path, b.Path)
	})
}
