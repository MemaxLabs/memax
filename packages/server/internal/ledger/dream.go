package ledger

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Dream editions (plan 25 §5.10, epic 2.2).
//
// Dream is a space's overnight upkeep, done in the open. internal/v2dream
// reads the space (DreamSnapshot), decides what to do (its phases, with
// the model where they need one) and hands the ledger a plan; the ledger
// applies it as one command, PublishEdition, as Dream (actor `dream`, via
// `system`):
//
//   - fold: notes become lineage of a kept memory (folded_from links). The
//     memory's words never change; new words only ever arrive as a
//     proposal.
//   - propose: a new fact from notes, as a proposal citing them. Its trust
//     is the minimum of the notes' and Dream's own (agent_own_work), so
//     Dream can't raise it, and the judge looks at it like any proposal.
//   - dedupe: a proposal that repeats another waiting one is folded into it
//     (merged, with a merged_into link) before Review.
//   - conflict: a memory that contradicts another kept one is flagged, with
//     a conflicts_with link, for a person to settle.
//   - stale: a kept memory whose stale_after date has passed is flagged.
//   - fade: a kept memory nobody has read in 60 days fades. Restorable,
//     never deleted; facts in files whose loads Memax can't observe, and
//     decisions in force, never fade.
//   - brief: a new Brief version from a few small operations on the one in
//     force (place, move, reword or add a line of prose), each citing the
//     memories it rests on. Never a rewrite.
//
// The ledger re-checks every planned action against the record as it is
// at publish time and skips what no longer applies (a proposal kept
// meanwhile, a memory forgotten, the Brief revised), so a run that takes a
// minute never acts on a stale picture. Each applied action writes its
// receipts, citing the edition (source {kind: dream, ref: D-0214}), and a
// dream_actions row with its inverse; the edition's own `published`
// receipt and row come last. UndoDreamAction applies an inverse as a
// person, refused when a later change depends on it, as Undo is.

// DreamActionKind is what one of an edition's actions did.
type DreamActionKind string

// The action kinds.
const (
	DreamFold     DreamActionKind = "fold"
	DreamPropose  DreamActionKind = "propose"
	DreamDedupe   DreamActionKind = "dedupe"
	DreamConflict DreamActionKind = "conflict"
	DreamStale    DreamActionKind = "stale"
	DreamFade     DreamActionKind = "fade"
	DreamBrief    DreamActionKind = "brief"
)

// DreamActionKinds lists every kind, in the order an edition applies them.
var DreamActionKinds = []DreamActionKind{DreamFold, DreamPropose, DreamDedupe, DreamConflict, DreamStale, DreamFade, DreamBrief}

// Valid reports whether k is a known kind.
func (k DreamActionKind) Valid() bool { return slices.Contains(DreamActionKinds, k) }

// DreamTrigger is why an edition ran.
type DreamTrigger string

// The triggers.
const (
	DreamScheduled DreamTrigger = "schedule" // the space's night came
	DreamManual    DreamTrigger = "manual"   // its owner asked Dream to run now
)

// Dream's receipt verbs and object kind (migration 047).
const (
	ActionPublished Action = "published" // an edition
	ActionFolded    Action = "folded"    // notes folded into a memory as lineage
	ActionRestored  Action = "restored"  // a faded memory, kept again
	ObjectDream            = "dream"
)

// Limits on what one edition may carry.
const (
	MaxDreamActions    = 400
	MaxDreamNotes      = 1000
	MaxNotesPerAction  = 200
	MaxBriefOps        = 10
	MaxDreamSurfaced   = 200
	DefaultDreamUndo   = 30 * 24 * time.Hour
	dreamSourceKind    = "dream"
	dreamNoteSourceKey = "note"
)

// WithDreamUndoWindow sets how long one of Dream's actions can be undone
// (DefaultDreamUndo).
func WithDreamUndoWindow(d time.Duration) Option { return func(l *Ledger) { l.dreamUndoWindow = d } }

// NoteRead is a note an edition read: who wrote it, never its words.
type NoteRead struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	// AuthorKind is agent or person.
	AuthorKind string `json:"author_kind"`
	// Agent is the agent's slug when an agent wrote it ("claude-code").
	Agent string `json:"agent,omitempty"`
	// Source is the surface it came through in V1 (mcp, web, cli, chat…).
	Source string `json:"source,omitempty"`
}

// NoteCursor is the last note an edition read; the next one reads after it.
type NoteCursor struct {
	At time.Time `json:"at"`
	ID uuid.UUID `json:"id"`
}

// DreamFact is a new fact Dream proposes from notes.
type DreamFact struct {
	Statement string          `json:"statement"`
	Section   Section         `json:"section"`
	Kind      Kind            `json:"kind,omitempty"`
	Decision  *DecisionFields `json:"decision,omitempty"`
}

// BriefOp is one small change to the Brief in force. Items are named by
// their identity in the version Dream read: a memory item by its ref
// ("M-0219"), a prose line by "P:<section key>:<index>" (BriefItemID).
type BriefOp struct {
	// Op is place (a kept memory the Brief doesn't place yet), move (a
	// placed memory to another position or section), reword (a prose
	// line, with new words and citations) or add (a new prose line).
	Op string `json:"op"`
	// Item is the item moved or reworded: a memory ref for place and move,
	// a prose id for reword.
	Item string `json:"item,omitempty"`
	// Section is the section key the item goes to (place, move, add).
	Section string `json:"section,omitempty"`
	// After puts it after this item (an id as Item takes); empty puts it
	// at the end of the section.
	After string `json:"after,omitempty"`
	// Text and Cites are the words of a prose line (reword, add). Every
	// line cites at least one kept memory: an uncited claim fails.
	Text  string   `json:"text,omitempty"`
	Cites []string `json:"cites,omitempty"`
}

// The Brief operations.
const (
	BriefOpPlace  = "place"
	BriefOpMove   = "move"
	BriefOpReword = "reword"
	BriefOpAdd    = "add"
)

// BriefDelta is a Brief action: operations on the version Dream read.
type BriefDelta struct {
	BaseVersion int       `json:"base_version"`
	Ops         []BriefOp `json:"ops"`
}

// PlannedAction is one change Dream decided on. PublishEdition re-checks
// it against the record and applies it, or skips it.
type PlannedAction struct {
	Kind DreamActionKind
	// Memory is the memory acted on: the kept memory notes fold into, the
	// proposal folded (dedupe), the flagged side (conflict), the stale or
	// fading memory. Version is its statement version when Dream read it.
	Memory  uuid.UUID
	Version int
	// Related is the proposal a duplicate folds into (dedupe) or the other
	// side of a conflict, at RelatedVersion.
	Related        uuid.UUID
	RelatedVersion int
	// Notes are the notes a fold or a new fact rests on.
	Notes []uuid.UUID
	// New is the fact proposed (propose).
	New *DreamFact
	// Brief is the Brief action's delta.
	Brief *BriefDelta
	// Saw lists every memory whose words the model read while planning
	// this action. If one of them is forgotten before the edition lands,
	// the action is skipped, so forgotten words can't come back through
	// what the model wrote.
	Saw []uuid.UUID
	// StaleAfter is the date a stale action is about.
	StaleAfter *time.Time
	// Unread is when a fading memory was last read or touched (fade).
	Unread *time.Time
}

// Surfaced is something an edition lists because it needs a person, though
// Dream didn't do it: a conflict the judge flagged since the last edition.
type Surfaced struct {
	Kind   string    `json:"kind"`
	Memory uuid.UUID `json:"memory_id"`
	With   uuid.UUID `json:"with_id,omitzero"`
}

// DreamModelUsage counts an edition's model calls: no prompts, no words.
type DreamModelUsage struct {
	Calls        int            `json:"calls"`
	Failures     int            `json:"failures,omitempty"`
	InputTokens  int            `json:"input_tokens,omitempty"`
	OutputTokens int            `json:"output_tokens,omitempty"`
	Tiers        map[string]int `json:"tiers,omitempty"`
	ZDR          bool           `json:"zdr,omitempty"`
}

// DreamStats are an edition's counts and timings. Never words.
type DreamStats struct {
	Model   *DreamModelUsage `json:"model,omitempty"`
	Timings map[string]int64 `json:"timings,omitempty"`
	// Plan and Cadence say what schedule it ran on ("pro", "nightly").
	Plan    string `json:"plan,omitempty"`
	Cadence string `json:"cadence,omitempty"`
	// Considered counts what each phase looked at.
	Considered map[string]int `json:"considered,omitempty"`
	// Changes counts the record changes since the previous edition.
	Changes int `json:"changes,omitempty"`
	// Filled by the ledger at publish time.
	Applied map[string]int `json:"applied,omitempty"`
	Skipped map[string]int `json:"skipped,omitempty"`
	Notes   []NoteRead     `json:"notes,omitempty"`
}

// CommandPublishEdition names PublishEdition.
const CommandPublishEdition CommandName = "publish_edition"

// PublishEdition applies a Dream run's plan and publishes its edition
// (D-), as Dream. Only Dream may; the key makes a retried run publish
// once. An edition is unique per (space, slot): a second run for the same
// night replays the first.
type PublishEdition struct {
	Meta
	SpaceID uuid.UUID
	// Slot is the night (or the run-now moment) the edition answers.
	Slot        time.Time
	Trigger     DreamTrigger
	RequestedBy uuid.UUID
	// Since and Until are the window of record changes it read.
	Since *time.Time
	Until time.Time
	// Cursor is the last note read (nil: the previous edition's stays).
	Cursor    *NoteCursor
	StartedAt time.Time
	Notes     []NoteRead
	Actions   []PlannedAction
	Surfaced  []Surfaced
	Stats     DreamStats
}

// Name implements Command.
func (*PublishEdition) Name() CommandName { return CommandPublishEdition }

func (c *PublishEdition) validate() error {
	if c.SpaceID == uuid.Nil {
		return invalid("space", "say which space the edition is for")
	}
	if c.Slot.IsZero() || c.Until.IsZero() || c.StartedAt.IsZero() {
		return invalid("slot", "an edition needs its slot, its window and when it started")
	}
	if c.Trigger != DreamScheduled && c.Trigger != DreamManual {
		return invalid("trigger", "use schedule or manual")
	}
	if (c.Trigger == DreamManual) != (c.RequestedBy != uuid.Nil) {
		return invalid("requested_by", "a manual run names the person who asked, and only a manual run does")
	}
	if c.Since != nil && c.Since.After(c.Until) {
		return invalid("since", "must not be after until")
	}
	if len(c.Notes) > MaxDreamNotes {
		return invalid("notes", "an edition reads at most %d notes", MaxDreamNotes)
	}
	if len(c.Actions) > MaxDreamActions {
		return invalid("actions", "an edition makes at most %d changes", MaxDreamActions)
	}
	if len(c.Surfaced) > MaxDreamSurfaced {
		c.Surfaced = c.Surfaced[:MaxDreamSurfaced]
	}
	c.Slot, c.Until, c.StartedAt = c.Slot.UTC().Truncate(time.Microsecond), c.Until.UTC().Truncate(time.Microsecond),
		c.StartedAt.UTC().Truncate(time.Microsecond)
	if c.Since != nil {
		s := c.Since.UTC().Truncate(time.Microsecond)
		c.Since = &s
	}
	for i := range c.Actions {
		if err := c.Actions[i].validate(); err != nil {
			return err
		}
	}
	return nil
}

func (a *PlannedAction) validate() error {
	if !a.Kind.Valid() {
		return invalid("actions.kind", "use fold, propose, dedupe, conflict, stale, fade or brief")
	}
	if len(a.Notes) > MaxNotesPerAction {
		return invalid("actions.notes", "an action rests on at most %d notes", MaxNotesPerAction)
	}
	switch a.Kind {
	case DreamFold:
		if a.Memory == uuid.Nil || len(a.Notes) == 0 {
			return invalid("actions", "a fold names the memory and the notes")
		}
	case DreamPropose:
		if a.New == nil || len(a.Notes) == 0 {
			return invalid("actions", "a new fact needs its words and the notes it rests on")
		}
		a.New.Statement = strings.TrimSpace(a.New.Statement)
		if err := checkText("actions.new.statement", a.New.Statement, MaxStatementRunes, true); err != nil {
			return err
		}
		if !a.New.Section.Valid() {
			return invalid("actions.new.section", "use decisions, conventions, preferences or open_question")
		}
		if a.New.Kind == "" {
			a.New.Kind = KindFact
		}
		if !a.New.Kind.Valid() || (a.New.Decision != nil && a.New.Kind != KindDecision) {
			return invalid("actions.new.kind", "use fact or decision")
		}
		if a.New.Decision != nil {
			if err := a.New.Decision.validate(); err != nil {
				return err
			}
		}
	case DreamDedupe, DreamConflict:
		if a.Memory == uuid.Nil || a.Related == uuid.Nil || a.Memory == a.Related {
			return invalid("actions", "a %s names two different memories", a.Kind)
		}
	case DreamStale, DreamFade:
		if a.Memory == uuid.Nil {
			return invalid("actions", "a %s names the memory", a.Kind)
		}
	case DreamBrief:
		if a.Brief == nil || len(a.Brief.Ops) == 0 || a.Brief.BaseVersion < 1 {
			return invalid("actions.brief", "a Brief action names the version it starts from and at least one change")
		}
		if len(a.Brief.Ops) > MaxBriefOps {
			return invalid("actions.brief", "a Brief action makes at most %d small changes", MaxBriefOps)
		}
	}
	return nil
}

// CommandUndoDream names UndoDreamAction.
const CommandUndoDream CommandName = "undo_dream"

// UndoDreamAction undoes one of an edition's actions
// (POST /v2/dream/actions/{action}:undo), as a person who may keep.
type UndoDreamAction struct {
	Meta
	Action uuid.UUID
}

// Name implements Command.
func (*UndoDreamAction) Name() CommandName { return CommandUndoDream }

// CommandRestore names Restore.
const CommandRestore CommandName = "restore"

// Restore keeps a faded memory again (plan 25 §5.5: faded → kept). Fading
// never deletes anything, and a person brings a fact back whenever they
// like, not only inside the undo window of the edition that faded it.
type Restore struct {
	Meta
	Memory string
	// ExpectedVersion, when set, must be the memory's version (If-Match).
	ExpectedVersion int
}

// Name implements Command.
func (*Restore) Name() CommandName { return CommandRestore }

// DreamEdition is one edition (D-): what Dream read and what it did.
type DreamEdition struct {
	ID          uuid.UUID    `json:"id"`
	Ref         string       `json:"ref"`
	N           int64        `json:"n"`
	SpaceID     uuid.UUID    `json:"space_id"`
	Slot        time.Time    `json:"slot"`
	Trigger     DreamTrigger `json:"trigger"`
	RequestedBy *uuid.UUID   `json:"requested_by,omitempty"`
	Since       *time.Time   `json:"since,omitempty"`
	Until       time.Time    `json:"until"`
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  time.Time    `json:"finished_at"`
	// NoteRefs are the N- refs of the notes it read, oldest first;
	// NotesRead counts them.
	NoteRefs  []string `json:"note_refs"`
	NotesRead int      `json:"notes_read"`
	// NotesBy counts the notes by who wrote them.
	NotesBy []NoteAuthorCount `json:"notes_by"`
	// FactRefs are the memories the notes became: the kept memories they
	// folded into and the facts proposed from them.
	FactRefs []string `json:"fact_refs"`
	// Counts are the actions by kind (applied), Undone those undone since.
	Counts map[DreamActionKind]int `json:"counts"`
	Undone int                     `json:"undone"`
	// NeedsYou counts what waits on a person: conflicts and stale facts it
	// flagged or found, that are still flagged.
	NeedsYou  int        `json:"needs_you"`
	Stats     DreamStats `json:"-"`
	ReceiptID uuid.UUID  `json:"receipt_id"`
	// Actions and Surfaced are filled by GetEdition.
	Actions  []DreamAction     `json:"actions,omitempty"`
	Surfaced []SurfacedMemory  `json:"surfaced,omitempty"`
	surfaced []Surfaced        `json:"-"`
	seq      int64             `json:"-"`
	notes    []uuid.UUID       `json:"-"`
	byNote   map[uuid.UUID]int `json:"-"`
}

// NoteAuthorCount counts an edition's notes by author.
type NoteAuthorCount struct {
	// Kind is agent, person or chat (a note captured in a chat app).
	Kind  string `json:"kind"`
	Agent string `json:"agent,omitempty"`
	Count int    `json:"count"`
}

// DreamAction is one of an edition's actions, with the memory it is about
// as it is now (its words read live, so a forgotten one has none).
type DreamAction struct {
	ID         uuid.UUID       `json:"id"`
	EditionID  uuid.UUID       `json:"edition_id"`
	EditionRef string          `json:"edition_ref"`
	N          int             `json:"n"`
	Kind       DreamActionKind `json:"kind"`
	Memory     *Memory         `json:"memory,omitempty"`
	// Version is the memory's statement version when Dream acted.
	Version int     `json:"version,omitempty"`
	Related *Memory `json:"related,omitempty"`
	// NoteRefs are the N- refs of the notes it rests on.
	NoteRefs []string `json:"note_refs"`
	// Brief is the Brief version a Brief action wrote, and how many
	// changes it made.
	Brief      *DreamBriefChange `json:"brief,omitempty"`
	ReceiptIDs []uuid.UUID       `json:"receipt_ids"`
	// Undone is set once a person undid it.
	Undone *DreamUndone `json:"undone,omitempty"`
	// Undoable says whether Undo may still apply (inside the window, not
	// undone, the memory not forgotten); a later change can still refuse.
	Undoable  bool      `json:"undoable"`
	CreatedAt time.Time `json:"created_at"`

	memoryID, relatedID uuid.UUID
	noteIDs             []uuid.UUID
	inverse             []byte
	spaceID             uuid.UUID
	briefID             uuid.UUID
	briefVersion        int
	receiptID           uuid.UUID
}

// DreamBriefChange is what a Brief action wrote.
type DreamBriefChange struct {
	Ref     string `json:"ref"`
	Version int    `json:"version"`
	Ops     int    `json:"ops"`
}

// DreamUndone is who undid an action, and when.
type DreamUndone struct {
	ReceiptID uuid.UUID  `json:"receipt_id"`
	By        *uuid.UUID `json:"by,omitempty"`
	At        time.Time  `json:"at"`
}

// SurfacedMemory is something an edition lists because it needs a person.
type SurfacedMemory struct {
	Kind   string  `json:"kind"`
	Memory *Memory `json:"memory"`
	With   *Memory `json:"with,omitempty"`
}

// dreamInverse is what undoing an action restores: the memories' states
// before (no words), the links it made, the Brief version it replaced.
type dreamInverse struct {
	Memories     []undoMemory `json:"memories"`
	LinksCreated []uuid.UUID  `json:"links_created"`
	// Created is set when the action created the memory (propose): undoing
	// it withdraws the proposal.
	Created bool `json:"created,omitempty"`
	// Brief is the Brief action's versions.
	BriefID       uuid.UUID `json:"brief_id,omitzero"`
	BriefBefore   int       `json:"brief_before,omitempty"`
	BriefAfter    int       `json:"brief_after,omitempty"`
	BriefOps      int       `json:"brief_ops,omitempty"`
	StaleAfter    string    `json:"stale_after,omitempty"`
	FoldedNoteIDs int       `json:"folded_notes,omitempty"`
}

// dreamSource is the receipt source of everything an edition does.
func dreamSource(ref string) *ReceiptSource { return &ReceiptSource{Kind: dreamSourceKind, Ref: ref} }

// BriefItemID is the identity an operation uses for an item of a Brief
// version: a memory item's ref, or "P:<section key>:<index>" for prose.
func BriefItemID(sectionKey string, index int, it BriefItem) string {
	if it.Ref != "" {
		return it.Ref
	}
	return fmt.Sprintf("P:%s:%d", sectionKey, index)
}

// briefEntry is an item with its identity in the base version.
type briefEntry struct {
	id   string
	item BriefItem
}

// ApplyBriefOps applies small operations to a Brief version's sections and
// returns the new sections. It refuses anything that isn't small and
// cited: an unknown item or section, a memory placed twice, prose without
// a citation. kept says whether a ref is a kept memory of the space. It is
// pure; the ledger checks the citations against the record again when it
// writes the version.
func ApplyBriefOps(base []BriefSection, ops []BriefOp, kept func(ref string) bool) ([]BriefSection, error) {
	if len(ops) == 0 {
		return nil, invalid("ops", "a Brief change has at least one operation")
	}
	if len(ops) > MaxBriefOps {
		return nil, invalid("ops", "at most %d small changes at a time; the Brief is never rewritten", MaxBriefOps)
	}
	type section struct {
		key, heading string
		items        []briefEntry
	}
	secs := make([]*section, 0, len(base))
	where := map[string]*section{}
	for _, s := range base {
		sec := &section{key: s.Key, heading: s.Heading}
		for i, it := range s.Items {
			id := BriefItemID(s.Key, i, it)
			sec.items = append(sec.items, briefEntry{id: id, item: it})
			where[id] = sec
		}
		secs = append(secs, sec)
	}
	findSection := func(key string) *section {
		for _, s := range secs {
			if s.key == key {
				return s
			}
		}
		return nil
	}
	remove := func(id string) (briefEntry, bool) {
		sec := where[id]
		if sec == nil {
			return briefEntry{}, false
		}
		for i, e := range sec.items {
			if e.id == id {
				sec.items = slices.Delete(sec.items, i, i+1)
				delete(where, id)
				return e, true
			}
		}
		return briefEntry{}, false
	}
	insert := func(sec *section, after string, e briefEntry) {
		pos := len(sec.items)
		if after != "" {
			for i, x := range sec.items {
				if x.id == after {
					pos = i + 1
					break
				}
			}
		}
		sec.items = slices.Insert(sec.items, pos, e)
		where[e.id] = sec
	}
	cited := func(op BriefOp) ([]string, error) {
		text := strings.TrimSpace(op.Text)
		if err := checkLine("ops.text", text, MaxBriefProseRunes, true); err != nil {
			return nil, err
		}
		if len(op.Cites) == 0 {
			return nil, invalid("ops.cites", uncitedClaim)
		}
		var refs []string
		for _, c := range op.Cites {
			ref, err := memoryRef("ops.cites", c)
			if err != nil {
				return nil, err
			}
			if !kept(ref) {
				return nil, invalid("ops.cites", "%s isn't a kept memory of this space", ref)
			}
			if !slices.Contains(refs, ref) {
				refs = append(refs, ref)
			}
		}
		if len(refs) > MaxBriefCites {
			return nil, invalid("ops.cites", "cite at most %d memories on one line", MaxBriefCites)
		}
		return refs, nil
	}
	added := 0
	for _, op := range ops {
		switch op.Op {
		case BriefOpPlace, BriefOpMove:
			ref, err := memoryRef("ops.item", op.Item)
			if err != nil {
				return nil, err
			}
			sec := findSection(op.Section)
			if sec == nil {
				return nil, invalid("ops.section", "the Brief has no section %q; Dream moves items between sections, it doesn't add them", op.Section)
			}
			if !kept(ref) {
				return nil, invalid("ops.item", "%s isn't a kept memory of this space", ref)
			}
			e := briefEntry{id: ref, item: BriefItem{Ref: ref}}
			if op.Op == BriefOpPlace {
				if where[ref] != nil {
					return nil, invalid("ops.item", "%s is placed already; move it instead", ref)
				}
			} else if _, ok := remove(ref); !ok {
				return nil, invalid("ops.item", "%s isn't in the Brief; place it instead", ref)
			}
			insert(sec, op.After, e)
		case BriefOpReword:
			sec := where[op.Item]
			if sec == nil || !strings.HasPrefix(op.Item, "P:") {
				return nil, invalid("ops.item", "reword names a line of prose in the Brief, like P:decisions:0")
			}
			refs, err := cited(op)
			if err != nil {
				return nil, err
			}
			for i := range sec.items {
				if sec.items[i].id == op.Item {
					if sec.items[i].item.Forgotten {
						return nil, invalid("ops.item", "that line's words were forgotten; add a new line instead")
					}
					sec.items[i].item = BriefItem{Text: strings.TrimSpace(op.Text), Cites: refs}
				}
			}
		case BriefOpAdd:
			sec := findSection(op.Section)
			if sec == nil {
				return nil, invalid("ops.section", "the Brief has no section %q", op.Section)
			}
			refs, err := cited(op)
			if err != nil {
				return nil, err
			}
			added++
			insert(sec, op.After, briefEntry{id: fmt.Sprintf("N:%d", added), item: BriefItem{Text: strings.TrimSpace(op.Text), Cites: refs}})
		default:
			return nil, invalid("ops.op", "use place, move, reword or add")
		}
	}
	out := make([]BriefSection, 0, len(secs))
	for _, s := range secs {
		items := make([]BriefItem, 0, len(s.items))
		for _, e := range s.items {
			items = append(items, e.item)
		}
		out = append(out, BriefSection{Key: s.key, Heading: s.heading, Items: items})
	}
	return out, nil
}

// marshalInverse encodes an inverse.
func marshalInverse(inv dreamInverse) ([]byte, error) {
	if inv.Memories == nil {
		inv.Memories = []undoMemory{}
	}
	if inv.LinksCreated == nil {
		inv.LinksCreated = []uuid.UUID{}
	}
	return json.Marshal(inv)
}
